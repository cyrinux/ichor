package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
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
// token check, which the pod proxy does not pass on: there the cluster's Prometheus, which
// scrapes them with its own token, is asked instead (promBGPSessions).
var bgpSessionMetrics = []string{"metallb_bgp_session_up", "frrk8s_bgp_session_up"}

// promBGPQuery is the same gauges from Prometheus, one series per speaker and peer. An
// instant query: a series gone stale (a speaker that stopped) is not read as "down".
const promBGPQuery = "max by (peer, pod, instance) (metallb_bgp_session_up or frrk8s_bgp_session_up)"

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
// that cannot be reached (often a 403 on the pod proxy) says nothing; when none answered,
// the cluster's Prometheus is asked. nil when neither could tell.
func readBGPSessions(ctx context.Context, k *kubeClient, pods []lbPod) bgpSessions {
	if s := speakersBGPSessions(ctx, k, pods); s != nil {
		return s
	}

	// No speaker running here (kube-vip alone): nothing Prometheus could add.
	if !slices.ContainsFunc(pods, func(p lbPod) bool { return p.App == lbControllerMetalLB && metricsPorts(p) != nil }) {
		return nil
	}

	return promBGPSessions(ctx, k, pods)
}

// speakersBGPSessions reads the speakers' own metrics; nil when none answered.
func speakersBGPSessions(ctx context.Context, k *kubeClient, pods []lbPod) bgpSessions {
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

// promBGPSessions asks the first Prometheus discovered for the session gauges. A series names
// its speaker by pod (a ServiceMonitor's target) or by instance address (the hostNetwork
// pod's, so its node's); the node is the pod's. nil when there is no Prometheus, it cannot be
// read, or it has no such series.
func promBGPSessions(ctx context.Context, k *kubeClient, pods []lbPod) bgpSessions {
	found, err := discoverProm(ctx, k)
	if err != nil || len(found.Sources) == 0 {
		return nil
	}

	src := found.Sources[0]
	apiPath := "/api/v1/query?query=" + url.QueryEscape(promBGPQuery)

	data, err := promAPIData(src, apiPath)(promSendWith(ctx, k, src, http.MethodGet, apiPath))
	if err != nil {
		return nil
	}

	var res struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &res); err != nil {
		return nil
	}

	nodeOf := map[string]string{}
	for _, p := range pods {
		nodeOf["pod/"+p.Name] = p.Node
		for _, ip := range p.IPs {
			nodeOf["ip/"+ip] = p.Node
		}
	}

	var out bgpSessions

	for _, r := range res.Result {
		peer := r.Metric["peer"]
		if peer == "" || len(r.Value) != 2 {
			continue
		}

		value, _ := r.Value[1].(string)
		host := instanceHost(r.Metric["instance"])
		node := cmp.Or(nodeOf["pod/"+r.Metric["pod"]], nodeOf["ip/"+host], host)

		out = append(out, bgpSession{peer: peer, node: node, up: value == "1"})
	}

	return out
}

// instanceHost is the address of a scrape target's instance label ("192.0.2.5:7472",
// "[2001:db8::5]:7472"), without its port.
func instanceHost(instance string) string {
	if host, _, err := net.SplitHostPort(instance); err == nil {
		return host
	}

	return instance
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

		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}

		v, err := strconv.ParseFloat(fields[0], 64)
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
// "[2001:db8::1]:179", native mode's "2001:db8::1:179") names address: the address alone,
// or followed by ":<port>" (or "]:<port>").
func samePeer(label, address string) bool {
	label = strings.TrimPrefix(label, "[")

	if label == address {
		return true
	}

	rest, ok := strings.CutPrefix(label, address)
	if !ok {
		return false
	}

	port, ok := strings.CutPrefix(strings.TrimPrefix(rest, "]"), ":")
	if !ok || port == "" {
		return false
	}

	_, err := strconv.Atoi(port)

	return err == nil
}
