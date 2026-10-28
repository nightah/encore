package store

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectFile(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()

	require.NoError(t, db.SecretRows().Put(ctx, SecretRow{ID: "a", KeyID: "v1-x", Nonce: []byte{1}, Ciphertext: []byte{2}}))

	copyPath := filepath.Join(t.TempDir(), "copy.db")
	require.NoError(t, db.BackupTo(ctx, copyPath))

	before, err := os.ReadFile(copyPath)
	require.NoError(t, err)

	info, err := InspectFile(ctx, copyPath)
	require.NoError(t, err)

	latest, err := SchemaVersion()
	require.NoError(t, err)
	assert.Equal(t, latest, info.SchemaVersion)
	require.Len(t, info.Secrets, 1)
	assert.Equal(t, "v1-x", info.Secrets[0].KeyID)

	after, err := os.ReadFile(copyPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "inspecting changes nothing")
	assert.NoFileExists(t, copyPath+"-wal")

	empty := filepath.Join(t.TempDir(), "empty.db")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))

	info, err = InspectFile(ctx, empty)
	require.NoError(t, err, "an empty file is a valid, empty SQLite database")
	assert.Zero(t, info.SchemaVersion)

	junk := filepath.Join(t.TempDir(), "junk.db")
	require.NoError(t, os.WriteFile(junk, make([]byte, 8192), 0o600))

	_, err = InspectFile(ctx, junk)
	require.Error(t, err)
}

func TestInspectFileCorruptDatabase(t *testing.T) {
	db, _ := openTest(t, Options{})
	ctx := t.Context()

	for i := range 200 {
		require.NoError(t, db.Settings().Set(ctx, fmt.Sprintf("key-%03d", i), strings.Repeat("x", 200)))
	}

	path := filepath.Join(t.TempDir(), "copy.db")
	require.NoError(t, db.BackupTo(ctx, path))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Greater(t, len(raw), 8*4096)

	// Scribble over the last pages, where the settings rows live, leaving the header and schema intact.
	for i := len(raw) - 3*4096; i < len(raw); i += 7 {
		raw[i] ^= 0x5a
	}

	require.NoError(t, os.WriteFile(path, raw, 0o600))

	_, err = InspectFile(ctx, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), path, "the error names the file")
}

func TestSchemaVersionMatchesMigrations(t *testing.T) {
	v, err := SchemaVersion()
	require.NoError(t, err)

	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	require.NoError(t, err)
	require.NotEmpty(t, names)

	newest := slices.Max(names)
	assert.True(t, strings.HasPrefix(filepath.Base(newest), fmt.Sprintf("%05d_", v)), "%d is the newest of %v", v, names)

	db, _ := openTest(t, Options{})

	var applied int64
	require.NoError(t, db.read.QueryRowContext(t.Context(), `SELECT MAX(version_id) FROM goose_db_version`).Scan(&applied))
	assert.Equal(t, v, applied, "Open migrates to the newest version")
}
