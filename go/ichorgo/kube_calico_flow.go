package ichorgo

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

// A Whisker flow as the app shows it: one `data:` event of whisker-backend's
// GET /flows?watch=true (its FlowResponse), mapped to the hubbleFlow the views know.
// Goldmane aggregates: a flow is 15 seconds of one source, destination, protocol and port,
// with the policies that decided it, not one packet.

// whiskerFlow is the wire shape (snake_case, as whisker-backend marshals it).
type whiskerFlow struct {
	StartTime       string          `json:"start_time"`
	EndTime         string          `json:"end_time"`
	Action          string          `json:"action"` // Allow, Deny, Pass
	SourceName      string          `json:"source_name"`
	SourceNamespace string          `json:"source_namespace"`
	SourceLabels    string          `json:"source_labels"` // "k=v | k=v"
	SourceType      string          `json:"source_type"`   // WorkloadEndpoint, HostEndpoint, NetworkSet, Network; "" on 3.30
	DestName        string          `json:"dest_name"`
	DestNamespace   string          `json:"dest_namespace"`
	DestLabels      string          `json:"dest_labels"`
	DestType        string          `json:"dest_type"`
	Protocol        string          `json:"protocol"`
	DestPort        int64           `json:"dest_port"`
	Reporter        string          `json:"reporter"` // Src: seen leaving the source; Dst: reaching the destination
	Policies        whiskerPolicies `json:"policies"`
	PacketsIn       uint64          `json:"packets_in"`
	PacketsOut      uint64          `json:"packets_out"`
}

type whiskerPolicies struct {
	Enforced []whiskerPolicyHit `json:"enforced"`
	Pending  []whiskerPolicyHit `json:"pending"` // staged policies: what would happen
}

// whiskerPolicyHit is one policy Calico evaluated for the flow. An EndOfTier hit is the
// implicit deny of a tier none of whose rules matched: Trigger names the policy that
// selected the endpoint there.
type whiskerPolicyHit struct {
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Tier      string            `json:"tier"`
	Action    string            `json:"action"`
	Trigger   *whiskerPolicyHit `json:"trigger"`
}

// Whisker's names for what is not a pod.
const (
	whiskerPublicNetwork  = "PUBLIC NETWORK"
	whiskerPrivateNetwork = "PRIVATE NETWORK"
	whiskerGlobal         = "Global"
	// whiskerKnpPrefix is how Calico names a Kubernetes NetworkPolicy in its own store.
	whiskerKnpPrefix = "knp.default."
)

// parseWhiskerFlow maps one event; ok is false for a payload that is not a flow.
func parseWhiskerFlow(data []byte) (hubbleFlow, bool) {
	var w whiskerFlow
	if err := json.Unmarshal(data, &w); err != nil || w.Action == "" {
		return hubbleFlow{}, false
	}

	f := hubbleFlow{
		Time:        whiskerTime(w.StartTime, w.EndTime),
		Verdict:     whiskerVerdict(w.Action),
		Protocol:    strings.ToUpper(w.Protocol),
		Port:        uint32(max(w.DestPort, 0)),
		Type:        "L3_L4",
		Source:      whiskerPeer(w.SourceName, w.SourceNamespace, w.SourceLabels, w.SourceType),
		Destination: whiskerPeer(w.DestName, w.DestNamespace, w.DestLabels, w.DestType),
		Packets:     w.PacketsIn + w.PacketsOut,
	}

	switch w.Reporter {
	case "Dst":
		f.Direction = "INGRESS"
	case "Src":
		f.Direction = "EGRESS"
	}

	f.DeniedBy, f.Isolating, f.Reason = whiskerAttribution(w.Policies.Enforced)

	if f.Verdict != "DROPPED" {
		f.Reason = ""
	} else if f.Reason == "" {
		f.Reason = "POLICY_DENIED"
	}

	return f, true
}

// whiskerTime is the flow's start (its aggregation bucket) in unix ms, else its end, else 0.
func whiskerTime(start, end string) int64 {
	for _, s := range []string{start, end} {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UnixMilli()
		}
	}

	return 0
}

// whiskerVerdict maps Calico's final action: a Pass left the profiles decide, which allow.
func whiskerVerdict(action string) string {
	switch action {
	case "Allow", "Pass":
		return "FORWARDED"
	case "Deny":
		return "DROPPED"
	default:
		return strings.ToUpper(action)
	}
}

// whiskerPeer maps one side. Workload names are aggregates ("curl-85bfb6d759-*" for a
// ReplicaSet's pods): the workload alone then; an exact name is a pod. Off-cluster peers and host
// endpoints are reserved names, like Cilium's. The labels are kept as k8s: labels, with
// Calico's own, so the policies can be matched against them.
func whiskerPeer(name, namespace, labels, kind string) hubblePeer {
	if namespace == "-" || namespace == whiskerGlobal {
		namespace = ""
	}

	p := hubblePeer{Namespace: namespace}

	switch {
	case name == whiskerPublicNetwork:
		p.Reserved = "world"
	case name == whiskerPrivateNetwork:
		p.Reserved = "private-network"
	case kind == "HostEndpoint":
		p.Reserved, p.Workload = "host", name
	case kind == "NetworkSet", kind == "Network":
		p.Workload = name
	case strings.HasSuffix(name, "*"):
		// "curl-85bfb6d759-*": the pods of one ReplicaSet; the Deployment is the workload.
		p.Workload = podBaseName(strings.TrimSuffix(strings.TrimSuffix(name, "*"), "-"))
	case name != "":
		p.Pod, p.Workload = name, podBaseName(name)
	}

	if p.Reserved != "" && p.Workload == "" {
		return p
	}

	for _, l := range strings.Split(labels, "|") {
		if l = strings.TrimSpace(l); strings.Contains(l, "=") {
			p.Labels = append(p.Labels, "k8s:"+l)
		}
	}

	if namespace != "" {
		p.Labels = append(p.Labels, "k8s:"+calicoNamespaceLabel+"="+namespace, "k8s:"+calicoOrchestratorLabel+"=k8s")
	}

	return p
}

// whiskerAttribution reads the enforced hits: an explicit Deny names the policy that denied
// the flow (POLICY_DENY); a tier ending in Deny names the policy that put the endpoint in
// default-deny there (POLICY_DENIED).
func whiskerAttribution(hits []whiskerPolicyHit) (deniedBy, isolating []policyRef, reason string) {
	for _, h := range hits {
		switch {
		case h.Kind == "EndOfTier" && h.Action == "Deny" && h.Trigger != nil:
			if ref, ok := whiskerPolicyRef(*h.Trigger); ok {
				isolating = append(isolating, ref)
			}

			if reason == "" {
				reason = "POLICY_DENIED"
			}
		case h.Kind != "EndOfTier" && h.Action == "Deny":
			// A profile's deny (no policy to name) is a default-deny for the view.
			if ref, ok := whiskerPolicyRef(h); ok {
				deniedBy = append(deniedBy, ref)
				reason = "POLICY_DENY"
			}
		}
	}

	return deniedBy, isolating, reason
}

// whiskerPolicyRef names a hit the way the policy list does; ok is false for what the list
// never shows (profiles, staged policies, the end of a tier).
func whiskerPolicyRef(h whiskerPolicyHit) (policyRef, bool) {
	name := h.Name

	switch h.Kind {
	case "CalicoNetworkPolicy", "GlobalNetworkPolicy":
		// Objects are stored with their tier as a prefix: "default.deny-world".
		if h.Tier != "" && !strings.HasPrefix(name, h.Tier+".") {
			name = h.Tier + "." + name
		}

		if h.Kind == "GlobalNetworkPolicy" {
			return policyRef{Kind: kindCalicoGlobalPolicy, Name: name}, true
		}

		return policyRef{Kind: kindCalicoPolicy, Namespace: h.Namespace, Name: name}, true
	case "NetworkPolicy":
		return policyRef{Kind: kindNetworkPolicy, Namespace: h.Namespace, Name: strings.TrimPrefix(name, whiskerKnpPrefix)}, true
	case "AdminNetworkPolicy", "BaselineAdminNetworkPolicy", "ClusterNetworkPolicy":
		return policyRef{Kind: h.Kind, Name: name}, true
	default:
		return policyRef{}, false
	}
}

// readWhiskerSSE hands each `data:` event to onData until the stream ends. Whisker reports
// a failure of its own (Goldmane gone) as an `error:` event: that ends the stream with it.
func readWhiskerSSE(r io.Reader, onData func(data []byte)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxSSELine)

	for sc.Scan() {
		line := sc.Bytes()

		if data, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			onData(bytes.TrimSpace(data))
		} else if msg, ok := bytes.CutPrefix(line, []byte("error:")); ok {
			return errors.New("Whisker: " + strings.TrimSpace(string(msg)))
		}
	}

	return sc.Err()
}
