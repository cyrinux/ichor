package ichorgo

import (
	"bufio"
	"bytes"
	"cmp"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// Ring layout: the magic "ICHH", a version byte, then a gzip stream of JSON lines: a meta
// object first, then one record per line, oldest first.
const (
	historyMagic   = "ICHH"
	historyVersion = 1
	// historyMaxRaw bounds what a ring may inflate to (a ring under the cap is far smaller).
	historyMaxRaw = 32 << 20
	// historyMaxLine bounds one JSON line.
	historyMaxLine = 4 << 20
)

var (
	// errHistoryNewer: the ring was written by a newer version of the app.
	errHistoryNewer = errors.New("history was written by a newer version of the app")
	// errHistoryCorrupt: the ring cannot be read.
	errHistoryCorrupt = errors.New("history cannot be read")
)

type historyMeta struct {
	// ResetAt is when an unreadable ring was started over, ResetReason why.
	ResetAt     int64  `json:"resetAt,omitempty"`
	ResetReason string `json:"resetReason,omitempty"`
}

type historyRing struct {
	meta    historyMeta
	records []historyRecord
}

func historyCorrupt(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errHistoryCorrupt, fmt.Sprintf(format, args...))
}

func decodeHistoryRing(b []byte) (historyRing, error) {
	var ring historyRing

	if len(b) == 0 {
		return ring, nil
	}

	if len(b) <= len(historyMagic) || string(b[:len(historyMagic)]) != historyMagic {
		return ring, historyCorrupt("not a history ring")
	}

	switch v := b[len(historyMagic)]; {
	case v > historyVersion:
		return ring, fmt.Errorf("%w (format %d, this one reads %d)", errHistoryNewer, v, historyVersion)
	case v < historyVersion:
		return ring, historyCorrupt("unknown format %d", v)
	}

	zr, err := gzip.NewReader(bytes.NewReader(b[len(historyMagic)+1:]))
	if err != nil {
		return ring, historyCorrupt("%v", err)
	}

	sc := bufio.NewScanner(io.LimitReader(zr, historyMaxRaw))
	sc.Buffer(make([]byte, 0, 64<<10), historyMaxLine)

	first := true

	for sc.Scan() {
		if first {
			first = false

			if err := json.Unmarshal(sc.Bytes(), &ring.meta); err != nil {
				return historyRing{}, historyCorrupt("meta: %v", err)
			}

			continue
		}

		var rec historyRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return historyRing{}, historyCorrupt("record %d: %v", len(ring.records)+1, err)
		}

		ring.records = append(ring.records, rec)
	}

	if err := sc.Err(); err != nil {
		return historyRing{}, historyCorrupt("%v", err)
	}

	if first {
		return historyRing{}, historyCorrupt("empty")
	}

	// The scanner stops quietly at the limit: whatever is left means the ring is too large.
	if n, _ := zr.Read(make([]byte, 1)); n > 0 {
		return historyRing{}, historyCorrupt("larger than %d bytes", historyMaxRaw)
	}

	slices.SortStableFunc(ring.records, func(a, b historyRecord) int { return cmp.Compare(a.At, b.At) })

	return ring, nil
}

func encodeHistoryRing(ring historyRing) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString(historyMagic)
	buf.WriteByte(historyVersion)

	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}

	enc := json.NewEncoder(zw)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(ring.meta); err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}

	for _, rec := range ring.records {
		if err := enc.Encode(rec); err != nil {
			return nil, fmt.Errorf("history: %w", err)
		}
	}

	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}

	return buf.Bytes(), nil
}

// encodeHistoryCapped encodes ring, dropping its oldest records (a tenth at a time) until
// it fits in limit bytes. The newest record always stays.
func encodeHistoryCapped(ring historyRing, limit int) ([]byte, error) {
	for {
		b, err := encodeHistoryRing(ring)
		if err != nil || len(b) <= limit || len(ring.records) <= 1 {
			return b, err
		}

		drop := max(1, len(ring.records)/10)
		ring.records = ring.records[min(drop, len(ring.records)-1):]
	}
}

// insertHistoryRecord adds rec in time order; a record at the same time is replaced.
func insertHistoryRecord(records []historyRecord, rec historyRecord) []historyRecord {
	i, found := slices.BinarySearchFunc(records, rec.At, func(r historyRecord, at int64) int { return cmp.Compare(r.At, at) })
	if found {
		out := slices.Clone(records)
		out[i] = rec

		return out
	}

	return slices.Insert(slices.Clone(records), i, rec)
}

// pruneHistory drops the records older than historyKeep.
func pruneHistory(records []historyRecord, nowMillis int64) []historyRecord {
	cutoff := nowMillis - historyKeep.Milliseconds()

	i, _ := slices.BinarySearchFunc(records, cutoff, func(r historyRecord, at int64) int { return cmp.Compare(r.At, at) })

	return records[i:]
}

// thinHistory keeps, among the records older than historyFullDetail, the first of each
// hour and every record whose state differs from the last one kept (a node's health or
// version, reachability, the open alerts), so intervals and alerts stay exact.
func thinHistory(records []historyRecord, nowMillis int64) []historyRecord {
	cutoff := nowMillis - historyFullDetail.Milliseconds()
	hour := time.Hour.Milliseconds()
	out := make([]historyRecord, 0, len(records))

	var (
		lastHour int64 = -1
		lastSig  string
	)

	for _, rec := range records {
		sig := historySignature(rec)

		if rec.At < cutoff && rec.At/hour == lastHour && sig == lastSig {
			continue
		}

		out = append(out, rec)
		lastHour, lastSig = rec.At/hour, sig
	}

	return out
}

// historySignature is what a record says about state (not measurements).
func historySignature(rec historyRecord) string {
	var b strings.Builder

	if !rec.reachable() {
		b.WriteString("!")
	}

	for _, n := range rec.Nodes {
		b.WriteString(n.Node + "=" + n.Health + "/" + n.Version + ";")
	}

	b.WriteString("|")

	for _, a := range rec.Alerts {
		b.WriteString(a.Key + ";")
	}

	return b.String()
}
