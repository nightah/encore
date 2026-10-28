package kodi

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ConfigFromURLs builds a Config from an HTTP JSON-RPC URL such as http://kodi:8080/jsonrpc and an optional WebSocket URL; credentials in the HTTP URL's userinfo are used for basic auth.
func ConfigFromURLs(httpURL, wsURL string) (Config, error) {
	u, err := parseURL(httpURL)
	if err != nil {
		return Config{}, fmt.Errorf("parsing Kodi URL: %w", err)
	}

	cfg := Config{Scheme: u.Scheme, Host: u.Hostname(), Path: u.Path}

	if cfg.Port, err = portOf(u, DefaultHTTPPort); err != nil {
		return Config{}, err
	}

	if u.User != nil {
		cfg.Username = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}

	if wsURL == "" {
		return cfg.withDefaults(), nil
	}

	w, err := parseURL(wsURL)
	if err != nil {
		return Config{}, fmt.Errorf("parsing Kodi WebSocket URL: %w", err)
	}

	if cfg.WebSocketPort, err = portOf(w, DefaultWebSocketPort); err != nil {
		return Config{}, err
	}

	return cfg.withDefaults(), nil
}

// parseURL is url.Parse with errors that leave out the URL, which may hold a password, and that refuses a URL without a host to connect to.
func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)

	var urlErr *url.Error

	switch {
	case errors.As(err, &urlErr):
		return nil, urlErr.Err
	case err == nil && !hasHost(u):
		return nil, errNoHost
	}

	return u, err
}

// hasHost reports whether u names a host to connect to: not empty, and a bracketed IP address if it holds colons ("//::" leaves the host ":").
func hasHost(u *url.URL) bool {
	h := u.Hostname()
	if h == "" {
		return false
	}

	if !strings.Contains(h, ":") {
		return true
	}

	ip, _, _ := strings.Cut(h, "%")

	return strings.HasPrefix(u.Host, "[") && net.ParseIP(ip) != nil
}

func portOf(u *url.URL, fallback int) (int, error) {
	if u.Port() == "" {
		return fallback, nil
	}

	p, err := strconv.Atoi(u.Port())
	if err != nil {
		return 0, fmt.Errorf("invalid port in %q: %w", u.Redacted(), err)
	}

	return p, nil
}

// RedactURL hides the password in a Kodi URL as url.URL.Redacted does, also in a URL that doesn't parse, so the URL can be shown in an error.
func RedactURL(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		if u.User == nil {
			return raw
		}

		return u.Redacted()
	}

	// An unparsable URL can still carry a password; hide it as Redacted would.
	prefix, rest, ok := strings.Cut(raw, "//")
	if !ok {
		return raw
	}

	authority := rest
	if i := strings.IndexAny(rest, "/?#"); i >= 0 {
		authority = rest[:i]
	}

	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return raw
	}

	user, _, hasPassword := strings.Cut(authority[:at], ":")
	if !hasPassword {
		return raw
	}

	return prefix + "//" + user + ":xxxxx" + rest[at:]
}

func (c Config) withDefaults() Config {
	if c.Scheme == "" {
		c.Scheme = "http"
	}

	if c.Port == 0 {
		c.Port = DefaultHTTPPort
	}

	if c.WebSocketPort == 0 {
		c.WebSocketPort = DefaultWebSocketPort
	}

	if c.Path == "" {
		c.Path = DefaultPath
	}

	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}

	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = DefaultHeartbeatInterval
	}

	if c.ReconnectMin <= 0 {
		c.ReconnectMin = DefaultReconnectMin
	}

	if c.ReconnectMax < c.ReconnectMin {
		c.ReconnectMax = max(DefaultReconnectMax, c.ReconnectMin)
	}

	return c
}

// HTTPURL is the HTTP JSON-RPC endpoint, without credentials.
func (c Config) HTTPURL() string {
	c = c.withDefaults()
	u := url.URL{Scheme: c.Scheme, Host: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Path: c.Path}

	return u.String()
}

// WebSocketURL is the WebSocket JSON-RPC endpoint.
func (c Config) WebSocketURL() string {
	c = c.withDefaults()

	scheme := "ws"
	if c.Scheme == "https" {
		scheme = "wss"
	}

	u := url.URL{Scheme: scheme, Host: net.JoinHostPort(c.Host, strconv.Itoa(c.WebSocketPort)), Path: c.Path}

	return u.String()
}
