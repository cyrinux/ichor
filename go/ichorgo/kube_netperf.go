package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// A network test measures the network between two nodes like `cilium connectivity perf`,
// on any CNI: netperf's netserver runs in a pod on the server node, then one short-lived
// netperf pod per measurement on the client node, whose log holds the result. Everything
// lives in a namespace of its own, deleted at the end. The pods pass the "restricted" pod
// security level; only the host network variant needs a privileged namespace.

const (
	// netPerfImage is the image `cilium connectivity perf` uses (netperf 2.7), pinned.
	netPerfImage = "quay.io/cilium/network-perf:3.21-1782913202-88c270c@sha256:c115a00b80bbf4ff49857dd545f0c40025f226d79051b2c8fdab3e8b938c7f92"
	// netPerfName labels the test namespaces, and prefixes their names.
	netPerfName = "ichor-netperf"
	// The netserver control and data ports: fixed, so a node firewall can allow them.
	netPerfControlPort = 12866
	netPerfDataPort    = 12867

	netPerfMinSeconds = 3
	netPerfMaxSeconds = 30
	// netPerfTimeout bounds a whole run.
	netPerfTimeout = 15 * time.Minute
	// netPerfStartTimeout bounds the start of a pod, image pull included.
	netPerfStartTimeout = 4 * time.Minute
	// netPerfStaleAge is when a test namespace left behind (the app was killed) is removed.
	netPerfStaleAge = 30 * time.Minute
	// netPerfCleanupTimeout bounds the namespace deletion, which runs even after a cancel.
	netPerfCleanupTimeout = 30 * time.Second
)

// Network test phases reported through NetPerfListener.OnProgress.
const (
	netPerfPhasePreparing = "preparing" // checking the nodes, creating the namespace
	netPerfPhaseStarting  = "starting"  // starting netserver, pulling the image
	netPerfPhaseTesting   = "testing"   // one measurement (path, test, step of steps)
	netPerfPhaseCleaning  = "cleaning"  // deleting the namespace
)

// Network paths and measurements of a test.
const (
	netPerfPathPod     = "pod"  // pod to pod, through the CNI
	netPerfPathHost    = "host" // node to node, host network
	netPerfThroughput  = "throughput"
	netPerfLatencyTest = "latency"
)

var (
	errNetPerfStopped  = errors.New("network test stopped")
	errNetPerfTimedOut = errors.New("network test timed out")
)

// netPerfRefusal is a test that cannot run as asked (a node not ready, a pod that cannot
// start): reported as is, unlike a failed Kubernetes call it does not drop the client.
type netPerfRefusal struct{ msg string }

func (e *netPerfRefusal) Error() string { return e.msg }

func netPerfRefused(format string, args ...any) error {
	return &netPerfRefusal{fmt.Sprintf(format, args...)}
}

// NetPerfListener follows a network test (implemented in Kotlin/Swift).
type NetPerfListener interface {
	// OnProgress gets {"phase","path","test","step","steps","message","at","results"} on
	// every step; results are the measurements done so far.
	OnProgress(json string)
	// OnDone is called exactly once with the report (see netPerfReport; the measurements
	// done so far on failure) and errMessage, empty on success.
	OnDone(reportJSON string, errMessage string)
}

// NetPerfRun is a handle on a running network test.
type NetPerfRun struct {
	cancel context.CancelFunc
}

// Cancel stops the test; its namespace is still deleted and OnDone follows.
func (r *NetPerfRun) Cancel() { r.cancel() }

// netPerfLatency is a request/response round trip in microseconds.
type netPerfLatency struct {
	Min  float64 `json:"min"`
	Mean float64 `json:"mean"`
	Max  float64 `json:"max"`
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P99  float64 `json:"p99"`
}

type netPerfResult struct {
	Path            string          `json:"path"`
	Test            string          `json:"test"`
	ThroughputMbps  float64         `json:"throughputMbps,omitempty"`
	TransactionRate float64         `json:"transactionRate,omitempty"` // round trips per second
	Latency         *netPerfLatency `json:"latencyUs,omitempty"`
	Error           string          `json:"error,omitempty"`
}

type netPerfReport struct {
	Server      string          `json:"server"`
	Client      string          `json:"client"`
	HostNetwork bool            `json:"hostNetwork"`
	Seconds     int             `json:"seconds"`
	Image       string          `json:"image"`
	Started     int64           `json:"started"`
	Finished    int64           `json:"finished"`
	Results     []netPerfResult `json:"results"`
}

type netPerfProgress struct {
	Phase   string          `json:"phase"`
	Path    string          `json:"path,omitempty"`
	Test    string          `json:"test,omitempty"`
	Step    int             `json:"step,omitempty"`
	Steps   int             `json:"steps"`
	Message string          `json:"message,omitempty"`
	At      int64           `json:"at"`
	Results []netPerfResult `json:"results"`
}

type netPerfOptions struct {
	server, client string
	hostNetwork    bool
	seconds        int
}

type netPerfCase struct{ path, test string }

// cases lists the measurements of a test, in the order they run.
func (o netPerfOptions) cases() []netPerfCase {
	paths := []string{netPerfPathPod}
	if o.hostNetwork {
		paths = append(paths, netPerfPathHost)
	}

	var out []netPerfCase
	for _, p := range paths {
		out = append(out, netPerfCase{p, netPerfThroughput}, netPerfCase{p, netPerfLatencyTest})
	}

	return out
}

// NetPerfNodes lists the Kubernetes nodes a network test can run between, through the
// Kubernetes API (os:admin): {"nodes":[{name,address,controlPlane,ready}]}, by name.
// kubeServer: see KubePods.
func NetPerfNodes(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() netPerfNodeList { return netPerfNodeList{Nodes: demoNetPerfNodes()} }, listNetPerfNodes)
}

// StartNetPerf measures the network from clientNode to serverNode (Kubernetes node names,
// see NetPerfNodes) like `cilium connectivity perf` (os:admin): TCP throughput, then TCP
// round-trip latency, for seconds each (3 to 30), pod to pod and, with hostNetwork, node to
// node. It creates a namespace for the test (privileged with hostNetwork), runs netserver
// and netperf pods there and deletes it at the end, also on failure or Cancel. The
// measurements saturate the link between the nodes while they run. kubeServer: see KubePods.
func StartNetPerf(configYAML, contextName, kubeServer, serverNode, clientNode string, hostNetwork bool, seconds int, listener NetPerfListener) *NetPerfRun {
	contextName = unmaskContext(configYAML, contextName)

	listener = maskedNetPerfListener{listener}

	opts := netPerfOptions{
		server:      privacy.reveal(strings.TrimSpace(serverNode)),
		client:      privacy.reveal(strings.TrimSpace(clientNode)),
		hostNetwork: hostNetwork,
		seconds:     min(max(seconds, netPerfMinSeconds), netPerfMaxSeconds),
	}

	ctx, cancel := context.WithTimeout(context.Background(), netPerfTimeout)
	emit := func(p netPerfProgress) {
		if js, err := toJSON(p); err == nil {
			listener.OnProgress(js)
		}
	}

	go func() {
		defer cancel()

		var (
			report netPerfReport
			err    error
		)

		switch {
		case isDemoContext(configYAML, contextName):
			report, err = runDemoNetPerf(ctx, opts, emit)
		default:
			report, err = runNetPerfWith(ctx, kubeTarget{configYAML, contextName, kubeServer}, opts, emit)
		}

		listener.OnDone(netPerfDone(ctx, report, err))
	}()

	return &NetPerfRun{cancel: cancel}
}

// runNetPerfWith runs a test with target's client. A refusal is returned as is, outside
// withKubeContext, which would take it for a failed call.
func runNetPerfWith(ctx context.Context, target kubeTarget, opts netPerfOptions, emit func(netPerfProgress)) (netPerfReport, error) {
	var (
		report  netPerfReport
		refusal error
	)

	_, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (struct{}, error) {
		var runErr error
		report, runErr = runNetPerf(ctx, k, opts, emit)

		var r *netPerfRefusal
		if errors.As(runErr, &r) {
			refusal, runErr = runErr, nil
		}

		return struct{}{}, runErr
	})
	if refusal != nil {
		err = refusal
	}

	return report, err
}

// netPerfDone is what OnDone gets for a run that returned report and err.
func netPerfDone(ctx context.Context, report netPerfReport, err error) (string, string) {
	errMessage := ""

	// A run that measured everything succeeded, even if the deadline fell during cleanup.
	switch {
	case err == nil:
	case errors.Is(ctx.Err(), context.Canceled):
		errMessage = errNetPerfStopped.Error()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		errMessage = errNetPerfTimedOut.Error()
	default:
		errMessage = err.Error()
	}

	if report.Finished == 0 {
		report.Finished = time.Now().UnixMilli()
	}

	js, jsErr := toJSON(report)
	if jsErr != nil && errMessage == "" {
		errMessage = jsErr.Error()
	}

	return js, errMessage
}

func (o netPerfOptions) validate() error {
	if o.server == "" || o.client == "" {
		return netPerfRefused("choose a server node and a client node")
	}

	for _, n := range []string{o.server, o.client} {
		if !kubeNamePattern.MatchString(n) || strings.Contains(n, "..") {
			return netPerfRefused("invalid Kubernetes node name %q", n)
		}
	}

	return nil
}

// runNetPerf runs a test with k. Every measurement is attempted: one that fails (a firewall
// on the host network) is reported with its error, the others still run.
func runNetPerf(ctx context.Context, k *kubeClient, opts netPerfOptions, emit func(netPerfProgress)) (netPerfReport, error) {
	report := netPerfReport{
		Server: opts.server, Client: opts.client, HostNetwork: opts.hostNetwork, Seconds: opts.seconds,
		Image: netPerfImage, Started: time.Now().UnixMilli(), Results: []netPerfResult{},
	}
	cases := opts.cases()
	progress := func(p netPerfProgress) {
		p.Steps, p.At, p.Results = len(cases), time.Now().UnixMilli(), slices.Clone(report.Results)
		emit(p)
	}

	if err := opts.validate(); err != nil {
		return report, err
	}

	progress(netPerfProgress{Phase: netPerfPhasePreparing})

	if err := checkNetPerfNodes(ctx, k, opts); err != nil {
		return report, err
	}

	sweepNetPerfNamespaces(ctx, k, time.Now())

	ns, err := createNetPerfNamespace(ctx, k, opts.hostNetwork)
	if err != nil {
		return report, fmt.Errorf("create the test namespace: %w", err)
	}

	defer func() {
		progress(netPerfProgress{Phase: netPerfPhaseCleaning})
		deleteNetPerfNamespace(ctx, k, ns)
	}()

	progress(netPerfProgress{Phase: netPerfPhaseStarting, Message: opts.server})

	servers, err := startNetPerfServers(ctx, k, ns, opts, func(msg string) {
		progress(netPerfProgress{Phase: netPerfPhaseStarting, Message: msg})
	})
	if err != nil {
		return report, err
	}

	for i, c := range cases {
		progress(netPerfProgress{Phase: netPerfPhaseTesting, Path: c.path, Test: c.test, Step: i + 1})

		result := netPerfResult{Path: c.path, Test: c.test}
		if srv := servers[c.path]; srv.err != "" {
			result.Error = srv.err
		} else {
			result = runNetPerfCase(ctx, k, ns, opts, c, srv.address, i+1)
		}

		if ctx.Err() != nil {
			return report, ctx.Err()
		}

		report.Results = append(report.Results, result)
	}

	return report, nil
}
