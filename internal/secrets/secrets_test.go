package secrets

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/store"
)

func openDB(t *testing.T, dir string) *store.DB {
	t.Helper()

	db, err := store.Open(t.Context(), store.Options{Path: store.DefaultPath(dir)})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, db.Close()) })

	return db
}

func testKey(t *testing.T) []byte {
	t.Helper()

	k, err := GenerateKey()
	require.NoError(t, err)

	return k
}

func TestPutGetDelete(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)
	ctx := t.Context()

	s, err := Open(ctx, db.SecretRows(), Options{Key: EncodeKey(testKey(t))})
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.Put(ctx, "kodi/1/password", []byte("hunter2")))

	got, err := s.Get(ctx, "kodi/1/password")
	require.NoError(t, err)
	assert.Equal(t, "hunter2", string(got))

	row, err := db.SecretRows().Get(ctx, "kodi/1/password")
	require.NoError(t, err)
	assert.Equal(t, s.KeyID(), row.KeyID)
	assert.NotContains(t, string(row.Ciphertext), "hunter2")

	require.NoError(t, s.Put(ctx, "kodi/1/password", []byte("correct horse")))
	row2, err := db.SecretRows().Get(ctx, "kodi/1/password")
	require.NoError(t, err)
	assert.NotEqual(t, row.Nonce, row2.Nonce, "every seal uses a fresh nonce")

	require.NoError(t, s.Delete(ctx, "kodi/1/password"))
	_, err = s.Get(ctx, "kodi/1/password")
	require.ErrorIs(t, err, ErrNotFound)

	require.Error(t, s.Put(ctx, "", []byte("x")))
}

func TestCiphertextBoundToID(t *testing.T) {
	db := openDB(t, t.TempDir())
	ctx := t.Context()

	s, err := Open(ctx, db.SecretRows(), Options{Key: EncodeKey(testKey(t))})
	require.NoError(t, err)

	require.NoError(t, s.Put(ctx, "a", []byte("alpha")))

	row, err := db.SecretRows().Get(ctx, "a")
	require.NoError(t, err)

	row.ID = "b"
	require.NoError(t, db.SecretRows().Put(ctx, row))

	_, err = s.Get(ctx, "b")
	require.ErrorIs(t, err, ErrDecrypt, "a row copied to another ID must not open")

	row.ID = "a"
	row.Ciphertext[0] ^= 0xff
	require.NoError(t, db.SecretRows().Put(ctx, row))

	_, err = s.Get(ctx, "a")
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestWrongKey(t *testing.T) {
	db := openDB(t, t.TempDir())
	ctx := t.Context()

	s1, err := Open(ctx, db.SecretRows(), Options{Key: EncodeKey(testKey(t))})
	require.NoError(t, err)
	require.NoError(t, s1.Put(ctx, "a", []byte("alpha")))

	var logs bytes.Buffer

	s2, err := Open(ctx, db.SecretRows(), Options{Key: EncodeKey(testKey(t)), Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "cannot be read")

	_, err = s2.Get(ctx, "a")
	require.ErrorIs(t, err, ErrUnknownKey)
}

func TestKeyFromFile(t *testing.T) {
	k := testKey(t)
	path := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(path, []byte(EncodeKey(k)+"\n"), 0o600))

	db := openDB(t, t.TempDir())

	s, err := Open(t.Context(), db.SecretRows(), Options{KeyFile: path})
	require.NoError(t, err)
	assert.Equal(t, KeyID(k), s.KeyID())

	_, err = Open(t.Context(), db.SecretRows(), Options{KeyFile: filepath.Join(t.TempDir(), "missing")})
	require.Error(t, err)
}

func TestInvalidKeyNotEchoed(t *testing.T) {
	db := openDB(t, t.TempDir())

	_, err := Open(t.Context(), db.SecretRows(), Options{Key: "c3VwZXJzZWNyZXQ="})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "c3VwZXJzZWNyZXQ=")
}

func TestGeneratedKey(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)
	ctx := t.Context()

	var logs bytes.Buffer

	log := slog.New(slog.NewTextHandler(&logs, nil))

	s, err := Open(ctx, db.SecretRows(), Options{DataDir: dir, Logger: log})
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "level=WARN")
	assert.Contains(t, logs.String(), "next to the database")

	info, err := os.Stat(filepath.Join(dir, KeyFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	require.NoError(t, s.Put(ctx, "a", []byte("alpha")))
	require.NoError(t, s.Close())

	s2, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	assert.Equal(t, s.KeyID(), s2.KeyID(), "the generated key is reused")

	got, err := s2.Get(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, "alpha", string(got))

	require.NoError(t, os.Remove(filepath.Join(dir, KeyFileName)))

	_, err = Open(ctx, db.SecretRows(), Options{DataDir: dir})
	require.ErrorContains(t, err, "missing", "a lost key file must not be silently replaced")
}

func TestRotate(t *testing.T) {
	db := openDB(t, t.TempDir())
	ctx := t.Context()

	oldKey := testKey(t)
	oldEncoded := EncodeKey(oldKey)

	s, err := Open(ctx, db.SecretRows(), Options{Key: oldEncoded})
	require.NoError(t, err)

	for _, id := range []string{"a", "b", "c"} {
		require.NoError(t, s.Put(ctx, id, []byte("value-"+id)))
	}

	oldID := s.KeyID()

	newKey := testKey(t)
	newEncoded := EncodeKey(newKey)
	require.NoError(t, s.Rotate(ctx, newKey))
	assert.NotEqual(t, oldID, s.KeyID())

	ids, err := db.SecretRows().KeyIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{s.KeyID()}, ids)

	for _, id := range []string{"a", "b", "c"} {
		got, err := s.Get(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, "value-"+id, string(got))
	}

	// After a restart with the new key everything still opens.
	s2, err := Open(ctx, db.SecretRows(), Options{Key: newEncoded})
	require.NoError(t, err)

	got, err := s2.Get(ctx, "b")
	require.NoError(t, err)
	assert.Equal(t, "value-b", string(got))

	require.Error(t, s2.Rotate(ctx, mustParse(t, newEncoded)), "rotating to the current key is refused")
	require.Error(t, s2.Rotate(ctx, []byte("short")))
}

func TestRotateKeepsOldRowsReadable(t *testing.T) {
	db := openDB(t, t.TempDir())
	ctx := t.Context()

	s, err := Open(ctx, db.SecretRows(), Options{Key: EncodeKey(testKey(t))})
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, "a", []byte("alpha")))

	stale, err := db.SecretRows().Get(ctx, "a")
	require.NoError(t, err)

	require.NoError(t, s.Rotate(ctx, testKey(t)))

	// A row sealed under the previous key (e.g. read from a snapshot taken mid-rotation, or restored) still opens.
	require.NoError(t, db.SecretRows().Put(ctx, stale))

	got, err := s.Get(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, "alpha", string(got))
}

func TestRotateFailureIsAtomic(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)
	ctx := t.Context()

	s, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, "a", []byte("alpha")))
	require.NoError(t, s.Put(ctx, "b", []byte("bravo")))

	// Corrupt one row so rotation fails part way through.
	row, err := db.SecretRows().Get(ctx, "b")
	require.NoError(t, err)
	row.Ciphertext[0] ^= 0xff
	require.NoError(t, db.SecretRows().Put(ctx, row))

	before := s.KeyID()
	keyBefore, err := os.ReadFile(filepath.Join(dir, KeyFileName))
	require.NoError(t, err)

	require.ErrorIs(t, s.Rotate(ctx, testKey(t)), ErrDecrypt)
	assert.Equal(t, before, s.KeyID())

	ids, err := db.SecretRows().KeyIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{before}, ids)

	keyAfter, err := os.ReadFile(filepath.Join(dir, KeyFileName))
	require.NoError(t, err)
	assert.Equal(t, keyBefore, keyAfter)
	assert.NoFileExists(t, filepath.Join(dir, KeyFileName+".new"))
}

func TestRotateManagedFile(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)
	ctx := t.Context()

	s, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, "a", []byte("alpha")))

	newKey := testKey(t)
	newID := KeyID(newKey)
	require.NoError(t, s.Rotate(ctx, newKey))

	s2, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	assert.Equal(t, newID, s2.KeyID(), "the key file is replaced")

	got, err := s2.Get(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, "alpha", string(got))
}

func TestRecoverInterruptedRotation(t *testing.T) {
	ctx := t.Context()

	setup := func(t *testing.T) (dir string, db *store.DB, s *Store) {
		t.Helper()

		dir = t.TempDir()
		db = openDB(t, dir)

		s, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
		require.NoError(t, err)
		require.NoError(t, s.Put(ctx, "a", []byte("alpha")))

		return dir, db, s
	}

	t.Run("committed", func(t *testing.T) {
		dir, db, s := setup(t)

		// Simulate a crash after the database commit but before the rename.
		keyFile := filepath.Join(dir, KeyFileName)
		oldContent, err := os.ReadFile(keyFile)
		require.NoError(t, err)

		newKey := testKey(t)
		newID := KeyID(newKey)
		require.NoError(t, s.Rotate(ctx, mustParse(t, EncodeKey(newKey))))
		require.NoError(t, os.WriteFile(keyFile, oldContent, 0o600))
		require.NoError(t, writeKeyFile(keyFile+".new", newKey))

		s2, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
		require.NoError(t, err)
		assert.Equal(t, newID, s2.KeyID())
		assert.NoFileExists(t, keyFile+".new")

		got, err := s2.Get(ctx, "a")
		require.NoError(t, err)
		assert.Equal(t, "alpha", string(got))
	})

	t.Run("uncommitted", func(t *testing.T) {
		dir, db, s := setup(t)

		keyFile := filepath.Join(dir, KeyFileName)
		require.NoError(t, writeKeyFile(keyFile+".new", testKey(t)))

		s2, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
		require.NoError(t, err)
		assert.Equal(t, s.KeyID(), s2.KeyID())
		assert.NoFileExists(t, keyFile+".new")
	})
}

func TestCloseClearsKeys(t *testing.T) {
	db := openDB(t, t.TempDir())

	k := testKey(t)
	s, err := Open(t.Context(), db.SecretRows(), Options{Key: EncodeKey(k)})
	require.NoError(t, err)

	e, err := s.key(s.KeyID())
	require.NoError(t, err)

	raw := e.raw
	require.NoError(t, s.Close())
	assert.Equal(t, make([]byte, KeySize), raw)

	_, err = s.Get(context.Background(), "a")
	require.Error(t, err)
}

func TestParseKey(t *testing.T) {
	k := testKey(t)

	got, err := ParseKey("  " + EncodeKey(k) + "\n")
	require.NoError(t, err)
	assert.Equal(t, k, got)

	for _, bad := range []string{"", "not base64!", EncodeKey(k[:16]), strings.Repeat("A", 100)} {
		_, err := ParseKey(bad)
		require.Error(t, err, bad)
	}

	assert.True(t, strings.HasPrefix(KeyID(k), "v1-"))
	assert.NotEqual(t, KeyID(k), KeyID(testKey(t)))
}

func mustParse(t *testing.T, s string) []byte {
	t.Helper()

	k, err := ParseKey(s)
	require.NoError(t, err)

	return k
}

func TestReadKeyNeverGenerates(t *testing.T) {
	dir := t.TempDir()

	_, err := ReadKey(Options{DataDir: dir})
	require.ErrorIs(t, err, ErrNoKey)
	assert.NoFileExists(t, filepath.Join(dir, KeyFileName))

	k := testKey(t)

	got, err := ReadKey(Options{Key: EncodeKey(k), DataDir: dir})
	require.NoError(t, err)
	assert.Equal(t, k, got)

	db := openDB(t, dir)
	s, err := Open(t.Context(), db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	require.NoError(t, s.Put(t.Context(), "a", []byte("x")))

	got, err = ReadKey(Options{DataDir: dir})
	require.NoError(t, err)
	assert.Equal(t, s.KeyID(), KeyID(got), "the generated key file is read")

	_, err = ReadKey(Options{Key: "short"})
	require.Error(t, err)
}

func TestOpenWithKeyCanOpen(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)
	ctx := t.Context()
	k := testKey(t)

	s, err := Open(ctx, db.SecretRows(), Options{Key: EncodeKey(k)})
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, "kodi/1/password", []byte("hunter2")))

	row, err := db.SecretRows().Get(ctx, "kodi/1/password")
	require.NoError(t, err)

	right, err := OpenWithKey(db.SecretRows(), bytes.Clone(k))
	require.NoError(t, err)
	require.NoError(t, right.CanOpen([]store.SecretRow{row}))

	pt, err := right.Get(ctx, "kodi/1/password")
	require.NoError(t, err)
	assert.Equal(t, "hunter2", string(pt))

	wrong, err := OpenWithKey(nil, testKey(t))
	require.NoError(t, err)
	err = wrong.CanOpen([]store.SecretRow{row})
	require.ErrorIs(t, err, ErrUnknownKey)
	assert.NotContains(t, err.Error(), "hunter2")

	none, err := OpenWithKey(nil, nil)
	require.NoError(t, err)
	require.ErrorIs(t, none.CanOpen([]store.SecretRow{row}), ErrUnknownKey)
	require.NoError(t, none.CanOpen(nil))

	assert.NoFileExists(t, filepath.Join(dir, KeyFileName), "OpenWithKey writes nothing")
}
