package ichorgo

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
)

// packetSummary is one line of the packet list, like a tcpdump line.
type packetSummary struct {
	N     int    `json:"n"`  // index in the capture file, from 0
	TS    int64  `json:"ts"` // unix ms
	Len   int    `json:"len"`
	Src   string `json:"src"` // ip:port, ip or MAC
	Dst   string `json:"dst"`
	Proto string `json:"proto"`
	Info  string `json:"info"`
}

const maxInfoLen = 160

func decodePacket(data []byte, lt layers.LinkType) gopacket.Packet {
	return gopacket.NewPacket(data, lt, gopacket.DecodeOptions{Lazy: true, NoCopy: true})
}

// summarize decodes one captured packet into its list line.
func summarize(n int, ci gopacket.CaptureInfo, data []byte, lt layers.LinkType) packetSummary {
	s := packetSummary{N: n, TS: ci.Timestamp.UnixMilli(), Len: ci.Length}
	if s.Len == 0 {
		s.Len = len(data)
	}

	describe(decodePacket(data, lt), &s)

	if len(s.Info) > maxInfoLen {
		s.Info = s.Info[:maxInfoLen-1] + "…"
	}

	return s
}

func describe(p gopacket.Packet, s *packetSummary) {
	if eth, ok := p.Layer(layers.LayerTypeEthernet).(*layers.Ethernet); ok {
		s.Src, s.Dst, s.Proto = eth.SrcMAC.String(), eth.DstMAC.String(), "Ethernet"
		s.Info = "ethertype " + eth.EthernetType.String()
	}

	if arp, ok := p.Layer(layers.LayerTypeARP).(*layers.ARP); ok {
		describeARP(arp, s)

		return
	}

	var srcIP, dstIP net.IP

	switch ip := p.NetworkLayer().(type) {
	case *layers.IPv4:
		srcIP, dstIP = ip.SrcIP, ip.DstIP
		s.Proto, s.Info = "IPv4", "proto "+ip.Protocol.String()

		if ip.Flags&layers.IPv4MoreFragments != 0 || ip.FragOffset != 0 {
			s.Src, s.Dst = srcIP.String(), dstIP.String()
			s.Info = fmt.Sprintf("fragment offset %d, proto %s", int(ip.FragOffset)*8, ip.Protocol)

			return
		}
	case *layers.IPv6:
		srcIP, dstIP = ip.SrcIP, ip.DstIP
		s.Proto, s.Info = "IPv6", "next header "+ip.NextHeader.String()
	}

	if srcIP != nil {
		s.Src, s.Dst = srcIP.String(), dstIP.String()
	}

	switch {
	case describeTCP(p, srcIP, dstIP, s):
	case describeUDP(p, srcIP, dstIP, s):
	case describeICMP(p, s):
	default:
		if errLayer := p.ErrorLayer(); errLayer != nil && s.Proto == "" {
			s.Proto, s.Info = "?", "undecodable: "+errLayer.Error().Error()
		}
	}
}

func describeARP(arp *layers.ARP, s *packetSummary) {
	s.Proto = "ARP"
	s.Src = net.HardwareAddr(arp.SourceHwAddress).String()
	sender, target := net.IP(arp.SourceProtAddress).String(), net.IP(arp.DstProtAddress).String()

	switch arp.Operation {
	case layers.ARPRequest:
		s.Info = "who-has " + target + " tell " + sender
	case layers.ARPReply:
		s.Info = sender + " is-at " + s.Src
	default:
		s.Info = "operation " + strconv.Itoa(int(arp.Operation))
	}
}

func hostPort(ip net.IP, port int) string {
	if ip == nil {
		return strconv.Itoa(port)
	}

	return net.JoinHostPort(ip.String(), strconv.Itoa(port))
}

func describeTCP(p gopacket.Packet, srcIP, dstIP net.IP, s *packetSummary) bool {
	tcp, ok := p.Layer(layers.LayerTypeTCP).(*layers.TCP)
	if !ok {
		return false
	}

	s.Proto = "TCP"
	s.Src, s.Dst = hostPort(srcIP, int(tcp.SrcPort)), hostPort(dstIP, int(tcp.DstPort))

	info := fmt.Sprintf("%d → %d [%s] seq %d", tcp.SrcPort, tcp.DstPort, tcpFlags(tcp), tcp.Seq)
	if tcp.ACK {
		info += fmt.Sprintf(" ack %d", tcp.Ack)
	}

	s.Info = fmt.Sprintf("%s win %d len %d", info, tcp.Window, len(tcp.Payload))

	payload := tcp.Payload
	if len(payload) == 0 {
		return true
	}

	if tcp.SrcPort == 53 || tcp.DstPort == 53 {
		if len(payload) > 2 && describeDNS(payload[2:], s) {
			return true
		}
	}

	if proto, info := appProtocol(payload); proto != "" {
		s.Proto, s.Info = proto, info
	}

	return true
}

// tcpFlags renders the flags like tcpdump: S (SYN), F (FIN), P (PUSH), R (RST), U (URG),
// E (ECE), W (CWR) and "." for ACK.
func tcpFlags(t *layers.TCP) string {
	var b strings.Builder

	for _, f := range []struct {
		set  bool
		char byte
	}{{t.SYN, 'S'}, {t.FIN, 'F'}, {t.PSH, 'P'}, {t.RST, 'R'}, {t.URG, 'U'}, {t.ECE, 'E'}, {t.CWR, 'W'}, {t.ACK, '.'}} {
		if f.set {
			b.WriteByte(f.char)
		}
	}

	if b.Len() == 0 {
		return "none"
	}

	return b.String()
}

func describeUDP(p gopacket.Packet, srcIP, dstIP net.IP, s *packetSummary) bool {
	udp, ok := p.Layer(layers.LayerTypeUDP).(*layers.UDP)
	if !ok {
		return false
	}

	s.Proto = "UDP"
	s.Src, s.Dst = hostPort(srcIP, int(udp.SrcPort)), hostPort(dstIP, int(udp.DstPort))
	s.Info = fmt.Sprintf("%d → %d len %d", udp.SrcPort, udp.DstPort, len(udp.Payload))

	ports := func(ps ...layers.UDPPort) bool {
		for _, port := range ps {
			if udp.SrcPort == port || udp.DstPort == port {
				return true
			}
		}

		return false
	}

	switch {
	case ports(53, 5353, 5355) && describeDNS(udp.Payload, s):
	case ports(51820) && len(udp.Payload) >= 4:
		s.Proto, s.Info = "WireGuard", wireGuardType(udp.Payload[0])+fmt.Sprintf(", len %d", len(udp.Payload))
	case ports(4789, 8472):
		s.Proto = "VXLAN"
	case ports(6081):
		s.Proto = "Geneve"
	case ports(123):
		s.Proto = "NTP"
	case ports(67, 68):
		s.Proto = "DHCP"
	case ports(546, 547):
		s.Proto = "DHCPv6"
	}

	return true
}

func wireGuardType(t byte) string {
	switch t {
	case 1:
		return "handshake initiation"
	case 2:
		return "handshake response"
	case 3:
		return "cookie reply"
	case 4:
		return "transport data"
	default:
		return "type " + strconv.Itoa(int(t))
	}
}

func describeDNS(payload []byte, s *packetSummary) bool {
	var dns layers.DNS
	if err := dns.DecodeFromBytes(payload, gopacket.NilDecodeFeedback); err != nil || len(dns.Questions) == 0 && !dns.QR {
		return false
	}

	s.Proto = "DNS"

	var b strings.Builder

	if dns.QR {
		fmt.Fprintf(&b, "response 0x%04x", dns.ID)
	} else {
		fmt.Fprintf(&b, "query 0x%04x", dns.ID)
	}

	for _, q := range dns.Questions {
		fmt.Fprintf(&b, " %s %s", q.Type, q.Name)
	}

	if dns.QR {
		if dns.ResponseCode != layers.DNSResponseCodeNoErr {
			b.WriteString(" " + dns.ResponseCode.String())
		}

		for i, a := range dns.Answers {
			if i == 4 {
				fmt.Fprintf(&b, " (+%d)", len(dns.Answers)-i)

				break
			}

			fmt.Fprintf(&b, " %s %s", a.Type, dnsAnswerValue(a))
		}
	}

	s.Info = b.String()

	return true
}

func dnsAnswerValue(a layers.DNSResourceRecord) string {
	switch {
	case a.IP != nil:
		return a.IP.String()
	case len(a.CNAME) > 0:
		return string(a.CNAME)
	case len(a.PTR) > 0:
		return string(a.PTR)
	case len(a.NS) > 0:
		return string(a.NS)
	case a.Type == layers.DNSTypeSRV:
		return fmt.Sprintf("%s:%d", a.SRV.Name, a.SRV.Port)
	case len(a.TXTs) > 0:
		return strconv.Quote(string(a.TXTs[0]))
	default:
		return fmt.Sprintf("(%d bytes)", len(a.Data))
	}
}

func describeICMP(p gopacket.Packet, s *packetSummary) bool {
	if icmp, ok := p.Layer(layers.LayerTypeICMPv4).(*layers.ICMPv4); ok {
		s.Proto = "ICMP"
		s.Info = icmpv4Text(icmp)

		return true
	}

	icmp, ok := p.Layer(layers.LayerTypeICMPv6).(*layers.ICMPv6)
	if !ok {
		return false
	}

	s.Proto = "ICMPv6"
	s.Info = icmp.TypeCode.String()

	switch icmp.TypeCode.Type() {
	case layers.ICMPv6TypeEchoRequest, layers.ICMPv6TypeEchoReply:
		if echo, ok := p.Layer(layers.LayerTypeICMPv6Echo).(*layers.ICMPv6Echo); ok {
			kind := "echo request"
			if icmp.TypeCode.Type() == layers.ICMPv6TypeEchoReply {
				kind = "echo reply"
			}

			s.Info = fmt.Sprintf("%s id %d seq %d", kind, echo.Identifier, echo.SeqNumber)
		}
	case layers.ICMPv6TypeNeighborSolicitation:
		if ns, ok := p.Layer(layers.LayerTypeICMPv6NeighborSolicitation).(*layers.ICMPv6NeighborSolicitation); ok {
			s.Info = "neighbor solicitation, who has " + ns.TargetAddress.String()
		}
	case layers.ICMPv6TypeNeighborAdvertisement:
		if na, ok := p.Layer(layers.LayerTypeICMPv6NeighborAdvertisement).(*layers.ICMPv6NeighborAdvertisement); ok {
			s.Info = "neighbor advertisement, tgt is " + na.TargetAddress.String()
		}
	case layers.ICMPv6TypeRouterSolicitation:
		s.Info = "router solicitation"
	case layers.ICMPv6TypeRouterAdvertisement:
		s.Info = "router advertisement"
	}

	return true
}

func icmpv4Text(icmp *layers.ICMPv4) string {
	switch icmp.TypeCode.Type() {
	case layers.ICMPv4TypeEchoRequest:
		return fmt.Sprintf("echo request id %d seq %d", icmp.Id, icmp.Seq)
	case layers.ICMPv4TypeEchoReply:
		return fmt.Sprintf("echo reply id %d seq %d", icmp.Id, icmp.Seq)
	case layers.ICMPv4TypeDestinationUnreachable:
		return "destination unreachable (" + icmpCodeText(icmp.TypeCode.String()) + ")"
	case layers.ICMPv4TypeTimeExceeded:
		return "time exceeded (" + icmpCodeText(icmp.TypeCode.String()) + ")"
	default:
		return icmp.TypeCode.String()
	}
}

// icmpCodeText extracts "Port" from gopacket's "DestinationUnreachable(Port)".
func icmpCodeText(s string) string {
	if i := strings.IndexByte(s, '('); i >= 0 && strings.HasSuffix(s, ")") {
		return strings.ToLower(s[i+1 : len(s)-1])
	}

	return s
}

// appProtocol recognises TLS and HTTP/1 in a TCP payload, whatever the port.
func appProtocol(payload []byte) (string, string) {
	if info := tlsInfo(payload); info != "" {
		return "TLS", info
	}

	if line := httpFirstLine(payload); line != "" {
		return "HTTP", line
	}

	return "", ""
}

var tlsContentTypes = map[byte]string{20: "change cipher spec", 21: "alert", 22: "handshake", 23: "application data"}

func tlsInfo(p []byte) string {
	if len(p) < 5 || p[1] != 3 || p[2] > 4 {
		return ""
	}

	kind, ok := tlsContentTypes[p[0]]
	if !ok {
		return ""
	}

	if p[0] != 22 || len(p) < 6 {
		return kind
	}

	switch p[5] {
	case 1:
		if sni := clientHelloSNI(p[5:]); sni != "" {
			return "client hello, SNI " + sni
		}

		return "client hello"
	case 2:
		return "server hello"
	default:
		return kind
	}
}

// clientHelloSNI extracts the server_name extension from a (possibly truncated) ClientHello
// handshake message.
func clientHelloSNI(h []byte) string {
	// type(1) length(3) version(2) random(32)
	pos := 38
	if len(h) < pos+1 {
		return ""
	}

	pos += 1 + int(h[pos]) // session id

	if len(h) < pos+2 {
		return ""
	}

	pos += 2 + int(binary.BigEndian.Uint16(h[pos:])) // cipher suites

	if len(h) < pos+1 {
		return ""
	}

	pos += 1 + int(h[pos]) // compression methods

	if len(h) < pos+2 {
		return ""
	}

	end := min(len(h), pos+2+int(binary.BigEndian.Uint16(h[pos:])))
	pos += 2

	for pos+4 <= end {
		extType, extLen := binary.BigEndian.Uint16(h[pos:]), int(binary.BigEndian.Uint16(h[pos+2:]))
		pos += 4

		if extType == 0 && pos+extLen <= end && extLen >= 5 {
			// list length(2) name type(1) name length(2) name
			nameLen := int(binary.BigEndian.Uint16(h[pos+3:]))
			if h[pos+2] == 0 && pos+5+nameLen <= end {
				return string(h[pos+5 : pos+5+nameLen])
			}

			return ""
		}

		pos += extLen
	}

	return ""
}

var httpPrefixes = []string{"GET ", "POST ", "PUT ", "DELETE ", "HEAD ", "OPTIONS ", "PATCH ", "CONNECT ", "HTTP/1."}

func httpFirstLine(p []byte) string {
	head := string(p[:min(len(p), 200)])

	for _, prefix := range httpPrefixes {
		if strings.HasPrefix(head, prefix) {
			line, _, _ := strings.Cut(head, "\r\n")

			return line
		}
	}

	return ""
}
