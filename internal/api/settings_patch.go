package api

import (
	"context"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/nightah/encore/internal/config"
)

func (c *changes) str(key string, v *string) {
	if v != nil {
		*c = append(*c, Change{Key: key, Value: strings.TrimSpace(*v)})
	}
}

func (c *changes) num(key string, v *int) {
	if v != nil {
		*c = append(*c, Change{Key: key, Value: *v})
	}
}

func (c *changes) flag(key string, v *bool) {
	if v != nil {
		*c = append(*c, Change{Key: key, Value: *v})
	}
}

func (c *changes) list(key string, v *[]string) {
	if v != nil {
		out := []string{}

		for _, s := range *v {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}

		*c = append(*c, Change{Key: key, Value: out})
	}
}

// basePath tidies a typed base path: a leading slash and no trailing one, so "encore/" saves as "/encore".
func basePath(v string) string {
	v = strings.Trim(strings.TrimSpace(v), "/")

	return "/" + v
}

func (p SettingsPatch) changes() []Change {
	var c changes

	if s := p.Server; s != nil && s.BasePath != nil {
		c = append(c, Change{Key: "server.base_path", Value: basePath(*s.BasePath)})
	}

	if a := p.Auth; a != nil {
		c.str("auth.mode", a.Mode)

		if l := a.Local; l != nil {
			c.num("auth.session_hours", l.SessionHours)
			c.num("auth.lockout_attempts", l.LockoutAttempts)
			c.num("auth.lockout_minutes", l.LockoutMinutes)
		}

		if o := a.OIDC; o != nil {
			c.str("auth.oidc.issuer", o.Issuer)
			c.str("auth.oidc.client_id", o.ClientID)
			c.str("auth.oidc.client_secret", o.ClientSecret)
			c.list("auth.oidc.scopes", o.Scopes)
			c.str("auth.oidc.admin_group", o.AdminGroup)
			c.str("auth.oidc.groups_claim", o.GroupsClaim)
			c.str("auth.oidc.redirect_uri", o.RedirectURI)
			c.flag("auth.break_glass", o.BreakGlass)
		}

		if h := a.Header; h != nil {
			c.list("server.trusted_proxies", h.TrustedProxies)
			c.str("auth.header.user_header", h.UserHeader)
			c.str("auth.header.groups_header", h.GroupsHeader)
			c.str("auth.header.admin_group", h.AdminGroup)
		}
	}

	if f := p.Features; f != nil {
		c.flag("alexa.deep_search", f.DeepSearch)
		c.flag("alexa.shutdown_quits", f.ShutdownQuits)
		c.flag("alexa.confirm_sound", f.ConfirmSound)
		c.num("alexa.queue_limit", f.QueueLimit)
	}

	if l := p.Logging; l != nil {
		c.str("log.level", l.Level)
	}

	if s := p.Storage; s != nil {
		c.str("storage.backend", s.Backend)

		if v := s.S3; v != nil {
			c.str("storage.s3.endpoint", v.Endpoint)
			c.str("storage.s3.region", v.Region)
			c.str("storage.s3.bucket", v.Bucket)
			c.str("storage.s3.prefix", v.Prefix)
			c.str("storage.s3.addressing", v.Addressing)
			c.str("storage.s3.access_key", v.AccessKey)
			c.str("storage.s3.secret_key", v.SecretKey)
			c.str("storage.s3.ca_bundle", v.CABundle)
			c.flag("storage.s3.insecure_skip_verify", v.InsecureSkipVerify)
			c.str("storage.s3.sse", v.SSE)
		}

		if v := s.WebDAV; v != nil {
			c.str("storage.webdav.url", v.URL)
			c.str("storage.webdav.username", v.Username)
			c.str("storage.webdav.password", v.Password)
			c.str("storage.webdav.prefix", v.Prefix)
		}

		if v := s.Local; v != nil {
			c.str("storage.local.path", v.Path)
		}

		if v := s.Backups; v != nil {
			c.str("storage.backups.schedule", v.Schedule)
			c.num("storage.backups.keep", v.Keep)
			c.str("storage.backups.prefix", v.Prefix)
		}
	}

	return c
}

func backupTarget(s *config.Storage) string {
	where := s.Local.Path

	switch s.Backend {
	case config.BackendS3:
		where = strings.Join([]string{s.S3.Endpoint, s.S3.Region, s.S3.Bucket, s.S3.Prefix}, "|")
	case config.BackendWebDAV:
		where = s.WebDAV.URL + "|" + s.WebDAV.Prefix
	}

	return s.Backend + "|" + where + "|" + s.Backups.Prefix
}

func (a *API) patchSettings(ctx context.Context, p SettingsPatch) error {
	before, err := a.prefs.AuthSettings(ctx)
	if err != nil {
		return err
	}

	// Check the parts saved outside the config overrides first, so a bad value there changes nothing.
	if err := a.validateLocale(ctx, p); err != nil {
		return err
	}

	if ch := p.changes(); len(ch) > 0 {
		if err := a.guardAuthChange(ctx, ch); err != nil {
			return err
		}

		was, err := a.prefs.Effective(ctx)
		if err != nil {
			return err
		}

		target := backupTarget(&was.Storage)

		now, err := a.prefs.Apply(ctx, ch)
		if err != nil {
			return err
		}

		a.setLogLevel(now)

		// A new place for backups gets a first one straight away, rather than at the next scheduled time; a failure shows in the log as for any backup.
		if backupTarget(&now.Storage) != target {
			a.background(func(ctx context.Context) {
				if err := a.backup(ctx); err != nil {
					a.log.Error("backup to the new storage failed", "error", err)
				}
			})
		}
	}

	if err := a.patchLocale(ctx, p); err != nil {
		return err
	}

	after, err := a.prefs.AuthSettings(ctx)
	if err != nil {
		return err
	}

	if after.Mode != before.Mode {
		a.log.Info("admin sign-in mode changed; signing out other sessions", "from", before.Mode, "to", after.Mode)

		return a.auth.EndOtherSessions(ctx, info(ctx).r)
	}

	return nil
}

// guardAuthChange refuses a change that would lock the admin making it out: switching to header mode from a connection the proxy settings would not trust.
func (a *API) guardAuthChange(ctx context.Context, ch []Change) error {
	eff, err := a.prefs.Effective(ctx)
	if err != nil {
		return err
	}

	mode, proxies := eff.Auth.Mode, eff.Server.TrustedProxies

	for _, c := range ch {
		switch c.Key {
		case "auth.mode":
			mode, _ = c.Value.(string)
		case "server.trusted_proxies":
			proxies, _ = c.Value.([]string)
		}
	}

	if mode != config.AuthModeHeader {
		return nil
	}

	r := info(ctx).r
	if r == nil {
		return nil
	}

	peer := r.RemoteAddr
	if i := strings.LastIndexByte(peer, ':'); i >= 0 {
		peer = peer[:i]
	}

	addr, err := netip.ParseAddr(strings.Trim(peer, "[]"))
	if err != nil {
		return nil //nolint:nilerr // an unparsable peer (e.g. a test transport) cannot be checked
	}

	if !slices.ContainsFunc(proxies, func(cidr string) bool {
		pfx, err := netip.ParsePrefix(cidr)

		return err == nil && pfx.Contains(addr.Unmap())
	}) {
		return huma.Error409Conflict("switching to header mode from a connection that does not come through a trusted proxy would lock you out; add your proxy to trusted_proxies and connect through it first")
	}

	return nil
}

func (a *API) patchLocale(ctx context.Context, p SettingsPatch) error {
	if l := p.Locale; l != nil {
		cur, err := a.localeSettings(ctx)
		if err != nil {
			return err
		}

		if l.Timezone != nil {
			cur.Timezone = strings.TrimSpace(*l.Timezone)
		}

		if l.DefaultLocale != nil {
			cur.DefaultLocale = strings.TrimSpace(*l.DefaultLocale)
		}

		if err := a.app.DB().Settings().Set(ctx, localeSetting, cur); err != nil {
			return err
		}
	}

	return nil
}

func (a *API) validateLocale(ctx context.Context, p SettingsPatch) error {
	l := p.Locale
	if l == nil {
		return nil
	}

	if l.Timezone != nil {
		tz := strings.TrimSpace(*l.Timezone)
		if _, err := time.LoadLocation(tz); err != nil || tz == "" {
			return fieldError("body.locale.timezone", "is not an IANA time zone such as Australia/Melbourne", tz)
		}
	}

	if l.DefaultLocale != nil {
		loc := strings.TrimSpace(*l.DefaultLocale)
		if !slices.Contains(a.opts.Sources.LocaleNames(), loc) {
			return fieldError("body.locale.default_locale", "is not a locale Encore ships", loc)
		}

		enabled, err := a.skill.locales(ctx)
		if err != nil {
			return err
		}

		if !slices.Contains(enabled, loc) {
			return fieldError("body.locale.default_locale", "is not one of the skill's enabled locales", loc)
		}
	}

	return nil
}

func (a *API) setLogLevel(eff *config.Config) {
	if a.opts.LogLevel == nil {
		return
	}

	level, err := config.ParseLevel(eff.Log.Level)
	if err != nil || level == a.opts.LogLevel.Level() {
		return
	}

	// Logged at info under whichever of the two levels shows it, so the change is recorded unless both hide info.
	if a.opts.LogLevel.Level() <= slog.LevelInfo {
		a.log.Info("log level changed", "level", eff.Log.Level)
		a.opts.LogLevel.Set(level)

		return
	}

	a.opts.LogLevel.Set(level)
	a.log.Info("log level changed", "level", eff.Log.Level)
}
