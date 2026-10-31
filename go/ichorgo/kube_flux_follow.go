package ichorgo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/kube"
	"github.com/cyrinux/ichor/go/kube/flux"
)

// Following a Flux reconcile started from the phone: the logic is in go/kube/flux; here are the
// listener the apps implement, the dedup of what they hear and the demo reconcile.

// fluxDemoStep is the pause between two steps of the demo reconcile.
var fluxDemoStep = 1500 * time.Millisecond

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

	var last *flux.Progress

	emit := func(p flux.Progress) {
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
		defer holdCalls(configYAML, contextName)()

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
			result, err = flux.Follow(ctx, fluxFollowClient(kubeTarget{configYAML, contextName, kubeServer}), followFatal, kind, namespace, name, emit)
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
	if !flux.HasKind(kind) {
		return fmt.Errorf("unsupported Flux kind %q", kind)
	}

	return validateKubeName(strings.ToLower(kind), namespace, name)
}

// fluxFollowClient gives flux.Follow target's client for each read: a call that shows the
// client no longer works drops it.
func fluxFollowClient(target kubeTarget) flux.With {
	return func(ctx context.Context, fn func(context.Context, kube.Client) error) error {
		return kubeDo(ctx, target, func(ctx context.Context, k *kubeClient) error { return fn(ctx, k.api()) })
	}
}

// demoFluxReconcile is the demo's reconcile: a fetch, an apply, then Ready.
func demoFluxReconcile() []flux.Progress {
	return []flux.Progress{
		{Phase: flux.FollowReconciling, Message: "fetching the latest revision"},
		{Phase: flux.FollowReconciling, Message: "applying the manifests"},
		{Phase: flux.FollowReady, Message: "Applied revision: main@sha1:3f2a9c1"},
	}
}

// runDemoFluxReconcile plays demoFluxReconcile, fluxDemoStep apart.
func runDemoFluxReconcile(ctx context.Context, emit func(flux.Progress)) (string, error) {
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
