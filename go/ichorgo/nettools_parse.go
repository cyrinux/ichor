package ichorgo

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// netToolResult is a network tool's parsed run; only the tool's own section is set.
type netToolResult struct {
	Tool   string `json:"tool"`
	Target string `json:"target"`
	OK     bool   `json:"ok"`
	// ExitCode is the tool's (the http check always exits 0).
	ExitCode int         `json:"exitCode"`
	DNS      *dnsResult  `json:"dns,omitempty"`
	Ping     *pingResult `json:"ping,omitempty"`
	Port     *portResult `json:"port,omitempty"`
	Trace    []traceHop  `json:"trace,omitempty"`
	HTTP     *httpResult `json:"http,omitempty"`
	Raw      string      `json:"raw"`
}

type dnsRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	TTL   int    `json:"ttl"`
	Value string `json:"value"`
}

type dnsResult struct {
	// Status is the answer's (NOERROR, NXDOMAIN, SERVFAIL…), "" when dig got none.
	Status  string      `json:"status"`
	Records []dnsRecord `json:"records"`
	// Server answered, as dig prints it without the port ("10.96.0.10").
	Server  string `json:"server"`
	QueryMs int    `json:"queryMs"`
}

type pingResult struct {
	Sent     int     `json:"sent"`
	Received int     `json:"received"`
	LossPct  float64 `json:"lossPct"`
	MinMs    float64 `json:"minMs"`
	AvgMs    float64 `json:"avgMs"`
	MaxMs    float64 `json:"maxMs"`
}

type portResult struct {
	Open    bool   `json:"open"`
	Message string `json:"message"`
}

// traceHop is a hop of the path; Host is "???" when it did not answer.
type traceHop struct {
	Hop     int     `json:"hop"`
	Host    string  `json:"host"`
	LossPct float64 `json:"lossPct"`
	AvgMs   float64 `json:"avgMs"`
}

type httpResult struct {
	// Status is the final status after redirects; 0: no answer.
	Status      int     `json:"status"`
	RedirectURL string  `json:"redirectUrl"`
	TotalMs     float64 `json:"totalMs"`
	// TLS: an https URL; TLSOk then says whether its certificate verified.
	TLS       bool   `json:"tls"`
	TLSOk     bool   `json:"tlsOk"`
	Subject   string `json:"subject"`
	Issuer    string `json:"issuer"`
	NotBefore string `json:"notBefore"` // RFC 3339
	NotAfter  string `json:"notAfter"`  // RFC 3339
	DaysLeft  int    `json:"daysLeft"`
	// Error is curl's, when it could not connect.
	Error string `json:"error,omitempty"`
}

// parseNetTool reads raw, the run's output, into the tool's result; now dates the certificate.
func parseNetTool(cmd netToolCommand, raw string, exitCode int, now time.Time) (netToolResult, error) {
	result := netToolResult{Tool: cmd.tool, Target: cmd.target, ExitCode: exitCode, Raw: raw}

	switch cmd.tool {
	case netToolDNS:
		dns := parseDig(raw)
		result.DNS = &dns
		result.OK = exitCode == 0 && (dns.Status == "NOERROR" || dns.Status == "" && len(dns.Records) > 0)

		if exitCode != 0 && dns.Status == "" && len(dns.Records) == 0 && dns.Server == "" {
			return result, errors.New("dig failed: " + netToolTail(raw))
		}
	case netToolPing:
		ping, ok := parsePing(raw)
		if !ok {
			return result, errors.New("ping failed: " + netToolTail(raw))
		}

		result.Ping = &ping
		result.OK = ping.Received > 0
	case netToolPort:
		port := parseNc(raw, exitCode)
		result.Port = &port
		result.OK = port.Open
	case netToolTrace:
		hops := parseMtr(raw)
		if len(hops) == 0 {
			hops = parseTracepath(raw)
		}

		if len(hops) == 0 {
			return result, errors.New("traceroute failed: " + netToolTail(raw))
		}

		result.Trace = hops
		result.OK = hops[len(hops)-1].Host != "???"
	case netToolHTTP:
		http, ok := parseCurlTLS(raw, cmd.https, now)
		if !ok {
			return result, errors.New("curl failed: " + netToolTail(raw))
		}

		result.HTTP = &http
		result.OK = http.Status > 0 && http.Status < 400 && (!http.TLS || http.TLSOk)
	}

	return result, nil
}

var (
	digStatus = regexp.MustCompile(`status: ([A-Z]+)`)
	digQuery  = regexp.MustCompile(`;; Query time: (\d+) msec`)
	digServer = regexp.MustCompile(`;; SERVER: ([^#\s]+)`)
)

// parseDig reads `dig +noall +answer +comments +stats`: the header's status, the answer
// lines (name ttl class type value…), the query time and the server.
func parseDig(raw string) dnsResult {
	result := dnsResult{Records: []dnsRecord{}}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)

		switch {
		case line == "":
		case strings.HasPrefix(line, ";"):
			if m := digStatus.FindStringSubmatch(line); m != nil && result.Status == "" {
				result.Status = m[1]
			}

			if m := digQuery.FindStringSubmatch(line); m != nil {
				result.QueryMs, _ = strconv.Atoi(m[1])
			}

			if m := digServer.FindStringSubmatch(line); m != nil {
				result.Server = m[1]
			}
		default:
			if record, ok := parseDigRecord(line); ok {
				result.Records = append(result.Records, record)
			}
		}
	}

	return result
}

func parseDigRecord(line string) (dnsRecord, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[2] != "IN" {
		return dnsRecord{}, false
	}

	ttl, err := strconv.Atoi(fields[1])
	if err != nil {
		return dnsRecord{}, false
	}

	return dnsRecord{Name: fields[0], Type: fields[3], TTL: ttl, Value: strings.Join(fields[4:], " ")}, true
}

var (
	pingCounts = regexp.MustCompile(`(\d+) packets transmitted, (\d+) (?:packets )?received`)
	pingLoss   = regexp.MustCompile(`([\d.]+)% packet loss`)
	pingRTT    = regexp.MustCompile(`(?:rtt|round-trip) min/avg/max(?:/[a-z]+)? = ([\d.]+)/([\d.]+)/([\d.]+)`)
)

// parsePing reads ping's summary (iputils or busybox); false without one.
func parsePing(raw string) (pingResult, bool) {
	m := pingCounts.FindStringSubmatch(raw)
	if m == nil {
		return pingResult{}, false
	}

	var result pingResult

	result.Sent, _ = strconv.Atoi(m[1])
	result.Received, _ = strconv.Atoi(m[2])

	if l := pingLoss.FindStringSubmatch(raw); l != nil {
		result.LossPct = parseFiniteFloat(l[1])
	}

	if r := pingRTT.FindStringSubmatch(raw); r != nil {
		result.MinMs, result.AvgMs, result.MaxMs = parseFiniteFloat(r[1]), parseFiniteFloat(r[2]), parseFiniteFloat(r[3])
	}

	return result, true
}

// parseNc reads `nc -zv`: open when it exits 0; the message is its last line.
func parseNc(raw string, exitCode int) portResult {
	message := ""

	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			message = line
		}
	}

	return portResult{Open: exitCode == 0, Message: message}
}

var mtrHop = regexp.MustCompile(`^\s*(\d+)\.(?:\|--)?\s+(.+)$`)

// parseMtr reads `mtr -rwz -n`: "  1. AS???  10.0.0.1  0.0%  10  0.3  0.3  0.2  0.4  0.1"
// (the AS column only with -z).
func parseMtr(raw string) []traceHop {
	hops := []traceHop{}

	for _, line := range strings.Split(raw, "\n") {
		m := mtrHop.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		fields := strings.Fields(m[2])
		if len(fields) > 0 && strings.HasPrefix(fields[0], "AS") {
			fields = fields[1:]
		}

		// host loss% sent last avg best worst stdev
		if len(fields) < 5 {
			continue
		}

		hop, _ := strconv.Atoi(m[1])
		hops = append(hops, traceHop{
			Hop:     hop,
			Host:    fields[0],
			LossPct: parseFiniteFloat(strings.TrimSuffix(fields[1], "%")),
			AvgMs:   parseFiniteFloat(fields[4]),
		})
	}

	return hops
}

var tracepathHop = regexp.MustCompile(`^\s*(\d+)\??:\s+(\S+)(?:\s+([\d.]+)ms)?`)

// parseTracepath reads `tracepath -n`: a hop's first answer, "no reply" as a lost hop.
func parseTracepath(raw string) []traceHop {
	hops := []traceHop{}
	seen := map[int]bool{}

	for _, line := range strings.Split(raw, "\n") {
		m := tracepathHop.FindStringSubmatch(line)
		if m == nil || m[2] == "[LOCALHOST]" {
			continue
		}

		hop, _ := strconv.Atoi(m[1])
		if seen[hop] {
			continue
		}

		seen[hop] = true

		if m[2] == "no" { // "no reply"
			hops = append(hops, traceHop{Hop: hop, Host: "???", LossPct: 100})

			continue
		}

		hops = append(hops, traceHop{Hop: hop, Host: m[2], AvgMs: parseFiniteFloat(m[3])})
	}

	return hops
}

const opensslDate = "Jan _2 15:04:05 2006 MST"

// parseCurlTLS reads the http script's output: curl's "ICHOR-HTTP code|redirect|verify|time"
// line, then for https the certificate's subject, issuer and dates. False without curl's line.
func parseCurlTLS(raw string, https bool, now time.Time) (httpResult, bool) {
	result := httpResult{TLS: https}
	found := false

	var curlErrors []string

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "ICHOR-HTTP "):
			found = true
			parts := strings.Split(strings.TrimPrefix(line, "ICHOR-HTTP "), "|")

			if len(parts) == 4 {
				result.Status, _ = strconv.Atoi(parts[0])
				result.RedirectURL = parts[1]
				result.TLSOk = https && parts[2] == "0" && result.Status > 0
				result.TotalMs = math.Round(parseFiniteFloat(parts[3])*1000*10) / 10
			}
		case strings.HasPrefix(line, "curl: "):
			curlErrors = append(curlErrors, strings.TrimPrefix(line, "curl: "))
		case strings.HasPrefix(line, "subject="):
			result.Subject = strings.TrimSpace(strings.TrimPrefix(line, "subject="))
		case strings.HasPrefix(line, "issuer="):
			result.Issuer = strings.TrimSpace(strings.TrimPrefix(line, "issuer="))
		case strings.HasPrefix(line, "notBefore="):
			if t, err := time.Parse(opensslDate, strings.TrimPrefix(line, "notBefore=")); err == nil {
				result.NotBefore = t.UTC().Format(time.RFC3339)
			}
		case strings.HasPrefix(line, "notAfter="):
			if t, err := time.Parse(opensslDate, strings.TrimPrefix(line, "notAfter=")); err == nil {
				result.NotAfter = t.UTC().Format(time.RFC3339)
				result.DaysLeft = int(math.Floor(t.Sub(now).Hours() / 24))
			}
		}
	}

	result.Error = strings.Join(curlErrors, "; ")

	return result, found
}

// parseFiniteFloat is 0 for what is not a finite number (json.Marshal refuses NaN and Inf).
func parseFiniteFloat(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}

	return v
}

// netToolTail is the end of the output, for an error message.
func netToolTail(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "no output"
	}

	lines := strings.Split(raw, "\n")

	return strings.Join(lines[max(0, len(lines)-3):], " / ")
}
