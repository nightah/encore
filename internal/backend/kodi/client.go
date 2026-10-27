package kodi

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
)

// NewClient returns a Client for cfg. It connects lazily; call Run to keep the WebSocket and notifications up.
func NewClient(cfg Config, log *slog.Logger) *Client {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	c := &Client{t: NewTransport(cfg, log), log: log}
	c.Client = kodirpc.Client{Caller: gate{c}}

	// A reconnect may be a different Kodi build, so look the version up again.
	c.t.Listen(Listener{State: func(bool) {
		c.mu.Lock()
		c.known = false
		c.mu.Unlock()
	}})

	return c
}

// Transport returns the underlying transport.
func (c *Client) Transport() *Transport { return c.t }

// Run keeps the WebSocket connected until ctx is done.
func (c *Client) Run(ctx context.Context) error { return c.t.Run(ctx) }

// Listen registers a transport listener; see Transport.Listen.
func (c *Client) Listen(l Listener) (cancel func()) { return c.t.Listen(l) }

// APIVersion returns the server's JSON-RPC API version, asking JSONRPC.Version once per connection.
func (c *Client) APIVersion(ctx context.Context) (kodirpc.APIVersion, error) {
	c.mu.Lock()
	v, known := c.version, c.known
	c.mu.Unlock()

	if known {
		return v, nil
	}

	res, err := kodirpc.Call[kodirpc.JSONRPCVersionParams, kodirpc.JSONRPCVersionResult](ctx, c.t, kodirpc.MethodJSONRPCVersion, kodirpc.JSONRPCVersionParams{})
	if err != nil {
		return kodirpc.APIVersion{}, err
	}

	v = kodirpc.APIVersion{Major: res.Version.Major, Minor: res.Version.Minor, Patch: res.Version.Patch}

	c.mu.Lock()
	c.version, c.known = v, true
	c.mu.Unlock()

	c.log.Debug("kodi api version", "version", v.String())

	return v, nil
}

// Supports reports whether the server provides method. It assumes yes when the version can't be determined, letting the call itself fail.
func (c *Client) Supports(ctx context.Context, method string) bool {
	info, ok := kodirpc.Methods[method]
	if !ok {
		return true
	}

	v, err := c.APIVersion(ctx)
	if err != nil {
		return true
	}

	return info.AvailableIn(v)
}

func (g gate) Call(ctx context.Context, method string, params, result any) error {
	switch method {
	case kodirpc.MethodJSONRPCVersion, kodirpc.MethodJSONRPCPing:
	default:
		if !g.c.Supports(ctx, method) {
			return fmt.Errorf("%w: %s", ErrUnsupported, method)
		}
	}

	return g.c.t.Call(ctx, method, params, result)
}
