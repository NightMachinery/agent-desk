package cli

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/NightMachinery/agent-desk/internal/ident"
	"github.com/NightMachinery/agent-desk/internal/ids"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// perfDesk uses synthetic data. A larger workload can be supplied as JSONL
// with AGENT_DESK_BENCH_FIXTURE, never by opening a real desk.
func perfDesk(b *testing.B) (*App, string) {
	b.Helper()
	now := time.Now()
	home := b.TempDir()
	a := &App{Host: "fixture-host", Home: b.TempDir(), Now: func() time.Time { return now },
		Stderr: io.Discard, Env: ident.Env{Home: home, Getenv: func(k string) string {
			switch k {
			case "PASEO_HOME", "AGENT_DESK_CONFIG":
				if os.Getenv("AGENT_DESK_BENCH_FIXTURE") != "" {
					return os.Getenv(k)
				}
			}
			return ""
		}}}
	st, err := a.store()
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { st.Close() })
	if fixture := os.Getenv("AGENT_DESK_BENCH_FIXTURE"); fixture != "" {
		f, err := os.Open(fixture)
		if err != nil {
			b.Fatal(err)
		}
		defer f.Close()
		events, err := st.Import(f)
		if err != nil {
			b.Fatal(err)
		}
		for _, e := range events {
			if e.Title == "Review the long thread" {
				return a, e.ID
			}
		}
		b.Fatal("fixture has no long thread")
	}
	var events []store.Event
	add := func(kind, path, ref string) string {
		id := ids.New(now.Add(time.Duration(len(events)-8000) * time.Second))
		events = append(events, store.Event{ID: id, TS: now.Format(time.RFC3339),
			Host: a.Host, Kind: kind, Path: path, Ref: ref, Body: strings.Repeat("Fixture text. ", 12), Meta: []byte("{}")})
		return id
	}
	const node = "project-0/run/coord"
	root := add("tell", node, "")
	for i := 0; i < 90; i++ {
		m := add("reply", node, root)
		add("delivered", node, m)
		add("tell", node, m)
	}
	for i := 0; len(events) < 7500; i++ {
		path := fmt.Sprintf("project-%d/run-%d/coord", i%5, i%60)
		switch (i / 60) % 12 {
		case 0:
			add("join", path, "")
		case 1, 2, 3, 4, 5, 6:
			add("set", path+"/status", "")
			add("set", path+"/next", "")
		case 7, 8:
			add("instruction", path, "")
		case 9:
			add("read", path, add("tell", path, ""))
		case 10:
			add("delivered", path, add("answer", path, add("ask", path, "")))
		default:
			add("tell", path, "")
		}
	}
	if err := st.Append(events...); err != nil {
		b.Fatal(err)
	}
	return a, root
}

func BenchmarkDeskLoad(b *testing.B) {
	a, _ := perfDesk(b)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := a.load(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDashboard(b *testing.B) {
	a, root := perfDesk(b)
	s, err := newServer(a, fstest.MapFS{"index.html": {Data: []byte("<html></html>")}})
	if err != nil {
		b.Fatal(err)
	}
	h := s.routes()
	for _, run := range []struct{ name, url string }{
		{"View", "/api/view"},
		{"Item", "/api/item?id=" + root},
	} {
		b.Run(run.name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, run.url, nil)
			b.ReportAllocs()
			for b.Loop() {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					b.Fatalf("%s: %d %s", run.url, w.Code, w.Body.String())
				}
			}
		})
	}
	b.Run("StreamTick", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := s.stamp(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
