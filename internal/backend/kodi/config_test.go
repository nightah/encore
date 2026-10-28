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
		"colons for a host":  {"//::", "0"},
		"no host":            {"http://", ""},
		"port but no host":   {"http://:8080/jsonrpc", ""},
		"path but no host":   {"http:///jsonrpc", ""},
		"opaque":             {"http:media.lan", ""},
		"no scheme":          {"media.lan", ""},
		"scheme typo":        {"media.lan:8080", ""},
		"unbracketed IPv6":   {"http://fe80::1/jsonrpc", ""},
		"bracketed nonsense": {"http://[kodi]/", ""},
		"WebSocket no host":  {"http://kodi/", "0"},
		"WebSocket colons":   {"http://kodi/", "ws://::"},
		"WebSocket port":     {"http://kodi/", "ws://:9090/jsonrpc"},
	} {
		_, err := ConfigFromURLs(urls[0], urls[1])
		assert.Error(t, err, name)
	}
}

func TestConfigFromURLsAccepts(t *testing.T) {
	for _, tt := range []struct {
		httpURL, wsURL, host string
	}{
		{"http://kodi/", "", "kodi"},
		{"https://media.lan:8443/jsonrpc", "wss://media.lan:9443/jsonrpc", "media.lan"},
		{"http://192.168.1.20:8080/jsonrpc", "ws://192.168.1.20:9090/jsonrpc", "192.168.1.20"},
		{"http://[fe80::1]:8080/jsonrpc", "ws://[fe80::1]:9090/jsonrpc", "fe80::1"},
		{"http://[fe80::1%25eth0]/jsonrpc", "", "fe80::1%eth0"},
		{"http://[::1]/", "", "::1"},
		{"//kodi:8080/jsonrpc", "", "kodi"},
		{"http://kodi:secret@media.lan/", "", "media.lan"}, // trufflehog:ignore
	} {
		cfg, err := ConfigFromURLs(tt.httpURL, tt.wsURL)
		require.NoError(t, err, tt.httpURL)
		assert.Equal(t, tt.host, cfg.Host, tt.httpURL)
	}
}

func TestConfigFromURLsDoesNotLeakPasswords(t *testing.T) {
	for name, urls := range map[string][2]string{
		"HTTP URL":            {"http://kodi:hunter2@media.lan:port/", ""},            // trufflehog:ignore
		"WebSocket URL":       {"http://media.lan/", "ws://kodi:hunter2@media.lan:%"}, // trufflehog:ignore
		"HTTP URL no host":    {"http://kodi:hunter2@:8080/", ""},                     // trufflehog:ignore
		"HTTP URL colons":     {"http://kodi:hunter2@::/", ""},                        // trufflehog:ignore
		"WebSocket URL colon": {"http://media.lan/", "ws://kodi:hunter2@::/"},         // trufflehog:ignore
	} {
		_, err := ConfigFromURLs(urls[0], urls[1])

		if assert.Error(t, err, name) {
			assert.NotContains(t, err.Error(), "hunter2", "%s: the error is logged, so it mustn't carry the password", name)
		}
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

func TestRedactURL(t *testing.T) {
	withPassword := (&url.URL{Scheme: "http", User: url.UserPassword("kodi", "hunter2"), Host: "kodi", Path: "/jsonrpc"}).String()
	got := RedactURL(withPassword)
	assert.NotContains(t, got, "hunter2")
	assert.Equal(t, (&url.URL{Scheme: "http", User: url.UserPassword("kodi", "xxxxx"), Host: "kodi", Path: "/jsonrpc"}).String(), got)

	assert.Equal(t, "http://kodi/jsonrpc", RedactURL("http://kodi/jsonrpc"))
	assert.Equal(t, "http://%zz", RedactURL("http://%zz"), "an unparsable URL without credentials is shown as given")
	assert.Equal(t, "kodi:8080", RedactURL("kodi:8080"), "nor is one without an authority")
	assert.Equal(t, "http://kodi:xxxxx@[::1/jsonrpc", RedactURL("http://kodi:hunter2@[::1/jsonrpc"), "an unparsable URL still hides its password") // trufflehog:ignore
	assert.Equal(t, "ws://kodi:xxxxx@media.lan:80a?x=@", RedactURL("ws://kodi:hunter2@media.lan:80a?x=@"), "only the authority's @ counts")        // trufflehog:ignore
	assert.Equal(t, "http://kodi@[::1", RedactURL("http://kodi@[::1"), "a user name alone isn't secret")
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

		assert.NotEmpty(t, cfg.Host, "an accepted URL names a host")
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
