package kodi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/coder/websocket"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
)

// ProbeVersion asks a Kodi for its version over its WebSocket port, which answers JSON-RPC without credentials, so a discovered instance can be described before the user has entered a password.
func ProbeVersion(ctx context.Context, host string, wsPort int) (string, error) {
	if wsPort == 0 {
		return "", errors.New("kodi: no WebSocket port announced")
	}

	c, resp, err := websocket.Dial(ctx, "ws://"+net.JoinHostPort(host, strconv.Itoa(wsPort))+"/jsonrpc", nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	if err != nil {
		return "", fmt.Errorf("kodi: %w", err)
	}

	defer func() { _ = c.CloseNow() }()

	if err := c.Write(ctx, websocket.MessageText, []byte(probeRequest)); err != nil {
		return "", fmt.Errorf("kodi: %w", err)
	}

	// Kodi may push notifications on the same socket; skip anything that isn't our reply.
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return "", fmt.Errorf("kodi: %w", err)
		}

		var reply struct {
			ID     *int                             `json:"id"`
			Result kodirpc.ApplicationPropertyValue `json:"result"`
		}

		if json.Unmarshal(data, &reply) != nil || reply.ID == nil || *reply.ID != 1 {
			continue
		}

		if v := formatVersion(reply.Result.Version); v != "" {
			return v, nil
		}

		return "", errors.New("kodi: no version in the reply")
	}
}

// "21.3", or with its pre-release tag, e.g. "22.0-rc1" or "22.0-beta2".
func formatVersion(v *kodirpc.ApplicationPropertyValueVersion) string {
	if v == nil {
		return ""
	}

	s := fmt.Sprintf("%d.%d", v.Major, v.Minor)

	switch tag := string(v.Tag); tag {
	case "", "stable":
	case "releasecandidate":
		s += "-rc" + v.Tagversion
	default:
		s += "-" + tag + v.Tagversion
	}

	return s
}
