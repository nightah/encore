package api

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	goyaml "go.yaml.in/yaml/v3"

	"github.com/nightah/encore/internal/auth"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/store"
)

func (a *API) registerConfigIO(api huma.API) {
	huma.Register(api, protected(huma.Operation{
		OperationID: "exportConfig", Method: http.MethodGet, Path: Prefix + "/config/export", Tags: []string{"settings"}, Summary: "Export the configuration as encore.yml", Description: "Auth, storage, `alexa.locales`, trusted proxies and Kodi instances in `encore.yml` form. Secrets are left out unless `include_secrets=true`, which needs a sign-in within the last 15 minutes (403 otherwise); trusted-header requests always qualify.",
		Responses: map[string]*huma.Response{"200": {Description: "encore.yml", Content: map[string]*huma.MediaType{"application/yaml": {Schema: &huma.Schema{Type: huma.TypeString}}}}},
	}),
		func(ctx context.Context, in *struct {
			IncludeSecrets bool `query:"include_secrets"`
		},
		) (*struct {
			ContentType        string `header:"Content-Type"`
			ContentDisposition string `header:"Content-Disposition"`
			Body               []byte
		}, error,
		) {
			if in.IncludeSecrets {
				p := info(ctx).principal
				if p == nil || (p.Method != auth.ModeHeader && a.now().Sub(p.SessionCreated) > freshAuth) {
					return nil, huma.Error403Forbidden("sign in again to export secrets; exports with secrets need a sign-in within the last 15 minutes")
				}
			}

			b, err := a.exportConfig(ctx, in.IncludeSecrets)
			if err != nil {
				return nil, a.problemFor(ctx, err)
			}

			return &struct {
				ContentType        string `header:"Content-Type"`
				ContentDisposition string `header:"Content-Disposition"`
				Body               []byte
			}{ContentType: "application/yaml", ContentDisposition: `attachment; filename="encore.yml"`, Body: b}, nil
		})

	huma.Register(api, protected(huma.Operation{
		OperationID: "importConfig", Method: http.MethodPost, Path: Prefix + "/config/import", Tags: []string{"settings"}, Summary: "Import an encore.yml", Description: "Applies the settings and Kodi instances it contains; Kodi instances are added or updated by name. Keys managed by config, and keys the web interface can't change, are skipped. Invalid values answer 422 and nothing is saved.",
		MaxBodyBytes: maxImportBytes,
	}),
		func(ctx context.Context, in *struct {
			RawBody []byte `contentType:"application/yaml"`
		},
		) (*body[ImportResult], error) {
			res, err := a.importConfig(ctx, in.RawBody)
			if err != nil {
				return nil, a.settingsProblem(ctx, err)
			}

			return &body[ImportResult]{Body: res}, nil
		})
}

// Only keys the web interface manages.
func flatValues(c *config.Config, withSecrets bool) map[string]any {
	m := map[string]any{
		"server.trusted_proxies":              nonNil(c.Server.TrustedProxies),
		"alexa.locales":                       nonNil(c.Alexa.Locales),
		"alexa.content":                       nonNil(c.Alexa.Content),
		"auth.mode":                           c.Auth.Mode,
		"auth.session_hours":                  c.Auth.SessionHours,
		"auth.lockout_attempts":               c.Auth.LockoutAttempts,
		"auth.lockout_minutes":                c.Auth.LockoutMinutes,
		"auth.oidc.issuer":                    c.Auth.OIDC.Issuer,
		"auth.oidc.client_id":                 c.Auth.OIDC.ClientID,
		"auth.oidc.scopes":                    nonNil(c.Auth.OIDC.Scopes),
		"auth.oidc.admin_group":               c.Auth.OIDC.AdminGroup,
		"auth.oidc.groups_claim":              c.Auth.OIDC.GroupsClaim,
		"auth.oidc.redirect_uri":              c.Auth.OIDC.RedirectURI,
		"auth.header.user_header":             c.Auth.Header.UserHeader,
		"auth.header.groups_header":           c.Auth.Header.GroupsHeader,
		"auth.header.admin_group":             c.Auth.Header.AdminGroup,
		"storage.backend":                     c.Storage.Backend,
		"storage.local.path":                  c.Storage.Local.Path,
		"storage.s3.endpoint":                 c.Storage.S3.Endpoint,
		"storage.s3.region":                   c.Storage.S3.Region,
		"storage.s3.bucket":                   c.Storage.S3.Bucket,
		"storage.s3.prefix":                   c.Storage.S3.Prefix,
		"storage.s3.addressing":               c.Storage.S3.Addressing,
		"storage.s3.access_key":               c.Storage.S3.AccessKey,
		"storage.s3.ca_bundle":                c.Storage.S3.CABundle,
		"storage.s3.insecure_skip_verify":     c.Storage.S3.InsecureSkipVerify,
		"storage.s3.sse":                      c.Storage.S3.SSE,
		"storage.s3.sse_kms_key_id":           c.Storage.S3.SSEKMSKeyID,
		"storage.webdav.url":                  c.Storage.WebDAV.URL,
		"storage.webdav.username":             c.Storage.WebDAV.Username,
		"storage.webdav.prefix":               c.Storage.WebDAV.Prefix,
		"storage.webdav.ca_bundle":            c.Storage.WebDAV.CABundle,
		"storage.webdav.insecure_skip_verify": c.Storage.WebDAV.InsecureSkipVerify,
		"storage.backups.schedule":            c.Storage.Backups.Schedule,
		"storage.backups.keep":                c.Storage.Backups.Keep,
		"storage.backups.prefix":              c.Storage.Backups.Prefix,
	}

	if withSecrets {
		m["auth.oidc.client_secret"] = c.Auth.OIDC.ClientSecret
		m["storage.s3.secret_key"] = c.Storage.S3.SecretKey
		m["storage.webdav.password"] = c.Storage.WebDAV.Password
	}

	return m
}

func (a *API) exportConfig(ctx context.Context, withSecrets bool) ([]byte, error) {
	eff, err := a.prefs.Effective(ctx)
	if err != nil {
		return nil, err
	}

	// A confmap provider never fails to load.
	k := koanf.New(".")
	_ = k.Load(confmap.Provider(flatValues(eff, withSecrets), "."), nil)

	doc := k.Raw()

	recs, err := a.app.DB().KodiInstances().List(ctx)
	if err != nil {
		return nil, err
	}

	kodis := make([]exportedKodi, 0, len(recs))

	for _, r := range recs {
		e := exportedKodi{
			Name: r.Name, URL: httpURL(r), WebSocketURL: wsURL(r), Username: r.Username, MAC: r.MAC, Broadcast: r.BroadcastAddr, Default: r.IsDefaultForUnlinked,
			Language: r.Language, Timezone: r.Timezone, ShutdownQuits: r.ShutdownQuits,
		}

		if withSecrets && r.PasswordSecretRef != "" {
			if pw, err := a.app.Secrets().Get(ctx, r.PasswordSecretRef); err == nil {
				e.Password = string(pw)
				clear(pw)
			}
		}

		kodis = append(kodis, e)
	}

	doc["kodi"] = kodis

	var buf bytes.Buffer

	note := "Secrets are not included."
	if withSecrets {
		note = "This file contains secrets; keep it safe."
	}

	fmt.Fprintf(&buf, "# Encore configuration exported %s. %s\n", a.now().UTC().Format(time.RFC3339), note)

	enc := goyaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(doc); err != nil {
		return nil, err
	}

	return buf.Bytes(), enc.Close()
}

func (a *API) importConfig(ctx context.Context, raw []byte) (ImportResult, error) {
	res := ImportResult{Applied: []string{}, Skipped: []string{}}

	parsed, err := yaml.Parser().Unmarshal(raw)
	if err != nil {
		return res, fieldError("body", "is not valid YAML: "+err.Error(), nil)
	}

	// A confmap provider never fails to load.
	k := koanf.New(".")
	_ = k.Load(confmap.Provider(parsed, ""), nil)

	var ch []Change

	for _, key := range k.Keys() {
		if key == "kodi" || strings.HasPrefix(key, "kodi.") {
			continue
		}

		switch {
		case !isOverridable(key):
			res.Skipped = append(res.Skipped, key+": cannot be changed in the web interface")
		case a.prefs.Managed(key):
			res.Skipped = append(res.Skipped, key+": managed by config")
		default:
			ch = append(ch, Change{Key: key, Value: k.Get(key)})
			res.Applied = append(res.Applied, key)
		}
	}

	var kodis []exportedKodi

	if k.Exists("kodi") {
		if err := k.UnmarshalWithConf("kodi", &kodis, koanf.UnmarshalConf{Tag: "yaml"}); err != nil {
			return res, fieldError("body.kodi", "must be a list of Kodi instances: "+err.Error(), nil)
		}
	}

	if len(ch) > 0 {
		if _, err := a.prefs.Apply(ctx, ch); err != nil {
			return res, err
		}
	}

	for _, kd := range kodis {
		applied, err := a.importKodi(ctx, kd)
		if err != nil {
			res.Skipped = append(res.Skipped, "kodi "+kd.Name+": "+err.Error())

			continue
		}

		res.Applied = append(res.Applied, applied)
	}

	slices.Sort(res.Applied)
	slices.Sort(res.Skipped)

	return res, nil
}

func isOverridable(key string) bool {
	_, ok := overridable[key]

	return ok
}

func (a *API) importKodi(ctx context.Context, kd exportedKodi) (string, error) {
	if kd.Name == "" {
		return "", errors.New("has no name")
	}

	if a.managedInstance(kd.Name) {
		return "", errors.New("managed by config")
	}

	lang, shutdown := cmp.Or(kd.Language, "en"), shutdownOf(kd.ShutdownQuits)
	in := InstanceInput{
		Name: &kd.Name, URL: &kd.URL, WebSocketURL: &kd.WebSocketURL, Username: &kd.Username, MAC: &kd.MAC, Broadcast: &kd.Broadcast,
		Language: &lang, Timezone: &kd.Timezone, Shutdown: &shutdown,
	}
	if kd.Password != "" {
		in.Password = &kd.Password
	}

	recs, err := a.app.DB().KodiInstances().List(ctx)
	if err != nil {
		return "", err
	}

	var (
		id     int64
		action string
	)

	if i := slices.IndexFunc(recs, func(r store.KodiInstance) bool { return strings.EqualFold(r.Name, kd.Name) }); i >= 0 {
		inst, err := a.updateInstance(ctx, recs[i].ID, in)
		if err != nil {
			return "", importErr(err)
		}

		id, action = inst.ID, "updated"
	} else {
		inst, err := a.createInstance(ctx, in)
		if err != nil {
			return "", importErr(err)
		}

		id, action = inst.ID, "added"
	}

	if kd.Default {
		if err := a.app.DB().KodiInstances().SetDefault(ctx, id); err != nil {
			return "", err
		}
	}

	return "kodi " + kd.Name + " (" + action + ")", nil
}

// importErr flattens a problem into one line for the skipped list.
func importErr(err error) error {
	var m *huma.ErrorModel
	if !errors.As(err, &m) {
		return err
	}

	detail := m.Detail
	for _, e := range m.Errors {
		detail += "; " + e.Location + " " + e.Message
	}

	return errors.New(detail)
}
