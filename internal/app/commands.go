package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/store"
)

// SyncInstance reloads an instance's whole library from Kodi.
func (a *App) SyncInstance(ctx context.Context, id int64) error {
	_, cat, _, err := a.runtimeLibrary(id)
	if err != nil {
		return err
	}

	if cat == nil {
		return ErrNoRuntime
	}

	return cat.Sync(ctx)
}

// WakeInstance sends Wake-on-LAN packets until Kodi answers or timeout passes.
func (a *App) WakeInstance(ctx context.Context, id int64, timeout time.Duration) error {
	inst, err := a.runtime(id)
	if err != nil {
		return err
	}

	if inst.Waker.MAC() == nil {
		return errors.New("no MAC address is known for this Kodi instance; set one or let Encore learn it while Kodi is awake")
	}

	return inst.Waker.WakeAndWait(ctx, timeout)
}

// TestConnection checks a Kodi instance's settings with fresh connections, independent of its running runtime: whether JSON-RPC answers over HTTP, whether the WebSocket connects, whether the credentials are accepted and whether the video library can be read.
func (a *App) TestConnection(ctx context.Context, rec store.KodiInstance, password string) []Check {
	cfg := kodi.Config{
		Scheme: rec.Scheme, Host: rec.Host, Port: rec.Port, Path: rec.Path, WebSocketPort: rec.WSPort,
		Username: rec.Username, Password: password, Timeout: testTimeout, DisableWebSocket: true, HTTPClient: a.tune.kodiClient,
	}

	httpClient := kodi.NewClient(cfg, nil)

	checks := make([]Check, 0, 4)

	start := time.Now()
	pctx, cancel := context.WithTimeout(ctx, testTimeout)
	_, pingErr := httpClient.JSONRPCPing(pctx)

	cancel()

	ping := Check{Name: "ping", OK: pingErr == nil || errors.Is(pingErr, kodi.ErrUnauthorized), Duration: time.Since(start)}
	if !ping.OK {
		ping.Detail = pingErr.Error()
	}

	checks = append(checks, ping, a.testWebSocket(ctx, cfg))

	authCheck := Check{Name: "auth", OK: pingErr == nil, Duration: ping.Duration}

	// Why the library isn't read: the credentials were refused, or Kodi did not answer at all.
	skipped := ""

	switch {
	case errors.Is(pingErr, kodi.ErrUnauthorized):
		authCheck.Detail = "Kodi rejected the username or password"
		skipped = "not checked: " + authCheck.Detail
	case pingErr != nil:
		authCheck.Detail = "not checked: Kodi did not answer"
		skipped = authCheck.Detail
	}

	checks = append(checks, authCheck)

	lib := Check{Name: "library"}

	if pingErr == nil {
		start = time.Now()
		lctx, cancel := context.WithTimeout(ctx, testTimeout)
		r, err := httpClient.VideoLibraryGetMovies(lctx, kodirpc.VideoLibraryGetMoviesParams{Limits: &kodirpc.ListLimits{End: 1}})

		cancel()

		lib.Duration = time.Since(start)
		if err != nil {
			lib.Detail = err.Error()
		} else {
			lib.OK, lib.Detail = true, strconv.Itoa(r.Limits.Total)+" movies"
		}
	} else {
		lib.Detail = skipped
	}

	return append(checks, lib)
}

func (a *App) testWebSocket(ctx context.Context, cfg kodi.Config) Check {
	c := Check{Name: "websocket"}

	if cfg.WebSocketPort == 0 {
		c.Detail = "no WebSocket port configured; library changes are picked up by periodic syncs only"

		return c
	}

	cfg.DisableWebSocket = false
	client := kodi.NewClient(cfg, nil)

	up := make(chan struct{}, 1)
	stop := client.Listen(kodi.Listener{State: func(connected bool) {
		if connected {
			select {
			case up <- struct{}{}:
			default:
			}
		}
	}})
	defer stop()

	rctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()

	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = client.Run(rctx)
	}()

	start := time.Now()

	select {
	case <-up:
		c.OK = true
	case <-rctx.Done():
		c.Detail = fmt.Sprintf("could not connect to port %d; enable \"Allow remote control from applications on other systems\" in Kodi", cfg.WebSocketPort)
	}

	c.Duration = time.Since(start)

	cancel()
	<-done

	return c
}
