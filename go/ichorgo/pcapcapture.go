package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

const (
	defaultCaptureSeconds = 60
	maxCaptureSeconds     = 600
	defaultCaptureBytes   = 20 << 20
	maxCaptureBytes       = 200 << 20
	maxSnapLen            = 65535

	// pcapFileHeaderLen and pcapPacketHeaderLen count towards the byte limit.
	pcapFileHeaderLen   = 24
	pcapPacketHeaderLen = 16

	summariesPerSecond = 20
	statsInterval      = 500 * time.Millisecond
)

// CaptureListener receives a running packet capture (implemented in Kotlin/Swift).
type CaptureListener interface {
	// OnPacket gets a packet summary JSON (see packetSummary), at most ~20 per second: the
	// others are written to the file but not summarised live ("n" skips them).
	OnPacket(summaryJSON string)
	// OnStats gets the packets and bytes written so far, about twice a second.
	OnStats(packets int64, bytes int64)
	// OnDone is called exactly once. path is the finished pcap file ("" when nothing was
	// kept); errMessage is empty on success, which includes Cancel and the time/size limits.
	// On an error after some packets, the partial capture is still kept at path.
	OnDone(path string, packets int64, bytes int64, errMessage string)
}

// CaptureRun is a handle on a running packet capture.
type CaptureRun struct {
	cancel context.CancelFunc
}

// Cancel stops the capture; what was captured is kept and OnDone reports success.
func (r *CaptureRun) Cancel() { r.cancel() }

type captureOptions struct {
	iface       string
	filter      string
	promiscuous bool
	snapLen     int
	maxBytes    int64
	match       func([]byte) bool // client-side filter, nil keeps everything
}

type captureResult struct {
	packets, bytes int64
}

// StartPacketCapture captures packets on node's interface iface into destPath (a pcap file),
// like `talosctl pcap -i IFACE --bpf-filter ...` (os:operator or os:admin). filter is a
// tcpdump expression (see ValidateCaptureFilter); the Talos API port (50000) is always
// excluded so the capture does not feed on its own stream. iface must name one link (Talos
// has no "any" interface); Ethernet, loopback and raw (WireGuard/KubeSpan) links work.
// snapLen truncates the saved packets (0: whole packets). The capture stops after maxSeconds
// (0: 60 s, at most 600) or maxBytes of file (0: 20 MiB, at most 200 MiB), or on Cancel.
// The file is written to destPath.part and renamed when the capture ends; the packet bytes
// are never masked (the file is the user's capture).
func StartPacketCapture(
	configYAML, contextName, node, iface, filter string, promiscuous bool, snapLen int,
	maxSeconds int, maxBytes int64, destPath string, listener CaptureListener,
) *CaptureRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)

	listener = maskedCaptureListener{listener}

	opts := captureOptions{
		iface:       strings.TrimSpace(iface),
		filter:      filter,
		promiscuous: promiscuous,
		snapLen:     clampSnapLen(snapLen),
		maxBytes:    clampOr(maxBytes, defaultCaptureBytes, maxCaptureBytes),
	}

	seconds := clampOr(int64(maxSeconds), defaultCaptureSeconds, maxCaptureSeconds)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)

	go func() {
		defer cancel()

		res, path, err := runCapture(ctx, configYAML, contextName, node, destPath, opts, listener)

		errMessage := ""
		if err != nil {
			errMessage = err.Error()
		}

		listener.OnDone(path, res.packets, res.bytes, errMessage)
	}()

	return &CaptureRun{cancel: cancel}
}

func clampSnapLen(n int) int {
	if n <= 0 || n >= maxSnapLen {
		return 0
	}

	return max(n, 64)
}

func runCapture(
	ctx context.Context, configYAML, contextName, node, destPath string, opts captureOptions, listener CaptureListener,
) (captureResult, string, error) {
	if opts.iface == "" {
		return captureResult{}, "", errors.New("pick an interface to capture on")
	}

	s, release, err := sessions.acquire(configYAML, contextName)
	if err != nil {
		return captureResult{}, "", err
	}

	defer release()

	if err := validatePowerTarget(s.context, node); err != nil {
		return captureResult{}, "", err
	}

	nodeCtx := withNode(ctx, node)

	lt, err := captureLinkType(nodeCtx, s.client.COSI, opts.iface)
	if err != nil {
		return captureResult{}, "", err
	}

	bpfFilter, match, err := captureFilter(opts.filter, lt == layers.LinkTypeRaw)
	if err != nil {
		return captureResult{}, "", err
	}

	stream, err := s.client.PacketCapture(nodeCtx, &machineapi.PacketCaptureRequest{
		Interface:   opts.iface,
		Promiscuous: opts.promiscuous,
		SnapLen:     uint32(opts.snapLen),
		BpfFilter:   bpfFilter,
	})

	if err != nil {
		return captureResult{}, "", errors.New(s.friendly(node, err))
	}

	opts.match = match

	defer stream.Close() //nolint:errcheck

	return writeCapture(ctx, stream, lt, destPath, opts, newCaptureReporter(listener))
}

// captureLinkType reads the link's type like Talos does to pick the pcap link type.
func captureLinkType(ctx context.Context, st state.State, iface string) (layers.LinkType, error) {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	link, err := safe.StateGetByID[*network.LinkStatus](ctx, st, iface)
	if err != nil {
		if state.IsNotFoundError(err) {
			return 0, fmt.Errorf("interface %q not found on the node", iface)
		}

		return 0, errors.New(friendlyError(err))
	}

	switch t := link.TypedSpec().Type; t { //nolint:exhaustive
	case nethelpers.LinkEther, nethelpers.LinkLoopbck:
		return layers.LinkTypeEthernet, nil
	case nethelpers.LinkNone:
		return layers.LinkTypeRaw, nil
	default:
		return 0, fmt.Errorf("cannot capture on %s: unsupported link type %s", iface, t)
	}
}

// captureReporter throttles the listener: summaries up to summariesPerSecond, stats every
// statsInterval.
type captureReporter struct {
	listener    CaptureListener
	now         func() time.Time
	windowStart time.Time
	inWindow    int
	lastStats   time.Time
	dropped     int64
}

func newCaptureReporter(l CaptureListener) *captureReporter {
	return &captureReporter{listener: l, now: time.Now}
}

func (r *captureReporter) packet(summary func() packetSummary) {
	now := r.now()
	if now.Sub(r.windowStart) >= time.Second {
		r.windowStart, r.inWindow = now, 0
	}

	if r.inWindow >= summariesPerSecond {
		r.dropped++

		return
	}

	r.inWindow++

	b, err := json.Marshal(summary())
	if err == nil {
		r.listener.OnPacket(string(b))
	}
}

func (r *captureReporter) stats(res captureResult, force bool) {
	now := r.now()
	if !force && now.Sub(r.lastStats) < statsInterval {
		return
	}

	r.lastStats = now
	r.listener.OnStats(res.packets, res.bytes)
}

// writeCapture re-writes the pcap stream from Talos packet by packet into destPath.part, so
// the file stays a valid pcap whatever stops it, then renames it to destPath. Ending through
// ctx (Cancel, time limit) or the size limit is a success.
func writeCapture(
	ctx context.Context, stream io.Reader, lt layers.LinkType, destPath string, opts captureOptions, rep *captureReporter,
) (captureResult, string, error) {
	part := destPath + ".part"

	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return captureResult{}, "", err
	}

	snaplen := uint32(readSnaplen)
	if opts.snapLen > 0 {
		snaplen = uint32(opts.snapLen)
	}

	w := pcapgo.NewWriter(f)
	if err := w.WriteFileHeader(snaplen, lt); err != nil {
		return failCapture(f, part, captureResult{}, err)
	}

	res := captureResult{bytes: pcapFileHeaderLen}

	copyErr := copyPackets(ctx, stream, w, lt, opts, rep, &res)

	rep.stats(res, true)

	if err := f.Sync(); err != nil {
		return failCapture(f, part, res, err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(part) //nolint:errcheck

		return captureResult{}, "", err
	}

	if copyErr != nil && res.packets == 0 {
		_ = os.Remove(part) //nolint:errcheck

		return res, "", copyErr
	}

	if err := os.Rename(part, destPath); err != nil {
		_ = os.Remove(part) //nolint:errcheck

		return captureResult{}, "", err
	}

	return res, destPath, copyErr
}

func copyPackets(
	ctx context.Context, stream io.Reader, w *pcapgo.Writer, lt layers.LinkType, opts captureOptions,
	rep *captureReporter, res *captureResult,
) error {
	r, err := pcapgo.NewReader(stream)
	if err != nil {
		return streamEnd(ctx, err)
	}

	r.SetSnaplen(readSnaplen)

	for {
		data, ci, err := r.ReadPacketData()
		if err != nil {
			return streamEnd(ctx, err)
		}

		if opts.match != nil && !opts.match(data) {
			continue
		}

		if opts.snapLen > 0 && len(data) > opts.snapLen {
			data = data[:opts.snapLen]
			ci.CaptureLength = opts.snapLen
		}

		ci.Length = max(ci.Length, ci.CaptureLength)

		size := int64(pcapPacketHeaderLen + len(data))
		if res.bytes+size > opts.maxBytes {
			return nil
		}

		if err := w.WritePacket(ci, data); err != nil {
			return err
		}

		n := int(res.packets)
		res.packets++
		res.bytes += size

		rep.packet(func() packetSummary { return summarize(n, ci, data, lt) })
		rep.stats(*res, false)
	}
}

// streamEnd tells a capture that ended (EOF, or the stream cut by Cancel or the time limit)
// from a failure.
func streamEnd(ctx context.Context, err error) error {
	if errors.Is(err, io.EOF) || ctx.Err() != nil {
		return nil
	}

	return errors.New(friendlyError(err))
}

func failCapture(f *os.File, part string, res captureResult, err error) (captureResult, string, error) {
	_ = f.Close()       //nolint:errcheck
	_ = os.Remove(part) //nolint:errcheck

	return res, "", err
}

// maskedCaptureListener masks the live summaries and the error; the file is not masked.
type maskedCaptureListener struct{ CaptureListener }

func (l maskedCaptureListener) OnPacket(summaryJSON string) {
	l.CaptureListener.OnPacket(privacy.mask(summaryJSON))
}

func (l maskedCaptureListener) OnDone(path string, packets int64, bytes int64, errMessage string) {
	l.CaptureListener.OnDone(path, packets, bytes, privacy.maskPlain(errMessage))
}
