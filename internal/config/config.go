package config

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
)

var localePattern = regexp.MustCompile(`^[a-z]{2}-[A-Z]{2}$`)

// basePathPattern allows "/" or slash-separated segments of URL-safe characters, with an optional trailing slash.
var basePathPattern = regexp.MustCompile(`^/([A-Za-z0-9_~-][A-Za-z0-9._~-]*(/[A-Za-z0-9_~-][A-Za-z0-9._~-]*)*/?)?$`)

// ReservedBasePaths are the top-level paths Encore serves itself, which server.base_path can't start with: the API, the web interface's pages and its assets folder.
var ReservedBasePaths = []string{"alexa-skill", "api", "brand", "echo-devices", "kodi", "library", "login", "request-log", "settings", "setup", "static"}

// Defaults returns the built-in configuration.
func Defaults() Config {
	return Config{
		// Loopback is trusted by default for a proxy on the same host (and the Vite dev server); being a default, not config, it can be changed in the web interface.
		Server:  Server{Address: ":8080", BasePath: "/", TrustedProxies: []string{"127.0.0.1/32", "::1/128"}},
		Log:     Log{Level: "info", Format: "auto"},
		DataDir: DefaultDataDir(),
		Alexa:   Alexa{QueueLimit: DefaultQueueLimit, DeepSearch: true, ConfirmSound: true, Content: []string{ContentMovies, ContentTV, ContentMusic}},
		Storage: Storage{
			Backend: BackendLocal,
			S3:      StorageS3{Addressing: AddressingAuto},
			Backups: Backups{Schedule: "daily@03:00", Keep: 14, Prefix: "Encore"},
		},
		Auth: defaultAuth(),
	}
}

// Load builds the configuration from all sources and validates it.
func Load(opts Options) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(defaultsMap(), "."), nil); err != nil {
		return nil, fmt.Errorf("loading defaults: %w", err)
	}

	// explicit collects what the operator set, so the web interface can show those keys as managed by config.
	explicit := koanf.New(".")

	if opts.File != "" {
		if err := explicit.Load(file.Provider(opts.File), yaml.Parser()); err != nil {
			return nil, fmt.Errorf("loading config file %q: %w", opts.File, err)
		}
	}

	environ := opts.Environ
	if environ == nil {
		environ = os.Environ
	}

	var fileErr error

	envProvider := env.Provider(".", env.Opt{
		Prefix:      EnvPrefix,
		EnvironFunc: environ,
		TransformFunc: func(name, value string) (string, any) {
			name = strings.ToLower(strings.TrimPrefix(name, EnvPrefix))

			// The _FILE form of a setting comes first, so ENCORE_SECRETS_KEY_FILE reads secrets.key from the file.
			if base, isFile := strings.CutSuffix(name, "_file"); isFile && envKeys[base] != "" {
				b, err := os.ReadFile(value) //nolint:gosec // the operator chooses which secret file to read
				if err != nil {
					fileErr = errors.Join(fileErr, fmt.Errorf("reading %s%s_FILE: %w", EnvPrefix, strings.ToUpper(base), err))
					return "", nil
				}

				name, value = base, strings.TrimRight(string(b), "\r\n")
			}

			key, ok := envKeys[name]
			if !ok {
				// Not a setting: other tools use ENCORE_ variables too.
				return "", nil
			}

			if listKeys[key] {
				return key, splitList(value)
			}

			return key, value
		},
	})

	if err := explicit.Load(envProvider, nil); err != nil {
		return nil, fmt.Errorf("loading environment: %w", err)
	}

	if fileErr != nil {
		return nil, fileErr
	}

	if opts.Flags != nil {
		// Without a Koanf to compare against, only flags the user set are read.
		if err := explicit.Load(posflag.Provider(opts.Flags, ".", nil), nil); err != nil {
			return nil, fmt.Errorf("loading flags: %w", err)
		}
	}

	if err := k.Merge(explicit); err != nil {
		return nil, fmt.Errorf("merging configuration: %w", err)
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("decoding configuration: %w", err)
	}

	cfg.managed = map[string]bool{}
	for _, key := range explicit.Keys() {
		cfg.managed[key] = true
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate reports every invalid setting at once.
func (c *Config) Validate() error {
	var errs []error

	if c.Server.Address == "" {
		errs = append(errs, errors.New("server.address must not be empty"))
	}

	errs = append(errs, validateBasePath(c.Server.BasePath)...)

	for _, p := range c.Server.TrustedProxies {
		if _, err := netip.ParsePrefix(p); err != nil {
			errs = append(errs, fmt.Errorf("server.trusted_proxies: %q is not a CIDR", p))
		}
	}

	if _, err := ParseLevel(c.Log.Level); err != nil {
		errs = append(errs, err)
	}

	if !slices.Contains([]string{"auto", "pretty", "text", "json"}, c.Log.Format) {
		errs = append(errs, fmt.Errorf("log.format %q must be auto, pretty, text or json", c.Log.Format))
	}

	if c.DataDir == "" {
		errs = append(errs, errors.New("data_dir must not be empty"))
	}

	errs = append(errs, c.Storage.validate()...)
	errs = append(errs, c.Secrets.validate()...)
	errs = append(errs, validateKodi(c.Kodi)...)
	errs = append(errs, c.validateRoomsAndDevices()...)
	errs = append(errs, c.validateStateless()...)

	if c.Alexa.QueueLimit < 1 {
		errs = append(errs, fmt.Errorf("alexa.queue_limit %d must be at least 1", c.Alexa.QueueLimit))
	}

	errs = append(errs, c.Auth.validate(c.Server.TrustedProxies)...)

	for _, l := range c.Alexa.Locales {
		if !localePattern.MatchString(l) {
			errs = append(errs, fmt.Errorf("alexa.locales: %q is not a locale such as en-AU", l))
		}
	}

	for _, g := range c.Alexa.Content {
		if g != ContentMovies && g != ContentTV && g != ContentMusic {
			errs = append(errs, fmt.Errorf("alexa.content: %q is not one of movies, tv or music", g))
		}
	}

	if len(c.Alexa.Content) == 0 {
		errs = append(errs, errors.New("alexa.content must list at least one of movies, tv or music"))
	}

	return errors.Join(errs...)
}

// Managed reports whether key (a dotted path such as storage.backend) or any key below it was set by the config file, environment or flags. The web interface shows such settings read-only.
func (c *Config) Managed(key string) bool {
	if c.managed[key] {
		return true
	}

	for k := range c.managed {
		if strings.HasPrefix(k, key+".") {
			return true
		}
	}

	return false
}

// ManagedKeys returns the keys set by the config file, environment or flags, sorted.
func (c *Config) ManagedKeys() []string {
	return slices.Sorted(maps.Keys(c.managed))
}

// listKeys are keys whose environment variables hold comma-separated lists.
var listKeys = map[string]bool{"server.trusted_proxies": true, "alexa.locales": true, "alexa.content": true, "auth.oidc.scopes": true}

// envAliases maps shorter environment variable names, lower-cased and without the prefix, to the keys they set.
var envAliases = map[string]string{"break_glass": "auth.break_glass"}

// envKeys maps environment variable names, lower-cased and without the prefix, to the keys they set: each key with its dots as underscores, plus envAliases.
var envKeys = func() map[string]string {
	out := map[string]string{}

	for key := range defaultsMap() {
		out[EnvName(key)] = key
	}

	maps.Copy(out, envAliases)

	return out
}()

// EnvName returns key's environment variable name, lower-cased and without the prefix.
func EnvName(key string) string {
	return strings.ReplaceAll(key, ".", "_")
}

// IsEnvList reports whether key's environment variable holds a comma-separated list.
func IsEnvList(key string) bool {
	return listKeys[key]
}

// EnvAliases returns the environment variables, without the ENCORE_ prefix, that set a key under a shorter name, mapped to that key.
func EnvAliases() map[string]string {
	out := make(map[string]string, len(envAliases))
	for name, key := range envAliases {
		out[strings.ToUpper(name)] = key
	}

	return out
}

// ParseLevel converts a configured log level to an slog.Level.
func ParseLevel(s string) (slog.Level, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("log.level %q must be debug, info, warn or error", s)
	}

	return level, nil
}

// validateBasePath also refuses a first part that names a top-level route: Encore serves at / as well as under the base path, so /kodi would make /kodi/lounge ambiguous.
func validateBasePath(base string) []error {
	if !basePathPattern.MatchString(base) {
		return []error{fmt.Errorf("server.base_path %q must be a path such as /encore: it starts with /, and each part uses only letters, digits and . _ ~ -", base)}
	}

	first, _, _ := strings.Cut(strings.Trim(base, "/"), "/")
	if slices.Contains(ReservedBasePaths, strings.ToLower(first)) {
		return []error{fmt.Errorf("server.base_path %q must not start with /%s, which is one of Encore's own routes", base, first)}
	}

	return nil
}

func defaultsMap() map[string]any {
	d := Defaults()

	return map[string]any{
		"server.address":         d.Server.Address,
		"server.base_path":       d.Server.BasePath,
		"server.trusted_proxies": d.Server.TrustedProxies,
		"alexa.locales":          []string{},
		"alexa.content":          d.Alexa.Content,
		"log.level":              d.Log.Level,
		"log.format":             d.Log.Format,
		"stateless":              d.Stateless,
		"data_dir":               d.DataDir,
		"alexa.skill_id":         d.Alexa.SkillID,
		"alexa.shutdown_quits":   d.Alexa.ShutdownQuits,
		"alexa.confirm_sound":    d.Alexa.ConfirmSound,
		"alexa.deep_search":      d.Alexa.DeepSearch,
		"alexa.queue_limit":      d.Alexa.QueueLimit,

		"storage.backend":                     d.Storage.Backend,
		"storage.local.path":                  d.Storage.Local.Path,
		"storage.s3.endpoint":                 d.Storage.S3.Endpoint,
		"storage.s3.region":                   d.Storage.S3.Region,
		"storage.s3.bucket":                   d.Storage.S3.Bucket,
		"storage.s3.prefix":                   d.Storage.S3.Prefix,
		"storage.s3.addressing":               d.Storage.S3.Addressing,
		"storage.s3.access_key":               d.Storage.S3.AccessKey,
		"storage.s3.secret_key":               d.Storage.S3.SecretKey,
		"storage.s3.ca_bundle":                d.Storage.S3.CABundle,
		"storage.s3.insecure_skip_verify":     d.Storage.S3.InsecureSkipVerify,
		"storage.s3.sse":                      d.Storage.S3.SSE,
		"storage.s3.sse_kms_key_id":           d.Storage.S3.SSEKMSKeyID,
		"storage.webdav.url":                  d.Storage.WebDAV.URL,
		"storage.webdav.username":             d.Storage.WebDAV.Username,
		"storage.webdav.password":             d.Storage.WebDAV.Password,
		"storage.webdav.prefix":               d.Storage.WebDAV.Prefix,
		"storage.webdav.ca_bundle":            d.Storage.WebDAV.CABundle,
		"storage.webdav.insecure_skip_verify": d.Storage.WebDAV.InsecureSkipVerify,
		"storage.backups.schedule":            d.Storage.Backups.Schedule,
		"storage.backups.keep":                d.Storage.Backups.Keep,
		"storage.backups.prefix":              d.Storage.Backups.Prefix,

		"secrets.key":      d.Secrets.Key,
		"secrets.key_file": d.Secrets.KeyFile,

		"auth.mode":                 d.Auth.Mode,
		"auth.break_glass":          d.Auth.BreakGlass,
		"auth.session_hours":        d.Auth.SessionHours,
		"auth.lockout_attempts":     d.Auth.LockoutAttempts,
		"auth.lockout_minutes":      d.Auth.LockoutMinutes,
		"auth.oidc.issuer":          d.Auth.OIDC.Issuer,
		"auth.oidc.client_id":       d.Auth.OIDC.ClientID,
		"auth.oidc.client_secret":   d.Auth.OIDC.ClientSecret,
		"auth.oidc.scopes":          d.Auth.OIDC.Scopes,
		"auth.oidc.admin_group":     d.Auth.OIDC.AdminGroup,
		"auth.oidc.groups_claim":    d.Auth.OIDC.GroupsClaim,
		"auth.oidc.redirect_uri":    d.Auth.OIDC.RedirectURI,
		"auth.header.user_header":   d.Auth.Header.UserHeader,
		"auth.header.groups_header": d.Auth.Header.GroupsHeader,
		"auth.header.admin_group":   d.Auth.Header.AdminGroup,
	}
}

func splitList(s string) []string {
	var out []string

	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}

	return out
}
