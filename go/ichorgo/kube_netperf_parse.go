package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// netPerfFields are the netperf output selectors, the ones `cilium connectivity perf` reads.
const netPerfFields = "MIN_LATENCY,MEAN_LATENCY,MAX_LATENCY,P50_LATENCY,P90_LATENCY,P99_LATENCY,TRANSACTION_RATE,THROUGHPUT,THROUGHPUT_UNITS"

// parseNetPerf reads netperf's output: a header line, then one line of netPerfFields.
// Latencies are in microseconds; a TCP_STREAM throughput is in 10^6 bits/s.
func parseNetPerf(test, log string) (netPerfResult, error) {
	lines := strings.Split(strings.TrimSpace(log), "\n")

	for i := len(lines) - 1; i >= 0; i-- {
		fields := strings.Split(strings.TrimSpace(lines[i]), ",")
		if len(fields) != 9 {
			continue
		}

		values := make([]float64, 8)

		ok := true
		for j := range values {
			v, err := strconv.ParseFloat(fields[j], 64)
			ok = ok && err == nil
			values[j] = v
		}

		if !ok {
			continue
		}

		result := netPerfResult{Test: test}

		switch test {
		case netPerfThroughput:
			if fields[8] != "10^6bits/s" {
				return result, fmt.Errorf("unexpected netperf throughput unit %q", fields[8])
			}

			result.ThroughputMbps = values[7]
		default:
			result.TransactionRate = values[6]
			result.Latency = &netPerfLatency{Min: values[0], Mean: values[1], Max: values[2], P50: values[3], P90: values[4], P99: values[5]}
		}

		return result, nil
	}

	return netPerfResult{Test: test}, errors.New("no result in the netperf output: " + netPerfTail(log))
}

// netPerfFailure explains a netperf pod that failed.
func netPerfFailure(log string, pod netPerfPod, address string) string {
	firewall := fmt.Sprintf("a firewall may block TCP ports %d-%d", netPerfControlPort, netPerfDataPort)

	switch {
	case strings.Contains(log, "establish control") || strings.Contains(log, "netserver listening"):
		return fmt.Sprintf("no answer from netserver at %s: %s", address, firewall)
	case pod.Status.Reason == "DeadlineExceeded":
		return fmt.Sprintf("timed out waiting for %s: %s", address, firewall)
	}

	for _, c := range pod.Status.ContainerStatuses {
		if t := c.State.Terminated; t != nil && strings.TrimSpace(log) == "" {
			return fmt.Sprintf("netperf failed (%s, exit code %d)", t.Reason, t.ExitCode)
		}
	}

	return "netperf failed: " + netPerfTail(log)
}

// netPerfTail is the last line of log, shortened.
func netPerfTail(log string) string {
	lines := strings.Split(strings.TrimSpace(log), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])

	if last == "" {
		return "empty output"
	}

	if len(last) > 200 {
		return last[:200] + "…"
	}

	return last
}

// netPerfDemoStep is the pause between two steps of the demo test.
var netPerfDemoStep = 700 * time.Millisecond

func demoNetPerfNodes() []netPerfNode {
	nodes := demoNodes()
	out := make([]netPerfNode, 0, len(nodes))

	for _, n := range nodes {
		out = append(out, netPerfNode{Name: n.Hostname, Address: n.Node, ControlPlane: n.Role == "controlplane", Ready: true})
	}

	return out
}

// runDemoNetPerf plays a test with made-up measurements: nothing runs anywhere.
func runDemoNetPerf(ctx context.Context, opts netPerfOptions, emit func(netPerfProgress)) (netPerfReport, error) {
	report := netPerfReport{
		Server: opts.server, Client: opts.client, HostNetwork: opts.hostNetwork, Seconds: opts.seconds,
		Image: netPerfImage, Started: time.Now().UnixMilli(), Results: []netPerfResult{},
	}

	if err := opts.validate(); err != nil {
		return report, err
	}

	cases := opts.cases()
	step := func(p netPerfProgress) error {
		p.Steps, p.At, p.Results = len(cases), time.Now().UnixMilli(), append([]netPerfResult(nil), report.Results...)
		emit(p)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(netPerfDemoStep):
			return nil
		}
	}

	if err := step(netPerfProgress{Phase: netPerfPhasePreparing}); err != nil {
		return report, err
	}

	if err := step(netPerfProgress{Phase: netPerfPhaseStarting, Message: opts.server}); err != nil {
		return report, err
	}

	for i, c := range cases {
		if err := step(netPerfProgress{Phase: netPerfPhaseTesting, Path: c.path, Test: c.test, Step: i + 1}); err != nil {
			return report, err
		}

		report.Results = append(report.Results, demoNetPerfResult(c, opts.server == opts.client))
	}

	return report, step(netPerfProgress{Phase: netPerfPhaseCleaning})
}

func demoNetPerfResult(c netPerfCase, sameNode bool) netPerfResult {
	r := netPerfResult{Path: c.path, Test: c.test}
	scale := 1.0

	if sameNode {
		scale = 3.2
	}

	switch {
	case c.test == netPerfThroughput && c.path == netPerfPathPod:
		r.ThroughputMbps = 8734.5 * scale
	case c.test == netPerfThroughput:
		r.ThroughputMbps = 9412.8 * scale
	case c.path == netPerfPathPod:
		r.TransactionRate = 15873.2 * scale
		r.Latency = &netPerfLatency{Min: 38, Mean: 62.4, Max: 1874, P50: 58, P90: 74, P99: 131}
	default:
		r.TransactionRate = 22421.7 * scale
		r.Latency = &netPerfLatency{Min: 27, Mean: 44.1, Max: 1210, P50: 41, P90: 52, P99: 96}
	}

	return r
}
