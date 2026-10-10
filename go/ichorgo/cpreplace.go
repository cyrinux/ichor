package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Steps of a control-plane replacement, in order.
const (
	stepConfirmQuorum   = "confirmQuorum"
	stepRemoveMember    = "removeMember"
	stepResetOrPowerOff = "resetOrPowerOff"
	stepBootNewNode     = "bootNewNode"
	stepWaitMember      = "waitMember"
)

// Step states.
const (
	stepPending = "pending"
	stepReady   = "ready"
	stepDone    = "done"
	stepSkipped = "skipped"
	stepBlocked = "blocked"
)

// cpReplacePlan is what ControlPlaneReplacePlan returns: where the replacement of a failed
// control plane stands, computed from the live cluster so that reopening the screen resumes.
type cpReplacePlan struct {
	Member   cpReplaceMember `json:"member"`
	Quorum   cpReplaceQuorum `json:"quorum"`
	Leader   cpReplaceNode   `json:"leader"`
	Steps    []cpReplaceStep `json:"steps"`
	Template cpReplaceNode   `json:"template"` // a healthy control plane whose config the new node copies
}

type cpReplaceMember struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Node     string `json:"node"`
	Found    bool   `json:"found"` // false once removed from etcd
	Healthy  bool   `json:"healthy"`
	// Reachable tells whether the node's Talos API answers (it can be reset).
	Reachable bool `json:"reachable"`
}

type cpReplaceQuorum struct {
	Members      int  `json:"members"`      // voting members now
	Healthy      int  `json:"healthy"`      // healthy voting members now
	AfterRemoval int  `json:"afterRemoval"` // voting members once the member is removed
	HealthyAfter int  `json:"healthyAfter"`
	Safe         bool `json:"safe"` // the removal keeps quorum
}

type cpReplaceNode struct {
	ID       string `json:"id,omitempty"`
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
}

type cpReplaceStep struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Detail string `json:"detail"`
}

type cpReplaceInput struct {
	etcd memberPlanInput // memberID set
	// node is the member's node address: from the member list, else the caller's hint.
	node      string
	reachable bool
}

// ControlPlaneReplacePlan tells where replacing the failed etcd member memberID (hex, as in
// EtcdStatus) stands, read-only (os:reader): see cpReplacePlan for the JSON. node is the
// member's address as the app last saw it: once the member is removed from etcd the list
// no longer names its node, and the reset step needs it ("" when unknown). The steps call
// existing actions (EtcdRemoveMember, NodeReset, ControlPlaneReplaceWait), each with its
// own checks.
func ControlPlaneReplacePlan(configYAML, contextName, memberID, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoCPReplacePlan(configYAML, contextName, memberID)
	}

	return withSession(configYAML, contextName, planTimeout, func(ctx context.Context, s *session) (string, error) {
		cps, err := s.controlPlanes(ctx)
		if err != nil {
			return "", err
		}

		in := cpReplaceInput{etcd: gatherMemberPlan(ctx, s.client, cps, memberID), node: node}

		if m := in.target(); m != nil && m.address != "" {
			in.node = m.address
		}

		if in.node != "" {
			in.reachable = peerFromProbe(in.node, probeNode(ctx, s.client, in.node)).reachable
		}

		return toJSON(computeCPReplacePlan(in))
	})
}

// target is the member being replaced, nil once removed.
func (in cpReplaceInput) target() *memberState {
	for i, m := range in.etcd.members {
		if hexID(m.id) == in.etcd.memberID {
			return &in.etcd.members[i]
		}
	}

	return nil
}

// computeCPReplacePlan orders the replacement: confirm quorum, remove the member, reset the
// old node (or skip it when it no longer answers), boot the new one, wait for it to join.
func computeCPReplacePlan(in cpReplaceInput) cpReplacePlan {
	removal := computeMemberPlan(in.etcd)
	plan := cpReplacePlan{
		Member: cpReplaceMember{
			ID: in.etcd.memberID, Hostname: removal.Member.Hostname, Node: in.node,
			Found: removal.Found, Healthy: removal.Healthy, Reachable: in.reachable,
		},
		Quorum: cpReplaceQuorum{
			Members: removal.Members, AfterRemoval: removal.MembersAfter,
			HealthyAfter: removal.HealthyAfter, Safe: removal.QuorumAfter,
		},
	}

	for _, m := range in.etcd.members {
		if !m.learner && m.healthy {
			plan.Quorum.Healthy++
		}

		if in.etcd.leader != 0 && m.id == in.etcd.leader {
			plan.Leader = cpReplaceNode{ID: hexID(m.id), Node: m.address, Hostname: m.hostname}
		}
	}

	plan.Template = replacementTemplate(in.etcd)

	switch {
	case in.etcd.listErr != "":
		plan.Steps = blockedSteps("cannot list the etcd members: " + in.etcd.listErr)
	case removal.Found:
		plan.Steps = stepsBeforeRemoval(plan, removal)
	default:
		plan.Steps = stepsAfterRemoval(plan, addressOwner(in))
	}

	return plan
}

// addressOwner is the current member whose address is the old node's: a replacement that
// reused the old IP. Its node must not be reset as if it were the old one.
func addressOwner(in cpReplaceInput) *memberState {
	if in.node == "" {
		return nil
	}

	for i, m := range in.etcd.members {
		if m.address == in.node && hexID(m.id) != in.etcd.memberID {
			return &in.etcd.members[i]
		}
	}

	return nil
}

// replacementTemplate is the control plane whose machine config the new node copies: the
// leader when it is healthy, else the first healthy voting member, never the one replaced.
func replacementTemplate(in memberPlanInput) cpReplaceNode {
	var pick *memberState

	for i, m := range in.members {
		if hexID(m.id) == in.memberID || m.learner || !m.healthy || m.address == "" {
			continue
		}

		if pick == nil || m.id == in.leader {
			pick = &in.members[i]
		}
	}

	if pick == nil {
		return cpReplaceNode{}
	}

	return cpReplaceNode{ID: hexID(pick.id), Node: pick.address, Hostname: pick.hostname}
}

func blockedSteps(reason string) []cpReplaceStep {
	return []cpReplaceStep{
		{ID: stepConfirmQuorum, State: stepBlocked, Detail: reason},
		{ID: stepRemoveMember, State: stepPending},
		{ID: stepResetOrPowerOff, State: stepPending},
		{ID: stepBootNewNode, State: stepPending},
		{ID: stepWaitMember, State: stepPending},
	}
}

func stepsBeforeRemoval(plan cpReplacePlan, removal etcdMemberPlan) []cpReplaceStep {
	if removal.Healthy {
		// Never remove a running member by this path: it would fail to rejoin.
		return blockedSteps("this member is healthy: only a failed control plane is replaced here")
	}

	if len(removal.Blockers) > 0 {
		steps := blockedSteps(strings.Join(removal.Blockers, "; "))
		steps[1].State = stepBlocked

		return steps
	}

	quorum := fmt.Sprintf("%d of %d remaining members are healthy: etcd keeps quorum", removal.HealthyAfter, removal.MembersAfter)

	remove := cpReplaceStep{ID: stepRemoveMember, State: stepReady, Detail: "sent through " + plan.Template.Hostname}
	if plan.Template.Node == "" {
		remove.State, remove.Detail = stepBlocked, "no healthy member to send the removal through"
	}

	return []cpReplaceStep{
		{ID: stepConfirmQuorum, State: stepDone, Detail: quorum},
		remove,
		resetStep(plan, stepPending),
		{ID: stepBootNewNode, State: stepPending},
		{ID: stepWaitMember, State: stepPending},
	}
}

func stepsAfterRemoval(plan cpReplacePlan, owner *memberState) []cpReplaceStep {
	reset := resetStep(plan, stepReady)
	if owner != nil {
		reset = cpReplaceStep{ID: stepResetOrPowerOff, State: stepSkipped, Detail: fmt.Sprintf(
			"%s now belongs to etcd member %s (%s): never reset it from here", plan.Member.Node, hexID(owner.id), owner.hostname)}
	}

	next := stepReady

	if reset.State == stepReady {
		next = stepPending // the old node still runs: reset it first
	}

	boot := cpReplaceStep{ID: stepBootNewNode, State: next}
	if plan.Template.Node == "" {
		boot.State, boot.Detail = stepBlocked, "no healthy control plane to copy the machine config from"
	}

	return []cpReplaceStep{
		{ID: stepConfirmQuorum, State: stepDone},
		{ID: stepRemoveMember, State: stepDone, Detail: "the member is no longer in the etcd member list"},
		reset,
		boot,
		{ID: stepWaitMember, State: boot.State},
	}
}

// resetStep resets the old node when its Talos API still answers; otherwise the user powers
// it off by hand (or it is already gone), and the step is skipped.
func resetStep(plan cpReplacePlan, whenReachable string) cpReplaceStep {
	switch {
	case plan.Member.Node == "":
		return cpReplaceStep{ID: stepResetOrPowerOff, State: stepSkipped, Detail: "the old node's address is unknown: power it off yourself"}
	case !plan.Member.Reachable:
		return cpReplaceStep{ID: stepResetOrPowerOff, State: stepSkipped, Detail: plan.Member.Node + " does not answer (powered off, or reset into maintenance mode): power it off yourself if it still runs"}
	default:
		return cpReplaceStep{ID: stepResetOrPowerOff, State: whenReachable, Detail: plan.Member.Node + " still answers: reset it so it cannot rejoin with stale etcd data"}
	}
}

// ControlPlaneReplaceRemove removes the failed etcd member memberID as step 2 of the
// replacement (os:admin): it reads the plan again and refuses unless the step is ready (the
// member is still failed and its removal keeps quorum), then sends the removal through the
// plan's template, a healthy control plane. Audited like EtcdRemoveMember.
func ControlPlaneReplaceRemove(configYAML, contextName, memberID string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)

	defer recordAction(&err, configYAML, contextName, auditAction{Action: "etcd-remove-member", Params: "member=" + memberID + ",replace=true"})

	if isDemoContext(configYAML, contextName) {
		return errDemoUnavailable
	}

	_, err = withSession(configYAML, contextName, planTimeout, func(ctx context.Context, s *session) (struct{}, error) {
		cps, err := s.controlPlanes(ctx)
		if err != nil {
			return struct{}{}, err
		}

		plan := computeCPReplacePlan(cpReplaceInput{etcd: gatherMemberPlan(ctx, s.client, cps, memberID)})
		if err := replaceRemovalAllowed(plan); err != nil {
			return struct{}{}, err
		}

		if err := removeEtcdMember(ctx, s.client, plan.Template.Node, memberID); err != nil {
			return struct{}{}, s.friendlyErr(plan.Template.Node, err)
		}

		return struct{}{}, nil
	})

	return err
}

// replaceRemovalAllowed refuses a removal the fresh plan does not mark ready: a member that
// recovered, a quorum loss, no healthy member to send it through.
func replaceRemovalAllowed(plan cpReplacePlan) error {
	for _, st := range plan.Steps {
		if st.ID != stepRemoveMember {
			continue
		}

		if st.State == stepReady && plan.Template.Node != "" {
			return nil
		}

		reason := st.Detail
		if blocked := plan.Steps[0]; blocked.State == stepBlocked {
			reason = blocked.Detail
		}

		if reason == "" {
			reason = "the step is " + st.State
		}

		return errors.New("removal refused: " + reason)
	}

	return errors.New("removal refused: no removal step")
}

// cpReplaceWait is what ControlPlaneReplaceWait returns.
type cpReplaceWait struct {
	Joined  bool              `json:"joined"`
	Members []cpReplaceMember `json:"members"`
	Detail  string            `json:"detail,omitempty"`
}

const (
	cpReplacePoll       = 5 * time.Second
	cpReplaceMaxTimeout = 15 * time.Minute
)

// ControlPlaneReplaceWait polls the etcd members (read-only, os:reader) until the cluster has
// more voting members than memberCountBefore, all of them healthy: the new control plane
// joined. At timeoutSec (1 s to 15 min) it returns {"joined": false} with the members seen;
// the apps call it again while their screen stays open.
func ControlPlaneReplaceWait(configYAML, contextName string, memberCountBefore, timeoutSec int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	timeout := min(max(time.Duration(timeoutSec)*time.Second, time.Second), cpReplaceMaxTimeout)

	return withSession(configYAML, contextName, timeout+planTimeout, func(ctx context.Context, s *session) (string, error) {
		deadline := time.Now().Add(timeout)

		for {
			result := cpReplaceWait{Detail: "no control plane answered"}

			if cps, err := s.controlPlanes(ctx); err == nil {
				result = joinState(gatherMemberPlan(ctx, s.client, cps, ""), memberCountBefore)
			}

			if result.Joined || !time.Now().Add(cpReplacePoll).Before(deadline) {
				return toJSON(result)
			}

			select {
			case <-ctx.Done():
				return toJSON(result)
			case <-time.After(cpReplacePoll):
			}
		}
	})
}

// joinState: joined once there are more voting members than before and all are healthy (a
// new member first joins as a learner, then is promoted).
func joinState(in memberPlanInput, before int) cpReplaceWait {
	if in.listErr != "" {
		return cpReplaceWait{Members: []cpReplaceMember{}, Detail: "cannot list the etcd members: " + in.listErr}
	}

	result := cpReplaceWait{Members: make([]cpReplaceMember, 0, len(in.members))}

	voting, healthy, learners := 0, 0, 0

	for _, m := range in.members {
		result.Members = append(result.Members, cpReplaceMember{ID: hexID(m.id), Hostname: m.hostname, Node: m.address, Found: true, Healthy: m.healthy})

		switch {
		case m.learner:
			learners++
		case m.healthy:
			voting++
			healthy++
		default:
			voting++
		}
	}

	switch {
	case voting > before && healthy == voting:
		result.Joined = true
	case learners > 0:
		result.Detail = "a new member is catching up as a learner"
	case voting > before:
		result.Detail = fmt.Sprintf("a new member joined; %d of %d members are healthy", healthy, voting)
	default:
		result.Detail = fmt.Sprintf("%d members, waiting for a new one", voting)
	}

	return result
}
