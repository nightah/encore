package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/nightah/encore/internal/buildinfo"
	"github.com/nightah/encore/internal/config"
)

// New builds the server's routes.
func New(cfg config.Server, log *slog.Logger, deps Deps) (*Server, error) {
	basePath := deps.BasePath
	if basePath == nil {
		basePath = func() string { return cfg.BasePath }
	}

	spa, err := newSPAHandler(uiFS(), log)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	mux.Handle("POST "+PathAlexa, orUnavailable(deps.Alexa, "the Alexa skill is not configured yet"))
	mux.HandleFunc(PathAlexa, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", http.MethodPost)
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	})
	mux.HandleFunc("GET "+PathHealthz, handleHealthz)

	if deps.API != nil {
		mux.Handle(PathAPI, deps.API)
	}

	mountAPIDocs(mux)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", spa)

	return &Server{cfg: cfg, log: log, handler: chain(mux, withBasePath(basePath), securityHeaders), basePath: basePath}, nil
}

// Handler returns the root handler, for tests.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// Run serves until ctx is cancelled, then shuts down gracefully.
func (s *Server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.cfg.Address, err)
	}

	return s.Serve(ctx, ln)
}

// Serve serves on ln until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}

	errc := make(chan error, 1)

	go func() {
		s.log.Info("listening", "address", ln.Addr().String(), "base_path", s.basePath())
		errc <- srv.Serve(ln)
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}

	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	info := buildinfo.Get()

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": info.Version})
}

func orUnavailable(h http.Handler, reason string) http.Handler {
	if h != nil {
		return h
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSONError(w, http.StatusServiceUnavailable, reason)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
