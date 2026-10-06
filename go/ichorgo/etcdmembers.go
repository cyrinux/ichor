package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc"
)

// etcdReader is the read-only part of the Talos etcd API (implemented by *client.Client).
type etcdReader interface {
	EtcdMemberList(ctx context.Context, req *machineapi.EtcdMemberListRequest, opts ...grpc.CallOption) (*machineapi.EtcdMemberListResponse, error)
	EtcdStatus(ctx context.Context, opts ...grpc.CallOption) (*machineapi.EtcdStatusResponse, error)
}

// etcdAdmin adds the membership changes; tests run them against a fake, never a cluster.
type etcdAdmin interface {
	etcdReader
	EtcdRemoveMemberByID(ctx context.Context, req *machineapi.EtcdRemoveMemberByIDRequest, opts ...grpc.CallOption) error
	EtcdForfeitLeadership(ctx context.Context, req *machineapi.EtcdForfeitLeadershipRequest, opts ...grpc.CallOption) (*machineapi.EtcdForfeitLeadershipResponse, error)
}

type etcdMemberPlan struct {
	Member       planMember `json:"member"`
	Found        bool       `json:"found"`
	IsLeader     bool       `json:"isLeader"`
	IsLearner    bool       `json:"isLearner"`
	Healthy      bool       `json:"healthy"`      // the member answers now
	Members      int        `json:"members"`      // voting members now
	MembersAfter int        `json:"membersAfter"` // voting members once removed
	HealthyAfter int        `json:"healthyAfter"` // healthy voting members once removed
	QuorumAfter  bool       `json:"quorumAfter"`
	Blockers     []string   `json:"blockers"`
	Warnings     []string   `json:"warnings"`
}

type planMember struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
}

// memberState is one etcd member as the plan sees it.
type memberState struct {
	id       uint64
	hostname string
	address  string
	learner  bool
	healthy  bool
}

type memberPlanInput struct {
	memberID string
	members  []memberState
	leader   uint64
	listErr  string
}

// EtcdMemberPlan tells what removing the etcd member memberID (hex, as in EtcdStatus) would
// leave, read-only (os:reader): see etcdMemberPlan for the JSON. Blockers are hard stops
// that EtcdRemoveMember refuses too.
func EtcdMemberPlan(configYAML, contextName, memberID string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return withSession(configYAML, contextName, planTimeout, func(ctx context.Context, s *session) (string, error) {
		cps, err := s.controlPlanes(ctx)
		if err != nil {
			return "", err
		}

		return toJSON(computeMemberPlan(gatherMemberPlan(ctx, s.client, cps, memberID)))
	})
}

// gatherMemberPlan lists the members through the first node of via that answers and asks
// each member for its status through its own address.
func gatherMemberPlan(ctx context.Context, c etcdReader, via []string, memberID string) memberPlanInput {
	in := memberPlanInput{memberID: normalizeMemberID(memberID)}

	members, listErr := firstAnswer(via, func(n string) ([]*machineapi.EtcdMember, error) { return etcdMembers(ctx, c, n) })
	if listErr != nil {
		in.listErr = friendlyError(listErr)

		return in
	}

	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	in.members = make([]memberState, len(members))

	var ask []int // the members with an address to ask

	for i, m := range members {
		in.members[i] = memberState{id: m.GetId(), hostname: m.GetHostname(), address: memberAddress(m), learner: m.GetIsLearner()}

		if in.members[i].address != "" {
			ask = append(ask, i)
		}
	}

	var mu sync.Mutex

	forEachNode(ask, func(_, i int) {
		st, healthy := memberHealthy(ctx, c, in.members[i].address)
		if !healthy {
			return
		}

		mu.Lock()
		defer mu.Unlock()

		in.members[i].healthy = true

		if in.leader == 0 {
			in.leader = st.GetLeader()
		}
	})

	return in
}

// computeMemberPlan applies the removal rules: never the last member, never a removal that
// leaves fewer healthy voting members than the new quorum.
func computeMemberPlan(in memberPlanInput) etcdMemberPlan {
	plan := etcdMemberPlan{Member: planMember{ID: in.memberID}, Blockers: []string{}, Warnings: []string{}}

	if in.listErr != "" {
		plan.Blockers = append(plan.Blockers, "cannot list the etcd members: "+in.listErr)

		return plan
	}

	var (
		target  *memberState
		healthy int
	)

	for i, m := range in.members {
		if hexID(m.id) == in.memberID {
			target = &in.members[i]
		}

		if m.learner {
			continue
		}

		plan.Members++

		if m.healthy {
			healthy++
		}
	}

	plan.MembersAfter, plan.HealthyAfter = plan.Members, healthy

	if target == nil {
		plan.QuorumAfter = healthy >= plan.Members/2+1
		plan.Blockers = append(plan.Blockers, "etcd has no member "+in.memberID)

		return plan
	}

	plan.Found = true
	plan.Member.Hostname = target.hostname
	plan.IsLearner, plan.Healthy = target.learner, target.healthy
	plan.IsLeader = in.leader != 0 && in.leader == target.id

	if !target.learner {
		plan.MembersAfter--

		if target.healthy {
			plan.HealthyAfter--
		}
	}

	quorum := plan.MembersAfter/2 + 1
	plan.QuorumAfter = plan.MembersAfter > 0 && plan.HealthyAfter >= quorum

	switch {
	case len(in.members) <= 1:
		plan.Blockers = append(plan.Blockers, "it is the only etcd member: removing it would destroy the cluster")
	case !plan.QuorumAfter:
		plan.Blockers = append(plan.Blockers, fmt.Sprintf(
			"etcd would lose quorum: %d of %d members would be healthy, %d needed", max(plan.HealthyAfter, 0), plan.MembersAfter, quorum))
	}

	if plan.IsLeader {
		plan.Warnings = append(plan.Warnings, "it is the current leader: etcd elects a new one (forfeit leadership first for a smooth handover)")
	}

	if target.healthy {
		plan.Warnings = append(plan.Warnings,
			"this member is running: its node keeps etcd data and fails to rejoin until it is reset (prefer resetting the node, which leaves etcd cleanly)")
	}

	if plan.QuorumAfter && !target.learner {
		switch {
		case plan.MembersAfter == 1:
			plan.Warnings = append(plan.Warnings, "a single member would remain: no fault tolerance")
		case plan.MembersAfter%2 == 0:
			plan.Warnings = append(plan.Warnings, fmt.Sprintf(
				"%d members would remain: an even number tolerates no more failures than %d", plan.MembersAfter, plan.MembersAfter-1))
		}
	}

	return plan
}

// normalizeMemberID accepts the hex id with or without 0x and leading zeros.
func normalizeMemberID(id string) string {
	id = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "0x")

	if n, err := strconv.ParseUint(id, 16, 64); err == nil {
		return hexID(n)
	}

	return id
}

// EtcdRemoveMember removes the etcd member memberID (hex, as in EtcdStatus) through node,
// like `talosctl -n NODE etcd remove-member ID` (os:admin). It is meant for members whose
// node is gone. It refuses to remove node's own member (the request must go through
// another, healthy control plane) and everything EtcdMemberPlan reports as a blocker: the
// last member, an unknown id, or a removal that loses quorum.
func EtcdRemoveMember(configYAML, contextName, node string, memberID string) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	_, err = withNodeSession(configYAML, contextName, node, planTimeout, func(ctx context.Context, s *session) (struct{}, error) {
		if err := removeEtcdMember(ctx, s.client, node, memberID); err != nil {
			if isUnavailableAPI(err) {
				return struct{}{}, s.friendlyErr(node, err)
			}

			return struct{}{}, err
		}

		return struct{}{}, nil
	})

	return err
}

func removeEtcdMember(ctx context.Context, c etcdAdmin, node, memberID string) error {
	id, err := strconv.ParseUint(normalizeMemberID(memberID), 16, 64)
	if err != nil || id == 0 {
		return fmt.Errorf("invalid etcd member id %q", memberID)
	}

	nodeCtx := client.WithNode(ctx, node)

	resp, err := c.EtcdStatus(nodeCtx)
	if err != nil {
		if isUnavailableAPI(err) {
			return err
		}

		return errors.New("cannot check which etcd member " + node + " is: " + friendlyError(err))
	}

	st := first(resp.GetMessages()).GetMemberStatus()
	if st == nil {
		return errors.New("cannot check which etcd member " + node + " is: no etcd status")
	}

	if st.GetMemberId() == id {
		return errors.New("refused: this is the etcd member of the node the request is sent to; send it through another control plane")
	}

	plan := computeMemberPlan(gatherMemberPlan(ctx, c, []string{node}, memberID))
	if len(plan.Blockers) > 0 {
		return errors.New("removal refused: " + strings.Join(plan.Blockers, "; "))
	}

	if err := c.EtcdRemoveMemberByID(nodeCtx, &machineapi.EtcdRemoveMemberByIDRequest{MemberId: id}); err != nil {
		if isUnavailableAPI(err) {
			return err
		}

		return errors.New("remove member failed: " + friendlyError(err))
	}

	return nil
}

type forfeitResult struct {
	Member string `json:"member"` // the new leader, "" when node was not the leader
}

// EtcdForfeitLeadership makes node hand etcd leadership over to another member, like
// `talosctl -n NODE etcd forfeit-leadership` (os:admin). It returns {"member": the new
// leader}, with "" when node was not the leader (nothing changed).
func EtcdForfeitLeadership(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		member, err := forfeitEtcdLeadership(ctx, s.client, node)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		if member != "" {
			privacy.learnHost(member, "controlplane")
		}

		return toJSON(forfeitResult{Member: member})
	})
}

func forfeitEtcdLeadership(ctx context.Context, c etcdAdmin, node string) (string, error) {
	resp, err := c.EtcdForfeitLeadership(client.WithNode(ctx, node), &machineapi.EtcdForfeitLeadershipRequest{})
	if err != nil {
		return "", err
	}

	return first(resp.GetMessages()).GetMember(), nil
}
