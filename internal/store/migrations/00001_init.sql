-- Initial schema. Timestamps are Unix milliseconds (UTC); booleans are 0/1.

-- +goose Up
CREATE TABLE kodi_instances (
    id                      INTEGER PRIMARY KEY,
    name                    TEXT    NOT NULL CHECK (name <> ''),
    scheme                  TEXT    NOT NULL DEFAULT 'http' CHECK (scheme IN ('http', 'https')),
    host                    TEXT    NOT NULL CHECK (host <> ''),
    port                    INTEGER NOT NULL DEFAULT 8080 CHECK (port BETWEEN 1 AND 65535),
    path                    TEXT    NOT NULL DEFAULT '/jsonrpc',
    username                TEXT    NOT NULL DEFAULT '',
    password_secret_ref     TEXT    NOT NULL DEFAULT '',
    ws_port                 INTEGER NOT NULL DEFAULT 9090 CHECK (ws_port BETWEEN 0 AND 65535),
    mac                     TEXT    NOT NULL DEFAULT '',
    broadcast_addr          TEXT    NOT NULL DEFAULT '',
    is_default_for_unlinked INTEGER NOT NULL DEFAULT 0 CHECK (is_default_for_unlinked IN (0, 1)),
    language                TEXT    NOT NULL DEFAULT '',
    timezone                TEXT    NOT NULL DEFAULT '',
    shutdown_quits          INTEGER CHECK (shutdown_quits IN (0, 1)),
    created_at              INTEGER NOT NULL,
    updated_at              INTEGER NOT NULL,
    library_group_id        INTEGER REFERENCES library_groups (id) ON DELETE SET NULL
) STRICT;

CREATE UNIQUE INDEX kodi_instances_name ON kodi_instances (name COLLATE NOCASE);
CREATE INDEX kodi_instances_library_group ON kodi_instances (library_group_id);

-- At most one instance answers devices that are not linked to one.
CREATE UNIQUE INDEX kodi_instances_one_default ON kodi_instances (is_default_for_unlinked) WHERE is_default_for_unlinked = 1;

-- Library groups: Kodi instances that share one library database (MySQL/MariaDB) and so need only one catalog and title index. Every instance belongs to a group; an ungrouped instance is a group of one.
CREATE TABLE library_groups (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT    NOT NULL DEFAULT '',
    -- The member that runs library syncs; NULL until one is chosen.
    source_instance_id INTEGER REFERENCES kodi_instances (id) ON DELETE SET NULL,
    state              TEXT    NOT NULL DEFAULT 'solo' CHECK (state IN ('solo', 'likely_shared', 'confirmed_shared', 'manual')),
    -- A user or config choice, which detection never overrides.
    manual             INTEGER NOT NULL DEFAULT 0 CHECK (manual IN (0, 1)),
    -- What detection found: fingerprint agreement, confirmations, split reasons.
    evidence           TEXT    CHECK (evidence IS NULL OR json_valid(evidence)),
    created_at         INTEGER NOT NULL,
    updated_at         INTEGER NOT NULL
) STRICT;

CREATE TABLE rooms (
    id               INTEGER PRIMARY KEY,
    name             TEXT    NOT NULL CHECK (name <> ''),
    normalised       TEXT    NOT NULL UNIQUE CHECK (normalised <> ''),
    kodi_instance_id INTEGER REFERENCES kodi_instances (id) ON DELETE SET NULL,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
) STRICT;

CREATE TABLE room_aliases (
    id         INTEGER PRIMARY KEY,
    room_id    INTEGER NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    alias      TEXT    NOT NULL,
    normalised TEXT    NOT NULL UNIQUE CHECK (normalised <> ''),
    created_at INTEGER NOT NULL
) STRICT;

CREATE INDEX room_aliases_room ON room_aliases (room_id);

CREATE TABLE echo_devices (
    id               INTEGER PRIMARY KEY,
    alexa_device_id  TEXT    NOT NULL UNIQUE CHECK (alexa_device_id <> ''),
    display_name     TEXT    NOT NULL DEFAULT '',
    room_id          INTEGER REFERENCES rooms (id) ON DELETE SET NULL,
    kodi_instance_id INTEGER REFERENCES kodi_instances (id) ON DELETE SET NULL,
    first_seen       INTEGER NOT NULL,
    last_seen        INTEGER NOT NULL,
    request_count    INTEGER NOT NULL DEFAULT 0
) STRICT;

CREATE INDEX echo_devices_room ON echo_devices (room_id);
CREATE INDEX echo_devices_kodi ON echo_devices (kodi_instance_id);

CREATE TABLE settings (
    key        TEXT    PRIMARY KEY,
    value      TEXT    NOT NULL CHECK (json_valid(value)),
    updated_at INTEGER NOT NULL
) STRICT;

-- User-defined spoken aliases for library titles, consulted by the matcher.
CREATE TABLE aliases (
    id               INTEGER PRIMARY KEY,
    kodi_instance_id INTEGER NOT NULL REFERENCES kodi_instances (id) ON DELETE CASCADE,
    media_kind       TEXT    NOT NULL CHECK (media_kind <> ''),
    media_id         INTEGER NOT NULL,
    alias            TEXT    NOT NULL,
    normalised       TEXT    NOT NULL CHECK (normalised <> ''),
    created_at       INTEGER NOT NULL,
    UNIQUE (kodi_instance_id, normalised, media_kind, media_id)
) STRICT;

CREATE INDEX aliases_media ON aliases (kodi_instance_id, media_kind, media_id);

CREATE TABLE request_log (
    id          INTEGER PRIMARY KEY,
    ts          INTEGER NOT NULL,
    request_id  TEXT    NOT NULL DEFAULT '',
    device_id   TEXT    NOT NULL DEFAULT '',
    locale      TEXT    NOT NULL DEFAULT '',
    intent      TEXT    NOT NULL DEFAULT '',
    slots       TEXT    CHECK (slots IS NULL OR json_valid(slots)),
    match_info  TEXT    CHECK (match_info IS NULL OR json_valid(match_info)),
    kodi_calls  TEXT    CHECK (kodi_calls IS NULL OR json_valid(kodi_calls)),
    speech      TEXT    NOT NULL DEFAULT '',
    outcome     TEXT    NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    -- How long each phase took: signature check, title matching and calls to Kodi.
    timing      TEXT    CHECK (timing IS NULL OR json_valid(timing)),
    -- Sent by Amazon's simulator during Encore's own deploy smoke test, not by an Echo.
    simulated   INTEGER NOT NULL DEFAULT 0 CHECK (simulated IN (0, 1))
) STRICT;

CREATE INDEX request_log_ts ON request_log (ts);

-- Admin web sessions. Only a hash of the session token is stored.
CREATE TABLE sessions (
    id_hash      BLOB    PRIMARY KEY,
    username     TEXT    NOT NULL,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

CREATE INDEX sessions_expires ON sessions (expires_at);
CREATE INDEX sessions_username ON sessions (username);

-- Encrypted secrets, managed by internal/secrets.
CREATE TABLE secrets (
    id         TEXT    PRIMARY KEY CHECK (id <> ''),
    key_id     TEXT    NOT NULL,
    nonce      BLOB    NOT NULL,
    ciphertext BLOB    NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

-- +goose Down
DROP TABLE secrets;
DROP TABLE sessions;
DROP TABLE request_log;
DROP TABLE aliases;
DROP TABLE settings;
DROP TABLE echo_devices;
DROP TABLE room_aliases;
DROP TABLE rooms;
DROP TABLE kodi_instances;
DROP TABLE library_groups;
