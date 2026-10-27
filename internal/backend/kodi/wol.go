package kodi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// SendUDP is the real SendFunc. Go enables SO_BROADCAST on UDP sockets, so broadcast addresses work.
func SendUDP(ctx context.Context, addr string, payload []byte) error {
	var d net.Dialer

	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return err
	}

	defer conn.Close()

	if _, err := conn.Write(payload); err != nil {
		return err
	}

	return nil
}

// MagicPacket builds a Wake-on-LAN packet: six 0xFF bytes then the MAC sixteen times.
func MagicPacket(mac net.HardwareAddr) ([]byte, error) {
	if len(mac) != 6 {
		return nil, fmt.Errorf("wake-on-lan needs a 6-byte MAC address, got %q", mac)
	}

	return append(bytes.Repeat([]byte{0xff}, 6), bytes.Repeat(mac, 16)...), nil
}

// MACAddress asks Kodi for its network MAC address. Kodi answers "Busy" while it looks the address up; that's reported as an error, so ask again later.
func (c *Client) MACAddress(ctx context.Context) (net.HardwareAddr, error) {
	const label = "Network.MacAddress"

	labels, err := c.XBMCGetInfoLabels(ctx, kodirpc.XBMCGetInfoLabelsParams{Labels: []string{label}})
	if err != nil {
		return nil, err
	}

	mac, err := net.ParseMAC(labels[label])
	if err != nil || len(mac) != 6 {
		// No quotes around Kodi's text: log output would escape them.
		if labels[label] == "" {
			return nil, errors.New("kodi reported no MAC address")
		}

		return nil, fmt.Errorf("kodi reported %s instead of a MAC address", labels[label])
	}

	return mac, nil
}

// SetMAC sets the address to wake, e.g. from configuration.
func (w *Waker) SetMAC(mac net.HardwareAddr) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.mac = mac
}

// MAC returns the address to wake, if known.
func (w *Waker) MAC() net.HardwareAddr {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.mac
}

// Learn reads the MAC address from Kodi while it's awake.
func (w *Waker) Learn(ctx context.Context) error {
	mac, err := w.Client.MACAddress(ctx)
	if err != nil {
		return err
	}

	w.SetMAC(mac)

	return nil
}

func (w *Waker) addr() string {
	if w.Broadcast == "" {
		return DefaultBroadcast
	}

	if _, _, err := net.SplitHostPort(w.Broadcast); err == nil {
		return w.Broadcast
	}

	return net.JoinHostPort(w.Broadcast, "9")
}

// Wake sends one magic packet.
func (w *Waker) Wake(ctx context.Context) error {
	mac := w.MAC()
	if mac == nil {
		return errors.New("wake-on-lan: MAC address unknown")
	}

	packet, err := MagicPacket(mac)
	if err != nil {
		return err
	}

	send := w.Send
	if send == nil {
		send = SendUDP
	}

	if err := send(ctx, w.addr(), packet); err != nil {
		return fmt.Errorf("wake-on-lan: sending to %s: %w", w.addr(), err)
	}

	return nil
}

// WakeAndWait wakes Kodi unless it already answers, then waits up to timeout for JSONRPC.Ping to succeed, repeating the packet in case one is lost.
func (w *Waker) WakeAndWait(ctx context.Context, timeout time.Duration) error {
	poll, resend := w.PollInterval, w.ResendInterval
	if poll <= 0 {
		poll = DefaultPollInterval
	}

	if resend <= 0 {
		resend = DefaultResendInterval
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if w.alive(ctx, poll) {
		return nil
	}

	var lastSent time.Time

	for {
		if time.Since(lastSent) >= resend {
			if err := w.Wake(ctx); err != nil {
				return err
			}

			lastSent = time.Now()
		}

		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("kodi did not wake within %s: %w", timeout, media.ErrUnreachable)
			}

			return ctx.Err()
		case <-time.After(poll):
		}

		if w.alive(ctx, poll) {
			return nil
		}
	}
}

func (w *Waker) alive(ctx context.Context, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := w.Client.JSONRPCPing(ctx)

	return err == nil
}
