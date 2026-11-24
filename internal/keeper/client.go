// Package keeper talks to the local agent-quotas daemon.
package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

var ErrUnavailable = errors.New("agent-quotas is not running")

const (
	readTimeout = 15 * time.Second // keeper reads can take several seconds
	// listTimeout is longer: the list is read in the background, and the
	// keeper can take several seconds to build it.
	listTimeout = 30 * time.Second
)

type Client struct {
	Socket string
	Home   string // Paseo home
}

type Agent struct {
	ID             string   `json:"id"`
	Big            bool     `json:"big"`
	CacheExpiresAt *float64 `json:"cache_expires_at"`
	WarmUntil      *float64 `json:"warm_until"`
	Archived       bool     `json:"archived"`
	Cache          *Cache   `json:"cache"`
	LockPath       string   `json:"lock_path"`
	Seat           *Seat    `json:"seat"`
	// LastActivityAt is its last real turn (Unix seconds), not a keeper ping.
	LastActivityAt *float64      `json:"last_activity_at"`
	AutoContinue   *AutoContinue `json:"auto_continue"`
}

// AutoContinue is an agent's auto-continue registration. SetBy is who
// decided: default for a default (agent-desk's inherited setting, or the
// keeper's own), empty for no decision or an old registration, else the
// agent itself, the desk's checkbox, the CLI or the API.
type AutoContinue struct {
	Enabled bool   `json:"enabled"`
	SetBy   string `json:"set_by"`
}

// Seat is the account an agent runs on and its verdict for the agent's
// model: usable, blocked or unknown. AvailableAt (Unix seconds) is when a
// blocked one has room for that model again, where the keeper knows it per
// model (Antigravity pools); otherwise the account's BlockedUntil applies.
type Seat struct {
	Account     string   `json:"account"`
	Verdict     string   `json:"verdict"`
	AvailableAt *float64 `json:"available_at"`
}

// Account is one account's usage snapshot, as much as the desk reads.
// BlockedUntil is the latest reset among its blocking windows (Unix
// seconds), when it is blocked.
type Account struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	Verdict        string   `json:"verdict"`
	BlockedUntil   *float64 `json:"blocked_until"`
	BlockedReasons []string `json:"blocked_reasons"`
}

// Accounts reads every account's last snapshot (no refresh).
func (c Client) Accounts(ctx context.Context) ([]Account, error) {
	var result struct {
		Accounts []Account `json:"accounts"`
	}
	err := c.request(ctx, readTimeout, http.MethodGet, "/v1/accounts", nil, nil, &result)
	return result.Accounts, err
}

func (a Agent) Cold(now time.Time) bool {
	return a.Big && a.CacheExpiresAt != nil && *a.CacheExpiresAt < float64(now.Unix())+float64(now.Nanosecond())/1e9
}

type Cache struct {
	Policy     string      `json:"policy"`
	Phase      string      `json:"phase"`
	WarmHours  float64     `json:"warm_hours"`
	Compaction *Compaction `json:"compaction"`
}

type Compaction struct {
	ID          string  `json:"id"`
	State       string  `json:"state"`
	Error       string  `json:"error"`
	RequestedAt float64 `json:"requested_at"`
}

type APIError struct {
	Status  int
	Message string
}

func (e APIError) Error() string { return "agent-quotas: " + e.Message }

type PolicyRequest struct {
	Cache     string  `json:"cache,omitempty"`
	WarmHours float64 `json:"warm_hours,omitempty"`
	SetBy     string  `json:"set_by"`
}

type List struct {
	Enabled bool    `json:"keeper_enabled"`
	Agents  []Agent `json:"agents"`
}

func (c Client) Agents(ctx context.Context, all bool) (List, error) {
	var result List
	q := url.Values{}
	if all {
		q.Set("all", "1")
	}
	err := c.request(ctx, listTimeout, http.MethodGet, "/v1/agents", q, nil, &result)
	return result, err
}

func (c Client) Agent(ctx context.Context, id string) (Agent, error) {
	return c.agent(ctx, readTimeout, http.MethodGet, agentPath(id), nil, nil)
}

func (c Client) Policy(ctx context.Context, id, action string, value PolicyRequest) (Agent, error) {
	return c.agent(ctx, 15*time.Second, http.MethodPost, agentPath(id)+"/"+action, nil, value)
}

// SetAutoContinue records an auto-continue decision. With setBy "default"
// it is a default, which agent-quotas writes only over no decision or an
// earlier default; reset makes it replace a decision made for the agent too.
func (c Client) SetAutoContinue(ctx context.Context, id string, enabled bool, setBy string, reset bool) (Agent, error) {
	body := map[string]any{"enabled": enabled, "set_by": setBy}
	if reset {
		body["reset"] = true
	}
	return c.agent(ctx, 15*time.Second, http.MethodPost, agentPath(id)+"/auto-continue", nil, body)
}

func (c Client) Compact(ctx context.Context, id string) (Compaction, error) {
	var result struct {
		ID    string `json:"compaction_id"`
		State string `json:"state"`
	}
	err := c.request(ctx, 30*time.Second, http.MethodPost, agentPath(id)+"/compact", nil, nil, &result)
	return Compaction{ID: result.ID, State: result.State}, err
}

func (c Client) Wait(ctx context.Context, id, compactionID string) (Agent, error) {
	q := url.Values{"wait_compaction": {compactionID}, "timeout": {"60"}}
	return c.agent(ctx, 65*time.Second, http.MethodGet, agentPath(id), q, nil)
}

func agentPath(id string) string { return "/v1/agents/" + url.PathEscape(id) }

func (c Client) agent(ctx context.Context, timeout time.Duration, method, path string, q url.Values, body any) (Agent, error) {
	var result struct {
		Agent Agent `json:"agent"`
	}
	err := c.request(ctx, timeout, method, path, q, body, &result)
	return result.Agent, err
}

func (c Client) request(ctx context.Context, timeout time.Duration, method, path string, q url.Values, body any, result any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("home", c.Home)
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://keeper"+path+"?"+q.Encode(), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	dialer := net.Dialer{Timeout: time.Second}
	client := http.Client{Timeout: timeout, Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", c.Socket)
		},
	}}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
			return ErrUnavailable
		}
		return fmt.Errorf("agent-quotas request: %w", err)
	}
	defer res.Body.Close()
	data := json.NewDecoder(io.LimitReader(res.Body, 16<<20))
	if res.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		if data.Decode(&failure) != nil || failure.Error == "" {
			failure.Error = res.Status
		}
		return APIError{Status: res.StatusCode, Message: failure.Error}
	}
	return data.Decode(result)
}
