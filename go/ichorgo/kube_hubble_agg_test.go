package ichorgo

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func fixtureFlow(t *testing.T, i int, at int64) hubbleLine {
	t.Helper()

	line, ok := parseHubbleLine(readHubbleFixture(t)[i])
	if !ok {
		t.Fatalf("fixture line %d", i)
	}

	if line.flow != nil {
		line.flow.Time = at
	}

	return line
}

func TestHubbleAggGroupsDropsAndAttributes(t *testing.T) {
	agg := newHubbleAgg(ciliumStatus{Namespace: "kube-system", Agents: []ciliumAgent{{Node: "worker-1", Pod: "cilium-a"}, {Node: "worker-2", Pod: "cilium-b"}}})
	agg.setPolicies(readFixtureNetPolicies(t).Policies, nil)

	for i, at := range []int64{3000, 1000, 2000} {
		agg.add("worker-2", fixtureFlow(t, 0, at))

		if i == 0 {
			agg.add("worker-1", fixtureFlow(t, 1, 1500))
			agg.add("worker-2", fixtureFlow(t, 2, 1600))
			agg.add("worker-3", fixtureFlow(t, 3, 0)) // lost events
		}
	}

	s, ok := agg.snapshot(false)
	if !ok || s.Seen != 5 || s.Dropped != 4 || s.Lost != 17 || len(s.Flows) != 5 || len(s.Drops) != 2 {
		t.Fatalf("snapshot %+v", s)
	}

	db := s.Drops[0]
	if db.Destination.Pod != "db-0" || db.Count != 3 || db.FirstSeen != 1000 || db.LastSeen != 3000 ||
		!slices.Equal(db.Nodes, []string{"worker-2"}) || len(db.DeniedBy) != 0 {
		t.Fatalf("db group %+v", db)
	}

	if want := []policyRef{{Kind: kindNetworkPolicy, Namespace: "shop", Name: "db-allow-api"}}; !reflect.DeepEqual(db.Isolating, want) {
		t.Fatalf("db isolating %+v", db.Isolating)
	}

	dns := s.Drops[1]
	if dns.Reason != "POLICY_DENY" || len(dns.DeniedBy) != 1 || len(dns.Isolating) != 0 {
		t.Fatalf("dns group %+v", dns)
	}

	// Newest first is insertion order reversed: the ring holds arrival order.
	if s.Flows[0].Time != 2000 || s.Flows[4].Time != 3000 {
		t.Fatalf("flow order %d … %d", s.Flows[0].Time, s.Flows[4].Time)
	}

	nodes := map[string]hubbleNodeState{}
	for _, n := range s.Nodes {
		nodes[n.Node] = n
	}

	if nodes["worker-2"].State != hubbleNodeLive || nodes["worker-2"].Flows != 4 || nodes["worker-1"].Flows != 1 {
		t.Fatalf("nodes %+v", s.Nodes)
	}

	if _, changed := agg.snapshot(false); changed {
		t.Fatal("nothing changed since the last snapshot")
	}
}

func TestHubbleAggRingAndGroupCaps(t *testing.T) {
	agg := newHubbleAgg(ciliumStatus{})

	for i := range hubbleMaxFlows + 25 {
		line := fixtureFlow(t, 0, int64(i))
		line.flow.Destination.Workload = "db-" + string(rune('a'+i%26)) + string(rune('a'+i/26%26))
		agg.add("n", line)
	}

	s, _ := agg.snapshot(true)
	if len(s.Flows) != hubbleMaxFlows || s.Flows[0].Time != int64(hubbleMaxFlows+24) || s.Flows[hubbleMaxFlows-1].Time != 25 {
		t.Fatalf("%d flows, newest %d, oldest %d", len(s.Flows), s.Flows[0].Time, s.Flows[len(s.Flows)-1].Time)
	}

	if len(s.Drops) != hubbleMaxGroups || s.Drops[len(s.Drops)-1].LastSeen != 25 {
		t.Fatalf("%d groups, oldest %d", len(s.Drops), s.Drops[len(s.Drops)-1].LastSeen)
	}
}

func TestHubbleCommand(t *testing.T) {
	base := []string{"hubble", "observe", "--follow", "-o", "jsonpb", "--last", hubbleBackfill}

	cases := []struct {
		filter hubbleFilter
		extra  []string
	}{
		{hubbleFilter{}, nil},
		{hubbleFilter{namespace: "shop"}, []string{"--namespace", "shop"}},
		{hubbleFilter{namespace: "shop", pod: "db-0", dropsOnly: true}, []string{"--pod", "shop/db-0", "--verdict", "DROPPED", "--verdict", "AUDIT"}},
	}

	for _, c := range cases {
		if got := hubbleCommand(c.filter, 0); !slices.Equal(got, append(slices.Clone(base), c.extra...)) {
			t.Errorf("%+v: %v", c.filter, got)
		}
	}

	// Following again resumes after the last flow instead of replaying the backfill.
	since := time.Date(2026, 10, 4, 10, 15, 2, 123e6, time.UTC).UnixMilli()
	want := []string{"hubble", "observe", "--follow", "-o", "jsonpb", "--since", "2026-10-04T10:15:02.124Z"}
	if got := hubbleCommand(hubbleFilter{}, since); !slices.Equal(got, want) {
		t.Errorf("since: %v", got)
	}

	for _, bad := range []hubbleFilter{{namespace: "Shop"}, {pod: "db-0"}, {namespace: "shop", pod: "--all"}} {
		if bad.validate() == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}
}

func TestFollowAgentFeedsTheAggregate(t *testing.T) {
	lines := readHubbleFixture(t)

	k, query := fakeExecServer(t,
		frame(execStdout, string(lines[0])+"\n"+string(lines[3])+"\n"),
		frame(execStatus, `{"status":"Success"}`),
	)

	agent := ciliumAgent{Node: "worker-2", Pod: "cilium-b"}
	agg := newHubbleAgg(ciliumStatus{Agents: []ciliumAgent{agent}})

	followAgent(context.Background(), k, "kube-system", agent, hubbleFilter{dropsOnly: true}, agg)

	s, _ := agg.snapshot(true)
	if s.Seen != 1 || s.Lost != 17 || s.Nodes[0].State != hubbleNodeError || s.Nodes[0].Error != "the flow stream ended" {
		t.Fatalf("snapshot %+v", s)
	}

	if q := *query; q.Get("container") != ciliumAgentContainer || !slices.Contains(q["command"], "--last") || !slices.Contains(q["command"], "--verdict") {
		t.Fatalf("query %v", q)
	}

	// Following again picks up after the flow already seen.
	followAgent(context.Background(), k, "kube-system", agent, hubbleFilter{}, agg)

	if q := *query; !slices.Contains(q["command"], "--since") || slices.Contains(q["command"], "--last") {
		t.Fatalf("second query %v", q)
	}
}

type recordingHubbleListener struct{ updates []string }

func (l *recordingHubbleListener) OnUpdate(js string) { l.updates = append(l.updates, js) }
func (l *recordingHubbleListener) OnDone(string)      {}

func TestHubbleListenerIsMasked(t *testing.T) {
	agg := newHubbleAgg(ciliumStatus{Agents: []ciliumAgent{{Node: "worker-2", Pod: "cilium-b"}}})
	agg.add("worker-2", fixtureFlow(t, 0, 1000))

	s, _ := agg.snapshot(true)

	js, err := toJSON(s)
	if err != nil {
		t.Fatal(err)
	}

	SetPrivacyMask(true, "shop")
	defer SetPrivacyMask(false, "")

	rec := &recordingHubbleListener{}
	maskedHubbleListener{rec}.OnUpdate(js)

	for _, secret := range []string{"10.244.1.23", "10.244.2.41", `"shop"`} {
		if strings.Contains(rec.updates[0], secret) {
			t.Errorf("%s leaks through the mask", secret)
		}
	}
}

func TestHubbleAggSyncAgents(t *testing.T) {
	agg := newHubbleAgg(ciliumStatus{Agents: []ciliumAgent{{Node: "a", Pod: "cilium-1"}, {Node: "b", Pod: "cilium-2"}}})
	agg.syncAgents([]ciliumAgent{{Node: "a", Pod: "cilium-9"}, {Node: "c", Pod: "cilium-3"}})

	s, _ := agg.snapshot(true)
	want := []hubbleNodeState{{Node: "a", Pod: "cilium-9", State: hubbleNodeConnecting}, {Node: "c", Pod: "cilium-3", State: hubbleNodeConnecting}}

	if !reflect.DeepEqual(s.Nodes, want) {
		t.Fatalf("nodes %+v", s.Nodes)
	}
}

func TestDemoHubbleAttributesDrops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	var first hubbleSnapshot

	err := runDemoHubble(ctx, hubbleFilter{}, func(s hubbleSnapshot) {
		if first.Seen == 0 {
			first = s
		}

		cancel()
	})
	if err != context.Canceled || first.Seen == 0 || len(first.Nodes) != 5 {
		t.Fatalf("err %v, snapshot %+v", err, first)
	}

	want := map[string]policyRef{
		"jellyfin-5f8c>immich-postgres": {Kind: kindNetworkPolicy, Namespace: "media", Name: "immich-postgres-ingress"},
		"zigbee2mqtt>":                  {Kind: kindNetworkPolicy, Namespace: "home", Name: "default-deny-egress"},
	}

	for _, g := range first.Drops {
		key := g.Source.Workload + ">" + g.Destination.Workload
		if ref, ok := want[key]; ok {
			if !slices.Contains(g.Isolating, ref) {
				t.Errorf("%s isolating %+v", key, g.Isolating)
			}

			delete(want, key)
		}
	}

	if len(want) > 0 {
		t.Fatalf("missing drop groups %v", want)
	}
}

func TestDemoFilterKeeps(t *testing.T) {
	f := hubbleFlow{Verdict: "FORWARDED", Source: hubblePeer{Namespace: "home", Pod: "a"}, Destination: hubblePeer{Namespace: "media", Pod: "b"}}

	if !(hubbleFilter{namespace: "media"}).keeps(f) || (hubbleFilter{namespace: "default"}).keeps(f) ||
		(hubbleFilter{dropsOnly: true}).keeps(f) || !(hubbleFilter{namespace: "home", pod: "a"}).keeps(f) {
		t.Fatal("unexpected filter result")
	}
}
