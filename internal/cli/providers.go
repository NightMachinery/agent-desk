package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/NightMachinery/agent-desk/internal/ident"
)

// ProviderChoice is a Paseo provider the dashboard offers for a new agent
// (Replace on an agent out of quota): its models with their thinking
// options, and the account it runs on with that account's quota, when the
// agent-quotas keeper knows it.
type ProviderChoice struct {
	ID     string          `json:"id"`
	Label  string          `json:"label"`
	Models []ProviderModel `json:"models"`
	// Account is the keeper's account id (such as claude:work), learnt from
	// the live agents on this provider; empty when none has a seat.
	Account      string   `json:"account,omitempty"`
	AccountLabel string   `json:"account_label,omitempty"`
	Verdict      string   `json:"verdict,omitempty"` // usable, blocked or unknown
	AvailableAt  *float64 `json:"available_at,omitempty"`
	Reasons      []string `json:"reasons,omitempty"` // why it is blocked
}

// ProviderModel is one model a provider offers, in Paseo's order (the
// provider's preferred first).
type ProviderModel struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	Description     string   `json:"description,omitempty"`
	Thinking        []string `json:"thinking"`
	DefaultThinking string   `json:"default_thinking,omitempty"`
}

// providerChoices asks Paseo for every enabled, available provider and its
// models (in parallel), then joins each with its keeper account.
func (a *App) providerChoices(ctx context.Context) ([]ProviderChoice, error) {
	out, err := a.runExt(ctx, nil, "paseo", "provider", "ls", "--json")
	if err != nil {
		return nil, fmt.Errorf("paseo provider ls: %v", paseoError(out))
	}
	var list []struct {
		Provider string `json:"provider"`
		Label    string `json:"label"`
		Status   string `json:"status"`
		Enabled  string `json:"enabled"`
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, fmt.Errorf("paseo provider ls: %v", err)
	}
	choices := []ProviderChoice{}
	for _, p := range list {
		if p.Status == "available" && p.Enabled == "Enabled" {
			choices = append(choices, ProviderChoice{ID: p.Provider, Label: p.Label, Models: []ProviderModel{}})
		}
	}
	var wg sync.WaitGroup
	for i := range choices {
		wg.Add(1)
		go func(c *ProviderChoice) {
			defer wg.Done()
			out, err := a.runExt(ctx, nil, "paseo", "provider", "models", c.ID, "--json")
			if err != nil {
				return // offered without models: the provider picks its default
			}
			var ms []struct {
				Model    string   `json:"model"`
				ID       string   `json:"id"`
				Desc     string   `json:"description"`
				Thinking []string `json:"thinkingOptionIds"`
				Default  string   `json:"defaultThinkingOptionId"`
			}
			if json.Unmarshal([]byte(out), &ms) != nil {
				return
			}
			for _, m := range ms {
				if m.ID == "" {
					continue
				}
				if m.Thinking == nil {
					m.Thinking = []string{}
				}
				c.Models = append(c.Models, ProviderModel{ID: m.ID, Label: m.Model, Description: m.Desc, Thinking: m.Thinking, DefaultThinking: m.Default})
			}
		}(&choices[i])
	}
	wg.Wait()
	a.providerAccounts(ctx, choices)
	return choices, nil
}

// providerAccounts fills in each provider's account. The keeper names
// accounts per agent, not per Paseo provider (a second Claude login is its
// own Paseo provider), so the account is the one most of the provider's
// live agents are seated on. Without the keeper the choices stay as they
// are.
func (a *App) providerAccounts(ctx context.Context, choices []ProviderChoice) {
	client, err := a.keeperClient()
	if err != nil {
		return
	}
	list, err := client.Agents(ctx, true)
	if err != nil {
		return
	}
	accounts, err := client.Accounts(ctx)
	if err != nil {
		return
	}
	seatOf := map[string]string{}
	for _, ag := range list.Agents {
		if ag.Seat != nil && ag.Seat.Account != "" {
			seatOf[ag.ID] = ag.Seat.Account
		}
	}
	votes := map[string]map[string]int{} // provider -> account -> agents
	for _, r := range ident.PaseoRecords(a.paseoHome()) {
		if acc := seatOf[r.ID]; acc != "" {
			if votes[r.Provider] == nil {
				votes[r.Provider] = map[string]int{}
			}
			votes[r.Provider][acc]++
		}
	}
	byID := map[string]int{}
	for i, acc := range accounts {
		byID[acc.ID] = i
	}
	for i := range choices {
		v := votes[choices[i].ID]
		if len(v) == 0 {
			continue
		}
		names := make([]string, 0, len(v))
		for acc := range v {
			names = append(names, acc)
		}
		sort.Slice(names, func(x, y int) bool {
			if v[names[x]] != v[names[y]] {
				return v[names[x]] > v[names[y]]
			}
			return names[x] < names[y]
		})
		c := &choices[i]
		c.Account = names[0]
		if j, ok := byID[c.Account]; ok {
			acc := accounts[j]
			c.AccountLabel, c.Verdict, c.Reasons = acc.Label, acc.Verdict, acc.BlockedReasons
			if acc.Verdict == "blocked" {
				c.AvailableAt = acc.BlockedUntil
			}
		}
	}
}

// providersTTL is how long the provider list is served before a background
// refresh. Models change with Paseo releases; quota verdicts change more
// often, so the keeper half is refreshed on the same clock.
const providersTTL = 5 * time.Minute

// apiProviders serves the provider choices: from the last refresh when
// there is one (refreshing it in the background once stale), else after
// asking Paseo, which takes a second or two. ?fresh=1 waits for a refresh.
func (s *server) apiProviders(w http.ResponseWriter, r *http.Request) {
	s.provMu.Lock()
	resp, at := s.provResp, s.provAt
	fresh := r.URL.Query().Get("fresh") != ""
	if resp != nil && !fresh {
		if time.Since(at) >= providersTTL && !s.provRefreshing {
			s.provRefreshing = true
			go s.refreshProviders()
		}
		s.provMu.Unlock()
		writeJSON(w, map[string]any{"providers": resp})
		return
	}
	s.provMu.Unlock()
	choices, err := s.loadProviders(r.Context())
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]any{"providers": choices})
}

func (s *server) loadProviders(ctx context.Context) ([]ProviderChoice, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	app, _, _ := s.userApp()
	choices, err := app.providerChoices(ctx)
	if err != nil {
		return nil, err
	}
	s.provMu.Lock()
	s.provResp, s.provAt = choices, time.Now()
	s.provMu.Unlock()
	return choices, nil
}

func (s *server) refreshProviders() {
	_, _ = s.loadProviders(context.Background())
	s.provMu.Lock()
	s.provRefreshing = false
	s.provMu.Unlock()
}
