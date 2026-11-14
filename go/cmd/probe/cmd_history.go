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
	{name: "history-memory-forecast", args: "RING_FILE [CRITICAL]", noConfig: true, run: probeMemoryForecast},
	{name: "history-flaps", args: "RING_FILE [HOURS]", noConfig: true, run: probeHistoryFlaps},
}

// probeHistoryFlaps prints the alerts of RING_FILE (a ring or JSON lines, as incident-summary
// reads it) that opened more than once in the last HOURS (24 by default): what the alert
// rules' flapping check counts.
func probeHistoryFlaps(env) (string, error) {
	if flag.Arg(1) == "" {
		return "", errors.New("usage: history-flaps RING_FILE [HOURS]")
	}

	hours := 0

	if arg := flag.Arg(2); arg != "" {
		h, err := strconv.Atoi(arg)
		if err != nil {
			return "", fmt.Errorf("HOURS %q: %w", arg, err)
		}

		hours = h
	}

	now := time.Now().UnixMilli()

	ring, err := readProbeRing(flag.Arg(1), now)
	if err != nil {
		return "", err
	}

	return ichorgo.HistoryAlertFlaps(ring, now, hours)
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

// probeMemoryForecast prints where each node's memory use in RING_FILE (a ring or JSON lines,
// as incident-summary reads it) is heading, projected to CRITICAL percent (90 by default).
func probeMemoryForecast(env) (string, error) {
	if flag.Arg(1) == "" {
		return "", errors.New("usage: history-memory-forecast RING_FILE [CRITICAL]")
	}

	crit := 0.0

	if arg := flag.Arg(2); arg != "" {
		c, err := strconv.ParseFloat(arg, 64)
		if err != nil {
			return "", fmt.Errorf("CRITICAL %q: %w", arg, err)
		}

		crit = c
	}

	now := time.Now().UnixMilli()

	ring, err := readProbeRing(flag.Arg(1), now)
	if err != nil {
		return "", err
	}

	return ichorgo.HistoryMemoryForecast(ring, now, crit)
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
