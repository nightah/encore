package statestore

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/store"
)

func newBlobStore(t *testing.T) blobstore.Store {
	t.Helper()

	s, err := blobstore.Open(config.Storage{Backend: config.BackendLocal, Local: config.StorageLocal{Path: t.TempDir()}}, t.TempDir())
	require.NoError(t, err)

	return s
}

func openDB(t *testing.T, dir string) *store.DB {
	t.Helper()

	db, err := store.Open(t.Context(), store.Options{Path: store.DefaultPath(dir), Logger: slog.New(slog.DiscardHandler)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

func TestRestoreWithNothingSaved(t *testing.T) {
	ok, err := Restore(t.Context(), newBlobStore(t), filepath.Join(t.TempDir(), "encore.db"))
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestSaveAndRestoreRoundTrip(t *testing.T) {
	bs := newBlobStore(t)

	// First container: create state and save it.
	first := openDB(t, t.TempDir())
	require.NoError(t, first.Settings().Set(t.Context(), "skill.id", "amzn1.ask.skill.test"))

	saver := &Saver{DB: first, Store: bs, Log: slog.New(slog.DiscardHandler), Owner: "first"}

	uploaded, err := saver.Save(t.Context())
	require.NoError(t, err)
	assert.True(t, uploaded)

	uploaded, err = saver.Save(t.Context())
	require.NoError(t, err)
	assert.False(t, uploaded, "an unchanged database isn't uploaded again")

	// A change is saved, and the earlier copy is kept as a fallback.
	require.NoError(t, first.Settings().Set(t.Context(), "skill.invocation", "encore"))

	uploaded, err = saver.Save(t.Context())
	require.NoError(t, err)
	assert.True(t, uploaded)

	_, err = bs.Get(t.Context(), KeyPrevious)
	require.NoError(t, err)

	// Second container, empty disk: restore and read the state back.
	dir := t.TempDir()
	ok, err := Restore(t.Context(), bs, store.DefaultPath(dir))
	require.NoError(t, err)
	require.True(t, ok)

	info, err := os.Stat(store.DefaultPath(dir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	second := openDB(t, dir)

	var invocation string
	require.NoError(t, second.Settings().Get(t.Context(), "skill.invocation", &invocation))
	assert.Equal(t, "encore", invocation)
}

func TestRunSavesOnShutdown(t *testing.T) {
	bs := newBlobStore(t)
	db := openDB(t, t.TempDir())
	saver := &Saver{DB: db, Store: bs, Log: slog.New(slog.DiscardHandler), Interval: time.Hour, Owner: "me"}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		saver.Run(ctx)
		close(done)
	}()

	require.NoError(t, db.Settings().Set(t.Context(), "k", "v"))
	cancel()
	<-done

	r, err := bs.Get(t.Context(), KeyDatabase)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	_, err = bs.Get(t.Context(), KeyOwner)
	require.ErrorIs(t, err, blobstore.ErrNotFound, "a clean shutdown releases the owner marker")
}

func TestReleaseKeepsAnotherOwnersMarker(t *testing.T) {
	bs := newBlobStore(t)

	other := &Saver{Store: bs, Log: slog.New(slog.DiscardHandler), Owner: "other"}
	other.heartbeat(t.Context())

	me := &Saver{Store: bs, Log: slog.New(slog.DiscardHandler), Owner: "me"}
	me.release(t.Context())

	_, _, active, err := ActiveOwner(t.Context(), bs, time.Now())
	require.NoError(t, err)
	assert.True(t, active)
}

func TestActiveOwner(t *testing.T) {
	bs := newBlobStore(t)
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

	_, _, active, err := ActiveOwner(t.Context(), bs, now)
	require.NoError(t, err)
	assert.False(t, active, "no marker")

	s := &Saver{Store: bs, Log: slog.New(slog.DiscardHandler), Owner: "pod-1", Now: func() time.Time { return now }}
	s.heartbeat(t.Context())

	name, seen, active, err := ActiveOwner(t.Context(), bs, now.Add(time.Minute))
	require.NoError(t, err)
	assert.True(t, active)
	assert.Equal(t, "pod-1", name)
	assert.True(t, seen.Equal(now))

	_, _, active, err = ActiveOwner(t.Context(), bs, now.Add(ownerStale-time.Millisecond))
	require.NoError(t, err)
	assert.True(t, active, "a heartbeat is fresh until it is ownerStale old")

	_, _, active, err = ActiveOwner(t.Context(), bs, now.Add(ownerStale))
	require.NoError(t, err)
	assert.False(t, active, "a heartbeat exactly ownerStale old is stale")

	_, _, active, err = ActiveOwner(t.Context(), bs, now.Add(time.Hour))
	require.NoError(t, err)
	assert.False(t, active, "a stale heartbeat")
}

func TestUploadKeepsPrevious(t *testing.T) {
	bs := newBlobStore(t)

	first := openDB(t, t.TempDir())
	require.NoError(t, first.Settings().Set(t.Context(), "marker", "saved"))

	_, err := (&Saver{DB: first, Store: bs, Log: slog.New(slog.DiscardHandler)}).Save(t.Context())
	require.NoError(t, err)

	replacement := filepath.Join(t.TempDir(), "restored.db")
	second := openDB(t, t.TempDir())
	require.NoError(t, second.Settings().Set(t.Context(), "marker", "uploaded"))
	require.NoError(t, second.BackupTo(t.Context(), replacement))

	require.NoError(t, Upload(t.Context(), bs, replacement))

	read := func(key string) string {
		r, err := bs.Get(t.Context(), key)
		require.NoError(t, err)

		zr, err := gzip.NewReader(r)
		require.NoError(t, err)

		raw, err := io.ReadAll(zr)
		require.NoError(t, err)
		require.NoError(t, r.Close())

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(store.DefaultPath(dir), raw, 0o600))

		var v string
		require.NoError(t, openDB(t, dir).Settings().Get(t.Context(), "marker", &v))

		return v
	}

	assert.Equal(t, "uploaded", read(KeyDatabase))
	assert.Equal(t, "saved", read(KeyPrevious))
}

func TestOwnerWarning(t *testing.T) {
	bs := newBlobStore(t)
	now := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)

	other := &Saver{Store: bs, Log: slog.New(slog.DiscardHandler), Owner: "other", Now: func() time.Time { return now }}
	other.heartbeat(t.Context())

	var logs bytes.Buffer

	me := &Saver{Store: bs, Log: slog.New(slog.NewTextHandler(&logs, nil)), Owner: "me", Now: func() time.Time { return now.Add(time.Minute) }}
	me.checkOwner(t.Context())
	assert.Contains(t, logs.String(), "another Encore seems to be using this storage")

	logs.Reset()

	later := &Saver{Store: bs, Log: slog.New(slog.NewTextHandler(&logs, nil)), Owner: "later", Now: func() time.Time { return now.Add(time.Hour) }}
	later.checkOwner(t.Context())
	assert.NotContains(t, logs.String(), "another Encore", "a stale owner is ignored")

	logs.Reset()

	restarted := &Saver{Store: bs, Log: slog.New(slog.NewTextHandler(&logs, nil)), Owner: "later", Now: func() time.Time { return now.Add(time.Hour + time.Second) }}
	restarted.checkOwner(t.Context())
	assert.NotContains(t, logs.String(), "another Encore", "its own fresh marker is not another Encore")
}

type failingDB struct{}

func (failingDB) Backup(context.Context, io.Writer) error { return io.ErrUnexpectedEOF }

func TestSaveReportsBackupErrors(t *testing.T) {
	_, err := (&Saver{DB: failingDB{}, Store: newBlobStore(t), Log: slog.New(slog.DiscardHandler)}).Save(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copying database")
}

// memDB is a Database whose backup is whatever bytes it currently holds.
type memDB struct {
	mu   sync.Mutex
	data []byte
	err  error
}

func (m *memDB) Backup(_ context.Context, w io.Writer) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return m.err
	}

	_, err := w.Write(m.data)

	return err
}

func (m *memDB) set(data string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data, m.err = []byte(data), err
}

// faultyStore wraps a real store and fails the operations configured for a key.
type faultyStore struct {
	blobstore.Store

	mu        sync.Mutex
	getErr    map[string]error
	putErr    map[string]error
	readErr   map[string]error
	deleteErr error
}

func newFaultyStore(t *testing.T) *faultyStore {
	t.Helper()

	return &faultyStore{Store: newBlobStore(t), getErr: map[string]error{}, putErr: map[string]error{}, readErr: map[string]error{}}
}

func (f *faultyStore) fail(ops map[string]error, key string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ops[key] = err
}

func (f *faultyStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	getErr, readErr := f.getErr[key], f.readErr[key]
	f.mu.Unlock()

	if getErr != nil {
		return nil, getErr
	}

	if readErr != nil {
		return io.NopCloser(iotest.ErrReader(readErr)), nil
	}

	return f.Store.Get(ctx, key)
}

func (f *faultyStore) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	f.mu.Lock()
	err := f.putErr[key]
	f.mu.Unlock()

	if err != nil {
		return err
	}

	return f.Store.Put(ctx, key, r, size)
}

func (f *faultyStore) Delete(ctx context.Context, key string) error {
	f.mu.Lock()
	err := f.deleteErr
	f.mu.Unlock()

	if err != nil {
		return err
	}

	return f.Store.Delete(ctx, key)
}

func readGzip(t *testing.T, s blobstore.Store, key string) string {
	t.Helper()

	r, err := s.Get(t.Context(), key)
	require.NoError(t, err)

	defer r.Close()

	zr, err := gzip.NewReader(r)
	require.NoError(t, err)

	raw, err := io.ReadAll(zr)
	require.NoError(t, err)

	return string(raw)
}

func putRaw(t *testing.T, s blobstore.Store, key string, data []byte) {
	t.Helper()
	require.NoError(t, s.Put(t.Context(), key, bytes.NewReader(data), int64(len(data))))
}

func gzipped(t *testing.T, s string) []byte {
	t.Helper()

	gz, err := compress([]byte(s))
	require.NoError(t, err)

	return gz.Bytes()
}

func TestRestoreFailuresLeaveTheExistingFile(t *testing.T) {
	whole := gzipped(t, strings.Repeat("database page ", 1000))

	for name, tt := range map[string]struct {
		prepare func(t *testing.T, s *faultyStore)
		want    string
	}{
		"storage error": {
			prepare: func(_ *testing.T, s *faultyStore) { s.fail(s.getErr, KeyDatabase, assert.AnError) },
			want:    "fetching saved database",
		},
		"not gzip": {
			prepare: func(t *testing.T, s *faultyStore) { putRaw(t, s, KeyDatabase, []byte("SQLite format 3\x00")) },
			want:    "reading saved database",
		},
		"truncated": {
			prepare: func(t *testing.T, s *faultyStore) { putRaw(t, s, KeyDatabase, whole[:len(whole)/2]) },
			want:    "restoring saved database",
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newFaultyStore(t)
			tt.prepare(t, s)

			dir := t.TempDir()
			path := filepath.Join(dir, "encore.db")
			require.NoError(t, os.WriteFile(path, []byte("current"), 0o600))

			ok, err := Restore(t.Context(), s, path)
			require.ErrorContains(t, err, tt.want)
			assert.False(t, ok)

			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, "current", string(got))

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Len(t, entries, 1, "no temporary file is left behind")
		})
	}
}

func TestRestoreReplacesAnExistingFile(t *testing.T) {
	s := newBlobStore(t)
	putRaw(t, s, KeyDatabase, gzipped(t, "saved"))

	path := filepath.Join(t.TempDir(), "nested", "encore.db")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("stale"), 0o600))

	ok, err := Restore(t.Context(), s, path)
	require.NoError(t, err)
	assert.True(t, ok)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "saved", string(got))
}

func TestRestoreIntoAnUnusablePath(t *testing.T) {
	s := newBlobStore(t)
	putRaw(t, s, KeyDatabase, gzipped(t, "saved"))

	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	_, err := Restore(t.Context(), s, filepath.Join(file, "encore.db"))
	require.Error(t, err)

	// A non-empty directory where the database belongs cannot be replaced, so the final rename fails.
	dir := t.TempDir()
	path := filepath.Join(dir, "encore.db")
	require.NoError(t, os.MkdirAll(filepath.Join(path, "occupied"), 0o700))

	ok, err := Restore(t.Context(), s, path)
	require.Error(t, err, "a failed rename is reported, not a restored database")
	assert.True(t, ok, "a saved database did exist")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the temporary copy is removed")
}

func TestSaveRetriesAFailedUpload(t *testing.T) {
	s := newFaultyStore(t)
	db := &memDB{}
	db.set("v1", nil)

	saver := &Saver{DB: db, Store: s, Log: slog.New(slog.DiscardHandler)}

	s.fail(s.putErr, KeyDatabase, assert.AnError)

	uploaded, err := saver.Save(t.Context())
	require.ErrorIs(t, err, assert.AnError)
	assert.False(t, uploaded)

	s.fail(s.putErr, KeyDatabase, nil)

	uploaded, err = saver.Save(t.Context())
	require.NoError(t, err)
	assert.True(t, uploaded, "an unchanged database is uploaded again when the last upload failed")
	assert.Equal(t, "v1", readGzip(t, s, KeyDatabase))
}

func TestSaveUploadsEvenWhenThePreviousCopyCannotBeKept(t *testing.T) {
	s := newFaultyStore(t)
	putRaw(t, s, KeyDatabase, gzipped(t, "v1"))

	db := &memDB{}
	db.set("v2", nil)

	var logs bytes.Buffer

	saver := &Saver{DB: db, Store: s, Log: slog.New(slog.NewTextHandler(&logs, nil))}

	heal := func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		clear(s.getErr)
		clear(s.putErr)
		clear(s.readErr)
	}

	for name, fail := range map[string]func(){
		"reading":  func() { s.fail(s.readErr, KeyDatabase, assert.AnError) },
		"writing":  func() { s.fail(s.putErr, KeyPrevious, assert.AnError) },
		"fetching": func() { s.fail(s.getErr, KeyDatabase, assert.AnError) },
	} {
		t.Run(name, func(t *testing.T) {
			heal()
			putRaw(t, s, KeyDatabase, gzipped(t, "v1"))

			saver.last = [32]byte{}

			logs.Reset()
			fail()

			uploaded, err := saver.Save(t.Context())
			require.NoError(t, err)
			assert.True(t, uploaded)
			assert.Contains(t, logs.String(), "keeping previous database copy")

			heal()
			assert.Equal(t, "v2", readGzip(t, s, KeyDatabase))
		})
	}
}

func TestUploadFailures(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "encore.db")
	require.NoError(t, os.WriteFile(dbFile, []byte("replacement"), 0o600))

	err := Upload(t.Context(), newBlobStore(t), filepath.Join(t.TempDir(), "missing.db"))
	require.ErrorContains(t, err, "reading database")

	s := newFaultyStore(t)
	putRaw(t, s, KeyDatabase, gzipped(t, "current"))

	s.fail(s.putErr, KeyPrevious, assert.AnError)
	require.ErrorContains(t, Upload(t.Context(), s, dbFile), KeyPrevious)
	assert.Equal(t, "current", readGzip(t, s, KeyDatabase), "the current copy is not replaced unless it was kept")

	s.fail(s.putErr, KeyPrevious, nil)
	s.fail(s.putErr, KeyDatabase, assert.AnError)
	require.ErrorContains(t, Upload(t.Context(), s, dbFile), "uploading database")

	s.fail(s.putErr, KeyDatabase, nil)
	require.NoError(t, Upload(t.Context(), s, dbFile))
	assert.Equal(t, "replacement", readGzip(t, s, KeyDatabase))
	assert.Equal(t, "current", readGzip(t, s, KeyPrevious))
}

func TestUploadToEmptyStorage(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "encore.db")
	require.NoError(t, os.WriteFile(dbFile, []byte("first"), 0o600))

	s := newBlobStore(t)
	require.NoError(t, Upload(t.Context(), s, dbFile))
	assert.Equal(t, "first", readGzip(t, s, KeyDatabase))

	_, err := s.Get(t.Context(), KeyPrevious)
	require.ErrorIs(t, err, blobstore.ErrNotFound, "there was nothing to keep")
}

func TestActiveOwnerErrors(t *testing.T) {
	s := newFaultyStore(t)
	putRaw(t, s, KeyOwner, []byte("{not json"))

	_, _, active, err := ActiveOwner(t.Context(), s, time.Now())
	require.ErrorContains(t, err, "owner marker")
	assert.False(t, active)

	s.fail(s.getErr, KeyOwner, assert.AnError)

	_, _, _, err = ActiveOwner(t.Context(), s, time.Now())
	require.ErrorIs(t, err, assert.AnError)
}

func TestOwnerMarkerFailuresAreNotFatal(t *testing.T) {
	s := newFaultyStore(t)

	var logs bytes.Buffer

	saver := &Saver{Store: s, Log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})), Owner: "me"}
	saver.heartbeat(t.Context())

	s.fail(s.putErr, KeyOwner, assert.AnError)
	saver.heartbeat(t.Context())
	assert.Contains(t, logs.String(), "writing owner marker")

	s.mu.Lock()
	s.deleteErr = assert.AnError
	s.mu.Unlock()

	saver.release(t.Context())
	assert.Contains(t, logs.String(), "removing owner marker")

	name, _, _, err := ActiveOwner(t.Context(), s, time.Now())
	require.NoError(t, err)
	assert.Equal(t, "me", name, "the marker survives a failed release")
}

func TestRunSavesOnEveryTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newFaultyStore(t)
		db := &memDB{}
		db.set("v1", nil)

		var logs bytes.Buffer

		saver := &Saver{DB: db, Store: s, Log: slog.New(slog.NewTextHandler(&logs, nil)), Owner: "me", Now: time.Now}
		start := time.Now()

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			saver.Run(ctx)
			close(done)
		}()

		synctest.Wait()

		name, seen, active, err := ActiveOwner(t.Context(), s, time.Now())
		require.NoError(t, err)
		assert.True(t, active, "the storage is claimed at start")
		assert.Equal(t, "me", name)
		assert.True(t, seen.Equal(start))

		_, err = s.Get(t.Context(), KeyDatabase)
		require.ErrorIs(t, err, blobstore.ErrNotFound, "nothing is saved before the first tick")

		time.Sleep(DefaultInterval - time.Second)
		synctest.Wait()

		_, err = s.Get(t.Context(), KeyDatabase)
		require.ErrorIs(t, err, blobstore.ErrNotFound, "a zero interval means the default")

		time.Sleep(time.Second)
		synctest.Wait()
		assert.Equal(t, "v1", readGzip(t, s, KeyDatabase))

		_, seen, _, err = ActiveOwner(t.Context(), s, time.Now())
		require.NoError(t, err)
		assert.True(t, seen.Equal(start.Add(DefaultInterval)), "each tick refreshes the heartbeat")

		db.set("v2", assert.AnError)
		time.Sleep(DefaultInterval)
		synctest.Wait()
		assert.Contains(t, logs.String(), "saving database to storage")

		db.set("v3", nil)
		s.fail(s.putErr, KeyDatabase, assert.AnError)
		cancel()
		<-done

		assert.Contains(t, logs.String(), "saving database on shutdown")
		assert.Equal(t, "v1", readGzip(t, s, KeyDatabase))

		_, err = s.Get(t.Context(), KeyOwner)
		require.ErrorIs(t, err, blobstore.ErrNotFound, "the marker is released even when the last save failed")
	})
}
