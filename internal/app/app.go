package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/store"
)

// New opens the database, secrets and blob store, applies Kodi instances and library groups declared in config, and prepares a runtime for every instance and library group. Nothing connects to Kodi until Run.
func New(ctx context.Context, cfg *config.Config, log *slog.Logger) (*App, error) {
	return newApp(ctx, cfg, log, defaultTuning())
}

func newApp(ctx context.Context, cfg *config.Config, log *slog.Logger, tune tuning) (*App, error) {
	db, err := store.Open(ctx, store.Options{Path: store.DefaultPath(cfg.DataDir), Logger: log.With("component", "store")})
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	sec, err := secrets.Open(ctx, db.SecretRows(), secrets.Options{
		Key:     cfg.Secrets.Key,
		KeyFile: cfg.Secrets.KeyFile,
		DataDir: cfg.DataDir,
		Logger:  log.With("component", "secrets"),
	})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("opening secrets: %w", err), db.Close())
	}

	a := &App{
		cfg: cfg, log: log, db: db, secrets: sec, tune: tune,
		instances: map[int64]*Instance{}, groups: map[int64]*group{}, prints: map[int64]*kodi.LibraryFingerprint{},
	}
	a.recorder = newRecorder(db.RequestLog(), log.With("component", "requestlog"))
	a.rejections = alexa.NewRejections(nil)

	// Snapshots only speed up start-up, so a store that can't be opened isn't fatal.
	if a.blobs, err = blobstore.Open(cfg.Storage, cfg.DataDir); err != nil {
		log.Warn("library snapshots disabled: opening the blob store failed", "error", err)
	}

	if err := a.applyConfiguredInstances(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("applying kodi instances: %w", err), a.Close())
	}

	if err := a.applyConfiguredGroups(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("applying library groups: %w", err), a.Close())
	}

	if err := a.loadInstances(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("loading kodi instances: %w", err), a.Close())
	}

	a.planMu.Lock()
	err = a.plan(ctx)
	a.planMu.Unlock()

	if err != nil {
		return nil, errors.Join(err, a.Close())
	}

	return a, nil
}

// DB exposes the database to other components, such as the admin API.
func (a *App) DB() *store.DB { return a.db }

// Run keeps every instance's connection and every library group's catalog and index current until ctx is done.
func (a *App) Run(ctx context.Context) error {
	var wg sync.WaitGroup

	wg.Go(func() { a.recorder.run(ctx) })

	a.planMu.Lock()
	a.runCtx = ctx

	for _, g := range a.groups {
		a.startGroup(g)
	}

	for _, inst := range a.instances {
		a.startInstance(inst)
	}
	a.planMu.Unlock()

	<-ctx.Done()

	a.planMu.Lock()
	a.runCtx = nil

	a.mu.RLock()
	running := make([]*Instance, 0, len(a.instances))
	for _, inst := range a.instances {
		running = append(running, inst)
	}
	a.mu.RUnlock()
	a.planMu.Unlock()

	// Instances are waited for outside planMu, which their detection may be waiting for; their contexts are already done.
	for _, inst := range running {
		inst.stop()
	}

	a.planMu.Lock()
	for _, g := range a.groups {
		g.stop()
	}
	a.planMu.Unlock()

	wg.Wait()

	return nil
}

// Close releases the database, secrets and title indexes. Call it after Run returns.
func (a *App) Close() error {
	a.mu.Lock()

	errs := make([]error, 0, len(a.groups)+2)
	for _, g := range a.groups {
		errs = append(errs, g.close())
	}
	a.mu.Unlock()

	errs = append(errs, a.secrets.Close(), a.db.Close())

	return errors.Join(errs...)
}
