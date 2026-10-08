package ichorgo

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// A Hubble flow as the app shows it, parsed from one `hubble observe -o jsonpb` line (a
// GetFlowsResponse of Cilium's api/v1/observer). Labels are kept for the policy search.
type hubbleFlow struct {
	Time        int64       `json:"time"` // unix ms
	Node        string      `json:"node"`
	Verdict     string      `json:"verdict"`          // FORWARDED, DROPPED, AUDIT, ERROR, …
	Reason      string      `json:"reason,omitempty"` // drop reason (POLICY_DENIED, …)
	Direction   string      `json:"direction,omitempty"`
	Protocol    string      `json:"protocol,omitempty"` // TCP, UDP, ICMPv4, …
	Port        uint32      `json:"port,omitempty"`     // destination port
	Flags       string      `json:"flags,omitempty"`    // TCP flags, "SYN", "SYN,ACK"
	Reply       bool        `json:"reply,omitempty"`
	Type        string      `json:"type,omitempty"` // L3_L4, L7, …
	L7          string      `json:"l7,omitempty"`   // "DNS query example.org. A", "HTTP GET /"
	Source      hubblePeer  `json:"source"`
	Destination hubblePeer  `json:"destination"`
	DeniedBy    []policyRef `json:"deniedBy,omitempty"`
	// Isolating is what put the endpoint in default-deny when the flow itself tells (Calico's
	// policy trace); Packets is how many packets an aggregated (Calico) flow stood for.
	Isolating []policyRef `json:"isolating,omitempty"`
	Packets   uint64      `json:"packets,omitempty"`
}

type hubblePeer struct {
	Namespace string   `json:"namespace,omitempty"`
	Pod       string   `json:"pod,omitempty"`
	Workload  string   `json:"workload,omitempty"`
	Identity  uint32   `json:"identity,omitempty"`
	IP        string   `json:"ip,omitempty"`
	Names     []string `json:"names,omitempty"`    // DNS names Cilium knows for the IP
	Reserved  string   `json:"reserved,omitempty"` // world, host, remote-node, kube-apiserver…
	Labels    []string `json:"-"`
}

// policyRef names a policy: a NetworkPolicy, CiliumNetworkPolicy or
// CiliumClusterwideNetworkPolicy (no namespace).
type policyRef struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// The wire shape. Keys are normalised first (see normaliseKeys): Cilium versions print
// either the proto names (drop_reason_desc) or their JSON names (dropReasonDesc).
type wireFlowResponse struct {
	Flow       *wireFlow `json:"flow"`
	LostEvents *struct {
		NumEventsLost uint64 `json:"numeventslost"`
	} `json:"lostevents"`
	NodeName string   `json:"nodename"`
	Time     wireTime `json:"time"`
}

type wireFlow struct {
	Time             wireTime     `json:"time"`
	Verdict          wireEnum     `json:"verdict"`
	DropReasonDesc   wireEnum     `json:"dropreasondesc"`
	DropReason       uint32       `json:"dropreason"`
	IP               *wireIP      `json:"ip"`
	L4               wireL4       `json:"l4"`
	L7               *wireL7      `json:"l7"`
	Source           wireEndpoint `json:"source"`
	Destination      wireEndpoint `json:"destination"`
	Type             wireEnum     `json:"type"`
	NodeName         string       `json:"nodename"`
	SourceNames      []string     `json:"sourcenames"`
	DestinationNames []string     `json:"destinationnames"`
	TrafficDirection wireEnum     `json:"trafficdirection"`
	IsReply          *bool        `json:"isreply"`
	EgressDeniedBy   []wirePolicy `json:"egressdeniedby"`
	IngressDeniedBy  []wirePolicy `json:"ingressdeniedby"`
}

type wireIP struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type wirePorts struct {
	SourcePort      uint32          `json:"sourceport"`
	DestinationPort uint32          `json:"destinationport"`
	Flags           map[string]bool `json:"flags"`
}

type wireL4 struct {
	TCP    *wirePorts `json:"tcp"`
	UDP    *wirePorts `json:"udp"`
	SCTP   *wirePorts `json:"sctp"`
	ICMPv4 *struct{}  `json:"icmpv4"`
	ICMPv6 *struct{}  `json:"icmpv6"`
}

type wireL7 struct {
	DNS *struct {
		Query  string   `json:"query"`
		Qtypes []string `json:"qtypes"`
		Rcode  uint32   `json:"rcode"`
	} `json:"dns"`
	HTTP *struct {
		Method string `json:"method"`
		URL    string `json:"url"`
		Code   uint32 `json:"code"`
	} `json:"http"`
}

type wireEndpoint struct {
	Identity  uint32   `json:"identity"`
	Namespace string   `json:"namespace"`
	Labels    []string `json:"labels"`
	PodName   string   `json:"podname"`
	Workloads []struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
	} `json:"workloads"`
}

type wirePolicy struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
}

// wireTime is a protobuf Timestamp: an RFC 3339 string, or {seconds, nanos}.
type wireTime int64

func (t *wireTime) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		if parsed, err := time.Parse(time.RFC3339Nano, s); err == nil {
			*t = wireTime(parsed.UnixMilli())
		}

		return nil
	}

	var ts struct {
		Seconds int64 `json:"seconds"`
		Nanos   int64 `json:"nanos"`
	}
	if json.Unmarshal(b, &ts) == nil {
		*t = wireTime(ts.Seconds*1000 + ts.Nanos/1e6)
	}

	return nil
}

// wireEnum is a protobuf enum: its name, or its number (kept as digits, see name).
type wireEnum string

func (e *wireEnum) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*e = wireEnum(s)

		return nil
	}

	var n json.Number
	if json.Unmarshal(b, &n) == nil {
		*e = wireEnum(n.String())
	}

	return nil
}

// name is the name of e, looked up in names when it came as a number.
func (e wireEnum) name(names map[int]string) string {
	n, err := strconv.Atoi(string(e))
	if err != nil {
		return string(e)
	}

	if name, ok := names[n]; ok {
		return name
	}

	if n == 0 {
		return ""
	}

	return string(e)
}

var (
	verdictNames   = map[int]string{1: "FORWARDED", 2: "DROPPED", 3: "ERROR", 4: "AUDIT", 5: "REDIRECTED", 6: "TRACED", 7: "TRANSLATED"}
	directionNames = map[int]string{1: "INGRESS", 2: "EGRESS"}
	flowTypeNames  = map[int]string{1: "L3_L4", 2: "L7", 3: "SOCK"}
	// dropReasonNames are the reasons the app explains, by number (Cilium's flow.DropReason).
	dropReasonNames = map[int]string{
		132: "INVALID_SOURCE_IP", 133: "POLICY_DENIED", 134: "INVALID_PACKET_DROPPED",
		137: "CT_UNKNOWN_L4_PROTOCOL", 139: "UNSUPPORTED_L3_PROTOCOL", 151: "STALE_OR_UNROUTABLE_IP",
		155: "CT_MAP_INSERTION_FAILED", 158: "SERVICE_BACKEND_NOT_FOUND", 160: "NO_TUNNEL_OR_ENCAPSULATION_ENDPOINT",
		164: "LOCAL_HOST_IS_UNREACHABLE", 165: "NO_CONFIGURATION_AVAILABLE_TO_PERFORM_POLICY_DECISION",
		169: "FIB_LOOKUP_FAILED", 171: "INVALID_IDENTITY", 172: "UNKNOWN_SENDER", 177: "DENIED_BY_LB_SRC_RANGE_CHECK",
		181: "POLICY_DENY", 189: "AUTH_REQUIRED", 194: "NO_EGRESS_GATEWAY", 195: "UNENCRYPTED_TRAFFIC",
		196: "TTL_EXCEEDED", 198: "DROP_RATE_LIMITED", 202: "DROP_HOST_NOT_READY", 203: "DROP_EP_NOT_READY",
	}
)

// hubbleLine is one parsed output line: a flow, a count of events Hubble lost, or neither.
type hubbleLine struct {
	flow *hubbleFlow
	lost uint64
}

// parseHubbleLine parses one `hubble observe -o jsonpb` line; ok is false for what is not
// JSON (a warning on stdout) or neither a flow nor lost events (node status).
func parseHubbleLine(line []byte) (hubbleLine, bool) {
	// encoding/json matches keys without regard to case, so the JSON names (dropReasonDesc)
	// decode straight into the lowercase tags. Proto names (drop_reason_desc) are normalised
	// first, a slower path: the response's node_name tells that output apart.
	if bytes.Contains(line, []byte(`"node_name"`)) {
		var raw any
		if err := json.Unmarshal(line, &raw); err != nil {
			return hubbleLine{}, false
		}

		normalised, err := json.Marshal(normaliseKeys(raw))
		if err != nil {
			return hubbleLine{}, false
		}

		line = normalised
	}

	var resp wireFlowResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return hubbleLine{}, false
	}

	switch {
	case resp.Flow != nil:
		f := resp.Flow.toFlow()
		if f.Node == "" {
			f.Node = resp.NodeName
		}

		if f.Time == 0 {
			f.Time = int64(resp.Time)
		}

		return hubbleLine{flow: &f}, true
	case resp.LostEvents != nil:
		return hubbleLine{lost: resp.LostEvents.NumEventsLost}, true
	default:
		return hubbleLine{}, false
	}
}

// normaliseKeys lowercases every object key and drops its underscores, so proto names
// (drop_reason_desc, IP, l4) and JSON names (dropReasonDesc) decode alike.
func normaliseKeys(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[strings.ToLower(strings.ReplaceAll(k, "_", ""))] = normaliseKeys(val)
		}

		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = normaliseKeys(val)
		}

		return out
	default:
		return v
	}
}

func (w *wireFlow) toFlow() hubbleFlow {
	f := hubbleFlow{
		Time:        int64(w.Time),
		Node:        w.NodeName,
		Verdict:     w.Verdict.name(verdictNames),
		Direction:   w.TrafficDirection.name(directionNames),
		Type:        w.Type.name(flowTypeNames),
		Source:      w.Source.toPeer(),
		Destination: w.Destination.toPeer(),
	}

	if f.Verdict == "DROPPED" || f.Verdict == "AUDIT" || f.Verdict == "ERROR" {
		f.Reason = w.DropReasonDesc.name(dropReasonNames)
		if f.Reason == "" && w.DropReason != 0 {
			f.Reason = wireEnum(strconv.Itoa(int(w.DropReason))).name(dropReasonNames)
		}
	}

	if w.IP != nil {
		f.Source.IP, f.Destination.IP = w.IP.Source, w.IP.Destination
	}

	f.Source.Names, f.Destination.Names = w.SourceNames, w.DestinationNames
	f.Protocol, f.Port, f.Flags = w.L4.describe()
	f.L7 = w.L7.describe()

	if w.IsReply != nil {
		f.Reply = *w.IsReply
	}

	for _, p := range append(w.IngressDeniedBy, w.EgressDeniedBy...) {
		f.DeniedBy = append(f.DeniedBy, policyRef{Kind: p.Kind, Namespace: p.Namespace, Name: p.Name})
	}

	return f
}

func (l wireL4) describe() (protocol string, port uint32, flags string) {
	switch {
	case l.TCP != nil:
		return "TCP", l.TCP.DestinationPort, hubbleTCPFlags(l.TCP.Flags)
	case l.UDP != nil:
		return "UDP", l.UDP.DestinationPort, ""
	case l.SCTP != nil:
		return "SCTP", l.SCTP.DestinationPort, ""
	case l.ICMPv4 != nil:
		return "ICMPv4", 0, ""
	case l.ICMPv6 != nil:
		return "ICMPv6", 0, ""
	default:
		return "", 0, ""
	}
}

// hubbleTCPFlags lists the set flags in the order tcpdump prints them.
func hubbleTCPFlags(flags map[string]bool) string {
	var out []string

	for _, name := range []string{"syn", "fin", "rst", "psh", "ack", "urg", "ece", "cwr", "ns"} {
		if flags[name] {
			out = append(out, strings.ToUpper(name))
		}
	}

	return strings.Join(out, ",")
}

func (l *wireL7) describe() string {
	switch {
	case l == nil:
		return ""
	case l.DNS != nil:
		return strings.TrimSpace("DNS " + l.DNS.Query + " " + strings.Join(l.DNS.Qtypes, ","))
	case l.HTTP != nil:
		s := "HTTP " + l.HTTP.Method + " " + l.HTTP.URL
		if l.HTTP.Code != 0 {
			s += " → " + strconv.Itoa(int(l.HTTP.Code))
		}

		return s
	default:
		return ""
	}
}

func (e wireEndpoint) toPeer() hubblePeer {
	p := hubblePeer{Namespace: e.Namespace, Pod: e.PodName, Identity: e.Identity, Labels: e.Labels}

	switch {
	case len(e.Workloads) > 0:
		p.Workload = e.Workloads[0].Name
	case e.PodName != "":
		p.Workload = podBaseName(e.PodName)
	}

	if e.PodName == "" {
		p.Reserved = reservedName(e.Labels, e.Identity)
	}

	return p
}

// reservedIdentities are Cilium's fixed identities, for a peer without labels.
var reservedIdentities = map[uint32]string{1: "host", 2: "world", 3: "unmanaged", 4: "health", 5: "init", 6: "remote-node", 7: "kube-apiserver", 8: "ingress", 9: "world-ipv4", 10: "world-ipv6"}

// reservedName is "world", "host"… for a peer that is not a pod.
func reservedName(labels []string, identity uint32) string {
	for _, l := range labels {
		if name, ok := strings.CutPrefix(l, "reserved:"); ok {
			return name
		}
	}

	return reservedIdentities[identity]
}
