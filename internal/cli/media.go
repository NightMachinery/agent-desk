package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NightMachinery/agent-desk/internal/desk"
	"github.com/NightMachinery/agent-desk/internal/media"
	"github.com/NightMachinery/agent-desk/internal/store"
)

// attachOpts are the options of a command that takes attachments.
var attachOpts = []string{"attach", "caption"}

// attach copies the files named by --attach into the desk and records them
// in extra["media"]. Each --attach needs a --caption after it.
func (a *App) attach(o opts, extra map[string]any) error {
	refs, err := a.attachRefs(o, true)
	if err != nil || len(refs) == 0 {
		return err
	}
	extra["media"] = refs
	return nil
}

// attachRefs copies the files named by --attach into the desk. A --caption
// after an --attach describes it; the user's own (needCaption false) may
// leave it out, and the file's name stands in.
func (a *App) attachRefs(o opts, needCaption bool) ([]media.Ref, error) {
	type pair struct{ file, caption string }
	var ps []pair
	for _, kv := range o.seq {
		switch kv[0] {
		case "attach":
			ps = append(ps, pair{file: kv[1]})
		case "caption":
			if len(ps) == 0 || ps[len(ps)-1].caption != "" {
				return nil, usagef("--caption describes the --attach before it: --attach FILE --caption 'what it shows'")
			}
			ps[len(ps)-1].caption = kv[1]
		}
	}
	if len(ps) == 0 {
		return nil, nil
	}
	if len(ps) > media.MaxPerItem {
		return nil, usagef("at most %d attachments per item, not %d", media.MaxPerItem, len(ps))
	}
	if _, err := a.store(); err != nil {
		return nil, err
	}
	var refs []media.Ref
	for _, p := range ps {
		if strings.TrimSpace(p.caption) == "" && !needCaption {
			p.caption = filepath.Base(p.file)
		}
		if strings.TrimSpace(p.caption) == "" {
			return nil, usagef("%s needs a caption: --attach FILE --caption 'what it shows and why it matters'", p.file)
		}
		f := p.file
		if !filepath.IsAbs(f) && a.Env.Cwd != "" {
			f = filepath.Join(a.Env.Cwd, f)
		}
		r, err := media.Add(a.Home, f, p.caption)
		if err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, nil
}

// keepDays is media_keep_days, else 30; 0 keeps attachments for good.
func (a *App) keepDays() int {
	c, _ := a.config()
	if c.MediaKeepDays != nil {
		return *c.MediaKeepDays
	}
	return 30
}

// collectable reports whether the attachments of e may go: e is not pinned,
// is read (a notice or report) or answered or dismissed (a question), and was
// so at least keep ago. The clock starts when the user dealt with the item,
// not when it was filed: one read on day 29 keeps its files another keep.
// An item read without a mark (a replaced deliverable) counts from filing.
func collectable(d *desk.Desk, e store.Event, keep time.Duration) bool {
	if d.Pinned(e) {
		return false
	}
	if e.Kind == "ask" {
		if isOpen(d, e.ID) && !d.Dismissed(e.ID) {
			return false
		}
	} else if d.Unread(e.ID) {
		return false
	}
	since, ok := d.SettledAt(e)
	if !ok {
		since = desk.Time(e)
	}
	return d.Now.Sub(since) >= keep
}

// orphanAge is how long a stored file no event names is kept: the window in
// which a command that copied it is still writing its event.
const orphanAge = 24 * time.Hour

// mediaPlan sorts the stored files: those to keep, and those that may go,
// each with why.
func (a *App) mediaPlan(d *desk.Desk, days int) (keep map[string]bool, drop map[string]string) {
	keep, drop = map[string]bool{}, map[string]string{}
	keepFor := time.Duration(days) * 24 * time.Hour
	for _, e := range d.Events {
		for _, r := range media.Of(e) {
			f := r.File()
			if days <= 0 || !collectable(d, e, keepFor) {
				keep[f] = true
				delete(drop, f)
			} else if !keep[f] {
				drop[f] = fmt.Sprintf("%s %s, %s old", d.Display(e.ID), r.Name, desk.Age(d.Now.Sub(desk.Time(e))))
			}
		}
	}
	return keep, drop
}

func (a *App) cmdGC(args []string) error {
	o, err := parse(args, []string{"dry-run"}, []string{"days"}, 0, 0)
	if err != nil {
		return err
	}
	days := a.keepDays()
	if v, ok := o.vals["days"]; ok {
		if _, err := fmt.Sscanf(v, "%d", &days); err != nil || days < 1 {
			return usagef("--days takes a whole number of days, 1 or more")
		}
	}
	cfg, err := a.config()
	if err != nil {
		return err
	}
	var n int
	var freed int64
	if days > 0 {
		n, freed, err = a.gc(days, o.bools["dry-run"])
		if err != nil {
			return err
		}
	} else {
		fmt.Fprintln(a.Stdout, "media_keep_days is 0: attachments are kept for good")
	}
	count, bytes, err := a.gcVoice(cfg.Voice.Days(), o.bools["dry-run"])
	if err != nil {
		return err
	}
	n, freed = n+count, freed+bytes
	verb := "deleted"
	if o.bools["dry-run"] {
		verb = "would delete"
	}
	fmt.Fprintf(a.Stdout, "%s %d file(s), %s\n", verb, n, media.Size(freed))
	return nil
}

// gc deletes the stored files of items past days, and files no event names
// that are a day old. It prints each file it deletes.
func (a *App) gc(days int, dry bool) (int, int64, error) {
	d, err := a.load()
	if err != nil {
		return 0, 0, err
	}
	files, err := media.List(a.Home)
	if err != nil {
		return 0, 0, err
	}
	keep, drop := a.mediaPlan(d, days)
	n, freed := 0, int64(0)
	for _, f := range files {
		p := filepath.Join(media.Dir(a.Home), f)
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		why, ok := drop[f]
		if !ok && !keep[f] {
			if d.Now.Sub(st.ModTime()) < orphanAge {
				continue
			}
			why = "no item names it"
		}
		if !ok && keep[f] {
			continue
		}
		fmt.Fprintf(a.Stderr, "agent-desk: gc %s (%s)\n", f, why)
		if !dry {
			if err := os.Remove(p); err != nil {
				return n, freed, err
			}
		}
		n, freed = n+1, freed+st.Size()
	}
	return n, freed, nil
}

// MediaView is an attachment as the dashboard shows it. URL is relative to
// the dashboard; Gone says why a file is not here.
type MediaView struct {
	media.Ref
	Kind string `json:"kind"`
	URL  string `json:"url,omitempty"`
	Path string `json:"path,omitempty"`
	Gone string `json:"gone,omitempty"`
	Size string `json:"size_text"`
}

func (a *App) mediaViews(d *desk.Desk, e store.Event) []MediaView {
	var out []MediaView
	days := a.keepDays()
	for _, r := range media.Of(e) {
		v := MediaView{Ref: r, Kind: r.Kind(), Size: media.Size(r.Size)}
		switch {
		case r.Present(a.Home):
			v.URL, v.Path = "api/media/"+r.File(), r.Path(a.Home)
			if v.Kind == "file" {
				v.URL += "?name=" + url.QueryEscape(r.Name)
			}
		case days > 0 && collectable(d, e, time.Duration(days)*24*time.Hour):
			v.Gone = fmt.Sprintf("deleted %d days after the item was read or answered (media_keep_days)", days)
		case e.Host != a.Host:
			v.Gone = "not fetched from " + e.Host + " yet; it comes with the next merge"
		default:
			v.Gone = "missing from " + media.Dir(a.Home)
		}
		out = append(out, v)
	}
	return out
}

// maybeGC runs gc at most once a day, after a write, so every host collects
// its media whether or not it serves the dashboard.
func (a *App) maybeGC() {
	days := a.keepDays()
	if days <= 0 {
		return
	}
	stamp := filepath.Join(media.Dir(a.Home), ".gc")
	if st, err := os.Stat(stamp); err == nil && a.Now().Sub(st.ModTime()) < 24*time.Hour {
		return
	}
	if _, err := os.Stat(media.Dir(a.Home)); err != nil {
		return
	}
	os.WriteFile(stamp, nil, 0o600)
	now := a.Now()
	os.Chtimes(stamp, now, now)
	saved := a.Stderr
	a.Stderr = io.Discard
	defer func() { a.Stderr = saved }()
	if _, _, err := a.gc(days, false); err != nil {
		fmt.Fprintf(saved, "agent-desk: warning: media gc: %v\n", err)
	}
}

// apiMedia serves a stored attachment. The policy header keeps an SVG from
// running scripts when opened on its own.
func (s *server) apiMedia(w http.ResponseWriter, r *http.Request) {
	file := r.PathValue("file")
	if !media.ValidFile(file) {
		jsonError(w, http.StatusNotFound, "no such attachment")
		return
	}
	p := filepath.Join(media.Dir(s.a.Home), file)
	if _, err := os.Stat(p); err != nil {
		jsonError(w, http.StatusNotFound, "this attachment is not on this host: deleted by gc, or not fetched yet")
		return
	}
	h := w.Header()
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	t := media.TypeOfFile(file)
	h.Set("Content-Type", t)
	if media.KindOf(t) == "file" {
		// Never shown in the page: a download, under its own name.
		name := media.CleanName(r.URL.Query().Get("name"))
		if r.URL.Query().Get("name") == "" {
			name = file
		}
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, p)
}

// cmdMedia is the plumbing hosts use to exchange media over ssh:
// `media list` names the stored files, `media cat F` writes one,
// `media put F` stores one from stdin.
func (a *App) cmdMedia(args []string) error {
	o, err := parse(args, nil, nil, 1, 2)
	if err != nil {
		return err
	}
	if _, err := a.store(); err != nil {
		return err
	}
	switch {
	case o.pos[0] == "list" && len(o.pos) == 1:
		files, err := media.List(a.Home)
		if err != nil {
			return err
		}
		for _, f := range files {
			fmt.Fprintln(a.Stdout, f)
		}
		return nil
	case o.pos[0] == "cat" && len(o.pos) == 2:
		if !media.ValidFile(o.pos[1]) {
			return usagef("%q is not a media file name", o.pos[1])
		}
		f, err := os.Open(filepath.Join(media.Dir(a.Home), o.pos[1]))
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(a.Stdout, f)
		return err
	case o.pos[0] == "put" && len(o.pos) == 2:
		return media.Put(a.Home, o.pos[1], a.Stdin)
	}
	return usagef("media list | media cat <file> | media put <file>")
}

// wanted is every stored file that events name and gc would keep, sorted.
func (a *App) wanted() ([]string, error) {
	d, err := a.load()
	if err != nil {
		return nil, err
	}
	keep, _ := a.mediaPlan(d, a.keepDays())
	var out []string
	for f := range keep {
		out = append(out, f)
	}
	sort.Strings(out)
	return out, nil
}

// mergeMediaDir copies, from the desk directory dir, the files this desk
// wants and lacks. It is best effort: what fails is reported, not fatal.
func (a *App) mergeMediaDir(dir string) {
	want, err := a.wanted()
	if err != nil {
		return
	}
	for _, f := range want {
		if _, err := os.Stat(filepath.Join(media.Dir(a.Home), f)); err == nil {
			continue
		}
		src, err := os.Open(filepath.Join(media.Dir(dir), f))
		if err != nil {
			continue
		}
		if err := media.Put(a.Home, f, src); err != nil {
			fmt.Fprintf(a.Stderr, "agent-desk: media %s: %v\n", f, err)
		}
		src.Close()
	}
}

// mergeMediaSSH exchanges, with host, the files each side wants and the
// other has. It runs after the events are merged both ways, so both want the
// same files. It is best effort, and returns how many files it sent.
func (a *App) mergeMediaSSH(ctx context.Context, sshBin, host, remote string) (sent int) {
	want, err := a.wanted()
	if err != nil || len(want) == 0 {
		return 0
	}
	run := func(stdin io.Reader, stdout io.Writer, cmd string) error {
		c := exec.CommandContext(ctx, sshBin, "-o", "BatchMode=yes", host, cmd)
		var errb bytes.Buffer
		c.Stdin, c.Stdout, c.Stderr = stdin, stdout, &errb
		if err := c.Run(); err != nil {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(errb.String()))
		}
		return nil
	}
	var list bytes.Buffer
	if err := run(nil, &list, remote+" media list"); err != nil {
		// An older agent-desk there has no media command.
		fmt.Fprintf(a.Stderr, "agent-desk: media on %s: %v\n", host, err)
		return 0
	}
	theirs := map[string]bool{}
	for _, f := range strings.Fields(list.String()) {
		theirs[f] = true
	}
	for _, f := range want {
		local := filepath.Join(media.Dir(a.Home), f)
		_, err := os.Stat(local)
		have := err == nil
		switch {
		case have && !theirs[f]:
			src, err := os.Open(local)
			if err != nil {
				continue
			}
			err = run(src, io.Discard, remote+" media put "+f)
			src.Close()
			if err != nil {
				fmt.Fprintf(a.Stderr, "agent-desk: sending %s to %s: %v\n", f, host, err)
			} else {
				sent++
			}
		case !have && theirs[f]:
			pr, pw := io.Pipe()
			done := make(chan error, 1)
			go func() {
				err := media.Put(a.Home, f, pr)
				pr.CloseWithError(err) // unblocks the copy if Put stopped early
				done <- err
			}()
			err := run(nil, pw, remote+" media cat "+f)
			pw.CloseWithError(err)
			if perr := <-done; err == nil {
				err = perr
			}
			if err != nil {
				fmt.Fprintf(a.Stderr, "agent-desk: fetching %s from %s: %v\n", f, host, err)
			}
		}
	}
	return sent
}

// apiUpload stores a file the user attaches in the dashboard, before the
// answer or message that carries it is sent: POST /api/upload?name=N, the
// file as the body. It returns the attachment's Ref and how it shows.
func (s *server) apiUpload(w http.ResponseWriter, r *http.Request) {
	if err := s.sameOriginAny(r); err != nil {
		jsonError(w, http.StatusForbidden, err.Error())
		return
	}
	if _, err := s.a.store(); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := r.URL.Query()
	ref, err := media.AddReader(s.a.Home, q.Get("name"), q.Get("caption"), http.MaxBytesReader(w, r.Body, media.MaxAny+1))
	if err != nil {
		jsonError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	d, err := s.a.load()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	meta, _ := json.Marshal(map[string]any{"media": []media.Ref{ref}})
	views := s.a.mediaViews(d, store.Event{Meta: meta})
	writeJSON(w, map[string]any{"ref": ref, "view": views[0]})
}

// uploaded checks the attachments a dashboard request names: each must be
// a file stored here (uploaded first); its type and size are taken from the
// stored file, not from the request.
func (a *App) uploaded(refs []media.Ref) ([]media.Ref, error) {
	if len(refs) > media.MaxPerItem {
		return nil, usagef("at most %d attachments per message, not %d", media.MaxPerItem, len(refs))
	}
	out := make([]media.Ref, 0, len(refs))
	for _, r := range refs {
		if len(r.SHA256) != 64 || media.TypeOfFile(r.File()) != r.MIME || !media.ValidFile(r.File()) {
			return nil, usagef("%q is not an uploaded attachment", r.Name)
		}
		st, err := os.Stat(r.Path(a.Home))
		if err != nil {
			return nil, usagef("%s is not on this host: upload it again", r.Name)
		}
		r.Size, r.Name = st.Size(), media.CleanName(r.Name)
		if r.Caption = strings.TrimSpace(r.Caption); r.Caption == "" {
			r.Caption = r.Name
		}
		if len(r.Caption) > 500 {
			r.Caption = r.Caption[:500]
		}
		out = append(out, r)
	}
	return out, nil
}
