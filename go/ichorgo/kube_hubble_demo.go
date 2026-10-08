package ichorgo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"
)

// demoFlowTick is how often the demo's agents report a burst of flows.
const demoFlowTick = 700 * time.Millisecond

func demoCiliumStatus() ciliumStatus {
	out := ciliumStatus{Installed: true, CNI: flowCNICilium, Namespace: "kube-system", Version: "v1.18.2", Hubble: true, Buffer: hubbleDefaultBuffer, Agents: []ciliumAgent{}}

	for i, n := range demoNodes() {
		out.Agents = append(out.Agents, ciliumAgent{Node: n.Hostname, Pod: fmt.Sprintf("cilium-%c%c2kd", 'a'+i, 'h'+i), Ready: true})
	}

	return out
}

// demoFlowPattern is a kind of traffic the demo repeats.
type demoFlowPattern struct {
	weight             int
	srcNS, src         string // pod, or "" with srcReserved
	srcReserved        string
	dstNS, dst         string
	dstReserved, dstIP string
	dstName            string
	protocol           string
	port               uint32
	verdict, reason    string
	direction          string
	deniedBy           *policyRef
	l7                 string
}

var demoFlowPatterns = []demoFlowPattern{
	{weight: 8, srcNS: "networking", src: "traefik-8c6d-w7r2m", dstNS: "default", dst: "vaultwarden-0", protocol: "TCP", port: 8080, verdict: "FORWARDED", direction: "INGRESS", l7: "HTTP GET /api/sync → 200"},
	{weight: 6, srcNS: "home", src: "home-assistant-0", dstNS: "home", dst: "mosquitto-0", protocol: "TCP", port: 1883, verdict: "FORWARDED", direction: "EGRESS"},
	{weight: 5, srcNS: "home", src: "zigbee2mqtt-0", dstNS: "home", dst: "mosquitto-0", protocol: "TCP", port: 1883, verdict: "FORWARDED", direction: "INGRESS"},
	{weight: 6, srcNS: "media", src: "immich-server-7d9f-k2m8p", dstNS: "media", dst: "immich-postgres-1", protocol: "TCP", port: 5432, verdict: "FORWARDED", direction: "INGRESS"},
	{weight: 5, srcNS: "monitoring", src: "prometheus-kube-prometheus-0", dstNS: "media", dst: "jellyfin-5f8c-xq2wz", protocol: "TCP", port: 8096, verdict: "FORWARDED", direction: "INGRESS", l7: "HTTP GET /metrics → 200"},
	{weight: 6, srcNS: "media", src: "sonarr-0", dstNS: "kube-system", dst: "coredns-7c9d-x2k4p", protocol: "UDP", port: 53, verdict: "FORWARDED", direction: "EGRESS", l7: "DNS api.example.org. A"},
	{weight: 3, srcReserved: "world", dstNS: "networking", dst: "traefik-8c6d-w7r2m", protocol: "TCP", port: 443, verdict: "FORWARDED", direction: "INGRESS"},
	{weight: 3, srcNS: "media", src: "jellyfin-5f8c-xq2wz", dstNS: "media", dst: "immich-postgres-1", protocol: "TCP", port: 5432, verdict: "DROPPED", reason: "POLICY_DENIED", direction: "INGRESS"},
	{weight: 2, srcNS: "home", src: "home-assistant-0", dstReserved: "world", dstIP: "198.51.100.25", dstName: "smtp.example.net", protocol: "TCP", port: 25, verdict: "DROPPED", reason: "POLICY_DENY", direction: "EGRESS",
		deniedBy: &policyRef{Kind: kindCiliumPolicy, Namespace: "home", Name: "home-egress"}},
	{weight: 2, srcNS: "home", src: "zigbee2mqtt-0", dstReserved: "world", dstIP: "198.51.100.80", dstName: "ota.example.org", protocol: "TCP", port: 443, verdict: "DROPPED", reason: "POLICY_DENIED", direction: "EGRESS"},
	{weight: 1, srcNS: "media", src: "radarr-0", dstNS: "default", dst: "vaultwarden-0", protocol: "TCP", port: 8080, verdict: "AUDIT", reason: "POLICY_DENIED", direction: "INGRESS"},
}

// runDemoHubble feeds the demo's traffic through the real aggregation until ctx ends.
func runDemoHubble(ctx context.Context, filter hubbleFilter, emit func(hubbleSnapshot)) error {
	status := demoCiliumStatus()
	agg := newHubbleAgg(status)
	agg.setPolicies(demoNetPolicies().Policies, nil)

	rng := rand.New(rand.NewPCG(7, 11))
	nodes := demoPodNodes()
	now := time.Now()

	// Backfill like --last: a minute of history.
	for i := 0; i < 60; i++ {
		demoFlowBurst(agg, rng, nodes, filter, now.Add(time.Duration(i-60)*time.Second))
	}

	go func() {
		ticker := time.NewTicker(demoFlowTick)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case t := <-ticker.C:
				demoFlowBurst(agg, rng, nodes, filter, t)
			}
		}
	}()

	emitSnapshots(ctx, agg, emit)

	return ctx.Err()
}

// demoPodNodes maps each demo pod to its node's hostname.
func demoPodNodes() map[string]string {
	hosts := demoNodes()
	out := map[string]string{}

	for _, w := range demoWorkloads {
		out[w.namespace+"/"+w.pod] = hosts[w.node].Hostname
	}

	return out
}

func demoFlowBurst(agg *hubbleAgg, rng *rand.Rand, nodes map[string]string, filter hubbleFilter, at time.Time) {
	total := 0
	for _, p := range demoFlowPatterns {
		total += p.weight
	}

	for range 1 + rng.IntN(3) {
		pick := rng.IntN(total)

		for _, p := range demoFlowPatterns {
			if pick -= p.weight; pick < 0 {
				f := p.flow(rng, nodes, at.Add(-time.Duration(rng.IntN(500))*time.Millisecond))
				if filter.keeps(f) {
					agg.add(f.Node, hubbleLine{flow: &f})
				}

				break
			}
		}
	}
}

func (p demoFlowPattern) flow(rng *rand.Rand, nodes map[string]string, at time.Time) hubbleFlow {
	f := hubbleFlow{
		Time: at.UnixMilli(), Verdict: p.verdict, Reason: p.reason, Direction: p.direction,
		Protocol: p.protocol, Port: p.port, Type: "L3_L4",
		Source:      demoPeer(p.srcNS, p.src, p.srcReserved, fmt.Sprintf("203.0.113.%d", 100+rng.IntN(50)), ""),
		Destination: demoPeer(p.dstNS, p.dst, p.dstReserved, p.dstIP, p.dstName),
	}

	if p.protocol == "TCP" {
		f.Flags = "SYN"
	}

	if p.l7 != "" {
		f.Type, f.L7 = "L7", p.l7
	}

	if p.deniedBy != nil {
		f.DeniedBy = []policyRef{*p.deniedBy}
	}

	// A flow is reported by the node of the endpoint that enforces it.
	f.Node = nodes[p.srcNS+"/"+p.src]
	if p.direction == "INGRESS" || f.Node == "" {
		f.Node = nodes[p.dstNS+"/"+p.dst]
	}

	return f
}

func demoPeer(namespace, pod, reserved, ip, name string) hubblePeer {
	if pod == "" {
		peer := hubblePeer{Reserved: reserved, IP: ip, Identity: 2, Labels: []string{"reserved:" + reserved}}
		if name != "" {
			peer.Names = []string{name}
		}

		return peer
	}

	base := podBaseName(pod)

	return hubblePeer{
		Namespace: namespace, Pod: pod, Workload: base, Identity: uint32(10000 + len(base)*97 + len(namespace)*13),
		IP:     fmt.Sprintf("10.244.%d.%d", len(namespace)%8, 10+len(pod)*3%200),
		Labels: []string{"k8s:app=" + base, "k8s:" + ciliumNamespaceLabel + "=" + namespace},
	}
}
