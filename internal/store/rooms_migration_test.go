package store

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoomAliasKindMigrationOnExistingDatabase upgrades a database created by the first migration, with a room and a spoken name already in it.
func TestRoomAliasKindMigrationOnExistingDatabase(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), FileName)

	raw, err := sql.Open("sqlite", dsn(path, false))
	require.NoError(t, err)

	initial, err := fs.ReadFile(migrationFiles, "migrations/00001_init.sql")
	require.NoError(t, err)

	p, err := goose.NewProvider(goose.DialectSQLite3, raw, fstest.MapFS{"00001_init.sql": {Data: initial}})
	require.NoError(t, err)

	_, err = p.Up(ctx)
	require.NoError(t, err)

	_, err = raw.ExecContext(ctx, `INSERT INTO rooms (id, name, normalised, created_at, updated_at) VALUES (1, 'Lounge', 'lounge', 1, 1)`)
	require.NoError(t, err)
	_, err = raw.ExecContext(ctx, `INSERT INTO room_aliases (room_id, alias, normalised, created_at) VALUES (1, 'TV', 'tv', 1)`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	db, _ := openTest(t, Options{Path: path})
	rooms := db.Rooms()

	aliases, err := rooms.Aliases(ctx, 1)
	require.NoError(t, err)
	require.Len(t, aliases, 1)
	assert.Equal(t, SpokenRoom, aliases[0].Kind, "existing spoken names become room names")

	_, err = rooms.AddAlias(ctx, 1, "projector", SpokenDevice)
	require.NoError(t, err)

	_, err = rooms.AddAlias(ctx, 1, "hall", "speaker")
	require.ErrorIs(t, err, ErrInvalid, "the upgraded column checks its kind like a new one")

	// The migration reverses cleanly.
	fsys, err := fs.Sub(migrationFiles, "migrations")
	require.NoError(t, err)

	down, err := goose.NewProvider(goose.DialectSQLite3, db.write, fsys)
	require.NoError(t, err)

	_, err = down.Down(ctx)
	require.NoError(t, err)

	_, err = down.Up(ctx)
	require.NoError(t, err)

	aliases, err = rooms.Aliases(ctx, 1)
	require.NoError(t, err)
	require.Len(t, aliases, 2)
	assert.Equal(t, []SpokenKind{SpokenRoom, SpokenRoom}, []SpokenKind{aliases[0].Kind, aliases[1].Kind}, "names kept through a round trip start again as room names")
}
