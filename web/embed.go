// Package web embeds the built React single-page app so the binary serves the
// UI with no external assets. The dist directory is produced by `npm run build`
// (see web/README.md); a placeholder index.html is committed so the Go build
// succeeds before the frontend has been built.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets returns the SPA build output rooted at dist/, ready to serve.
func Assets() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
