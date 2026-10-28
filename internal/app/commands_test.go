package app

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi"
	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/media"
	"github.com/nightah/encore/internal/store"
)

func TestSyncInstancePicksUpChangesWithoutNotifications(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)
		inst := h.instance(t)

		h.kodi.Update(func(l *fakekodi.Library) {
			l.Movies = append(l.Movies, fakekodi.Movie{MovieID: 50, Title: "Arrival", Label: "Arrival", File: "nfs://nas/movies/arrival.mkv", DateAdded: "2026-10-27 10:00:00"})
		})

		require.NoError(t, h.app.SyncInstance(t.Context(), inst.id))
		assert.True(t, hasTitle(inst.Catalog, "Arrival"))
	})
}

func TestSyncInstanceReportsKodiErrors(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		h.waitIndexed(t)

		h.kodi.Close()

		require.Error(t, h.app.SyncInstance(t.Context(), h.instance(t).id))
	})
}

// packets records the Wake-on-LAN packets a Waker sends.
type packets struct {
	mu      sync.Mutex
	sent    []string
	payload [][]byte
}

func (p *packets) send(_ context.Context, addr string, payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.sent, p.payload = append(p.sent, addr), append(p.payload, payload)

	return nil
}

func (p *packets) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.sent)
}

func TestWakeInstance(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		h := newHarness(t, true)
		inst := h.instance(t)
		ctx := t.Context()

		err := h.app.WakeInstance(ctx, inst.id, time.Second)
		require.ErrorContains(t, err, "no MAC address is known")

		sent := &packets{}
		inst.Waker.Send = sent.send
		inst.Waker.Broadcast = "192.0.2.255"
		inst.Waker.SetMAC(net.HardwareAddr{0xdc, 0xa6, 0x32, 0x4b, 0x19, 0x0e})

		require.NoError(t, h.app.WakeInstance(ctx, inst.id, time.Second))
		assert.Zero(t, sent.count(), "an awake Kodi isn't sent a packet")

		h.kodi.Close()

		// Packets go out at once and then every five seconds, the default, until the budget runs out.
		const budget = 12 * time.Second

		start := time.Now()
		err = h.app.WakeInstance(ctx, inst.id, budget)
		require.ErrorIs(t, err, media.ErrUnreachable)
		assert.Equal(t, budget, time.Since(start), "a Kodi that stays asleep is given the whole budget")
		require.Equal(t, 3, sent.count(), "the packet is repeated while Kodi sleeps")
		assert.Equal(t, "192.0.2.255:9", sent.sent[0], "the default Wake-on-LAN port is added")
		assert.Len(t, sent.payload[0], 102, "a magic packet is 6 bytes of 0xFF and the MAC 16 times")
	})
}

// connectionRecord describes a fake Kodi as the admin API saves it.
func connectionRecord(t *testing.T, srv *fakekodi.Server) store.KodiInstance {
	t.Helper()

	kc, err := kodi.ConfigFromURLs(srv.URL(), srv.WebSocketURL())
	require.NoError(t, err)

	return store.KodiInstance{Name: "Lounge", Scheme: kc.Scheme, Host: kc.Host, Port: kc.Port, Path: kc.Path, WSPort: kc.WebSocketPort}
}

func checkNames(checks []Check) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name
	}

	return out
}

func checkNamed(t *testing.T, checks []Check, name string) Check {
	t.Helper()

	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}

	t.Fatalf("no %q check in %v", name, checkNames(checks))

	return Check{}
}

func TestTestConnectionToAHealthyKodi(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		const pingTime = 5 * time.Millisecond

		a := newBareApp(t)
		srv := newKodi(t, fakekodi.Options{Username: "kodi", Password: "secret"})
		srv.Handle("JSONRPC.Ping", func(json.RawMessage) (any, *fakekodi.Error) {
			time.Sleep(pingTime)

			return "pong", nil
		})
		rec := connectionRecord(t, srv)
		rec.Username = "kodi"

		checks := a.TestConnection(t.Context(), rec, "secret")
		require.Equal(t, []string{"ping", "websocket", "auth", "library"}, checkNames(checks))

		for _, c := range checks {
			assert.True(t, c.OK, "%s: %s", c.Name, c.Detail)
		}

		assert.Equal(t, "7 movies", checkNamed(t, checks, "library").Detail)
		assert.Equal(t, pingTime, checkNamed(t, checks, "ping").Duration, "each check says how long it took")
		assert.Empty(t, srv.CallsTo("Player.Open"), "the test plays nothing")
		settled(t, func() bool { return srv.WebSocketClients() == 0 }, "the test leaves nothing connected")
	})
}

func TestTestConnectionWithoutAWebSocketPort(t *testing.T) {
	a := newBareApp(t)
	srv := newKodi(t, fakekodi.Options{})
	rec := connectionRecord(t, srv)
	rec.WSPort = 0

	checks := a.TestConnection(t.Context(), rec, "")

	ws := checkNamed(t, checks, "websocket")
	assert.False(t, ws.OK)
	assert.Contains(t, ws.Detail, "no WebSocket port configured")
	assert.True(t, checkNamed(t, checks, "library").OK, "HTTP alone still reads the library")
}

func TestTestConnectionWithTheWrongPassword(t *testing.T) {
	a := newBareApp(t)
	srv := newKodi(t, fakekodi.Options{Username: "kodi", Password: "secret"})
	rec := connectionRecord(t, srv)
	rec.Username = "kodi"

	checks := a.TestConnection(t.Context(), rec, "wrong")

	assert.True(t, checkNamed(t, checks, "ping").OK, "Kodi answered, if only to refuse")
	assert.True(t, checkNamed(t, checks, "websocket").OK, "Kodi doesn't authenticate WebSockets")

	auth := checkNamed(t, checks, "auth")
	assert.False(t, auth.OK)
	assert.Equal(t, "Kodi rejected the username or password", auth.Detail)

	lib := checkNamed(t, checks, "library")
	assert.False(t, lib.OK)
	assert.Contains(t, lib.Detail, "not checked")
	assert.Empty(t, srv.CallsTo("VideoLibrary.GetMovies"))
}

func TestTestConnectionToAnUnreachableKodi(t *testing.T) {
	inBubble(t, func(t *testing.T) {
		a := newBareApp(t)
		srv := newKodi(t, fakekodi.Options{})
		rec := connectionRecord(t, srv)
		srv.Close()

		start := time.Now()
		checks := a.TestConnection(t.Context(), rec, "")
		require.Equal(t, []string{"ping", "websocket", "auth", "library"}, checkNames(checks))
		assert.Equal(t, testTimeout, time.Since(start), "the WebSocket is retried for the whole test")

		for _, c := range checks {
			assert.False(t, c.OK, c.Name)
			assert.NotEmpty(t, c.Detail, c.Name)
		}

		assert.Contains(t, checkNamed(t, checks, "websocket").Detail, "could not connect to port")
		assert.Equal(t, "not checked: Kodi did not answer", checkNamed(t, checks, "auth").Detail)
		assert.Equal(t, "not checked: Kodi did not answer", checkNamed(t, checks, "library").Detail)
	})
}

func TestTestConnectionWhenTheLibraryCantBeRead(t *testing.T) {
	a := newBareApp(t)
	srv := newKodi(t, fakekodi.Options{})
	srv.Handle("VideoLibrary.GetMovies", func(json.RawMessage) (any, *fakekodi.Error) {
		return nil, &fakekodi.Error{Code: fakekodi.CodeUnavailable, Message: "database locked"}
	})

	checks := a.TestConnection(t.Context(), connectionRecord(t, srv), "")

	assert.True(t, checkNamed(t, checks, "auth").OK)

	lib := checkNamed(t, checks, "library")
	assert.False(t, lib.OK)
	assert.Contains(t, lib.Detail, "database locked")
}
