//go:generate go test -run TestOpenAPIDrift -update .

package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/nightah/encore/internal/auth"
)

// New builds the API, its settings store and the auth service.
func New(ctx context.Context, opts Options) (*API, error) {
	if opts.App == nil {
		return nil, errors.New("api: App is required")
	}

	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}

	if opts.Now == nil {
		opts.Now = time.Now
	}

	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 20 * time.Second}
	}

	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 15 * time.Second
	}

	if opts.StatusInterval <= 0 {
		opts.StatusInterval = 10 * time.Second
	}

	if opts.after == nil {
		opts.after = time.After
	}

	if opts.Sources == nil {
		src, err := loadSources()
		if err != nil {
			return nil, err
		}

		opts.Sources = src
	}

	cfg := opts.App.Config()
	db := opts.App.DB()

	a := &API{
		app:    opts.App,
		log:    opts.Logger.With("component", "api"),
		now:    opts.Now,
		opts:   opts,
		events: newHub(),
		runCtx: context.WithoutCancel(ctx),
	}

	a.prefs = NewPrefs(cfg, db, opts.App.Secrets())
	opts.App.SetSettings(a.prefs.Effective)

	if eff, err := a.prefs.Effective(ctx); err == nil {
		a.setLogLevel(eff)
	}

	authSvc, err := auth.New(ctx, auth.Options{
		DB:         db,
		Secrets:    opts.App.Secrets(),
		Settings:   a.prefs.AuthSettings,
		Logger:     opts.Logger.With("component", "auth"),
		Now:        opts.Now,
		HTTPClient: opts.HTTPClient,
	})
	if err != nil {
		return nil, err
	}

	a.auth = authSvc
	a.libraries = opts.Libraries

	if a.libraries == nil {
		a.libraries = &appLibraries{api: a}
	}

	if a.opts.SlotValues == nil {
		a.opts.SlotValues = opts.App.SlotValues
	}

	a.status = newStatusCache(a)
	opts.App.OnPlayerChange(a.status.playerChanged)
	a.skill = newSkillService(a)

	mux := http.NewServeMux()
	a.humaAPI = humago.New(mux, humaConfig())
	a.humaAPI.UseMiddleware(a.middleware)
	a.register(a.humaAPI)

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(w, http.StatusNotFound, "not found")
	})

	a.handler = http.NewCrossOriginProtection().Handler(mux)

	return a, nil
}

// Handler serves every /api/v1/ route except the Alexa endpoint and health check.
func (a *API) Handler() http.Handler { return a.handler }

// BasePath returns the base path Encore is served under, from config or as changed in the web interface.
func (a *API) BasePath() string { return a.prefs.BasePath() }

// Auth returns the auth service.
func (a *API) Auth() *auth.Service { return a.auth }

// SkillID returns the skill ID recorded by the last deploy, for request verification when alexa.skill_id is not configured.
func (a *API) SkillID() string { return a.skill.storedSkillID() }

// Simulating reports whether a deploy's smoke test is running or has just finished, so the Alexa endpoint can mark the simulator's requests.
func (a *API) Simulating() bool { return a.skill.simulating() }

// OpenAPI returns the generated OpenAPI document.
func (a *API) OpenAPI() *huma.OpenAPI { return a.humaAPI.OpenAPI() }

// Run feeds the event stream (request log, Kodi status, attention list) and runs scheduled backups until ctx is done, then waits for background work.
func (a *API) Run(ctx context.Context) {
	a.runMu.Lock()
	a.runCtx = ctx
	a.runMu.Unlock()

	var wg sync.WaitGroup

	wg.Go(func() { a.watchRequests(ctx) })
	wg.Go(func() { a.status.run(ctx) })
	wg.Go(func() { a.runBackups(ctx) })

	wg.Wait()
	a.bg.Wait()
}

// background runs fn under the API's lifetime context rather than the request's.
func (a *API) background(fn func(ctx context.Context)) {
	a.runMu.Lock()
	ctx := a.runCtx
	a.runMu.Unlock()

	a.bg.Go(func() { fn(ctx) })
}

func humaConfig() huma.Config {
	cfg := huma.DefaultConfig("Encore admin API", "1")
	cfg.Info.Description = "The contract between Encore's web interface and its server. See https://nightah.github.io/encore/reference/api/."
	cfg.OpenAPIPath = Prefix + "/openapi"
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	// No $schema links in responses: the web client is generated from the document instead.
	cfg.CreateHooks = nil
	cfg.Transformers = []huma.Transformer{nonNilTransformer}
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"session": {Type: "apiKey", In: "cookie", Name: auth.SessionCookie, Description: "Admin session; trusted-header requests from server.trusted_proxies are accepted too."},
	}

	return cfg
}
