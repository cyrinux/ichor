package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Following a Flux reconcile started from the phone. Flux has no operation object: the
// reconcile request is the reconcile.fluxcd.io/requestedAt annotation, which the controller
// copies to status.lastHandledReconcileAt once it handled it; the Ready condition then says
// how it went. The object is read every few seconds until then, at most fluxFollowTimeout:
// a reconcile still running by then is reported as such, not as a failure.

// Phases of a followed reconcile (FluxReconcileListener.OnProgress), and the result of one
// that outlived the follow.
const (
	fluxFollowReconciling = "reconciling"
	fluxFollowReady       = "ready"
	fluxFollowFailed      = "failed"
	fluxFollowStill       = "still reconciling"
)

var (
	// fluxFollowInterval is the pause between two reads of the followed object.
	fluxFollowInterval = 3 * time.Second
	// fluxFollowTimeout bounds a follow.
	fluxFollowTimeout = 10 * time.Minute
	// fluxDemoStep is the pause between two steps of the demo reconcile.
	fluxDemoStep = 1500 * time.Millisecond
)

// FluxReconcileListener follows a Flux reconcile (implemented in Kotlin/Swift).
type FluxReconcileListener interface {
	// OnProgress gets {"phase","message","at"} on every change: phase reconciling, ready or
	// failed, message the Ready condition's; at is unix ms.
	OnProgress(json string)
	// OnDone is called exactly once: result is ready, failed, or "still reconciling" when the
	// reconcile outlived the 10-minute follow; errMessage why following failed. Both are
	// empty after Cancel.
	OnDone(result, errMessage string)
}

// FluxReconcileRun is a handle on a followed Flux reconcile.
type FluxReconcileRun struct {
	cancel context.CancelFunc
}

// Cancel stops following (the reconcile goes on); OnDone follows with empty arguments.
func (r *FluxReconcileRun) Cancel() { r.cancel() }

type fluxReconcileProgress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	At      int64  `json:"at"`
}

// fluxFollowObject is what a follow reads of a Flux object of any kind.
type fluxFollowObject struct {
	Metadata struct {
		fluxMeta
		Generation int64 `json:"generation"`
	} `json:"metadata"`
	Spec struct {
		Suspend bool `json:"suspend"`
	} `json:"spec"`
	Status struct {
		fluxCommonStatus
		ObservedGeneration int64 `json:"observedGeneration"`
	} `json:"status"`
}

// StartFluxReconcileFollow follows the reconcile of the Flux object kind namespace/name the
// phone just requested with KubeFluxAction (os:admin), reading it every 3 s until the
// controller handled the request, for 10 minutes at most. Reads only. kind is
// Kustomization, HelmRelease, HelmChart, GitRepository, OCIRepository, HelmRepository or
// Bucket. In the demo it plays a short reconcile. kubeServer: see KubePods.
func StartFluxReconcileFollow(configYAML, contextName, kubeServer, kind, namespace, name string, listener FluxReconcileListener) *FluxReconcileRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedFluxReconcileListener{listener}
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	ctx, cancel := context.WithCancel(context.Background())

	var last *fluxReconcileProgress

	emit := func(p fluxReconcileProgress) {
		if last != nil && last.Phase == p.Phase && last.Message == p.Message {
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

		switch invalid := validateFluxFollow(kind, namespace, name); {
		case invalid != nil:
			err = invalid
		case isDemoContext(configYAML, contextName):
			result, err = runDemoFluxReconcile(ctx, emit)
		default:
			result, err = followFluxReconcile(ctx, kubeTarget{configYAML, contextName, kubeServer}, kind, namespace, name, emit)
		}

		if ctx.Err() != nil {
			listener.OnDone("", "")

			return
		}

		listener.OnDone(result, errText(err))
	}()

	return &FluxReconcileRun{cancel: cancel}
}

func validateFluxFollow(kind, namespace, name string) error {
	if _, ok := fluxKinds[kind]; !ok {
		return fmt.Errorf("unsupported Flux kind %q", kind)
	}

	return validateKubeName(strings.ToLower(kind), namespace, name)
}

// followFluxReconcile reads the object every fluxFollowInterval until the reconcile was
// handled, or fluxFollowTimeout passed (fluxFollowStill, no error).
func followFluxReconcile(ctx context.Context, target kubeTarget, kind, namespace, name string, emit func(fluxReconcileProgress)) (string, error) {
	bounded, cancel := context.WithTimeout(ctx, fluxFollowTimeout)
	defer cancel()

	var result string

	err := pollFollow(bounded, target, fluxFollowInterval, func(ctx context.Context, k *kubeClient) (bool, error) {
		groups, err := readAPIGroups(ctx, k)
		if err != nil {
			return false, err
		}

		path, err := fluxPath(groups, kind, namespace, name)
		if err != nil {
			return false, err
		}

		var obj fluxFollowObject
		if err := k.get(ctx, path, &obj); err != nil {
			return false, err
		}

		p := fluxReconcileProgressOf(obj)
		emit(p)

		if p.Phase == fluxFollowReconciling {
			return false, nil
		}

		result = p.Phase

		return true, nil
	})

	if ctx.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded) {
		return fluxFollowStill, nil
	}

	return result, err
}

// fluxReconcileProgressOf is where the reconcile of obj stands. The request is handled once
// the controller echoed the requestedAt annotation back; without one (nothing to compare),
// once the controller observed the object's generation. Ready Unknown is still running.
func fluxReconcileProgressOf(obj fluxFollowObject) fluxReconcileProgress {
	ready := obj.Status.Conditions.get("Ready")
	msg := ready.Message

	if obj.Spec.Suspend {
		return fluxReconcileProgress{Phase: fluxFollowFailed, Message: "suspended: resume it first"}
	}

	req := obj.Metadata.Annotations[fluxRequestedAt]

	handled := obj.Status.LastHandledReconcileAt == req
	if req == "" {
		handled = obj.Status.ObservedGeneration >= obj.Metadata.Generation
	}

	switch {
	case !handled || ready.Status == "Unknown" || ready.Status == "":
		if r := obj.Status.Conditions.get("Reconciling"); r.Status == "True" && r.Message != "" {
			msg = r.Message
		}

		return fluxReconcileProgress{Phase: fluxFollowReconciling, Message: msg}
	case ready.Status == "True":
		return fluxReconcileProgress{Phase: fluxFollowReady, Message: msg}
	default:
		return fluxReconcileProgress{Phase: fluxFollowFailed, Message: msg}
	}
}

// demoFluxReconcile is the demo's reconcile: a fetch, an apply, then Ready.
func demoFluxReconcile() []fluxReconcileProgress {
	return []fluxReconcileProgress{
		{Phase: fluxFollowReconciling, Message: "fetching the latest revision"},
		{Phase: fluxFollowReconciling, Message: "applying the manifests"},
		{Phase: fluxFollowReady, Message: "Applied revision: main@sha1:3f2a9c1"},
	}
}

// runDemoFluxReconcile plays demoFluxReconcile, fluxDemoStep apart.
func runDemoFluxReconcile(ctx context.Context, emit func(fluxReconcileProgress)) (string, error) {
	steps := demoFluxReconcile()

	for i, p := range steps {
		if i > 0 {
			if err := waitFollow(ctx, fluxDemoStep); err != nil {
				return "", err
			}
		}

		emit(p)
	}

	return steps[len(steps)-1].Phase, nil
}
