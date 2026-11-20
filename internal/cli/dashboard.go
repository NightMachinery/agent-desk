package cli

import (
	"encoding/json"
	"net/http"
	"strings"
)

// The sections of the dashboard's list, in their default order: what
// "sections" may name.
var dashboardSections = []string{"needs", "quota", "deliverables", "pinned", "responses", "new", "runs", "deferred"}

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
// "sections" is the list's sections in order, each named once.
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
	default:
		return nil, usagef("the shared dashboard setting is sections")
	}
	st, err := s.a.store()
	if err != nil {
		return nil, err
	}
	return map[string]string{}, st.PutDashboardPref(body.Key, body.Data, s.a.Now())
}
