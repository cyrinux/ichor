package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

// The fixtures follow MembershipStatusSpec's yaml tags (pkg/machinery/resources/etcd/
// membership_status.go at v1.15.0-beta.1); the phase is MembershipPhase's text and the member id
// etcd.FormatMemberID's zero-padded hex.
const (
	etcdLearnerYAML = `phase: learner
memberID: 00000000000000c3
`
	etcdFailedJoinYAML = `phase: joining
error: |
  error adding member:   etcdserver: too many learner
  members in cluster
`
	etcdMemberYAML = `phase: member
memberID: 00000000000000a1
`
)

func decodeMembershipFixture(t *testing.T, doc string) etcdMembership {
	t.Helper()

	spec := map[string]any{}
	if err := yaml.Unmarshal([]byte(doc), &spec); err != nil {
		t.Fatal(err)
	}

	return mapEtcdMembership(spec)
}

func TestMapEtcdMembership(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want etcdMembership
	}{
		{"learner", etcdLearnerYAML, etcdMembership{Phase: "learner", MemberID: "c3"}},
		{"failed join", etcdFailedJoinYAML, etcdMembership{Phase: "joining", Error: "error adding member: etcdserver: too many learner members in cluster"}},
		{"member", etcdMemberYAML, etcdMembership{Phase: "member", MemberID: "a1"}},
		{"empty", "{}", etcdMembership{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decodeMembershipFixture(t, tt.doc); got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMapEtcdMembershipClipsError(t *testing.T) {
	got := mapEtcdMembership(map[string]any{"phase": "joining", "error": strings.Repeat("x", 2000)})
	if len(got.Error) > etcdMembershipErrorMax+len("…") {
		t.Errorf("error is %d bytes, want at most %d", len(got.Error), etcdMembershipErrorMax)
	}
}

func TestWithEtcdMembership(t *testing.T) {
	overview := etcdOverview{
		Statuses: []etcdNodeStatus{
			{Node: "10.0.0.1", MemberID: "a1", IsLeader: true},
			{Node: "10.0.0.3", MemberID: "c3", IsLearner: true},
			// Etcd is not running yet on a joining node: it answers no status.
			{Node: "10.0.0.4", Error: "etcd is not running"},
			// A Talos 1.14 node: no membership status.
			{Node: "10.0.0.5", MemberID: "e5"},
		},
	}
	byNode := map[string]etcdMembership{
		"10.0.0.1": decodeMembershipFixture(t, etcdMemberYAML),
		"10.0.0.3": decodeMembershipFixture(t, etcdLearnerYAML),
		"10.0.0.4": decodeMembershipFixture(t, etcdFailedJoinYAML),
	}

	got := withEtcdMembership(overview, byNode)

	want := []struct{ phase, err, id string }{
		{"member", "", "a1"},
		{"learner", "", "c3"},
		{"joining", "error adding member: etcdserver: too many learner members in cluster", ""},
		{"", "", "e5"},
	}

	for i, w := range want {
		st := got.Statuses[i]
		if st.MembershipPhase != w.phase || st.MembershipError != w.err || st.MemberID != w.id {
			t.Errorf("%s: phase %q error %q id %q, want %q %q %q", st.Node, st.MembershipPhase, st.MembershipError, st.MemberID, w.phase, w.err, w.id)
		}
	}

	if overview.Statuses[1].MembershipPhase != "" {
		t.Error("the input overview was changed")
	}

	// On Talos 1.14 the JSON is the one the apps read before.
	raw, err := json.Marshal(got.Statuses[3])
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(raw), "membership") {
		t.Errorf("1.14 node JSON carries a membership field: %s", raw)
	}
}

func TestWithEtcdMembershipTakesMemberIDWhenEtcdHasNone(t *testing.T) {
	overview := etcdOverview{Statuses: []etcdNodeStatus{{Node: "10.0.0.3", Error: "etcd is not running"}}}

	got := withEtcdMembership(overview, map[string]etcdMembership{"10.0.0.3": {Phase: "learner", MemberID: "c3"}})
	if got.Statuses[0].MemberID != "c3" {
		t.Errorf("member id = %q, want c3", got.Statuses[0].MemberID)
	}
}

func TestMembershipDetail(t *testing.T) {
	byNode := map[string]etcdMembership{
		"10.0.0.1": {Phase: "member", MemberID: "a1"},
		"10.0.0.4": {Phase: "joining", Error: "too many learner members"},
		"10.0.0.3": {Phase: "learner", MemberID: "c3"},
	}

	if got, want := membershipDetail(byNode), "10.0.0.3: learner; 10.0.0.4: joining (too many learner members)"; got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}

	if got := withMembershipDetail("3 members, waiting for a new one", map[string]etcdMembership{"10.0.0.1": {Phase: "member"}}); got != "3 members, waiting for a new one" {
		t.Errorf("settled nodes changed the detail: %q", got)
	}

	got := withMembershipDetail("a new member is catching up as a learner", map[string]etcdMembership{"10.0.0.3": {Phase: "learner"}})
	if want := "a new member is catching up as a learner; etcd membership: 10.0.0.3: learner"; got != want {
		t.Errorf("detail = %q, want %q", got, want)
	}
}
