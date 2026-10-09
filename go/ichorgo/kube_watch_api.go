package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// KubeWatchListener follows a list kept live (implemented in Kotlin/Swift).
type KubeWatchListener interface {
	// OnEvent gets one change. eventType SYNC carries the whole list as JSON, at the start
	// and again whenever the watch had to start over: a kubePodPage for pods (complete, no
	// continue token), a kubeResourcePage for any other resource. ADDED, MODIFIED and DELETED
	// carry one item (a kubePod, a kubeResourceRow) the app merges into its list by namespace
	// and name.
	OnEvent(eventType, json string)
	// OnDone is called exactly once; errMessage is empty when cancelled.
	OnDone(errMessage string)
}

// KubeWatchRun is a handle on a list kept live.
type KubeWatchRun struct {
	cancel context.CancelFunc
}

// Cancel stops the watch; OnDone follows.
func (r *KubeWatchRun) Cancel() { r.cancel() }

// watchItems maps the API server's objects to what the app shows: the whole list (watchSync)
// or one item.
type watchItems func(ev watchEvent) (string, error)

// StartKubeWatch keeps the list of one resource live (os:admin): the list once (SYNC), then
// each ADDED, MODIFIED or DELETED object as the API server reports it, with bookmarks and
// the list again when the server expired its version. group "" is the core API; namespace ""
// is every namespace; labelSelector and fieldSelector narrow it as `kubectl get -l` and
// `--field-selector` do, "" for none. Pods come as kubePod, any other resource as the rows of
// its server-side Table. kubeServer: see KubePods.
func StartKubeWatch(configYAML, contextName, kubeServer, group, version, resource, namespace, labelSelector, fieldSelector string, listener KubeWatchListener) *KubeWatchRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedKubeWatchListener{listener}
	namespace = privacy.revealNamespace(strings.TrimSpace(namespace))

	ref, err := newResourceRef(group, version, resource)
	if err == nil {
		err = validateNamespace(namespace)
	}

	spec := watchSpec{
		labelSelector: privacy.reveal(strings.TrimSpace(labelSelector)),
		fieldSelector: privacy.reveal(strings.TrimSpace(fieldSelector)),
		table:         !ref.isPods(),
	}
	if err == nil {
		spec.path = ref.path(namespace)
	}

	demo := func() (string, error) {
		if ref.isPods() {
			return toJSON(demoSelectedPods("", func(p kubePod) bool { return namespace == "" || p.Namespace == namespace }))
		}

		return toJSON(demoResourcePage(ref, namespace))
	}

	return startWatch(kubeTarget{configYAML, contextName, kubeServer}, err, listener, watchItemsOf(ref), func(ctx context.Context, k *kubeClient, emit func(watchEvent) error) error {
		return watchList(ctx, k, spec, emit)
	}, demo)
}

// StartKubeWorkloadPodsWatch keeps the pods of a Deployment, StatefulSet or DaemonSet (kind)
// live (os:admin): those of its namespace its spec.selector matches, narrowed to phase as in
// KubeWorkloadPodsPage; a workload whose selector is empty has none. Events as in
// StartKubeWatch, items as kubePod.
func StartKubeWorkloadPodsWatch(configYAML, contextName, kubeServer, kind, namespace, name, phase string, listener KubeWatchListener) *KubeWatchRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedKubeWatchListener{listener}
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.revealName(strings.TrimSpace(name))

	wk, err := findWorkloadKind(kind)
	if err == nil {
		err = validateKubeName("workload", namespace, name)
	}

	var fields string
	if err == nil {
		fields, err = podFieldSelector("", phase)
	}

	demo := func() (string, error) {
		return toJSON(demoSelectedPods(phase, func(p kubePod) bool { return p.Namespace == namespace && ownedByWorkload(p.Owner, wk.kind, name) }))
	}

	return startWatch(kubeTarget{configYAML, contextName, kubeServer}, err, listener, podWatchItems, func(ctx context.Context, k *kubeClient, emit func(watchEvent) error) error {
		var obj appsObject
		if err := k.get(ctx, appsPath(wk, namespace, name), &obj); err != nil {
			return err
		}

		selector, ok := selectorQuery(obj.Spec.Selector)
		if !ok {
			if err := emit(watchEvent{Type: watchSync, Page: kubePage{items: []json.RawMessage{}}}); err != nil {
				return err
			}

			<-ctx.Done()

			return ctx.Err()
		}

		spec := watchSpec{path: scopedPath("/api/v1", namespace, "pods"), labelSelector: selector, fieldSelector: fields}

		return watchList(ctx, k, spec, emit)
	}, demo)
}

// StartKubeNodePodsWatch follows the pods of the Kubernetes node nodeName in every namespace,
// narrowed to phase ("" for all), as KubeNodePodsPage lists them. Events as in StartKubeWatch,
// items as kubePod. An account that may not list pods cluster-wide gets the list polled.
func StartKubeNodePodsWatch(configYAML, contextName, kubeServer, nodeName, phase string, listener KubeWatchListener) *KubeWatchRun {
	contextName = unmaskContext(configYAML, contextName)
	listener = maskedKubeWatchListener{listener}
	nodeName = privacy.reveal(strings.TrimSpace(nodeName))

	var err error
	if !kubeNamePattern.MatchString(nodeName) || strings.Contains(nodeName, "..") {
		err = fmt.Errorf("invalid Kubernetes node name %q", nodeName)
	}

	var fields string
	if err == nil {
		fields, err = podFieldSelector("spec.nodeName="+nodeName, phase)
	}

	demo := func() (string, error) {
		return toJSON(demoSelectedPods(phase, func(p kubePod) bool { return p.Node == nodeName }))
	}

	return startWatch(kubeTarget{configYAML, contextName, kubeServer}, err, listener, podWatchItems, func(ctx context.Context, k *kubeClient, emit func(watchEvent) error) error {
		return watchList(ctx, k, watchSpec{path: "/api/v1/pods", fieldSelector: fields}, emit)
	}, demo)
}

// startWatch runs follow with the context's client until it ends or the run is cancelled,
// mapping each event with items for listener (masked by the caller); in the demo inventory
// it sends demo() as the one SYNC and waits. err, when set, ends the run at once.
func startWatch(target kubeTarget, err error, listener KubeWatchListener, items watchItems, follow func(context.Context, *kubeClient, func(watchEvent) error) error, demo func() (string, error)) *KubeWatchRun {
	ctx, cancel := context.WithCancel(context.Background())

	emit := func(ev watchEvent) error {
		js, err := items(ev)
		if err != nil {
			return err
		}

		listener.OnEvent(ev.Type, js)

		return nil
	}

	go func() {
		defer cancel()

		switch {
		case err != nil:
		case isDemoContext(target.config, target.context):
			var js string
			if js, err = demo(); err == nil {
				listener.OnEvent(watchSync, js)
				<-ctx.Done()
			}
		default:
			_, err = withKubeContext(ctx, target, noResult(func(ctx context.Context, k *kubeClient) error {
				return follow(ctx, k, emit)
			}))
		}

		listener.OnDone(doneMessage(ctx, err))
	}()

	return &KubeWatchRun{cancel: cancel}
}

// doneMessage is what OnDone gets for err: "" when the run was cancelled (the client
// helpers wrap the context's error in their own message).
func doneMessage(ctx context.Context, err error) string {
	if err == nil || ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return ""
	}

	return err.Error()
}

// isPods tells the core pods resource, which the app shows as kubePod rather than Table rows.
func (r resourceRef) isPods() bool { return r.group == "" && r.resource == "pods" }

// watchItemsOf maps ref's objects: pods as kubePod, anything else as Table rows.
func watchItemsOf(ref resourceRef) watchItems {
	if ref.isPods() {
		return podWatchItems
	}

	return rowWatchItems
}

// podWatchItems maps a pod list to a kubePodPage, one pod to a kubePod.
func podWatchItems(ev watchEvent) (string, error) {
	pods, err := mapPodPage(ev.Page)
	if err != nil {
		return "", err
	}

	learnPodNames(pods)

	if ev.Type == watchSync {
		return toJSON(kubePodPage{Pods: pods, pageCursor: completeCursor})
	}

	if len(pods) != 1 {
		return "", fmt.Errorf("watch event %s carries %d pods", ev.Type, len(pods))
	}

	return toJSON(pods[0])
}

// rowWatchItems maps a list to a kubeResourcePage, one object to a kubeResourceRow.
func rowWatchItems(ev watchEvent) (string, error) {
	page, err := mapResourcePage(ev.Page)
	if err != nil {
		return "", err
	}

	if ev.Type == watchSync {
		page.Continue, page.Remaining = "", 0

		return toJSON(page)
	}

	if len(page.Rows) != 1 {
		return "", fmt.Errorf("watch event %s carries %d rows", ev.Type, len(page.Rows))
	}

	return toJSON(page.Rows[0])
}
