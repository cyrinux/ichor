package ichorgo

import (
	"regexp"
	"strings"
	"time"
)

func demoLogsDiscovery() logsDiscovery {
	return logsDiscoveryOf([]promSource{
		{Mode: promModeProxy, Kind: logsKindLoki, Namespace: "monitoring", Service: "loki-gateway", Port: 80},
	})
}

// demoLogsLabels offers what demoLogsQuery can narrow on.
func demoLogsLabels(label string) []string {
	switch label {
	case "":
		return []string{"app", "namespace", "pod"}
	case "namespace":
		return []string{"demo-shop", "kube-system", "monitoring"}
	case "pod":
		return []string{"checkout-7c9d-2xk4q", "payments-5f8b-qr7t2"}
	case "app":
		return []string{"checkout", "payments"}
	default:
		return []string{}
	}
}

var (
	demoLogsSelector = regexp.MustCompile(`\b(namespace|pod)\s*[=:]\s*"([^"]*)"`)
	demoLogsContains = regexp.MustCompile(`\|=\s*"([^"]*)"`)
)

// demoLogsQuery gives sample lines, taking from the query the namespace and pod it selects and
// the text its line filter asks for, so a filter and "open in" behave as on a real backend.
func demoLogsQuery(query string, now time.Time) []logsLine {
	namespace, pod := "demo-shop", "checkout-7c9d-2xk4q"

	for _, m := range demoLogsSelector.FindAllStringSubmatch(query, -1) {
		if m[1] == "namespace" {
			namespace = m[2]
		} else {
			pod = m[2]
		}
	}

	samples := []struct {
		age  time.Duration
		text string
	}{
		{2 * time.Minute, `level=error msg="payment gateway timeout" order=1042 upstream=payments:8080`},
		{9 * time.Minute, `level=info msg="order accepted" order=1041`},
		{37 * time.Minute, `level=warn msg="retrying after timeout" order=1039 attempt=2`},
		{3 * time.Hour, `level=info msg="order accepted" order=1033`},
		{20 * time.Hour, `level=error msg="payment declined" order=1012`},
	}

	var contains []string
	for _, m := range demoLogsContains.FindAllStringSubmatch(query, -1) {
		contains = append(contains, m[1])
	}

	lines := []logsLine{}

	for _, s := range samples {
		matches := true
		for _, c := range contains {
			matches = matches && strings.Contains(s.text, c)
		}

		if matches {
			lines = append(lines, logsLine{
				Time:   now.Add(-s.age).UnixMilli(),
				Labels: map[string]string{"namespace": namespace, "pod": pod},
				Text:   s.text,
			})
		}
	}

	return lines
}
