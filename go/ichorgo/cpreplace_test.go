package ichorgo

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func stepStates(plan cpReplacePlan) []string {
	out := make([]string, len(plan.Steps))
	for i, s := range plan.Steps {
		out[i] = s.ID + "=" + s.State
	}

	return out
}

func wantSteps(t *testing.T, plan cpReplacePlan, out string, states ...string) {
	t.Helper()

	ids := []string{stepConfirmQuorum, stepRemoveMember, stepResetOrPowerOff, stepBootNewNode, stepWaitMember}
	want := make([]string, len(ids))

	for i, id := range ids {
		want[i] = id + "=" + states[i]
	}

	if got := stepStates(plan); !slices.Equal(got, want) {
		t.Fatalf("steps = %v, want %v\nplan = %s", got, want, out)
	}
}

func TestControlPlaneReplaceUnreachableMember(t *testing.T) {
	_, cfg := threeControlPlanes(t, false) // 192.0.2.53 (a3) is down

	out, err := ControlPlaneReplacePlan(cfg, "fake", "a3", "")
	plan := decodeJSON[cpReplacePlan](t, out, err)

	if !plan.Member.Found || plan.Member.Healthy || plan.Member.Reachable || plan.Member.Node != "192.0.2.53" {
		t.Fatalf("member = %+v", plan.Member)
	}

	q := plan.Quorum
	if q.Members != 3 || q.Healthy != 2 || q.AfterRemoval != 2 || q.HealthyAfter != 2 || !q.Safe {
		t.Fatalf("quorum = %+v", q)
	}

	if plan.Template.Node != "192.0.2.51" || plan.Leader.ID != hexID(0xa1) {
		t.Fatalf("template = %+v, leader = %+v", plan.Template, plan.Leader)
	}

	wantSteps(t, plan, out, stepDone, stepReady, stepSkipped, stepPending, stepPending)
}

func TestControlPlaneReplaceReachableMember(t *testing.T) {
	f, cfg := threeControlPlanes(t, true)
	f.etcdErrors = map[string][]string{"192.0.2.53": {"etcdserver: no leader"}}

	out, err := ControlPlaneReplacePlan(cfg, "fake", "0x00a3", "")
	plan := decodeJSON[cpReplacePlan](t, out, err)

	if plan.Member.Healthy || !plan.Member.Reachable {
		t.Fatalf("member = %+v", plan.Member)
	}

	wantSteps(t, plan, out, stepDone, stepReady, stepPending, stepPending, stepPending)
}

func TestControlPlaneReplaceQuorumUnsafeBlocksRemoval(t *testing.T) {
	f, cfg := threeControlPlanes(t, false)
	f.etcdErrors = map[string][]string{"192.0.2.52": {"etcdserver: no leader"}}

	out, err := ControlPlaneReplacePlan(cfg, "fake", "a3", "")
	plan := decodeJSON[cpReplacePlan](t, out, err)

	if plan.Quorum.Safe {
		t.Fatalf("quorum = %+v", plan.Quorum)
	}

	wantSteps(t, plan, out, stepBlocked, stepBlocked, stepPending, stepPending, stepPending)

	if !strings.Contains(plan.Steps[0].Detail, "lose quorum") {
		t.Errorf("detail = %q", plan.Steps[0].Detail)
	}
}

func TestControlPlaneReplaceRefusesHealthyMember(t *testing.T) {
	_, cfg := threeControlPlanes(t, true)

	out, err := ControlPlaneReplacePlan(cfg, "fake", "a2", "")
	plan := decodeJSON[cpReplacePlan](t, out, err)

	wantSteps(t, plan, out, stepBlocked, stepPending, stepPending, stepPending, stepPending)

	if !strings.Contains(plan.Steps[0].Detail, "healthy") {
		t.Errorf("detail = %q", plan.Steps[0].Detail)
	}
}

func TestControlPlaneReplaceResumesAfterRemoval(t *testing.T) {
	for _, tc := range []struct {
		name   string
		up     bool
		states []string
	}{
		{"old node gone", false, []string{stepDone, stepDone, stepSkipped, stepReady, stepReady}},
		{"old node still answers", true, []string{stepDone, stepDone, stepReady, stepPending, stepPending}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, cfg := threeControlPlanes(t, tc.up)
			delete(f.members, "192.0.2.53")

			out, err := ControlPlaneReplacePlan(cfg, "fake", "a3", "192.0.2.53")
			plan := decodeJSON[cpReplacePlan](t, out, err)

			if plan.Member.Found || plan.Member.Node != "192.0.2.53" || plan.Member.Reachable != tc.up {
				t.Fatalf("member = %+v", plan.Member)
			}

			wantSteps(t, plan, out, tc.states...)
		})
	}
}

func TestControlPlaneReplaceWait(t *testing.T) {
	_, cfg := threeControlPlanes(t, true)

	out, err := ControlPlaneReplaceWait(cfg, "fake", 2, 30)
	joined := decodeJSON[cpReplaceWait](t, out, err)

	if !joined.Joined || len(joined.Members) != 3 {
		t.Fatalf("wait = %s", out)
	}

	out, err = ControlPlaneReplaceWait(cfg, "fake", 3, 1)
	waiting := decodeJSON[cpReplaceWait](t, out, err)

	if waiting.Joined || !strings.Contains(waiting.Detail, "waiting for a new one") {
		t.Fatalf("wait = %s", out)
	}
}

func TestJoinState(t *testing.T) {
	members := []memberState{
		{id: 0xa1, hostname: "cp-a", address: "192.0.2.51", healthy: true},
		{id: 0xa2, hostname: "cp-b", address: "192.0.2.52", healthy: true},
	}

	learner := append(slices.Clone(members), memberState{id: 0xa4, hostname: "cp-d", address: "192.0.2.54", learner: true, healthy: true})
	if got := joinState(memberPlanInput{members: learner}, 2); got.Joined || !strings.Contains(got.Detail, "learner") {
		t.Errorf("learner: %+v", got)
	}

	sick := append(slices.Clone(members), memberState{id: 0xa4, hostname: "cp-d", address: "192.0.2.54"})
	if got := joinState(memberPlanInput{members: sick}, 2); got.Joined || !strings.Contains(got.Detail, "2 of 3") {
		t.Errorf("unhealthy: %+v", got)
	}

	ready := append(slices.Clone(members), memberState{id: 0xa4, hostname: "cp-d", address: "192.0.2.54", healthy: true})
	if got := joinState(memberPlanInput{members: ready}, 2); !got.Joined {
		t.Errorf("joined: %+v", got)
	}

	if got := joinState(memberPlanInput{listErr: "timeout"}, 2); got.Joined || !strings.Contains(got.Detail, "timeout") {
		t.Errorf("list error: %+v", got)
	}
}

func TestControlPlaneReplaceDemo(t *testing.T) {
	yaml := demoConfigForTest(t)

	out, err := ControlPlaneReplacePlan(yaml, "", "a3", "")
	plan := decodeJSON[cpReplacePlan](t, out, err)

	if plan.Member.Hostname != "demo-cp-3" || plan.Template.Hostname != "demo-cp-1" {
		t.Fatalf("plan = %s", out)
	}

	wantSteps(t, plan, out, stepDone, stepReady, stepSkipped, stepPending, stepPending)

	if _, err := ControlPlaneReplacePlan(yaml, "", "ff", ""); err == nil {
		t.Error("unknown demo member: no error")
	}

	if _, err := ControlPlaneReplaceWait(yaml, "", 2, 1); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo wait = %v", err)
	}

	if err := EtcdRemoveMember(yaml, "", "192.0.2.10", "a3"); err == nil {
		t.Error("demo removal went through")
	}
}
