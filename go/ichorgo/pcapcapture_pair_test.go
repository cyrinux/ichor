package ichorgo

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

const pairA, pairB = "192.0.2.91", "192.0.2.92"

// streamAt is a pcap stream like Talos sends, packet i at second offsets[i].
func streamAt(t *testing.T, offsets []int, pkt []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	w := pcapgo.NewWriter(&buf)
	if err := w.WriteFileHeader(4096, layers.LinkTypeEthernet); err != nil {
		t.Fatal(err)
	}

	for _, s := range offsets {
		ci := gopacket.CaptureInfo{Timestamp: time.Unix(1700000000+int64(s), 0), CaptureLength: len(pkt), Length: len(pkt)}
		if err := w.WritePacket(ci, pkt); err != nil {
			t.Fatal(err)
		}
	}

	return buf.Bytes()
}

// pairCluster is two nodes with an eth0 link each, sending streams a and b.
func pairCluster(t *testing.T, a, b []byte) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.pcaps, f.pcapHold = map[string][]byte{pairA: a, pairB: b}, map[string]bool{}
	// Node A's clock is 5 ms behind its NTP server, node B's 7 ms ahead: 12 ms apart.
	f.clockOffsets = map[string]int64{pairA: 5, pairB: -7}

	for _, n := range []string{pairA, pairB} {
		f.addNode(t, n, "v1.11.0", machine.TypeWorker)

		link := network.NewLinkStatus(network.NamespaceName, "eth0")
		link.TypedSpec().Type = nethelpers.LinkEther
		f.put(n, link)
	}

	return f, f.start(t, pairA, pairB)
}

type pairEvents struct {
	mu        sync.Mutex
	summaries []packetSummary
	stats     []pairStats
	path      string
	packets   int64
	errText   string
	done      chan struct{}
}

func newPairEvents() *pairEvents { return &pairEvents{done: make(chan struct{})} }

func (e *pairEvents) OnPacket(s string) {
	var p packetSummary
	_ = json.Unmarshal([]byte(s), &p) //nolint:errcheck

	e.mu.Lock()
	e.summaries = append(e.summaries, p)
	e.mu.Unlock()
}

func (e *pairEvents) OnStats(s string) {
	var p pairStats
	_ = json.Unmarshal([]byte(s), &p) //nolint:errcheck

	e.mu.Lock()
	e.stats = append(e.stats, p)
	e.mu.Unlock()
}

func (e *pairEvents) OnDone(path string, packets, _ int64, errMessage string) {
	e.path, e.packets, e.errText = path, packets, errMessage
	close(e.done)
}

func (e *pairEvents) wait(t *testing.T) {
	t.Helper()

	select {
	case <-e.done:
	case <-time.After(20 * time.Second):
		t.Fatal("the pair capture did not end")
	}
}

func TestCapturePairMergesBothNodesInTimeOrder(t *testing.T) {
	pkt := udpPacket(t, 40000, 51820, []byte("wg"))
	_, cfg := pairCluster(t, streamAt(t, []int{1, 3}, pkt), streamAt(t, []int{2, 4}, pkt))

	dest := filepath.Join(t.TempDir(), "pair.pcapng")
	ev := newPairEvents()

	StartPacketCapturePair(cfg, "fake", pairA+","+pairB, "eth0", "", false, 0, 30, 0, dest, ev)
	ev.wait(t)

	if ev.errText != "" || ev.path != dest || ev.packets != 4 {
		t.Fatalf("done: path=%q packets=%d err=%q", ev.path, ev.packets, ev.errText)
	}

	out, err := ReadPcap(dest, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	var page pcapPage
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatal(err)
	}

	var nodes []string
	for _, p := range page.Packets {
		nodes = append(nodes, p.Node)
	}

	hostA, hostB := "host-192-0-2-91", "host-192-0-2-92"
	if want := []string{hostA, hostB, hostA, hostB}; strings.Join(nodes, ",") != strings.Join(want, ",") {
		t.Errorf("nodes = %v, want %v", nodes, want)
	}

	// The live summaries name their node; the counters add both up.
	ev.mu.Lock()
	defer ev.mu.Unlock()

	if len(ev.summaries) != 4 || ev.summaries[0].Node == "" {
		t.Errorf("summaries = %+v", ev.summaries)
	}

	// Both clocks were read: the gap is in the live counters and in the file.
	if page.ClockGapMs == nil || *page.ClockGapMs != 12 || ev.stats[0].ClockGapMs == nil || *ev.stats[0].ClockGapMs != 12 {
		t.Errorf("clock gap: file %v, stats %v", page.ClockGapMs, ev.stats[0].ClockGapMs)
	}

	last := ev.stats[len(ev.stats)-1]
	if len(last.Nodes) != 2 || last.Packets != last.Nodes[0].Packets+last.Nodes[1].Packets {
		t.Errorf("stats = %+v", last)
	}
}

func TestCapturePairStopsBothAtOneLimit(t *testing.T) {
	pkt := udpPacket(t, 40000, 51820, bytes.Repeat([]byte("x"), 200))
	f, cfg := pairCluster(t, streamAt(t, []int{1, 2, 3, 4, 5}, pkt), streamAt(t, []int{1}, pkt))
	f.pcapHold[pairA], f.pcapHold[pairB] = true, true

	dest := filepath.Join(t.TempDir(), "pair.pcapng")
	ev := newPairEvents()
	started := time.Now()

	// Room for two packets per node: node A reaches it, node B's stream stays open.
	limit := int64(pcapFileHeaderLen + 2*(pcapPacketHeaderLen+len(pkt)) + 10)
	StartPacketCapturePair(cfg, "fake", pairA+","+pairB, "eth0", "", false, 0, 60, limit, dest, ev)
	ev.wait(t)

	if ev.errText != "" || ev.packets < 2 || ev.packets > 3 || time.Since(started) > 15*time.Second {
		t.Fatalf("done after %s: packets=%d err=%q", time.Since(started), ev.packets, ev.errText)
	}
}

func TestCapturePairKeepsOneEndWhenTheOtherFails(t *testing.T) {
	pkt := udpPacket(t, 40000, 51820, []byte("wg"))
	f, cfg := pairCluster(t, streamAt(t, []int{1, 2}, pkt), streamAt(t, []int{1}, pkt))
	f.pcapHold[pairA] = true

	dest := filepath.Join(t.TempDir(), "pair.pcapng")
	ev := newPairEvents()

	// Node B has no wg0: it fails, which stops node A too; what A captured is kept.
	StartPacketCapturePair(cfg, "fake", pairA+","+pairB, "eth0,wg0", "", false, 0, 30, 0, dest, ev)
	ev.wait(t)

	if (ev.path != dest && ev.packets > 0) || ev.packets > 2 || !strings.Contains(ev.errText, `"wg0" not found`) {
		t.Fatalf("done: path=%q packets=%d err=%q", ev.path, ev.packets, ev.errText)
	}
}

func TestCapturePairRefusesBadTargets(t *testing.T) {
	for _, tc := range []struct{ nodes, ifaces, want string }{
		{pairA, "eth0", "two different nodes"},
		{pairA + "," + pairA, "eth0", "two different nodes"},
		{pairA + "," + pairB, "", "pick an interface"},
		{pairA + "," + pairB, "a,b,c", "pick an interface"},
	} {
		if _, err := pairTargets(tc.nodes, tc.ifaces, "/tmp/x.pcapng"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q %q: %v", tc.nodes, tc.ifaces, err)
		}
	}
}

func TestCapturePairDemoIsRefused(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	nodes := demoNodes()
	ev := newPairEvents()

	StartPacketCapturePair(cfg, "", nodes[0].Node+","+nodes[1].Node, "eth0", "", false, 0, 5, 0, filepath.Join(t.TempDir(), "x.pcapng"), ev)
	ev.wait(t)

	if ev.path != "" || ev.errText == "" {
		t.Errorf("demo: path=%q err=%q", ev.path, ev.errText)
	}
}

func TestCapturePairWithoutClocksRecordsNoGap(t *testing.T) {
	pkt := udpPacket(t, 40000, 51820, []byte("wg"))
	f, cfg := pairCluster(t, streamAt(t, []int{1}, pkt), streamAt(t, []int{2}, pkt))
	delete(f.clockOffsets, pairB) // a node whose time cannot be read

	dest := filepath.Join(t.TempDir(), "pair.pcapng")
	ev := newPairEvents()

	StartPacketCapturePair(cfg, "fake", pairA+","+pairB, "eth0", "", false, 0, 30, 0, dest, ev)
	ev.wait(t)

	out, err := ReadPcap(dest, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	var page pcapPage
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatal(err)
	}

	if page.ClockGapMs != nil || page.Total != 2 {
		t.Errorf("page = %+v", page)
	}
}
