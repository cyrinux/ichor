package ichorgo

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
)

// demoCaptureInterval paces the sample packets, under the summaries' per-second cap.
const demoCaptureInterval = 60 * time.Millisecond

// demoFlow is one sample connection of the node: to a peer's port, TLS or bare ACKs.
type demoFlow struct {
	peer    string
	srcPort uint16
	dstPort uint16
	payload int
}

var demoFlows = []demoFlow{
	{peer: "192.0.2.11", srcPort: 44312, dstPort: 2380, payload: 214},
	{peer: "192.0.2.50", srcPort: 55708, dstPort: 6443, payload: 388},
	{peer: "192.0.2.21", srcPort: 47504, dstPort: 10250, payload: 0},
	{peer: "192.0.2.12", srcPort: 36804, dstPort: 2379, payload: 146},
}

// runDemoCapture is runCapture on the demo: sample packets go through the real pipeline, so
// the summaries, the stats, the file and its viewer are the ones of a real capture.
func runDemoCapture(
	ctx context.Context, node, destPath string, opts captureOptions, listener CaptureListener,
) (captureResult, string, error) {
	_, match, err := captureFilter(opts.filter, false)
	if err != nil {
		return captureResult{}, "", err
	}

	opts.match = match
	stream, sink := io.Pipe()

	defer stream.Close() //nolint:errcheck

	go func() { _ = sink.CloseWithError(writeDemoPackets(ctx, sink, node)) }() //nolint:errcheck

	return writeCapture(ctx, stream, layers.LinkTypeEthernet, destPath, opts, newCaptureReporter(listener))
}

// writeDemoPackets writes a pcap stream until ctx ends (nil: the reader then sees its end).
func writeDemoPackets(ctx context.Context, sink io.Writer, node string) error {
	w := pcapgo.NewWriter(sink)
	if err := w.WriteFileHeader(readSnaplen, layers.LinkTypeEthernet); err != nil {
		return err
	}

	ticker := time.NewTicker(demoCaptureInterval)
	defer ticker.Stop()

	for n := 0; ; n++ {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		data, err := demoPacket(n, node)
		if err != nil {
			return err
		}

		info := gopacket.CaptureInfo{Timestamp: time.Now(), CaptureLength: len(data), Length: len(data)}
		if err := w.WritePacket(info, data); err != nil {
			return err
		}
	}
}

// demoPacket is packet n of the sample traffic: each flow in turn, a request then its ACK.
func demoPacket(n int, node string) ([]byte, error) {
	flow := demoFlows[(n/2)%len(demoFlows)]
	seq := uint32(1000 + n*97)
	src, dst := net.ParseIP(node), net.ParseIP(flow.peer)
	tcp := &layers.TCP{SrcPort: layers.TCPPort(flow.srcPort), DstPort: layers.TCPPort(flow.dstPort), Seq: seq, Ack: seq - 97, ACK: true, Window: 501}
	payload := demoTLSRecord(flow.payload)

	if n%2 == 1 { // the answer: an ACK the other way
		src, dst = dst, src
		tcp.SrcPort, tcp.DstPort = tcp.DstPort, tcp.SrcPort
		payload = nil
	}

	ip := &layers.IPv4{Version: 4, TTL: 64, Protocol: layers.IPProtocolTCP, SrcIP: src, DstIP: dst}
	if err := tcp.SetNetworkLayerForChecksum(ip); err != nil {
		return nil, err
	}

	eth := &layers.Ethernet{
		SrcMAC: net.HardwareAddr{2, 0, 0, 0, 0, 0x10}, DstMAC: net.HardwareAddr{2, 0, 0, 0, 0, 0x11}, EthernetType: layers.EthernetTypeIPv4,
	}
	buf := gopacket.NewSerializeBuffer()
	options := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}

	if err := gopacket.SerializeLayers(buf, options, eth, ip, tcp, gopacket.Payload(payload)); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// demoTLSRecord is a TLS application data record of size bytes; none for 0.
func demoTLSRecord(size int) []byte {
	if size == 0 {
		return nil
	}

	return append([]byte{0x17, 0x03, 0x03, byte(size >> 8), byte(size)}, make([]byte, size)...)
}
