package talosmobile

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
	"golang.org/x/net/bpf"
)

var (
	macA = net.HardwareAddr{0x02, 0, 0, 0, 0, 0xa}
	macB = net.HardwareAddr{0x02, 0, 0, 0, 0, 0xb}
	ipA  = net.IP{192, 0, 2, 1}
	ipB  = net.IP{198, 51, 100, 7}
	ip6A = net.ParseIP("2001:db8::1")
	ip6B = net.ParseIP("2001:db8::2")
)

func serialize(t *testing.T, ls ...gopacket.SerializableLayer) []byte {
	t.Helper()

	buf := gopacket.NewSerializeBuffer()
	if err := gopacket.SerializeLayers(buf, gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}, ls...); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func eth4() *layers.Ethernet {
	return &layers.Ethernet{SrcMAC: macA, DstMAC: macB, EthernetType: layers.EthernetTypeIPv4}
}

func ip4(proto layers.IPProtocol) *layers.IPv4 {
	return &layers.IPv4{Version: 4, TTL: 64, Protocol: proto, SrcIP: ipA, DstIP: ipB}
}

func tcpPacket(t *testing.T, tcp *layers.TCP, payload []byte) []byte {
	t.Helper()

	ip := ip4(layers.IPProtocolTCP)
	if err := tcp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatal(err)
	}

	return serialize(t, eth4(), ip, tcp, gopacket.Payload(payload))
}

func udpPacket(t *testing.T, src, dst layers.UDPPort, payload []byte) []byte {
	t.Helper()

	ip := ip4(layers.IPProtocolUDP)
	udp := &layers.UDP{SrcPort: src, DstPort: dst}

	if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatal(err)
	}

	return serialize(t, eth4(), ip, udp, gopacket.Payload(payload))
}

func dnsBytes(t *testing.T, d *layers.DNS) []byte {
	t.Helper()

	return serialize(t, d)
}

func sum(t *testing.T, data []byte, lt layers.LinkType) packetSummary {
	t.Helper()

	return summarize(3, gopacket.CaptureInfo{Timestamp: time.UnixMilli(1700000000123), Length: len(data), CaptureLength: len(data)}, data, lt)
}

func TestSummarizeTCPSyn(t *testing.T) {
	s := sum(t, tcpPacket(t, &layers.TCP{SrcPort: 51234, DstPort: 443, SYN: true, Seq: 1000, Window: 64240}, nil), layers.LinkTypeEthernet)

	if s.N != 3 || s.TS != 1700000000123 || s.Proto != "TCP" || s.Src != "192.0.2.1:51234" || s.Dst != "198.51.100.7:443" {
		t.Fatalf("unexpected summary %+v", s)
	}

	if s.Info != "51234 → 443 [S] seq 1000 win 64240 len 0" {
		t.Fatalf("info %q", s.Info)
	}

	s = sum(t, tcpPacket(t, &layers.TCP{SrcPort: 443, DstPort: 51234, SYN: true, ACK: true, Seq: 5, Ack: 1001, Window: 100}, nil), layers.LinkTypeEthernet)
	if !strings.Contains(s.Info, "[S.]") || !strings.Contains(s.Info, "ack 1001") {
		t.Fatalf("info %q", s.Info)
	}
}

func TestSummarizeDNS(t *testing.T) {
	query := dnsBytes(t, &layers.DNS{ID: 0x1a2b, RD: true, Questions: []layers.DNSQuestion{
		{Name: []byte("example.com"), Type: layers.DNSTypeA, Class: layers.DNSClassIN},
	}})

	s := sum(t, udpPacket(t, 40000, 53, query), layers.LinkTypeEthernet)
	if s.Proto != "DNS" || s.Info != "query 0x1a2b A example.com" {
		t.Fatalf("query summary %+v", s)
	}

	answer := dnsBytes(t, &layers.DNS{
		ID: 0x1a2b, QR: true, RD: true, RA: true,
		Questions: []layers.DNSQuestion{{Name: []byte("example.com"), Type: layers.DNSTypeA, Class: layers.DNSClassIN}},
		Answers: []layers.DNSResourceRecord{
			{Name: []byte("example.com"), Type: layers.DNSTypeCNAME, Class: layers.DNSClassIN, TTL: 60, CNAME: []byte("web.example.com")},
			{Name: []byte("web.example.com"), Type: layers.DNSTypeA, Class: layers.DNSClassIN, TTL: 60, IP: net.IP{203, 0, 113, 5}},
		},
	})

	s = sum(t, udpPacket(t, 53, 40000, answer), layers.LinkTypeEthernet)
	if s.Proto != "DNS" || s.Info != "response 0x1a2b A example.com CNAME web.example.com A 203.0.113.5" {
		t.Fatalf("answer summary %+v", s)
	}

	nx := dnsBytes(t, &layers.DNS{
		ID: 7, QR: true, ResponseCode: layers.DNSResponseCodeNXDomain,
		Questions: []layers.DNSQuestion{{Name: []byte("nope.test"), Type: layers.DNSTypeAAAA, Class: layers.DNSClassIN}},
	})

	if s = sum(t, udpPacket(t, 53, 40000, nx), layers.LinkTypeEthernet); !strings.Contains(s.Info, "Non-Existent Domain") {
		t.Fatalf("nxdomain summary %+v", s)
	}
}

func TestSummarizeARP(t *testing.T) {
	req := serialize(t,
		&layers.Ethernet{SrcMAC: macA, DstMAC: layers.EthernetBroadcast, EthernetType: layers.EthernetTypeARP},
		&layers.ARP{
			AddrType: layers.LinkTypeEthernet, Protocol: layers.EthernetTypeIPv4, HwAddressSize: 6, ProtAddressSize: 4,
			Operation: layers.ARPRequest, SourceHwAddress: macA, SourceProtAddress: ipA,
			DstHwAddress: make([]byte, 6), DstProtAddress: ipB,
		})

	s := sum(t, req, layers.LinkTypeEthernet)
	if s.Proto != "ARP" || s.Info != "who-has 198.51.100.7 tell 192.0.2.1" || s.Dst != "ff:ff:ff:ff:ff:ff" {
		t.Fatalf("arp request %+v", s)
	}

	reply := serialize(t,
		&layers.Ethernet{SrcMAC: macB, DstMAC: macA, EthernetType: layers.EthernetTypeARP},
		&layers.ARP{
			AddrType: layers.LinkTypeEthernet, Protocol: layers.EthernetTypeIPv4, HwAddressSize: 6, ProtAddressSize: 4,
			Operation: layers.ARPReply, SourceHwAddress: macB, SourceProtAddress: ipB,
			DstHwAddress: macA, DstProtAddress: ipA,
		})

	if s = sum(t, reply, layers.LinkTypeEthernet); s.Info != "198.51.100.7 is-at 02:00:00:00:00:0b" {
		t.Fatalf("arp reply %+v", s)
	}
}

func TestSummarizeICMP(t *testing.T) {
	data := serialize(t, eth4(), ip4(layers.IPProtocolICMPv4),
		&layers.ICMPv4{TypeCode: layers.CreateICMPv4TypeCode(layers.ICMPv4TypeEchoRequest, 0), Id: 9, Seq: 2},
		gopacket.Payload("ping"))

	s := sum(t, data, layers.LinkTypeEthernet)
	if s.Proto != "ICMP" || s.Info != "echo request id 9 seq 2" || s.Src != "192.0.2.1" {
		t.Fatalf("icmp %+v", s)
	}

	unreach := serialize(t, eth4(), ip4(layers.IPProtocolICMPv4),
		&layers.ICMPv4{TypeCode: layers.CreateICMPv4TypeCode(layers.ICMPv4TypeDestinationUnreachable, layers.ICMPv4CodePort)})

	if s = sum(t, unreach, layers.LinkTypeEthernet); s.Info != "destination unreachable (port)" {
		t.Fatalf("unreachable %+v", s)
	}
}

func TestSummarizeIPv6(t *testing.T) {
	ip := &layers.IPv6{Version: 6, HopLimit: 64, NextHeader: layers.IPProtocolICMPv6, SrcIP: ip6A, DstIP: ip6B}
	icmp := &layers.ICMPv6{TypeCode: layers.CreateICMPv6TypeCode(layers.ICMPv6TypeEchoRequest, 0)}

	if err := icmp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatal(err)
	}

	data := serialize(t, &layers.Ethernet{SrcMAC: macA, DstMAC: macB, EthernetType: layers.EthernetTypeIPv6},
		ip, icmp, &layers.ICMPv6Echo{Identifier: 4, SeqNumber: 1})

	s := sum(t, data, layers.LinkTypeEthernet)
	if s.Proto != "ICMPv6" || s.Info != "echo request id 4 seq 1" || s.Src != "2001:db8::1" {
		t.Fatalf("icmpv6 %+v", s)
	}

	// Raw link (WireGuard/KubeSpan): no Ethernet header, IPv6 TCP.
	ipt := &layers.IPv6{Version: 6, HopLimit: 64, NextHeader: layers.IPProtocolTCP, SrcIP: ip6A, DstIP: ip6B}
	tcp := &layers.TCP{SrcPort: 1234, DstPort: 6443, ACK: true, Ack: 1, Window: 10}

	if err := tcp.SetNetworkLayerForChecksum(ipt); err != nil {
		t.Fatal(err)
	}

	s = sum(t, serialize(t, ipt, tcp), layers.LinkTypeRaw)
	if s.Proto != "TCP" || s.Src != "[2001:db8::1]:1234" || s.Dst != "[2001:db8::2]:6443" {
		t.Fatalf("raw ipv6 tcp %+v", s)
	}

	// IPv6 with an unknown next header.
	ipx := &layers.IPv6{Version: 6, HopLimit: 64, NextHeader: layers.IPProtocol(253), SrcIP: ip6A, DstIP: ip6B}
	if s = sum(t, serialize(t, ipx, gopacket.Payload("x")), layers.LinkTypeRaw); s.Proto != "IPv6" {
		t.Fatalf("ipv6 %+v", s)
	}
}

// clientHello builds a TLS 1.2 record carrying a ClientHello with an SNI extension.
func clientHello(sni string) []byte {
	var ext bytes.Buffer

	name := []byte(sni)
	_ = binary.Write(&ext, binary.BigEndian, uint16(0))           // server_name
	_ = binary.Write(&ext, binary.BigEndian, uint16(len(name)+5)) // ext length
	_ = binary.Write(&ext, binary.BigEndian, uint16(len(name)+3)) // list length
	ext.WriteByte(0)                                              // host_name
	_ = binary.Write(&ext, binary.BigEndian, uint16(len(name)))
	ext.Write(name)

	var body bytes.Buffer

	body.Write([]byte{3, 3})
	body.Write(make([]byte, 32)) // random
	body.WriteByte(0)            // session id
	_ = binary.Write(&body, binary.BigEndian, uint16(2))
	body.Write([]byte{0x13, 0x01})
	body.Write([]byte{1, 0}) // compression
	_ = binary.Write(&body, binary.BigEndian, uint16(ext.Len()))
	body.Write(ext.Bytes())

	hs := append([]byte{1, 0, byte(body.Len() >> 8), byte(body.Len())}, body.Bytes()...)

	return append([]byte{22, 3, 1, byte(len(hs) >> 8), byte(len(hs))}, hs...)
}

func TestSummarizeTLSAndHTTP(t *testing.T) {
	s := sum(t, tcpPacket(t, &layers.TCP{SrcPort: 51234, DstPort: 443, ACK: true, PSH: true}, clientHello("api.example.com")), layers.LinkTypeEthernet)
	if s.Proto != "TLS" || s.Info != "client hello, SNI api.example.com" {
		t.Fatalf("tls %+v", s)
	}

	s = sum(t, tcpPacket(t, &layers.TCP{SrcPort: 443, DstPort: 51234, ACK: true}, []byte{23, 3, 3, 0, 4, 1, 2, 3, 4}), layers.LinkTypeEthernet)
	if s.Proto != "TLS" || s.Info != "application data" {
		t.Fatalf("tls data %+v", s)
	}

	s = sum(t, tcpPacket(t, &layers.TCP{SrcPort: 40000, DstPort: 80, ACK: true}, []byte("GET /healthz HTTP/1.1\r\nHost: x\r\n\r\n")), layers.LinkTypeEthernet)
	if s.Proto != "HTTP" || s.Info != "GET /healthz HTTP/1.1" {
		t.Fatalf("http %+v", s)
	}

	// A truncated ClientHello still says what it is.
	if info := tlsInfo(clientHello("x.example")[:20]); info != "client hello" {
		t.Fatalf("truncated hello %q", info)
	}
}

func TestValidateCaptureFilter(t *testing.T) {
	for _, ok := range []string{
		"", "udp port 53", "icmp", "tcp and dst port 80", "host 192.0.2.1", "net 10.0.0.0/8", "arp", "ip6",
		"not port 22", "(tcp port 443 or icmp)", "ip6 host 2001:db8::1", "src net 2001:db8::/32", "port https",
		"ip proto 1", "ether src 02:00:00:00:00:0a", "tcp src port 22 || ! icmp6",
	} {
		if msg := ValidateCaptureFilter(ok); msg != "" {
			t.Errorf("%q: unexpected error %q", ok, msg)
		}
	}

	for _, bad := range []string{
		"foo bar", "port", "udp port 53 and", "portrange 1-100", "tcp port abc", "port 99999", "(udp", "udp)", "host",
		"vlan", "host example.com", "tcp[13] & 2 != 0", "ip host 2001:db8::1", "net 10.0.0.0/33", "icmp udp", "and tcp",
	} {
		if msg := ValidateCaptureFilter(bad); msg == "" {
			t.Errorf("%q: accepted", bad)
		}
	}
}

func TestFilterComposition(t *testing.T) {
	dns := udpPacket(t, 40000, 53, []byte("q"))
	api := tcpPacket(t, &layers.TCP{SrcPort: 50000, DstPort: 40000, ACK: true}, []byte("x"))
	https := tcpPacket(t, &layers.TCP{SrcPort: 40000, DstPort: 443, ACK: true}, nil)
	ping := serialize(t, eth4(), ip4(layers.IPProtocolICMPv4), &layers.ICMPv4{TypeCode: layers.CreateICMPv4TypeCode(8, 0)})

	for _, tc := range []struct {
		expr                  string
		dns, api, https, ping bool
	}{
		{"udp port 53", true, false, false, false},
		{"not udp port 53", false, true, true, true},
		{"tcp port 443 or icmp", false, false, true, true},
		{"(tcp port 443 or icmp)", false, false, true, true},
		{"tcp and not port 50000", false, false, true, false},
		{"not port 50000 and tcp", false, false, true, false},
		{"! (tcp or udp)", false, false, false, true},
		{"icmp or udp port 53 or tcp port 443", true, false, true, true},
		// tcpdump: and/or have equal precedence, left to right: (icmp or tcp) and port 443.
		{"icmp or tcp and port 443", false, false, true, false},
		{"icmp or (tcp and port 443)", false, false, true, true},
		{"src host 192.0.2.1 and dst port 53", true, false, false, false},
		{"host 203.0.113.9", false, false, false, false},
		{"net 198.51.100.0/24 && not tcp", true, false, false, true},
		{"dst net 198.51.100.0/24 and src port 40000", true, false, true, false},
		{"src port 50000", false, true, false, false},
		{"tcp dst port 53", false, false, false, false},
		{"ip proto 17", true, false, false, false},
		{"ip6 or arp", false, false, false, false},
		{"ether dst 02:00:00:00:00:0b and icmp", false, false, false, true},
		{"not not icmp", false, false, false, true},
	} {
		ins, err := compileFilter(tc.expr)
		if err != nil {
			t.Fatalf("%q: %v", tc.expr, err)
		}

		for name, c := range map[string]struct {
			pkt  []byte
			want bool
		}{"dns": {dns, tc.dns}, "api": {api, tc.api}, "https": {https, tc.https}, "ping": {ping, tc.ping}} {
			if got := runFilter(t, ins, c.pkt); got != c.want {
				t.Errorf("%q on %s: got %v", tc.expr, name, got)
			}
		}
	}
}

func runFilter(t *testing.T, ins []bpf.Instruction, pkt []byte) bool {
	t.Helper()

	vm, err := bpf.NewVM(ins)
	if err != nil {
		t.Fatal(err)
	}

	n, err := vm.Run(pkt)
	if err != nil {
		t.Fatal(err)
	}

	return n > 0
}

func TestCaptureFilterExcludesTalosAPI(t *testing.T) {
	dns := udpPacket(t, 40000, 53, dnsBytes(t, &layers.DNS{ID: 1, Questions: []layers.DNSQuestion{{Name: []byte("a.b"), Type: layers.DNSTypeA, Class: layers.DNSClassIN}}}))
	api := tcpPacket(t, &layers.TCP{SrcPort: 50000, DstPort: 40000, ACK: true}, []byte("x"))
	https := tcpPacket(t, &layers.TCP{SrcPort: 40000, DstPort: 443, ACK: true}, nil)

	for _, tc := range []struct {
		expr               string
		dns, api, httpsHit bool
	}{
		{"", true, false, true},
		{"udp port 53", true, false, false},
		{"tcp", false, false, true},
	} {
		raw, match, err := captureFilter(tc.expr, false)
		if err != nil {
			t.Fatal(err)
		}

		if match(dns) != tc.dns || match(api) != tc.api || match(https) != tc.httpsHit {
			t.Errorf("%q: local filter disagrees", tc.expr)
		}

		ins := make([]bpf.Instruction, len(raw))
		for i, r := range raw {
			ins[i] = bpf.RawInstruction{Op: uint16(r.Op), Jt: uint8(r.Jt), Jf: uint8(r.Jf), K: r.K}.Disassemble()
		}

		if got := runFilter(t, ins, dns); got != tc.dns {
			t.Errorf("%q dns: %v", tc.expr, got)
		}

		if got := runFilter(t, ins, api); got != tc.api {
			t.Errorf("%q api: %v", tc.expr, got)
		}

		if got := runFilter(t, ins, https); got != tc.httpsHit {
			t.Errorf("%q https: %v", tc.expr, got)
		}
	}
}

func TestRawLinkProgram(t *testing.T) {
	ins, err := compileFilter("(udp port 53) and not port 50000")
	if err != nil {
		t.Fatal(err)
	}

	rawIns, err := rawLinkProgram(ins)
	if err != nil {
		t.Fatal(err)
	}

	// The VM has no SKF_AD_PROTOCOL: stand in the EtherType of the test packet.
	emulate := func(etherType uint16) []bpf.Instruction {
		out := make([]bpf.Instruction, len(rawIns))
		for i, in := range rawIns {
			if ext, ok := in.(bpf.LoadExtension); ok && ext.Num == bpf.ExtProto {
				out[i] = bpf.LoadConstant{Dst: bpf.RegA, Val: uint32(etherType)}
			} else {
				out[i] = in
			}
		}

		return out
	}

	full := udpPacket(t, 40000, 53, []byte("q"))
	ipOnly := full[ethernetHeaderLen:]

	if !runFilter(t, emulate(0x0800), ipOnly) {
		t.Fatal("raw DNS packet not matched")
	}

	other := udpPacket(t, 40000, 123, []byte("q"))[ethernetHeaderLen:]
	if runFilter(t, emulate(0x0800), other) {
		t.Fatal("raw NTP packet matched")
	}

	_, match, err := captureFilter("udp port 53", true)
	if err != nil {
		t.Fatal(err)
	}

	if !match(ipOnly) || match(other) || match(nil) {
		t.Fatal("raw local filter")
	}

	if _, _, err := captureFilter("ether host 02:00:00:00:00:0a", true); err == nil {
		t.Fatal("Ethernet filter accepted on a raw link")
	}
}

type captureEvents struct {
	summaries []string
	stats     int
}

func (c *captureEvents) OnPacket(s string)                   { c.summaries = append(c.summaries, s) }
func (c *captureEvents) OnStats(int64, int64)                { c.stats++ }
func (c *captureEvents) OnDone(string, int64, int64, string) {}
func (c *captureEvents) reporter(now func() time.Time) *captureReporter {
	return &captureReporter{listener: c, now: now}
}

// talosStream builds a pcap stream like Talos sends (snaplen 4096 in the header).
func talosStream(t *testing.T, packets ...[]byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := pcapgo.NewWriter(&buf)
	if err := w.WriteFileHeader(4096, layers.LinkTypeEthernet); err != nil {
		t.Fatal(err)
	}

	for i, p := range packets {
		ci := gopacket.CaptureInfo{Timestamp: time.Unix(1700000000, int64(i)*1000), CaptureLength: len(p), Length: len(p)}
		if err := w.WritePacket(ci, p); err != nil {
			t.Fatal(err)
		}
	}

	return buf.Bytes()
}

func TestWriteCaptureRoundTrip(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "cap.pcap")

	pkts := [][]byte{
		tcpPacket(t, &layers.TCP{SrcPort: 1, DstPort: 443, SYN: true}, nil),
		udpPacket(t, 40000, 53, dnsBytes(t, &layers.DNS{ID: 2, Questions: []layers.DNSQuestion{{Name: []byte("x.y"), Type: layers.DNSTypeA, Class: layers.DNSClassIN}}})),
		tcpPacket(t, &layers.TCP{SrcPort: 1, DstPort: 443, ACK: true}, bytes.Repeat([]byte("z"), 5000)), // > Talos snaplen
	}

	ev := &captureEvents{}
	opts := captureOptions{maxBytes: defaultCaptureBytes}

	res, path, err := writeCapture(context.Background(), bytes.NewReader(talosStream(t, pkts...)), layers.LinkTypeEthernet, dest, opts, ev.reporter(time.Now))
	if err != nil || path != dest || res.packets != 3 {
		t.Fatalf("res=%+v path=%q err=%v", res, path, err)
	}

	if fi, err := os.Stat(dest); err != nil || fi.Size() != res.bytes {
		t.Fatalf("file size %v vs %d (%v)", fi, res.bytes, err)
	}

	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatal("part file left")
	}

	if len(ev.summaries) != 3 || ev.stats == 0 {
		t.Fatalf("events %+v", ev)
	}

	out, err := ReadPcap(dest, 1, 10)
	if err != nil {
		t.Fatal(err)
	}

	var page pcapPage
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatal(err)
	}

	if page.Total != 3 || len(page.Packets) != 2 || page.Packets[0].N != 1 || page.Packets[0].Proto != "DNS" || page.Packets[1].Len != len(pkts[2]) {
		t.Fatalf("page %+v", page)
	}

	detailJSON, err := PacketDetail(dest, 0)
	if err != nil {
		t.Fatal(err)
	}

	var d packetDetail
	if err := json.Unmarshal([]byte(detailJSON), &d); err != nil {
		t.Fatal(err)
	}

	names := []string{}
	for _, l := range d.Layers {
		names = append(names, l.Name)
	}

	if strings.Join(names, ",") != "Frame,Ethernet,IPv4,TCP" {
		t.Fatalf("layers %v", names)
	}

	if !hasField(d.Layers[3], "SYN", "true") || !hasField(d.Layers[2], "SrcIP", "192.0.2.1") {
		t.Fatalf("fields %+v", d.Layers)
	}

	if first := strings.SplitN(d.Hex, "\n", 2)[0]; !strings.HasPrefix(first, "0000  02 00 00 00 00 0b 02 00  00 00 00 0a 08 00 45 00  |..............E.|") {
		t.Fatalf("hex %q", first)
	}

	if _, err := PacketDetail(dest, 7); err == nil || !strings.Contains(err.Error(), "3 packets") {
		t.Fatalf("missing packet: %v", err)
	}
}

func hasField(l detailLayer, k, v string) bool {
	for _, f := range l.Fields {
		if f.K == k && f.V == v {
			return true
		}
	}

	return false
}

func TestWriteCaptureDropsUnfiltered(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "cap.pcap")
	dns := udpPacket(t, 40000, 53, []byte("q"))
	https := tcpPacket(t, &layers.TCP{SrcPort: 1, DstPort: 443, ACK: true}, nil)

	_, match, err := captureFilter("udp port 53", false)
	if err != nil {
		t.Fatal(err)
	}

	opts := captureOptions{maxBytes: 1 << 20, match: match}

	res, _, err := writeCapture(context.Background(), bytes.NewReader(talosStream(t, https, dns, https)), layers.LinkTypeEthernet, dest, opts, (&captureEvents{}).reporter(time.Now))
	if err != nil || res.packets != 1 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestWriteCaptureLimitsAndSnaplen(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "cap.pcap")
	pkt := tcpPacket(t, &layers.TCP{SrcPort: 1, DstPort: 2, ACK: true}, bytes.Repeat([]byte("a"), 400))
	stream := talosStream(t, pkt, pkt, pkt, pkt, pkt)

	// Room for two packets truncated to 100 bytes.
	opts := captureOptions{snapLen: 100, maxBytes: pcapFileHeaderLen + 2*(pcapPacketHeaderLen+100) + 50}

	res, path, err := writeCapture(context.Background(), bytes.NewReader(stream), layers.LinkTypeEthernet, dest, opts, (&captureEvents{}).reporter(time.Now))
	if err != nil || path != dest || res.packets != 2 {
		t.Fatalf("res=%+v err=%v", res, err)
	}

	err = eachPacket(dest, func(_ int, ci gopacket.CaptureInfo, data []byte, _ layers.LinkType) bool {
		if len(data) != 100 || ci.Length != len(pkt) {
			t.Errorf("captured %d of %d", len(data), ci.Length)
		}

		return true
	})
	if err != nil {
		t.Fatal(err)
	}
}

// brokenStream yields a pcap header then fails.
type brokenStream struct{ r io.Reader }

func (b *brokenStream) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err == io.EOF {
		return n, io.ErrClosedPipe
	}

	return n, err
}

func TestWriteCaptureErrors(t *testing.T) {
	dir := t.TempDir()

	// An error before any packet: nothing is kept.
	dest := filepath.Join(dir, "empty.pcap")

	_, path, err := writeCapture(context.Background(), &brokenStream{bytes.NewReader(talosStream(t))}, layers.LinkTypeEthernet, dest, captureOptions{maxBytes: 1 << 20}, (&captureEvents{}).reporter(time.Now))
	if err == nil || path != "" {
		t.Fatalf("path=%q err=%v", path, err)
	}

	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("files left: %v", entries)
	}

	// An error after packets: the partial capture is kept.
	pkt := udpPacket(t, 1, 2, []byte("x"))

	res, path, err := writeCapture(context.Background(), &brokenStream{bytes.NewReader(talosStream(t, pkt))}, layers.LinkTypeEthernet, dest, captureOptions{maxBytes: 1 << 20}, (&captureEvents{}).reporter(time.Now))
	if err == nil || path != dest || res.packets != 1 {
		t.Fatalf("res=%+v path=%q err=%v", res, path, err)
	}

	// Stopped (cancelled) before Talos sent anything: an empty but valid capture.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, path, err = writeCapture(ctx, &brokenStream{bytes.NewReader(nil)}, layers.LinkTypeEthernet, filepath.Join(dir, "cancelled.pcap"), captureOptions{maxBytes: 1 << 20}, (&captureEvents{}).reporter(time.Now))
	if err != nil || path == "" || res.packets != 0 {
		t.Fatalf("res=%+v path=%q err=%v", res, path, err)
	}

	if out, err := ReadPcap(path, 0, 0); err != nil || out != `{"packets":[],"total":0}` {
		t.Fatalf("empty read %q %v", out, err)
	}
}

func TestCaptureReporterThrottles(t *testing.T) {
	ev := &captureEvents{}
	clock := time.Unix(1000, 0)
	rep := ev.reporter(func() time.Time { return clock })

	for i := range 100 {
		rep.packet(func() packetSummary { return packetSummary{N: i} })
		rep.stats(captureResult{packets: int64(i)}, false)
		clock = clock.Add(10 * time.Millisecond) // 100 packets/s for 1 s
	}

	if len(ev.summaries) > 2*summariesPerSecond || len(ev.summaries) < summariesPerSecond {
		t.Fatalf("%d summaries", len(ev.summaries))
	}

	if ev.stats < 2 || ev.stats > 3 {
		t.Fatalf("%d stats", ev.stats)
	}

	if rep.dropped == 0 {
		t.Fatal("no summary dropped")
	}
}

func TestHexDumpShortLine(t *testing.T) {
	got := hexDump([]byte("ABC"))
	want := "0000  41 42 43 " + strings.Repeat(" ", 13*3+1) + " |ABC|\n"

	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestReadPcapRejectsGarbage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.pcap")
	if err := os.WriteFile(p, []byte("not a capture at all"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadPcap(p, 0, 10); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestFilterIPv6(t *testing.T) {
	ip := &layers.IPv6{Version: 6, HopLimit: 64, NextHeader: layers.IPProtocolUDP, SrcIP: ip6A, DstIP: ip6B}
	udp := &layers.UDP{SrcPort: 5353, DstPort: 53}

	if err := udp.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatal(err)
	}

	pkt := serialize(t, &layers.Ethernet{SrcMAC: macA, DstMAC: macB, EthernetType: layers.EthernetTypeIPv6}, ip, udp, gopacket.Payload("q"))

	for expr, want := range map[string]bool{
		"ip6 host 2001:db8::2":        true,
		"src host 2001:db8::2":        false,
		"src net 2001:db8::/32":       true,
		"net 2001:db9::/32":           false,
		"host 2001:db8::3":            false,
		"udp dst port 53":             true,
		"tcp port 53":                 false,
		"ip6 proto udp and port 5353": true,
		"ip":                          false,
		"net ::/0":                    true,
		"dst net 2001:db8::2/128":     true,
	} {
		ins, err := compileFilter(expr)
		if err != nil {
			t.Fatalf("%q: %v", expr, err)
		}

		if got := runFilter(t, ins, pkt); got != want {
			t.Errorf("%q: got %v", expr, got)
		}
	}
}
