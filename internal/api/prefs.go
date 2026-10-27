package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"

	"github.com/nightah/encore/internal/auth"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/store"
)

// overridable lists the config keys the web interface may change, and whether each is a secret.
var overridable = map[string]bool{
	"server.base_path":       false,
	"server.trusted_proxies": false,
	"alexa.locales":          false,
	"alexa.content":          false,
	"alexa.deep_search":      false,
	"alexa.shutdown_quits":   false,
	"alexa.confirm_sound":    false,
	"alexa.queue_limit":      false,
	"log.level":              false,

	"auth.mode":                 false,
	"auth.break_glass":          false,
	"auth.session_hours":        false,
	"auth.lockout_attempts":     false,
	"auth.lockout_minutes":      false,
	"auth.oidc.issuer":          false,
	"auth.oidc.client_id":       false,
	"auth.oidc.client_secret":   true,
	"auth.oidc.scopes":          false,
	"auth.oidc.admin_group":     false,
	"auth.oidc.groups_claim":    false,
	"auth.oidc.redirect_uri":    false,
	"auth.header.user_header":   false,
	"auth.header.groups_header": false,
	"auth.header.admin_group":   false,

	"storage.backend":                     false,
	"storage.local.path":                  false,
	"storage.s3.endpoint":                 false,
	"storage.s3.region":                   false,
	"storage.s3.bucket":                   false,
	"storage.s3.prefix":                   false,
	"storage.s3.addressing":               false,
	"storage.s3.access_key":               false,
	"storage.s3.secret_key":               true,
	"storage.s3.ca_bundle":                false,
	"storage.s3.insecure_skip_verify":     false,
	"storage.s3.sse":                      false,
	"storage.s3.sse_kms_key_id":           false,
	"storage.webdav.url":                  false,
	"storage.webdav.username":             false,
	"storage.webdav.password":             true,
	"storage.webdav.prefix":               false,
	"storage.webdav.ca_bundle":            false,
	"storage.webdav.insecure_skip_verify": false,
	"storage.backups.schedule":            false,
	"storage.backups.keep":                false,
	"storage.backups.prefix":              false,
}

// settingPaths maps config keys to where the settings document (or, for alexa.locales, the skill document) shows them, where the two differ.
var settingPaths = map[string]string{
	"server.trusted_proxies": "auth.header.trusted_proxies",
	"auth.session_hours":     "auth.local.session_hours",
	"auth.lockout_attempts":  "auth.local.lockout_attempts",
	"auth.lockout_minutes":   "auth.local.lockout_minutes",
	"auth.break_glass":       "auth.oidc.break_glass",
	"alexa.locales":          "skill.locales_enabled",
	"alexa.content":          "skill.content",
	"alexa.deep_search":      "features.deep_search",
	"alexa.shutdown_quits":   "features.shutdown_quits",
	"alexa.confirm_sound":    "features.confirm_sound",
	"alexa.queue_limit":      "features.queue_limit",
	"server.base_path":       "server.base_path",
	"log.level":              "logging.level",
	"secrets.key":            "secrets.key_source",
	"secrets.key_file":       "secrets.key_source",
}

// settingPrefixes are the config sections the settings document shows under the same paths.
var settingPrefixes = []string{"auth.", "storage."}

// NewPrefs returns the settings store.
func NewPrefs(cfg *config.Config, db *store.DB, sec *secrets.Store) *Prefs {
	return &Prefs{cfg: cfg, db: db, sec: sec}
}

// Config returns the configuration the process was started with.
func (p *Prefs) Config() *config.Config { return p.cfg }

// Managed reports whether key is set by config.
func (p *Prefs) Managed(key string) bool { return p.cfg.Managed(key) }

// ManagedList is the settings document's managed list: the dotted paths, as the settings document names them, of values set by config. Config keys with no place in the document (server.address, log.format, kodi, ...) are left out.
func (p *Prefs) ManagedList() []string {
	out := []string{}

	for _, k := range p.cfg.ManagedKeys() {
		if path, ok := settingPaths[k]; ok {
			out = append(out, path)

			continue
		}

		for _, prefix := range settingPrefixes {
			if strings.HasPrefix(k, prefix) {
				out = append(out, k)
			}
		}
	}

	slices.Sort(out)

	return slices.Compact(out)
}

// BasePath returns the effective server.base_path, falling back to the startup value if the saved settings can't be read. It runs on every request; after the first it only takes a lock.
func (p *Prefs) BasePath() string {
	eff, err := p.Effective(context.Background())
	if err != nil {
		return p.cfg.Server.BasePath
	}

	return eff.Server.BasePath
}

// Effective returns the merged configuration. The result is shared; do not modify it.
func (p *Prefs) Effective(ctx context.Context) (*config.Config, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.effective != nil {
		return p.effective, nil
	}

	overrides, err := p.overrides(ctx)
	if err != nil {
		return nil, err
	}

	eff, err := p.merge(ctx, overrides)
	if err != nil {
		return nil, err
	}

	p.effective = eff

	return eff, nil
}

func (p *Prefs) overrides(ctx context.Context) (map[string]any, error) {
	return store.Setting(ctx, p.db.Settings(), overridesSetting, map[string]any{})
}

// merge applies overrides and stored secrets to a copy of the startup configuration, skipping keys config manages.
func (p *Prefs) merge(ctx context.Context, overrides map[string]any) (*config.Config, error) {
	flat := map[string]any{}

	for k, v := range overrides {
		if _, ok := overridable[k]; ok && !p.cfg.Managed(k) {
			flat[k] = v
		}
	}

	for k, secret := range overridable {
		if !secret || p.cfg.Managed(k) {
			continue
		}

		v, err := p.sec.Get(ctx, secretRef(k))

		switch {
		case errors.Is(err, secrets.ErrNotFound):
		case err != nil:
			return nil, fmt.Errorf("reading %s: %w", k, err)
		default:
			flat[k] = string(v)
			clear(v)
		}
	}

	return applyFlat(p.cfg, flat)
}

func applyFlat(base *config.Config, flat map[string]any) (*config.Config, error) {
	eff := *base

	// mapstructure merges into existing slices element by element; start overridden lists empty so a shorter list replaces a longer one.
	for k := range flat {
		switch k {
		case "server.trusted_proxies":
			eff.Server.TrustedProxies = nil
		case "alexa.locales":
			eff.Alexa.Locales = nil
		case "alexa.content":
			eff.Alexa.Content = nil
		case "auth.oidc.scopes":
			eff.Auth.OIDC.Scopes = nil
		}
	}

	k := koanf.New(".")
	if err := k.Load(confmap.Provider(flat, "."), nil); err != nil {
		return nil, err
	}

	if err := k.UnmarshalWithConf("", &eff, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return nil, fmt.Errorf("applying saved settings: %w", err)
	}

	return &eff, nil
}

// Apply validates and saves changes atomically: either every change is saved or none is. It returns ErrManaged for a key set by config.
func (p *Prefs) Apply(ctx context.Context, changes []Change) (*config.Config, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	overrides, err := p.overrides(ctx)
	if err != nil {
		return nil, err
	}

	secretsToSet := map[string]any{}

	for _, c := range changes {
		secret, ok := overridable[c.Key]
		if !ok {
			return nil, fmt.Errorf("%w: %s", errNotOverridable, c.Key)
		}

		if p.cfg.Managed(c.Key) {
			return nil, &managedKeyError{key: c.Key}
		}

		switch {
		case secret:
			secretsToSet[c.Key] = c.Value
		case c.Value == nil:
			delete(overrides, c.Key)
		default:
			overrides[c.Key] = c.Value
		}
	}

	// Validate the result before saving anything.
	candidate, err := p.merge(ctx, overrides)
	if err != nil {
		return nil, err
	}

	if len(secretsToSet) > 0 {
		flat := map[string]any{}
		for k, v := range secretsToSet {
			if v != nil {
				flat[k] = v
			} else {
				flat[k] = ""
			}
		}

		if candidate, err = applyFlat(candidate, flat); err != nil {
			return nil, err
		}
	}

	if err := candidate.Validate(); err != nil {
		return nil, &invalidSettingsError{err: err}
	}

	for k, v := range secretsToSet {
		s, _ := v.(string)
		if s == "" {
			err = p.sec.Delete(ctx, secretRef(k))
		} else {
			err = p.sec.Put(ctx, secretRef(k), []byte(s))
		}

		if err != nil {
			return nil, err
		}
	}

	if err := p.db.Settings().Set(ctx, overridesSetting, overrides); err != nil {
		return nil, err
	}

	p.effective = candidate

	return candidate, nil
}

// Overrides returns the saved overrides (without secrets), for export.
func (p *Prefs) Overrides(ctx context.Context) (map[string]any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	o, err := p.overrides(ctx)
	if err != nil {
		return nil, err
	}

	return maps.Clone(o), nil
}

// HasSecret reports whether a secret config key has a value, from config or saved.
func (p *Prefs) HasSecret(ctx context.Context, key string) bool {
	eff, err := p.Effective(ctx)
	if err != nil {
		return false
	}

	switch key {
	case "auth.oidc.client_secret":
		return eff.Auth.OIDC.ClientSecret != ""
	case "storage.s3.secret_key":
		return eff.Storage.S3.SecretKey != ""
	case "storage.webdav.password":
		return eff.Storage.WebDAV.Password != ""
	}

	return false
}

// AuthSettings is the auth service's view of the effective configuration.
func (p *Prefs) AuthSettings(ctx context.Context) (auth.Settings, error) {
	eff, err := p.Effective(ctx)
	if err != nil {
		return auth.Settings{}, err
	}

	a := eff.Auth

	mode := auth.Mode(a.Mode)
	if mode == "" {
		mode = auth.ModeLocal
	}

	s := auth.Settings{
		Mode:            mode,
		SessionTTL:      time.Duration(a.SessionHours) * time.Hour,
		LockoutAttempts: a.LockoutAttempts,
		LockoutWindow:   time.Duration(a.LockoutMinutes) * time.Minute,
		BreakGlass:      a.BreakGlass,
		OIDC: auth.OIDCSettings{
			Issuer: a.OIDC.Issuer, ClientID: a.OIDC.ClientID, ClientSecret: a.OIDC.ClientSecret, Scopes: a.OIDC.Scopes,
			AdminGroup: a.OIDC.AdminGroup, GroupsClaim: a.OIDC.GroupsClaim, RedirectURI: a.OIDC.RedirectURI,
		},
		Header: auth.HeaderSettings{UserHeader: a.Header.UserHeader, GroupsHeader: a.Header.GroupsHeader, AdminGroup: a.Header.AdminGroup},
	}

	for _, cidr := range eff.Server.TrustedProxies {
		if pfx, err := netip.ParsePrefix(strings.TrimSpace(cidr)); err == nil {
			s.TrustedProxies = append(s.TrustedProxies, pfx.Masked())
		}
	}

	return s, nil
}

func secretRef(key string) string { return "config/" + key }

func (e *managedKeyError) Error() string { return e.key + " is managed by config" }

func (e *invalidSettingsError) Error() string { return e.err.Error() }

func (e *invalidSettingsError) Unwrap() error { return e.err }
