package ichorgo

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cyrinux/ichor/go/kube"
	"github.com/cyrinux/ichor/go/kube/argocd"
)

// Following an Argo CD sync started from the phone: the logic is in go/kube/argocd (Follow);
// here are the listener the apps implement, the run handle, the dedup of what they hear and
// the demo sync.

const (
	argoSyncSucceeded = argocd.SyncSucceeded
	argoSyncPending   = argocd.SyncPending
	argoSyncRunning   = argocd.SyncRunning
)

type argoSyncProgress = argocd.SyncProgress

// argoDemoStep is the pause between two steps of the demo sync.
var argoDemoStep = 1500 * time.Millisecond

// ArgoSyncListener follows an Argo CD sync (implemented in Kotlin/Swift).
type ArgoSyncListener interface {
	// OnProgress gets {"phase","message","wave","waves","done","total","at"} on every change:
	// phase Pending (requested, not started yet), Running, Terminating, then the final one;
	// wave is the position (1-based) of the current sync wave among the app's waves, done
	// and total its resources synced so far; at is unix ms.
	OnProgress(json string)
	// OnDone is called exactly once: result is the final phase (Succeeded, Failed, Error, or
	// Terminated after Terminate), errMessage why following failed. Both are empty after
	// Cancel.
	OnDone(result, errMessage string)
}

// ArgoSyncRun is a handle on a followed Argo CD sync.
type ArgoSyncRun struct {
	cancel     context.CancelFunc
	target     kubeTarget
	namespace  string
	name       string
	terminated atomic.Bool
}

// Cancel stops following (the sync goes on); OnDone follows with empty arguments.
func (r *ArgoSyncRun) Cancel() { r.cancel() }

// Terminate stops the sync, as KubeArgoAction's terminate does (refused in the demo and
// when no sync runs); the follow goes on until the operation ends, as Terminated.
func (r *ArgoSyncRun) Terminate() (err error) {
	defer maskErr(&err)
	defer recordAction(&err, r.target.config, r.target.context, auditAction{Server: r.target.server, Action: "argo-" + argoActionTerminate, Namespace: r.namespace, Object: "Application/" + r.name})

	if err := validateKubeName("application", r.namespace, r.name); err != nil {
		return err
	}

	r.terminated.Store(true)

	return runArgoAction(r.target, r.namespace, r.name, argoActionTerminate, argoSyncOptions{})
}

// sameArgoProgress: p and o differ only by their time.
func sameArgoProgress(p, o argoSyncProgress) bool {
	p.At = o.At

	return p == o
}

// StartArgoSyncFollow follows the sync of the Argo CD Application namespace/name the phone
// just started with KubeArgoAction (os:admin), reading it every 3 s until the operation
// ends. Reads only; Terminate on the run stops the sync. In the demo it plays a 4-wave sync.
// kubeServer: see KubePods.
func StartArgoSyncFollow(configYAML, contextName, kubeServer, namespace, name string, listener ArgoSyncListener) *ArgoSyncRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedArgoSyncListener{listener}
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	ctx, cancel := context.WithCancel(context.Background())
	run := &ArgoSyncRun{cancel: cancel, target: kubeTarget{configYAML, contextName, kubeServer}, namespace: namespace, name: name}

	var last *argoSyncProgress

	emit := func(p argoSyncProgress) {
		if last != nil && sameArgoProgress(*last, p) {
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

		switch invalid := validateKubeName("application", namespace, name); {
		case invalid != nil:
			err = invalid
		case isDemoContext(configYAML, contextName):
			result, err = runDemoArgoSync(ctx, emit)
		default:
			result, err = followArgoSync(ctx, run, time.Now(), emit)
		}

		if ctx.Err() != nil {
			listener.OnDone("", "")

			return
		}

		listener.OnDone(result, errText(err))
	}()

	return run
}

// followArgoSync follows run's app with a fresh client per read, as pollFollow does.
func followArgoSync(ctx context.Context, run *ArgoSyncRun, since time.Time, emit func(argoSyncProgress)) (string, error) {
	with := func(ctx context.Context, fn func(context.Context, kube.Client) error) error {
		_, err := withKubeContext(ctx, run.target, func(ctx context.Context, k *kubeClient) (struct{}, error) {
			return struct{}{}, fn(ctx, k.api())
		})

		return err
	}

	return argocd.Follow(ctx, with, followFatal, run.namespace, run.name, since, run.terminated.Load, emit)
}

// demoArgoSync is the demo's sync: four waves of three resources each, then Succeeded.
func demoArgoSync() []argoSyncProgress {
	const waves, perWave = 4, 3

	steps := []argoSyncProgress{{Phase: argoSyncPending}}

	for w := 1; w <= waves; w++ {
		steps = append(steps, argoSyncProgress{
			Phase: argoSyncRunning, Message: fmt.Sprintf("waiting for healthy state of wave %d", w-1),
			Wave: w, Waves: waves, Done: (w - 1) * perWave, Total: waves * perWave,
		})
	}

	return append(steps, argoSyncProgress{
		Phase: argoSyncSucceeded, Message: "successfully synced (all tasks run)",
		Wave: waves, Waves: waves, Done: waves * perWave, Total: waves * perWave,
	})
}

// runDemoArgoSync plays demoArgoSync, argoDemoStep apart.
func runDemoArgoSync(ctx context.Context, emit func(argoSyncProgress)) (string, error) {
	steps := demoArgoSync()

	for i, p := range steps {
		if i > 0 {
			if err := waitFollow(ctx, argoDemoStep); err != nil {
				return "", err
			}
		}

		emit(p)
	}

	return steps[len(steps)-1].Phase, nil
}
