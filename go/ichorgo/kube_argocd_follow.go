package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// Following an Argo CD sync started from the phone: the Application is read every few
// seconds, as the app detail reads it (its operation, sync waves and per-resource results),
// until the operation ends. Only the operation the phone started counts: one that started
// before the follow (the last sync, or one a webhook ran) is ignored, so a follow that
// starts before the controller picked the sync up waits for the new one.

// Results of a followed sync (ArgoSyncListener.OnDone): the operation's final phase, or
// argoSyncTerminated when it ended after a Terminate from the run.
const (
	argoSyncSucceeded  = "Succeeded"
	argoSyncFailed     = "Failed"
	argoSyncError      = "Error"
	argoSyncTerminated = "Terminated"
	argoSyncPending    = "Pending" // the sync was requested, the controller has not started it
	argoSyncRunning    = "Running"
)

var (
	// argoFollowInterval is the pause between two reads of the followed app.
	argoFollowInterval = 3 * time.Second
	// argoFollowGrace is how many reads a follow waits for the sync it was started for to
	// show up before it reports whatever operation finished last.
	argoFollowGrace = 10
	// argoFollowSkew is how much older than the follow's start (the phone's clock against
	// the cluster's, second-rounded) an operation's start may be and still be the phone's.
	argoFollowSkew = 5 * time.Second
	// argoDemoStep is the pause between two steps of the demo sync.
	argoDemoStep = 1500 * time.Millisecond
)

var errArgoSyncNotStarted = &kubeAPIError{Code: 409, Reason: "NotStarted", Message: "Argo CD has not started the sync"}

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

type argoSyncProgress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	Wave    int    `json:"wave"`
	Waves   int    `json:"waves"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	At      int64  `json:"at"`
}

// same: p and o differ only by their time.
func (p argoSyncProgress) same(o argoSyncProgress) bool {
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

// argoFollow is the state of a follow between two reads.
type argoFollow struct {
	since time.Time
	reads int
	// stale is the start of the operation found finished while a new one was requested:
	// the last sync, not the phone's.
	stale int64
}

// mine tells whether op is the operation the phone started.
func (f *argoFollow) mine(op *argoOperation) bool {
	return op != nil && op.StartedAt != f.stale && op.StartedAt >= f.since.Add(-argoFollowSkew).UnixMilli()
}

// followArgoSync reads the app every argoFollowInterval until the phone's operation ends;
// it returns the result for OnDone.
func followArgoSync(ctx context.Context, run *ArgoSyncRun, since time.Time, emit func(argoSyncProgress)) (string, error) {
	f := &argoFollow{since: since}

	var result string

	err := pollFollow(ctx, run.target, argoFollowInterval, func(ctx context.Context, k *kubeClient) (bool, error) {
		app, pending, err := readArgoApp(ctx, k, run.namespace, run.name)
		if err != nil {
			return false, err
		}

		var done bool

		result, done, err = f.step(app.Operation, pending, run.terminated.Load(), emit)

		return done, err
	})

	return result, err
}

// step takes one read of the app: its operation op, and pending when a sync was requested
// and not picked up yet. It returns the result once done.
func (f *argoFollow) step(op *argoOperation, pending, terminated bool, emit func(argoSyncProgress)) (string, bool, error) {
	f.reads++

	if pending && op != nil && op.Phase != argoSyncRunning && op.Phase != "Terminating" {
		f.stale = op.StartedAt
	}

	if !f.mine(op) {
		if f.reads <= argoFollowGrace {
			emit(argoSyncProgress{Phase: argoSyncPending})

			return "", false, nil
		}

		// The sync never showed: report what finished last, if anything did.
		if op == nil || pending {
			return "", true, errArgoSyncNotStarted
		}
	}

	p := argoSyncProgressOf(op)
	running := op.Phase == argoSyncRunning || op.Phase == "Terminating"

	if !running && terminated && p.Phase == argoSyncFailed {
		p.Phase = argoSyncTerminated
	}

	emit(p)

	if running {
		return "", false, nil
	}

	return p.Phase, true, nil
}

func argoSyncProgressOf(op *argoOperation) argoSyncProgress {
	p := argoSyncProgress{Phase: op.Phase, Message: op.Message, Waves: len(op.Waves), Done: op.Done, Total: op.Total}

	if i := slices.Index(op.Waves, op.Wave); i >= 0 {
		p.Wave = i + 1
	}

	if strings.Contains(strings.ToLower(op.Message), "terminated") && op.Phase == argoSyncFailed {
		p.Phase = argoSyncTerminated
	}

	return p
}

// readArgoApp reads one Application as the listing maps it, and whether a sync was
// requested that the controller has not started yet.
func readArgoApp(ctx context.Context, k *kubeClient, namespace, name string) (argoApp, bool, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return argoApp{}, false, err
	}

	version, ok := groups[groupArgo]
	if !ok {
		return argoApp{}, false, &kubeAPIError{Code: 404, Reason: "NotFound", Message: "Argo CD is not installed"}
	}

	var o argoObject
	if err := k.get(ctx, "/apis/"+groupArgo+"/"+version+"/namespaces/"+url.PathEscape(namespace)+"/applications/"+url.PathEscape(name), &o); err != nil {
		return argoApp{}, false, err
	}

	return mapArgoApp(o, nil), len(o.Operation) > 0 && string(o.Operation) != "null", nil
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
