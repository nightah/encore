package api

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"golang.org/x/oauth2"

	"github.com/nightah/encore/internal/adapter/alexa"
	"github.com/nightah/encore/internal/app"
	"github.com/nightah/encore/internal/auth"
	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/core/actions"
	"github.com/nightah/encore/internal/core/match"
	"github.com/nightah/encore/internal/secrets"
	"github.com/nightah/encore/internal/skillmodel"
	"github.com/nightah/encore/internal/smapi"
	"github.com/nightah/encore/internal/store"
)

// Options configure New. Only App is required.
type Options struct {
	App *app.App
	// Sources is the interaction model, for skill packages and replays; nil loads the embedded one.
	Sources *skillmodel.Sources
	// Speech and Actions render replayed requests; replay is unavailable without them.
	Speech  *alexa.Speech
	Actions *actions.Service
	// Libraries implements the shared-library endpoints; nil uses the app's library groups.
	Libraries Libraries
	// SlotValues supplies library titles, rooms and playlists for the skill model; nil uses the app's.
	SlotValues SlotValuesFunc
	// Discover finds Kodi instances on the network; nil uses mDNS.
	Discover func(ctx context.Context) ([]kodi.Instance, error)
	// ProbeVersion reads a discovered Kodi's version; nil asks it over its WebSocket port.
	ProbeVersion func(ctx context.Context, host string, wsPort int) (string, error)
	Logger       *slog.Logger
	// LogLevel is Logger's level, set from log.level when it's saved on the settings page; nil leaves the level alone.
	LogLevel *slog.LevelVar
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// HTTPClient makes outbound requests (OIDC provider, endpoint check); nil means a client with a timeout.
	HTTPClient *http.Client
	// PublicLookup resolves a host name the way the internet sees it, for the endpoint check; nil asks public DNS servers.
	PublicLookup func(ctx context.Context, host string) ([]string, error)
	// SMAPI and LWAEndpoint point skill deploys at a fake in tests.
	SMAPI       smapi.Options
	LWAEndpoint *oauth2.Endpoint
	// Heartbeat is the event stream's keep-alive interval; zero means 15 seconds.
	Heartbeat time.Duration
	// StatusInterval is how often Kodi instances are polled for the event stream; zero means 10 seconds.
	StatusInterval time.Duration
	// after waits between scheduled backup checks; nil means time.After. Tests replace it to step through the schedule.
	after func(time.Duration) <-chan time.Time
}

// API is the admin API.
type API struct {
	app       *app.App
	auth      *auth.Service
	prefs     *Prefs
	libraries Libraries
	skill     *skillService
	events    *hub
	status    *statusCache
	log       *slog.Logger
	now       func() time.Time
	opts      Options
	handler   http.Handler
	humaAPI   huma.API

	clientErrors clientErrorLimiter
	runMu        sync.Mutex
	// runCtx outlives requests: background work started by a request (sync, wake, deploy) runs under it.
	runCtx context.Context
	bg     sync.WaitGroup
}

// AuthState tells the web interface whether to show setup, the sign-in page or the app.
type AuthState struct {
	SetupRequired bool           `json:"setup_required"`
	Authenticated bool           `json:"authenticated"`
	User          Nullable[User] `json:"user"`
	Mode          string         `json:"mode" enum:"local,oidc,header"`
	CSRFToken     string         `json:"csrf_token" doc:"Send as X-CSRF-Token on every POST, PUT, PATCH and DELETE"`
	OIDCLoginURL  *string        `json:"oidc_login_url" doc:"Where to send the browser to sign in with OpenID Connect, or null"`
	LocalLogin    bool           `json:"local_login" doc:"Whether the username and password form is available (always in local mode; in other modes only with the break-glass login on)"`
}

// TOTPEnrollment is a new TOTP secret to add to an authenticator app.
type TOTPEnrollment struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url" doc:"otpauth:// URL, for a QR code"`
}

// User is the signed-in admin.
type User struct {
	Name string `json:"name"`
}

type redirect struct {
	Status   int
	Location string `header:"Location"`
}

// ClientErrorReport is an error the web interface hit in the browser.
type ClientErrorReport struct {
	Kind    string `json:"kind" enum:"render,unhandled" doc:"render: a page failed to draw; unhandled: an uncaught error or rejected promise"`
	Message string `json:"message" maxLength:"1000"`
	Stack   string `json:"stack,omitempty" maxLength:"8000"`
	Path    string `json:"path" maxLength:"500" doc:"The page, e.g. /setup"`
}

type clientErrorLimiter struct {
	mu      sync.Mutex
	window  time.Time
	count   int
	dropped int
}

// ImportResult lists what an import changed.
type ImportResult struct {
	Applied []string `json:"applied"`
	Skipped []string `json:"skipped" doc:"Keys left alone, each with the reason"`
}

type exportedKodi struct {
	Name          string `yaml:"name"`
	URL           string `yaml:"url"`
	WebSocketURL  string `yaml:"websocket_url,omitempty"`
	Username      string `yaml:"username,omitempty"`
	Password      string `yaml:"password,omitempty"`
	MAC           string `yaml:"mac,omitempty"`
	Broadcast     string `yaml:"broadcast,omitempty"`
	Default       bool   `yaml:"default,omitempty"`
	Language      string `yaml:"language,omitempty"`
	Timezone      string `yaml:"timezone,omitempty"`
	ShutdownQuits *bool  `yaml:"shutdown_quits,omitempty"`
}

// DeployEvent is one step of a deploy's progress.
type DeployEvent struct {
	Step    string                  `json:"step" enum:"package,vendor,skill,upload,import,build,enable,simulate"`
	State   string                  `json:"state" enum:"started,succeeded,failed,skipped"`
	Message string                  `json:"message"`
	Locales map[string]LocaleStatus `json:"locales"`
	At      time.Time               `json:"at"`
}

// LocaleStatus is one locale's build state during a deploy.
type LocaleStatus struct {
	Status string   `json:"status" enum:"succeeded,failed,in_progress"`
	Errors []string `json:"errors"`
}

// Deploy is a deploy and its progress.
type Deploy struct {
	ID     string        `json:"id"`
	State  string        `json:"state" enum:"succeeded,failed,in_progress"`
	Events []DeployEvent `json:"events"`
	Error  string        `json:"error"`
}

type deployEventMessage struct {
	DeployID string `json:"deploy_id"`
	DeployEvent
}

// deployRecord is a deploy as kept in memory and, for the last one, in settings.
type deployRecord struct {
	ID       string         `json:"id"`
	At       time.Time      `json:"at"`
	State    string         `json:"state"`
	Hash     string         `json:"hash"`
	Events   []DeployEvent  `json:"events"`
	Locales  []LocaleDeploy `json:"locales"`
	Warnings []string       `json:"warnings"`
	Error    string         `json:"error"`
}

// Device is an Echo device that has talked to the skill.
type Device struct {
	ID                int64     `json:"id"`
	AlexaDeviceID     string    `json:"alexa_device_id"`
	Name              string    `json:"name"`
	RoomID            *int64    `json:"room_id"`
	KodiInstanceID    *int64    `json:"kodi_instance_id" doc:"null when the device is not linked; it then uses the default instance"`
	FirstSeen         time.Time `json:"first_seen"`
	LastSeen          time.Time `json:"last_seen"`
	RequestCount      int64     `json:"request_count"`
	RequestCountToday int       `json:"request_count_today"`
}

// OptionalID is a nullable ID in a partial update: absent leaves the value alone, null clears it.
type OptionalID struct {
	Set bool
	ID  int64
}

type event struct {
	ID   uint64
	Name string
	Data []byte
}

// hub fans events out to stream clients and remembers recent ones for resumption.
type hub struct {
	mu      sync.Mutex
	nextID  uint64
	ring    []event
	clients map[*streamClient]struct{}
}

type streamClient struct {
	ch      chan event
	dropped chan struct{}
}

// Instance is a Kodi instance with its live status.
type Instance struct {
	ID                 int64                `json:"id"`
	Name               string               `json:"name"`
	URL                string               `json:"url" doc:"HTTP JSON-RPC endpoint, without credentials"`
	WebSocketURL       string               `json:"websocket_url" doc:"Empty when live updates are disabled"`
	Username           string               `json:"username"`
	HasPassword        bool                 `json:"has_password"`
	MAC                string               `json:"mac"`
	Broadcast          string               `json:"broadcast"`
	DefaultForUnlinked bool                 `json:"default_for_unlinked"`
	Language           string               `json:"language" doc:"Title-matching rules for this library"`
	Timezone           string               `json:"timezone" doc:"The Kodi host's IANA time zone; empty when learnt from Kodi's clock"`
	Shutdown           string               `json:"shutdown" enum:"inherit,quit,power_off" doc:"What \"shut down\" does; inherit follows Settings › Features"`
	ManagedByConfig    bool                 `json:"managed_by_config"`
	Status             InstanceStatus       `json:"status"`
	Library            InstanceLibrary      `json:"library"`
	NowPlaying         Nullable[NowPlaying] `json:"now_playing"`
}

// InstanceStatus is a Kodi instance's connection state.
type InstanceStatus struct {
	State       string     `json:"state" enum:"connected,http_only,asleep,offline"`
	KodiVersion string     `json:"kodi_version"`
	APIVersion  string     `json:"api_version"`
	LastEventAt *time.Time `json:"last_event_at"`
	LastSyncAt  *time.Time `json:"last_sync_at"`
	NextSyncAt  *time.Time `json:"next_sync_at"`
}

// InstanceLibrary summarises an instance's library and its place in a shared library.
type InstanceLibrary struct {
	GroupID     int64                 `json:"group_id"`
	Shared      bool                  `json:"shared"`
	IsSource    bool                  `json:"is_source"`
	Movies      int                   `json:"movies"`
	Shows       int                   `json:"shows"`
	Episodes    int                   `json:"episodes"`
	Songs       int                   `json:"songs"`
	NewestTitle Nullable[NewestTitle] `json:"newest_title"`
}

// NewestTitle is the most recently added movie or episode.
type NewestTitle struct {
	Title    string `json:"title"`
	Year     int    `json:"year"`
	Findable bool   `json:"findable"`
}

// NowPlaying is what an instance is playing.
type NowPlaying struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Show      string `json:"show"`
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	PositionS int    `json:"position_s"`
	DurationS int    `json:"duration_s"`
	Paused    bool   `json:"paused"`
	ArtURL    string `json:"art_url" doc:"The item's poster or thumbnail, fetched from Kodi through encore; empty when Kodi has none"`
}

// InstanceInput creates or changes a Kodi instance. On PATCH every field is optional.
type InstanceInput struct {
	Name         *string `json:"name,omitempty" minLength:"1" maxLength:"64"`
	URL          *string `json:"url,omitempty" doc:"HTTP JSON-RPC endpoint, e.g. http://192.168.1.20:8080/jsonrpc"`
	WebSocketURL *string `json:"websocket_url,omitempty" doc:"e.g. ws://192.168.1.20:9090/jsonrpc; empty disables live updates"`
	Username     *string `json:"username,omitempty"`
	Password     *string `json:"password,omitempty" doc:"Write-only; empty removes it"`
	MAC          *string `json:"mac,omitempty"`
	Broadcast    *string `json:"broadcast,omitempty"`
	Language     *string `json:"language,omitempty" doc:"Title-matching rules, e.g. en or de"`
	Timezone     *string `json:"timezone,omitempty" doc:"The Kodi host's IANA time zone; empty learns it from Kodi's clock"`
	Shutdown     *string `json:"shutdown,omitempty" enum:"inherit,quit,power_off"`
}

// Check is one step of a connection test.
type Check struct {
	Name   string `json:"name" enum:"ping,websocket,auth,library"`
	OK     bool   `json:"ok"`
	MS     int64  `json:"ms"`
	Detail string `json:"detail"`
}

// TestResult is a connection test's checks, in order.
type TestResult struct {
	Checks []Check `json:"checks"`
}

// DiscoveredInstance is a Kodi found on the network that is not configured yet.
type DiscoveredInstance struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	HTTPPort int    `json:"http_port"`
	WSPort   int    `json:"ws_port"`
	Version  string `json:"version"`
}

// Accepted is the body of a 202 for work that continues in the background.
type Accepted struct {
	ID string `json:"id"`
}

type idPath struct {
	ID int64 `path:"id"`
}

type body[T any] struct {
	Body T
}

// Library is a media library shared by one or more Kodi instances (one MySQL/MariaDB database), synced from one source instance.
type Library struct {
	ID                int64           `json:"id"`
	Name              string          `json:"name"`
	Members           []LibraryMember `json:"members"`
	SourceInstanceID  int64           `json:"source_instance_id"`
	SyncingInstanceID int64           `json:"syncing_instance_id" doc:"The instance Encore currently reads the library from: the source, or another member while the source is unreachable"`
	State             string          `json:"state" enum:"solo,likely_shared,confirmed_shared,manual"`
	Evidence          string          `json:"evidence"`
	LastSyncAt        *time.Time      `json:"last_sync_at"`
	Counts            LibraryCounts   `json:"counts"`
}

// LibraryMember is one Kodi instance in a library.
type LibraryMember struct {
	InstanceID int64  `json:"instance_id"`
	Name       string `json:"name"`
}

// LibraryCounts are a library's sizes.
type LibraryCounts struct {
	Movies int `json:"movies"`
	Shows  int `json:"shows"`
	Songs  int `json:"songs"`
}

// RecentTitle is a recently added library item.
type RecentTitle struct {
	Title        string     `json:"title"`
	Year         int        `json:"year"`
	Kind         string     `json:"kind"`
	Instance     string     `json:"instance"`
	AddedAt      *time.Time `json:"added_at"`
	Findable     bool       `json:"findable"`
	InSkillModel bool       `json:"in_skill_model"`
}

// Libraries is what the shared-library endpoints need from the runtime. appLibraries implements it with the app's library groups; tests can substitute their own.
type Libraries interface {
	List(ctx context.Context) ([]Library, error)
	Get(ctx context.Context, id int64) (Library, error)
	// LibraryOf returns the library an instance belongs to.
	LibraryOf(ctx context.Context, instanceID int64) (Library, error)
	SetSource(ctx context.Context, id, instanceID int64) (Library, error)
	Group(ctx context.Context, instanceIDs []int64) (Library, error)
	Split(ctx context.Context, id, instanceID int64) error
	Recent(ctx context.Context, id int64, limit int) ([]RecentTitle, error)
}

// appLibraries serves the app's library groups: Kodi instances that share a library database are one library, with the group's ID.
type appLibraries struct {
	api *API
}

// MatchResult is the match tester's answer.
type MatchResult struct {
	Normalized string      `json:"normalized"`
	Decision   string      `json:"decision" enum:"play,ask,not_found"`
	Margin     float64     `json:"margin"`
	Candidates []Candidate `json:"candidates"`
}

// TitleAlias is a user-defined spoken name for one library title.
type TitleAlias struct {
	ID        int64  `json:"id"`
	LibraryID int64  `json:"library_id"`
	Kind      string `json:"kind"`
	MediaID   int    `json:"media_id"`
	Title     string `json:"title"`
	Alias     string `json:"alias"`
}

type ctxKey struct{}

// reqInfo is the request-scoped state the middleware hands to handlers.
type reqInfo struct {
	r         *http.Request
	w         http.ResponseWriter
	principal *auth.Principal
}

// Nullable is an object that is null when absent. Huma cannot mark a referenced object schema nullable, so Nullable describes itself as oneOf the object or null, which the web client generator turns into `T | null`.
type Nullable[T any] struct {
	V *T
}

// Overview is the dashboard.
type Overview struct {
	Skill          OverviewSkill    `json:"skill"`
	Endpoint       EndpointCheck    `json:"endpoint"`
	Libraries      OverviewLibs     `json:"libraries"`
	RequestsToday  RequestsToday    `json:"requests_today"`
	Instances      []Instance       `json:"instances"`
	RecentRequests []RequestSummary `json:"recent_requests"`
	Attention      []Attention      `json:"attention"`
}

// OverviewSkill summarises the skill's deploy state.
type OverviewSkill struct {
	State        string   `json:"state" enum:"live,not_deployed,deploying,failed"`
	Stage        string   `json:"stage"`
	Locales      []string `json:"locales"`
	NeedsDeploy  bool     `json:"needs_deploy"`
	UpdateReason string   `json:"update_reason" enum:"none,settings,library" doc:"Why needs_deploy is set: the skill's settings changed, or only the library titles in the model"`
}

// EndpointCheck is whether the skill endpoint answers from its public URL.
type EndpointCheck struct {
	URL       string     `json:"url"`
	Reachable bool       `json:"reachable"`
	LatencyMS int64      `json:"latency_ms"`
	CheckedAt *time.Time `json:"checked_at"`
}

// OverviewLibs summarises the libraries.
type OverviewLibs struct {
	Count        int        `json:"count"`
	SyncedAt     *time.Time `json:"synced_at"`
	SharedGroups int        `json:"shared_groups"`
}

// RequestsToday summarises today's requests.
type RequestsToday struct {
	Total    int   `json:"total"`
	Failed   int   `json:"failed"`
	MedianMS int64 `json:"median_ms"`
}

// Attention is something the user should look at.
type Attention struct {
	Kind   string `json:"kind" doc:"unlinked_device, amazon_not_connected, amazon_reauthorise, privacy_unanswered, skill_not_deployed, needs_deploy, deploy_failed, instance_offline, kodi_unauthorized, no_instances"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Link   string `json:"link" doc:"A web interface route: /echo-devices, /kodi, /alexa-skill, /request-log, /library or /settings"`
}

type endpointCache struct {
	mu    sync.Mutex
	url   string
	check EndpointCheck
}

// Prefs is the effective configuration: encore.yml and ENCORE_* variables where they set a key, otherwise what was saved in the web interface, otherwise the defaults.
type Prefs struct {
	cfg *config.Config
	db  *store.DB
	sec *secrets.Store

	mu        sync.Mutex
	effective *config.Config
}

// Change is one key to save: a config key and its new value; nil clears the override, falling back to config or the default.
type Change struct {
	Key   string
	Value any
}

// Reachability is how Encore's public address looks from outside.
type Reachability struct {
	URL    string              `json:"url"`
	Checks []ReachabilityCheck `json:"checks"`
}

// ReachabilityCheck is one finding.
type ReachabilityCheck struct {
	Name   string `json:"name" enum:"alexa_endpoint,certificate,admin_exposed"`
	Level  string `json:"level" enum:"ok,warn,fail"`
	Detail string `json:"detail"`
	Note   string `json:"note,omitempty" doc:"Extra information about a passing check, shown beneath it; may hold line breaks"`
}

// endpointRoute is where the endpoint check connects for a host, and how that was decided.
type endpointRoute struct {
	host string
	// addrs are the public addresses to connect to; empty means let the transport resolve the host itself.
	addrs []string
	// local is what this server's own resolver returned, when it differs from the public answer.
	local []string
	// lookupErr is why public DNS couldn't be used.
	lookupErr error
}

// RequestSummary is one handled Alexa request.
type RequestSummary struct {
	ID         int64     `json:"id"`
	Time       time.Time `json:"time"`
	DeviceName string    `json:"device_name"`
	DeviceID   *int64    `json:"device_id" doc:"The Echo device's ID in /devices, or null if it has been forgotten"`
	Locale     string    `json:"locale"`
	Intent     string    `json:"intent"`
	Heard      string    `json:"heard" doc:"What Alexa heard for the title, if the intent has one"`
	Outcome    string    `json:"outcome" enum:"played,asked,not_found,unlinked,error,ok"`
	DurationMS int64     `json:"duration_ms"`
	Target     string    `json:"target" doc:"The Kodi instance that handled it"`
	Simulated  bool      `json:"simulated" doc:"Sent by Amazon's simulator during a deploy's smoke test, not by an Echo"`
}

// RequestDetail is a request with everything that was logged about it.
type RequestDetail struct {
	RequestSummary

	Slots     map[string]string `json:"slots"`
	Via       Nullable[Via]     `json:"via" doc:"The room name in the request that picked its Kodi instance; null when none did, or for requests logged before Encore recorded it"`
	Match     Nullable[Match]   `json:"match"`
	KodiCalls []KodiCall        `json:"kodi_calls"`
	Timing    Nullable[Timing]  `json:"timing" doc:"How long each phase took; null for requests logged before Encore recorded it"`
	Speech    string            `json:"speech"`
}

// Via is a room name said in a request ("on the TV"): the name that matched, whether it names the room or a device in it, and the room. Spoken equals Room when the room's own name was said.
type Via struct {
	Spoken string `json:"spoken"`
	Kind   string `json:"kind" enum:"room,device"`
	Room   string `json:"room"`
}

// Timing splits a request's handling time: the signature check, title matching and calls to Kodi.
type Timing struct {
	VerifyMS int64 `json:"verify_ms"`
	MatchMS  int64 `json:"match_ms"`
	KodiMS   int64 `json:"kodi_ms"`
}

// Match explains how a title was matched.
type Match struct {
	Normalized string      `json:"normalized"`
	Decision   string      `json:"decision" enum:"play,ask,not_found"`
	Margin     float64     `json:"margin"`
	Candidates []Candidate `json:"candidates"`
	Target     string      `json:"target"`
	Error      string      `json:"error"`
}

// Candidate is one scored title. Kind and MediaID identify it for POST /aliases.
type Candidate struct {
	Title     string    `json:"title"`
	Year      int       `json:"year"`
	Kind      string    `json:"kind"`
	MediaID   int       `json:"media_id"`
	Score     float64   `json:"score"`
	Breakdown Breakdown `json:"breakdown"`
}

// Breakdown explains a candidate's score.
type Breakdown struct {
	Alias       string  `json:"alias"`
	TokenSet    float64 `json:"token_set"`
	JaroWinkler float64 `json:"jaro_winkler"`
	OSA         float64 `json:"osa"`
	Phonetic    float64 `json:"phonetic"`
	Boost       float64 `json:"boost"`
}

// KodiCall is one JSON-RPC call made while handling a request.
type KodiCall struct {
	Method string `json:"method"`
	MS     int64  `json:"ms"`
	OK     bool   `json:"ok"`
}

// RequestPage is a page of the request log, newest first.
type RequestPage struct {
	Items      []RequestSummary `json:"items"`
	NextBefore *int64           `json:"next_before" doc:"Pass as before= for the next page; null on the last page"`
}

// Replay is a dry run of a logged request against the current index.
type Replay struct {
	Speech    string          `json:"speech"`
	Match     Nullable[Match] `json:"match"`
	WouldCall []WouldCall     `json:"would_call"`
}

// WouldCall is a command the replay would have sent to Kodi.
type WouldCall struct {
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// loggedMatch is the request log's match column; the breakdown keeps match.Breakdown's Go field names.
type loggedMatch struct {
	Target     string  `json:"target"`
	Via        *Via    `json:"via"`
	Normalized string  `json:"normalized"`
	Decision   string  `json:"decision"`
	Margin     float64 `json:"margin"`
	Candidates []struct {
		Title   string          `json:"title"`
		Year    int             `json:"year"`
		Kind    string          `json:"kind"`
		MediaID int             `json:"media_id"`
		Score   float64         `json:"score"`
		Why     match.Breakdown `json:"breakdown"`
	} `json:"candidates"`
	Error string `json:"error"`
}

// captureRecorder keeps the one entry a replay produces.
type captureRecorder struct {
	entry alexa.LogEntry
}

// Room is a named place devices and spoken targets ("in the lounge") resolve to.
type Room struct {
	ID             int64       `json:"id"`
	Name           string      `json:"name"`
	KodiInstanceID *int64      `json:"kodi_instance_id"`
	Aliases        []RoomAlias `json:"aliases"`
}

// RoomAlias is another spoken name for a room: another name for the room itself ("lounge room") or a device in it ("TV").
type RoomAlias struct {
	ID    int64  `json:"id"`
	Alias string `json:"alias"`
	Kind  string `json:"kind" enum:"room,device"`
}

type roomAliasPath struct {
	ID      int64 `path:"id"`
	AliasID int64 `path:"alias_id"`
}

// Settings is the settings page.
type Settings struct {
	Server   ServerSettings  `json:"server"`
	Auth     AuthSettings    `json:"auth"`
	Storage  StorageSettings `json:"storage"`
	Secrets  SecretsInfo     `json:"secrets"`
	Features Features        `json:"features"`
	Logging  LoggingSettings `json:"logging"`
	Locale   LocaleSettings  `json:"locale"`
	Managed  []string        `json:"managed" doc:"Dotted keys set by encore.yml or ENCORE_* variables; shown read-only"`
}

// ServerSettings are how Encore is reached.
type ServerSettings struct {
	BasePath string `json:"base_path" doc:"The URL path Encore is served under behind a path-routing proxy, e.g. /encore; applies straight away"`
}

// AuthSettings configure how admins sign in.
type AuthSettings struct {
	Mode   string     `json:"mode" enum:"local,oidc,header"`
	Local  LocalAuth  `json:"local"`
	OIDC   OIDCAuth   `json:"oidc"`
	Header HeaderAuth `json:"header"`
}

// LocalAuth configures username and password sign-in.
type LocalAuth struct {
	Username        string `json:"username"`
	SessionHours    int    `json:"session_hours"`
	LockoutAttempts int    `json:"lockout_attempts"`
	LockoutMinutes  int    `json:"lockout_minutes"`
	TOTPEnabled     bool   `json:"totp_enabled"`
}

// OIDCAuth configures OpenID Connect sign-in.
type OIDCAuth struct {
	Issuer          string   `json:"issuer"`
	ClientID        string   `json:"client_id"`
	HasClientSecret bool     `json:"has_client_secret"`
	Scopes          []string `json:"scopes"`
	AdminGroup      string   `json:"admin_group"`
	GroupsClaim     string   `json:"groups_claim"`
	RedirectURI     string   `json:"redirect_uri"`
	BreakGlass      bool     `json:"break_glass" doc:"Whether the local password still works in oidc and header modes (the config key auth.break_glass, or ENCORE_BREAK_GLASS=true)"`
}

// HeaderAuth configures trusted-header sign-in.
type HeaderAuth struct {
	TrustedProxies []string `json:"trusted_proxies"`
	UserHeader     string   `json:"user_header"`
	GroupsHeader   string   `json:"groups_header"`
	AdminGroup     string   `json:"admin_group"`
}

// StorageSettings configure where backups and exports go.
type StorageSettings struct {
	Backend string         `json:"backend" enum:"local,s3,webdav"`
	S3      S3Settings     `json:"s3"`
	WebDAV  WebDAVSettings `json:"webdav"`
	Local   LocalStorage   `json:"local"`
	Backups BackupSettings `json:"backups"`
}

// S3Settings configure an S3-compatible backend.
type S3Settings struct {
	Endpoint           string `json:"endpoint"`
	Region             string `json:"region"`
	Bucket             string `json:"bucket"`
	Prefix             string `json:"prefix"`
	Addressing         string `json:"addressing" enum:"auto,path,virtual"`
	AccessKey          string `json:"access_key"`
	HasSecretKey       bool   `json:"has_secret_key"`
	CABundle           string `json:"ca_bundle"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
	SSE                string `json:"sse" enum:",s3,kms"`
}

// WebDAVSettings configure a WebDAV backend.
type WebDAVSettings struct {
	URL         string `json:"url"`
	Username    string `json:"username"`
	HasPassword bool   `json:"has_password"`
	Prefix      string `json:"prefix"`
}

// LocalStorage configures the local filesystem backend.
type LocalStorage struct {
	Path string `json:"path"`
}

// BackupSettings configure scheduled database backups.
type BackupSettings struct {
	Schedule     string     `json:"schedule" doc:"off, hourly, daily@HH:MM, weekly@DAY@HH:MM or every@DURATION"`
	Keep         int        `json:"keep"`
	Prefix       string     `json:"prefix" doc:"The folder backups are written to; empty is the top of the storage location"`
	LastBackupAt *time.Time `json:"last_backup_at"`
}

// SecretsInfo describes the key secrets are encrypted with.
type SecretsInfo struct {
	KeySource string     `json:"key_source" enum:"generated,config,file"`
	KeyID     string     `json:"key_id"`
	RotatedAt *time.Time `json:"rotated_at"`
}

// Features are optional behaviours. They are the config keys alexa.deep_search, alexa.shutdown_quits, alexa.confirm_sound and alexa.queue_limit.
type Features struct {
	DeepSearch    bool `json:"deep_search" doc:"\"Play X\" also searches music when the request doesn't say what kind of title X is"`
	ShutdownQuits bool `json:"shutdown_quits" doc:"\"Shut down\" quits Kodi instead of powering off the device; a Kodi instance's own kodi[].shutdown_quits wins"`
	ConfirmSound  bool `json:"confirm_sound" doc:"A short sound answers commands that have nothing to say, such as pause or volume up"`
	QueueLimit    int  `json:"queue_limit" doc:"The most items a shuffle or queue plays at once"`
}

// LoggingSettings are the config key log.level.
type LoggingSettings struct {
	Level string `json:"level" enum:"debug,info,warn,error" doc:"The least severe messages written to the log; applies straight away"`
}

// LocaleSettings are the server's time zone and default spoken locale.
type LocaleSettings struct {
	Timezone      string `json:"timezone"`
	DefaultLocale string `json:"default_locale"`
}

// StorageTest is the result of probing the storage backend.
type StorageTest struct {
	OK    bool   `json:"ok"`
	MS    int64  `json:"ms"`
	Error string `json:"error"`
}

// SettingsPatch is a partial settings document. Read-only fields of the settings document are accepted and ignored, so a whole document can be sent back.
type SettingsPatch struct {
	_        struct{}       `additionalProperties:"true"`
	Server   *ServerPatch   `json:"server,omitempty"`
	Auth     *AuthPatch     `json:"auth,omitempty"`
	Storage  *StoragePatch  `json:"storage,omitempty"`
	Features *FeaturesPatch `json:"features,omitempty"`
	Logging  *LoggingPatch  `json:"logging,omitempty"`
	Locale   *LocalePatch   `json:"locale,omitempty"`
}

// ServerPatch changes how Encore is reached.
type ServerPatch struct {
	_        struct{} `additionalProperties:"true"`
	BasePath *string  `json:"base_path,omitempty" maxLength:"128" doc:"A path such as /encore, or / for none"`
}

// AuthPatch changes sign-in settings.
type AuthPatch struct {
	_      struct{}         `additionalProperties:"true"`
	Mode   *string          `json:"mode,omitempty" enum:"local,oidc,header"`
	Local  *LocalAuthPatch  `json:"local,omitempty"`
	OIDC   *OIDCAuthPatch   `json:"oidc,omitempty"`
	Header *HeaderAuthPatch `json:"header,omitempty"`
}

// LocalAuthPatch changes local sign-in settings.
type LocalAuthPatch struct {
	_               struct{} `additionalProperties:"true"`
	SessionHours    *int     `json:"session_hours,omitempty" minimum:"1" maximum:"8760"`
	LockoutAttempts *int     `json:"lockout_attempts,omitempty" minimum:"1" maximum:"100"`
	LockoutMinutes  *int     `json:"lockout_minutes,omitempty" minimum:"1" maximum:"1440"`
}

// OIDCAuthPatch changes OpenID Connect settings.
type OIDCAuthPatch struct {
	_            struct{}  `additionalProperties:"true"`
	Issuer       *string   `json:"issuer,omitempty"`
	ClientID     *string   `json:"client_id,omitempty"`
	ClientSecret *string   `json:"client_secret,omitempty" doc:"Write-only"`
	Scopes       *[]string `json:"scopes,omitempty"`
	AdminGroup   *string   `json:"admin_group,omitempty"`
	GroupsClaim  *string   `json:"groups_claim,omitempty"`
	RedirectURI  *string   `json:"redirect_uri,omitempty" doc:"Empty derives it from the request"`
	BreakGlass   *bool     `json:"break_glass,omitempty" doc:"Keep the local password working in oidc and header modes"`
}

// HeaderAuthPatch changes trusted-header settings.
type HeaderAuthPatch struct {
	_              struct{}  `additionalProperties:"true"`
	TrustedProxies *[]string `json:"trusted_proxies,omitempty"`
	UserHeader     *string   `json:"user_header,omitempty"`
	GroupsHeader   *string   `json:"groups_header,omitempty"`
	AdminGroup     *string   `json:"admin_group,omitempty"`
}

// StoragePatch changes storage settings.
type StoragePatch struct {
	_       struct{}             `additionalProperties:"true"`
	Backend *string              `json:"backend,omitempty" enum:"local,s3,webdav"`
	S3      *S3Patch             `json:"s3,omitempty"`
	WebDAV  *WebDAVPatch         `json:"webdav,omitempty"`
	Local   *LocalStoragePatch   `json:"local,omitempty"`
	Backups *BackupSettingsPatch `json:"backups,omitempty"`
}

// S3Patch changes S3 settings.
type S3Patch struct {
	_                  struct{} `additionalProperties:"true"`
	Endpoint           *string  `json:"endpoint,omitempty"`
	Region             *string  `json:"region,omitempty"`
	Bucket             *string  `json:"bucket,omitempty"`
	Prefix             *string  `json:"prefix,omitempty"`
	Addressing         *string  `json:"addressing,omitempty" enum:"auto,path,virtual"`
	AccessKey          *string  `json:"access_key,omitempty"`
	SecretKey          *string  `json:"secret_key,omitempty" doc:"Write-only"`
	CABundle           *string  `json:"ca_bundle,omitempty"`
	InsecureSkipVerify *bool    `json:"insecure_skip_verify,omitempty"`
	SSE                *string  `json:"sse,omitempty" enum:",s3,kms"`
}

// WebDAVPatch changes WebDAV settings.
type WebDAVPatch struct {
	_        struct{} `additionalProperties:"true"`
	URL      *string  `json:"url,omitempty"`
	Username *string  `json:"username,omitempty"`
	Password *string  `json:"password,omitempty" doc:"Write-only"`
	Prefix   *string  `json:"prefix,omitempty"`
}

// LocalStoragePatch changes the local backend's directory.
type LocalStoragePatch struct {
	_    struct{} `additionalProperties:"true"`
	Path *string  `json:"path,omitempty"`
}

// BackupSettingsPatch changes the backup schedule.
type BackupSettingsPatch struct {
	_        struct{} `additionalProperties:"true"`
	Schedule *string  `json:"schedule,omitempty"`
	Keep     *int     `json:"keep,omitempty" minimum:"1" maximum:"1000"`
	Prefix   *string  `json:"prefix,omitempty" maxLength:"256" doc:"Empty puts backups at the top of the storage location"`
}

// FeaturesPatch toggles optional behaviours.
type FeaturesPatch struct {
	_             struct{} `additionalProperties:"true"`
	DeepSearch    *bool    `json:"deep_search,omitempty"`
	ShutdownQuits *bool    `json:"shutdown_quits,omitempty"`
	ConfirmSound  *bool    `json:"confirm_sound,omitempty"`
	QueueLimit    *int     `json:"queue_limit,omitempty" minimum:"1" maximum:"1000"`
}

// LoggingPatch changes the log level.
type LoggingPatch struct {
	_     struct{} `additionalProperties:"true"`
	Level *string  `json:"level,omitempty" enum:"debug,info,warn,error"`
}

// LocalePatch changes the time zone and default locale.
type LocalePatch struct {
	_             struct{} `additionalProperties:"true"`
	Timezone      *string  `json:"timezone,omitempty"`
	DefaultLocale *string  `json:"default_locale,omitempty"`
}

type changes []Change

// SlotValuesFunc supplies the library titles built into the skill model, keyed by slot type (e.g. AMAZON.Movie). It is the seam for the shared-library runtime; until it is wired in, the model is built without titles.
type SlotValuesFunc func(ctx context.Context) (map[string][]skillmodel.SlotValue, error)

// skillSettings is stored as one setting. Locales live in the alexa.locales config key.
type skillSettings struct {
	Invocation string        `json:"invocation"`
	SkillName  string        `json:"skill_name"`
	Endpoint   SkillEndpoint `json:"endpoint"`
	Privacy    Privacy       `json:"privacy"`
}

// SkillEndpoint is where Alexa sends requests.
type SkillEndpoint struct {
	URL string `json:"url" doc:"The public HTTPS URL of /api/v1/alexa, served with a certificate from a public authority"`
}

// Privacy holds the user's own answers to Amazon's privacy and compliance questions; null until answered.
type Privacy struct {
	UsesPersonalInfo  *bool `json:"uses_personal_info"`
	IsChildDirected   *bool `json:"is_child_directed"`
	IsExportCompliant *bool `json:"is_export_compliant"`
	ContainsAds       *bool `json:"contains_ads"`
	AllowsPurchases   *bool `json:"allows_purchases"`
}

type lwaSettings struct {
	ClientID string `json:"client_id"`
}

type lwaPending struct {
	verifier string
	redirect string
	// page is where the callback sends the browser afterwards: the page that started the authorisation.
	page    string
	expires time.Time
}

type skillService struct {
	a *API

	mu      sync.Mutex
	pending map[string]lwaPending
	deploys map[string]*deployRecord
	order   []string
	running string

	skillID atomic.Pointer[string]
	// simulatingUntil is when the deploy smoke test's window closes, as Unix nanoseconds; requests arriving before then came from Amazon's simulator.
	simulatingUntil atomic.Int64
}

// SkillInfo is the Alexa skill page.
type SkillInfo struct {
	Amazon           AmazonConnection     `json:"amazon"`
	SkillID          string               `json:"skill_id"`
	State            string               `json:"state" enum:"live,not_deployed,deploying,failed"`
	Invocation       string               `json:"invocation" enum:"encore,kodi"`
	InvocationName   string               `json:"invocation_name" doc:"What people say, \"Alexa, ask <invocation_name> to…\", in the first enabled locale"`
	SkillName        string               `json:"skill_name"`
	LocalesAvailable []string             `json:"locales_available"`
	LocalesEnabled   []string             `json:"locales_enabled"`
	Content          []string             `json:"content" enum:"movies,tv,music" doc:"What the skill can play; music includes music videos"`
	Endpoint         SkillEndpointInfo    `json:"endpoint"`
	Privacy          Privacy              `json:"privacy"`
	LastDeploy       Nullable[LastDeploy] `json:"last_deploy"`
	NeedsDeploy      bool                 `json:"needs_deploy"`
	UpdateReason     string               `json:"update_reason" enum:"none,settings,library" doc:"Why needs_deploy is set: the skill's settings changed, or only the library titles in the model"`
	ModelSize        Nullable[ModelSize]  `json:"model_size"`
}

// AmazonConnection is the state of the Login with Amazon connection.
type AmazonConnection struct {
	Connected        bool   `json:"connected"`
	VendorID         string `json:"vendor_id"`
	ProfileClientID  string `json:"profile_client_id"`
	HasClientSecret  bool   `json:"has_client_secret"`
	NeedsReauthorise bool   `json:"needs_reauthorise"`
}

// SkillEndpointInfo is the endpoint as shown.
type SkillEndpointInfo struct {
	URL string `json:"url"`
}

// LastDeploy summarises the most recent deploy.
type LastDeploy struct {
	ID       string         `json:"id"`
	At       time.Time      `json:"at"`
	State    string         `json:"state" enum:"succeeded,failed,in_progress"`
	Hash     string         `json:"hash"`
	Locales  []LocaleDeploy `json:"locales"`
	Warnings []string       `json:"warnings"`
	Error    string         `json:"error"`
}

// LocaleDeploy is one locale's deploy result.
type LocaleDeploy struct {
	Locale    string     `json:"locale"`
	Status    string     `json:"status" enum:"succeeded,failed,in_progress"`
	Errors    []string   `json:"errors"`
	Titles    int        `json:"titles"`
	SizeBytes int        `json:"size_bytes"`
	BuiltAt   *time.Time `json:"built_at"`
}

// ModelSize is the largest locale's interaction model against Amazon's limit.
type ModelSize struct {
	Locale           string `json:"locale"`
	Bytes            int    `json:"bytes"`
	LimitBytes       int    `json:"limit_bytes"`
	TitlesInModel    int    `json:"titles_in_model"`
	TitlesServerOnly int    `json:"titles_server_only"`
}

// SkillSettingsInput replaces the skill's settings.
type SkillSettingsInput struct {
	_              struct{}      `additionalProperties:"true"`
	Invocation     string        `json:"invocation" enum:"encore,kodi" default:"encore" doc:"encore uses each locale's own name for it (\"kodi\" in French); kodi is \"kodi\" in every locale"`
	SkillName      string        `json:"skill_name" maxLength:"50" doc:"Empty uses each locale's default"`
	LocalesEnabled []string      `json:"locales_enabled"`
	Content        []string      `json:"content,omitempty" enum:"movies,tv,music" doc:"What the skill can play; music includes music videos. Omit to keep the current choice."`
	Endpoint       SkillEndpoint `json:"endpoint"`
	Privacy        Privacy       `json:"privacy"`
}

// AuthorizeInfo starts Login with Amazon.
type AuthorizeInfo struct {
	URL        string   `json:"url" doc:"The consent page; empty until the client ID and secret are saved"`
	ReturnURLs []string `json:"return_urls" doc:"Add these to the security profile's Allowed Return URLs"`
}

// secretTokenStore keeps the Login with Amazon token in internal/secrets.
type secretTokenStore struct {
	sec *secrets.Store
}

// statusCache holds the latest probe of each Kodi instance, so listing instances does not wait on every Kodi, and publishes changes to the event stream.
type statusCache struct {
	a *API

	mu        sync.Mutex
	entries   map[int64]statusEntry
	published map[string]string
	// gens counts each instance's forgets, so a probe of a runtime that has since been replaced isn't cached.
	gens map[int64]uint64
	// ctx is run's context while it runs; refreshing holds the instances with a refresh already scheduled.
	ctx        context.Context
	refreshing map[int64]bool
	// afterFunc runs a scheduled refresh: time.AfterFunc, or a test's hand-stepped stand-in.
	afterFunc func(d time.Duration, f func())
}

type statusEntry struct {
	st     app.InstanceStatus
	at     time.Time
	infoAt time.Time
}
