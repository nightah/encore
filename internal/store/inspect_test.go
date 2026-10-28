package store

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/testkit"
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

func TestInspectFileReportsASingleIntegrityProblem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "check.db")

	raw, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)

	for _, stmt := range []string{
		`CREATE TABLE t (a INTEGER)`,
		`INSERT INTO t VALUES (1), (9)`,
		`PRAGMA writable_schema = ON`,
		`UPDATE sqlite_schema SET sql = 'CREATE TABLE t (a INTEGER CHECK (a < 5))' WHERE name = 't'`,
	} {
		_, err = raw.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}

	require.NoError(t, raw.Close())

	_, err = InspectFile(t.Context(), path)
	require.ErrorContains(t, err, "failed its integrity check", "one problem is as fatal as many")
	assert.Contains(t, err.Error(), "CHECK constraint failed")
}

// sqliteFile makes a database file at path from stmts, without Encore's schema.
func sqliteFile(t *testing.T, path string, stmts ...string) {
	t.Helper()

	raw, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)

	for _, stmt := range stmts {
		_, err = raw.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}

	require.NoError(t, raw.Close())
}

func TestInspectFileReportsTablesItCannotRead(t *testing.T) {
	testCases := []struct {
		name  string
		stmts []string
		want  string
	}{
		{
			name:  "a schema version table without versions",
			stmts: []string{`CREATE TABLE goose_db_version (id INTEGER PRIMARY KEY)`},
			want:  "reading schema version: ",
		},
		{
			name:  "a secrets table without Encore's columns",
			stmts: []string{`CREATE TABLE goose_db_version (version_id INTEGER)`, `INSERT INTO goose_db_version VALUES (1)`, `CREATE TABLE secrets (id TEXT)`},
			want:  "reading secrets: ",
		},
		{
			name:  "secrets with columns of the wrong type",
			stmts: []string{`CREATE TABLE secrets (id TEXT, key_id TEXT, nonce BLOB, ciphertext BLOB, created_at TEXT, updated_at INTEGER)`, `INSERT INTO secrets VALUES ('a', 'k', x'00', x'00', 'yesterday', 0)`},
			want:  "reading secrets: ",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "crafted.db")
			sqliteFile(t, path, tc.stmts...)

			_, err := InspectFile(t.Context(), path)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestInspectFileReportsSchemaReadFailures(t *testing.T) {
	db, _ := openTest(t, Options{})

	path := filepath.Join(t.TempDir(), "copy.db")
	require.NoError(t, db.BackupTo(t.Context(), path))

	for nth := 1; nth <= 2; nth++ {
		t.Run(fmt.Sprintf("read %d", nth), func(t *testing.T) {
			faults := testkit.NewFaults(t, path)
			faults.Inject(testkit.Fault{Op: testkit.Read, Table: "sqlite_master", Nth: nth})

			_, err := InspectFile(t.Context(), path)
			require.ErrorContains(t, err, "reading schema: "+testkit.ReadFaultMessage)
		})
	}

	_, err := InspectFile(t.Context(), path)
	require.NoError(t, err, "the file itself was sound")
}
