package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
)

// resetPlan is what NodeResetPlan returns: what resetting the node would wipe and leave.
// Blockers are hard stops that NodeReset refuses too.
type resetPlan struct {
	Node             string       `json:"node"`
	Hostname         string       `json:"hostname"`
	Role             string       `json:"role"`       // controlplane | worker
	EtcdMember       *resetMember `json:"etcdMember"` // nil for a worker or a control plane outside etcd
	LastControlPlane bool         `json:"lastControlPlane"`
	Blockers         []string     `json:"blockers"`
	Warnings         []string     `json:"warnings"`
	UserDisks        []string     `json:"userDisks"` // the disks other than the system disk, as /dev paths
}

type resetMember struct {
	ID      string `json:"id"`
	Healthy bool   `json:"healthy"`
}

type resetPlanInput struct {
	target    planPeer
	others    []planPeer
	etcd      *memberPlanInput // control planes only; memberID unset
	endpoints []string
	lock      *lockState
	userDisks []string
	disksErr  string
}

// NodeResetPlan tells what resetting node would do, read-only (os:reader; os:admin to read the
// cluster upgrade lock): see resetPlan for the JSON. The last control plane, or one whose
// departure loses etcd quorum, cannot be reset. kubeServer: see KubePods.
func NodeResetPlan(configYAML, contextName, kubeServer, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeResetPlan", configYAML, contextName, node)
	}

	return withSession(configYAML, contextName, planTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		return toJSON(computeResetPlan(gatherResetPlan(ctx, s, node, kubeTarget{configYAML, contextName, kubeServer})))
	})
}

// NodeReset resets node like `talosctl reset --graceful=G --reboot=R --wipe-mode=M` (os:admin):
//   - wipe "all": the system disk and every user disk of the plan;
//   - wipe "system": the system disk only (Talos reinstalls on the next boot);
//   - wipe "user": the user disks of the plan only.
//
// graceful leaves etcd and drains the node first; reboot restarts it instead of powering off.
// It refuses everything NodeResetPlan reports as a blocker.
func NodeReset(configYAML, contextName, node, wipe string, graceful, reboot bool) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	defer recordAction(&err, configYAML, contextName, auditAction{
		Action: "reset", Node: node, Params: fmt.Sprintf("wipe=%s,graceful=%t,reboot=%t", wipe, graceful, reboot),
	})

	mode, err := parseWipeMode(wipe)
	if err != nil {
		return err
	}

	_, err = withSession(configYAML, contextName, planTimeout, func(ctx context.Context, s *session) (struct{}, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return struct{}{}, err
		}

		// The upgrade lock is only a warning of the plan: not read again here.
		plan := computeResetPlan(gatherResetPlan(ctx, s, node, kubeTarget{}))

		req, err := resetRequest(plan, mode, graceful, reboot)
		if err != nil {
			return struct{}{}, err
		}

		callCtx, cancel := context.WithTimeout(client.WithNode(ctx, node), callTimeout)
		defer cancel()

		if err := s.client.ResetGeneric(callCtx, req); err != nil {
			return struct{}{}, s.friendlyErr(node, err)
		}

		return struct{}{}, nil
	})

	return err
}

func parseWipeMode(wipe string) (machineapi.ResetRequest_WipeMode, error) {
	switch strings.ToLower(strings.TrimSpace(wipe)) {
	case "all":
		return machineapi.ResetRequest_ALL, nil
	case "system":
		return machineapi.ResetRequest_SYSTEM_DISK, nil
	case "user":
		return machineapi.ResetRequest_USER_DISKS, nil
	default:
		return 0, fmt.Errorf("unknown wipe mode %q (all, system, user)", wipe)
	}
}

// resetRequest builds the request for plan, or refuses it.
func resetRequest(plan resetPlan, mode machineapi.ResetRequest_WipeMode, graceful, reboot bool) (*machineapi.ResetRequest, error) {
	if len(plan.Blockers) > 0 {
		return nil, errors.New("reset refused: " + strings.Join(plan.Blockers, "; "))
	}

	req := &machineapi.ResetRequest{Graceful: graceful, Reboot: reboot, Mode: mode}

	switch mode {
	case machineapi.ResetRequest_USER_DISKS:
		if len(plan.UserDisks) == 0 {
			return nil, errors.New("reset refused: this node has no user disk to wipe")
		}

		req.UserDisksToWipe = plan.UserDisks
	case machineapi.ResetRequest_ALL:
		req.UserDisksToWipe = plan.UserDisks
	}

	return req, nil
}

// gatherResetPlan probes the context's nodes, the etcd members (for a control plane), the
// node's disks and the cluster upgrade lock (skipped for a zero kube).
func gatherResetPlan(ctx context.Context, s *session, node string, kube kubeTarget) resetPlanInput {
	nodes := targetNodes(s.context)
	peers := make([]planPeer, len(nodes))

	forEachNode(nodes, func(i int, n string) { peers[i] = peerFromProbe(n, probeNode(ctx, s.client, n)) })

	in := resetPlanInput{endpoints: s.context.Endpoints}

	for _, p := range peers {
		if p.node == node {
			in.target = p
		} else {
			in.others = append(in.others, p)
		}
	}

	if !in.target.reachable {
		return in
	}

	if in.target.controlPlane {
		via := []string{node}
		for _, p := range in.others {
			if p.controlPlane && p.reachable {
				via = append(via, p.node)
			}
		}

		etcd := gatherMemberPlan(ctx, s.client, via, "")
		in.etcd = &etcd
	}

	disks, err := nodeUserDisks(client.WithNode(ctx, node), s)
	if err != nil {
		in.disksErr = s.friendly(node, err)
	}

	in.userDisks = disks
	in.lock = readUpgradeLock(ctx, kube, time.Now())

	return in
}

// nodeUserDisks lists the node's writable disks other than the system disk, through the
// Disks API or, on Talos versions without it, the block.Disk and block.SystemDisk resources.
func nodeUserDisks(ctx context.Context, s *session) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	var out []string

	if resp, err := s.client.Disks(ctx); err == nil {
		for _, d := range mapAPIDisks(first(resp.GetMessages()).GetDisks()) {
			if !d.SystemDisk && !d.Readonly {
				out = append(out, d.DevPath)
			}
		}

		return out, nil
	}

	disks, err := safe.StateListAll[*block.Disk](ctx, s.client.COSI)
	if err != nil {
		return nil, err
	}

	system, err := safe.StateGetByID[*block.SystemDisk](ctx, s.client.COSI, block.SystemDiskID)
	if err != nil {
		return nil, fmt.Errorf("cannot tell the system disk: %w", err)
	}

	for _, d := range mapBlockDisks(safe.ToSlice(disks, identity)) {
		if d.Name != system.TypedSpec().DiskID && d.DevPath != system.TypedSpec().DevPath && !d.Readonly {
			out = append(out, d.DevPath)
		}
	}

	return out, nil
}

// computeResetPlan applies the reset rules: never the last control plane, never a control
// plane whose departure loses etcd quorum.
func computeResetPlan(in resetPlanInput) resetPlan {
	t := in.target
	plan := resetPlan{
		Node: t.node, Hostname: t.hostname, Role: "worker",
		Blockers: []string{}, Warnings: []string{}, UserDisks: in.userDisks,
	}

	if plan.UserDisks == nil {
		plan.UserDisks = []string{}
	}

	if t.controlPlane {
		plan.Role = "controlplane"
	}

	if !t.reachable {
		plan.Blockers = append(plan.Blockers, "node is unreachable: "+strings.TrimPrefix(t.err, "unreachable: "))

		return plan
	}

	if t.controlPlane {
		applyResetEtcd(&plan, in)
	}

	if in.lock != nil && in.lock.held != nil && !lockSettled(in.lock.held, append([]planPeer{t}, in.others...)) {
		plan.Warnings = append(plan.Warnings, in.lock.held.describe())
	}

	if onlyEndpoint(t, in.endpoints) {
		plan.Warnings = append(plan.Warnings,
			"this node is the only Talos endpoint of this context: the app loses access to the cluster once it is reset")
	}

	if in.disksErr != "" {
		plan.Warnings = append(plan.Warnings, "cannot list the user disks ("+in.disksErr+"): only the system disk can be wiped")
	}

	return plan
}

func applyResetEtcd(plan *resetPlan, in resetPlanInput) {
	controlPlanes := 1

	for _, p := range in.others {
		if p.controlPlane {
			controlPlanes++
		}
	}

	if controlPlanes == 1 {
		plan.LastControlPlane = true
		plan.Blockers = append(plan.Blockers, "it is the only control plane: resetting it would destroy the cluster")

		return
	}

	if in.etcd == nil || in.etcd.listErr != "" {
		reason := "no answer"
		if in.etcd != nil {
			reason = in.etcd.listErr
		}

		plan.Blockers = append(plan.Blockers, "cannot check etcd quorum: "+reason)

		return
	}

	member := findResetMember(in.etcd.members, in.target)
	if member == nil {
		plan.Warnings = append(plan.Warnings, "this control-plane node is not an etcd member")

		return
	}

	etcd := *in.etcd
	etcd.memberID = hexID(member.id)
	removal := computeMemberPlan(etcd)

	plan.EtcdMember = &resetMember{ID: etcd.memberID, Healthy: member.healthy}
	plan.Blockers = append(plan.Blockers, removal.Blockers...)

	if removal.IsLeader {
		plan.Warnings = append(plan.Warnings, "it is the etcd leader: a graceful reset hands leadership over before leaving")
	}
}

// findResetMember is the etcd member of the node, matched by address, else by hostname.
func findResetMember(members []memberState, t planPeer) *memberState {
	for i, m := range members {
		if m.address != "" && m.address == t.node {
			return &members[i]
		}
	}

	for i, m := range members {
		if t.hostname != "" && m.hostname == t.hostname {
			return &members[i]
		}
	}

	return nil
}
