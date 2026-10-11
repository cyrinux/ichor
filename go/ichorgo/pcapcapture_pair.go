package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/pcapgo"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// A capture on two nodes at once (both ends of a KubeSpan tunnel, a cross-node flow): each
// node is captured as StartPacketCapture does, then both are merged by time into one pcapng
// file with one interface per node, named after it, so the viewer can tell them apart.

// PairCaptureListener receives a capture on two nodes (implemented in Kotlin/Swift).
type PairCaptureListener interface {
	// OnPacket gets a packet summary JSON (see packetSummary) with "node": the hostname it was
	// captured on; at most ~20 per second per node.
	OnPacket(summaryJSON string)
	// OnStats gets {"packets","bytes","nodes":[{"node","hostname","packets","bytes"}],
	// "clockGapMs"}, the totals and each node's, about twice a second per node; clockGapMs is
	// how far apart the two nodes' clocks are (the merged order across nodes is no better).
	OnStats(statsJSON string)
	// OnDone is called exactly once, as CaptureListener's: path is the merged pcapng file.
	OnDone(path string, packets int64, bytes int64, errMessage string)
}

type pairNodeStats struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	Packets  int64  `json:"packets"`
	Bytes    int64  `json:"bytes"`
}

type pairStats struct {
	Packets int64           `json:"packets"`
	Bytes   int64           `json:"bytes"`
	Nodes   []pairNodeStats `json:"nodes"`
	// ClockGapMs is how far apart the two nodes' clocks are (see pairClockGap); absent when
	// either could not be read.
	ClockGapMs *int64 `json:"clockGapMs,omitempty"`
}

// StartPacketCapturePair captures on two nodes at once (nodes: "nodeA,nodeB"), with the same
// options as StartPacketCapture; iface is one interface for both, or "ifaceA,ifaceB". The size
// limit applies to each node. Both stop together: on Cancel, at the time limit, when either
// reaches its size limit or fails (what was captured is kept). The two captures are merged into
// destPath, a pcapng file with one interface per node named after its hostname.
func StartPacketCapturePair(
	configYAML, contextName, nodes, iface, filter string, promiscuous bool, snapLen int,
	maxSeconds int, maxBytes int64, destPath string, listener PairCaptureListener,
) *CaptureRun {
	contextName, nodes = unmaskTargets(configYAML, contextName, nodes)

	listener = maskedPairCaptureListener{listener}

	seconds := clampOr(int64(maxSeconds), defaultCaptureSeconds, maxCaptureSeconds)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", 0, 0, msg) })

		opts := captureOptions{
			filter:      filter,
			promiscuous: promiscuous,
			snapLen:     clampSnapLen(snapLen),
			maxBytes:    clampOr(maxBytes, defaultCaptureBytes, maxCaptureBytes),
		}

		res, path, err := runCapturePair(ctx, cancel, configYAML, contextName, nodes, iface, destPath, opts, listener)

		listener.OnDone(path, res.packets, res.bytes, errText(err))
	}()

	return &CaptureRun{cancel: cancel}
}

// pairTarget is one end of a pair capture; offsetMs is its clock's offset to its NTP server
// (nil when it could not be read).
type pairTarget struct {
	node, hostname, iface, part string
	offsetMs                    *int64
}

func runCapturePair(
	ctx context.Context, stop context.CancelFunc, configYAML, contextName, nodes, ifaces, destPath string,
	opts captureOptions, listener PairCaptureListener,
) (captureResult, string, error) {
	targets, err := pairTargets(nodes, ifaces, destPath)
	if err != nil {
		return captureResult{}, "", err
	}

	for i := range targets {
		targets[i].hostname, targets[i].offsetMs = pairNodeInfo(ctx, configYAML, contextName, targets[i].node)
	}

	gap := pairClockGap(targets)
	stats := newPairStatsReporter(targets, listener)
	stats.clockGapMs = gap

	type outcome struct {
		path string
		err  error
	}

	outcomes := make([]outcome, len(targets))

	var wg sync.WaitGroup

	for i, t := range targets {
		wg.Add(1)

		go func() {
			defer wg.Done()

			o := opts
			o.iface = t.iface

			res, path, err := runCapture(ctx, configYAML, contextName, t.node, t.part, o, pairNodeListener{stats, i, t.hostname})

			// An end that fills up or fails stops the other one too; one whose stream just ended
			// (the node went away) leaves it running.
			if res.full || err != nil {
				defer stop()
			}

			switch {
			case err != nil && ctx.Err() != nil:
				// The other end stopped the pair (or the time ran out): this end was cut, not failing.
				err = nil
			case err != nil:
				err = fmt.Errorf("%s: %w", t.hostname, err)
			}

			outcomes[i] = outcome{path, err}
		}()
	}

	wg.Wait()

	var (
		inputs []captureInput
		errs   []error
	)

	for i, o := range outcomes {
		errs = append(errs, o.err)

		if o.path != "" {
			inputs = append(inputs, captureInput{path: o.path, name: targets[i].hostname})
		}
	}

	defer func() {
		for _, in := range inputs {
			_ = os.Remove(in.path) //nolint:errcheck
		}
	}()

	if len(inputs) == 0 {
		return captureResult{}, "", errors.Join(errs...)
	}

	res, err := mergeCaptureFiles(destPath, inputs, gap)
	if err != nil {
		return captureResult{}, "", err
	}

	return res, destPath, errors.Join(errs...)
}

// pairTargets splits "nodeA,nodeB" and "iface" or "ifaceA,ifaceB"; each node's capture is
// written next to destPath until they are merged.
func pairTargets(nodes, ifaces, destPath string) ([]pairTarget, error) {
	names := splitTrim(nodes)
	if len(names) != 2 || names[0] == names[1] {
		return nil, errors.New("pick two different nodes")
	}

	links := splitTrim(ifaces)
	switch len(links) {
	case 1:
		links = append(links, links[0])
	case 2:
	default:
		return nil, errors.New("pick an interface to capture on")
	}

	targets := make([]pairTarget, 2)
	for i := range targets {
		targets[i] = pairTarget{node: names[i], hostname: names[i], iface: links[i], part: fmt.Sprintf("%s.node%d", destPath, i+1)}
	}

	return targets, nil
}

func splitTrim(s string) []string {
	var out []string

	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}

	return out
}

// pairNodeInfo is node's hostname (its address when it cannot be read) and its clock's offset
// to its NTP server, like `talosctl time` (nil when it cannot be read).
func pairNodeInfo(ctx context.Context, configYAML, contextName, node string) (string, *int64) {
	s, release, err := acquireNode(configYAML, contextName, node)
	if err != nil {
		return node, nil
	}

	defer release()

	hostname := node

	nodeCtx, cancel := context.WithTimeout(client.WithNode(ctx, node), nodeTimeout)
	defer cancel()

	if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, s.client.COSI, network.HostnameID); err == nil && hs.TypedSpec().Hostname != "" {
		hostname = hs.TypedSpec().Hostname
	}

	t := probeTime(ctx, s.client, node)
	if t.Error != "" {
		return hostname, nil
	}

	return hostname, &t.OffsetMs
}

// pairClockGap is how far apart the two nodes' clocks are, each measured against its NTP
// server: packets of the two nodes are ordered no better than that. Nil when either is unknown.
func pairClockGap(targets []pairTarget) *int64 {
	if len(targets) != 2 || targets[0].offsetMs == nil || targets[1].offsetMs == nil {
		return nil
	}

	gap := *targets[0].offsetMs - *targets[1].offsetMs
	if gap < 0 {
		gap = -gap
	}

	return &gap
}

// clockGapPrefix starts the pcapng section comment that records the gap in a merged file.
const clockGapPrefix = "ichor clock-gap-ms="

// pairStatsReporter adds up both nodes' counters for the listener.
type pairStatsReporter struct {
	mu         sync.Mutex
	nodes      []pairNodeStats
	listener   PairCaptureListener
	clockGapMs *int64
}

func newPairStatsReporter(targets []pairTarget, l PairCaptureListener) *pairStatsReporter {
	r := &pairStatsReporter{listener: l, nodes: make([]pairNodeStats, len(targets))}
	for i, t := range targets {
		r.nodes[i] = pairNodeStats{Node: t.node, Hostname: t.hostname}
	}

	return r
}

func (r *pairStatsReporter) update(i int, packets, bytes int64) {
	r.mu.Lock()
	r.nodes[i].Packets, r.nodes[i].Bytes = packets, bytes
	out := pairStats{Nodes: append([]pairNodeStats(nil), r.nodes...), ClockGapMs: r.clockGapMs}
	r.mu.Unlock()

	for _, n := range out.Nodes {
		out.Packets += n.Packets
		out.Bytes += n.Bytes
	}

	emitJSON(out, r.listener.OnStats)
}

// pairNodeListener is one node's CaptureListener within a pair: summaries get the node's
// hostname, counters go to the pair's totals; the run reports the end itself.
type pairNodeListener struct {
	stats    *pairStatsReporter
	index    int
	hostname string
}

func (l pairNodeListener) OnPacket(summaryJSON string) {
	var s packetSummary
	if json.Unmarshal([]byte(summaryJSON), &s) != nil {
		return
	}

	s.Node = l.hostname
	emitJSON(s, l.stats.listener.OnPacket)
}

func (l pairNodeListener) OnStats(packets int64, bytes int64) {
	l.stats.update(l.index, packets, bytes)
}

func (pairNodeListener) OnDone(string, int64, int64, string) {}

// captureInput is a capture file to merge, and the interface name it gets.
type captureInput struct {
	path, name string
}

// mergeCaptureFiles writes inputs into one pcapng file at dest, packets in time order, each
// on the interface of its input (named name); clockGapMs, when known, is recorded in the
// section comment (see captureClockGap). The file is written to dest.part, then renamed.
func mergeCaptureFiles(dest string, inputs []captureInput, clockGapMs *int64) (captureResult, error) {
	type source struct {
		r     packetReader
		data  []byte
		ci    gopacket.CaptureInfo
		alive bool
	}

	sources := make([]*source, len(inputs))

	for i, in := range inputs {
		r, closer, err := openPcap(in.path)
		if err != nil {
			return captureResult{}, fmt.Errorf("%s: %w", in.name, err)
		}

		defer closer.Close() //nolint:errcheck

		sources[i] = &source{r: r}
	}

	part := dest + ".part"

	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return captureResult{}, err
	}

	defer os.Remove(part) //nolint:errcheck // gone once renamed

	w, err := newMergeWriter(f, inputs, sources[0].r, clockGapMs)
	if err != nil {
		_ = f.Close() //nolint:errcheck

		return captureResult{}, err
	}

	for i, s := range sources {
		if i > 0 {
			if _, err := w.AddInterface(mergeInterface(inputs[i].name, s.r)); err != nil {
				_ = f.Close() //nolint:errcheck

				return captureResult{}, err
			}
		}
	}

	next := func(s *source) {
		data, ci, err := s.r.ReadPacketData()
		s.data, s.ci, s.alive = data, ci, err == nil
	}

	for _, s := range sources {
		next(s)
	}

	var res captureResult

	for {
		pick := -1

		for i, s := range sources {
			if s.alive && (pick < 0 || s.ci.Timestamp.Before(sources[pick].ci.Timestamp)) {
				pick = i
			}
		}

		if pick < 0 {
			break
		}

		s := sources[pick]
		s.ci.InterfaceIndex = pick

		if err := w.WritePacket(s.ci, s.data); err != nil {
			_ = f.Close() //nolint:errcheck

			return captureResult{}, err
		}

		res.packets++

		next(s)
	}

	if err := w.Flush(); err != nil {
		_ = f.Close() //nolint:errcheck

		return captureResult{}, err
	}

	if err := f.Close(); err != nil {
		return captureResult{}, err
	}

	if info, err := os.Stat(part); err == nil {
		res.bytes = info.Size()
	}

	return res, os.Rename(part, dest)
}

func newMergeWriter(w io.Writer, inputs []captureInput, first packetReader, clockGapMs *int64) (*pcapgo.NgWriter, error) {
	options := pcapgo.NgWriterOptions{SectionInfo: pcapgo.NgSectionInfo{Application: "Ichor"}}
	if clockGapMs != nil {
		options.SectionInfo.Comment = fmt.Sprintf("%s%d", clockGapPrefix, *clockGapMs)
	}

	return pcapgo.NewNgWriterInterface(w, mergeInterface(inputs[0].name, first), options)
}

// captureClockGap reads the clock gap a merged capture recorded (mergeCaptureFiles); nil for
// any other file.
func captureClockGap(path string) *int64 {
	r, closer, err := openPcap(path)
	if err != nil {
		return nil
	}

	defer closer.Close() //nolint:errcheck

	ng, ok := r.(*pcapgo.NgReader)
	if !ok {
		return nil
	}

	value, found := strings.CutPrefix(ng.SectionInfo().Comment, clockGapPrefix)
	if !found {
		return nil
	}

	gap, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return nil
	}

	return &gap
}

func mergeInterface(name string, r packetReader) pcapgo.NgInterface {
	intf := pcapgo.DefaultNgInterface
	intf.Name = name
	intf.LinkType = r.LinkType()

	return intf
}

// maskedPairCaptureListener masks the live summaries, the counters' hostnames and the error;
// the file is not masked.
type maskedPairCaptureListener struct{ PairCaptureListener }

func (l maskedPairCaptureListener) OnPacket(summaryJSON string) {
	l.PairCaptureListener.OnPacket(privacy.mask(summaryJSON))
}

func (l maskedPairCaptureListener) OnStats(statsJSON string) {
	l.PairCaptureListener.OnStats(privacy.mask(statsJSON))
}

func (l maskedPairCaptureListener) OnDone(path string, packets int64, bytes int64, errMessage string) {
	l.PairCaptureListener.OnDone(path, packets, bytes, privacy.maskPlain(errMessage))
}
