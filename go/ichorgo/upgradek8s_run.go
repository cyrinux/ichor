package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
)

// Running the Kubernetes upgrade of K8sUpgradePlan: see StartK8sUpgrade.

const (
	k8sPhaseControlPlane = "controlplane"
	k8sPhaseKubelet      = "kubelet"
	k8sPhaseProxy        = "proxy"
	k8sPhaseDone         = "done"

	// k8sWaitTimeout bounds the wait for a node's static pods or kubelet on the new version.
	k8sWaitTimeout = 5 * time.Minute
	k8sWaitPoll    = 5 * time.Second
	// k8sUpgradeTimeout bounds a whole run.
	k8sUpgradeTimeout = 3 * time.Hour

	kubeProxyPath = "/apis/apps/v1/namespaces/kube-system/daemonsets/kube-proxy"
)

var (
	errK8sCancelled   = errors.New("cancelled: the nodes done so far keep the new version, the others were not touched")
	errK8sNeedsReboot = errors.New("the node answered that the change needs a reboot: the Kubernetes images never do, so it was not applied")
)

// k8sStaticPods are the static pods (ID prefix in the k8s namespace) of the control-plane
// components; kube-proxy is a DaemonSet.
var k8sStaticPods = map[string]string{
	k8sAPIServer:         "kube-system/kube-apiserver-",
	k8sControllerManager: "kube-system/kube-controller-manager-",
	k8sScheduler:         "kube-system/kube-scheduler-",
}

// K8sUpgradeListener follows a Kubernetes upgrade (implemented in Kotlin/Swift).
type K8sUpgradeListener interface {
	// OnProgress gets {"phase","index","total","node","hostname","component","message",
	// "dryRun","at"} on every change: phase is controlplane (a control plane's images, then
	// the wait for its static pods), kubelet (a node's kubelet image, then the wait for it),
	// proxy (the kube-proxy DaemonSet) or done; index is the node's position among the
	// total nodes to change; component lists the node's components changed.
	OnProgress(json string)
	// OnDone is called exactly once; errMessage is empty when every component was upgraded
	// (or, in a dry run, every change was accepted by its node).
	OnDone(errMessage string)
}

// K8sUpgradeRun is a handle on a running Kubernetes upgrade.
type K8sUpgradeRun struct {
	stop chan struct{}
	once sync.Once
}

// Cancel stops the run before its next node: a node being changed is finished and waited
// for (a control plane is never left half-way).
func (r *K8sUpgradeRun) Cancel() { r.once.Do(func() { close(r.stop) }) }

type k8sProgress struct {
	Phase     string `json:"phase"`
	Index     int    `json:"index"`
	Total     int    `json:"total"`
	Node      string `json:"node"`
	Hostname  string `json:"hostname"`
	Component string `json:"component"`
	Message   string `json:"message"`
	DryRun    bool   `json:"dryRun"`
	At        int64  `json:"at"`
}

// StartK8sUpgrade upgrades Kubernetes to toVersion (1.X.Y) like `talosctl upgrade-k8s`
// (os:admin), in K8sUpgradePlan's order, refused while the plan has blockers. Each control
// plane in turn gets the new API server, controller manager, scheduler and kube-proxy images
// in its machine config, and the run waits until its static pods run them and are Ready
// (5 min at most) before the next one. Then each node gets the new kubelet image, waiting
// until Kubernetes reports the new kubelet version and the node Ready. Last, the kube-proxy
// DaemonSet gets its new image (Talos only creates its bootstrap manifests). Only the image
// fields change; every change is first checked by its node with a dry run, and one that
// would need a reboot is refused. With dryRun nothing is applied: each node's dry run is
// reported. A failed wait stops the run. The cluster upgrade lock is held and renewed for
// the whole run. kubeServer: see KubePods.
func StartK8sUpgrade(configYAML, contextName, kubeServer, toVersion string, dryRun bool, listener K8sUpgradeListener) *K8sUpgradeRun {
	contextName = unmaskContext(configYAML, contextName)

	listener = maskedK8sUpgradeListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), k8sUpgradeTimeout)
	run := &K8sUpgradeRun{stop: make(chan struct{})}

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		from := ""

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{Action: "k8s-upgrade", Params: fmt.Sprintf("from=%s to=%s dry-run=%t", from, toVersion, dryRun)}
		}, func() (err error) {
			from, err = startK8sUpgrade(ctx, kubeTarget{configYAML, contextName, kubeServer}, toVersion, dryRun, run.stop,
				func(p k8sProgress) { emitJSON(p, listener.OnProgress) })

			return err
		})

		listener.OnDone(errText(err))
	}()

	return run
}

func startK8sUpgrade(ctx context.Context, kube kubeTarget, toVersion string, dryRun bool, stop <-chan struct{}, emit func(k8sProgress)) (string, error) {
	if isDemoContext(kube.config, kube.context) {
		return "", errDemoUnavailable
	}

	if isKubeconfig(kube.config) {
		return "", errK8sUpgradeNoTalos
	}

	s, release, err := acquireSession(kube.config, kube.context)
	if err != nil {
		return "", err
	}

	defer release()

	planCtx, planCancel := context.WithTimeout(ctx, clusterPlanTimeout(kube.config, kube.context))
	plan, err := gatherK8sPlan(planCtx, s, kube, toVersion)

	planCancel()

	if err != nil {
		return "", err
	}

	if len(plan.Blockers) > 0 {
		return plan.From, errors.New("upgrade refused: " + strings.Join(plan.Blockers, "; "))
	}

	steps := &talosK8sSteps{s: s, kube: kube}

	if !dryRun {
		steps.lock, err = takeUpgradeLock(ctx, kube,
			upgradeLockRequest{holder: newLockHolder(), hostname: "cluster", from: plan.From, to: plan.To, run: lockRunKubernetes, duration: clusterLockDuration},
			func(*upgradeLockInfo) bool { return false },
			func(msg string) {
				emit(k8sProgress{Phase: k8sPhaseControlPlane, Message: msg, At: time.Now().UnixMilli()})
			})
		if err != nil {
			return plan.From, err
		}

		defer steps.lock.release()

		renewCtx, stopRenewing := context.WithCancel(ctx)
		defer stopRenewing()

		go keepLockRenewed(renewCtx, steps.lock)
	}

	return plan.From, runK8sUpgrade(ctx, steps, plan, k8sControl{dryRun: dryRun, stop: stop, emit: emit})
}

// k8sWork is one node's change: its changed components, of one kind.
type k8sWork struct {
	kind, node, hostname string
	components           []k8sPlanStep
}

func (w k8sWork) names() string {
	names := make([]string, len(w.components))
	for i, c := range w.components {
		names[i] = c.Component
	}

	return strings.Join(names, ",")
}

// k8sWorks groups the plan's changed steps by node and kind, in the plan's order.
func k8sWorks(steps []k8sPlanStep) []k8sWork {
	var out []k8sWork

	for _, st := range steps {
		if !st.Changed {
			continue
		}

		if n := len(out); n > 0 && out[n-1].node == st.Node && out[n-1].kind == st.Kind {
			out[n-1].components = append(out[n-1].components, st)

			continue
		}

		out = append(out, k8sWork{kind: st.Kind, node: st.Node, hostname: st.Hostname, components: []k8sPlanStep{st}})
	}

	return out
}

// k8sSteps are the run's steps on the cluster; tests replace them.
type k8sSteps interface {
	// apply sets w's images in its node's config (only a dry run when dryRun).
	apply(ctx context.Context, w k8sWork, dryRun bool) error
	// wait returns once w's components run their new image (static pods or kubelet).
	wait(ctx context.Context, w k8sWork) error
	// updateProxy sets the kube-proxy DaemonSet's image; message says what was done.
	updateProxy(ctx context.Context, image string, dryRun bool) (message string, err error)
}

// k8sControl is how a run is steered and reported.
type k8sControl struct {
	dryRun bool
	stop   <-chan struct{}
	emit   func(k8sProgress)
}

// runK8sUpgrade changes each node in turn (control planes, then kubelets), waiting for each
// before the next, then kube-proxy.
func runK8sUpgrade(ctx context.Context, steps k8sSteps, plan k8sUpgradePlan, c k8sControl) error {
	works := k8sWorks(plan.Steps)
	report := func(phase string, i int, w k8sWork, message string) {
		c.emit(k8sProgress{
			Phase: phase, Index: i, Total: len(works), Node: w.node, Hostname: w.hostname,
			Component: w.names(), Message: message, DryRun: c.dryRun, At: time.Now().UnixMilli(),
		})
	}

	for i, w := range works {
		select {
		case <-c.stop:
			return errK8sCancelled
		default:
		}

		report(w.kind, i, w, fmt.Sprintf("setting %s to %s on %s", w.names(), plan.To, w.hostname))

		if err := steps.apply(ctx, w, c.dryRun); err != nil {
			return fmt.Errorf("%s: %w (the nodes before it are upgraded, the ones after it were not touched)", w.hostname, err)
		}

		if c.dryRun {
			report(w.kind, i, w, w.hostname+" accepts the change (dry run: nothing applied)")

			continue
		}

		report(w.kind, i, w, "waiting for "+w.names()+" on "+w.hostname+" to run "+plan.To)

		if err := steps.wait(ctx, w); err != nil {
			return fmt.Errorf("%s: %w (its config holds %s: check the node; the ones after it were not touched)", w.hostname, err, plan.To)
		}
	}

	if image := proxyImage(plan.Steps); image != "" {
		message, err := steps.updateProxy(ctx, image, c.dryRun)
		if err != nil {
			return fmt.Errorf("kube-proxy: %w", err)
		}

		c.emit(k8sProgress{Phase: k8sPhaseProxy, Index: len(works), Total: len(works), Component: k8sProxy, Message: message, DryRun: c.dryRun, At: time.Now().UnixMilli()})
	}

	message := "every component runs " + plan.To
	if c.dryRun {
		message = "dry run: every node accepts the change"
	}

	c.emit(k8sProgress{Phase: k8sPhaseDone, Index: len(works), Total: len(works), Message: message, DryRun: c.dryRun, At: time.Now().UnixMilli()})

	return nil
}

// proxyImage is the new kube-proxy image when the plan changes it.
func proxyImage(steps []k8sPlanStep) string {
	for _, st := range steps {
		if st.Component == k8sProxy && st.Changed {
			return st.Image
		}
	}

	return ""
}

// applyK8sImages sets w's images in the node's config, checks the change with a dry run
// (one that needs a reboot is refused), then applies it unless dryRun.
func applyK8sImages(ctx context.Context, a configApplier, w k8sWork, dryRun bool) error {
	snap, err := a.current(ctx)
	if err != nil {
		return err
	}

	provider, err := configloader.NewFromBytes(snap.raw)
	if err != nil {
		return fmt.Errorf("read machine config: %w", err)
	}

	want := map[string]string{}
	for _, c := range w.components {
		want[c.Component] = c.Image
	}

	patched, err := withK8sImages(provider, want)
	if err != nil {
		return fmt.Errorf("set the images: %w", err)
	}

	data, err := patched.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return fmt.Errorf("encode machine config: %w", err)
	}

	dry, err := a.apply(ctx, data, machineapi.ApplyConfigurationRequest_AUTO, true, 0)
	if err != nil {
		return err
	}

	if needsReboot(dry) {
		return errK8sNeedsReboot
	}

	if dryRun {
		return nil
	}

	// NO_REBOOT: the dry run said none is needed; should that change, Talos refuses.
	_, err = a.apply(ctx, data, machineapi.ApplyConfigurationRequest_NO_REBOOT, false, 0)

	return err
}

// staticPodsAt tells whether every static pod of want (ID prefix -> image tag) runs its tag
// and is Ready; waiting names the first one that does not.
func staticPodsAt(pods map[string]map[string]any, want map[string]string) (done bool, waiting string) {
	for prefix, tag := range want {
		name := strings.TrimSuffix(strings.TrimPrefix(prefix, "kube-system/"), "-")
		ok := false

		for id, status := range pods {
			if strings.HasPrefix(id, prefix) && describeStaticPod(id, status).Ready && podRunsTag(status, tag) {
				ok = true
			}
		}

		if !ok {
			return false, name
		}
	}

	return true, ""
}

// podRunsTag: every container of a raw pod status runs an image with tag.
func podRunsTag(status map[string]any, tag string) bool {
	containers := anyMaps(status["containerStatuses"])

	for _, c := range containers {
		image, _ := c["image"].(string) //nolint:errcheck
		if _, t := splitImageRef(image); t != tag {
			return false
		}
	}

	return len(containers) > 0
}

// pollUntil checks every k8sWaitPoll until check passes, ctx ends or timeout runs out.
func pollUntil(ctx context.Context, timeout time.Duration, check func(context.Context) (bool, string)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		done, waiting := check(ctx)
		if done {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%s did not come back on the new version within %s", waiting, timeout)
		case <-time.After(k8sWaitPoll):
		}
	}
}

// talosK8sSteps are the run's steps on the cluster, under the run's lock.
type talosK8sSteps struct {
	s    *session
	kube kubeTarget
	lock *upgradeLock
}

func (t *talosK8sSteps) apply(ctx context.Context, w k8sWork, dryRun bool) error {
	if !dryRun {
		renewCtx, cancel := context.WithTimeout(ctx, callTimeout)
		_ = t.lock.renew(renewCtx, map[string]string{annotationNode: w.node, annotationHostname: w.hostname}) //nolint:errcheck // stillHeld checks it
		cancel()

		if t.lock.holder() != "" {
			if err := kubeDo(ctx, t.kube, func(ctx context.Context, k *kubeClient) error { return t.lock.stillHeld(ctx, k) }); err != nil {
				return err
			}
		}
	}

	return applyK8sImages(ctx, nodeConfigApplier{t.s, w.node}, w, dryRun)
}

func (t *talosK8sSteps) wait(ctx context.Context, w k8sWork) error {
	if w.kind == k8sKindKubelet {
		return t.waitKubelet(ctx, w)
	}

	want := map[string]string{}

	for _, c := range w.components {
		if prefix := k8sStaticPods[c.Component]; prefix != "" {
			_, want[prefix] = splitImageRef(c.Image)
		}
	}

	return pollUntil(ctx, k8sWaitTimeout, func(ctx context.Context) (bool, string) {
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()

		list, err := safe.StateListAll[*k8s.StaticPodStatus](client.WithNode(callCtx, w.node), t.s.client.COSI)
		if err != nil {
			return false, "the static pods of " + w.hostname
		}

		pods := map[string]map[string]any{}
		for status := range list.All() {
			pods[status.Metadata().ID()] = status.TypedSpec().PodStatus
		}

		return staticPodsAt(pods, want)
	})
}

// waitKubelet waits until Kubernetes reports the node's kubelet at its new version, Ready.
func (t *talosK8sSteps) waitKubelet(ctx context.Context, w k8sWork) error {
	_, tag := splitImageRef(w.components[0].Image)

	return pollUntil(ctx, k8sWaitTimeout, func(ctx context.Context) (bool, string) {
		callCtx, cancel := context.WithTimeout(ctx, callTimeout)
		defer cancel()

		name := w.hostname
		if state := fetchKubeNodeState(client.WithNode(callCtx, w.node), t.s.client); state != nil && state.Name != "" {
			name = state.Name
		}

		var obj kubeNodeObject

		err := kubeDo(callCtx, t.kube, func(ctx context.Context, k *kubeClient) error {
			return k.get(ctx, "/api/v1/nodes/"+url.PathEscape(name), &obj)
		})

		return err == nil && sameVersion(obj.Status.NodeInfo.KubeletVersion, tag) && obj.Status.Conditions.is("Ready"), "the kubelet of " + w.hostname
	})
}

func (t *talosK8sSteps) updateProxy(ctx context.Context, image string, dryRun bool) (string, error) {
	message := ""

	err := kubeDo(ctx, t.kube, func(ctx context.Context, k *kubeClient) error {
		var ds struct{}

		if err := k.get(ctx, kubeProxyPath, &ds); err != nil {
			var apiErr *kubeAPIError
			if errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound {
				message = "no kube-proxy DaemonSet in kube-system: nothing to update"

				return nil
			}

			return err
		}

		if dryRun {
			message = "the kube-proxy DaemonSet would run " + image

			return nil
		}

		patch := map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
			"containers": []map[string]string{{"name": "kube-proxy", "image": image}},
		}}}}

		message = "the kube-proxy DaemonSet runs " + image

		return k.patch(ctx, kubeProxyPath, "application/strategic-merge-patch+json", patch, nil)
	})

	if err != nil && dryRun {
		return "the kube-proxy DaemonSet could not be checked (" + err.Error() + ")", nil
	}

	return message, err
}
