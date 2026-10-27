package server

import (
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/nightah/encore/internal/config"
)

// Server is the HTTP listener and its routes.
type Server struct {
	cfg      config.Server
	log      *slog.Logger
	handler  http.Handler
	basePath func() string
}

// Deps are the handlers the server mounts. A nil Alexa handler makes its route answer 503 so the process can start before everything is configured; a nil API leaves /api/v1/ answering 404.
type Deps struct {
	Alexa http.Handler
	// API serves the rest of /api/v1/; it does its own authentication.
	API http.Handler
	// BasePath returns the current base path, which the web interface can change while Encore runs; nil means config.Server.BasePath. Requests without it are served too.
	BasePath func() string
}

type spaHandler struct {
	files http.Handler
	fsys  fs.FS
	index []byte
	// baseHref is false for the placeholder page.
	baseHref bool
}
