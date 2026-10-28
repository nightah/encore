package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
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
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"serve", "--log.level", "loud"}, `log.level "loud" must be debug, info, warn or error`},
		{[]string{"serve", "--log.format", "fancy"}, `log.format "fancy" must be auto, pretty, text or json`},
		{[]string{"serve", "--server.address", ""}, "server.address must not be empty"},
		{[]string{"serve", "extra"}, `unknown command "extra"`},
	} {
		dataDir := t.TempDir()
		t.Setenv("ENCORE_DATA_DIR", dataDir)

		_, _, err := runCLI(t, tc.args...)
		require.ErrorContains(t, err, tc.want, tc.args)
		assert.NoFileExists(t, store.DefaultPath(dataDir), "%v: nothing starts on invalid flags", tc.args)
	}
}

func TestRunAllWaitsForEveryTaskToStop(t *testing.T) {
	for name, served := range map[string]error{"serve stops cleanly": nil, "serve fails": errors.New("listen tcp: address in use")} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()

				stopServe := make(chan struct{})
				serve := func(context.Context) error {
					<-stopServe

					return served
				}

				// Each task takes its time to wind down after its context is done, as the app and the admin API do.
				var started sync.WaitGroup

				release := []chan struct{}{make(chan struct{}), make(chan struct{})}
				tasks := make([]func(context.Context), len(release))

				for i, ch := range release {
					started.Add(1)

					tasks[i] = func(ctx context.Context) {
						started.Done()
						<-ctx.Done()
						<-ch
					}
				}

				var (
					done atomic.Bool
					err  error
				)

				go func() {
					err = runAll(ctx, cancel, serve, tasks...)
					done.Store(true)
				}()

				synctest.Wait()
				started.Wait()
				require.NoError(t, ctx.Err(), "the tasks run while serve does")

				close(stopServe)
				synctest.Wait()
				require.ErrorIs(t, ctx.Err(), context.Canceled, "the tasks are told to stop once serve returns")
				assert.False(t, done.Load(), "but runAll waits for them")

				close(release[0])
				synctest.Wait()
				assert.False(t, done.Load(), "for every one of them")

				close(release[1])
				synctest.Wait()
				require.True(t, done.Load())
				assert.Equal(t, served, err, "serve's result is returned")
			})
		})
	}
}

func TestServeStopsWhenStartupFails(t *testing.T) {
	missing := t.TempDir() + "/missing"

	for name, tc := range map[string]struct {
		setup func(t *testing.T, dataDir string)
		want  string
		// Stateless mode works in a temporary directory and never locks the data directory.
		locks bool
	}{
		"the database can't be opened": {
			setup: func(t *testing.T, dataDir string) { require.NoError(t, os.Mkdir(store.DefaultPath(dataDir), 0o700)) },
			want:  "opening database",
			locks: true,
		},
		"stateless mode has nowhere to restore to": {
			setup: func(t *testing.T, _ string) {
				t.Setenv("ENCORE_STATELESS", "true")
				t.Setenv("ENCORE_SECRETS_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
				// Nothing is fetched: the temporary directory to restore into is missing first.
				t.Setenv("ENCORE_STORAGE_BACKEND", "s3")
				t.Setenv("ENCORE_STORAGE_S3_ENDPOINT", "https://s3.invalid")
				t.Setenv("ENCORE_STORAGE_S3_BUCKET", "encore")
				t.Setenv("ENCORE_STORAGE_S3_ACCESS_KEY", "AKIA")
				t.Setenv("ENCORE_STORAGE_S3_SECRET_KEY", "not-a-real-secret")
				t.Setenv("TMPDIR", missing)
			},
			want: "stat " + missing + ": no such file or directory",
		},
		"a configured room takes a name another room answers to": {
			setup: func(t *testing.T, dataDir string) {
				db := openStore(t, store.DefaultPath(dataDir))
				study := store.Room{Name: "Study"}
				require.NoError(t, db.Rooms().Create(t.Context(), &study))
				_, err := db.Rooms().AddAlias(t.Context(), study.ID, "den")
				require.NoError(t, err)
				require.NoError(t, db.Close())

				require.NoError(t, os.WriteFile(filepath.Join(dataDir, "encore.yml"), []byte("rooms:\n  - name: Den\n"), 0o600))
			},
			want:  `room "Den": `,
			locks: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			dataDir := t.TempDir()
			t.Setenv("ENCORE_DATA_DIR", dataDir)
			tc.setup(t, dataDir)

			_, stderr, err := runCLI(t, "serve", "--server.address", freeAddress(t), "--log.format", "text")
			require.ErrorContains(t, err, tc.want)
			assert.NotContains(t, stderr, "listening on", "the server never starts")

			if !tc.locks {
				assert.NoFileExists(t, store.DefaultPath(dataDir), "the data directory isn't used")

				return
			}

			lock, err := lockDataDir(dataDir)
			require.NoError(t, err, "the data directory is released")
			require.NoError(t, lock.Close())
		})
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
