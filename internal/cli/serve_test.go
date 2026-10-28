package cli

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/server"
	"github.com/nightah/encore/internal/store"
)

// freeAddress returns a loopback address nothing is listening on.
func freeAddress(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := l.Addr().String()
	require.NoError(t, l.Close())

	return address
}

// syncBuffer is a bytes.Buffer safe to read while serve writes its log.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// startServe runs serve on address in the background and waits until it answers its health check. Cancelling the returned function stops it and returns serve's error.
func startServe(t *testing.T, address string, args ...string) (stop func() error, logs *syncBuffer) {
	t.Helper()

	t.Setenv("ENCORE_CONFIG", "")

	logs = &syncBuffer{}
	cmd := NewRootCommand(io.Discard, logs)
	cmd.SetArgs(append([]string{"serve", "--server.address", address, "--log.format", "text"}, args...))

	ctx, cancel := context.WithCancel(t.Context())
	errc := make(chan error, 1)

	go func() { errc <- cmd.ExecuteContext(ctx) }()

	healthy := func() bool {
		resp, err := http.Get("http://" + address + server.PathHealthz) //nolint:noctx // a probe in a test
		if err != nil {
			return false
		}

		_ = resp.Body.Close()

		return resp.StatusCode == http.StatusOK
	}

	ready := false
	for !ready {
		select {
		case err := <-errc:
			cancel()
			t.Fatalf("serve exited before it was healthy: %v\n%s", err, logs)
		case <-time.After(20 * time.Millisecond):
			ready = healthy()
		}
	}

	var once sync.Once

	var err error

	stop = func() error {
		once.Do(func() {
			cancel()
			err = <-errc
		})

		return err
	}
	t.Cleanup(func() { _ = stop() })

	return stop, logs
}

func TestServeRunsUntilCancelled(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ENCORE_DATA_DIR", dataDir)

	address := freeAddress(t)
	stop, logs := startServe(t, address)

	_, err := lockDataDir(dataDir)
	require.ErrorIs(t, err, errLocked, "serve holds the data directory while it runs")

	_, _, err = runCLI(t, "healthcheck", "--server.address", address)
	require.NoError(t, err, "healthcheck finds the running server")

	assert.FileExists(t, store.DefaultPath(dataDir))

	require.NoError(t, stop())
	assert.Contains(t, logs.String(), "starting Encore")
	assert.Contains(t, logs.String(), "alexa.skill_id is not set", "an unconfigured skill ID is called out")
	assert.True(t, strings.HasSuffix(strings.TrimSpace(logs.String()), "msg=stopped"), "stopping is the last thing logged:\n%s", logs)

	lock, err := lockDataDir(dataDir)
	require.NoError(t, err, "the lock is released on shutdown")
	require.NoError(t, lock.Close())
}

func TestServeWithAConfiguredSkillID(t *testing.T) {
	t.Setenv("ENCORE_DATA_DIR", t.TempDir())
	t.Setenv("ENCORE_ALEXA_SKILL_ID", "amzn1.ask.skill.00000000-0000-0000-0000-000000000000")

	stop, logs := startServe(t, freeAddress(t))

	require.NoError(t, stop())
	assert.NotContains(t, logs.String(), "alexa.skill_id is not set")
}

func TestServeHonoursTheLogLevel(t *testing.T) {
	t.Setenv("ENCORE_DATA_DIR", t.TempDir())

	stop, logs := startServe(t, freeAddress(t), "--log.level", "warn")

	require.NoError(t, stop())
	assert.Contains(t, logs.String(), "level=WARN")
	assert.NotContains(t, logs.String(), "level=INFO", "info lines are left out at warn")
}

func TestServeRefusesALockedDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("ENCORE_DATA_DIR", dataDir)

	lock, err := lockDataDir(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Close() })

	_, _, err = runCLI(t, "serve", "--server.address", freeAddress(t))
	require.ErrorIs(t, err, errLocked)
	assert.NoFileExists(t, store.DefaultPath(dataDir), "the database isn't opened")
}

func TestServeReportsAListenFailureOnce(t *testing.T) {
	t.Setenv("ENCORE_DATA_DIR", t.TempDir())

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = busy.Close() })

	_, stderr, err := runCLI(t, "serve", "--server.address", busy.Addr().String(), "--log.format", "text")
	require.ErrorIs(t, err, errSilent, "the failure is already logged")
	assert.Contains(t, stderr, `msg="server stopped"`)
	assert.Contains(t, stderr, "listening on "+busy.Addr().String())
	assert.NotContains(t, stderr, "msg=stopped")
}

func TestServeRejectsInvalidFlags(t *testing.T) {
	for _, args := range [][]string{
		{"serve", "--log.level", "loud"},
		{"serve", "--log.format", "fancy"},
		{"serve", "--server.address", ""},
		{"serve", "extra"},
	} {
		_, _, err := runCLI(t, args...)
		require.Error(t, err, args)
	}
}

func TestServeFailsWhenTheDataDirCannotBeCreated(t *testing.T) {
	file := t.TempDir() + "/file"
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	t.Setenv("ENCORE_DATA_DIR", file+"/data")

	_, _, err := runCLI(t, "serve", "--server.address", freeAddress(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating data directory")
}
