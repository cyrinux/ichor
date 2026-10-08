package ichorgo

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// readWhiskerFixture is the flows a Whisker (Calico 3.33) streamed, one data event each.
func readWhiskerFixture(t *testing.T) [][]byte {
	t.Helper()

	data, err := os.ReadFile("testdata/calico/flows.sse")
	if err != nil {
		t.Fatal(err)
	}

	var out [][]byte

	for _, line := range bytes.Split(data, []byte("\n")) {
		if payload, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			out = append(out, bytes.TrimSpace(payload))
		}
	}

	return out
}

func TestParseWhiskerFlow(t *testing.T) {
	events := readWhiskerFixture(t)
	if len(events) != 5 {
		t.Fatalf("%d events", len(events))
	}

	curl := hubblePeer{Namespace: "flows", Workload: "curl", Labels: []string{"k8s:app=curl", "k8s:pod-template-hash=85bfb6d759", "k8s:projectcalico.org/namespace=flows", "k8s:projectcalico.org/orchestrator=k8s"}}
	nginx := hubblePeer{Namespace: "flows", Workload: "nginx", Labels: []string{"k8s:app=nginx", "k8s:pod-template-hash=86644db9cc", "k8s:projectcalico.org/namespace=flows", "k8s:projectcalico.org/orchestrator=k8s"}}
	at := int64(1791446895000) // 2026-10-08T08:08:15Z

	want := []hubbleFlow{
		{Time: at, Verdict: "FORWARDED", Direction: "EGRESS", Protocol: "TCP", Port: 80, Type: "L3_L4", Source: curl, Destination: nginx, Packets: 6},
		{Time: at, Verdict: "DROPPED", Reason: "POLICY_DENIED", Direction: "INGRESS", Protocol: "TCP", Port: 80, Type: "L3_L4", Source: curl, Destination: nginx,
			Isolating: []policyRef{{Kind: kindNetworkPolicy, Namespace: "flows", Name: "nginx-ingress"}}, Packets: 6},
		{Time: at, Verdict: "DROPPED", Reason: "POLICY_DENY", Direction: "EGRESS", Protocol: "TCP", Port: 80, Type: "L3_L4", Source: curl, Destination: hubblePeer{Reserved: "world"},
			DeniedBy: []policyRef{{Kind: kindCalicoPolicy, Namespace: "flows", Name: "default.deny-world"}}, Packets: 6},
	}

	for i, w := range want {
		got, ok := parseWhiskerFlow(events[i])
		if !ok || !reflect.DeepEqual(got, w) {
			t.Errorf("event %d:\n got %+v\nwant %+v", i, got, w)
		}
	}

	// A host endpoint denied by a global policy of another tier: the policy keeps its tier prefix.
	host, _ := parseWhiskerFlow(events[3])
	if host.Source.Reserved != "private-network" || host.Destination.Reserved != "host" || host.Destination.Workload != "minikube" ||
		!reflect.DeepEqual(host.DeniedBy, []policyRef{{Kind: kindCalicoGlobalPolicy, Name: "security.host-fw"}}) || host.Packets != 3 {
		t.Errorf("host flow: %+v", host)
	}

	// An exact pod name, a profile and a staged policy: allowed, nothing named.
	db, _ := parseWhiskerFlow(events[4])
	if db.Source.Pod != "db-0" || db.Source.Workload != "db" || db.Destination.Workload != "kube-dns" || db.Verdict != "FORWARDED" ||
		db.Reason != "" || len(db.DeniedBy) != 0 || len(db.Isolating) != 0 || db.Protocol != "UDP" || db.Port != 53 {
		t.Errorf("db flow: %+v", db)
	}

	if _, ok := parseWhiskerFlow([]byte(`{"total":{"totalPages":0}}`)); ok {
		t.Error("a list envelope is not a flow")
	}
}

func TestWhiskerPolicyRef(t *testing.T) {
	tests := []struct {
		hit  whiskerPolicyHit
		want policyRef
		ok   bool
	}{
		{whiskerPolicyHit{Kind: "CalicoNetworkPolicy", Name: "deny-world", Namespace: "flows", Tier: "default"}, policyRef{Kind: kindCalicoPolicy, Namespace: "flows", Name: "default.deny-world"}, true},
		{whiskerPolicyHit{Kind: "CalicoNetworkPolicy", Name: "default.deny-world", Namespace: "flows", Tier: "default"}, policyRef{Kind: kindCalicoPolicy, Namespace: "flows", Name: "default.deny-world"}, true},
		{whiskerPolicyHit{Kind: "GlobalNetworkPolicy", Name: "allow-dns", Tier: "default"}, policyRef{Kind: kindCalicoGlobalPolicy, Name: "default.allow-dns"}, true},
		{whiskerPolicyHit{Kind: "NetworkPolicy", Name: "knp.default.web", Namespace: "shop"}, policyRef{Kind: kindNetworkPolicy, Namespace: "shop", Name: "web"}, true},
		{whiskerPolicyHit{Kind: "AdminNetworkPolicy", Name: "baseline"}, policyRef{Kind: "AdminNetworkPolicy", Name: "baseline"}, true},
		{whiskerPolicyHit{Kind: "Profile", Name: "kns.flows"}, policyRef{}, false},
		{whiskerPolicyHit{Kind: "StagedNetworkPolicy", Name: "default.x", Namespace: "flows"}, policyRef{}, false},
		{whiskerPolicyHit{Kind: "EndOfTier", Tier: "default"}, policyRef{}, false},
	}

	for _, tt := range tests {
		got, ok := whiskerPolicyRef(tt.hit)
		if ok != tt.ok || got != tt.want {
			t.Errorf("%+v: %+v %v", tt.hit, got, ok)
		}
	}
}

func TestWhiskerPeerNames(t *testing.T) {
	if p := whiskerPeer("web-0", "shop", "app=web", "WorkloadEndpoint"); p.Pod != "web-0" || p.Workload != "web" || p.Namespace != "shop" {
		t.Errorf("exact pod: %+v", p)
	}

	if p := whiskerPeer("nginx-86644db9cc-*", "shop", "", ""); p.Pod != "" || p.Workload != "nginx" || len(p.Labels) != 2 {
		t.Errorf("replica set: %+v", p)
	}

	if p := whiskerPeer("corp", "-", "", "NetworkSet"); p.Workload != "corp" || p.Namespace != "" || p.Reserved != "" {
		t.Errorf("network set: %+v", p)
	}

	if p := whiskerPeer(whiskerPublicNetwork, whiskerGlobal, "", "Network"); p.Reserved != "world" || p.Namespace != "" || len(p.Labels) != 0 {
		t.Errorf("public network: %+v", p)
	}
}

func TestReadWhiskerSSE(t *testing.T) {
	data, err := os.ReadFile("testdata/calico/flows.sse")
	if err != nil {
		t.Fatal(err)
	}

	var got int

	err = readWhiskerSSE(bytes.NewReader(data), func([]byte) { got++ })
	if got != 5 || err == nil || err.Error() != "Whisker: goldmane unavailable" {
		t.Errorf("%d events, %v", got, err)
	}

	if err := readWhiskerSSE(strings.NewReader("data: {}\n\n"), func([]byte) {}); err != nil {
		t.Errorf("a stream that ends: %v", err)
	}

	if err := readWhiskerSSE(errReader{}, func([]byte) {}); !errors.Is(err, errCut) {
		t.Errorf("a connection cut: %v", err)
	}
}

var errCut = errors.New("cut")

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errCut }

func TestWhiskerFlowsPath(t *testing.T) {
	w := whiskerInfo{Namespace: "calico-system", Pod: "whisker-1", Port: 8081}

	if got := whiskerFlowsPath(w, hubbleFilter{}, 0); got != "/api/v1/namespaces/calico-system/services/whisker:8081/proxy/whisker-backend/flows?startTimeGte=-60&watch=true" {
		t.Errorf("first: %s", got)
	}

	w.Port, w.TLS = 8443, true
	if got := whiskerFlowsPath(w, hubbleFilter{dropsOnly: true}, 1791446895000); got != `/api/v1/namespaces/calico-system/services/https:whisker:8443/proxy/whisker-backend/flows?filters=%7B%22actions%22%3A%5B%22Deny%22%5D%7D&startTimeGte=1791446895&watch=true` {
		t.Errorf("again: %s", got)
	}
}

func TestHubbleFilterKeepsAggregates(t *testing.T) {
	flow := hubbleFlow{Verdict: "FORWARDED", Source: hubblePeer{Namespace: "flows", Workload: "curl"}, Destination: hubblePeer{Namespace: "kube-system", Workload: "kube-dns"}}

	for pod, want := range map[string]bool{"curl-85bfb6d759-d6skp": true, "curl-0": true, "curl-other-x2k4q": false, "nginx-1": false} {
		if got := (hubbleFilter{namespace: "flows", pod: pod}).keeps(flow); got != want {
			t.Errorf("pod %s: %v", pod, got)
		}
	}

	if (hubbleFilter{namespace: "flows", pod: "curl-0", dropsOnly: true}).keeps(flow) {
		t.Error("drops only")
	}

	if !(hubbleFilter{namespace: "kube-system"}).keeps(flow) || (hubbleFilter{namespace: "shop"}).keeps(flow) {
		t.Error("namespace on either side")
	}
}
