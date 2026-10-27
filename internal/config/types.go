package config

import (
	"time"

	"github.com/spf13/pflag"
)

// Config is the complete runtime configuration.
type Config struct {
	Server Server `koanf:"server"`
	Log    Log    `koanf:"log"`
	// DataDir holds the database, the generated secrets key, local storage and encore.yml. It defaults to a per-user directory; the container image sets it to /config and the systemd unit to /var/lib/encore.
	DataDir string  `koanf:"data_dir"`
	Alexa   Alexa   `koanf:"alexa"`
	Storage Storage `koanf:"storage"`
	Secrets Secrets `koanf:"secrets"`
	Auth    Auth    `koanf:"auth"`
	// Kodi declares Kodi instances as code; most installs add them in the web UI instead.
	Kodi []KodiInstance `koanf:"kodi"`
	// Rooms declares rooms as code, mainly for stateless deployments.
	Rooms []Room `koanf:"rooms"`
	// Devices declares Echo links as code, mainly for stateless deployments.
	Devices []Device `koanf:"devices"`
	// Stateless runs without a persistent data directory: the database is restored from the storage backend at startup and saved back as it changes, like Kanzi's cloud deployments.
	Stateless bool `koanf:"stateless"`

	// managed holds the keys set by the config file, environment or flags rather than defaults.
	managed map[string]bool
}

// Server configures the single HTTP listener.
type Server struct {
	// Address is the listen address, e.g. ":8080".
	Address string `koanf:"address"`
	// BasePath is the URL path prefix when served behind a path-routing proxy, e.g. /encore. Encore also keeps answering at /, so the proxy may forward with or without the prefix and a wrong value never locks you out. It can't start with one of Encore's own routes (/api, /kodi, /settings, ...). It can also be changed under Settings › General, which applies it straight away.
	BasePath string `koanf:"base_path"`
	// TrustedProxies lists the CIDRs whose X-Forwarded-* and auth headers are honoured. Headers from anywhere else are ignored.
	TrustedProxies []string `koanf:"trusted_proxies"`
}

// Log configures structured logging.
type Log struct {
	// Level is debug, info, warn or error. It can also be changed under Settings › General, which applies it without a restart.
	Level string `koanf:"level"`
	// Format is auto, pretty, text or json. pretty is coloured for a terminal (honouring NO_COLOR), text is logfmt key=value lines, json is one object per line; auto is pretty on a terminal and text otherwise.
	Format string `koanf:"format"`
}

// Alexa configures the custom skill endpoint.
type Alexa struct {
	// SkillID is the skill's application ID; requests for any other skill are rejected. It is normally set by the deploy flow rather than by hand.
	SkillID string `koanf:"skill_id"`
	// Locales limits which locales are built and deployed, e.g. ["en-AU"]. Empty means every locale Encore ships. An Echo can only use the skill when its language matches a deployed locale; fewer locales deploy faster.
	Locales []string `koanf:"locales"`
	// Content lists what the skill can play: movies, tv and music (music includes music videos). Leaving a group out leaves its intents out of the skill model and its titles out of "play X" searches, and library syncs no longer fetch it from Kodi. The setup wizard asks before the first Kodi is added.
	Content []string `koanf:"content"`
	// ShutdownQuits makes "shut down" quit Kodi rather than power off the machine, for Kodi running on a desktop. A Kodi instance can override it.
	ShutdownQuits bool `koanf:"shutdown_quits"`
	// ConfirmSound plays a short sound after a command that has nothing to say, such as pause or volume up, so Alexa doesn't answer with silence.
	ConfirmSound bool `koanf:"confirm_sound"`
	// DeepSearch makes "play X" search music as well as films and TV when the request doesn't say which (Kanzi's deep_search).
	DeepSearch bool `koanf:"deep_search"`
	// QueueLimit caps how many items a shuffle or queue plays at once (Kanzi's playlist_max_items), which keeps large libraries responsive. It can also be changed under Settings › Features.
	QueueLimit int `koanf:"queue_limit"`
}

// Auth configures how admins sign in to the web interface. Anything left unset here can be changed in the web interface instead; values set here are shown there as managed by config.
type Auth struct {
	// Mode is local (a username and password stored by Encore), oidc (an OpenID Connect provider) or header (a trusted reverse proxy's Remote-User header). Empty leaves the choice to the web interface, which starts in local mode.
	Mode string `koanf:"mode"`
	// BreakGlass keeps local sign-in working while oidc or header mode is active, as a way in when the provider or proxy is broken. It can be turned on under Settings › Access; if you're already locked out, set ENCORE_BREAK_GLASS=true and remove it afterwards.
	BreakGlass bool `koanf:"break_glass"`
	// SessionHours is how long an idle session lasts; activity extends it.
	SessionHours int `koanf:"session_hours"`
	// LockoutAttempts failed sign-ins within LockoutMinutes lock the account for LockoutMinutes.
	LockoutAttempts int `koanf:"lockout_attempts"`
	// LockoutMinutes is both the window failed sign-ins are counted in and how long the lockout lasts.
	LockoutMinutes int        `koanf:"lockout_minutes"`
	OIDC           AuthOIDC   `koanf:"oidc"`
	Header         AuthHeader `koanf:"header"`
}

// AuthOIDC configures sign-in through an OpenID Connect provider such as Authelia, Authentik, Keycloak or Pocket ID.
type AuthOIDC struct {
	// Issuer is the provider's issuer URL; its /.well-known/openid-configuration must be reachable from Encore.
	Issuer string `koanf:"issuer"`
	// ClientID is the client registered for Encore at the provider.
	ClientID string `koanf:"client_id"`
	// ClientSecret is best supplied with ENCORE_AUTH_OIDC_CLIENT_SECRET_FILE.
	ClientSecret string `koanf:"client_secret"`
	// Scopes are requested at sign-in; include the one that adds GroupsClaim to the ID token when AdminGroup is set.
	Scopes []string `koanf:"scopes"`
	// AdminGroup, when set, must appear in the GroupsClaim of the ID token. When empty, anyone the provider lets sign in is an admin.
	AdminGroup string `koanf:"admin_group"`
	// GroupsClaim is the ID token claim that lists the user's groups.
	GroupsClaim string `koanf:"groups_claim"`
	// RedirectURI overrides the callback URL derived from the request, e.g. https://encore.example.net/api/v1/auth/oidc/callback.
	RedirectURI string `koanf:"redirect_uri"`
}

// AuthHeader configures trusted-header sign-in. The headers are only honoured on connections from server.trusted_proxies and are stripped from all others.
type AuthHeader struct {
	// UserHeader carries the signed-in user's name.
	UserHeader string `koanf:"user_header"`
	// GroupsHeader carries the user's groups, comma-separated.
	GroupsHeader string `koanf:"groups_header"`
	// AdminGroup, when set, must be one of the comma-separated groups in GroupsHeader.
	AdminGroup string `koanf:"admin_group"`
}

// Storage configures where snapshots, exports and database backups are written. The SQLite database itself always lives in data_dir.
type Storage struct {
	// Backend is local, s3 or webdav.
	Backend string        `koanf:"backend"`
	Local   StorageLocal  `koanf:"local"`
	S3      StorageS3     `koanf:"s3"`
	WebDAV  StorageWebDAV `koanf:"webdav"`
	Backups Backups       `koanf:"backups"`
}

// StorageLocal configures the local filesystem backend.
type StorageLocal struct {
	// Path is the root directory; empty means data_dir/blobs.
	Path string `koanf:"path"`
}

// StorageS3 configures an S3-compatible backend such as AWS, MinIO, Garage, Ceph RGW, SeaweedFS, R2 or B2.
type StorageS3 struct {
	// Endpoint is the service URL, e.g. https://s3.eu-west-1.amazonaws.com or http://minio:9000.
	Endpoint string `koanf:"endpoint"`
	// Region is the signing region; empty lets the client discover it.
	Region string `koanf:"region"`
	// Bucket must already exist.
	Bucket string `koanf:"bucket"`
	// Prefix is prepended to every object key.
	Prefix string `koanf:"prefix"`
	// Addressing is auto, path (http://host/bucket/key) or virtual (http://bucket.host/key). Most self-hosted servers need path.
	Addressing string `koanf:"addressing"`
	// AccessKey is the access key ID.
	AccessKey string `koanf:"access_key"`
	// SecretKey is best supplied with ENCORE_STORAGE_S3_SECRET_KEY_FILE.
	SecretKey string `koanf:"secret_key"`
	// CABundle is a PEM file of extra CAs trusted in addition to the system pool.
	CABundle string `koanf:"ca_bundle"`
	// InsecureSkipVerify disables TLS certificate checks. For labs only.
	InsecureSkipVerify bool `koanf:"insecure_skip_verify"`
	// SSE requests server-side encryption: empty for none, s3 (SSE-S3) or kms (SSE-KMS with SSEKMSKeyID).
	SSE string `koanf:"sse"`
	// SSEKMSKeyID is the KMS key used when SSE is kms.
	SSEKMSKeyID string `koanf:"sse_kms_key_id"`
}

// StorageWebDAV configures a WebDAV backend such as Nextcloud or ownCloud.
type StorageWebDAV struct {
	// URL is the collection to write under, e.g. https://cloud.example.com/remote.php/dav/files/backup/encore.
	URL string `koanf:"url"`
	// Username is the WebDAV account name.
	Username string `koanf:"username"`
	// Password is best set to an app password rather than the account password.
	Password string `koanf:"password"`
	// Prefix is prepended to every object path below URL.
	Prefix string `koanf:"prefix"`
	// CABundle is a PEM file of extra CAs trusted in addition to the system pool.
	CABundle string `koanf:"ca_bundle"`
	// InsecureSkipVerify disables TLS certificate checks. For labs only.
	InsecureSkipVerify bool `koanf:"insecure_skip_verify"`
}

// Backups configures scheduled database backups to the storage backend.
type Backups struct {
	// Schedule is off, hourly, daily@HH:MM, weekly@DAY@HH:MM or every@DURATION (at least 1m), with times in the server's local time zone; see ParseSchedule.
	Schedule string `koanf:"schedule"`
	// Keep is how many backups to retain.
	Keep int `koanf:"keep"`
	// Prefix is the folder backups are written to, below the backend's own prefix. Empty puts them at the top.
	Prefix string `koanf:"prefix"`
}

// Secrets configures encryption of secrets at rest. With neither field set a key is generated into data_dir.
type Secrets struct {
	// Key is a base64-encoded 32-byte key. ENCORE_SECRETS_KEY_FILE fills it from a file.
	Key string `koanf:"key"`
	// KeyFile names a file holding the base64-encoded key. It can only be set in the config file, because the environment variable of that name is the _FILE form of Key.
	KeyFile string `koanf:"key_file"`
}

// KodiInstance declares a Kodi host in the config file. Instances declared here are created or updated in the database at startup and shown as managed by config in the web UI; instances added in the UI live only in the database.
type KodiInstance struct {
	// Name identifies the instance, e.g. "Lounge". It must be unique.
	Name string `koanf:"name"`
	// URL is the HTTP JSON-RPC endpoint, e.g. http://192.168.1.20:8080/jsonrpc.
	URL string `koanf:"url"`
	// WebSocketURL is the JSON-RPC WebSocket for live updates, e.g. ws://192.168.1.20:9090/jsonrpc. Empty disables live updates.
	WebSocketURL string `koanf:"websocket_url"`
	// Username is Kodi's web server user name; when empty, the user in URL is used.
	Username string `koanf:"username"`
	// Password is Kodi's web server password; when empty, the password in URL is used.
	Password string `koanf:"password"`
	// MAC enables Wake-on-LAN. Encore also learns it from Kodi once connected.
	MAC string `koanf:"mac"`
	// Broadcast is where Wake-on-LAN packets are sent, "host" or "host:port"; empty means 255.255.255.255:9.
	Broadcast string `koanf:"broadcast"`
	// Default makes this instance answer Echo devices that aren't linked to one.
	Default bool `koanf:"default"`
	// Language selects title-matching rules for this library: en, de, es, fr or it. Defaults to en.
	Language string `koanf:"language"`
	// LibraryGroup names the shared library database this instance reads (MySQL/MariaDB). Instances with the same name share one library catalog and title index. "none" (LibraryGroupNone) forces the instance to keep its own; empty lets Encore detect sharing.
	LibraryGroup string `koanf:"library_group"`
	// ShutdownQuits overrides alexa.shutdown_quits for this instance when set.
	ShutdownQuits *bool `koanf:"shutdown_quits"`
	// Timezone is the Kodi host's IANA time zone (e.g. "Australia/Melbourne"), which Kodi writes library dates in. Empty means learn the UTC offset from Kodi's clock.
	Timezone string `koanf:"timezone"`
}

// Room declares a room in the config file: where an Echo is, and the spoken names for "…in the lounge".
type Room struct {
	// Name is the room's name, which is also a spoken name for it.
	Name string `koanf:"name"`
	// Kodi is the name of the Kodi instance that plays in this room.
	Kodi string `koanf:"kodi"`
	// Aliases are other spoken names for the room, e.g. "living room" for the lounge.
	Aliases []string `koanf:"aliases"`
}

// Device declares an Echo in the config file, linking it to a Kodi instance (and optionally a room) without the web interface.
type Device struct {
	// ID is the Alexa device ID (amzn1.ask.device.…), shown in the request log for new Echos.
	ID string `koanf:"id"`
	// Name is shown in the web interface and the request log.
	Name string `koanf:"name"`
	// Kodi is the name of the Kodi instance this Echo controls.
	Kodi string `koanf:"kodi"`
	// Room is the name of the room the Echo is in.
	Room string `koanf:"room"`
}

// Schedule is a parsed backup schedule. The zero value is disabled.
type Schedule struct {
	kind    scheduleKind
	hour    int
	minute  int
	weekday time.Weekday
	every   time.Duration
}

type scheduleKind int

// Options control where Load reads from.
type Options struct {
	// File is an optional YAML file. A missing file is an error only when the path was given explicitly.
	File string
	// Flags are command-line flags; only flags the user set override other sources.
	Flags *pflag.FlagSet
	// Environ supplies environment variables; nil means os.Environ.
	Environ func() []string
}
