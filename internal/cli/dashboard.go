package cli

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

// The sections of the dashboard's list, in their default order: what
// "sections" may name.
var dashboardSections = []string{"needs", "quota", "deliverables", "pinned", "responses", "tasks", "new", "runs", "deferred"}

// navKinds are the kinds of item auto-navigation ranks (the "nav" setting):
// questions the agent waits on, questions asked with a default, then
// deliverables, responses to the user, and notices (the New section).
var navKinds = []string{"blocked", "default", "deliverable", "response", "notice"}

// navPrefs is the "nav" setting: auto-navigation's order (web/app's
// lib/moveon.js). Views puts the items opened least first; Kinds ranks
// them after that; Age breaks the remaining ties, "old" or "new" first.
type navPrefs struct {
	Views bool     `json:"views"`
	Kinds []string `json:"kinds"`
	Age   string   `json:"age"`
}

func checkNavPrefs(data json.RawMessage) error {
	var p navPrefs
	if err := json.Unmarshal(data, &p); err != nil {
		return usagef("nav is {views, kinds, age}: %v", err)
	}
	if p.Age != "old" && p.Age != "new" {
		return usagef("nav's age is old or new, not %q", p.Age)
	}
	seen := map[string]bool{}
	for _, k := range p.Kinds {
		if !slices.Contains(navKinds, k) || seen[k] {
			return usagef("nav's kinds name each of %s once; %q is not one or is repeated", strings.Join(navKinds, ", "), k)
		}
		seen[k] = true
	}
	if len(seen) != len(navKinds) {
		return usagef("nav's kinds name each of %s once", strings.Join(navKinds, ", "))
	}
	return nil
}

// apiDashboardPrefs returns the dashboard settings shared by every device
// (GET api/dashboard-prefs): {"sections": [...]} once the order is set.
func (s *server) apiDashboardPrefs(w http.ResponseWriter, r *http.Request) {
	st, err := s.a.store()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	prefs, err := st.DashboardPrefs()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, prefs)
}

// apiDashboardPrefPut sets one shared dashboard setting (POST
// api/dashboard-prefs {key, data}); a null data puts back the default.
// "sections" is the list's sections in order, each named once; "render"
// is when the server renders the org files (orgfiles.go); "task-changes"
// is whether and when it tells agents of the user's task changes
// (taskchanges.go); "nav" is auto-navigation's order (navPrefs).
func (s *server) apiDashboardPrefPut(_ *App, body request) (any, error) {
	switch body.Key {
	case "sections":
		if len(body.Data) > 0 && string(body.Data) != "null" {
			var order []string
			if err := json.Unmarshal(body.Data, &order); err != nil {
				return nil, usagef("sections is a list of section names")
			}
			seen := map[string]bool{}
			for _, n := range order {
				known := false
				for _, k := range dashboardSections {
					known = known || k == n
				}
				if !known || seen[n] {
					return nil, usagef("sections names each of %s once; %q is not one or is repeated", strings.Join(dashboardSections, ", "), n)
				}
				seen[n] = true
			}
		}
	case "render":
		if len(body.Data) > 0 && string(body.Data) != "null" {
			if err := checkRenderPrefs(body.Data); err != nil {
				return nil, err
			}
		}
	case "task-changes":
		if len(body.Data) > 0 && string(body.Data) != "null" {
			if err := checkTaskChangePrefs(body.Data); err != nil {
				return nil, err
			}
		}
	case "nav":
		if len(body.Data) > 0 && string(body.Data) != "null" {
			if err := checkNavPrefs(body.Data); err != nil {
				return nil, err
			}
		}
	default:
		return nil, usagef("the shared dashboard settings are sections, render, task-changes and nav")
	}
	st, err := s.a.store()
	if err != nil {
		return nil, err
	}
	if err := st.PutDashboardPref(body.Key, body.Data, s.a.Now()); err != nil {
		return nil, err
	}
	// Turned on: bring the files up to date now, not at the next change.
	if body.Key == "render" && s.renderPrefs().Enabled {
		go s.renderOrg()
	}
	return map[string]string{}, nil
}
