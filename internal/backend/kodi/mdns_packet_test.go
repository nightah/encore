package kodi

import (
	"encoding/binary"
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// mdnsPacket builds mDNS messages byte by byte, so tests don't depend on the DNS library under test. Names are compressed as responders compress them, against the exact spelling of earlier names.
type mdnsPacket struct {
	b       []byte
	names   map[string]int
	section int
	// rdlen is the offset of the last record's rdata length.
	rdlen int
	// ends are where each question and record ends, and its section.
	ends [][2]int
	// compress is false for queries, which Encore sends uncompressed.
	compress bool
}

const (
	sectionQuestion = iota
	sectionAnswer
	sectionAuthority
	sectionAdditional
)

const (
	typeA     = 1
	typePTR   = 12
	typeHINFO = 13
	typeTXT   = 16
	typeAAAA  = 28
	typeSRV   = 33
	typeOPT   = 41
	typeNSEC  = 47

	classIN = 1
	// classFlush is IN with the cache-flush bit set (RFC 6762 §10.2).
	classFlush = 0x8001

	flagResponse      = 0x8400 // QR and AA, as responders send
	flagTruncResponse = 0x8600 // as flagResponse, with TC
)

func newPacket(flags uint16) *mdnsPacket {
	p := &mdnsPacket{b: make([]byte, 12), names: map[string]int{}, compress: true}
	binary.BigEndian.PutUint16(p.b[2:], flags)

	return p
}

// labels splits a presentation name on dots, where "\." is a dot inside a label and "\\" a backslash.
func labels(name string) []string {
	var (
		out []string
		cur strings.Builder
	)

	name = strings.TrimSuffix(name, ".")
	for i := 0; i < len(name); i++ {
		switch c := name[i]; {
		case c == '\\' && i+1 < len(name):
			i++
			cur.WriteByte(name[i])
		case c == '.':
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}

	if name != "" {
		out = append(out, cur.String())
	}

	return out
}

func (p *mdnsPacket) name(name string) {
	ls := labels(name)
	for i := range ls {
		key := strings.Join(ls[i:], "\x00")
		if off, ok := p.names[key]; ok && p.compress {
			p.b = binary.BigEndian.AppendUint16(p.b, 0xC000|uint16(off))

			return
		}

		if len(p.b) < 0x3FFF {
			p.names[key] = len(p.b)
		}

		p.b = append(p.b, byte(len(ls[i])))
		p.b = append(p.b, ls[i]...)
	}

	p.b = append(p.b, 0)
}

func (p *mdnsPacket) count(section int) {
	if section < p.section {
		panic("mdnsPacket: sections out of order")
	}

	p.section = section
	off := 4 + 2*section
	binary.BigEndian.PutUint16(p.b[off:], binary.BigEndian.Uint16(p.b[off:])+1)
}

func (p *mdnsPacket) question(name string, typ uint16) *mdnsPacket {
	p.count(sectionQuestion)
	p.name(name)
	p.b = binary.BigEndian.AppendUint16(p.b, typ)
	p.b = binary.BigEndian.AppendUint16(p.b, classIN)
	p.ends = append(p.ends, [2]int{len(p.b), sectionQuestion})

	return p
}

// record appends a record to section, with rdata written by data so names in it can be compressed.
func (p *mdnsPacket) record(section int, name string, typ, class uint16, ttl uint32, data func(*mdnsPacket)) *mdnsPacket {
	p.count(section)
	p.name(name)
	p.b = binary.BigEndian.AppendUint16(p.b, typ)
	p.b = binary.BigEndian.AppendUint16(p.b, class)
	p.b = binary.BigEndian.AppendUint32(p.b, ttl)

	p.rdlen = len(p.b)
	p.b = append(p.b, 0, 0)

	if data != nil {
		data(p)
	}

	binary.BigEndian.PutUint16(p.b[p.rdlen:], uint16(len(p.b)-p.rdlen-2))
	p.ends = append(p.ends, [2]int{len(p.b), section})

	return p
}

func (p *mdnsPacket) answer(name string, typ uint16, ttl uint32, data func(*mdnsPacket)) *mdnsPacket {
	return p.record(sectionAnswer, name, typ, classIN, ttl, data)
}

func (p *mdnsPacket) additional(name string, typ uint16, ttl uint32, data func(*mdnsPacket)) *mdnsPacket {
	return p.record(sectionAdditional, name, typ, classFlush, ttl, data)
}

func (p *mdnsPacket) bytes() []byte { return p.b }

// truncate returns the packet's first n bytes and, when they end after a question or record, the same bytes with the header counting only what they hold.
func (p *mdnsPacket) truncate(n int) (truncated, recounted []byte) {
	truncated = p.b[:n]

	var counts [4]uint16

	for _, end := range p.ends {
		if end[0] > n {
			break
		}

		counts[end[1]]++
	}

	if n != 12 && !slices.ContainsFunc(p.ends, func(end [2]int) bool { return end[0] == n }) {
		return truncated, nil
	}

	recounted = slices.Clone(truncated)
	for i, c := range counts {
		binary.BigEndian.PutUint16(recounted[4+2*i:], c)
	}

	return truncated, recounted
}

func ptrData(target string) func(*mdnsPacket) {
	return func(p *mdnsPacket) { p.name(target) }
}

func srvData(port uint16, target string) func(*mdnsPacket) {
	return func(p *mdnsPacket) {
		p.b = append(p.b, 0, 0, 0, 0)
		p.b = binary.BigEndian.AppendUint16(p.b, port)
		p.name(target)
	}
}

func ipData(ip string) func(*mdnsPacket) {
	return func(p *mdnsPacket) {
		parsed := net.ParseIP(ip)
		if v4 := parsed.To4(); v4 != nil {
			parsed = v4
		}

		p.b = append(p.b, parsed...)
	}
}

func txtData(strs ...string) func(*mdnsPacket) {
	return func(p *mdnsPacket) {
		for _, s := range strs {
			p.b = append(p.b, byte(len(s)))
			p.b = append(p.b, s...)
		}
	}
}

func rawData(b ...byte) func(*mdnsPacket) {
	return func(p *mdnsPacket) { p.b = append(p.b, b...) }
}

// nsecData is an NSEC record saying the host has A and AAAA records only, as Avahi and mDNSResponder send; the next name is not compressed.
func nsecData(next string) func(*mdnsPacket) {
	return func(p *mdnsPacket) {
		compress := p.compress
		p.compress = false
		p.name(next)
		p.compress = compress
		p.b = append(p.b, 0, 4, 0x40, 0, 0, 0x08)
	}
}

// expectedQuery is the uncompressed query Encore sends for services, then A and AAAA for each host.
func expectedQuery(t *testing.T, services []string, hosts ...string) []byte {
	t.Helper()

	p := newPacket(0)
	p.compress = false

	for _, s := range services {
		p.question(s+"."+mdnsDomain, typePTR)
	}

	for _, h := range hosts {
		p.question(h, typeA).question(h, typeAAAA)
	}

	require.NotEmpty(t, p.b)

	return p.bytes()
}
