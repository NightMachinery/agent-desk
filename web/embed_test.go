package web

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// sourceHash hashes what the build reads, as app/scripts/stamp.mjs does.
func sourceHash(root string) (string, error) {
	files := []string{"index.html", "package.json", "package-lock.json", "svelte.config.js", "vite.config.js"}
	for _, dir := range []string{"src", "public"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			return "", err
		}
		h.Write([]byte(f + "\x00"))
		h.Write(b)
		h.Write([]byte("\x00"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// TestDistIsCurrent fails when the dashboard's sources changed without
// `npm run build` in app/, which would ship a stale page.
func TestDistIsCurrent(t *testing.T) {
	if _, err := os.Stat("app/package.json"); err != nil {
		t.Skip("no app/ sources here (a module download)")
	}
	want, err := sourceHash("app")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("dist/.source-hash")
	if err != nil {
		t.Fatalf("dist/ was not built: run `npm run build` in web/app (%v)", err)
	}
	if strings.TrimSpace(string(got)) != want {
		t.Fatal("web/dist is stale: run `npm ci && npm run build` in web/app and commit web/dist")
	}
	if _, err := fs.ReadFile(Dist(), "index.html"); err != nil {
		t.Fatal(err)
	}
}
