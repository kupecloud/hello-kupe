// Package static serves the built frontend from a directory.
package static

import (
	"io"
	"io/fs"
	"net/http"
	"strings"
)

// contentSecurityPolicy lets the page load only its own files and call only
// its own origin. The API is reached on the same host under /api, so the
// browser never needs another origin.
const contentSecurityPolicy = "default-src 'self'; connect-src 'self'; img-src 'self' data:; " +
	"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// Handler serves files from root with security headers, plus /healthz.
func Handler(root fs.FS) http.Handler {
	files := http.FileServerFS(root)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		// No directory listings: only "/" maps to a directory (index.html).
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// Vite puts a content hash in every file name under /assets/, so
		// those never change. index.html must be revalidated to pick up a
		// new build.
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			h.Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
	return mux
}
