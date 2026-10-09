package ichorgo

import (
	"context"
	"strings"
	"sync"
	"time"
)

// A view computed from several reads (a rollout and its pods, an object and its events),
// computed again whenever one of the lists it reads changes: watches on each of them
// (kube_watch.go) signal the change, the view is read again once the burst settles.

// liveDebounce is how long a view waits after the first change of a burst before it reads
// again: a rollout moves several pods at once.
const liveDebounce = 500 * time.Millisecond

// KubeLiveListener follows a view kept live (implemented in Kotlin/Swift).
type KubeLiveListener interface {
	// OnUpdate gets the view as JSON (what the one-shot read answers): once at the start,
	// then after each change of what it reads, at most every liveDebounce.
	OnUpdate(json string)
	// OnDone is called exactly once; errMessage is empty when cancelled.
	OnDone(errMessage string)
}

// KubeLiveRun is a handle on a view kept live.
type KubeLiveRun struct {
	cancel context.CancelFunc
}

// Cancel stops following; OnDone follows.
func (r *KubeLiveRun) Cancel() { r.cancel() }

// StartKubeRolloutWatch follows the rollout of a Deployment, StatefulSet or DaemonSet live
// (os:admin): KubeRolloutStatus's answer at the start, then again each time the workload or
// one of its pods changes, instead of polling. kubeServer: see KubePods.
func StartKubeRolloutWatch(configYAML, contextName, kubeServer, kind, namespace, name string, listener KubeLiveListener) *KubeLiveRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedKubeLiveListener{listener}
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	wk, err := findWorkloadKind(kind)
	if err == nil {
		err = validateKubeName("workload", namespace, name)
	}

	demo := func() (string, error) { return toJSON(demoRolloutStatus(wk.kind, namespace, name)) }

	return startLive(kubeTarget{configYAML, contextName, kubeServer}, err, listener, demo, func(ctx context.Context, k *kubeClient, emit func(string)) error {
		var obj appsObject
		if err := k.get(ctx, appsPath(wk, namespace, name), &obj); err != nil {
			return err
		}

		specs := []watchSpec{byName(appsPath(wk, namespace, ""), name)}

		if selector, ok := selectorQuery(obj.Spec.Selector); ok {
			specs = append(specs, watchSpec{path: scopedPath("/api/v1", namespace, "pods"), labelSelector: selector})
		}

		return liveView(ctx, k, specs, emit, func(ctx context.Context) (any, error) {
			return rolloutStatus(ctx, k, wk, namespace, name)
		})
	})
}

// StartKubeObjectWatch follows one object of any kind live (os:admin): KubeObjectSummary's
// answer at the start, then again each time the object or its events change. namespace ""
// for a cluster-scoped object. kubeServer: see KubePods.
func StartKubeObjectWatch(configYAML, contextName, kubeServer, group, version, resource, namespace, name string, listener KubeLiveListener) *KubeLiveRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedKubeLiveListener{listener}
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	ref, err := newResourceRef(group, version, resource)
	if err == nil {
		err = validateObjectName(namespace, name)
	}

	demo := func() (string, error) { return toJSON(demoObjectSummary(ref, namespace, name, time.Now())) }

	return startLive(kubeTarget{configYAML, contextName, kubeServer}, err, listener, demo, func(ctx context.Context, k *kubeClient, emit func(string)) error {
		specs := []watchSpec{
			byName(ref.path(namespace), name),
			{path: scopedPath("/api/v1", eventsNamespace(namespace), "events"), fieldSelector: "involvedObject.name=" + name},
		}

		return liveView(ctx, k, specs, emit, func(ctx context.Context) (any, error) {
			return readObjectSummary(ctx, k, ref, namespace, name)
		})
	})
}

// byName is the watch of the one object name of the collection at path.
func byName(path, name string) watchSpec {
	return watchSpec{path: strings.TrimSuffix(path, "/"), fieldSelector: "metadata.name=" + name, table: true}
}

// startLive runs follow with the context's client until it ends or the run is cancelled,
// for listener (masked by the caller); in the demo inventory it sends demo() once and waits.
// err, when set, ends the run at once.
func startLive(target kubeTarget, err error, listener KubeLiveListener, demo func() (string, error), follow func(context.Context, *kubeClient, func(string)) error) *KubeLiveRun {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()

		switch {
		case err != nil:
		case isDemoContext(target.config, target.context):
			var js string
			if js, err = demo(); err == nil {
				// "" is a view with nothing to send in the demo (a change signal).
				if js != "" {
					listener.OnUpdate(js)
				}

				<-ctx.Done()
			}
		default:
			_, err = withKubeContext(ctx, target, noResult(func(ctx context.Context, k *kubeClient) error {
				return follow(ctx, k, listener.OnUpdate)
			}))
		}

		listener.OnDone(doneMessage(ctx, err))
	}()

	return &KubeLiveRun{cancel: cancel}
}

// liveView computes the view with compute and emits it as JSON each time one of specs
// changes, liveDebounce after the first change of a burst. The first spec is the view's own
// object: its watch failing ends the view with the error, as does a compute that fails; the
// other specs (its pods, its events) failing only leaves them out of the changes seen.
func liveView(ctx context.Context, k *kubeClient, specs []watchSpec, emit func(string), compute func(context.Context) (any, error)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	changed := make(chan struct{}, 1)
	signal := func(watchEvent) error {
		select {
		case changed <- struct{}{}:
		default:
		}

		return nil
	}

	ended := make(chan error, 1)

	var wg sync.WaitGroup

	for i, spec := range specs {
		wg.Go(func() {
			err := watchList(ctx, k, spec, signal)
			if i == 0 {
				ended <- err
			}
		})
	}

	err := followChanges(ctx, changed, ended, emit, compute)

	cancel()
	wg.Wait()

	return err
}

// followChanges is liveView's loop: a read per settled burst of changes.
func followChanges(ctx context.Context, changed <-chan struct{}, ended <-chan error, emit func(string), compute func(context.Context) (any, error)) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-ended:
			return err
		case <-changed:
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(liveDebounce):
		}

		// What arrived while waiting is part of this read.
		select {
		case <-changed:
		default:
		}

		view, err := compute(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			return err
		}

		emitJSON(view, emit)
	}
}
