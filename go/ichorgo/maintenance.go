package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// Node maintenance: cordon → drain → reboot (or shut down, or nothing) → wait until the node
// is back and Ready → uncordon, as one followed run. See plans/roadmap/devops/01-node-maintenance.md.

const (
	// maintenanceTimeout is the cluster upgrade lock's lifetime: the lock is not renewed.
	maintenanceTimeout = upgradeLockDuration
	// drainTimeout bounds the drain: a PodDisruptionBudget that never allows a disruption
	// must not hold the node (and the lock) forever.
	drainTimeout = 15 * time.Minute
	// backTimeout bounds the wait for the rebooted node to be Ready again.
	backTimeout = 20 * time.Minute
	// maintenanceLockTo marks the cluster upgrade lock as held by a maintenance run.
	maintenanceLockTo = "maintenance"
)

// Maintenance actions after the drain.
const (
	maintenanceReboot   = "reboot"
	maintenanceShutdown = "shutdown"
	maintenanceNone     = "none"
)

// Maintenance phases reported through MaintenanceListener.OnProgress.
const (
	phaseCordon   = "cordon"
	phaseDrain    = "drain"
	phaseReboot   = "reboot"
	phaseShutdown = "shutdown"
	phaseBack     = "waiting"
	phaseUncordon = "uncordon"
)

// MaintenanceListener follows a node maintenance run (implemented in Kotlin/Swift).
type MaintenanceListener interface {
	// OnProgress gets {"phase","message","at","pods"} (at: unix ms; pods: the pods being
	// evicted with their state) on every change.
	OnProgress(json string)
	// OnDone is called exactly once; errMessage is empty on success.
	OnDone(errMessage string)
}

// MaintenanceRun is a handle on a running maintenance.
type MaintenanceRun struct {
	cancel context.CancelFunc
}

// Cancel stops the run after the current step; the node stays cordoned.
func (r *MaintenanceRun) Cancel() { r.cancel() }

type maintenanceProgress struct {
	Phase   string     `json:"phase"`
	Message string     `json:"message"`
	At      int64      `json:"at"`
	Pods    []drainPod `json:"pods,omitempty"`
}

// maintenancePlan is what NodeMaintenancePlan returns.
type maintenancePlan struct {
	Node         string     `json:"node"`
	Hostname     string     `json:"hostname"`
	KubeNode     string     `json:"kubeNode"`
	ControlPlane bool       `json:"controlPlane"`
	Cordoned     bool       `json:"cordoned"`
	Pods         []drainPod `json:"pods"`
	// Blockers refuse the run; Acknowledge must be confirmed; Warnings are informative.
	// They concern the reboot or shutdown (etcd quorum, peers, the only endpoint, the lock).
	Blockers    []string `json:"blockers"`
	Warnings    []string `json:"warnings"`
	Acknowledge []string `json:"acknowledge"`
}

// NodeMaintenancePlan reports what a maintenance of node would do, before running it: the
// pods a drain evicts or leaves (DaemonSet and static pods), their PodDisruptionBudgets and
// emptyDir data, and the checks of a reboot (etcd quorum, unhealthy peers, the only
// endpoint, the cluster upgrade lock). Needs the Kubernetes API (os:admin); kubeServer: see
// KubePods.
func NodeMaintenancePlan(configYAML, contextName, kubeServer, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return toJSON(demoMaintenancePlan(node))
	}

	kube := kubeTarget{configYAML, contextName, kubeServer}

	return withSession(configYAML, contextName, planTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		plan, err := gatherMaintenancePlan(ctx, s, kube, node)
		if err != nil {
			return "", err
		}

		return toJSON(plan)
	})
}

func gatherMaintenancePlan(ctx context.Context, s *session, kube kubeTarget, node string) (maintenancePlan, error) {
	reboot := gatherPlan(ctx, s, node, kube)

	plan := maintenancePlan{
		Node:         node,
		Hostname:     reboot.Hostname,
		ControlPlane: reboot.ControlPlane,
		Blockers:     reboot.Blockers,
		Warnings:     rebootWarnings(reboot.Warnings),
		Acknowledge:  reboot.Acknowledge,
	}

	state := fetchKubeNodeState(client.WithNode(ctx, node), s.client)
	if state == nil || state.Name == "" {
		return plan, errKubeNodeUnknown
	}

	plan.KubeNode, plan.Cordoned = state.Name, state.Unschedulable

	pods, err := withKubeContext(ctx, kube, func(ctx context.Context, k *kubeClient) ([]drainPod, error) {
		return drainPods(ctx, k, state.Name)
	})
	if err != nil {
		return plan, err
	}

	plan.Pods = pods

	for _, p := range pods {
		switch {
		case p.Kind == drainBare:
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s/%s has no controller: once evicted it is not recreated", p.Namespace, p.Name))
		case p.Kind == drainEvict && p.PDB != "" && p.PDBAllowed == 0:
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("PodDisruptionBudget %s/%s allows no disruption now: the drain waits for it", p.Namespace, p.PDB))
		}
	}

	plan.Warnings = slices.Compact(plan.Warnings)

	return plan, nil
}

// rebootWarnings drops the upgrade plan's warnings that are about upgrading, not rebooting.
func rebootWarnings(warnings []string) []string {
	out := []string{}

	for _, w := range warnings {
		if !strings.HasPrefix(w, noDrainWarning) && !strings.Contains(w, "installer image") {
			out = append(out, w)
		}
	}

	return out
}

// KubeCordon cordons (on) or uncordons node, like `kubectl cordon`/`uncordon` (os:admin):
// a cordoned node gets no new pods. kubeServer: see KubePods.
func KubeCordon(configYAML, contextName, kubeServer, node string, on bool) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return errDemoUnavailable
	}

	kubeNode, err := withNodeSession(configYAML, contextName, node, callTimeout, func(nodeCtx context.Context, s *session) (string, error) {
		return kubeNodeNameOf(nodeCtx, s.client)
	})
	if err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return setUnschedulable(ctx, k, kubeNode, on)
	})
}

// StartNodeMaintenance cordons node, drains it (evictions honour PodDisruptionBudgets; bare
// pods only with includeBare), then, per action: "reboot" reboots it, waits until it is back
// and Ready, and uncordons it; "shutdown" powers it off; "none" stops after the drain. The
// node stays cordoned after "shutdown", "none", a failure or Cancel. Reboot and shutdown
// re-check the plan and refuse its blockers, and its acknowledgments unless acknowledged.
// The run holds the cluster upgrade lock (no upgrade or other maintenance meanwhile). Needs
// os:admin (Kubernetes) and os:operator (reboot); kubeServer: see KubePods.
func StartNodeMaintenance(configYAML, contextName, kubeServer, node, action string, includeBare, acknowledged bool, listener MaintenanceListener) *MaintenanceRun {
	contextName, node = unmaskTarget(configYAML, contextName, node)

	listener = maskedMaintenanceListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), maintenanceTimeout)

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		m := maintenance{
			kube:         kubeTarget{configYAML, contextName, kubeServer},
			node:         node,
			action:       strings.ToLower(strings.TrimSpace(action)),
			includeBare:  includeBare,
			acknowledged: acknowledged,
			listener:     listener,
		}

		errMessage := errText(m.run(ctx))

		listener.OnDone(errMessage)
	}()

	return &MaintenanceRun{cancel: cancel}
}

type maintenance struct {
	kube                      kubeTarget
	node                      string
	action                    string
	includeBare, acknowledged bool
	listener                  MaintenanceListener
}

func (m maintenance) emit(phase, message string, pods []drainPod) {
	b, err := json.Marshal(maintenanceProgress{Phase: phase, Message: message, At: time.Now().UnixMilli(), Pods: pods})
	if err == nil {
		m.listener.OnProgress(string(b))
	}
}

// stoppedCordoned explains a run that ended early: the node is left cordoned on purpose.
func stoppedCordoned(kubeNode string, why error) error {
	return fmt.Errorf("%w; %s stays cordoned: uncordon it from its menu when ready", why, kubeNode)
}

func (m maintenance) run(ctx context.Context) error {
	if !slices.Contains([]string{maintenanceReboot, maintenanceShutdown, maintenanceNone}, m.action) {
		return fmt.Errorf("unknown maintenance action %q (reboot, shutdown, none)", m.action)
	}

	if isDemoContext(m.kube.config, m.kube.context) {
		return errDemoUnavailable
	}

	s, release, err := acquireNode(m.kube.config, m.kube.context, m.node)
	if err != nil {
		return err
	}

	defer release()

	planCtx, planCancel := context.WithTimeout(ctx, planTimeout)
	plan, err := gatherMaintenancePlan(planCtx, s, m.kube, m.node)

	planCancel()

	if err != nil {
		return err
	}

	if err := m.refusal(plan); err != nil {
		return err
	}

	lock, err := takeUpgradeLock(ctx, m.kube,
		upgradeLockRequest{holder: newLockHolder(), node: m.node, hostname: plan.Hostname, to: maintenanceLockTo},
		// Never take over a lock someone else holds: it frees itself when that run ends or expires.
		func(*upgradeLockInfo) bool { return false },
		func(msg string) { m.emit(phaseCordon, msg, nil) })
	if err != nil {
		return err
	}

	defer lock.release()

	return withKubeContext2(ctx, m.kube, func(ctx context.Context, k *kubeClient) error {
		return m.steps(ctx, s, k, lock, plan)
	})
}

// refusal applies the plan: no blockers, and acknowledgments confirmed, for a reboot or
// shutdown (a drain alone is always allowed).
func (m maintenance) refusal(plan maintenancePlan) error {
	if m.action == maintenanceNone {
		return nil
	}

	if len(plan.Blockers) > 0 {
		return errors.New("maintenance refused: " + strings.Join(plan.Blockers, "; "))
	}

	if len(plan.Acknowledge) > 0 && !m.acknowledged {
		return errors.New("maintenance refused until confirmed: " + strings.Join(plan.Acknowledge, "; "))
	}

	return nil
}

// recheck runs again, right before the reboot or shutdown, the checks the plan made before
// the drain (which can take drainTimeout): no blocker, no new acknowledgment, and the lock
// is still this run's. Something may have changed meanwhile (another control plane down
// through talosctl, an expired lock after the phone slept).
func (m maintenance) recheck(ctx context.Context, s *session, k *kubeClient, lock *upgradeLock, plan maintenancePlan) error {
	if err := lock.stillHeld(ctx, k); err != nil {
		return err
	}

	planCtx, cancel := context.WithTimeout(ctx, planTimeout)
	defer cancel()

	// The lock is not read here: it is this run's (checked above), not a blocker.
	now := gatherPlan(planCtx, s, m.node, kubeTarget{})
	if len(now.Blockers) > 0 {
		return errors.New("not rebooting: " + strings.Join(now.Blockers, "; "))
	}

	for _, a := range now.Acknowledge {
		if !slices.Contains(plan.Acknowledge, a) {
			return errors.New("not rebooting, this needs confirming first: " + a)
		}
	}

	return nil
}

func (m maintenance) steps(ctx context.Context, s *session, k *kubeClient, lock *upgradeLock, plan maintenancePlan) error {
	m.emit(phaseCordon, "cordoning "+plan.KubeNode, nil)

	if err := setUnschedulable(ctx, k, plan.KubeNode, true); err != nil {
		return fmt.Errorf("%w; %s may be cordoned now: check it before trying again", kubeMutationError(err), plan.KubeNode)
	}

	// Listed again now that nothing new is scheduled here, like kubectl drain: a pod placed
	// between the plan and the cordon must be evicted too, not killed by the reboot.
	listed, err := drainPods(ctx, k, plan.KubeNode)
	if err != nil {
		return stoppedCordoned(plan.KubeNode, err)
	}

	pods := toEvict(listed, m.includeBare)
	m.emit(phaseDrain, fmt.Sprintf("evicting %d pods", len(pods)), pods)

	drainCtx, drainCancel := context.WithTimeout(ctx, drainTimeout)
	err = drain(drainCtx, k, pods, func(p []drainPod) { m.emit(phaseDrain, drainMessage(p), p) }, drainPollInterval)

	drainCancel()

	if err != nil {
		if ctx.Err() == nil && drainCtx.Err() != nil {
			err = fmt.Errorf("the drain did not finish within %s (%s)", drainTimeout, drainMessage(pods))
		}

		return stoppedCordoned(plan.KubeNode, err)
	}

	switch m.action {
	case maintenanceNone:
		m.emit(phaseDrain, plan.KubeNode+" is drained and stays cordoned", pods)

		return nil
	}

	if err := m.recheck(ctx, s, k, lock, plan); err != nil {
		return stoppedCordoned(plan.KubeNode, err)
	}

	if m.action == maintenanceShutdown {
		m.emit(phaseShutdown, "shutting down "+plan.Hostname, nil)

		if err := s.client.Shutdown(client.WithNode(ctx, m.node)); err != nil {
			return stoppedCordoned(plan.KubeNode, s.friendlyErr(m.node, err))
		}

		return nil
	}

	m.emit(phaseReboot, "rebooting "+plan.Hostname, nil)

	rebootAt := time.Now()

	if err := s.client.Reboot(client.WithNode(ctx, m.node)); err != nil {
		return stoppedCordoned(plan.KubeNode, s.friendlyErr(m.node, err))
	}

	backCtx, backCancel := context.WithTimeout(ctx, backTimeout)
	err = waitBack(backCtx, func(ctx context.Context) backObservation {
		return observeBack(ctx, s.client, k, m.node, plan.KubeNode, rebootAt)
	},
		func(msg string) { m.emit(phaseBack, msg, nil) }, upgradePollInterval)

	backCancel()

	if err != nil {
		if ctx.Err() == nil && backCtx.Err() != nil {
			err = fmt.Errorf("the node is not back and Ready within %s", backTimeout)
		}

		return stoppedCordoned(plan.KubeNode, err)
	}

	// A node someone cordoned before the run (a bad disk, a pending decommission) stays so.
	if plan.Cordoned {
		m.emit(phaseUncordon, plan.KubeNode+" was cordoned before the maintenance: it stays cordoned", nil)

		return nil
	}

	m.emit(phaseUncordon, "uncordoning "+plan.KubeNode, nil)

	if err := setUnschedulable(ctx, k, plan.KubeNode, false); err != nil {
		return stoppedCordoned(plan.KubeNode, kubeMutationError(err))
	}

	return nil
}

func drainMessage(pods []drainPod) string {
	gone, blocked := 0, 0

	for _, p := range pods {
		switch p.State {
		case podGone:
			gone++
		case podBlocked:
			blocked++
		}
	}

	msg := fmt.Sprintf("%d of %d pods evicted", gone, len(pods))
	if blocked > 0 {
		msg += fmt.Sprintf(", %d waiting for a PodDisruptionBudget", blocked)
	}

	return msg
}

// backObservation is one poll of a rebooting node.
type backObservation struct {
	reachable, running, kubeReady bool
}

func observeBack(ctx context.Context, c *client.Client, k *kubeClient, node, kubeNode string, since time.Time) backObservation {
	o := observeNode(ctx, c, node)
	b := backObservation{reachable: o.reachable, running: o.stage == runtime.MachineStageRunning.String() && o.ready}

	if b.running {
		ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
		defer cancel()

		b.kubeReady = kubeNodeReadySince(ctx, k, kubeNode, since)
	}

	return b
}

// kubeNodeReadySince tells whether the Kubernetes node is Ready with a heartbeat after since:
// right after a fast reboot the Node object still says Ready from before it (Kubernetes
// marks a node NotReady only after a grace period), while the restarted kubelet posts its
// status, with a fresh heartbeat, as soon as it registers.
func kubeNodeReadySince(ctx context.Context, k *kubeClient, kubeNode string, since time.Time) bool {
	var obj struct {
		Status struct {
			Conditions []struct {
				Type              string    `json:"type"`
				Status            string    `json:"status"`
				LastHeartbeatTime time.Time `json:"lastHeartbeatTime"`
			} `json:"conditions"`
		} `json:"status"`
	}

	if err := k.get(ctx, "/api/v1/nodes/"+url.PathEscape(kubeNode), &obj); err != nil {
		return false
	}

	for _, c := range obj.Status.Conditions {
		if c.Type == "Ready" {
			return c.Status == "True" && c.LastHeartbeatTime.After(since)
		}
	}

	return false
}

// waitBack polls until the node went down and came back running with its Kubernetes node
// Ready. Stopping pods and services takes longer than a poll, so the reboot is seen.
func waitBack(ctx context.Context, observe func(context.Context) backObservation, emit func(string), interval time.Duration) error {
	sawDown, last := false, ""

	for {
		select {
		case <-ctx.Done():
			return errors.New("stopped while waiting for the node")
		case <-time.After(interval):
		}

		o := observe(ctx)
		if ctx.Err() != nil {
			continue
		}

		var msg string

		switch {
		case !o.reachable:
			sawDown, msg = true, "the node is rebooting"
		case !o.running:
			sawDown, msg = true, "the node is booting"
		case !sawDown:
			msg = "waiting for the node to reboot"
		case !o.kubeReady:
			msg = "the node is up, waiting for Kubernetes to report it Ready"
		default:
			emit("the node is back and Ready")

			return nil
		}

		if msg != last {
			emit(msg)
			last = msg
		}
	}
}

// withKubeContext2 is withKubeContext for an action without a result.
func withKubeContext2(ctx context.Context, target kubeTarget, fn func(context.Context, *kubeClient) error) error {
	_, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (struct{}, error) {
		return struct{}{}, fn(ctx, k)
	})

	return err
}

type maskedMaintenanceListener struct{ MaintenanceListener }

func (l maskedMaintenanceListener) OnProgress(json string) {
	l.MaintenanceListener.OnProgress(privacy.mask(json))
}

func (l maskedMaintenanceListener) OnDone(errMessage string) {
	l.MaintenanceListener.OnDone(privacy.maskPlain(errMessage))
}
