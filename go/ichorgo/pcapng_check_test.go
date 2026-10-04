package ichorgo

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcapgo"
)

func writeNg(t *testing.T, packets int) []byte {
	t.Helper()

	var buf bytes.Buffer

	w, err := pcapgo.NewNgWriter(&buf, layers.LinkTypeRaw)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte{0x45, 0, 0, 20, 0, 0, 0, 0, 64, 17, 0, 0, 10, 0, 0, 1, 10, 0, 0, 2}
	for range packets {
		ci := gopacket.CaptureInfo{Timestamp: time.Unix(1_700_000_000, 0), CaptureLength: len(data), Length: len(data)}
		if err := w.WritePacket(ci, data); err != nil {
			t.Fatal(err)
		}
	}

	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func writeFile(t *testing.T, content []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "capture.pcapng")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestReadPcapngValid(t *testing.T) {
	file := writeNg(t, 3)

	if err := checkPcapng(bytes.NewReader(file)); err != nil {
		t.Fatalf("valid file refused: %v", err)
	}

	out, err := ReadPcap(writeFile(t, file), 0, 10)
	if err != nil || !strings.Contains(out, `"10.0.0.2"`) && !strings.Contains(out, "10.0.0.2") {
		t.Fatalf("out=%s err=%v", out, err)
	}

	// A capture still being written ends on a partial block: read what is there.
	if err := checkPcapng(bytes.NewReader(file[:len(file)-10])); err != nil {
		t.Errorf("truncated file refused: %v", err)
	}
}

// crafted is a section header, an interface with snaplen 0 and an enhanced packet block
// declaring a 3 GiB packet in 32 bytes: gopacket would allocate it.
func crafted() []byte {
	le := binary.LittleEndian

	var b []byte

	b = le.AppendUint32(b, ngSectionHeader)
	b = le.AppendUint32(b, 28)
	b = le.AppendUint32(b, ngByteOrderMagic)
	b = le.AppendUint16(b, 1)
	b = le.AppendUint16(b, 0)
	b = le.AppendUint64(b, ^uint64(0))
	b = le.AppendUint32(b, 28)

	b = le.AppendUint32(b, ngInterface)
	b = le.AppendUint32(b, 20)
	b = le.AppendUint16(b, uint16(layers.LinkTypeRaw))
	b = le.AppendUint16(b, 0)
	b = le.AppendUint32(b, 0)
	b = le.AppendUint32(b, 20)

	b = le.AppendUint32(b, ngEnhancedPacket)
	b = le.AppendUint32(b, 32)
	b = le.AppendUint32(b, 0)
	b = le.AppendUint32(b, 0)
	b = le.AppendUint32(b, 0)
	b = le.AppendUint32(b, 0xc0000000)
	b = le.AppendUint32(b, 0xc0000000)
	b = le.AppendUint32(b, 32)

	return b
}

func TestReadPcapngRefusesOversizedPacket(t *testing.T) {
	if err := checkPcapng(bytes.NewReader(crafted())); err == nil || !strings.Contains(err.Error(), "larger than its block") {
		t.Fatalf("check: %v", err)
	}

	if _, err := ReadPcap(writeFile(t, crafted()), 0, 10); err == nil {
		t.Fatal("a packet larger than its block was accepted")
	}
}

func TestParseNetPerfRefusesNonFinite(t *testing.T) {
	for _, v := range []string{"inf", "nan", "-Inf"} {
		log := "1,2,3,4,5,6," + v + ",8,Trans/s\n"
		if res, err := parseNetPerf("TCP_RR", log); err == nil {
			if _, jerr := toJSON(res); jerr != nil {
				t.Errorf("%s: result does not encode: %v", v, jerr)
			}
		}
	}
}

func TestCompileFilterRefusesHugeExpression(t *testing.T) {
	if _, err := compileFilter(strings.Repeat("(", 100_000) + "tcp" + strings.Repeat(")", 100_000)); err == nil {
		t.Error("huge filter accepted")
	}

	if msg := ValidateCaptureFilter("tcp port 443 and not host 10.0.0.1"); msg != "" {
		t.Errorf("valid filter refused: %s", msg)
	}
}

func TestSplitTrailingJSONLongLineIsBounded(t *testing.T) {
	line := "x" + strings.Repeat(` {"a":[`, 50_000) + "}"

	start := time.Now()
	if msg, fields := splitTrailingJSON(line); msg != line || fields != nil {
		t.Error("long line was split")
	}

	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v", d)
	}

	if msg, fields := splitTrailingJSON(`started {"port":8080}`); msg != "started" || len(fields) != 1 {
		t.Errorf("msg=%q fields=%v", msg, fields)
	}
}
