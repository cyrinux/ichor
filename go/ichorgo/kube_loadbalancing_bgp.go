package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A BGPPeer has no status: the session state is the speakers' metallb_bgp_session_up gauge,
// or frrk8s_bgp_session_up from the frr-k8s pods in the FRR-K8s mode MetalLB now defaults to,
// read through the API server's pod proxy. Native mode labels the peer "address:port",
// FRR-K8s the address alone. Current charts serve these metrics over HTTPS behind their own
// token check, which the pod proxy does not pass on: there the state is mostly unknown.
var bgpSessionMetrics = []string{"metallb_bgp_session_up", "frrk8s_bgp_session_up"}

// The metrics endpoints tried, as pod proxy names ("https:" for TLS): the speaker's (9120
// over HTTPS, 7472 on older charts, its FRR sidecar 9121 or 7473), frr-k8s' (frr-metrics
// 9141, its controller 9140).
var (
	speakerMetricsPorts = []string{"https:%s:9120", "%s:7472", "https:%s:9121", "%s:7473"}
	frrK8sMetricsPorts  = []string{"https:%s:9141", "https:%s:9140"}
)

const (
	// maxSpeakers bounds the metrics calls; speakerTimeout each of them.
	maxSpeakers    = 32
	speakerTimeout = 5 * time.Second
	// maxMetricsBytes bounds the metrics text read from one speaker.
	maxMetricsBytes = 4 << 20
)

// bgpSession is one speaker's view of one peer.
type bgpSession struct {
	peer, node string
	up         bool
}

// bgpSessions is what the speakers report; nil when no speaker's metrics could be read.
type bgpSessions []bgpSession

// readBGPSessions asks every MetalLB speaker for its BGP sessions, in parallel. A speaker
// that cannot be reached (often a 403 on the pod proxy) says nothing.
func readBGPSessions(ctx context.Context, k *kubeClient, pods []lbPod) bgpSessions {
	speakers := slices.DeleteFunc(slices.Clone(pods), func(p lbPod) bool {
		return p.App != lbControllerMetalLB || metricsPorts(p) == nil
	})
	speakers = speakers[:min(len(speakers), maxSpeakers)]

	var (
		out  bgpSessions
		read bool
		mu   sync.Mutex
		wg   sync.WaitGroup
	)

	for _, p := range speakers {
		wg.Go(func() {
			sessions, ok := speakerSessions(ctx, k, p)

			mu.Lock()
			defer mu.Unlock()

			read = read || ok
			out = append(out, sessions...)
		})
	}

	wg.Wait()

	if !read {
		return nil
	}

	return append(bgpSessions{}, out...)
}

// metricsPorts are the endpoints a MetalLB pod may serve BGP metrics on: a speaker's or an
// frr-k8s pod's; nil for the others (the controller, the webhook).
func metricsPorts(p lbPod) []string {
	switch {
	case strings.Contains(p.Name, "frr-k8s") && !strings.Contains(p.Name, "webhook"):
		return frrK8sMetricsPorts
	case strings.Contains(p.Name, "speaker"):
		return speakerMetricsPorts
	default:
		return nil
	}
}

// speakerSessions reads one speaker's metrics, trying each known endpoint.
func speakerSessions(ctx context.Context, k *kubeClient, p lbPod) ([]bgpSession, bool) {
	for _, port := range metricsPorts(p) {
		callCtx, cancel := context.WithTimeout(ctx, speakerTimeout)
		text, err := k.getText(callCtx, "/api/v1/namespaces/"+url.PathEscape(p.Namespace)+"/pods/"+
			fmt.Sprintf(port, url.PathEscape(p.Name))+"/proxy/metrics")

		cancel()

		if err != nil {
			continue
		}

		if sessions := parseBGPSessions(text[:min(len(text), maxMetricsBytes)], p.Node); len(sessions) > 0 {
			return sessions, true
		}
	}

	return nil, false
}

// parseBGPSessions reads the session gauges of a Prometheus text exposition.
func parseBGPSessions(text, node string) []bgpSession {
	var out []bgpSession

	for line := range strings.SplitSeq(text, "\n") {
		if !slices.ContainsFunc(bgpSessionMetrics, func(m string) bool { return strings.HasPrefix(line, m+"{") }) {
			continue
		}

		labels, value, ok := strings.Cut(line[strings.IndexByte(line, '{')+1:], "} ")
		if !ok {
			continue
		}

		peer := metricLabel(labels, "peer")
		if peer == "" {
			continue
		}

		v, err := strconv.ParseFloat(strings.Fields(value)[0], 64)
		if err != nil {
			continue
		}

		out = append(out, bgpSession{peer: peer, node: node, up: v == 1})
	}

	return out
}

// metricLabel is the value of name in a label set (`a="x",b="y"`), "" when absent.
func metricLabel(labels, name string) string {
	_, rest, ok := strings.Cut(","+labels, ","+name+`="`)
	if !ok {
		return ""
	}

	value, _, _ := strings.Cut(rest, `"`)

	return value
}

// of lists the sessions with the peer at address, one per node, a down one winning.
func (s bgpSessions) of(address string) []lbBGPSession {
	out := []lbBGPSession{}

	if address == "" {
		return out
	}

	for _, sess := range s {
		if !samePeer(sess.peer, address) {
			continue
		}

		i := slices.IndexFunc(out, func(o lbBGPSession) bool { return o.Node == sess.node })

		switch {
		case i < 0:
			out = append(out, lbBGPSession{Node: sess.node, Up: sess.up})
		case !sess.up:
			out[i].Up = false
		}
	}

	slices.SortFunc(out, func(a, b lbBGPSession) int { return strings.Compare(a.Node, b.Node) })

	return out
}

// samePeer tells whether a metric's peer label ("192.0.2.1", "192.0.2.1:179",
// "[2001:db8::1]:179") names address.
func samePeer(label, address string) bool {
	label = strings.TrimPrefix(label, "[")

	if label == address {
		return true
	}

	port, ok := strings.CutPrefix(label, address)
	if !ok {
		return false
	}

	port = strings.TrimPrefix(strings.TrimPrefix(port, "]"), ":")
	_, err := strconv.Atoi(port)

	return port != "" && err == nil
}
