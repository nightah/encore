package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"slices"
	"strings"

	"github.com/nightah/encore/internal/basepath"
)

// newSPAHandler serves the UI in fsys. The prefix each request arrived under becomes the document's <base href>, so Vite's relative asset URLs and the router resolve correctly from deep links like /kodi/lounge.
func newSPAHandler(fsys fs.FS, log *slog.Logger) (*spaHandler, error) {
	if fsys == nil {
		return &spaHandler{index: []byte(placeholderIndex)}, nil
	}

	index, err := fs.ReadFile(fsys, "index.html")

	switch {
	case errors.Is(err, fs.ErrNotExist):
		log.Warn("web interface assets not found; serving a placeholder page (run `make build` to include them)")

		return &spaHandler{index: []byte(placeholderIndex)}, nil
	case err != nil:
		return nil, fmt.Errorf("reading embedded index.html: %w", err)
	}

	return &spaHandler{files: http.FileServerFS(fsys), fsys: fsys, index: index, baseHref: true}, nil
}

// withBaseHref inserts <base href> as the first element of <head>.
func withBaseHref(index []byte, href string) []byte {
	tag := []byte(`<base href="` + html.EscapeString(href) + `">`)

	i := headEnd(index)
	if i < 0 {
		return index
	}

	return slices.Concat(index[:i], tag, index[i:])
}

// headEnd is the offset just past the <head> start tag, whatever its case and attributes, or -1 if there is none. Tags that only start with "head", like <header>, are passed over.
func headEnd(index []byte) int {
	// Lower only ASCII: bytes.ToLower can change a non-ASCII character's length, and the offsets must hold in index.
	lower := bytes.Clone(index)
	for i, c := range lower {
		if 'A' <= c && c <= 'Z' {
			lower[i] = c + 'a' - 'A'
		}
	}

	for off := 0; ; {
		i := bytes.Index(lower[off:], []byte("<head"))
		if i < 0 {
			return -1
		}

		off += i + len("<head")
		if off < len(lower) && strings.IndexByte(tagNameEnd, lower[off]) >= 0 {
			return tagEnd(lower, off)
		}
	}
}

// tagEnd is the offset just past the > that closes the tag whose attributes start at off, skipping any > inside quoted values, or -1 if the tag never closes.
func tagEnd(b []byte, off int) int {
	var quote byte

	for i := off; i < len(b); i++ {
		switch c := b[i]; {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return i + 1
		}
	}

	return -1
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")

	if h.files != nil && name != "" && name != "index.html" && h.exists(name) {
		if strings.HasPrefix(name, hashedAssetsDir+"/") {
			// Vite puts content hashes in these names, so they never change.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}

		h.files.ServeHTTP(w, r)

		return
	}

	if path.Ext(name) != "" {
		// A missing asset should not be answered with the app shell.
		http.NotFound(w, r)

		return
	}

	h.serveIndex(w, r)
}

func (h *spaHandler) exists(name string) bool {
	info, err := fs.Stat(h.fsys, name)

	return err == nil && !info.IsDir()
}

func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	nonce := newNonce()

	index := h.index
	if h.baseHref {
		index = withBaseHref(index, basepath.From(r.Context()))
	}

	body := bytes.ReplaceAll(index, []byte(nonceMarker), []byte(nonce))

	hdr := w.Header()
	hdr.Set("Content-Type", "text/html; charset=utf-8")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("Content-Security-Policy", contentSecurityPolicy(nonce))

	if r.Method == http.MethodHead {
		return
	}

	_, _ = w.Write(body) //nolint:gosec // embedded index.html plus a server-generated nonce; no request data
}

func contentSecurityPolicy(nonce string) string {
	return strings.Join([]string{
		"default-src 'self'",
		"script-src 'self' 'nonce-" + nonce + "'",
		"style-src 'self' 'nonce-" + nonce + "'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

func newNonce() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error.

	return base64.RawStdEncoding.EncodeToString(b)
}
