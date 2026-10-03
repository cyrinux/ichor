package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// captureProbe collects a packet capture's callbacks.
type captureProbe struct {
	summaries []string
	stats     int
	done      chan string
}

func (p *captureProbe) OnPacket(summary string) {
	if len(p.summaries) < 5 {
		p.summaries = append(p.summaries, summary)
	}
}

func (p *captureProbe) OnStats(int64, int64) { p.stats++ }

func (p *captureProbe) OnDone(path string, packets, bytes int64, errMessage string) {
	p.done <- fmt.Sprintf("done path_set=%v packets=%d bytes=%d err=%q", path != "", packets, bytes, errMessage)
}

// pcapProbe captures on node's iface into a temporary file ($TMPDIR), reads it back with
// ReadPcap and PacketDetail, prints counts (and, with -mask only, a few masked summaries),
// then deletes the file.
func pcapProbe(cfg, contextName, node, iface, filter, seconds string, masked bool) string {
	secs, _ := strconv.Atoi(seconds) //nolint:errcheck

	if msg := ichorgo.ValidateCaptureFilter(filter); msg != "" {
		return "invalid filter: " + msg
	}

	dir, err := os.MkdirTemp("", "pcap-probe")
	if err != nil {
		return err.Error()
	}

	defer os.RemoveAll(dir) //nolint:errcheck

	dest := filepath.Join(dir, "probe.pcap")
	p := &captureProbe{done: make(chan string, 1)}

	ichorgo.StartPacketCapture(cfg, contextName, node, iface, filter, false, 0, secs, 0, dest, p)

	var b strings.Builder

	fmt.Fprintln(&b, <-p.done)
	fmt.Fprintf(&b, "live summaries=%d stats callbacks=%d\n", len(p.summaries), p.stats)

	if masked {
		for _, s := range p.summaries {
			fmt.Fprintln(&b, "  ", s)
		}
	}

	page, err := ichorgo.ReadPcap(dest, 0, 1000)
	if err != nil {
		return b.String() + "ReadPcap: " + err.Error()
	}

	var parsed struct {
		Packets []struct {
			Proto string `json:"proto"`
		} `json:"packets"`
		Total int `json:"total"`
	}

	_ = json.Unmarshal([]byte(page), &parsed) //nolint:errcheck

	protos := map[string]int{}
	for _, pkt := range parsed.Packets {
		protos[pkt.Proto]++
	}

	fmt.Fprintf(&b, "ReadPcap total=%d protocols=%v\n", parsed.Total, protos)

	if parsed.Total > 0 {
		detail, err := ichorgo.PacketDetail(dest, 0)
		if err != nil {
			return b.String() + "PacketDetail: " + err.Error()
		}

		var d struct {
			Layers []struct {
				Name string `json:"name"`
			} `json:"layers"`
			Hex string `json:"hex"`
		}

		_ = json.Unmarshal([]byte(detail), &d) //nolint:errcheck

		names := make([]string, 0, len(d.Layers))
		for _, l := range d.Layers {
			names = append(names, l.Name)
		}

		fmt.Fprintf(&b, "PacketDetail(0) layers=%v hex lines=%d\n", names, strings.Count(d.Hex, "\n"))
	}

	return b.String()
}
