package kodi

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nightah/encore/internal/backend/kodi/fakekodi"
	"github.com/nightah/encore/internal/core/media"
)

func TestMagicPacket(t *testing.T) {
	mac, err := net.ParseMAC("DC:A6:32:4B:19:0E")
	require.NoError(t, err)

	packet, err := MagicPacket(mac)
	require.NoError(t, err)
	require.Len(t, packet, 102)

	assert.Equal(t, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, packet[:6])

	for i := range 16 {
		assert.Equal(t, []byte{0xdc, 0xa6, 0x32, 0x4b, 0x19, 0x0e}, packet[6+i*6:12+i*6], "repetition %d", i)
	}

	_, err = MagicPacket(net.HardwareAddr{1, 2, 3})
	require.Error(t, err)
}

func TestSendUDP(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)

	defer conn.Close()

	require.NoError(t, SendUDP(t.Context(), conn.LocalAddr().String(), []byte("hello")))

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))

	buf := make([]byte, 16)
	n, _, err := conn.ReadFrom(buf)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buf[:n]))
}

func TestLearnMAC(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	w := &Waker{Client: NewClient(testConfig(t, s), nil)}

	require.NoError(t, w.Learn(t.Context()))
	assert.Equal(t, "dc:a6:32:4b:19:0e", w.MAC().String())

	s.SetMAC("Busy")
	require.EqualError(t, w.Learn(t.Context()), "kodi reported Busy instead of a MAC address", "no quotes for log output to escape")
	assert.Equal(t, "dc:a6:32:4b:19:0e", w.MAC().String(), "a failed lookup keeps the known address")

	s.SetMAC("")
	require.EqualError(t, w.Learn(t.Context()), "kodi reported no MAC address")
}

type packetLog struct {
	mu      sync.Mutex
	addrs   []string
	packets [][]byte
	onSend  func()
}

func (p *packetLog) send(_ context.Context, addr string, payload []byte) error {
	p.mu.Lock()
	p.addrs = append(p.addrs, addr)
	p.packets = append(p.packets, bytes.Clone(payload))
	p.mu.Unlock()

	if p.onSend != nil {
		p.onSend()
	}

	return nil
}

func (p *packetLog) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.packets)
}

func TestWakeAndWait(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})

	var awake atomic.Bool

	s.Handle("JSONRPC.Ping", func(json.RawMessage) (any, *fakekodi.Error) {
		if !awake.Load() {
			return nil, &fakekodi.Error{Code: -32100, Message: "asleep"}
		}

		return "pong", nil
	})

	cfg := testConfig(t, s)
	cfg.DisableWebSocket = true

	log := &packetLog{onSend: func() { awake.Store(true) }}
	w := &Waker{
		Client:         NewClient(cfg, nil),
		Broadcast:      "192.168.1.255",
		Send:           log.send,
		PollInterval:   10 * time.Millisecond,
		ResendInterval: time.Second,
	}
	w.SetMAC(net.HardwareAddr{0xdc, 0xa6, 0x32, 0x4b, 0x19, 0x0e})

	require.NoError(t, w.WakeAndWait(t.Context(), 2*time.Second))
	require.Equal(t, 1, log.count())
	assert.Equal(t, "192.168.1.255:9", log.addrs[0])

	want, err := MagicPacket(w.MAC())
	require.NoError(t, err)
	assert.Equal(t, want, log.packets[0])

	require.NoError(t, w.WakeAndWait(t.Context(), time.Second))
	assert.Equal(t, 1, log.count(), "an awake Kodi gets no packet")
}

func TestWakeAndWaitTimesOut(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	s.Handle("JSONRPC.Ping", func(json.RawMessage) (any, *fakekodi.Error) {
		return nil, &fakekodi.Error{Code: -32100, Message: "asleep"}
	})

	cfg := testConfig(t, s)
	cfg.DisableWebSocket = true

	log := &packetLog{}
	w := &Waker{
		Client:         NewClient(cfg, nil),
		Broadcast:      "10.0.0.255:7",
		Send:           log.send,
		PollInterval:   10 * time.Millisecond,
		ResendInterval: 50 * time.Millisecond,
	}
	w.SetMAC(net.HardwareAddr{1, 2, 3, 4, 5, 6})

	err := w.WakeAndWait(t.Context(), 300*time.Millisecond)
	require.ErrorIs(t, err, media.ErrUnreachable)
	assert.GreaterOrEqual(t, log.count(), 2, "the packet is repeated")
	assert.Equal(t, "10.0.0.255:7", log.addrs[0])

	require.Error(t, (&Waker{Client: w.Client}).Wake(t.Context()), "no MAC, no packet")
}

func TestSendUDPBadAddress(t *testing.T) {
	require.Error(t, SendUDP(t.Context(), "no port here", []byte("x")))
}

func TestMACAddressReplies(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply func(json.RawMessage) (any, *fakekodi.Error)
		err   string
	}{
		{"too long", labels("00:00:00:00:fe:80:00:00"), "instead of a MAC address"},
		{"failing", func(json.RawMessage) (any, *fakekodi.Error) {
			return nil, &fakekodi.Error{Code: CodeInternalError, Message: "x"}
		}, "XBMC.GetInfoLabels"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := fakekodi.New(t, fakekodi.Options{})
			s.Handle("XBMC.GetInfoLabels", tc.reply)

			known := net.HardwareAddr{1, 2, 3, 4, 5, 6}
			w := &Waker{Client: NewClient(testConfig(t, s), nil)}
			w.SetMAC(known)

			err := w.Learn(t.Context())

			require.ErrorContains(t, err, tc.err)
			assert.Equal(t, known, w.MAC(), "a failed lookup keeps the address already known")
		})
	}
}

func labels(mac string) func(json.RawMessage) (any, *fakekodi.Error) {
	return func(json.RawMessage) (any, *fakekodi.Error) { return map[string]string{"Network.MacAddress": mac}, nil }
}

func TestWakeAddressAndFailures(t *testing.T) {
	log := &packetLog{}
	w := &Waker{Send: log.send}
	w.SetMAC(net.HardwareAddr{1, 2, 3, 4, 5, 6})

	require.NoError(t, w.Wake(t.Context()))
	assert.Equal(t, DefaultBroadcast, log.addrs[0], "the default broadcast address")

	w.Send = func(context.Context, string, []byte) error { return errNotSent }
	err := w.Wake(t.Context())
	require.ErrorIs(t, err, errNotSent)
	require.ErrorContains(t, err, "sending to "+DefaultBroadcast)

	w.SetMAC(net.HardwareAddr{1, 2, 3, 4, 5, 6, 7, 8})
	require.ErrorContains(t, w.Wake(t.Context()), "6-byte MAC", "an EUI-64 address can't be woken")
}

func TestWakeAndWaitStops(t *testing.T) {
	s := fakekodi.New(t, fakekodi.Options{})
	s.Handle("JSONRPC.Ping", func(json.RawMessage) (any, *fakekodi.Error) {
		return nil, &fakekodi.Error{Code: -32100, Message: "asleep"}
	})

	cfg := testConfig(t, s)
	cfg.DisableWebSocket = true

	t.Run("without a MAC", func(t *testing.T) {
		w := &Waker{Client: NewClient(cfg, nil), PollInterval: time.Millisecond}

		require.ErrorContains(t, w.WakeAndWait(t.Context(), time.Second), "MAC address unknown")
	})

	t.Run("when the caller gives up", func(t *testing.T) {
		log := &packetLog{}
		w := &Waker{Client: NewClient(cfg, nil), Send: log.send, PollInterval: 5 * time.Millisecond}
		w.SetMAC(net.HardwareAddr{1, 2, 3, 4, 5, 6})

		ctx, cancel := context.WithCancel(t.Context())
		log.onSend = cancel

		err := w.WakeAndWait(ctx, time.Minute)

		require.ErrorIs(t, err, context.Canceled)
		require.NotErrorIs(t, err, media.ErrUnreachable, "giving up isn't Kodi failing to wake")
		assert.Equal(t, 1, log.count(), "the default resend interval doesn't repeat the packet straight away")
	})
}
