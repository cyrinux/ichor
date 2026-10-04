package ichorgo

import (
	"bytes"
	"os"
	"reflect"
	"testing"
	"time"
)

func readHubbleFixture(t *testing.T) [][]byte {
	t.Helper()

	data, err := os.ReadFile("testdata/cilium/flows.jsonl")
	if err != nil {
		t.Fatal(err)
	}

	return bytes.Split(bytes.TrimSpace(data), []byte("\n"))
}

func TestParseHubbleLineProtoNames(t *testing.T) {
	lines := readHubbleFixture(t)

	got, ok := parseHubbleLine(lines[0])
	if !ok || got.flow == nil {
		t.Fatal("expected a flow")
	}

	want := hubbleFlow{
		Time:      time.Date(2026, 10, 4, 10, 15, 2, 123e6, time.UTC).UnixMilli(),
		Node:      "worker-2",
		Verdict:   "DROPPED",
		Reason:    "POLICY_DENIED",
		Direction: "INGRESS",
		Protocol:  "TCP",
		Port:      5432,
		Flags:     "SYN",
		Type:      "L3_L4",
		Source: hubblePeer{Namespace: "shop", Pod: "frontend-7d9c8b6f5-x2k4q", Workload: "frontend", Identity: 48211, IP: "10.244.1.23",
			Labels: []string{"k8s:app=frontend", "k8s:io.kubernetes.pod.namespace=shop", "k8s:io.cilium.k8s.policy.serviceaccount=frontend"}},
		Destination: hubblePeer{Namespace: "shop", Pod: "db-0", Workload: "db", Identity: 30577, IP: "10.244.2.41",
			Labels: []string{"k8s:app=db", "k8s:io.kubernetes.pod.namespace=shop"}},
	}

	if !reflect.DeepEqual(*got.flow, want) {
		t.Fatalf("got  %+v\nwant %+v", *got.flow, want)
	}
}

func TestParseHubbleLineJSONNamesAndNumbers(t *testing.T) {
	got, ok := parseHubbleLine(readHubbleFixture(t)[1])
	if !ok || got.flow == nil {
		t.Fatal("expected a flow")
	}

	f := *got.flow
	if f.Verdict != "DROPPED" || f.Reason != "POLICY_DENY" || f.Direction != "EGRESS" || f.Type != "L3_L4" ||
		f.Protocol != "UDP" || f.Port != 53 || f.Node != "worker-1" || f.Source.Pod != "batch-28761234-abcde" ||
		f.Source.Workload != "batch" || f.Destination.Reserved != "world" || f.Destination.IP != "10.96.0.10" ||
		!reflect.DeepEqual(f.Destination.Names, []string{"dns.example.org"}) {
		t.Fatalf("got %+v", f)
	}

	if want := []policyRef{{Kind: "CiliumNetworkPolicy", Namespace: "jobs", Name: "deny-external-dns"}}; !reflect.DeepEqual(f.DeniedBy, want) {
		t.Fatalf("deniedBy %+v", f.DeniedBy)
	}
}

func TestParseHubbleLineForwardedL7(t *testing.T) {
	got, _ := parseHubbleLine(readHubbleFixture(t)[2])

	f := *got.flow
	if f.Verdict != "FORWARDED" || f.Reason != "" || !f.Reply || f.Flags != "SYN,ACK" || f.L7 != "HTTP GET http://api.shop/cart → 200" {
		t.Fatalf("got %+v", f)
	}
}

func TestParseHubbleLineLostAndOthers(t *testing.T) {
	lines := readHubbleFixture(t)

	if got, ok := parseHubbleLine(lines[3]); !ok || got.flow != nil || got.lost != 17 {
		t.Fatalf("lost events: %+v %v", got, ok)
	}

	for _, i := range []int{4, 5} {
		if _, ok := parseHubbleLine(lines[i]); ok {
			t.Fatalf("line %d should be skipped", i)
		}
	}
}

func TestWireEnumName(t *testing.T) {
	cases := map[wireEnum]string{"DROPPED": "DROPPED", "2": "DROPPED", "0": "", "99": "99", "": ""}
	for in, want := range cases {
		if got := in.name(verdictNames); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestReservedName(t *testing.T) {
	if reservedName([]string{"reserved:host"}, 0) != "host" || reservedName(nil, 7) != "kube-apiserver" || reservedName(nil, 0) != "" {
		t.Fatal("unexpected reserved name")
	}
}
