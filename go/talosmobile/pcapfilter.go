package talosmobile

import (
	"errors"
	"fmt"
	"strings"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"golang.org/x/net/bpf"
)

// talosAPIExclusion keeps the capture's own stream (and apid's node-to-node proxying) out of
// the capture: without it, capturing the link the API runs on feeds on itself.
const talosAPIExclusion = "not port 50000"

// maxBPFInstructions is the kernel's BPF_MAXINSNS.
const maxBPFInstructions = 4096

// ethernetHeaderLen is the offset of the network header in the compiled programs, which
// are built for an Ethernet link and rebased for raw links.
const ethernetHeaderLen = 14

// ValidateCaptureFilter checks a capture filter and returns "" when it compiles, or a short
// error message. The syntax is a tcpdump subset (see pcapfilter_prim.go): ip, ip6, arp, tcp,
// udp, icmp, icmp6; [src|dst] host IP; [src|dst] net CIDR; [tcp|udp] [src|dst] port N;
// [ip|ip6] proto N; ether [src|dst] host MAC; combined with and/or/not (&&, ||, !) and
// parentheses. Host names, VLAN, portrange and byte offsets (tcp[13]) are not supported.
func ValidateCaptureFilter(expr string) (out string) {
	defer maskResult(&out, new(error))

	if _, err := compileFilter(expr); err != nil {
		return err.Error()
	}

	return ""
}

// compileFilter compiles a filter expression for an Ethernet link; empty means no filter.
func compileFilter(expr string) ([]bpf.Instruction, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, nil
	}

	ins, err := parseFilter(expr)
	if err != nil {
		return nil, err
	}

	if _, err := assembleFilter(ins); err != nil {
		return nil, err
	}

	return ins, nil
}

func assembleFilter(ins []bpf.Instruction) ([]bpf.RawInstruction, error) {
	if len(ins) > maxBPFInstructions {
		return nil, errors.New("filter too complex")
	}

	raw, err := bpf.Assemble(ins)
	if err != nil {
		return nil, fmt.Errorf("filter too complex: %w", err)
	}

	return raw, nil
}

// captureFilter builds the kernel filter sent to Talos: the user's expression (if any) with
// the Talos API traffic excluded, for an Ethernet link or, when raw, for a link without a
// link-layer header (WireGuard/KubeSpan). It also returns the same filter to run on the
// received packets: Talos attaches the kernel filter after its socket starts receiving, so
// the first few packets of a capture can be unfiltered.
func captureFilter(expr string, raw bool) ([]*machineapi.BPFInstruction, func([]byte) bool, error) {
	full := talosAPIExclusion
	if expr = strings.TrimSpace(expr); expr != "" {
		full = "(" + expr + ") and " + talosAPIExclusion
	}

	ins, err := compileFilter(full)
	if err != nil {
		return nil, nil, err
	}

	vm, err := bpf.NewVM(ins)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid filter: %w", err)
	}

	kernel := ins
	if raw {
		if kernel, err = rawLinkProgram(ins); err != nil {
			return nil, nil, err
		}
	}

	assembled, err := assembleFilter(kernel)
	if err != nil {
		return nil, nil, err
	}

	out := make([]*machineapi.BPFInstruction, 0, len(assembled))
	for _, r := range assembled {
		out = append(out, &machineapi.BPFInstruction{Op: uint32(r.Op), Jt: uint32(r.Jt), Jf: uint32(r.Jf), K: r.K})
	}

	return out, localFilter(vm, raw), nil
}

// localFilter runs the Ethernet program on a received packet; a raw link's IP packet gets a
// stand-in Ethernet header carrying the EtherType of its IP version.
func localFilter(vm *bpf.VM, raw bool) func([]byte) bool {
	return func(data []byte) bool {
		pkt := data

		if raw {
			if len(data) == 0 {
				return false
			}

			header := make([]byte, ethernetHeaderLen, ethernetHeaderLen+len(data))

			switch data[0] >> 4 {
			case 4:
				header[12], header[13] = 0x08, 0x00
			case 6:
				header[12], header[13] = 0x86, 0xdd
			}

			pkt = append(header, data...)
		}

		n, err := vm.Run(pkt)

		return err == nil && n > 0
	}
}

// rawLinkProgram rebases an Ethernet program onto a link whose packets start with the IP
// header, like libpcap does for DLT_RAW: the EtherType comes from the kernel's skb protocol
// (SKF_AD_PROTOCOL) and every other offset moves back by the Ethernet header.
func rawLinkProgram(in []bpf.Instruction) ([]bpf.Instruction, error) {
	errEthernet := errors.New("this filter matches Ethernet fields, which this interface does not have")
	out := make([]bpf.Instruction, len(in))

	for i, ins := range in {
		switch v := ins.(type) {
		case bpf.LoadAbsolute:
			switch {
			case v.Off == 12 && v.Size == 2:
				out[i] = bpf.LoadExtension{Num: bpf.ExtProto}
			case v.Off >= ethernetHeaderLen:
				out[i] = bpf.LoadAbsolute{Off: v.Off - ethernetHeaderLen, Size: v.Size}
			default:
				return nil, errEthernet
			}
		case bpf.LoadIndirect:
			if v.Off < ethernetHeaderLen {
				return nil, errEthernet
			}

			out[i] = bpf.LoadIndirect{Off: v.Off - ethernetHeaderLen, Size: v.Size}
		case bpf.LoadMemShift:
			if v.Off < ethernetHeaderLen {
				return nil, errEthernet
			}

			out[i] = bpf.LoadMemShift{Off: v.Off - ethernetHeaderLen}
		default:
			out[i] = ins
		}
	}

	return out, nil
}
