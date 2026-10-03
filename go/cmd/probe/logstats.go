package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/ichorgo"
)

type logEntry struct {
	TS     int64             `json:"ts"`
	Level  string            `json:"level"`
	Source string            `json:"source"`
	Fields []json.RawMessage `json:"fields"`
}

// logStats fetches up to 5000 lines per service ("kernel" for dmesg) and prints only counts
// of the parsed entries, never the lines. It also re-parses each (masked) line through
// ParseLogLine, as the follow screens do, and counts disagreements with the tail's entries.
func logStats(cfg, contextName, node string, services []string) string {
	var b strings.Builder

	for _, svc := range services {
		var (
			out string
			err error
		)

		if svc == "kernel" {
			out, err = ichorgo.KernelLogs(cfg, contextName, node, 5000)
		} else {
			out, err = ichorgo.ServiceLogs(cfg, contextName, node, svc, 5000)
		}

		if err != nil {
			fmt.Fprintf(&b, "%-10s error: %v\n", svc, err)

			continue
		}

		var tail struct {
			Lines   []string   `json:"lines"`
			Entries []logEntry `json:"entries"`
		}
		if err := json.Unmarshal([]byte(out), &tail); err != nil {
			fmt.Fprintf(&b, "%-10s decode: %v\n", svc, err)

			continue
		}

		levels := map[string]int{}
		noTS, withSource, withFields, mismatch := 0, 0, 0, 0
		start := time.Now()

		for i, e := range tail.Entries {
			levels[e.Level]++
			if e.TS == 0 {
				noTS++
			}
			if e.Source != "" {
				withSource++
			}
			if len(e.Fields) > 0 {
				withFields++
			}

			var again logEntry
			if json.Unmarshal([]byte(ichorgo.ParseLogLine(tail.Lines[i])), &again) != nil ||
				again.Level != e.Level || again.TS != e.TS || len(again.Fields) != len(e.Fields) {
				mismatch++
			}
		}

		perLine := time.Duration(0)
		if n := len(tail.Entries); n > 0 {
			perLine = time.Since(start) / time.Duration(n)
		}

		keys := make([]string, 0, len(levels))
		for k := range levels {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		counts := make([]string, 0, len(keys))
		for _, k := range keys {
			name := k
			if name == "" {
				name = "none"
			}
			counts = append(counts, fmt.Sprintf("%s=%d", name, levels[k]))
		}

		fmt.Fprintf(&b, "%-10s lines=%d entries=%d %s noTS=%d source=%d fields=%d reparseMismatch=%d (%v/line incl. JSON round trip)\n",
			svc, len(tail.Lines), len(tail.Entries), strings.Join(counts, " "), noTS, withSource, withFields, mismatch, perLine)
	}

	return b.String()
}
