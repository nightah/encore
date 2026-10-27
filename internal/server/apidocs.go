package server

import (
	"embed"
	"io/fs"
	"net/http"
)

// apiDocsAssets is the Swagger UI build from swagger-ui-dist 5.17.14 (Apache-2.0, see apidocs/LICENSE), embedded so the page works offline and under a strict CSP.
//
//go:embed apidocs/swagger-ui-bundle.js apidocs/swagger-ui.css apidocs/init.js
var apiDocsAssets embed.FS

// mountAPIDocs registers the docs page at /api/ (exactly) and its assets under /api/docs/.
func mountAPIDocs(mux *http.ServeMux) {
	assets, err := fs.Sub(apiDocsAssets, "apidocs")
	if err != nil {
		panic(err) // the embedded directory is fixed at build time
	}

	files := http.StripPrefix(PathAPIDocs+"docs/", http.FileServerFS(assets))

	mux.HandleFunc("GET "+PathAPIDocs+"{$}", func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Security-Policy", apiDocsCSP)
		h.Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(apiDocsPage))
	})

	mux.HandleFunc("GET "+PathAPIDocs+"docs/{file}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", apiDocsCSP)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		files.ServeHTTP(w, r)
	})
}
