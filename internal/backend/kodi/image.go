package kodi

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Image fetches artwork from Kodi's web server, which serves an image:// reference at /image/<url-encoded reference> behind the same credentials as JSON-RPC. Only image:// references are accepted, so this can't be used to fetch anything else through Kodi. The caller closes the body.
func (c *Client) Image(ctx context.Context, ref string) (body io.ReadCloser, contentType string, err error) {
	if !strings.HasPrefix(ref, "image://") || len(ref) > maxImageRef {
		return nil, "", ErrNotImage
	}

	cfg := c.t.cfg.withDefaults()
	target := cfg.Scheme + "://" + net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)) + "/image/" + url.QueryEscape(ref)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, http.NoBody)
	if err != nil {
		return nil, "", fmt.Errorf("kodi: image: %w", err)
	}

	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}

	resp, err := c.t.http.Do(req)
	if err != nil {
		return nil, "", unreachable("image", err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		_ = resp.Body.Close()

		return nil, "", unreachable("image", ErrUnauthorized)
	case resp.StatusCode != http.StatusOK:
		_ = resp.Body.Close()

		return nil, "", fmt.Errorf("kodi: image: HTTP %s", resp.Status)
	}

	return resp.Body, resp.Header.Get("Content-Type"), nil
}
