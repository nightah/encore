package kodi

import (
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFromURLsRejects(t *testing.T) {
	testCases := []struct {
		name    string
		httpURL string
		wsURL   string
	}{
		{name: "ShouldRejectBadHTTPURL", httpURL: "http://kodi:8080/%zz"},
		{name: "ShouldRejectBadHTTPPort", httpURL: "http://kodi:eighty/"},
		{name: "ShouldRejectBadWebSocketURL", httpURL: "http://kodi/", wsURL: "ws://kodi:9090/%zz"},
		{name: "ShouldRejectBadWebSocketPort", httpURL: "http://kodi/", wsURL: "ws://kodi:ninety/"},
		{name: "ShouldRejectColonsForAHost", httpURL: "//::", wsURL: "0"},
		{name: "ShouldRejectNoHost", httpURL: "http://"},
		{name: "ShouldRejectPortButNoHost", httpURL: "http://:8080/jsonrpc"},
		{name: "ShouldRejectPathButNoHost", httpURL: "http:///jsonrpc"},
		{name: "ShouldRejectOpaqueURL", httpURL: "http:media.lan"},
		{name: "ShouldRejectNoScheme", httpURL: "media.lan"},
		{name: "ShouldRejectSchemeTypo", httpURL: "media.lan:8080"},
		{name: "ShouldRejectUnbracketedIPv6", httpURL: "http://fe80::1/jsonrpc"},
		{name: "ShouldRejectBracketedNonsense", httpURL: "http://[kodi]/"},
		{name: "ShouldRejectWebSocketWithoutHost", httpURL: "http://kodi/", wsURL: "0"},
		{name: "ShouldRejectWebSocketColons", httpURL: "http://kodi/", wsURL: "ws://::"},
		{name: "ShouldRejectWebSocketPortWithoutHost", httpURL: "http://kodi/", wsURL: "ws://:9090/jsonrpc"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConfigFromURLs(tc.httpURL, tc.wsURL)
			assert.Error(t, err)
		})
	}
}

func TestConfigFromURLsAccepts(t *testing.T) {
	testCases := []struct {
		name     string
		httpURL  string
		wsURL    string
		expected string
	}{
		{name: "ShouldAcceptHostOnly", httpURL: "http://kodi/", expected: "kodi"},
		{name: "ShouldAcceptHTTPSAndWSS", httpURL: "https://media.lan:8443/jsonrpc", wsURL: "wss://media.lan:9443/jsonrpc", expected: "media.lan"},
		{name: "ShouldAcceptIPv4", httpURL: "http://192.168.1.20:8080/jsonrpc", wsURL: "ws://192.168.1.20:9090/jsonrpc", expected: "192.168.1.20"},
		{name: "ShouldAcceptBracketedIPv6", httpURL: "http://[fe80::1]:8080/jsonrpc", wsURL: "ws://[fe80::1]:9090/jsonrpc", expected: "fe80::1"},
		{name: "ShouldAcceptIPv6Zone", httpURL: "http://[fe80::1%25eth0]/jsonrpc", expected: "fe80::1%eth0"},
		{name: "ShouldAcceptIPv6Loopback", httpURL: "http://[::1]/", expected: "::1"},
		{name: "ShouldAcceptSchemeRelativeURL", httpURL: "//kodi:8080/jsonrpc", expected: "kodi"},
		{name: "ShouldAcceptCredentials", httpURL: "http://kodi:secret@media.lan/", expected: "media.lan"}, // trufflehog:ignore
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ConfigFromURLs(tc.httpURL, tc.wsURL)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, cfg.Host)
		})
	}
}

func TestConfigFromURLsDoesNotLeakPasswords(t *testing.T) {
	testCases := []struct {
		name    string
		httpURL string
		wsURL   string
	}{
		{name: "ShouldHidePasswordInHTTPURL", httpURL: "http://kodi:hunter2@media.lan:port/"},                              // trufflehog:ignore
		{name: "ShouldHidePasswordInWebSocketURL", httpURL: "http://media.lan/", wsURL: "ws://kodi:hunter2@media.lan:%"},   // trufflehog:ignore
		{name: "ShouldHidePasswordInHTTPURLWithoutHost", httpURL: "http://kodi:hunter2@:8080/"},                            // trufflehog:ignore
		{name: "ShouldHidePasswordInHTTPURLWithColons", httpURL: "http://kodi:hunter2@::/"},                                // trufflehog:ignore
		{name: "ShouldHidePasswordInWebSocketURLWithColons", httpURL: "http://media.lan/", wsURL: "ws://kodi:hunter2@::/"}, // trufflehog:ignore
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConfigFromURLs(tc.httpURL, tc.wsURL)

			if assert.Error(t, err) {
				assert.NotContains(t, err.Error(), "hunter2", "the error is logged, so it mustn't carry the password")
			}
		})
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
	testCases := []struct {
		name     string
		have     string
		expected string
	}{
		{name: "ShouldHidePassword", have: (&url.URL{Scheme: "http", User: url.UserPassword("kodi", "hunter2"), Host: "kodi", Path: "/jsonrpc"}).String(), expected: (&url.URL{Scheme: "http", User: url.UserPassword("kodi", "xxxxx"), Host: "kodi", Path: "/jsonrpc"}).String()},
		{name: "ShouldKeepURLWithoutCredentials", have: "http://kodi/jsonrpc", expected: "http://kodi/jsonrpc"},
		{name: "ShouldShowUnparsableURLWithoutCredentialsAsGiven", have: "http://%zz", expected: "http://%zz"},
		{name: "ShouldShowURLWithoutAuthorityAsGiven", have: ":8080", expected: ":8080"},
		{name: "ShouldHidePasswordInUnparsableURL", have: "http://kodi:hunter2@[::1/jsonrpc", expected: "http://kodi:xxxxx@[::1/jsonrpc"},   // trufflehog:ignore
		{name: "ShouldOnlyCountTheAuthorityAt", have: "ws://kodi:hunter2@media.lan:80a?x=@", expected: "ws://kodi:xxxxx@media.lan:80a?x=@"}, // trufflehog:ignore
		{name: "ShouldKeepUserNameAlone", have: "http://kodi@[::1", expected: "http://kodi@[::1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := RedactURL(tc.have)

			assert.NotContains(t, actual, "hunter2")
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestConfigFromURLsPortRange(t *testing.T) {
	testCases := []struct {
		name       string
		have       string
		expected   int
		outOfRange bool
	}{
		{name: "ShouldAcceptLowestPort", have: "1", expected: 1},
		{name: "ShouldAcceptHighestPort", have: "65535", expected: 65535},
		{name: "ShouldRejectPortOneOver", have: "65536", outOfRange: true},
		{name: "ShouldRejectPortFarOver", have: "99999", outOfRange: true},
		{name: "ShouldRejectPortThatOverflows", have: "99999999999999999999", outOfRange: true},
		{name: "ShouldRejectPortZero", have: "0", outOfRange: true},
		{name: "ShouldRejectNegativePort", have: "-1"},
		{name: "ShouldRejectNonNumericPort", have: "http"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("ShouldApplyToHTTPURL", func(t *testing.T) {
				cfg, err := ConfigFromURLs("http://kodi:hunter2@media.lan:"+tc.have+"/jsonrpc", "") // trufflehog:ignore
				assertPort(t, tc.expected, tc.outOfRange, cfg.Port, err, ErrHTTPPort, "Kodi URL")
			})

			t.Run("ShouldApplyToWebSocketURL", func(t *testing.T) {
				cfg, err := ConfigFromURLs("http://media.lan/jsonrpc", "ws://kodi:hunter2@media.lan:"+tc.have+"/jsonrpc") // trufflehog:ignore
				assertPort(t, tc.expected, tc.outOfRange, cfg.WebSocketPort, err, ErrWebSocketPort, "Kodi WebSocket URL")
			})
		})
	}
}

// assertPort checks an accepted port is kept, and a refused one names the setting without the URL or its password.
func assertPort(t *testing.T, want int, outOfRange bool, got int, err, sentinel error, setting string) {
	t.Helper()

	if want != 0 {
		require.NoError(t, err)
		assert.Equal(t, want, got)

		return
	}

	require.Error(t, err)
	assert.Contains(t, err.Error(), setting, "the error names the setting")
	assert.NotContains(t, err.Error(), "media.lan", "the error never carries the URL")
	assert.NotContains(t, err.Error(), "hunter2", "the error never carries the password")

	if outOfRange {
		require.ErrorIs(t, err, sentinel)
	}
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
		assert.LessOrEqual(t, cfg.Port, maxPort)
		assert.LessOrEqual(t, cfg.WebSocketPort, maxPort)
		assert.Positive(t, cfg.Timeout)
		assert.GreaterOrEqual(t, cfg.ReconnectMax, cfg.ReconnectMin)

		for _, s := range []string{cfg.HTTPURL(), cfg.WebSocketURL()} {
			if u, err := url.Parse(s); err == nil {
				assert.Nil(t, u.User, "credentials never appear in an endpoint URL")
			}
		}
	})
}
