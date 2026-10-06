package ichorgo

import (
	"archive/tar"
	"bufio"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path"
	"slices"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

// auditLogPath is where Talos has every kube-apiserver write its audit log (JSON lines,
// rotated at 100 MB): each control-plane node logs the requests its own API server served.
const auditLogPath = "/var/log/audit/kube/kube-apiserver.log"

// auditTimeout bounds the whole read: each node streams up to 100 MB, gzipped on the wire.
const auditTimeout = 2 * time.Minute

// The window read, in minutes.
const (
	auditDefaultMinutes = 15
	auditMaxMinutes     = 120
	auditTopActors      = 15
	auditMaxLine        = 1 << 20 // longer events (request bodies logged) are skipped
)

// auditReport is who loads the API server and what they do wrong, from the audit logs of
// the control-plane nodes over the last minutes (from..to, unix milliseconds).
type auditReport struct {
	Nodes    []auditNodeRead `json:"nodes"`
	From     int64           `json:"from"`
	To       int64           `json:"to"`
	Seconds  float64         `json:"seconds"`
	Requests int             `json:"requests"`
	Findings []auditFinding  `json:"findings"`
	Actors   []auditActorRow `json:"actors"`
}

// auditNodeRead is how reading one control plane's log went: its compressed size on the
// wire, the events parsed, or why it failed.
type auditNodeRead struct {
	Node   string `json:"node"`
	Bytes  int64  `json:"bytes"`
	Events int    `json:"events"`
	Last   int64  `json:"last,omitempty"` // its newest event, unix ms: an API server that stopped logging lags behind
	Error  string `json:"error,omitempty"`
}

// auditActorRow is one of the busiest actors.
type auditActorRow struct {
	Actor       auditActor `json:"actor"`
	Requests    int        `json:"requests"`
	Rate        float64    `json:"rate"`
	Share       float64    `json:"share"`  // of all requests, 0..1
	Errors      int        `json:"errors"` // 4xx but 404, and 5xx
	Throttled   int        `json:"throttled"`
	LatencyMs   float64    `json:"latencyMs"`
	TopVerb     string     `json:"topVerb"`
	TopResource string     `json:"topResource"`
	TopCount    int        `json:"topCount"`
}

// KubeAuditAnalysis reads the kube-apiserver audit log of every control-plane node through
// the Talos API (os:admin; Talos audits every request by default) and finds who loads the
// API server and what goes wrong over the last minutes (1 to 120, 0 for 15): clients
// listing in loops, watches restarting, refused or failing requests, objects rewritten
// again and again, slow requests, throttled clients. JSON auditReport.
func KubeAuditAnalysis(configYAML, contextName string, minutes int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if minutes <= 0 {
		minutes = auditDefaultMinutes
	}

	minutes = min(minutes, auditMaxMinutes)

	if isDemoContext(configYAML, contextName) {
		return toJSON(demoAudit(minutes))
	}

	return withSession(configYAML, contextName, auditTimeout, func(ctx context.Context, s *session) (string, error) {
		cps, err := s.controlPlanes(ctx)
		if err != nil {
			return "", err
		}

		windows := make([]*auditWindow, len(cps))
		reads := make([]auditNodeRead, len(cps))

		forEachNode(cps, func(i int, node string) {
			windows[i], reads[i] = readAuditLog(ctx, s, node, minutes)
		})

		report, err := buildAuditReport(windows, reads)
		if err != nil {
			return "", err
		}

		return toJSON(report)
	})
}

// auditMaxBackups is how many rotated logs a node may add when its current one does not
// cover the window: a busy API server fills 100 MB in a quarter of an hour.
const auditMaxBackups = 2

// readAuditLog streams one node's audit log into a window of the last minutes: the current
// file, then the newest rotated ones while the window is not covered.
func readAuditLog(ctx context.Context, s *session, node string, minutes int) (*auditWindow, auditNodeRead) {
	read := auditNodeRead{Node: node}
	window := newAuditWindow(minutes)
	nodeCtx := client.WithNode(ctx, node)
	files := append([]string{auditLogPath}, auditBackups(nodeCtx, s)...)

	for i, file := range files[:min(len(files), 1+auditMaxBackups)] {
		if i > 0 && window.covered() {
			break
		}

		events, bytes, err := readAuditFile(nodeCtx, s, file, window)
		read.Events += events
		read.Bytes += bytes

		if err != nil {
			// The current log failing fails the node; a backup failing keeps what was read.
			if i == 0 {
				read.Error = s.friendly(node, err)
			}

			break
		}
	}

	return window, read
}

func readAuditFile(ctx context.Context, s *session, file string, window *auditWindow) (int, int64, error) {
	r, err := s.client.Copy(ctx, file)
	if err != nil {
		return 0, 0, err
	}

	defer r.Close() //nolint:errcheck

	counted := &countingReader{r: r}
	events, err := scanAuditTarGz(counted, window)

	return events, counted.n, err
}

// auditBackups lists the rotated audit logs next to the current one, newest first; none
// when the directory cannot be listed.
func auditBackups(ctx context.Context, s *session) []string {
	stream, err := s.client.LS(ctx, &machineapi.ListRequest{Root: path.Dir(auditLogPath), Types: []machineapi.ListRequest_Type{machineapi.ListRequest_REGULAR}})
	if err != nil {
		return nil
	}

	type logFile struct {
		path     string
		modified int64
	}

	var files []logFile

	for {
		info, err := stream.Recv()
		if err != nil || metaError(info.GetMetadata()) != "" {
			break
		}

		name := path.Join(path.Dir(auditLogPath), path.Base(info.GetName()))
		if name != auditLogPath && strings.HasSuffix(name, ".log") {
			files = append(files, logFile{name, info.GetModified()})
		}
	}

	slices.SortFunc(files, func(a, b logFile) int { return cmp.Compare(b.modified, a.modified) })

	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.path)
	}

	return out
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)

	return n, err
}

// scanAuditTarGz reads the log file out of a Copy (a tar.gz) line by line into window.
func scanAuditTarGz(r io.Reader, window *auditWindow) (int, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return 0, err
	}

	defer gz.Close() //nolint:errcheck

	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return 0, errors.New("the audit log is not there: is auditing turned off?")
		}

		if err != nil {
			return 0, err
		}

		if hdr.Typeflag == tar.TypeReg && strings.HasSuffix(path.Base(hdr.Name), ".log") {
			return scanAuditLines(tr, window)
		}
	}
}

// scanAuditLines feeds every event of a JSON-lines audit log to window. Lines that do not
// parse are skipped, and so are lines longer than auditMaxLine (a policy logging request
// bodies): one huge event must not lose the rest of the file.
func scanAuditLines(r io.Reader, window *auditWindow) (int, error) {
	br := bufio.NewReaderSize(r, auditMaxLine)
	events := 0

	for {
		line, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = br.ReadSlice('\n')
			}

			if err == nil {
				continue
			}

			line = nil
		}

		var e auditEvent
		if len(line) > 0 && json.Unmarshal(line, &e) == nil {
			events++

			window.add(e)
		}

		switch {
		case errors.Is(err, io.EOF):
			return events, nil
		case err != nil:
			return events, err
		}
	}
}

// buildAuditReport merges the nodes' windows and runs the detectors. It fails only when no
// node could be read.
func buildAuditReport(windows []*auditWindow, reads []auditNodeRead) (auditReport, error) {
	report := auditReport{Nodes: reads, Findings: []auditFinding{}, Actors: []auditActorRow{}}
	actors := map[string]*auditStats{}

	var from, to time.Time

	failed := 0
	latest := int64(0)

	for i, w := range windows {
		latest = max(latest, w.latest)

		if !w.last.IsZero() {
			reads[i].Last = w.last.UnixMilli()
		}
	}

	for i, w := range windows {
		if reads[i].Error != "" {
			// Its log failed part way: the minutes read are the oldest, and would count short.
			failed++

			continue
		}

		w.alignTo(latest)

		stats, f, t := w.actors()
		for key, s := range stats {
			if actors[key] == nil {
				actors[key] = newAuditStats()
			}

			actors[key].merge(s)
		}

		if !f.IsZero() && (from.IsZero() || f.Before(from)) {
			from = f
		}

		if t.After(to) {
			to = t
		}
	}

	if failed == len(reads) && len(reads) > 0 {
		return auditReport{}, errors.New(reads[0].Error)
	}

	if !from.IsZero() {
		report.From, report.To, report.Seconds = from.UnixMilli(), to.UnixMilli(), to.Sub(from).Seconds()
	}

	for _, s := range actors {
		report.Requests += s.Requests
	}

	for key, s := range actors {
		parts := strings.SplitN(key, "\x00", 3)
		actor := describeActor(parts[0], parts[1], parts[2])
		report.Findings = append(report.Findings, detect(actor, s, report.Requests, report.Seconds)...)
		report.Actors = append(report.Actors, actorRow(actor, s, report.Requests, report.Seconds))
	}

	report.Findings = append(report.Findings, staleLogs(reads, to)...)
	report.Findings = rankFindings(widespread(groupByActor(report.Findings)))
	report.Actors = topActors(report.Actors)

	return report, nil
}

func actorRow(actor auditActor, s *auditStats, total int, seconds float64) auditActorRow {
	row := auditActorRow{Actor: actor, Requests: s.Requests}

	if seconds > 0 {
		row.Rate = float64(s.Requests) / seconds
	}

	if total > 0 {
		row.Share = float64(s.Requests) / float64(total)
	}

	var latency float64

	timed := 0

	for _, stats := range s.Ops {
		for code, n := range stats.Codes {
			switch {
			case code == 429:
				row.Throttled += n
			case code >= 400 && code != 404:
				row.Errors += n
			}
		}

		latency += stats.LatencyMs
		timed += stats.Timed
	}

	if timed > 0 {
		row.LatencyMs = latency / float64(timed)
	}

	if top := topOp(s); s.Ops[top] != nil {
		row.TopVerb, row.TopResource, row.TopCount = top.Verb, top.Resource, s.Ops[top].Count
	}

	return row
}

func topActors(rows []auditActorRow) []auditActorRow {
	slices.SortFunc(rows, func(a, b auditActorRow) int {
		return cmp.Or(cmp.Compare(b.Requests, a.Requests), cmp.Compare(a.Actor.User, b.Actor.User), cmp.Compare(a.Actor.Agent, b.Actor.Agent))
	})

	return rows[:min(len(rows), auditTopActors)]
}

// auditStaleAfter is how far a node's newest event may lag the others' before it counts:
// its API server may be down, or get no traffic (the load balancer skips it).
const auditStaleAfter = 10 * time.Minute

// staleLogs reports the control planes whose audit log stopped well before the others'.
func staleLogs(reads []auditNodeRead, newest time.Time) []auditFinding {
	var out []auditFinding

	for _, r := range reads {
		if r.Error != "" || r.Last == 0 || newest.IsZero() {
			continue
		}

		if lag := newest.Sub(time.UnixMilli(r.Last)); lag > auditStaleAfter {
			out = append(out, auditFinding{Kind: findStaleLog, Severity: sevCritical, Name: r.Node, Value: lag.Seconds()})
		}
	}

	return out
}
