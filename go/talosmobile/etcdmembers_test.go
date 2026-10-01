package talosmobile

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func member(id uint64, host string, healthy bool) memberState {
	return memberState{id: id, hostname: host, address: host, healthy: healthy}
}

func TestComputeMemberPlan(t *testing.T) {
	three := func(h1, h2, h3 bool) []memberState {
		return []memberState{member(0xa1, "cp-1", h1), member(0xa2, "cp-2", h2), member(0xa3, "cp-3", h3)}
	}

	t.Run("removing a dead member of three", func(t *testing.T) {
		p := computeMemberPlan(memberPlanInput{memberID: "a3", members: three(true, true, false), leader: 0xa1})

		if !p.Found || p.Member.Hostname != "cp-3" || p.Member.ID != "a3" || p.Healthy || p.IsLeader {
			t.Errorf("plan = %+v", p)
		}

		if p.Members != 3 || p.MembersAfter != 2 || p.HealthyAfter != 2 || !p.QuorumAfter || len(p.Blockers) != 0 {
			t.Errorf("plan = %+v", p)
		}

		if !containsText(p.Warnings, "even number") {
			t.Errorf("warnings = %v", p.Warnings)
		}
	})

	t.Run("removing a healthy member while another is down loses quorum", func(t *testing.T) {
		p := computeMemberPlan(memberPlanInput{memberID: "a1", members: three(true, true, false), leader: 0xa1})

		if p.MembersAfter != 2 || p.HealthyAfter != 1 || p.QuorumAfter || !containsText(p.Blockers, "lose quorum") {
			t.Errorf("plan = %+v", p)
		}

		if !p.IsLeader || !containsText(p.Warnings, "leader") || !containsText(p.Warnings, "running") {
			t.Errorf("plan = %+v", p)
		}
	})

	t.Run("the only member is never removed", func(t *testing.T) {
		p := computeMemberPlan(memberPlanInput{memberID: "a1", members: []memberState{member(0xa1, "cp-1", true)}})

		if !containsText(p.Blockers, "only etcd member") || p.QuorumAfter || p.MembersAfter != 0 {
			t.Errorf("plan = %+v", p)
		}
	})

	t.Run("two members, removing the dead one", func(t *testing.T) {
		p := computeMemberPlan(memberPlanInput{memberID: "a2", members: []memberState{member(0xa1, "cp-1", true), member(0xa2, "cp-2", false)}})

		if len(p.Blockers) != 0 || !p.QuorumAfter || p.MembersAfter != 1 || !containsText(p.Warnings, "single member") {
			t.Errorf("plan = %+v", p)
		}
	})

	t.Run("unknown member and list failure", func(t *testing.T) {
		p := computeMemberPlan(memberPlanInput{memberID: "ffff", members: three(true, true, true)})
		if p.Found || !containsText(p.Blockers, "no member ffff") || p.MembersAfter != 3 {
			t.Errorf("plan = %+v", p)
		}

		p = computeMemberPlan(memberPlanInput{memberID: "a1", listErr: "timed out"})
		if !containsText(p.Blockers, "cannot list") || p.Warnings == nil {
			t.Errorf("plan = %+v", p)
		}
	})

	t.Run("a learner does not count for quorum", func(t *testing.T) {
		members := three(true, true, true)
		members = append(members, memberState{id: 0xa4, hostname: "cp-4", learner: true})

		p := computeMemberPlan(memberPlanInput{memberID: "a4", members: members})
		if !p.IsLearner || p.Members != 3 || p.MembersAfter != 3 || p.HealthyAfter != 3 || !p.QuorumAfter || len(p.Blockers) != 0 {
			t.Errorf("plan = %+v", p)
		}
	})

	if normalizeMemberID(" 0x00A1 ") != "a1" || normalizeMemberID("zz") != "zz" {
		t.Error("normalizeMemberID")
	}
}

// fakeEtcd is an in-memory etcd cluster behind the Talos API: it never talks to a node.
type fakeEtcd struct {
	members map[string]uint64 // node address -> member id
	down    map[string]bool   // nodes that do not answer
	leader  uint64
	removed []uint64
	forfeit []string
	// removeErr makes EtcdRemoveMemberByID fail.
	removeErr error
}

func ctxNode(ctx context.Context) string {
	md, _ := metadata.FromOutgoingContext(ctx)
	if v := md.Get("node"); len(v) > 0 {
		return v[0]
	}

	return ""
}

func (f *fakeEtcd) EtcdMemberList(ctx context.Context, _ *machineapi.EtcdMemberListRequest, _ ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error) {
	if f.down[ctxNode(ctx)] {
		return nil, status.Error(codes.Unavailable, "down")
	}

	var members []*machineapi.EtcdMember
	for node, id := range f.members {
		members = append(members, &machineapi.EtcdMember{Id: id, Hostname: "host-" + node, PeerUrls: []string{"https://" + node + ":2380"}})
	}

	return &machineapi.EtcdMemberListResponse{Messages: []*machineapi.EtcdMembers{{Members: members}}}, nil
}

func (f *fakeEtcd) EtcdStatus(ctx context.Context, _ ...grpc.CallOption) (*machineapi.EtcdStatusResponse, error) {
	node := ctxNode(ctx)

	id, ok := f.members[node]
	if !ok || f.down[node] {
		return nil, status.Error(codes.Unavailable, "down")
	}

	return &machineapi.EtcdStatusResponse{Messages: []*machineapi.EtcdStatus{{
		MemberStatus: &machineapi.EtcdMemberStatus{MemberId: id, Leader: f.leader},
	}}}, nil
}

func (f *fakeEtcd) EtcdRemoveMemberByID(_ context.Context, req *machineapi.EtcdRemoveMemberByIDRequest, _ ...grpc.CallOption) error {
	if f.removeErr != nil {
		return f.removeErr
	}

	f.removed = append(f.removed, req.GetMemberId())

	return nil
}

func (f *fakeEtcd) EtcdForfeitLeadership(ctx context.Context, _ *machineapi.EtcdForfeitLeadershipRequest, _ ...grpc.CallOption) (*machineapi.EtcdForfeitLeadershipResponse, error) {
	node := ctxNode(ctx)
	f.forfeit = append(f.forfeit, node)

	member := ""
	if f.members[node] == f.leader {
		member = "host-new-leader"
	}

	return &machineapi.EtcdForfeitLeadershipResponse{Messages: []*machineapi.EtcdForfeitLeadership{{Member: member}}}, nil
}

func threeMemberEtcd() *fakeEtcd {
	return &fakeEtcd{
		members: map[string]uint64{"10.0.0.1": 0xa1, "10.0.0.2": 0xa2, "10.0.0.3": 0xa3},
		down:    map[string]bool{},
		leader:  0xa1,
	}
}

func TestRemoveEtcdMemberFake(t *testing.T) {
	ctx := context.Background()

	t.Run("removes a dead member through a healthy node", func(t *testing.T) {
		f := threeMemberEtcd()
		f.down["10.0.0.3"] = true

		if err := removeEtcdMember(ctx, f, "10.0.0.1", "a3"); err != nil {
			t.Fatal(err)
		}

		if !slices.Equal(f.removed, []uint64{0xa3}) {
			t.Errorf("removed = %x", f.removed)
		}
	})

	t.Run("refuses the member of the node the call is sent to", func(t *testing.T) {
		f := threeMemberEtcd()

		err := removeEtcdMember(ctx, f, "10.0.0.2", "0xA2")
		if err == nil || !strings.Contains(err.Error(), "another control plane") || len(f.removed) != 0 {
			t.Errorf("err = %v, removed = %x", err, f.removed)
		}
	})

	t.Run("refuses when quorum would be lost", func(t *testing.T) {
		f := threeMemberEtcd()
		f.down["10.0.0.3"] = true

		err := removeEtcdMember(ctx, f, "10.0.0.1", "a2")
		if err == nil || !strings.Contains(err.Error(), "lose quorum") || len(f.removed) != 0 {
			t.Errorf("err = %v, removed = %x", err, f.removed)
		}
	})

	t.Run("refuses unknown and invalid ids, and the last member", func(t *testing.T) {
		f := threeMemberEtcd()

		for _, id := range []string{"beef", "", "xyz", "0"} {
			if err := removeEtcdMember(ctx, f, "10.0.0.1", id); err == nil {
				t.Errorf("id %q accepted", id)
			}
		}

		single := &fakeEtcd{members: map[string]uint64{"10.0.0.1": 0xa1}, down: map[string]bool{}}
		if err := removeEtcdMember(ctx, single, "10.0.0.1", "a1"); err == nil {
			t.Error("last member removed")
		}

		if len(f.removed)+len(single.removed) != 0 {
			t.Errorf("something was removed: %x %x", f.removed, single.removed)
		}
	})

	t.Run("refuses when the node does not answer", func(t *testing.T) {
		f := threeMemberEtcd()
		f.down["10.0.0.1"] = true

		if err := removeEtcdMember(ctx, f, "10.0.0.1", "a3"); err == nil || len(f.removed) != 0 {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("an old Talos without the API is reported as such", func(t *testing.T) {
		f := threeMemberEtcd()
		f.down["10.0.0.3"] = true
		f.removeErr = status.Error(codes.Unimplemented, "unknown method EtcdRemoveMemberByID")

		if err := removeEtcdMember(ctx, f, "10.0.0.1", "a3"); !isUnavailableAPI(err) {
			t.Errorf("err = %v", err)
		}

		f.removeErr = errors.New("etcdserver: unhealthy cluster")
		if err := removeEtcdMember(ctx, f, "10.0.0.1", "a3"); err == nil || !strings.Contains(err.Error(), "unhealthy cluster") {
			t.Errorf("err = %v", err)
		}
	})
}

func TestGatherMemberPlanFake(t *testing.T) {
	f := threeMemberEtcd()
	f.down["10.0.0.1"] = true

	// The first node is down: the list comes from the next one.
	in := gatherMemberPlan(context.Background(), f, []string{"10.0.0.1", "10.0.0.2"}, "A1")
	if in.listErr != "" || len(in.members) != 3 || in.leader != 0xa1 || in.memberID != "a1" {
		t.Fatalf("input = %+v", in)
	}

	p := computeMemberPlan(in)
	if !p.Found || p.Healthy || !p.IsLeader || p.Member.Hostname != "host-10.0.0.1" || p.HealthyAfter != 2 || !p.QuorumAfter {
		t.Errorf("plan = %+v", p)
	}

	f.down["10.0.0.2"] = true
	if in := gatherMemberPlan(context.Background(), f, []string{"10.0.0.1", "10.0.0.2"}, "a1"); in.listErr == "" {
		t.Errorf("input = %+v", in)
	}
}

func TestForfeitEtcdLeadershipFake(t *testing.T) {
	f := threeMemberEtcd()

	got, err := forfeitEtcdLeadership(context.Background(), f, "10.0.0.1")
	if err != nil || got != "host-new-leader" {
		t.Errorf("got %q, %v", got, err)
	}

	got, err = forfeitEtcdLeadership(context.Background(), f, "10.0.0.2")
	if err != nil || got != "" {
		t.Errorf("got %q, %v", got, err)
	}

	if !slices.Equal(f.forfeit, []string{"10.0.0.1", "10.0.0.2"}) {
		t.Errorf("calls = %v", f.forfeit)
	}
}
