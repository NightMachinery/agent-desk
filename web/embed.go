// Package web holds the dashboard's built files, embedded in the binary.
// The sources are in app/ (a Svelte project); `npm run build` there writes
// dist/, which is committed so that `go install` needs no Node.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist is the built dashboard: index.html and its hashed assets.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
