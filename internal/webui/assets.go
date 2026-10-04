// Package webui serves the embedded local web UI and its read-only JSON API.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// dist holds the Vite build output produced by `make ui`. Only .gitkeep is
// committed, so a binary built without the frontend serves a placeholder page.
//
//go:embed all:dist
var dist embed.FS

const placeholderPage = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Istok</title></head>
<body style="font-family: system-ui, sans-serif; padding: 2rem">
<h1>Istok web UI is not built</h1>
<p>This binary was built without the frontend. Run <code>make install</code> to build it.</p>
</body>
</html>
`

func assetsHandler() http.Handler {
	assets, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}

	if _, err := fs.Stat(assets, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(placeholderPage))
		})
	}

	files := http.FileServerFS(assets)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

		// Client-side routes have no file behind them, so they fall back to the SPA entry point.
		if name == "" || name == "." {
			name = "index.html"
		}
		if _, err := fs.Stat(assets, name); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}

		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		files.ServeHTTP(w, r)
	})
}
