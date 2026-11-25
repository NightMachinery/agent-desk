package cli

import (
	"github.com/NightMachinery/agent-desk/internal/desk"
)

// apiSeen counts one open of an item in the dashboard (POST api/seen {id},
// the event's full id). Auto-navigation goes to the items opened least
// first; the count is on the desk, so every device walks the same order.
func (s *server) apiSeen(_ *App, body request) (any, error) {
	st, err := s.a.store()
	if err != nil {
		return nil, err
	}
	if body.ID == "" {
		return nil, usagef("seen needs the item's id")
	}
	if _, err := st.Get(body.ID); err != nil {
		return nil, usagef("no item %s", body.ID)
	}
	n, err := st.AddView(body.ID, s.a.Now())
	if err != nil {
		return nil, err
	}
	return map[string]int{"views": n}, nil
}

// viewsStamp is the open counts' part of the page's stamp; "" when it
// cannot be read, which only delays other devices' reload.
func (s *server) viewsStamp() string {
	st, err := s.a.store()
	if err != nil {
		return ""
	}
	v, _ := st.ViewsStamp()
	return v
}

// markViewed sets each listed item's open count (Item.Views).
func (s *server) markViewed(v *desk.View) {
	st, err := s.a.store()
	if err != nil {
		return
	}
	groups := [][]desk.Group{v.NeedsYou, v.Deliverables, v.Responses, v.New}
	var ids []string
	for _, gs := range groups {
		for _, g := range gs {
			for _, it := range g.Items {
				ids = append(ids, it.Event.ID)
			}
		}
	}
	for _, it := range v.Pinned {
		ids = append(ids, it.Event.ID)
	}
	counts, err := st.Views(ids)
	if err != nil || len(counts) == 0 {
		return
	}
	mark := func(items []desk.Item) {
		for i := range items {
			items[i].Views = counts[items[i].Event.ID]
		}
	}
	mark(v.Pinned)
	for _, gs := range groups {
		for _, g := range gs {
			mark(g.Items)
		}
	}
}
