package kodi

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBrowser map[string][]ServiceEntry

func (f fakeBrowser) Browse(ctx context.Context, service string, _ time.Duration, entries chan<- ServiceEntry) error {
	answers, ok := f[service]
	if !ok {
		return errors.New("multicast unavailable")
	}

	for _, e := range answers {
		select {
		case entries <- e:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

func TestDiscoverMergesServices(t *testing.T) {
	lounge := []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("fe80::1")}

	d := Discoverer{Browser: fakeBrowser{
		ServiceJSONRPC: {
			{Instance: "Kodi (lounge)", Host: "lounge.local.", Addrs: lounge, Port: 9090},
			{Instance: "Kodi (bedroom)", Host: "bedroom.local.", Port: 9090},
		},
		ServiceJSONRPCHTTP: {
			{Instance: "Kodi (lounge)", Host: "lounge.local.", Addrs: lounge[:1], Port: 8080},
		},
	}}

	got, err := d.Discover(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, Instance{Name: "Kodi (bedroom)", Host: "bedroom.local.", WebSocketPort: 9090}, got[0])
	assert.Equal(t, Instance{Name: "Kodi (lounge)", Host: "lounge.local.", Addrs: lounge, Port: 8080, WebSocketPort: 9090}, got[1])

	cfg := got[1].Config()
	assert.Equal(t, "http://192.168.1.20:8080/jsonrpc", cfg.HTTPURL())
	assert.Equal(t, "ws://192.168.1.20:9090/jsonrpc", cfg.WebSocketURL())

	assert.Equal(t, "http://bedroom.local:8080/jsonrpc", got[0].Config().HTTPURL(), "falls back to the host name and default port")
}

func TestDiscoverMergesServicesByHost(t *testing.T) {
	ip := []net.IP{net.ParseIP("10.10.10.3")}

	d := Discoverer{Browser: fakeBrowser{
		ServiceJSONRPC: {
			{Instance: "HikariNUC jsonrpc-tcp", Host: "HikariNUC.local.", Addrs: ip, Port: 9090},
		},
		ServiceJSONRPCHTTP: {
			{Instance: "HikariNUC jsonrpc-http", Host: "hikarinuc.local.", Addrs: ip, Port: 80},
		},
	}}

	got, err := d.Discover(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "HikariNUC", got[0].Name)
	assert.Equal(t, 80, got[0].Port)
	assert.Equal(t, 9090, got[0].WebSocketPort)
}

func TestDiscoverToleratesOneFailedBrowse(t *testing.T) {
	got, err := Discoverer{Browser: fakeBrowser{ServiceJSONRPCHTTP: {{Instance: "Kodi", Port: 8080}}}}.Discover(t.Context())
	require.NoError(t, err)
	assert.Len(t, got, 1)

	_, err = Discoverer{Browser: fakeBrowser{}}.Discover(t.Context())
	require.Error(t, err)
}

func TestInstanceName(t *testing.T) {
	cases := map[string]string{
		`Kodi\ \(lounge\)._xbmc-jsonrpc._tcp.local.`: "Kodi (lounge)",
		`Kodi\032\(den\)._xbmc-jsonrpc._tcp.local.`:  "Kodi (den)",
		`LibreELEC._xbmc-jsonrpc._tcp.local.`:        "LibreELEC",
		`odd\`:                                       `odd\`,
	}

	for in, want := range cases {
		assert.Equal(t, want, instanceName(in, ServiceJSONRPC), in)
	}
}

func TestIsService(t *testing.T) {
	assert.True(t, isService(`Kodi\ \(lounge\)._xbmc-jsonrpc._tcp.local.`, ServiceJSONRPC))
	assert.True(t, isService(`HikariNUC\ jsonrpc-http._xbmc-jsonrpc-h._tcp.local.`, ServiceJSONRPCHTTP))
	assert.False(t, isService(`HikariNUC\ jsonrpc-http._xbmc-jsonrpc-h._tcp.local.`, ServiceJSONRPC))
	assert.False(t, isService(`BRAVIA-2015-d4377b._googlecast._tcp.local.`, ServiceJSONRPC))
}

// TestMDNSLive browses the real network; run with ENCORE_TEST_MDNS=1.
func TestMDNSLive(t *testing.T) {
	if os.Getenv("ENCORE_TEST_MDNS") == "" {
		t.Skip("set ENCORE_TEST_MDNS=1 to browse the local network")
	}

	got, err := Discoverer{Timeout: 3 * time.Second}.Discover(t.Context())
	require.NoError(t, err)

	for _, inst := range got {
		t.Logf("found %q at %s (http %d, ws %d)", inst.Name, inst.Host, inst.Port, inst.WebSocketPort)
	}
}
