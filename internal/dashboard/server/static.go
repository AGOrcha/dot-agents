package server

import (
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// embeddedDist carries the built web/dashboard SPA. A placeholder index.html
// is committed under dist/ so this package always compiles even before the
// Vite build has run (the release build overwrites dist/ with the real
// compiled assets). Guarding the embed this way keeps `go build` and
// `go test` green on a clean checkout.
//
//go:embed all:dist
var embeddedDist embed.FS

// distFS returns the file system the static handler serves from: the caller's
// --static-dir override when set (an on-disk build directory), otherwise the
// go:embed'd dist/ subtree.
func distFS(staticDir string) (fs.FS, error) {
	if staticDir != "" {
		if _, err := os.Stat(staticDir); err != nil {
			return nil, err
		}
		return os.DirFS(staticDir), nil
	}
	return fs.Sub(embeddedDist, "dist")
}

// spaHandler serves static assets from fsys with a single-page-app fallback:
// a request whose path maps to an existing file (or directory) is served
// verbatim; anything else falls back to index.html so client-side routes
// resolve. This is the non-/api arm of the standalone server's routing.
func spaHandler(fsys fs.FS) http.Handler {
	fileServer := http.FileServerFS(fsys)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(fsys, name); errors.Is(err, fs.ErrNotExist) {
			// Unknown route → hand the SPA its shell and let the router decide.
			http.ServeFileFS(w, r, fsys, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
