package ichorgo

import (
	"cmp"
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// The rolling cluster upgrade: every node of the context to one Talos version, one at a
// time, in a fixed order (control planes first, the etcd leader last, then the workers), with
// a health gate between nodes, and pause, resume and abort. See StartClusterUpgrade.

const (
	clusterPhaseNode   = "node"
	clusterPhaseGate   = "gate"
	clusterPhasePaused = "paused"
	clusterPhaseDone   = "done"

	clusterNodePending = "pending"
	clusterNodeRunning = "running"
	clusterNodeDone    = "done"
	clusterNodeFailed  = "failed"

	clusterCommandPause  = "pause"
	clusterCommandResume = "resume"
	clusterCommandAbort  = "abort"

	// clusterSettle is how long the cluster settles after a node passed its gate.
	clusterSettle = time.Minute
	// clusterLockDuration and clusterLockRenew: the roll renews its lock while it runs, so a
	// phone that died frees it within minutes and a rerun resumes.
	clusterLockDuration = 3 * time.Minute
	clusterLockRenew    = time.Minute
	// clusterUpgradeTimeout bounds a whole roll, pauses included.
	clusterUpgradeTimeout = 12 * time.Hour
)

var errClusterAborted = errors.New("aborted: the nodes upgraded so far stay upgraded, the others were not touched")

// clusterUpgradePlan is what ClusterUpgradePlan returns.
type clusterUpgradePlan struct {
	Version string `json:"version"`
	// Image is the installer image of the first node to upgrade (each node keeps its own
	// registry and schematic: see the nodes' image).
	Image    string            `json:"image"`
	Nodes    []clusterPlanNode `json:"nodes"`
	Blockers []string          `json:"blockers"`
	Warnings []string          `json:"warnings"`
	// Drain: a node to upgrade does not drain by itself: StartClusterUpgrade can drain it.
	Drain bool `json:"drain"`
}

type clusterPlanNode struct {
	Node     string   `json:"node"`
	Hostname string   `json:"hostname"`
	Role     string   `json:"role"` // controlplane | worker
	From     string   `json:"from"`
	Image    string   `json:"image"`
	State    string   `json:"state"` // pending | done (already on the version) | running | failed
	Leader   bool     `json:"leader"`
	Blockers []string `json:"blockers"`
	Warnings []string `json:"warnings"`
}

// ClusterUpgradePlan lists, in the order StartClusterUpgrade follows, the context's nodes
// with their version and each node's upgrade checks for version (vX.Y.Z), read-only
// (os:reader; os:admin to read the installer images and the upgrade lock). Nodes already on
// version are "done": a rerun after an interrupted roll resumes with the others. See
// clusterUpgradePlan for the JSON. kubeServer: see KubePods.
func ClusterUpgradePlan(configYAML, contextName, kubeServer, version string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isDemoContext(configYAML, contextName) {
		return demoRead("ClusterUpgradePlan", configYAML, contextName, "", version)
	}

	return withSession(configYAML, contextName, clusterPlanTimeout(configYAML, contextName), func(ctx context.Context, s *session) (string, error) {
		plan, err := gatherClusterPlan(ctx, s, kubeTarget{configYAML, contextName, kubeServer}, version)
		if err != nil {
			return "", err
		}

		return toJSON(plan)
	})
}

// clusterPlanTimeout gives each node of the context a plan's time.
func clusterPlanTimeout(configYAML, contextName string) time.Duration {
	nodes := 1

	if _, c, err := resolveContext(configYAML, contextName); err == nil {
		nodes = max(len(targetNodes(c)), 1)
	}

	return time.Duration(nodes) * planTimeout
}

func gatherClusterPlan(ctx context.Context, s *session, kube kubeTarget, version string) (clusterUpgradePlan, error) {
	v, ok := normalizeTalosVersion(version)
	if !ok {
		return clusterUpgradePlan{}, fmt.Errorf("%q is not a Talos version (vX.Y.Z)", version)
	}

	plan := clusterUpgradePlan{Version: v, Nodes: []clusterPlanNode{}, Blockers: []string{}, Warnings: []string{}}

	leader := etcdLeaderNode(ctx, s)

	for _, node := range targetNodes(s.context) {
		up := gatherPlan(ctx, s, node, kubeTarget{})
		n := clusterNodeOf(node, up, v, node == leader)

		// A pending node whose extensions the version has no build of (an unreadable list
		// only says it could not be checked).
		if n.State == clusterNodePending {
			if installed, err := nodeExtensions(ctx, s, node); err == nil {
				n.Warnings = append(n.Warnings, extensionWarnings(checkExtensions(installed, n.Image, officialExtensions))...)
			}
		}

		plan.Nodes = append(plan.Nodes, n)
	}

	plan.Nodes = orderClusterNodes(plan.Nodes)

	if lock := readUpgradeLock(ctx, kube, time.Now()); lock != nil {
		switch {
		case lock.err != "":
			plan.Warnings = append(plan.Warnings, "cannot check the cluster upgrade lock ("+lock.err+"): make sure nobody else upgrades this cluster now")
		case lock.held != nil:
			plan.Blockers = append(plan.Blockers, lock.held.describe())
		}
	}

	summarizeClusterPlan(&plan)

	return plan, nil
}

// clusterNodeOf is one node of the plan from its upgrade plan.
func clusterNodeOf(node string, up upgradePlan, version string, leader bool) clusterPlanNode {
	n := clusterPlanNode{
		Node: node, Hostname: up.Hostname, Role: "worker", From: up.CurrentVersion,
		Image: UpgradeImage(up.CurrentImage, version), State: clusterNodePending, Leader: leader,
		Blockers: orEmpty(up.Blockers), Warnings: []string{},
	}

	if n.Hostname == "" {
		n.Hostname = node
	}

	if up.ControlPlane {
		n.Role = "controlplane"
	}

	if up.CurrentVersion != "" && sameVersion(up.CurrentVersion, version) {
		n.State, n.Blockers = clusterNodeDone, []string{}
	}

	for _, w := range up.Warnings {
		// Peers' states and mixed versions are what a roll goes through: not this node's.
		if !strings.Contains(w, "different Talos versions") {
			n.Warnings = append(n.Warnings, w)
		}
	}

	n.Warnings = append(n.Warnings, up.Acknowledge...)

	return n
}

// summarizeClusterPlan fills the plan's image, drain and its cluster-wide notes.
func summarizeClusterPlan(plan *clusterUpgradePlan) {
	pending := 0

	for _, n := range plan.Nodes {
		if n.State != clusterNodePending {
			continue
		}

		pending++

		if plan.Image == "" {
			plan.Image = n.Image
		}

		if upgradeSkipsDrain(n.From) {
			plan.Drain = true
		}

		if len(n.Blockers) > 0 {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s cannot be upgraded now: the roll pauses before it until that is fixed", n.Hostname))
		}
	}

	if pending == 0 {
		plan.Warnings = append(plan.Warnings, "every node already runs "+plan.Version)
	}
}

// orderClusterNodes is the roll's order: control planes first, the etcd leader last of
// them, then the workers, each group by hostname (the order of the per-node rollout).
func orderClusterNodes(nodes []clusterPlanNode) []clusterPlanNode {
	out := slices.Clone(nodes)

	rank := func(n clusterPlanNode) int {
		switch {
		case n.Role == "controlplane" && !n.Leader:
			return 0
		case n.Role == "controlplane":
			return 1
		default:
			return 2
		}
	}

	slices.SortStableFunc(out, func(a, b clusterPlanNode) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.Hostname, b.Hostname), cmp.Compare(a.Node, b.Node))
	})

	return out
}

// etcdLeaderNode is the address of the etcd leader's node, "" when unknown.
func etcdLeaderNode(ctx context.Context, s *session) string {
	ov, err := gatherEtcdOverview(ctx, s)
	if err != nil {
		return ""
	}

	for _, st := range ov.Statuses {
		if st.IsLeader {
			return st.Node
		}
	}

	return ""
}

// ClusterUpgradeListener follows a cluster upgrade (implemented in Kotlin/Swift).
type ClusterUpgradeListener interface {
	// OnProgress gets {"phase","index","total","node","hostname","nodePhase","message","at",
	// "nodes"} on every change: phase is node (a node is upgraded: nodePhase and message are
	// its own progress), gate (health checks, then the settle time), paused (message says
	// why) or done; index is the node's position in nodes, total their count; nodes are the
	// plan's nodes with their state now.
	OnProgress(json string)
	// OnDone is called exactly once; errMessage is empty when every node was upgraded.
	OnDone(errMessage string)
}

// ClusterUpgradeRun is a handle on a running cluster upgrade.
type ClusterUpgradeRun struct {
	cancel   context.CancelFunc
	commands chan string
}

// Pause stops the roll before its next node (a node being upgraded finishes).
func (r *ClusterUpgradeRun) Pause() { r.send(clusterCommandPause) }

// Resume goes on after a pause, retries a failed gate, or skips the rest of the settle time.
func (r *ClusterUpgradeRun) Resume() { r.send(clusterCommandResume) }

// Abort ends the roll before its next node and frees the lock; a node being upgraded
// finishes first (it is never interrupted).
func (r *ClusterUpgradeRun) Abort() { r.send(clusterCommandAbort) }

// Cancel stops following now. A node being upgraded goes on by itself; the lock expires
// within minutes and a new StartClusterUpgrade resumes with the nodes left.
func (r *ClusterUpgradeRun) Cancel() { r.cancel() }

func (r *ClusterUpgradeRun) send(command string) {
	select {
	case r.commands <- command:
	default: // the run is busy with earlier commands: they say the same
	}
}

type clusterProgress struct {
	Phase     string            `json:"phase"`
	Index     int               `json:"index"`
	Total     int               `json:"total"`
	Node      string            `json:"node"`
	Hostname  string            `json:"hostname"`
	NodePhase string            `json:"nodePhase"`
	Message   string            `json:"message"`
	At        int64             `json:"at"`
	Nodes     []clusterPlanNode `json:"nodes"`
}

// StartClusterUpgrade upgrades every node of the context to version (vX.Y.Z), one at a time
// in ClusterUpgradePlan's order (os:admin). Nodes already on version are skipped, so a rerun
// after an interrupted roll resumes. Each node gets its own installer image (its registry
// and schematic) and goes through the checks of StartUpgrade (blockers refused, the plan's
// acknowledgments and the version risk refused unless acknowledged); with drain, a node
// whose upgrade does not drain it is cordoned and drained first (StartNodeMaintenanceUpgrade).
// The etcd leader forfeits its leadership before its own upgrade. Between nodes a gate
// checks that the node runs version, is Ready in Kubernetes and etcd is healthy without
// alarms, then the cluster settles for a minute; a failed gate pauses the roll (Resume
// retries it). The cluster upgrade lock is held, and renewed, for the whole roll.
// kubeServer: see KubePods.
func StartClusterUpgrade(configYAML, contextName, kubeServer, version string, drain, acknowledged bool, listener ClusterUpgradeListener) *ClusterUpgradeRun {
	contextName = unmaskContext(configYAML, contextName)

	listener = maskedClusterUpgradeListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), clusterUpgradeTimeout)
	run := &ClusterUpgradeRun{cancel: cancel, commands: make(chan string, 4)}

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		var upgraded []string

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{Action: "cluster-upgrade", Params: fmt.Sprintf("version=%s drain=%t nodes=%s", version, drain, strings.Join(upgraded, ","))}
		}, func() error {
			return startClusterUpgrade(ctx, kubeTarget{configYAML, contextName, kubeServer}, version, drain, acknowledged, run.commands,
				func(p clusterProgress) {
					upgraded = upgradedNodes(p.Nodes)
					emitJSON(p, listener.OnProgress)
				})
		})

		listener.OnDone(errText(err))
	}()

	return run
}

func upgradedNodes(nodes []clusterPlanNode) []string {
	var out []string

	for _, n := range nodes {
		if n.State == clusterNodeDone {
			out = append(out, n.Hostname)
		}
	}

	return out
}

func startClusterUpgrade(ctx context.Context, kube kubeTarget, version string, drain, acknowledged bool, commands <-chan string, emit func(clusterProgress)) error {
	if isDemoContext(kube.config, kube.context) {
		return errDemoUnavailable
	}

	s, release, err := acquireSession(kube.config, kube.context)
	if err != nil {
		return err
	}

	defer release()

	planCtx, planCancel := context.WithTimeout(ctx, clusterPlanTimeout(kube.config, kube.context))
	plan, err := gatherClusterPlan(planCtx, s, kube, version)

	planCancel()

	if err != nil {
		return err
	}

	if len(plan.Blockers) > 0 {
		return errors.New("cluster upgrade refused: " + strings.Join(plan.Blockers, "; "))
	}

	lock, err := takeUpgradeLock(ctx, kube,
		upgradeLockRequest{holder: newLockHolder(), hostname: "cluster", to: plan.Version, run: lockRunCluster, duration: clusterLockDuration},
		// A cluster roll never takes over a held lock: one that died frees it within minutes.
		func(*upgradeLockInfo) bool { return false },
		func(msg string) {
			emit(clusterProgress{Phase: clusterPhaseNode, Message: msg, At: time.Now().UnixMilli()})
		})
	if err != nil {
		return err
	}

	defer lock.release()

	steps := &talosClusterSteps{s: s, kube: kube, lock: lock, version: plan.Version, drain: drain, acknowledged: acknowledged}

	renewCtx, stopRenewing := context.WithCancel(ctx)
	defer stopRenewing()

	go steps.keepLock(renewCtx)

	return runClusterUpgrade(ctx, steps, plan.Nodes, clusterControl{commands: commands, emit: emit, settle: clusterSettle, after: time.After})
}

// clusterSteps are the steps of the roll on one node; tests replace them.
type clusterSteps interface {
	// upgrade upgrades n (its leadership forfeited first when it leads etcd).
	upgrade(ctx context.Context, n clusterPlanNode, emit func(phase, message string)) error
	// gate checks the cluster is healthy again after n.
	gate(ctx context.Context, n clusterPlanNode) error
}

// clusterControl is how a roll is steered and reported.
type clusterControl struct {
	commands <-chan string
	emit     func(clusterProgress)
	settle   time.Duration
	after    func(time.Duration) <-chan time.Time
}

// runClusterUpgrade upgrades the pending nodes in order through steps, with the gate and
// the commands between them.
func runClusterUpgrade(ctx context.Context, steps clusterSteps, nodes []clusterPlanNode, c clusterControl) error {
	nodes = slices.Clone(nodes)
	report := func(phase string, i int, nodePhase, message string) {
		p := clusterProgress{Phase: phase, Index: i, Total: len(nodes), NodePhase: nodePhase, Message: message, At: time.Now().UnixMilli(), Nodes: slices.Clone(nodes)}
		if i >= 0 && i < len(nodes) {
			p.Node, p.Hostname = nodes[i].Node, nodes[i].Hostname
		}

		c.emit(p)
	}

	gated := -1 // the node whose gate must pass before the next one

	for i := range nodes {
		if nodes[i].State == clusterNodeDone {
			continue
		}

		if gated >= 0 {
			if err := passGate(ctx, steps, nodes[gated], gated, c, report); err != nil {
				return err
			}
		}

		// Commands sent while the previous node was upgraded take effect now.
		if err := between(ctx, i, c, report); err != nil {
			return err
		}

		nodes[i].State = clusterNodeRunning
		report(clusterPhaseNode, i, "", "upgrading "+nodes[i].Hostname)

		err := steps.upgrade(ctx, nodes[i], func(phase, message string) { report(clusterPhaseNode, i, phase, message) })
		if err != nil {
			nodes[i].State = clusterNodeFailed
			report(clusterPhaseNode, i, "", err.Error())

			return fmt.Errorf("%s: %w (the nodes before it are upgraded, the ones after it were not touched)", nodes[i].Hostname, err)
		}

		nodes[i].State = clusterNodeDone
		gated = i
	}

	report(clusterPhaseDone, len(nodes)-1, "", "every node runs the new version")

	return nil
}

// between handles the commands waiting when a node is about to start: pause waits for
// resume or abort; abort stops.
func between(ctx context.Context, i int, c clusterControl, report func(string, int, string, string)) error {
	for {
		select {
		case command := <-c.commands:
			switch command {
			case clusterCommandAbort:
				return errClusterAborted
			case clusterCommandPause:
				if err := paused(ctx, i, "paused before this node", c, report); err != nil {
					return err
				}
			}
		default:
			return nil
		}
	}
}

// paused waits for resume (nil) or abort.
func paused(ctx context.Context, i int, why string, c clusterControl, report func(string, int, string, string)) error {
	report(clusterPhasePaused, i, "", why)

	for {
		select {
		case <-ctx.Done():
			return errStoppedFollowing
		case command := <-c.commands:
			switch command {
			case clusterCommandAbort:
				return errClusterAborted
			case clusterCommandResume:
				return nil
			}
		}
	}
}

// passGate checks the cluster after node n (index i) until it passes, pausing on each
// failure, then lets it settle (Resume skips the rest of the wait).
func passGate(ctx context.Context, steps clusterSteps, n clusterPlanNode, i int, c clusterControl, report func(string, int, string, string)) error {
	for {
		report(clusterPhaseGate, i, "", "checking "+n.Hostname+", its Kubernetes node and etcd")

		err := steps.gate(ctx, n)
		if err == nil {
			break
		}

		if ctx.Err() != nil {
			return errStoppedFollowing
		}

		if err := paused(ctx, i, "health check failed: "+err.Error(), c, report); err != nil {
			return err
		}
	}

	report(clusterPhaseGate, i, "", fmt.Sprintf("healthy: letting the cluster settle for %s", c.settle))

	select {
	case <-ctx.Done():
		return errStoppedFollowing
	case <-c.after(c.settle):
	case command := <-c.commands:
		switch command {
		case clusterCommandAbort:
			return errClusterAborted
		case clusterCommandPause:
			return paused(ctx, i, "paused after "+n.Hostname, c, report)
		}
	}

	return nil
}

// talosClusterSteps are the roll's steps on the cluster, under the roll's lock.
type talosClusterSteps struct {
	s                   *session
	kube                kubeTarget
	lock                *upgradeLock
	version             string
	drain, acknowledged bool
}

func (t *talosClusterSteps) keepLock(ctx context.Context) {
	ticker := time.NewTicker(clusterLockRenew)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(ctx, callTimeout)
			_ = t.lock.renew(renewCtx, nil) //nolint:errcheck // stillHeld refuses the next step when it is lost
			cancel()
		}
	}
}

func (t *talosClusterSteps) upgrade(ctx context.Context, n clusterPlanNode, emit func(phase, message string)) error {
	renewCtx, cancel := context.WithTimeout(ctx, callTimeout)
	_ = t.lock.renew(renewCtx, map[string]string{annotationNode: n.Node, annotationHostname: n.Hostname}) //nolint:errcheck // see keepLock
	cancel()

	if n.Leader {
		emit("forfeit", "handing etcd leadership over before upgrading the leader")

		if _, err := forfeitEtcdLeadership(ctx, t.s.client, n.Node); err != nil {
			return fmt.Errorf("forfeiting etcd leadership: %w", t.s.friendlyErr(n.Node, err))
		}
	}

	image := n.Image
	if image == "" {
		return errors.New("no installer image for this node")
	}

	if t.drain && upgradeSkipsDrain(n.From) {
		return t.drainedUpgrade(ctx, n, image, emit)
	}

	planCtx, planCancel := context.WithTimeout(ctx, planTimeout)
	plan := gatherPlan(planCtx, t.s, n.Node, kubeTarget{})

	planCancel()

	if err := upgradeRefusal(plan, false); err != nil {
		return err
	}

	_, tag := splitImageRef(image)
	if err := acknowledgmentRefusal(plan, tag, t.acknowledged); err != nil {
		return err
	}

	if err := t.stillHeld(ctx); err != nil {
		return err
	}

	_, err := requestAndFollow(ctx, t.s, n.Node, plan, upgradeOptions{image: image, acknowledged: t.acknowledged}, emit)

	return err
}

// drainedUpgrade is the node's maintenance upgrade (cordon, drain, upgrade, back,
// uncordon) under the roll's lock.
func (t *talosClusterSteps) drainedUpgrade(ctx context.Context, n clusterPlanNode, image string, emit func(phase, message string)) error {
	planCtx, planCancel := context.WithTimeout(ctx, planTimeout)
	plan, err := gatherMaintenancePlan(planCtx, t.s, t.kube, n.Node)

	planCancel()

	if err != nil {
		return err
	}

	m := maintenance{
		kube: t.kube, node: n.Node, action: maintenanceUpgrade, acknowledged: t.acknowledged,
		listener: maintenanceEmitter(emit), image: image,
	}

	if err := m.upgradeChecks(plan); err != nil {
		return err
	}

	return kubeDo(ctx, t.kube, func(ctx context.Context, k *kubeClient) error {
		return m.drainThenUpgrade(ctx, k, plan, m.talosUpgradeSteps(t.s, k, t.lock, plan))
	})
}

// stillHeld checks the roll's lock before a node's upgrade is requested.
func (t *talosClusterSteps) stillHeld(ctx context.Context) error {
	if t.lock.holder() == "" {
		return nil
	}

	return kubeDo(ctx, t.kube, func(ctx context.Context, k *kubeClient) error { return t.lock.stillHeld(ctx, k) })
}

func (t *talosClusterSteps) gate(ctx context.Context, n clusterPlanNode) error {
	ctx, cancel := context.WithTimeout(ctx, planTimeout)
	defer cancel()

	o := observeNode(ctx, t.s.client, n.Node)

	switch {
	case !o.reachable:
		return fmt.Errorf("%s does not answer", n.Hostname)
	case !sameVersion(o.version, t.version):
		return fmt.Errorf("%s runs %s, not %s", n.Hostname, o.version, t.version)
	case o.stage != runtime.MachineStageRunning.String() || !o.ready:
		return fmt.Errorf("%s is not running and ready yet", n.Hostname)
	}

	if state := fetchKubeNodeState(client.WithNode(ctx, n.Node), t.s.client); state == nil || !state.Ready {
		return fmt.Errorf("%s is not Ready in Kubernetes yet", n.Hostname)
	}

	ov, err := gatherEtcdOverview(ctx, t.s)
	if err != nil {
		return fmt.Errorf("etcd: %w", err)
	}

	return etcdGate(ov)
}

// etcdGate passes when every member answered without errors and no alarm is raised.
func etcdGate(ov etcdOverview) error {
	if ov.AlarmsError != "" {
		return errors.New("etcd alarms could not be read: " + ov.AlarmsError)
	}

	if len(ov.Alarms) > 0 {
		return fmt.Errorf("etcd raised the %s alarm", ov.Alarms[0].Alarm)
	}

	for _, st := range ov.Statuses {
		if st.Error != "" || len(st.Errors) > 0 {
			return fmt.Errorf("etcd on %s is not healthy", st.Node)
		}
	}

	return nil
}

// maintenanceEmitter reports a maintenance run's progress as a node's phases.
type maintenanceEmitter func(phase, message string)

func (e maintenanceEmitter) OnProgress(json string) {
	var p maintenanceProgress
	if err := stdjson.Unmarshal([]byte(json), &p); err == nil {
		e(p.Phase, p.Message)
	}
}

func (e maintenanceEmitter) OnDone(string) {}
