package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/blobstore"
	"github.com/nightah/encore/internal/config"
	"github.com/nightah/encore/internal/statestore"
	"github.com/nightah/encore/internal/store"
)

// statelessConfig is a stateless configuration whose state lives in local storage, with temporary directories confined to the test.
func statelessConfig(t *testing.T) (*config.Config, blobstore.Store) {
	t.Helper()

	t.Setenv("TMPDIR", t.TempDir())

	cfg := config.Defaults()
	cfg.Stateless = true
	cfg.DataDir = t.TempDir()
	cfg.Storage.Local.Path = t.TempDir()

	blobs, err := blobstore.Open(cfg.Storage, cfg.DataDir)
	require.NoError(t, err)

	return &cfg, blobs
}

func tempEntries(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(os.TempDir())
	require.NoError(t, err)

	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	return names
}

func TestPrepareStatelessDoesNothingWhenOff(t *testing.T) {
	cfg := config.Defaults()
	cfg.DataDir = t.TempDir()
	dataDir := cfg.DataDir

	run, err := prepareStateless(t.Context(), &cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	assert.Equal(t, dataDir, cfg.DataDir, "the data directory is used as configured")

	select {
	case <-run.startSaver(t.Context(), nil, slog.New(slog.DiscardHandler)):
	default:
		t.Fatal("without stateless mode there is no saver to wait for")
	}

	run.cleanup()
	assert.DirExists(t, dataDir, "cleanup never removes the configured data directory")
}

func TestPrepareStatelessStartsFreshWithoutSavedState(t *testing.T) {
	cfg, _ := statelessConfig(t)
	configured := cfg.DataDir

	var logs bytes.Buffer

	run, err := prepareStateless(t.Context(), cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	require.NoError(t, err)

	assert.NotEqual(t, configured, cfg.DataDir, "stateless mode works in a temporary directory")
	assert.Equal(t, run.dir, cfg.DataDir)
	assert.True(t, strings.HasPrefix(cfg.DataDir, os.TempDir()))
	assert.NoFileExists(t, store.DefaultPath(cfg.DataDir))
	assert.Contains(t, logs.String(), "starting fresh")

	run.cleanup()
	assert.NoDirExists(t, run.dir)
}

func TestPrepareStatelessRestoresTheSavedState(t *testing.T) {
	cfg, blobs := statelessConfig(t)

	saved := store.DefaultPath(t.TempDir())
	db := openStore(t, saved)
	require.NoError(t, db.Settings().Set(t.Context(), "marker", "saved"))

	_, err := (&statestore.Saver{DB: db, Store: blobs, Log: slog.New(slog.DiscardHandler)}).Save(t.Context())
	require.NoError(t, err)
	require.NoError(t, db.Close())

	var logs bytes.Buffer

	run, err := prepareStateless(t.Context(), cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	require.NoError(t, err)
	t.Cleanup(run.cleanup)

	assert.Equal(t, "saved", marker(t, store.DefaultPath(cfg.DataDir)))
	assert.Contains(t, logs.String(), "restored the database from storage")
}

func TestPrepareStatelessRejectsAnUnknownBackend(t *testing.T) {
	cfg, _ := statelessConfig(t)
	cfg.Storage.Backend = "floppy"

	_, err := prepareStateless(t.Context(), cfg, slog.New(slog.DiscardHandler))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening storage for stateless mode")
}

func TestPrepareStatelessRemovesItsDirectoryWhenTheSavedStateIsUnreadable(t *testing.T) {
	cfg, blobs := statelessConfig(t)
	configured := cfg.DataDir

	junk := []byte("not gzip")
	require.NoError(t, blobs.Put(t.Context(), statestore.KeyDatabase, bytes.NewReader(junk), int64(len(junk))))

	_, err := prepareStateless(t.Context(), cfg, slog.New(slog.DiscardHandler))
	require.Error(t, err)
	assert.Equal(t, configured, cfg.DataDir, "the configuration is left alone on failure")
	assert.Empty(t, tempEntries(t), "the temporary data directory was removed")
}

func TestPrepareStatelessFailsWithoutATemporaryDirectory(t *testing.T) {
	cfg, _ := statelessConfig(t)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))

	_, err := prepareStateless(t.Context(), cfg, slog.New(slog.DiscardHandler))
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestStatelessSaverSavesOnShutdownAndReleasesTheStorage(t *testing.T) {
	cfg, blobs := statelessConfig(t)

	run, err := prepareStateless(t.Context(), cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	t.Cleanup(run.cleanup)

	db := openStore(t, store.DefaultPath(cfg.DataDir))
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Settings().Set(t.Context(), "marker", "changed while running"))

	ctx, cancel := context.WithCancel(t.Context())
	done := run.startSaver(ctx, db, slog.New(slog.DiscardHandler))

	cancel()
	<-done

	restored := store.DefaultPath(t.TempDir())
	ok, err := statestore.Restore(t.Context(), blobs, restored)
	require.NoError(t, err)
	require.True(t, ok, "the final save uploaded the database")
	assert.Equal(t, "changed while running", marker(t, restored))

	_, err = blobs.Get(t.Context(), statestore.KeyOwner)
	assert.ErrorIs(t, err, blobstore.ErrNotFound, "the owner marker is removed on a clean shutdown")
}

// ownerClaims passes everything to Store and sends the owner named by each owner marker written.
type ownerClaims struct {
	blobstore.Store

	owners chan string
}

func (s *ownerClaims) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if key != statestore.KeyOwner {
		return s.Store.Put(ctx, key, r, size)
	}

	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	var marker struct{ Owner string }
	if err := json.Unmarshal(b, &marker); err != nil {
		return err
	}

	select {
	case s.owners <- marker.Owner:
	default:
	}

	return s.Store.Put(ctx, key, bytes.NewReader(b), int64(len(b)))
}

func TestStatelessSaverClaimsTheStorageUnderTheHostName(t *testing.T) {
	host, err := os.Hostname()
	require.NoError(t, err)

	cfg, _ := statelessConfig(t)

	run, err := prepareStateless(t.Context(), cfg, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	t.Cleanup(run.cleanup)

	claims := &ownerClaims{Store: run.blobs, owners: make(chan string, 1)}
	run.blobs = claims

	db := openStore(t, store.DefaultPath(cfg.DataDir))
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithCancel(t.Context())
	done := run.startSaver(ctx, db, slog.New(slog.DiscardHandler))

	assert.Equal(t, host, <-claims.owners, "restore names the host in its warning that Encore may still be running")

	cancel()
	<-done
}
