package ichorgo

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"

	"golang.org/x/net/bpf"
)

// Filter primitives, for an Ethernet frame (rawLinkProgram rebases them). Ports are read
// past the IPv4 header length (fragments other than the first never match) and right after
// the fixed IPv6 header (no extension headers, like libpcap).

const (
	etherTypeIPv4 = 0x0800
	etherTypeIPv6 = 0x86dd
	etherTypeARP  = 0x0806

	ipProtoICMP   = 1
	ipProtoTCP    = 6
	ipProtoUDP    = 17
	ipProtoICMPv6 = 58
)

func loadAbs(off uint32, size int) []bpf.Instruction {
	return []bpf.Instruction{bpf.LoadAbsolute{Off: off, Size: size}}
}

func eq(load []bpf.Instruction, val uint32) filterNode {
	return filterTest{load: load, cond: bpf.JumpEqual, val: val}
}

func isEtherType(t uint32) filterNode { return eq(loadAbs(12, 2), t) }

func isIPv4() filterNode { return isEtherType(etherTypeIPv4) }

func isIPv6() filterNode { return isEtherType(etherTypeIPv6) }

func ipv4Proto(p uint32) filterNode { return and(isIPv4(), eq(loadAbs(23, 1), p)) }

func ipv6Proto(p uint32) filterNode { return and(isIPv6(), eq(loadAbs(20, 1), p)) }

func anyIPProto(p uint32) filterNode { return or(ipv4Proto(p), ipv6Proto(p)) }

// notFragment: the IPv4 fragment offset is 0 (the first fragment carries the ports).
func notFragment() filterNode {
	return filterNot{filterTest{load: loadAbs(20, 2), cond: bpf.JumpBitsSet, val: 0x1fff}}
}

// parsePrimitive compiles one primitive (the tokens between operators).
func parsePrimitive(tokens []string) (filterNode, error) {
	words := make([]string, len(tokens))
	for i, t := range tokens {
		words[i] = strings.ToLower(t)
	}

	text := strings.Join(tokens, " ")

	if len(words) == 1 {
		if n, ok := protocolPrimitive(words[0]); ok {
			return n, nil
		}
	}

	proto := ""
	if len(words) > 1 {
		switch words[0] {
		case "ip", "ip6", "tcp", "udp", "ether", "arp":
			proto, words = words[0], words[1:]
		}
	}

	dir := ""
	if len(words) > 1 && (words[0] == "src" || words[0] == "dst") {
		dir, words = words[0], words[1:]
	}

	kind := ""
	if len(words) > 1 {
		kind, words = words[0], words[1:]
	} else if proto == "ether" && dir != "" {
		kind = "host" // "ether src MAC"
	}

	if len(words) != 1 {
		return nil, fmt.Errorf("unsupported filter %q", text)
	}

	value := tokens[len(tokens)-1]

	switch {
	case proto == "ether" && kind == "host":
		return etherHost(dir, value)
	case kind == "host" && (proto == "" || proto == "ip" || proto == "ip6"):
		return hostPrimitive(proto, dir, value)
	case kind == "net" && (proto == "" || proto == "ip" || proto == "ip6"):
		return netPrimitive(proto, dir, value)
	case kind == "port" && (proto == "" || proto == "tcp" || proto == "udp"):
		return portPrimitive(proto, dir, value)
	case kind == "proto" && dir == "" && (proto == "" || proto == "ip" || proto == "ip6"):
		return protoNumberPrimitive(proto, value)
	case kind == "portrange", kind == "vlan", kind == "less", kind == "greater", kind == "gateway":
		return nil, fmt.Errorf("%q is not supported", kind)
	default:
		return nil, fmt.Errorf("unsupported filter %q", text)
	}
}

func protocolPrimitive(word string) (filterNode, bool) {
	switch word {
	case "ip":
		return isIPv4(), true
	case "ip6":
		return isIPv6(), true
	case "arp":
		return isEtherType(etherTypeARP), true
	case "tcp":
		return anyIPProto(ipProtoTCP), true
	case "udp":
		return anyIPProto(ipProtoUDP), true
	case "icmp":
		return ipv4Proto(ipProtoICMP), true
	case "icmp6":
		return ipv6Proto(ipProtoICMPv6), true
	}

	return nil, false
}

var ipProtoNames = map[string]uint32{"icmp": ipProtoICMP, "tcp": ipProtoTCP, "udp": ipProtoUDP, "icmp6": ipProtoICMPv6}

func protoNumberPrimitive(proto, value string) (filterNode, error) {
	p, ok := ipProtoNames[strings.ToLower(value)]
	if !ok {
		n, err := strconv.ParseUint(value, 10, 8)
		if err != nil {
			return nil, fmt.Errorf("invalid protocol %q", value)
		}

		p = uint32(n)
	}

	switch proto {
	case "ip":
		return ipv4Proto(p), nil
	case "ip6":
		return ipv6Proto(p), nil
	default:
		return anyIPProto(p), nil
	}
}

// directions returns the IPv4/IPv6 address offsets to test for dir ("" = src or dst).
func directions(dir string, src, dst uint32) []uint32 {
	switch dir {
	case "src":
		return []uint32{src}
	case "dst":
		return []uint32{dst}
	default:
		return []uint32{src, dst}
	}
}

func hostPrimitive(proto, dir, value string) (filterNode, error) {
	ip := net.ParseIP(value)
	if ip == nil {
		return nil, fmt.Errorf("%q is not an IP address (host names are not supported)", value)
	}

	bits := 128
	if ip.To4() != nil {
		bits = 32
	}

	return addressMatch(proto, dir, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
}

func netPrimitive(proto, dir, value string) (filterNode, error) {
	_, cidr, err := net.ParseCIDR(value)
	if err != nil {
		if ip := net.ParseIP(value); ip != nil {
			return hostPrimitive(proto, dir, value)
		}

		return nil, fmt.Errorf("invalid network %q (use CIDR notation, e.g. 10.0.0.0/8)", value)
	}

	return addressMatch(proto, dir, cidr)
}

func addressMatch(proto, dir string, n *net.IPNet) (filterNode, error) {
	if v4 := n.IP.To4(); v4 != nil && len(n.Mask) == net.IPv4len {
		if proto == "ip6" {
			return nil, fmt.Errorf("%s is not an IPv6 address", n.IP)
		}

		var tests []filterNode
		for _, off := range directions(dir, 26, 30) {
			tests = append(tests, addrWord(off, v4, n.Mask))
		}

		return and(isIPv4(), or(tests...)), nil
	}

	if proto == "ip" {
		return nil, fmt.Errorf("%s is not an IPv4 address", n.IP)
	}

	ip16 := n.IP.To16()

	var tests []filterNode

	for _, off := range directions(dir, 22, 38) {
		var words []filterNode

		for w := 0; w < 4; w++ {
			mask := n.Mask[w*4 : w*4+4]
			if binary.BigEndian.Uint32(mask) == 0 {
				break
			}

			words = append(words, addrWord(off+uint32(w*4), ip16[w*4:w*4+4], mask))
		}

		if len(words) == 0 {
			words = append(words, filterConst(true))
		}

		tests = append(tests, and(words...))
	}

	return and(isIPv6(), or(tests...)), nil
}

func addrWord(off uint32, addr []byte, mask net.IPMask) filterNode {
	m := binary.BigEndian.Uint32(mask)
	load := loadAbs(off, 4)

	if m != 0xffffffff {
		load = append(load, bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: m})
	}

	return eq(load, binary.BigEndian.Uint32(addr)&m)
}

func portPrimitive(proto, dir, value string) (filterNode, error) {
	port, err := parsePort(value)
	if err != nil {
		return nil, err
	}

	transports := map[string][]uint32{"": {ipProtoTCP, ipProtoUDP}, "tcp": {ipProtoTCP}, "udp": {ipProtoUDP}}[proto]

	var v4Proto, v6Proto []filterNode
	for _, t := range transports {
		v4Proto = append(v4Proto, eq(loadAbs(23, 1), t))
		v6Proto = append(v6Proto, eq(loadAbs(20, 1), t))
	}

	var v4Ports, v6Ports []filterNode

	for _, off := range directions(dir, 0, 2) {
		indirect := []bpf.Instruction{bpf.LoadMemShift{Off: 14}, bpf.LoadIndirect{Off: 14 + off, Size: 2}}
		v4Ports = append(v4Ports, eq(indirect, port))
		v6Ports = append(v6Ports, eq(loadAbs(54+off, 2), port))
	}

	return or(
		and(isIPv4(), or(v4Proto...), notFragment(), or(v4Ports...)),
		and(isIPv6(), or(v6Proto...), or(v6Ports...)),
	), nil
}

var portNames = map[string]uint32{
	"ssh": 22, "domain": 53, "dns": 53, "http": 80, "ntp": 123, "https": 443,
	"etcd": 2379, "kubernetes": 6443, "kubelet": 10250, "talos": 50000, "trustd": 50001, "wireguard": 51820,
}

func parsePort(value string) (uint32, error) {
	if p, ok := portNames[strings.ToLower(value)]; ok {
		return p, nil
	}

	n, err := strconv.ParseUint(value, 10, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q", value)
	}

	return uint32(n), nil
}

func etherHost(dir, value string) (filterNode, error) {
	mac, err := net.ParseMAC(value)
	if err != nil || len(mac) != 6 {
		return nil, fmt.Errorf("invalid MAC address %q", value)
	}

	match := func(off uint32) filterNode {
		return and(eq(loadAbs(off+2, 4), binary.BigEndian.Uint32(mac[2:])), eq(loadAbs(off, 2), uint32(binary.BigEndian.Uint16(mac))))
	}

	switch dir {
	case "src":
		return match(6), nil
	case "dst":
		return match(0), nil
	default:
		return or(match(6), match(0)), nil
	}
}
