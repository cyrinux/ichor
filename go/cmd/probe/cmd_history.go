package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

// historyMagic starts an encoded ring (see ichorgo/history_ring.go).
const historyMagic = "ICHH"

var historyCommands = []command{
	{name: "incident-summary", args: "RING_FILE [SINCE] [names]", noConfig: true, run: probeIncidentSummary},
}

// probeIncidentSummary prints the incident summary of -context over the last SINCE (24h by
// default; "7d" works too), anonymised unless "names" is given. The probe keeps no history
// ring: RING_FILE is an unsealed ring, or the monitor records as JSON lines (one run each,
// as HistoryAppend takes them), built into a ring here. The actions come from -data-dir.
func probeIncidentSummary(e env) (string, error) {
	if e.context == "" {
		return "", errors.New("incident-summary needs -context (the cluster the actions were recorded for)")
	}

	if flag.Arg(1) == "" {
		return "", errors.New("usage: incident-summary RING_FILE [SINCE] [names]")
	}

	window := 24 * time.Hour

	if arg := flag.Arg(2); arg != "" {
		d, err := parseSince(arg)
		if err != nil {
			return "", err
		}

		window = d
	}

	now := time.Now().UnixMilli()

	ring, err := readProbeRing(flag.Arg(1), now)
	if err != nil {
		return "", err
	}

	return ichorgo.IncidentSummary(e.context, ring, now-window.Milliseconds(), now, flag.Arg(3) != "names")
}

// parseSince reads a window: a Go duration ("6h", "90m") or a number of days ("7d").
func parseSince(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("window %q: want a positive number of days", s)
		}

		return time.Duration(n) * 24 * time.Hour, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("window %q: want a duration like 6h or 7d", s)
	}

	return d, nil
}

// readProbeRing reads a ring file as it is, or builds one from JSON-lines records.
func readProbeRing(path string, now int64) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if bytes.HasPrefix(raw, []byte(historyMagic)) {
		return raw, nil
	}

	var ring []byte

	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)

	for line := 1; sc.Scan(); line++ {
		rec := strings.TrimSpace(sc.Text())
		if rec == "" {
			continue
		}

		if ring, err = ichorgo.HistoryAppend(ring, rec, now); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
	}

	return ring, sc.Err()
}
