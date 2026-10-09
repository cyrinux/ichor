package ichorgo

import (
	"context"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

const whiskerFlowsPathTest = "/api/v1/namespaces/calico-system/services/whisker:8081/proxy/whisker-backend/flows"

func TestStreamWhiskerFeedsTheAggregate(t *testing.T) {
	fixture, err := os.ReadFile("testdata/calico/flows.sse")
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeKubeAPI(t, map[string]string{"GET " + whiskerFlowsPathTest: string(fixture)})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	w := whiskerInfo{Namespace: "calico-system", Pod: "whisker-1", Port: 8081}
	agg := newHubbleAgg(ciliumStatus{CNI: flowCNICalico, Agents: []ciliumAgent{{Node: whiskerNode, Pod: w.Pod}}})

	noAnswer, streamErr := streamWhisker(context.Background(), k, w, hubbleFilter{}, agg)
	if streamErr == nil || streamErr.Error() != "Whisker: goldmane unavailable" || noAnswer {
		t.Fatalf("stream: %v %v", streamErr, noAnswer)
	}

	s, _ := agg.snapshot(true)
	if s.Seen != 5 || s.Dropped != 3 || len(s.Drops) != 3 || len(s.Flows) != 5 {
		t.Fatalf("seen %d dropped %d drops %d flows %d", s.Seen, s.Dropped, len(s.Drops), len(s.Flows))
	}

	byKey := map[string]dropGroup{}
	for _, g := range s.Drops {
		byKey[peerKey(g.Source)+">"+peerKey(g.Destination)] = g
	}

	if g := byKey["flows/curl>flows/nginx"]; g.Reason != "POLICY_DENIED" || g.Count != 1 || len(g.DeniedBy) != 0 ||
		!reflect.DeepEqual(g.Isolating, []policyRef{{Kind: kindNetworkPolicy, Namespace: "flows", Name: "nginx-ingress"}}) || !reflect.DeepEqual(g.Nodes, []string{whiskerNode}) {
		t.Errorf("default-deny group: %+v", g)
	}

	if g := byKey["flows/curl>"]; g.Reason != "POLICY_DENY" || g.Destination.Reserved != "world" ||
		!reflect.DeepEqual(g.DeniedBy, []policyRef{{Kind: kindCalicoPolicy, Namespace: "flows", Name: "default.deny-world"}}) || len(g.Isolating) != 0 {
		t.Errorf("deny group: %+v", g)
	}

	if len(s.Nodes) != 1 || s.Nodes[0].Node != whiskerNode || s.Nodes[0].State != hubbleNodeLive || s.Nodes[0].Flows != 5 {
		t.Errorf("nodes: %+v", s.Nodes)
	}

	reqs := f.recorded()
	last := reqs[len(reqs)-1]
	if last.path != whiskerFlowsPathTest || !strings.Contains(last.query, "watch=true") || !strings.Contains(last.query, "startTimeGte=-60") {
		t.Errorf("request: %+v", last)
	}

	// Streaming again resumes at the last bucket seen (2026-10-08T08:08:30Z).
	_, _ = streamWhisker(context.Background(), k, w, hubbleFilter{}, agg)
	reqs = f.recorded()
	if q := reqs[len(reqs)-1].query; !strings.Contains(q, "startTimeGte=1791446910") {
		t.Errorf("resume: %s", q)
	}
}

func TestStreamWhiskerErrors(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{})
	f.answerWith("GET "+whiskerFlowsPathTest, http.StatusServiceUnavailable, `{"kind":"Status","reason":"ServiceUnavailable","message":"no endpoints available for service \"whisker:8081\""}`)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	w := whiskerInfo{Namespace: "calico-system", Pod: "whisker-1", Port: 8081}
	agg := newHubbleAgg(ciliumStatus{Agents: []ciliumAgent{{Node: whiskerNode, Pod: w.Pod}}})

	noAnswer, err := streamWhisker(context.Background(), k, w, hubbleFilter{}, agg)
	if !noAnswer || err == nil || !strings.Contains(err.Error(), "no answer from the whisker Service on port 8081") {
		t.Errorf("proxy: %v %v", err, noAnswer)
	}

	f.answerWith("GET "+whiskerFlowsPathTest, http.StatusBadRequest, `{"error":"failed to decode filters"}`)

	noAnswer, err = streamWhisker(context.Background(), k, w, hubbleFilter{}, agg)
	if noAnswer || err == nil || err.Error() != "the Whisker API answered HTTP 400: failed to decode filters" {
		t.Errorf("backend: %v %v", err, noAnswer)
	}

	f.answerWith("GET "+whiskerFlowsPathTest, http.StatusNotFound, `{"kind":"Status","reason":"NotFound","message":"services \"whisker\" not found"}`)

	noAnswer, err = streamWhisker(context.Background(), k, w, hubbleFilter{}, agg)
	if noAnswer || err == nil || !strings.Contains(err.Error(), "Whisker is not installed anymore") {
		t.Errorf("gone: %v %v", err, noAnswer)
	}
}

// A denied Calico flow whose trace names nothing the list shows falls back to the policies,
// matched on the aggregate's labels.
func TestWhiskerAggregateFallsBackToPolicies(t *testing.T) {
	agg := newHubbleAgg(ciliumStatus{CNI: flowCNICalico, Agents: []ciliumAgent{{Node: whiskerNode, Pod: "whisker-1"}}})

	var o npObject
	o.Metadata.Name, o.Metadata.Namespace = "nginx-ingress", "flows"
	o.Spec.PodSelector.MatchLabels = map[string]string{"app": "nginx"}
	o.Spec.PolicyTypes = []string{"Ingress"}
	agg.setPolicies([]netPolicy{mapNetworkPolicy(o)}, nil)

	f, _ := parseWhiskerFlow([]byte(`{"action":"Deny","source_name":"curl-85bfb6d759-*","source_namespace":"flows","source_labels":"app=curl","dest_name":"nginx-86644db9cc-*","dest_namespace":"flows","dest_labels":"app=nginx","protocol":"tcp","dest_port":80,"reporter":"Dst","policies":{"enforced":[{"kind":"Profile","name":"kns.flows","action":"Deny"}]}}`))
	agg.add(whiskerNode, hubbleLine{flow: &f})

	s, _ := agg.snapshot(true)
	if len(s.Drops) != 1 || !reflect.DeepEqual(s.Drops[0].Isolating, []policyRef{{Kind: kindNetworkPolicy, Namespace: "flows", Name: "nginx-ingress"}}) {
		t.Errorf("drops: %+v", s.Drops)
	}
}
