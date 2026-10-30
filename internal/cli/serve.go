package cli

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	"github.com/spf13/cobra"

	"github.com/nightah/encore/i18n"
	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/api"
	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/buildinfo"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/server"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/store"
)

func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the Encore server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(cmd)
			if err != nil {
				return err
			}

			log, level, err := newLogger(cmd.ErrOrStderr(), cfg.Log)
			if err != nil {
				return err
			}

			log.Info("starting Encore", "version", buildinfo.Get().Version)

			// Hold the data directory for as long as we run, so a second server or a restore can't use the database under us. Stateless mode works in a private temporary directory instead.
			if !cfg.Stateless {
				lock, err := lockDataDir(cfg.DataDir)
				if err != nil {
					return err
				}

				defer lock.Close()
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			state, err := prepareStateless(ctx, cfg, log)
			if err != nil {
				return err
			}

			defer state.cleanup()

			a, err := app.New(ctx, cfg, log)
			if err != nil {
				return err
			}

			defer func() {
				if err := a.Close(); err != nil {
					log.Warn("closing", "error", err)
				}
			}()

			if err := a.ApplyConfiguredRoomsAndDevices(ctx); err != nil {
				return err
			}

			saverDone := state.startSaver(ctx, a.DB(), log)

			// Runs before the deferred a.Close and state.cleanup above, so the saver never writes to a closed database or a removed directory, even when startup fails below.
			defer func() {
				cancel()
				<-saverDone
			}()

			v, err := newVoice(log)
			if err != nil {
				return err
			}

			admin, err := api.New(ctx, api.Options{App: a, Sources: v.src, Speech: v.speech, Actions: v.actions, Logger: log, LogLevel: level})
			if err != nil {
				return err
			}

			skill, err := newSkillHandler(ctx, v, skillIDFunc(cfg.Alexa.SkillID, admin.SkillID), admin.Simulating, a, log)
			if err != nil {
				return err
			}

			srv, err := server.New(cfg.Server, log, server.Deps{Alexa: skill, API: admin.Handler(), BasePath: admin.BasePath})
			if err != nil {
				return err
			}

			if err := runAll(ctx, cancel, srv.Run, func(ctx context.Context) { _ = a.Run(ctx) }, admin.Run); err != nil {
				log.Error("server stopped", slog.Any("error", err))

				return errSilent
			}

			log.Info("stopped")

			return nil
		},
	}

	cmd.Flags().String("server.address", "", "listen address (default \":8080\")")
	cmd.Flags().String("log.level", "", "log level: debug, info, warn or error (default \"info\")")
	cmd.Flags().String("log.format", "", "log format: auto, pretty (coloured), text or json (default \"auto\": pretty on a terminal, text otherwise)")

	return cmd
}

// runAll runs every task alongside serve and, once serve returns, cancels them and waits for each to stop before returning serve's error, so nothing they still use is closed under them.
func runAll(ctx context.Context, cancel context.CancelFunc, serve func(context.Context) error, tasks ...func(context.Context)) error {
	var wg sync.WaitGroup

	for _, task := range tasks {
		wg.Go(func() { task(ctx) })
	}

	err := serve(ctx)

	cancel()
	wg.Wait()

	return err
}

func newVoice(log *slog.Logger) (*voice, error) {
	src, err := skillmodel.Load(i18n.FS)
	if err != nil {
		return nil, err
	}

	speech, err := alexa.NewSpeech(src, log)
	if err != nil {
		return nil, err
	}

	return &voice{src: src, speech: speech, actions: actions.NewService(log.With("component", "actions"))}, nil
}

// skillIDFunc prefers alexa.skill_id from config, falling back to the ID the last deploy recorded.
func skillIDFunc(configured string, stored func() string) func() string {
	if configured != "" {
		return func() string { return configured }
	}

	return stored
}

func newSkillHandler(ctx context.Context, v *voice, skillID func() string, simulating func() bool, a *app.App, log *slog.Logger) (http.Handler, error) {
	verifier, err := alexa.NewVerifier(log.With("component", "alexaverify"))
	if err != nil {
		return nil, err
	}

	// The certificate cache is in memory, so warm it with the chain the last run used rather than make the first request after a restart download it.
	var certURL string
	if err := a.DB().Settings().Get(ctx, certURLSetting, &certURL); err != nil && !errors.Is(err, store.ErrNotFound) {
		log.Warn("couldn't read the stored Alexa certificate URL", "error", err)
	}

	go alexa.WarmVerifier(ctx, verifier.Verifier, certURL, log)

	remembered := alexa.RememberCertURL(verifier, certURL, func(ctx context.Context, url string) {
		if err := a.DB().Settings().Set(context.WithoutCancel(ctx), certURLSetting, url); err != nil {
			log.WarnContext(ctx, "couldn't store the Alexa certificate URL", "error", err)
		}
	})

	if skillID() == "" {
		log.Warn("alexa.skill_id is not set and no skill has been deployed; requests for any skill ID are accepted until it is")
	}

	h := &alexa.Handler{
		Service:       v.actions,
		Speech:        v.speech,
		Targets:       a,
		Progressive:   alexa.NewProgressive(nil),
		Recorder:      a.Recorder(),
		IntentContent: v.src.Model.IntentContent(),
		Simulating:    simulating,
		Log:           log.With("component", "alexa"),
	}

	return alexa.VerifiedFunc(remembered, skillID, log, a.Rejections(), h.ServeBody), nil
}
