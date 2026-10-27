package kodi

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/ipv4"
)

type fakeBrowser struct {
	entries []ServiceEntry
	err     error
}

func (f fakeBrowser) Browse(context.Context, []string, time.Duration) ([]ServiceEntry, error) {
	return f.entries, f.err
}

// packetBrowser resolves canned mDNS packets as MDNSBrowser would.
type packetBrowser [][]byte

func (p packetBrowser) Browse(_ context.Context, services []string, _ time.Duration) ([]ServiceEntry, error) {
	answers := newMDNSAnswers()
	for _, packet := range p {
		answers.add(packet)
	}

	return answers.entries(services), nil
}

// repeatedAnswers are two Kodi instances' answers as a MikroTik mDNS repeater passed them on: one packet per Kodi, answering both service types, with the host's addresses between the two services' records. Only the IPv6 prefix differs from the capture.
func repeatedAnswers(t *testing.T) [][]byte {
	t.Helper()

	f, err := os.Open("testdata/mdns/repeated-answers.hex")
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	var out [][]byte

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		b, err := hex.DecodeString(sc.Text())
		require.NoError(t, err)

		out = append(out, b)
	}

	require.NoError(t, sc.Err())

	return out
}

func pack(t *testing.T, response bool, rrs ...string) []byte {
	t.Helper()

	msg := new(dns.Msg)
	msg.Response = response

	for _, s := range rrs {
		rr, err := dns.NewRR(s)
		require.NoError(t, err)

		msg.Answer = append(msg.Answer, rr)
	}

	b, err := msg.Pack()
	require.NoError(t, err)

	return b
}

func TestDiscoverMergesServices(t *testing.T) {
	lounge := []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("fe80::1")}

	d := Discoverer{Browser: fakeBrowser{entries: []ServiceEntry{
		{Service: ServiceJSONRPC, Instance: "Kodi (lounge)", Host: "lounge.local.", Addrs: lounge, Port: 9090},
		{Service: ServiceJSONRPC, Instance: "Kodi (bedroom)", Host: "bedroom.local.", Port: 9090},
		{Service: ServiceJSONRPCHTTP, Instance: "Kodi (lounge)", Host: "lounge.local.", Addrs: lounge[:1], Port: 8080},
	}}}

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

	d := Discoverer{Browser: fakeBrowser{entries: []ServiceEntry{
		{Service: ServiceJSONRPC, Instance: "HikariNUC jsonrpc-tcp", Host: "HikariNUC.local.", Addrs: ip, Port: 9090},
		{Service: ServiceJSONRPCHTTP, Instance: "HikariNUC jsonrpc-http", Host: "hikarinuc.local.", Addrs: ip, Port: 80},
	}}}

	got, err := d.Discover(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "HikariNUC", got[0].Name)
	assert.Equal(t, 80, got[0].Port)
	assert.Equal(t, 9090, got[0].WebSocketPort)
}

func TestDiscoverBrowseError(t *testing.T) {
	_, err := Discoverer{Browser: fakeBrowser{err: errors.New("multicast unavailable")}}.Discover(t.Context())
	require.Error(t, err)
}

// A repeater answers both service types in one packet per Kodi, which hashicorp/mdns resolved to nothing.
func TestDiscoverRepeatedAnswers(t *testing.T) {
	got, err := Discoverer{Browser: packetBrowser(repeatedAnswers(t))}.Discover(t.Context())
	require.NoError(t, err)

	assert.Equal(t, []Instance{
		{Name: "HikariNUC", Host: "HikariNUC.local.", Addrs: []net.IP{net.ParseIP("10.10.10.3").To4(), net.ParseIP("2001:db8::c23f:d5ff:fe6a:5dd3")}, Port: 80, WebSocketPort: 9090},
		{Name: "MisatoNUC", Host: "MisatoNUC.local.", Addrs: []net.IP{net.ParseIP("10.10.10.2").To4(), net.ParseIP("2001:db8::7270:fcff:fe0b:c3b1")}, Port: 80, WebSocketPort: 9090},
	}, got)
}

func TestMDNSAnswersAcrossPackets(t *testing.T) {
	answers := newMDNSAnswers()

	answers.add(pack(t, true,
		`_xbmc-jsonrpc._tcp.local. 4500 IN PTR Kodi\ \(den\)._xbmc-jsonrpc._tcp.local.`,
		`Kodi\ \(den\)._xbmc-jsonrpc._tcp.local. 120 IN SRV 0 0 9090 Den.local.`,
	))

	got := answers.entries([]string{ServiceJSONRPC})
	require.Len(t, got, 1)
	assert.Empty(t, got[0].Addrs, "the address hasn't arrived yet")

	q := answers.query([]string{ServiceJSONRPC})
	assert.Equal(t, []dns.Question{
		{Name: "_xbmc-jsonrpc._tcp.local.", Qtype: dns.TypePTR, Qclass: dns.ClassINET},
		{Name: "den.local.", Qtype: dns.TypeA, Qclass: dns.ClassINET},
		{Name: "den.local.", Qtype: dns.TypeAAAA, Qclass: dns.ClassINET},
	}, q.Question, "asks again for the missing address")

	answers.add(pack(t, true, `den.local. 120 IN A 192.168.1.30`))

	got = answers.entries([]string{ServiceJSONRPC})
	assert.Equal(t, []ServiceEntry{{Service: ServiceJSONRPC, Instance: "Kodi (den)", Host: "Den.local.", Addrs: []net.IP{net.ParseIP("192.168.1.30").To4()}, Port: 9090}}, got)
	assert.Len(t, answers.query([]string{ServiceJSONRPC}).Question, 1)
}

func TestMDNSAnswersIgnoresQueriesAndGoodbyes(t *testing.T) {
	answers := newMDNSAnswers()

	// Another host's query carrying a known answer is not an answer.
	answers.add(pack(t, false, `Kodi._xbmc-jsonrpc._tcp.local. 120 IN SRV 0 0 9090 kodi.local.`))
	assert.Empty(t, answers.entries([]string{ServiceJSONRPC}))

	answers.add(pack(t, true,
		`_xbmc-jsonrpc._tcp.local. 4500 IN PTR Kodi._xbmc-jsonrpc._tcp.local.`,
		`_xbmc-jsonrpc._tcp.local. 4500 IN PTR Gone._xbmc-jsonrpc._tcp.local.`,
		`Gone._xbmc-jsonrpc._tcp.local. 120 IN SRV 0 0 9090 gone.local.`,
		`_xbmc-jsonrpc._tcp.local. 0 IN PTR Gone._xbmc-jsonrpc._tcp.local.`,
		`BRAVIA._googlecast._tcp.local. 120 IN SRV 0 0 8009 bravia.local.`,
	))
	assert.Empty(t, answers.entries([]string{ServiceJSONRPC}), "Kodi has no SRV record yet and Gone said goodbye")

	answers.add(pack(t, true, `kodi._XBMC-JSONRPC._tcp.local. 120 IN SRV 0 0 9090 kodi.local.`))
	got := answers.entries([]string{ServiceJSONRPC})
	require.Len(t, got, 1, "names match whatever their case")
	assert.Equal(t, "kodi", got[0].Instance)
	assert.Equal(t, 9090, got[0].Port)
}

func TestInstanceName(t *testing.T) {
	cases := map[string]string{
		`Kodi\ \(lounge\)._xbmc-jsonrpc._tcp.local.`: "Kodi (lounge)",
		`Kodi\032\(den\)._xbmc-jsonrpc._tcp.local.`:  "Kodi (den)",
		`Lounge\ 2\.0._xbmc-jsonrpc._tcp.local.`:     "Lounge 2.0",
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

// TestMDNSBrowserLoopback browses the loopback interface, where a fake responder answers with the repeated packets.
func TestMDNSBrowserLoopback(t *testing.T) {
	lo := loopback(t)
	answers := repeatedAnswers(t)

	conn, err := (&net.ListenConfig{Control: reuseMDNSPort}).ListenPacket(t.Context(), "udp4", ":5353")
	if err != nil {
		t.Skipf("can't share the mDNS port: %v", err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	p := ipv4.NewPacketConn(conn)
	if err := p.JoinGroup(lo, &net.UDPAddr{IP: mdnsGroupV4}); err != nil {
		t.Skipf("can't join the mDNS group on %s: %v", lo.Name, err)
	}

	require.NoError(t, p.SetMulticastInterface(lo))
	require.NoError(t, p.SetMulticastLoopback(true))

	go func() {
		buf := make([]byte, 9000)
		group := &net.UDPAddr{IP: mdnsGroupV4, Port: mdnsPort}

		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}

			msg := new(dns.Msg)
			if msg.Unpack(buf[:n]) != nil || msg.Response {
				continue
			}

			for _, a := range answers {
				_, _ = conn.WriteTo(a, group)
			}
		}
	}()

	got, err := Discoverer{Browser: MDNSBrowser{Interface: lo}, Timeout: 500 * time.Millisecond}.Discover(t.Context())
	require.NoError(t, err)

	names := map[string][2]int{}
	for _, inst := range got {
		names[inst.Name] = [2]int{inst.Port, inst.WebSocketPort}
	}

	assert.Equal(t, [2]int{80, 9090}, names["HikariNUC"])
	assert.Equal(t, [2]int{80, 9090}, names["MisatoNUC"])
}

func loopback(t *testing.T) *net.Interface {
	t.Helper()

	ifaces, err := net.Interfaces()
	require.NoError(t, err)

	for i := range ifaces {
		if ifaces[i].Flags&net.FlagLoopback != 0 && ifaces[i].Flags&net.FlagUp != 0 {
			return &ifaces[i]
		}
	}

	t.Skip("no loopback interface")

	return nil
}

// TestMDNSLive browses the real network; run with ENCORE_TEST_MDNS=1.
func TestMDNSLive(t *testing.T) {
	if os.Getenv("ENCORE_TEST_MDNS") == "" {
		t.Skip("set ENCORE_TEST_MDNS=1 to browse the local network")
	}

	got, err := Discoverer{Timeout: 3 * time.Second}.Discover(t.Context())
	require.NoError(t, err)

	for _, inst := range got {
		t.Logf("found %q at %s %v (http %d, ws %d)", inst.Name, inst.Host, inst.Addrs, inst.Port, inst.WebSocketPort)
	}
}
