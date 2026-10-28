package kodi

import (
	"encoding/binary"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var bothServices = []string{ServiceJSONRPC, ServiceJSONRPCHTTP}

// avahiAnswers are LibreELEC's answers to a browse for both services, as Avahi sends them: one packet per service with the PTR in the answer section and the SRV, TXT, addresses and NSEC records in the additional section, names compressed throughout.
func avahiAnswers() [][]byte {
	var out [][]byte
	for _, p := range avahiPackets() {
		out = append(out, p.bytes())
	}

	return out
}

func avahiPackets() []*mdnsPacket {
	return []*mdnsPacket{
		newPacket(flagResponse).
			answer("_xbmc-jsonrpc._tcp.local.", typePTR, 4500, ptrData("Kodi (LibreELEC)._xbmc-jsonrpc._tcp.local.")).
			additional("Kodi (LibreELEC)._xbmc-jsonrpc._tcp.local.", typeSRV, 120, srvData(9090, "LibreELEC.local.")).
			additional("Kodi (LibreELEC)._xbmc-jsonrpc._tcp.local.", typeTXT, 4500, txtData("txtvers=1", "uuid=0a1b2c3d")).
			additional("LibreELEC.local.", typeA, 120, ipData("192.168.1.40")).
			additional("LibreELEC.local.", typeAAAA, 120, ipData("fd00::40")).
			additional("LibreELEC.local.", typeNSEC, 120, nsecData("LibreELEC.local.")),
		newPacket(flagResponse).
			answer("_xbmc-jsonrpc-h._tcp.local.", typePTR, 4500, ptrData("Kodi (LibreELEC)._xbmc-jsonrpc-h._tcp.local.")).
			additional("Kodi (LibreELEC)._xbmc-jsonrpc-h._tcp.local.", typeSRV, 120, srvData(8080, "LibreELEC.local.")).
			additional("Kodi (LibreELEC)._xbmc-jsonrpc-h._tcp.local.", typeTXT, 4500, txtData("")).
			additional("LibreELEC.local.", typeA, 120, ipData("192.168.1.40")).
			additional("LibreELEC.local.", typeAAAA, 120, ipData("fd00::40")),
	}
}

var libreELEC = []ServiceEntry{
	{Service: ServiceJSONRPC, Instance: "Kodi (LibreELEC)", Host: "LibreELEC.local.", Addrs: []net.IP{net.ParseIP("192.168.1.40").To4(), net.ParseIP("fd00::40")}, Port: 9090},
	{Service: ServiceJSONRPCHTTP, Instance: "Kodi (LibreELEC)", Host: "LibreELEC.local.", Addrs: []net.IP{net.ParseIP("192.168.1.40").To4(), net.ParseIP("fd00::40")}, Port: 8080},
}

func addAll(packets [][]byte) *mdnsAnswers {
	answers := newMDNSAnswers()
	for _, p := range packets {
		answers.add(p)
	}

	return answers
}

func TestMDNSAnswersAvahi(t *testing.T) {
	assert.Equal(t, libreELEC, addAll(avahiAnswers()).entries(bothServices))
}

func TestMDNSAnswersRepeatedCapture(t *testing.T) {
	got := addAll(repeatedAnswers(t)).entries(bothServices)
	require.Len(t, got, 4)

	for _, e := range got {
		assert.Len(t, e.Addrs, 2, e.Instance)
	}

	assert.Equal(t, "HikariNUC jsonrpc-tcp", got[0].Instance)
	assert.Equal(t, "HikariNUC.local.", got[0].Host)
}

// Records count wherever they are: several instances in the answer section, SRV records in the authority section and addresses in the additional one, among echoed questions, an OPT record and record types Encore doesn't know.
func TestMDNSAnswersSections(t *testing.T) {
	assert.Equal(t, []ServiceEntry{
		{Service: ServiceJSONRPC, Instance: "Den", Host: "den.local.", Addrs: []net.IP{net.ParseIP("fd00::30"), net.ParseIP("192.168.1.30").To4()}, Port: 9091},
		{Service: ServiceJSONRPC, Instance: "Lounge", Host: "lounge.local.", Addrs: []net.IP{net.ParseIP("192.168.1.20").To4(), net.ParseIP("192.168.1.21").To4()}, Port: 9090},
	}, addAll([][]byte{sectionsPacket().bytes()}).entries(bothServices))
}

func sectionsPacket() *mdnsPacket {
	return newPacket(flagTruncResponse).
		question("_xbmc-jsonrpc._tcp.local.", typePTR).
		answer("_xbmc-jsonrpc._tcp.local.", typePTR, 4500, ptrData("Lounge._xbmc-jsonrpc._tcp.local.")).
		answer("_xbmc-jsonrpc._tcp.local.", typePTR, 4500, ptrData("Den._xbmc-jsonrpc._tcp.local.")).
		answer("_xbmc-jsonrpc._tcp.local.", 0xFF00, 4500, rawData(1, 2, 3)).
		answer("Lounge.local.", typeHINFO, 120, txtData("ARM", "Linux")).
		record(sectionAuthority, "Lounge._xbmc-jsonrpc._tcp.local.", typeSRV, classIN, 120, srvData(9090, "lounge.local.")).
		record(sectionAuthority, "Den._xbmc-jsonrpc._tcp.local.", typeSRV, classIN, 120, srvData(9091, "den.local.")).
		additional("lounge.local.", typeA, 120, ipData("192.168.1.20")).
		additional("lounge.local.", typeA, 120, ipData("192.168.1.21")).
		additional("lounge.local.", typeA, 120, ipData("192.168.1.20")).
		additional("den.local.", typeAAAA, 120, ipData("fd00::30")).
		record(sectionAdditional, "", typeOPT, 1440, 0, nil).
		record(sectionAdditional, "den.local.", typeA, 3, 120, ipData("192.168.1.30"))
}

// Names compare without regard to ASCII case; an instance takes its SRV record's spelling and the host its SRV target's, while queries ask for hosts in lower case.
func TestMDNSAnswersCase(t *testing.T) {
	answers := addAll([][]byte{
		newPacket(flagResponse).
			answer("_XBMC-JSONRPC._TCP.LOCAL.", typePTR, 4500, ptrData("KODI._xbmc-jsonrpc._tcp.local.")).
			answer("Kodi._Xbmc-JsonRpc._tcp.local.", typeSRV, 120, srvData(9090, "Kodi-Lounge.Local.")).
			bytes(),
		newPacket(flagResponse).
			answer("_xbmc-jsonrpc-h._tcp.local.", typePTR, 4500, ptrData("Küche._xbmc-jsonrpc-h._tcp.local.")).
			answer("KÜCHE._xbmc-jsonrpc-h._tcp.local.", typeSRV, 120, srvData(8080, "kueche.local.")).
			bytes(),
	})

	q, err := answers.query(bothServices)
	require.NoError(t, err)
	assert.Equal(t, expectedQuery(t, bothServices, "kodi-lounge.local.", "kueche.local."), q)

	answers.add(newPacket(flagResponse).answer("KODI-LOUNGE.local.", typeA, 120, ipData("192.168.1.50")).bytes())

	assert.Equal(t, []ServiceEntry{
		{Service: ServiceJSONRPC, Instance: "Kodi", Host: "Kodi-Lounge.Local.", Addrs: []net.IP{net.ParseIP("192.168.1.50").To4()}, Port: 9090},
		{Service: ServiceJSONRPCHTTP, Instance: "KÜCHE", Host: "kueche.local.", Port: 8080},
	}, answers.entries(bothServices), "only ASCII letters fold, so the PTR's Küche lacks an SRV record")
}

// Instance names are arbitrary UTF-8 (RFC 6763 §4.3) and come back byte for byte, whatever DNS presentation format would escape.
func TestMDNSAnswersInstanceNames(t *testing.T) {
	for _, name := range []string{
		"Kodi (lounge)",
		`Kodi "quoted"; @home`,
		`back\slash`,
		`trailing\`,
		"tab\there",
		"Wohnzimmer Küche 📺",
		"Lounge 2.0",
	} {
		label := strings.NewReplacer(`\`, `\\`, ".", `\.`).Replace(name)
		full := label + "._xbmc-jsonrpc._tcp.local."
		p := newPacket(flagResponse).
			answer("_xbmc-jsonrpc._tcp.local.", typePTR, 4500, ptrData(full)).
			answer(full, typeSRV, 120, srvData(9090, "kodi.local.")).
			bytes()

		got := addAll([][]byte{p}).entries(bothServices)
		if assert.Len(t, got, 1, name) {
			assert.Equal(t, name, got[0].Instance)
		}
	}
}

// A host named with a dot inside a label is asked for byte for byte, and one such answer doesn't stop the query naming every other host.
func TestMDNSAnswersDottedTarget(t *testing.T) {
	answers := addAll([][]byte{newPacket(flagResponse).
		answer("Lounge._xbmc-jsonrpc._tcp.local.", typeSRV, 120, srvData(9090, `Kodi\.2\..local.`)).
		answer("Den._xbmc-jsonrpc._tcp.local.", typeSRV, 120, srvData(9091, "den.local.")).
		bytes()})

	q, err := answers.query(bothServices)
	require.NoError(t, err)
	assert.Equal(t, expectedQuery(t, bothServices, "den.local.", `kodi\.2\..local.`), q)

	answers.add(newPacket(flagResponse).answer(`kodi\.2\..local.`, typeA, 120, ipData("192.168.1.60")).bytes())

	assert.Equal(t, []ServiceEntry{
		{Service: ServiceJSONRPC, Instance: "Den", Host: "den.local.", Port: 9091},
		{Service: ServiceJSONRPC, Instance: "Lounge", Host: `Kodi\.2\..local.`, Addrs: []net.IP{net.ParseIP("192.168.1.60").To4()}, Port: 9090},
	}, answers.entries(bothServices))
}

// mdnsMalformed are packets that are wrong in one way each. Every one would otherwise add the instance Bad on port 9999.
func mdnsMalformed() map[string][]byte {
	const owner = "Bad._xbmc-jsonrpc._tcp.local."

	srv := func() *mdnsPacket {
		return newPacket(flagResponse).answer(owner, typeSRV, 120, srvData(9999, "bad.local."))
	}

	// rdlen moves the SRV record's rdata length by delta.
	rdlen := func(delta int) []byte {
		p := srv()
		binary.BigEndian.PutUint16(p.b[p.rdlen:], uint16(len(p.b)-p.rdlen-2+delta))

		return p.bytes()
	}

	withSRV := func(name string, typ uint16, data func(*mdnsPacket)) []byte {
		return srv().answer(name, typ, 120, data).bytes()
	}

	return map[string][]byte{
		"short header": srv().bytes()[:11],
		"pointer loop": newPacket(flagResponse).answer(owner, typeSRV, 120, func(p *mdnsPacket) {
			p.b = append(p.b, 0, 0, 0, 0, 0x27, 0x0F)
			p.b = binary.BigEndian.AppendUint16(p.b, 0xC000|uint16(len(p.b)))
		}).bytes(),
		"pointer past end": newPacket(flagResponse).answer(owner, typeSRV, 120, rawData(0, 0, 0, 0, 0x27, 0x0F, 0xC0, 0xFF)).bytes(),
		"label type 0x40":  newPacket(flagResponse).answer(owner, typeSRV, 120, rawData(0, 0, 0, 0, 0x27, 0x0F, 0x41, 'x', 0)).bytes(),
		"label type 0x80":  newPacket(flagResponse).answer(owner, typeSRV, 120, rawData(0, 0, 0, 0, 0x27, 0x0F, 0x81, 'x', 0)).bytes(),
		"name too long": newPacket(flagResponse).answer(owner, typeSRV, 120,
			srvData(9999, strings.Repeat(strings.Repeat("x", 63)+".", 4)+"local.")).bytes(),
		"rdlength past end":             rdlen(1),
		"SRV rdata short of its target": rdlen(-1),
		"SRV rdata past its target": func() []byte {
			p := srv()
			p.b = append(p.b, 0xDE, 0xAD)
			binary.BigEndian.PutUint16(p.b[p.rdlen:], uint16(len(p.b)-p.rdlen-2))

			return p.bytes()
		}(),
		"A rdata of 3 bytes":    withSRV("bad.local.", typeA, rawData(192, 168, 1)),
		"A rdata of 5 bytes":    withSRV("bad.local.", typeA, rawData(192, 168, 1, 99, 0)),
		"AAAA rdata of 4 bytes": withSRV("bad.local.", typeAAAA, ipData("192.168.1.99")),
		"TXT string overruns":   withSRV(owner, typeTXT, rawData(5, 'a', 'b')),
	}
}

// A malformed packet is dropped whole, keeping what earlier packets said.
func TestMDNSAnswersMalformed(t *testing.T) {
	for name, packet := range mdnsMalformed() {
		t.Run(name, func(t *testing.T) {
			answers := addAll(avahiAnswers())
			answers.add(packet)
			assert.Equal(t, libreELEC, answers.entries(bothServices))
		})
	}
}

// A packet cut short inside a question or record is dropped whole; one that ends between records keeps the records it holds, as though its header counted only those.
func TestMDNSAnswersTruncated(t *testing.T) {
	for _, p := range slices.Concat(avahiPackets(), []*mdnsPacket{sectionsPacket()}) {
		for n := range len(p.b) {
			truncated, recounted := p.truncate(n)

			want := []ServiceEntry(nil)
			if recounted != nil {
				want = addAll([][]byte{recounted}).entries(bothServices)
			}

			if !assert.Equal(t, want, addAll([][]byte{truncated}).entries(bothServices), "truncated to %d of %d bytes", n, len(p.b)) {
				return
			}
		}
	}

	for _, packet := range repeatedAnswers(t) {
		answers := addAll([][]byte{packet[:len(packet)-1]})
		assert.Empty(t, answers.entries(bothServices))

		q, err := answers.query(bothServices)
		require.NoError(t, err)
		assert.Equal(t, expectedQuery(t, bothServices), q)
	}
}

// Empty rdata parses as a record with zero fields: an A record without an address and an SRV record without a target or port.
func TestMDNSAnswersEmptyRData(t *testing.T) {
	answers := addAll([][]byte{newPacket(flagResponse).
		answer("Empty._xbmc-jsonrpc._tcp.local.", typeSRV, 120, nil).
		answer("Kodi._xbmc-jsonrpc._tcp.local.", typeSRV, 120, srvData(9090, "kodi.local.")).
		answer("kodi.local.", typeA, 120, nil).
		bytes()})

	assert.Equal(t, []ServiceEntry{
		{Service: ServiceJSONRPC, Instance: "Empty"},
		{Service: ServiceJSONRPC, Instance: "Kodi", Host: "kodi.local.", Addrs: []net.IP{nil}, Port: 9090},
	}, answers.entries(bothServices))
}

// A forward compression pointer and trailing bytes after the last record are both accepted.
func TestMDNSAnswersLenient(t *testing.T) {
	const instance = "\x04Kodi\x0d_xbmc-jsonrpc\x04_tcp\x05local\x00"

	// One SRV record, whose owner points ahead at its own target, then two stray bytes.
	p := []byte{
		0, 0, 0x84, 0, 0, 0, 0, 1, 0, 0, 0, 0,
		0xC0, 12 + 2 + 10 + 6,
		0, typeSRV, 0, classIN, 0, 0, 0, 120, 0, byte(6 + len(instance)), 0, 0, 0, 0, 0x23, 0x82,
	}
	p = append(p, instance...)
	p = append(p, 0xDE, 0xAD)

	assert.Equal(t, []ServiceEntry{{Service: ServiceJSONRPC, Instance: "Kodi", Host: "Kodi._xbmc-jsonrpc._tcp.local.", Port: 9090}}, addAll([][]byte{p}).entries(bothServices))
}

// A header may count more records than follow, so long as the packet ends between records.
func TestMDNSAnswersOvercounted(t *testing.T) {
	p := newPacket(flagResponse).answer("Kodi._xbmc-jsonrpc._tcp.local.", typeSRV, 120, srvData(9090, "kodi.local.")).bytes()
	binary.BigEndian.PutUint16(p[6:], 2)
	binary.BigEndian.PutUint16(p[10:], 5)

	assert.Equal(t, []ServiceEntry{{Service: ServiceJSONRPC, Instance: "Kodi", Host: "kodi.local.", Port: 9090}}, addAll([][]byte{p}).entries(bothServices))
}

func FuzzMDNSAnswers(f *testing.F) {
	for _, p := range avahiAnswers() {
		f.Add(p)
	}

	for _, p := range mdnsMalformed() {
		f.Add(p)
	}

	f.Fuzz(func(t *testing.T, packet []byte) {
		answers := newMDNSAnswers()
		answers.add(packet)

		for _, p := range avahiAnswers() {
			answers.add(p)
		}

		answers.add(packet)

		got := answers.entries(bothServices)
		for _, e := range got {
			if !slices.Contains(bothServices, e.Service) {
				t.Fatalf("entry for service %q", e.Service)
			}
		}

		if _, err := answers.query(bothServices); err != nil {
			t.Fatalf("query: %v", err)
		}
	})
}

func BenchmarkMDNSAnswersAdd(b *testing.B) {
	packets := slices.Concat(avahiAnswers(), repeatedAnswers(b))

	b.ReportAllocs()

	for b.Loop() {
		answers := newMDNSAnswers()
		for _, p := range packets {
			answers.add(p)
		}
	}
}

func BenchmarkMDNSAnswersQuery(b *testing.B) {
	answers := addAll([][]byte{newPacket(flagResponse).
		answer("Lounge._xbmc-jsonrpc._tcp.local.", typeSRV, 120, srvData(9090, "lounge.local.")).
		answer("Den._xbmc-jsonrpc-h._tcp.local.", typeSRV, 120, srvData(8080, "den.local.")).
		bytes()})

	b.ReportAllocs()

	for b.Loop() {
		if _, err := answers.query(bothServices); err != nil {
			b.Fatal(err)
		}
	}
}
