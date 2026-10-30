package ichorgo

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type demoCaptureListener struct {
	mu      sync.Mutex
	packets []string
	done    chan [2]string
}

func (l *demoCaptureListener) OnPacket(summary string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.packets = append(l.packets, summary)
}
func (l *demoCaptureListener) OnStats(int64, int64) {}
func (l *demoCaptureListener) OnDone(path string, _, _ int64, errMessage string) {
	l.done <- [2]string{path, errMessage}
}

func TestDemoCaptureWritesSamplePackets(t *testing.T) {
	yaml := demoConfigForTest(t)
	dest := filepath.Join(t.TempDir(), "demo.pcap")
	listener := &demoCaptureListener{done: make(chan [2]string, 1)}

	StartPacketCapture(yaml, "", demoNodes()[0].Node, "eth0", "", false, 0, 1, 0, dest, listener)

	result := <-listener.done
	if result[1] != "" || result[0] != dest {
		t.Fatalf("done: %q", result)
	}
	if info, err := os.Stat(dest); err != nil || info.Size() <= pcapFileHeaderLen {
		t.Fatalf("no packets in the file: %v %v", info, err)
	}
	listener.mu.Lock()
	defer listener.mu.Unlock()
	joined := strings.Join(listener.packets, "\n")
	if !strings.Contains(joined, demoNodes()[0].Node) || !strings.Contains(joined, "192.0.2.11") {
		t.Fatalf("summaries: %s", joined)
	}
}

func TestDemoFeaturesOfferCaptureButNotUpgrade(t *testing.T) {
	yaml := demoConfigForTest(t)
	out, err := NodeFeatures(yaml, "", demoNodes()[0].Node)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"packetCapture":{"supported":true`) {
		t.Fatalf("capture should be offered: %s", out)
	}
	if strings.Contains(out, `"upgrade":{"supported":true`) {
		t.Fatalf("a node upgrade stays unavailable: %s", out)
	}
}
