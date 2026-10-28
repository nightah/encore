package kodi

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromURLsRejects(t *testing.T) {
	for name, urls := range map[string][2]string{
		"bad HTTP URL":       {"http://kodi:8080/%zz", ""},
		"bad HTTP port":      {"http://kodi:eighty/", ""},
		"bad WebSocket URL":  {"http://kodi/", "ws://kodi:9090/%zz"},
		"bad WebSocket port": {"http://kodi/", "ws://kodi:ninety/"},
	} {
		_, err := ConfigFromURLs(urls[0], urls[1])
		require.Error(t, err, name)
	}
}

func TestConfigFromURLsDoesNotLeakPasswords(t *testing.T) {
	for name, urls := range map[string][2]string{
		"HTTP URL":      {"http://kodi:hunter2@media.lan:port/", ""},            // trufflehog:ignore
		"WebSocket URL": {"http://media.lan/", "ws://kodi:hunter2@media.lan:%"}, // trufflehog:ignore
	} {
		_, err := ConfigFromURLs(urls[0], urls[1])

		require.Error(t, err, name)
		assert.NotContains(t, err.Error(), "hunter2", "%s: the error is logged, so it mustn't carry the password", name)
	}
}

func TestConfigDefaults(t *testing.T) {
	c := Config{Host: "media.lan"}.withDefaults()

	assert.Equal(t, Config{
		Scheme: "http", Host: "media.lan", Port: DefaultHTTPPort, WebSocketPort: DefaultWebSocketPort, Path: DefaultPath,
		Timeout: DefaultTimeout, HeartbeatInterval: DefaultHeartbeatInterval, ReconnectMin: DefaultReconnectMin, ReconnectMax: DefaultReconnectMax,
	}, c)

	c = Config{ReconnectMin: 10 * time.Minute, ReconnectMax: time.Second}.withDefaults()
	assert.Equal(t, 10*time.Minute, c.ReconnectMax, "the most a reconnect waits is never less than the least")

	c = Config{ReconnectMin: time.Second, ReconnectMax: time.Hour, Timeout: time.Second}.withDefaults()
	assert.Equal(t, time.Hour, c.ReconnectMax, "set values are kept")
	assert.Equal(t, time.Second, c.Timeout)
}

func FuzzConfigFromURLs(f *testing.F) {
	f.Add("http://kodi:secret@media.lan:8080/jsonrpc", "ws://media.lan:9090/jsonrpc") // trufflehog:ignore
	f.Add("https://[fe80::1]/", "")
	f.Add("media.lan", "wss://media.lan")
	f.Add(":", "::")

	f.Fuzz(func(t *testing.T, httpURL, wsURL string) {
		cfg, err := ConfigFromURLs(httpURL, wsURL)
		if err != nil {
			return
		}

		assert.Positive(t, cfg.Port)
		assert.Positive(t, cfg.WebSocketPort)
		assert.Positive(t, cfg.Timeout)
		assert.GreaterOrEqual(t, cfg.ReconnectMax, cfg.ReconnectMin)

		for _, s := range []string{cfg.HTTPURL(), cfg.WebSocketURL()} {
			if u, err := url.Parse(s); err == nil {
				assert.Nil(t, u.User, "credentials never appear in an endpoint URL")
			}
		}
	})
}
