package store

import (
	"os"
	"path/filepath"
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
