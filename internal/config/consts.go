package config

import (
	"time"
)

// DefaultFileName is the config file Encore looks for in the data directory when --config isn't given.
const DefaultFileName = "encore.yml"

// EnvPrefix is the prefix for environment variable overrides.
const EnvPrefix = "ENCORE_"

// DataDirDefaultDoc describes DefaultDataDir for the configuration reference, which can't show one machine's path.
const DataDirDefaultDoc = "per user: `$XDG_DATA_HOME/encore` (normally `~/.local/share/encore`) on Linux and other Unix systems, `~/Library/Application Support/encore` on macOS, `%LocalAppData%\\encore` on Windows; the container image and the systemd unit set their own"

// fallbackDataDir is used when the user's home directory can't be found, e.g. a service account without one: a directory beside wherever Encore was started.
const fallbackDataDir = "encore-data"

// The groups alexa.content can list.
const (
	ContentMovies = "movies"
	ContentTV     = "tv"
	ContentMusic  = "music"
)

// DefaultQueueLimit is alexa.queue_limit's default, as in Kanzi.
const DefaultQueueLimit = 100

// Admin authentication modes.
const (
	AuthModeLocal  = "local"
	AuthModeOIDC   = "oidc"
	AuthModeHeader = "header"
)

// Storage backends.
const (
	BackendLocal  = "local"
	BackendS3     = "s3"
	BackendWebDAV = "webdav"
)

// S3 addressing styles.
const (
	AddressingAuto    = "auto"
	AddressingPath    = "path"
	AddressingVirtual = "virtual"
)

// S3 server-side encryption modes.
const (
	SSENone = ""
	SSES3   = "s3"
	SSEKMS  = "kms"
)

const (
	scheduleOff scheduleKind = iota
	scheduleHourly
	scheduleDaily
	scheduleWeekly
	scheduleEvery
)

// minEvery stops a typo such as every@1s from hammering the storage backend.
const minEvery = time.Minute

// LibraryGroupNone as a kodi instance's library_group keeps it out of every library group, even when it looks like it shares a library database.
const LibraryGroupNone = "none"
