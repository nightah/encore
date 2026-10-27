package api

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/auth"
	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/store"
)

// localeSettings keeps the default locale among the enabled ones: the first of them unless one was chosen, so a skill deployed for en-AU alone defaults to en-AU.
func (a *API) localeSettings(ctx context.Context) (LocaleSettings, error) {
	l, err := store.Setting(ctx, a.app.DB().Settings(), localeSetting, LocaleSettings{Timezone: time.Local.String()})
	if err != nil {
		return l, err
	}

	enabled, err := a.skill.locales(ctx)
	if err != nil {
		return l, err
	}

	if len(enabled) > 0 && !slices.Contains(enabled, l.DefaultLocale) {
		l.DefaultLocale = enabled[0]
	}

	return l, nil
}

func (a *API) registerSettings(api huma.API) {
	huma.Register(api, protected(huma.Operation{OperationID: "getSettings", Method: http.MethodGet, Path: Prefix + "/settings", Tags: []string{"settings"}, Summary: "Get settings"}),
		func(ctx context.Context, _ *struct{}) (*body[Settings], error) {
			s, err := a.settings(ctx)

			return &body[Settings]{Body: s}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "updateSettings", Method: http.MethodPatch, Path: Prefix + "/settings", Tags: []string{"settings"}, Summary: "Change settings", Description: "A partial settings document. Secrets are write-only (secret_key, password, client_secret); an empty string removes one. Keys managed by config are refused with 409. Changing auth.mode signs out every other session."}),
		func(ctx context.Context, in *struct{ Body SettingsPatch }) (*body[Settings], error) {
			if err := a.patchSettings(ctx, in.Body); err != nil {
				return nil, a.settingsProblem(ctx, err)
			}

			s, err := a.settings(ctx)

			return &body[Settings]{Body: s}, a.problemFor(ctx, err)
		})

	huma.Register(api, protected(huma.Operation{OperationID: "testStorage", Method: http.MethodPost, Path: Prefix + "/settings/storage/test", Tags: []string{"settings"}, Summary: "Write, read back and delete a probe object in the storage backend"}),
		func(ctx context.Context, _ *struct{}) (*body[StorageTest], error) {
			return &body[StorageTest]{Body: a.testStorage(ctx)}, nil
		})

	huma.Register(api, protected(huma.Operation{OperationID: "rotateSecretsKey", Method: http.MethodPost, Path: Prefix + "/settings/secrets/rotate", Tags: []string{"settings"}, Summary: "Re-encrypt every secret under a new generated key", Description: "Only when Encore generated the key itself; with secrets.key or a key file the operator rotates it (409).", DefaultStatus: http.StatusNoContent}),
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			return nil, a.problemFor(ctx, a.rotateSecrets(ctx))
		})

	huma.Register(api, protected(huma.Operation{OperationID: "runBackup", Method: http.MethodPost, Path: Prefix + "/settings/backups/run", Tags: []string{"settings"}, Summary: "Back up the database to the storage backend now", DefaultStatus: http.StatusAccepted}),
		func(context.Context, *struct{}) (*body[Accepted], error) {
			id := jobID("backup")
			a.background(func(ctx context.Context) {
				if err := a.backup(ctx); err != nil {
					a.log.Error("backup failed", "error", err)
				}
			})

			return &body[Accepted]{Body: Accepted{ID: id}}, nil
		})
}

func (a *API) settings(ctx context.Context) (Settings, error) {
	eff, err := a.prefs.Effective(ctx)
	if err != nil {
		return Settings{}, err
	}

	admin, err := a.auth.Admin(ctx)
	if err != nil {
		return Settings{}, err
	}

	loc, err := a.localeSettings(ctx)
	if err != nil {
		return Settings{}, err
	}

	mode := eff.Auth.Mode
	if mode == "" {
		mode = config.AuthModeLocal
	}

	redirectURI := eff.Auth.OIDC.RedirectURI
	if redirectURI == "" {
		redirectURI = a.auth.ExternalBase(info(ctx).r) + auth.OIDCCallbackPath
	}

	s := Settings{
		Server: ServerSettings{BasePath: eff.Server.BasePath},
		Auth: AuthSettings{
			Mode:  mode,
			Local: LocalAuth{SessionHours: eff.Auth.SessionHours, LockoutAttempts: eff.Auth.LockoutAttempts, LockoutMinutes: eff.Auth.LockoutMinutes},
			OIDC: OIDCAuth{
				Issuer: eff.Auth.OIDC.Issuer, ClientID: eff.Auth.OIDC.ClientID, HasClientSecret: eff.Auth.OIDC.ClientSecret != "",
				Scopes: nonNil(eff.Auth.OIDC.Scopes), AdminGroup: eff.Auth.OIDC.AdminGroup, GroupsClaim: eff.Auth.OIDC.GroupsClaim,
				RedirectURI: redirectURI, BreakGlass: eff.Auth.BreakGlass,
			},
			Header: HeaderAuth{
				TrustedProxies: nonNil(eff.Server.TrustedProxies), UserHeader: eff.Auth.Header.UserHeader,
				GroupsHeader: eff.Auth.Header.GroupsHeader, AdminGroup: eff.Auth.Header.AdminGroup,
			},
		},
		Storage: StorageSettings{
			Backend: eff.Storage.Backend,
			S3: S3Settings{
				Endpoint: eff.Storage.S3.Endpoint, Region: eff.Storage.S3.Region, Bucket: eff.Storage.S3.Bucket, Prefix: eff.Storage.S3.Prefix,
				Addressing: eff.Storage.S3.Addressing, AccessKey: eff.Storage.S3.AccessKey, HasSecretKey: eff.Storage.S3.SecretKey != "",
				CABundle: eff.Storage.S3.CABundle, InsecureSkipVerify: eff.Storage.S3.InsecureSkipVerify, SSE: eff.Storage.S3.SSE,
			},
			WebDAV: WebDAVSettings{URL: eff.Storage.WebDAV.URL, Username: eff.Storage.WebDAV.Username, HasPassword: eff.Storage.WebDAV.Password != "", Prefix: eff.Storage.WebDAV.Prefix},
			Local:  LocalStorage{Path: localPath(eff)},
			Backups: BackupSettings{
				Schedule: eff.Storage.Backups.Schedule, Keep: eff.Storage.Backups.Keep, Prefix: eff.Storage.Backups.Prefix,
				LastBackupAt: a.timeSetting(ctx, lastBackupSetting),
			},
		},
		Secrets: SecretsInfo{
			KeySource: keySource(eff),
			KeyID:     a.app.Secrets().KeyID(),
			RotatedAt: a.timeSetting(ctx, rotatedAtSetting),
		},
		Features: Features{DeepSearch: eff.Alexa.DeepSearch, ShutdownQuits: eff.Alexa.ShutdownQuits, ConfirmSound: eff.Alexa.ConfirmSound, QueueLimit: eff.Alexa.QueueLimit},
		Logging:  LoggingSettings{Level: eff.Log.Level},
		Locale:   loc,
		Managed:  a.prefs.ManagedList(),
	}

	if admin != nil {
		s.Auth.Local.Username, s.Auth.Local.TOTPEnabled = admin.Username, admin.TOTP
	}

	return s, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

func localPath(cfg *config.Config) string {
	if cfg.Storage.Local.Path != "" {
		return cfg.Storage.Local.Path
	}

	return filepath.Join(cfg.DataDir, blobstore.LocalDirName)
}

func keySource(cfg *config.Config) string {
	switch {
	case cfg.Secrets.Key != "":
		return "config"
	case cfg.Secrets.KeyFile != "":
		return "file"
	default:
		return "generated"
	}
}

func (a *API) timeSetting(ctx context.Context, key string) *time.Time {
	t, err := store.Setting(ctx, a.app.DB().Settings(), key, time.Time{})
	if err != nil {
		return nil
	}

	return timePtr(t)
}

func (a *API) settingsProblem(ctx context.Context, err error) error {
	var (
		managed *managedKeyError
		invalid *invalidSettingsError
	)

	switch {
	case errors.As(err, &managed):
		return managedError(managed.key)
	case errors.As(err, &invalid):
		return huma.Error422UnprocessableEntity(invalid.Error())
	case errors.Is(err, errNotOverridable):
		return huma.Error422UnprocessableEntity(err.Error())
	}

	return a.problemFor(ctx, err)
}

func (a *API) openStorage(ctx context.Context) (blobstore.Store, *config.Config, error) {
	eff, err := a.prefs.Effective(ctx)
	if err != nil {
		return nil, nil, err
	}

	bs, err := blobstore.Open(eff.Storage, eff.DataDir)
	if err != nil {
		return nil, nil, err
	}

	return bs, eff, nil
}

func (a *API) testStorage(ctx context.Context) StorageTest {
	start := time.Now()

	bs, _, err := a.openStorage(ctx)
	if err == nil {
		pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = bs.Probe(pctx)

		cancel()
	}

	t := StorageTest{OK: err == nil, MS: time.Since(start).Milliseconds()}
	if err != nil {
		t.Error = err.Error()
	}

	return t
}

func (a *API) backup(ctx context.Context) error {
	bs, eff, err := a.openStorage(ctx)
	if err != nil {
		return err
	}

	key, err := blobstore.Backup(ctx, a.app.DB(), bs, eff.Storage.Backups.Prefix, eff.Storage.Backups.Keep)
	if err != nil {
		return err
	}

	a.log.Info("database backed up", "key", key)

	return a.app.DB().Settings().Set(ctx, lastBackupSetting, a.now().UTC())
}

// runBackups runs scheduled backups until ctx is done. It wakes at least hourly to pick up a changed schedule, keeping the due time between wake-ups so intervals longer than an hour come due.
func (a *API) runBackups(ctx context.Context) {
	var (
		spec string
		due  time.Time
	)

	for {
		eff, err := a.prefs.Effective(ctx)
		if err != nil {
			return
		}

		sched, err := config.ParseSchedule(eff.Storage.Backups.Schedule)
		if err != nil {
			return
		}

		if eff.Storage.Backups.Schedule != spec || due.IsZero() {
			spec = eff.Storage.Backups.Schedule

			var last time.Time
			if t := a.timeSetting(ctx, lastBackupSetting); t != nil {
				last = *t
			}

			due = sched.Due(last, a.now())
		}

		wait := time.Hour
		if sched.Enabled() {
			wait = min(max(due.Sub(a.now()), time.Second), time.Hour)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		if !sched.Enabled() || a.now().Before(due) {
			continue
		}

		if err := a.backup(ctx); err != nil && ctx.Err() == nil {
			a.log.Error("scheduled backup failed", "error", err)
		}

		due = sched.Due(a.now(), a.now())
	}
}

func (a *API) rotateSecrets(ctx context.Context) error {
	if src := keySource(a.app.Config()); src != "generated" {
		return huma.Error409Conflict("the secrets key comes from " + map[string]string{"config": "secrets.key (or ENCORE_SECRETS_KEY_FILE)", "file": "secrets.key_file"}[src] + "; generate a new key, run Encore with it, and rotate there")
	}

	key, err := secrets.GenerateKey()
	if err != nil {
		return err
	}

	if err := a.app.Secrets().Rotate(ctx, key); err != nil {
		return err
	}

	return a.app.DB().Settings().Set(ctx, rotatedAtSetting, a.now().UTC())
}
