package ichorgo

import (
	"context"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/kube/helm"
)

// Waiting for a Helm release after a rollback: the logic is in go/kube/helm; here are the
// listener the apps implement, the dedup of what they hear and the demo rollout.

// helmDemoStep is the pause between two steps of the demo rollout.
var helmDemoStep = 1500 * time.Millisecond

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

	var last *helm.Progress

	emit := func(p helm.Progress) {
		if last != nil && last.Same(p) {
			return
		}

		p.At = time.Now().UnixMilli()
		last = &p
		emitJSON(p, listener.OnProgress)
	}

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", msg) })
		defer holdCalls(configYAML, contextName)()

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
			result, err = helm.Follow(ctx, helmFollowClient(kubeTarget{configYAML, contextName, kubeServer}), followFatal, namespace, name, emit)
		}

		if ctx.Err() != nil {
			listener.OnDone("", "")

			return
		}

		listener.OnDone(result, errText(err))
	}()

	return &HelmRollbackRun{cancel: cancel}
}

// helmFollowClient gives helm.Follow target's client for each read: a call that shows the
// client no longer works drops it.
func helmFollowClient(target kubeTarget) helm.With {
	return func(ctx context.Context, fn func(context.Context, helm.Env) error) error {
		return kubeDo(ctx, target, func(ctx context.Context, k *kubeClient) error { return fn(ctx, helmEnv(k)) })
	}
}

// demoHelmRollbackWorkloads are the demo release's workloads, none ready yet.
func demoHelmRollbackWorkloads(namespace, name string) []helm.Workload {
	return []helm.Workload{
		{Kind: "Deployment", Namespace: namespace, Name: name, Desired: 2},
		{Kind: "StatefulSet", Namespace: namespace, Name: name + "-cache", Desired: 1},
	}
}

// demoHelmRollbackSteps is the demo rollout: the Deployment's two pods, then the
// StatefulSet's one, ready one after the other.
func demoHelmRollbackSteps(namespace, name string) []helm.Progress {
	ready := [][2]int{{0, 0}, {1, 0}, {2, 0}, {2, 0}, {2, 1}}
	steps := make([]helm.Progress, 0, len(ready))

	for _, r := range ready {
		ws := demoHelmRollbackWorkloads(namespace, name)
		for i := range ws {
			ws[i].Ready = r[i]
			ws[i].Done = ws[i].Ready == ws[i].Desired
		}

		steps = append(steps, helm.WithWaitPhase(helm.Progress{Total: len(ws), Workloads: ws}))
	}

	return steps
}

// runDemoHelmRollbackFollow plays demoHelmRollbackSteps, helmDemoStep apart (about 6 s).
func runDemoHelmRollbackFollow(ctx context.Context, namespace, name string, emit func(helm.Progress)) (string, error) {
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
