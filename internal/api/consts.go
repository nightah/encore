package api

import "time"

// Prefix is where the API is mounted, relative to the base path.
const Prefix = "/api/v1"

// artCacheSeconds lets the browser keep artwork for a day; Kodi's references change when the image does.
const artCacheSeconds = "86400"

// artCSP sandboxes artwork opened on its own, since an SVG from Kodi could otherwise run script on Encore's origin.
const artCSP = "default-src 'none'; style-src 'unsafe-inline'; sandbox"

// clientErrorsPerMinute caps how many browser error reports are logged; a page stuck in an error loop can't flood the log.
const clientErrorsPerMinute = 20

// freshAuth is how recent a sign-in must be to export secrets.
const freshAuth = 15 * time.Minute

const maxImportBytes = 1 << 20

// keepDeploys is how many deploys are kept in memory for GET /skill/deploy/{id}; the last one is also persisted.
const keepDeploys = 10

// Event stream tuning.
const (
	// ringSize events are kept for clients resuming with Last-Event-ID.
	ringSize = 256
	// clientBuffer events may queue for one client; a client that falls further behind is dropped and reconnects.
	clientBuffer = 64
)

const wakeTimeout = 2 * time.Minute

// probeTimeout bounds asking one discovered Kodi for its version, so a host that doesn't answer can't hold up discovery.
const probeTimeout = time.Second

const (
	metaPublic = "public"
)

// endpointCheckEvery is how long an endpoint reachability check is reused.
const endpointCheckEvery = 5 * time.Minute

// recentRequests is how many requests the overview lists; the web interface keeps the same number (RECENT_REQUESTS) as live ones arrive.
const recentRequests = 10

// overridesSetting holds the settings changed in the web interface, as a flat map keyed by config key (storage.s3.bucket), so they mean exactly what the same key means in encore.yml. Secret values are kept in internal/secrets instead, under "config/<key>".
const overridesSetting = "config.overrides"

const certWarnWithin = 14 * 24 * time.Hour

// requestScan caps how many log entries a filtered listing reads. The log keeps 500 by default.
const requestScan = 5000

// Settings keys outside the config overrides.
const (
	localeSetting     = "locale"
	lastBackupSetting = "backups.last_at"
	rotatedAtSetting  = "secrets.rotated_at"
)

// Paths outside the admin session, relative to the base path.
const (
	lwaCallbackPath = Prefix + "/oauth/lwa/callback"
	alexaPath       = Prefix + "/alexa"
	// localReturnURL is the paste fallback's return URL: nothing listens there, the user copies the address the browser ends up at.
	localReturnURL = "http://127.0.0.1:9090/cb"
	lwaPendingTTL  = 15 * time.Minute
)

// The invocation names users can choose. "encore" is each locale's own name for it ("kodi" in French, where "encore" means "again"); "kodi" is the same everywhere.
const (
	invocationEncore = "encore"
	invocationKodi   = "kodi"
)

// simulateGrace keeps the window open briefly after the smoke test, for the session-ended request that follows its launch.
const simulateGrace = 15 * time.Second

// Settings keys and secret IDs for the Alexa skill.
const (
	skillSettingsKey = "skill.settings"
	lwaSettingsKey   = "skill.lwa"
	deployStateKey   = "skill.deploy_state"
	lastDeployKey    = "skill.last_deploy"
	skillIDKey       = "alexa.skill_id"
	reauthKey        = "skill.lwa.needs_reauthorise"

	lwaSecretRef = "skill/lwa/client_secret" //nolint:gosec // a secret's ID in internal/secrets, not a credential
	lwaTokenRef  = "skill/lwa/token"         //nolint:gosec // as above
)

// infoEvery is how often the Kodi version and episode total are asked for again; they rarely change.
const infoEvery = 10 * time.Minute

// playerSettle lets Kodi finish a change (and a burst, such as a seek) before its status is read.
const playerSettle = 250 * time.Millisecond

// portRangeMessage is the 422's message for a Kodi URL whose port is outside 1 to 65535, which is refused before anything is saved.
const portRangeMessage = "the port must be between 1 and 65535"

// The values of Instance.Shutdown.
const (
	shutdownInherit  = "inherit"
	shutdownQuit     = "quit"
	shutdownPowerOff = "power_off"
)
