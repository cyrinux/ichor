package ichorgo

import (
	"strings"
	"time"
)

// demoNetTool is a canned result of args (tool, target) run from node n: no container.
func demoNetTool(n nodeOverview, args []string) netToolResult {
	tool, target := "", ""
	if len(args) == 2 {
		tool, target = args[0], args[1]
	}

	result := netToolResult{Tool: tool, Target: target, OK: true, Raw: "(demo: canned result, nothing ran on " + n.Hostname + ")"}

	switch tool {
	case netToolDNS:
		result.DNS = &dnsResult{Status: "NOERROR", Server: "10.96.0.10", QueryMs: 2, Records: []dnsRecord{
			{Name: strings.TrimSuffix(target, ".") + ".", Type: "A", TTL: 30, Value: "10.96.0.1"},
		}}
	case netToolPing:
		result.Ping = &pingResult{Sent: 3, Received: 3, MinMs: 0.41, AvgMs: 0.52, MaxMs: 0.64}
	case netToolPort:
		result.Port = &portResult{Open: true, Message: "Connection to " + target + " succeeded!"}
	case netToolTrace:
		result.Trace = []traceHop{
			{Hop: 1, Host: "10.5.0.1", AvgMs: 0.3},
			{Hop: 2, Host: "???", LossPct: 100},
			{Hop: 3, Host: "203.0.113.20", AvgMs: 4.8},
		}
	case netToolHTTP:
		notAfter := time.Now().Add(61 * 24 * time.Hour).UTC().Truncate(time.Hour)
		result.HTTP = &httpResult{
			Status: 200, TotalMs: 84.2, TLS: strings.HasPrefix(target, "https://"),
			Subject: "CN = demo.invalid", Issuer: "CN = Demo CA",
			NotBefore: notAfter.Add(-90 * 24 * time.Hour).Format(time.RFC3339), NotAfter: notAfter.Format(time.RFC3339), DaysLeft: 60,
		}
		result.HTTP.TLSOk = result.HTTP.TLS
	}

	return result
}
