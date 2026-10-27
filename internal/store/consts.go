package store

import (
	"time"
)

// FileName is the database file's name within the data directory.
const FileName = "encore.db"

const aliasColumns = `id, kodi_instance_id, media_kind, media_id, alias, normalised, created_at`

const deviceColumns = `id, alexa_device_id, display_name, room_id, kodi_instance_id, first_seen, last_seen, request_count`

const kodiColumns = `id, name, scheme, host, port, path, username, password_secret_ref, ws_port, mac, broadcast_addr, is_default_for_unlinked, language, timezone, shutdown_quits, library_group_id, created_at, updated_at`

// Library group states.
const (
	// GroupSolo is a group of one instance.
	GroupSolo LibraryGroupState = "solo"
	// GroupLikelyShared means sampled library items agree across the members.
	GroupLikelyShared LibraryGroupState = "likely_shared"
	// GroupConfirmedShared means a playback change on one member was seen on the others.
	GroupConfirmedShared LibraryGroupState = "confirmed_shared"
	// GroupManual means the user or the config file grouped the members.
	GroupManual LibraryGroupState = "manual"
)

const libraryGroupColumns = `id, name, source_instance_id, state, manual, evidence, created_at, updated_at`

// Request log retention defaults.
const (
	DefaultRequestLogMaxEntries = 500
	DefaultRequestLogMaxAge     = 7 * 24 * time.Hour
)

const requestLogColumns = `id, ts, request_id, device_id, locale, intent, slots, match_info, kodi_calls, timing, speech, outcome, duration_ms, simulated`

const roomColumns = `id, name, kodi_instance_id, created_at, updated_at`

const secretColumns = `id, key_id, nonce, ciphertext, created_at, updated_at` //nolint:gosec // column names, not a credential

const sessionColumns = `id_hash, username, created_at, expires_at, last_seen_at`
