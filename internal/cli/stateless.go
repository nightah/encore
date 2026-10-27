package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/statestore"
	"github.com/nightah/encore/internal/store"
)

// prepareStateless restores the saved database into a temporary data directory when cfg.Stateless is set, pointing cfg.DataDir at it. Without stateless mode it does nothing.
func prepareStateless(ctx context.Context, cfg *config.Config, log *slog.Logger) (*statelessRun, error) {
	if !cfg.Stateless {
		return &statelessRun{}, nil
	}

	blobs, err := blobstore.Open(cfg.Storage, cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("opening storage for stateless mode: %w", err)
	}

	dir, err := os.MkdirTemp("", "encore-")
	if err != nil {
		return nil, err
	}

	restored, err := statestore.Restore(ctx, blobs, store.DefaultPath(dir))
	if err != nil {
		_ = os.RemoveAll(dir)

		return nil, err
	}

	if restored {
		log.Info("stateless mode: restored the database from storage", "backend", cfg.Storage.Backend)
	} else {
		log.Info("stateless mode: no saved database yet; starting fresh", "backend", cfg.Storage.Backend)
	}

	cfg.DataDir = dir

	return &statelessRun{blobs: blobs, dir: dir}, nil
}

// startSaver saves the database to storage as it changes and once more on shutdown. The returned channel closes when the final save is done.
func (s *statelessRun) startSaver(ctx context.Context, db statestore.Database, log *slog.Logger) <-chan struct{} {
	done := make(chan struct{})

	if s.blobs == nil {
		close(done)

		return done
	}

	host, _ := os.Hostname()
	saver := &statestore.Saver{DB: db, Store: s.blobs, Log: log.With("component", "statestore"), Owner: host}

	go func() {
		defer close(done)

		saver.Run(ctx)
	}()

	return done
}

func (s *statelessRun) cleanup() {
	if s.dir != "" {
		_ = os.RemoveAll(s.dir)
	}
}
