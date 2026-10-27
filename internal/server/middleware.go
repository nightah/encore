package server

import (
	"net/http"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/basepath"
)

// chain wraps h in middleware, the first listed outermost.
func chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for _, m := range slices.Backward(middleware) {
		h = m(h)
	}

	return h
}

// withBasePath serves requests both under the base path, which can change between requests, and at /. The prefix a request used is recorded with basepath.With, so a wrong base path never locks anyone out and proxies may forward with or without it.
func withBasePath(basePath func() string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			base := strings.TrimSuffix(basePath(), "/")

			if base == "" || (r.URL.Path != base && !strings.HasPrefix(r.URL.Path, base+"/")) {
				next.ServeHTTP(w, r.WithContext(basepath.With(r.Context(), "/")))

				return
			}

			r = r.WithContext(basepath.With(r.Context(), base))

			if r.URL.Path == base {
				// http.StripPrefix would leave an empty path; the trailing slash makes relative URLs resolve under the prefix.
				http.Redirect(w, r, base+"/", http.StatusMovedPermanently)

				return
			}

			http.StripPrefix(base, next).ServeHTTP(w, r)
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
