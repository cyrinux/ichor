package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Waiting for a Helm release after a rollback, as `helm rollback --wait`: the Deployments,
// StatefulSets and DaemonSets of its deployed revision are read every few seconds until
// every pod runs their latest template and is ready (`kubectl rollout status`), one of them
// exceeded its progress deadline, or helmWaitTimeout passed. It is a run of its own, after
// the rollback: a slow rollout never leaves the release pending-rollback. Reads only:
// get/list on the release's Secrets, its apps/v1 workloads, their ReplicaSets and pods.

// Phases of a followed rollback (HelmRollbackListener.OnProgress), the last one its result.
const (
	helmWaitRunning   = "Running"
	helmWaitSucceeded = "Succeeded"
	helmWaitFailed    = "Failed"
	helmWaitTimedOut  = "Timeout"
)

var (
	// helmWaitInterval is the pause between two reads of the workloads.
	helmWaitInterval = 3 * time.Second
	// helmWaitTimeout bounds a wait, Helm's default --timeout.
	helmWaitTimeout = 5 * time.Minute
	// helmDemoStep is the pause between two steps of the demo rollout.
	helmDemoStep = 1500 * time.Millisecond
)

// HelmRollbackListener follows the workloads of a release after a rollback (implemented in
// Kotlin/Swift).
type HelmRollbackListener interface {
	// OnProgress gets {"phase","done","total","workloads":[{"kind","namespace","name",
	// "ready","desired","done","failed","manual"}],"at"} on every change: phase Running,
	// Succeeded, Failed or Timeout; done counts the workloads done (manual ones included);
	// at is unix ms.
	OnProgress(json string)
	// OnDone is called exactly once: result is Succeeded, Failed or Timeout; errMessage why
	// following failed. Both are empty after Cancel.
	OnDone(result, errMessage string)
}

// HelmRollbackRun is a handle on a followed rollback.
type HelmRollbackRun struct {
	cancel context.CancelFunc
}

// Cancel stops following (the rollback is done already); OnDone follows with empty
// arguments.
func (r *HelmRollbackRun) Cancel() { r.cancel() }

type helmRollbackWorkload struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Ready     int    `json:"ready"`
	Desired   int    `json:"desired"`
	Done      bool   `json:"done"`
	Failed    bool   `json:"failed"`
	// Manual: pods are replaced only when deleted (OnDelete, StatefulSet partition); counted
	// as done.
	Manual bool `json:"manual"`
}

type helmRollbackProgress struct {
	Phase     string                 `json:"phase"`
	Done      int                    `json:"done"`
	Total     int                    `json:"total"`
	Workloads []helmRollbackWorkload `json:"workloads"`
	At        int64                  `json:"at"`
}

// same: p and o differ only by their time.
func (p helmRollbackProgress) same(o helmRollbackProgress) bool {
	return p.Phase == o.Phase && p.Done == o.Done && p.Total == o.Total && slices.Equal(p.Workloads, o.Workloads)
}

// StartHelmRollbackFollow waits for the workloads of the Helm release namespace/name (its
// deployed revision, so the one a rollback just recorded) to be rolled out and ready,
// reading them every 3 s for 5 minutes at most (os:admin). Reads only; a release whose
// latest revision is not deployed is refused. In the demo it plays two workloads becoming
// ready. kubeServer: see KubePods.
func StartHelmRollbackFollow(configYAML, contextName, kubeServer, namespace, name string, listener HelmRollbackListener) *HelmRollbackRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedHelmRollbackListener{listener}
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	ctx, cancel := context.WithCancel(context.Background())

	var last *helmRollbackProgress

	emit := func(p helmRollbackProgress) {
		if last != nil && last.same(p) {
			return
		}

		p.At = time.Now().UnixMilli()
		last = &p
		emitJSON(p, listener.OnProgress)
	}

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", msg) })

		var (
			result string
			err    error
		)

		switch invalid := validateKubeName("Helm release", namespace, name); {
		case invalid != nil:
			err = invalid
		case isDemoContext(configYAML, contextName):
			result, err = runDemoHelmRollbackFollow(ctx, namespace, name, emit)
		default:
			result, err = followHelmRollback(ctx, kubeTarget{configYAML, contextName, kubeServer}, namespace, name, emit)
		}

		if ctx.Err() != nil {
			listener.OnDone("", "")

			return
		}

		listener.OnDone(result, errText(err))
	}()

	return &HelmRollbackRun{cancel: cancel}
}

// followHelmRollback lists the workloads of the deployed revision, then reads them every
// helmWaitInterval until they are all done, one failed, or helmWaitTimeout passed (Timeout,
// no error).
func followHelmRollback(ctx context.Context, target kubeTarget, namespace, name string, emit func(helmRollbackProgress)) (string, error) {
	bounded, cancel := context.WithTimeout(ctx, helmWaitTimeout)
	defer cancel()

	readCtx, cancelRead := context.WithTimeout(bounded, followReadTimeout)
	workloads, err := withKubeContext(readCtx, target, func(ctx context.Context, k *kubeClient) ([]helmObject, error) {
		return deployedHelmWorkloads(ctx, k, namespace, name)
	})

	cancelRead()

	if err != nil {
		return "", err
	}

	if len(workloads) == 0 {
		emit(helmRollbackProgress{Phase: helmWaitSucceeded, Workloads: []helmRollbackWorkload{}})

		return helmWaitSucceeded, nil
	}

	p := helmRollbackProgress{Phase: helmWaitRunning, Total: len(workloads), Workloads: []helmRollbackWorkload{}}

	err = pollFollow(bounded, target, helmWaitInterval, func(ctx context.Context, k *kubeClient) (bool, error) {
		next, err := readHelmWorkloads(ctx, k, workloads)
		if err != nil {
			return false, err
		}

		p = next
		emit(p)

		return p.Phase != helmWaitRunning, nil
	})

	if ctx.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded) {
		p.Phase = helmWaitTimedOut
		emit(p)

		return helmWaitTimedOut, nil
	}

	if err != nil {
		return "", err
	}

	return p.Phase, nil
}

// deployedHelmWorkloads are the workloads of the latest revision of release namespace/name,
// which must be deployed.
func deployedHelmWorkloads(ctx context.Context, k *kubeClient, namespace, name string) ([]helmObject, error) {
	refs, err := helmSecretRefs(ctx, k, namespace, name)
	if err != nil {
		return nil, err
	}

	refs = helmHistoryRefs(refs, name)
	if len(refs) == 0 {
		return nil, fmt.Errorf("no Helm release %s/%s", namespace, name)
	}

	stored, err := readHelmStored(ctx, k, refs[0])
	if err != nil {
		return nil, fmt.Errorf("revision %d: %w", refs[0].revision, err)
	}

	if status := cmp.Or(stored.rel.Info.Status, refs[0].status); status != helmStatusDeployed {
		return nil, fmt.Errorf("revision %d of %s is %s, not deployed", refs[0].revision, name, status)
	}

	objs, err := helmObjects(ctx, newKubeKinds(k), stored.rel.Manifest, cmp.Or(stored.rel.Namespace, namespace))
	if err != nil {
		return nil, err
	}

	return helmWorkloads(objs), nil
}

// readHelmWorkloads reads the rollout of each workload: Failed as soon as one failed,
// Succeeded once all are done.
func readHelmWorkloads(ctx context.Context, k *kubeClient, workloads []helmObject) (helmRollbackProgress, error) {
	p := helmRollbackProgress{Phase: helmWaitRunning, Total: len(workloads), Workloads: make([]helmRollbackWorkload, 0, len(workloads))}

	for _, o := range workloads {
		wk, err := findWorkloadKind(o.Kind)
		if err != nil {
			return helmRollbackProgress{}, err
		}

		st, err := rolloutStatus(ctx, k, wk, o.Namespace, o.Name)
		if err != nil {
			return helmRollbackProgress{}, fmt.Errorf("%s: %w", o.label(), err)
		}

		p.Workloads = append(p.Workloads, helmRollbackWorkload{
			Kind: wk.kind, Namespace: o.Namespace, Name: o.Name,
			Ready: int(st.Workload.Ready), Desired: int(st.Workload.Desired),
			Done: st.Done, Failed: st.Failed, Manual: st.Manual,
		})
	}

	return withHelmWaitPhase(p), nil
}

// withHelmWaitPhase counts the workloads done and sets the phase from them.
func withHelmWaitPhase(p helmRollbackProgress) helmRollbackProgress {
	p.Done = 0
	failed := false

	for _, w := range p.Workloads {
		if w.Done || w.Manual {
			p.Done++
		}

		failed = failed || w.Failed
	}

	switch {
	case failed:
		p.Phase = helmWaitFailed
	case p.Done == p.Total:
		p.Phase = helmWaitSucceeded
	default:
		p.Phase = helmWaitRunning
	}

	return p
}

// demoHelmRollbackWorkloads are the demo release's workloads, none ready yet.
func demoHelmRollbackWorkloads(namespace, name string) []helmRollbackWorkload {
	return []helmRollbackWorkload{
		{Kind: "Deployment", Namespace: namespace, Name: name, Desired: 2},
		{Kind: "StatefulSet", Namespace: namespace, Name: name + "-cache", Desired: 1},
	}
}

// demoHelmRollbackSteps is the demo rollout: the Deployment's two pods, then the
// StatefulSet's one, ready one after the other.
func demoHelmRollbackSteps(namespace, name string) []helmRollbackProgress {
	ready := [][2]int{{0, 0}, {1, 0}, {2, 0}, {2, 0}, {2, 1}}
	steps := make([]helmRollbackProgress, 0, len(ready))

	for _, r := range ready {
		ws := demoHelmRollbackWorkloads(namespace, name)
		for i := range ws {
			ws[i].Ready = r[i]
			ws[i].Done = ws[i].Ready == ws[i].Desired
		}

		steps = append(steps, withHelmWaitPhase(helmRollbackProgress{Total: len(ws), Workloads: ws}))
	}

	return steps
}

// runDemoHelmRollbackFollow plays demoHelmRollbackSteps, helmDemoStep apart (about 6 s).
func runDemoHelmRollbackFollow(ctx context.Context, namespace, name string, emit func(helmRollbackProgress)) (string, error) {
	steps := demoHelmRollbackSteps(namespace, name)

	for i, p := range steps {
		if i > 0 {
			if err := waitFollow(ctx, helmDemoStep); err != nil {
				return "", err
			}
		}

		emit(p)
	}

	return steps[len(steps)-1].Phase, nil
}
