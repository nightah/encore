package secrets

import (
	"bytes"
	"context"
	"encoding/hex"
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

	return GenerateKey()
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

	require.ErrorIs(t, none.Put(ctx, "a", []byte("x")), ErrUnknownKey, "a store without a key cannot seal")

	_, err = OpenWithKey(nil, []byte("short"))
	require.Error(t, err)
}

// faultyRepo wraps a real repository and fails the operations whose error is set.
type faultyRepo struct {
	Repo

	putErr, keyIDsErr, rewriteErr error

	// beforePut, when set, runs inside Put before the row is written; beforeRewrite runs inside Rewrite before any row is.
	beforePut, beforeRewrite func()
}

func (r *faultyRepo) Put(ctx context.Context, row store.SecretRow) error {
	if r.putErr != nil {
		return r.putErr
	}

	if r.beforePut != nil {
		r.beforePut()
	}

	return r.Repo.Put(ctx, row)
}

func (r *faultyRepo) KeyIDs(ctx context.Context) ([]string, error) {
	if r.keyIDsErr != nil {
		return nil, r.keyIDsErr
	}

	return r.Repo.KeyIDs(ctx)
}

func (r *faultyRepo) Rewrite(ctx context.Context, fn func(row *store.SecretRow) error) error {
	if r.rewriteErr != nil {
		return r.rewriteErr
	}

	if r.beforeRewrite != nil {
		r.beforeRewrite()
	}

	return r.Repo.Rewrite(ctx, fn)
}

func TestOpenOptionErrors(t *testing.T) {
	db := openDB(t, t.TempDir())

	badFile := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(badFile, []byte("hunter2"), 0o600))

	notADir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notADir, nil, 0o600))

	for name, tt := range map[string]struct {
		opts Options
		want string
	}{
		"nothing configured": {Options{}, "no key configured"},
		"invalid key":        {Options{Key: "hunter2"}, "secrets.key:"},
		"invalid key file":   {Options{KeyFile: badFile}, "secrets.key_file:"},
		"missing key file":   {Options{KeyFile: filepath.Join(t.TempDir(), "missing")}, "secrets.key_file:"},
		"data dir is a file": {Options{DataDir: notADir}, "checking key file"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Open(t.Context(), db.SecretRows(), tt.opts)
			require.ErrorContains(t, err, tt.want)
			assert.NotContains(t, err.Error(), "hunter2")
		})
	}
}

func TestOpenKeyTakesPrecedenceOverKeyFile(t *testing.T) {
	db := openDB(t, t.TempDir())
	k, other := testKey(t), testKey(t)

	path := filepath.Join(t.TempDir(), "key")
	require.NoError(t, os.WriteFile(path, []byte(EncodeKey(other)), 0o600))

	s, err := Open(t.Context(), db.SecretRows(), Options{Key: EncodeKey(k), KeyFile: path})
	require.NoError(t, err)
	assert.Equal(t, KeyID(k), s.KeyID())

	got, err := ReadKey(Options{KeyFile: path})
	require.NoError(t, err)
	assert.Equal(t, other, got)
}

func TestOpenWarnsAboutAReadableKeyFile(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)

	_, err := Open(t.Context(), db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	require.NoError(t, os.Chmod(filepath.Join(dir, KeyFileName), 0o644))

	var logs bytes.Buffer

	_, err = Open(t.Context(), db.SecretRows(), Options{DataDir: dir, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	require.NoError(t, err)
	assert.Contains(t, logs.String(), "readable by other users")
}

func TestOpenWithUnwritableDataDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	db := openDB(t, t.TempDir())

	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, err := Open(t.Context(), db.SecretRows(), Options{DataDir: dir})
	require.ErrorContains(t, err, "creating key file")
}

func TestOpenReportsRepositoryErrors(t *testing.T) {
	ctx := t.Context()

	t.Run("generating a key", func(t *testing.T) {
		dir := t.TempDir()
		repo := &faultyRepo{Repo: openDB(t, dir).SecretRows(), keyIDsErr: assert.AnError}

		_, err := Open(ctx, repo, Options{DataDir: dir})
		require.ErrorIs(t, err, assert.AnError)
		assert.NoFileExists(t, filepath.Join(dir, KeyFileName), "no key is generated when existing secrets cannot be checked")
	})

	t.Run("checking key usage", func(t *testing.T) {
		repo := &faultyRepo{Repo: openDB(t, t.TempDir()).SecretRows(), keyIDsErr: assert.AnError}

		_, err := Open(ctx, repo, Options{Key: EncodeKey(testKey(t))})
		require.ErrorIs(t, err, assert.AnError)
	})

	t.Run("recovering a rotation", func(t *testing.T) {
		dir := t.TempDir()
		db := openDB(t, dir)

		_, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
		require.NoError(t, err)
		require.NoError(t, writeKeyFile(filepath.Join(dir, KeyFileName+".new"), testKey(t)))

		_, err = Open(ctx, &faultyRepo{Repo: db.SecretRows(), keyIDsErr: assert.AnError}, Options{DataDir: dir})
		require.ErrorIs(t, err, assert.AnError)
		assert.FileExists(t, filepath.Join(dir, KeyFileName+".new"), "the pending key is kept until it can be checked")
	})
}

func TestOpenRefusesACorruptPendingKey(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)

	_, err := Open(t.Context(), db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, KeyFileName+".new"), []byte("garbage"), 0o600))

	_, err = Open(t.Context(), db.SecretRows(), Options{DataDir: dir})
	require.ErrorContains(t, err, KeyFileName+".new")
}

func TestPutReportsRepositoryErrors(t *testing.T) {
	repo := &faultyRepo{Repo: openDB(t, t.TempDir()).SecretRows()}

	s, err := Open(t.Context(), repo, Options{Key: EncodeKey(testKey(t))})
	require.NoError(t, err)

	repo.putErr = assert.AnError
	err = s.Put(t.Context(), "kodi/1/password", []byte("hunter2"))
	require.ErrorIs(t, err, assert.AnError)
	assert.Contains(t, err.Error(), "kodi/1/password")
	assert.NotContains(t, err.Error(), "hunter2")
}

func TestGetRejectsAMalformedNonce(t *testing.T) {
	db := openDB(t, t.TempDir())

	s, err := Open(t.Context(), db.SecretRows(), Options{Key: EncodeKey(testKey(t))})
	require.NoError(t, err)
	require.NoError(t, s.Put(t.Context(), "a", []byte("alpha")))

	row, err := db.SecretRows().Get(t.Context(), "a")
	require.NoError(t, err)

	row.Nonce = row.Nonce[:4]
	require.NoError(t, db.SecretRows().Put(t.Context(), row))

	_, err = s.Get(t.Context(), "a")
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestRotateRepositoryFailureLeavesEverythingAsItWas(t *testing.T) {
	dir := t.TempDir()
	repo := &faultyRepo{Repo: openDB(t, dir).SecretRows()}

	s, err := Open(t.Context(), repo, Options{DataDir: dir})
	require.NoError(t, err)
	require.NoError(t, s.Put(t.Context(), "a", []byte("alpha")))

	before := s.KeyID()
	next := testKey(t)

	repo.rewriteErr = assert.AnError
	require.ErrorIs(t, s.Rotate(t.Context(), bytes.Clone(next)), assert.AnError)
	assert.Equal(t, before, s.KeyID())
	assert.NoFileExists(t, filepath.Join(dir, KeyFileName+".new"))

	repo.rewriteErr = nil
	require.NoError(t, s.Rotate(t.Context(), next), "the key is unloaded again, so the same key can be retried")
	assert.Equal(t, KeyID(next), s.KeyID())
}

func TestRotateCannotStageTheKeyFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	dir := t.TempDir()
	db := openDB(t, filepath.Join(dir, "db"))
	keyDir := filepath.Join(dir, "keys")

	s, err := Open(t.Context(), db.SecretRows(), Options{DataDir: keyDir})
	require.NoError(t, err)
	require.NoError(t, s.Put(t.Context(), "a", []byte("alpha")))

	before := s.KeyID()

	require.NoError(t, os.Chmod(keyDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(keyDir, 0o700) })

	require.Error(t, s.Rotate(t.Context(), testKey(t)))
	assert.Equal(t, before, s.KeyID())

	ids, err := db.SecretRows().KeyIDs(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{before}, ids, "no row is re-encrypted when the key cannot be saved first")
}

// TestRotateRenameFailureIsCompletedOnRestart simulates a crash-like failure after the database commit: the new key is in use, and the next Open finishes the rename from the staged file.
func TestRotateRenameFailureIsCompletedOnRestart(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)
	ctx := t.Context()
	keyFile := filepath.Join(dir, KeyFileName)

	s, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, "a", []byte("alpha")))

	oldKey, err := os.ReadFile(keyFile)
	require.NoError(t, err)

	// A non-empty directory in place of the key file makes the final rename fail.
	require.NoError(t, os.Remove(keyFile))
	require.NoError(t, os.MkdirAll(filepath.Join(keyFile, "blocker"), 0o700))

	next := testKey(t)
	err = s.Rotate(ctx, bytes.Clone(next))
	require.ErrorContains(t, err, "completed on restart")
	assert.Equal(t, KeyID(next), s.KeyID(), "the database is committed, so the new key is current")

	got, err := s.Get(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, "alpha", string(got))

	require.NoError(t, os.RemoveAll(keyFile))
	require.NoError(t, os.WriteFile(keyFile, oldKey, 0o600))

	s2, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	assert.Equal(t, KeyID(next), s2.KeyID())
	assert.NoFileExists(t, keyFile+".new")

	got, err = s2.Get(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, "alpha", string(got))
}

func TestRotateWithSuppliedKeyWarnsToUpdateConfig(t *testing.T) {
	db := openDB(t, t.TempDir())

	var logs bytes.Buffer

	s, err := Open(t.Context(), db.SecretRows(), Options{Key: EncodeKey(testKey(t)), Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	require.NoError(t, err)
	require.NoError(t, s.Rotate(t.Context(), testKey(t)))
	assert.Contains(t, logs.String(), "update secrets.key before restarting")
}

// TestPutDuringRotate holds a Put between sealing and writing while a rotation starts, which is when a row could be sealed under the outgoing key after the rotation re-encrypted everything.
func TestPutDuringRotate(t *testing.T) {
	ctx := t.Context()
	db := openDB(t, t.TempDir())
	repo := &faultyRepo{Repo: db.SecretRows()}

	s, err := Open(ctx, repo, Options{Key: EncodeKey(testKey(t))})
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, "seed", []byte("x")))

	entered, release := make(chan struct{}), make(chan struct{})
	repo.beforePut = func() {
		close(entered)
		<-release
	}

	putDone := make(chan error, 1)

	go func() { putDone <- s.Put(ctx, "racer", []byte("y")) }()

	<-entered

	// The Put holds the lock a rotation takes for the whole of its write, so the rotation can't start until the row is written.
	if s.writeMu.TryLock() {
		s.writeMu.Unlock()
		close(release)
		t.Fatal("a rotation could start while a Put was between sealing and writing")
	}

	// And the rotation holds that lock against every Put while it re-encrypts, so a Put that came after it can't start until it's done.
	rewrote := false
	repo.beforeRewrite = func() {
		rewrote = true

		if s.writeMu.TryRLock() {
			s.writeMu.RUnlock()
			t.Error("a Put could start while a rotation was re-encrypting")
		}
	}

	next := testKey(t)
	nextID := KeyID(next)
	rotated := make(chan error, 1)

	go func() { rotated <- s.Rotate(ctx, next) }()

	close(release)
	require.NoError(t, <-putDone)
	require.NoError(t, <-rotated)
	assert.True(t, rewrote, "the rotation re-encrypted the stored secrets")

	repo.beforePut = nil

	ids, err := db.SecretRows().KeyIDs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{nextID}, ids, "no secret is left under the outgoing key")
}

func TestReadKeyErrors(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notADir, nil, 0o600))

	_, err := ReadKey(Options{DataDir: notADir})
	require.ErrorContains(t, err, "checking key file")

	_, err = ReadKey(Options{})
	require.ErrorIs(t, err, ErrNoKey)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, KeyFileName), []byte("garbage"), 0o600))

	_, err = ReadKey(Options{DataDir: dir})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoKey, "a damaged key file is not a missing one")
}

func TestReadKeyWithoutADataDirIgnoresTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeKeyFile(filepath.Join(dir, KeyFileName), testKey(t)))
	t.Chdir(dir)

	_, err := ReadKey(Options{})
	require.ErrorIs(t, err, ErrNoKey, "a stray key file in the working directory is never picked up")
}

func TestPutRefusesAnEmptyID(t *testing.T) {
	db := openDB(t, t.TempDir())

	s, err := Open(t.Context(), db.SecretRows(), Options{Key: EncodeKey(testKey(t))})
	require.NoError(t, err)
	require.Error(t, s.Put(t.Context(), "", []byte("hunter2")))

	_, err = db.SecretRows().Get(t.Context(), "")
	require.ErrorIs(t, err, store.ErrNotFound, "nothing is stored")
}

// TestStoredFormatIsStable opens a row sealed by an earlier build. Key IDs and the envelope are persisted, so changing how either is derived would make every existing database unreadable.
func TestStoredFormatIsStable(t *testing.T) {
	const (
		encodedKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
		keyID      = "v1-c8f8abddbfc39267"
	)

	key, err := ParseKey(encodedKey)
	require.NoError(t, err)
	assert.Equal(t, keyID, KeyID(key))

	ciphertext, err := hex.DecodeString("8e6d125920b930072d67628f61170b62270b63e28769e9")
	require.NoError(t, err)

	db := openDB(t, t.TempDir())
	require.NoError(t, db.SecretRows().Put(t.Context(), store.SecretRow{
		ID:         "kodi/1/password",
		KeyID:      keyID,
		Nonce:      []byte{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab},
		Ciphertext: ciphertext,
	}))

	s, err := Open(t.Context(), db.SecretRows(), Options{Key: encodedKey})
	require.NoError(t, err)

	got, err := s.Get(t.Context(), "kodi/1/password")
	require.NoError(t, err)
	assert.Equal(t, "hunter2", string(got))
}

func TestRotateRefusesAShortKeyBeforeStagingIt(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)
	ctx := t.Context()

	s, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, "a", []byte("alpha")))

	before := s.KeyID()

	require.EqualError(t, s.Rotate(ctx, make([]byte, KeySize-1)), "key must be 32 bytes")
	assert.NoFileExists(t, filepath.Join(dir, KeyFileName+".new"), "nothing is staged for the next start to pick up")
	assert.Equal(t, before, s.KeyID())

	got, err := s.Get(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, "alpha", string(got))
}

func TestOpenReportsAnInterruptedRotationItCannotComplete(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	dir := t.TempDir()
	db := openDB(t, filepath.Join(dir, "db"))
	keyDir := filepath.Join(dir, "keys")
	keyFile := filepath.Join(keyDir, KeyFileName)
	ctx := t.Context()

	s, err := Open(ctx, db.SecretRows(), Options{DataDir: keyDir})
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, "a", []byte("alpha")))

	// The database committed the rotation, but the new key never replaced the old file.
	old, err := os.ReadFile(keyFile)
	require.NoError(t, err)

	next := testKey(t)
	require.NoError(t, s.Rotate(ctx, bytes.Clone(next)))
	require.NoError(t, os.WriteFile(keyFile, old, 0o600))
	require.NoError(t, writeKeyFile(keyFile+".new", next))

	require.NoError(t, os.Chmod(keyDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(keyDir, 0o700) })

	_, err = Open(ctx, db.SecretRows(), Options{DataDir: keyDir})
	require.ErrorContains(t, err, "completing key rotation: rename ")
	assert.FileExists(t, keyFile+".new", "the staged key is kept for the next try")

	require.NoError(t, os.Chmod(keyDir, 0o700))

	s2, err := Open(ctx, db.SecretRows(), Options{DataDir: keyDir})
	require.NoError(t, err)
	assert.Equal(t, KeyID(next), s2.KeyID())

	got, err := s2.Get(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, "alpha", string(got))
}

func TestOpenCompletesAStagedKeyThatIsAlreadyCurrent(t *testing.T) {
	dir := t.TempDir()
	db := openDB(t, dir)
	keyFile := filepath.Join(dir, KeyFileName)
	ctx := t.Context()

	s, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, "a", []byte("alpha")))

	// A copy of the current key left as if it were being rotated to, such as by an operator restoring files by hand.
	current, err := os.ReadFile(keyFile)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(keyFile+".new", current, 0o600))

	s2, err := Open(ctx, db.SecretRows(), Options{DataDir: dir})
	require.NoError(t, err)
	assert.Equal(t, s.KeyID(), s2.KeyID())
	assert.NoFileExists(t, keyFile+".new")

	after, err := os.ReadFile(keyFile)
	require.NoError(t, err)
	assert.Equal(t, current, after)

	got, err := s2.Get(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, "alpha", string(got))
}
