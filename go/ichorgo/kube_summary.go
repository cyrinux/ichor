package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The object summary is `kubectl describe` for any kind, CRDs included: the conditions and
// the health their worst one gives, who owns or manages the object (tappable up the chain to
// the Deployment, the Flux HelmRelease or the Argo CD Application), its metadata, a few spec
// fields and its events. Nothing in it is specific to a kind but the spec highlights.

// kubeObjectSummary is what KubeObjectSummary answers. Times are Unix milliseconds.
type kubeObjectSummary struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Namespace  string `json:"namespace"`
	Name       string `json:"name"`
	// Health is the worst tone of the conditions (or of the phase without any):
	// toneOK, toneWarn, toneBad, or toneNone when the object says nothing about it.
	Health       string `json:"health"`
	HealthReason string `json:"healthReason"`
	// Phase is status.phase when the kind has one (Pod, PVC, Namespace...).
	Phase      string             `json:"phase"`
	Conditions []summaryCondition `json:"conditions"`
	Owners     []kubeOwner        `json:"owners"`
	Labels     map[string]string  `json:"labels"`
	// Annotations without kubectl's last-applied-configuration, long values cut.
	Annotations map[string]string `json:"annotations"`
	Created     int64             `json:"created"`
	// Deleting is when its deletion was asked, 0 when none is pending.
	Deleting   int64           `json:"deleting"`
	Finalizers []string        `json:"finalizers"`
	Highlights []kubeHighlight `json:"highlights"`
	Events     []kubeEvent     `json:"events"`
	// EventsError says why the events could not be read (the role may not list them).
	EventsError string `json:"eventsError"`
}

// summaryCondition is one entry of status.conditions, with the tone it reads as.
type summaryCondition struct {
	Type           string `json:"type"`
	Status         string `json:"status"`
	Reason         string `json:"reason"`
	Message        string `json:"message"`
	LastTransition int64  `json:"lastTransition"`
	Tone           string `json:"tone"`
}

// Who owns or manages an object: an ownerReference, or a GitOps or Helm label/annotation.
const (
	ownerViaReference = "owner"
	ownerViaFlux      = "flux"
	ownerViaArgo      = "argocd"
	ownerViaHelm      = "helm"
)

// kubeOwner is an object up the chain. Group, Version and Resource are set when discovery
// knows the kind, so the app can open its summary; Via ownerViaHelm names a Helm release
// (its own screen), which no resource stands for.
type kubeOwner struct {
	Via        string   `json:"via"`
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Resource   string   `json:"resource"`
	Kind       string   `json:"kind"`
	Namespace  string   `json:"namespace"`
	Name       string   `json:"name"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs"`
	Controller bool     `json:"controller"`
	// Scalable as in kubeBrowserResource.
	Scalable bool `json:"scalable,omitempty"`
}

// kubeHighlight is a spec field worth seeing first: Key names it for the app to label.
type kubeHighlight struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// annotationValueLimit cuts long annotation values (a dashboard JSON, a checksum list).
const annotationValueLimit = 200

// KubeObjectSummary reads one object of any kind and sums it up (os:admin): conditions,
// owners and managers, metadata, spec highlights and events, as a JSON kubeObjectSummary.
// namespace "" for a cluster-scoped object. kubeServer: see KubePods.
func KubeObjectSummary(configYAML, contextName, kubeServer, group, version, resource, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	ref, err := newResourceRef(group, version, resource)
	if err != nil {
		return "", err
	}

	if err := validateObjectName(namespace, name); err != nil {
		return "", err
	}

	demo := func() kubeObjectSummary { return demoObjectSummary(ref, namespace, name, time.Now()) }

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (kubeObjectSummary, error) {
		return readObjectSummary(ctx, k, ref, namespace, name)
	})
}

// validateObjectName refuses what cannot be an object's name or namespace (they go into a path).
func validateObjectName(namespace, name string) error {
	if !kubeResourcePattern.MatchString(name) && !kubeNamePattern.MatchString(name) {
		return fmt.Errorf("invalid name %q", name)
	}

	if namespace != "" {
		return validateNamespace(namespace)
	}

	return nil
}

func readObjectSummary(ctx context.Context, k *kubeClient, ref resourceRef, namespace, name string) (kubeObjectSummary, error) {
	var obj map[string]any
	if err := k.get(ctx, ref.path(namespace)+"/"+url.PathEscape(name), &obj); err != nil {
		return kubeObjectSummary{}, err
	}

	summary := summarizeObject(obj)

	type eventsResult struct {
		list kubeEventList
		err  error
	}

	events := make(chan eventsResult, 1)

	go func() {
		list, err := readEvents(ctx, k, eventsNamespace(summary.Namespace), summary.Kind, summary.Name)
		events <- eventsResult{list, err}
	}()

	summary.Owners = resolveOwners(ctx, newKindResolver(k), obj, summary)

	if e := <-events; e.err != nil {
		summary.EventsError = e.err.Error()
	} else {
		summary.Events = e.list.Events
	}

	return summary, nil
}

// eventsNamespace is where an object's events are: a cluster-scoped object's (a Node, a
// Namespace) go to "default".
func eventsNamespace(namespace string) string {
	if namespace == "" {
		return "default"
	}

	return namespace
}

// summarizeObject reads what needs no other request from the object itself.
func summarizeObject(obj map[string]any) kubeObjectSummary {
	meta := mapAt(obj, "metadata")
	status := mapAt(obj, "status")

	s := kubeObjectSummary{
		Kind:        stringAt(obj, "kind"),
		APIVersion:  stringAt(obj, "apiVersion"),
		Namespace:   stringAt(meta, "namespace"),
		Name:        stringAt(meta, "name"),
		Phase:       stringAt(status, "phase"),
		Conditions:  readConditions(status),
		Owners:      []kubeOwner{},
		Labels:      stringMap(mapAt(meta, "labels")),
		Annotations: shownAnnotations(stringMap(mapAt(meta, "annotations"))),
		Created:     timeMilli(stringAt(meta, "creationTimestamp")),
		Deleting:    timeMilli(stringAt(meta, "deletionTimestamp")),
		Finalizers:  stringList(meta["finalizers"]),
		Highlights:  specHighlights(obj),
		Events:      []kubeEvent{},
	}

	s.Health, s.HealthReason = objectHealth(s.Conditions, s.Phase, s.Deleting != 0)

	return s
}

func shownAnnotations(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))

	for key, value := range in {
		if key == "kubectl.kubernetes.io/last-applied-configuration" {
			continue
		}

		if len(value) > annotationValueLimit {
			value = strings.ToValidUTF8(value[:annotationValueLimit], "") + "…"
		}

		out[key] = value
	}

	return out
}

// kindResolver finds the resource of a kind through discovery, each group version read once.
type kindResolver struct {
	k     *kubeClient
	lists map[string][]kubeBrowserResource
}

func newKindResolver(k *kubeClient) *kindResolver {
	return &kindResolver{k: k, lists: map[string][]kubeBrowserResource{}}
}

// byAPIVersion is the resource of kind in apiVersion ("apps/v1", "v1"), false when unknown.
func (r *kindResolver) byAPIVersion(ctx context.Context, apiVersion, kind string) (kubeBrowserResource, bool) {
	list, ok := r.lists[apiVersion]
	if !ok {
		path := "/apis/" + apiVersion
		if !strings.Contains(apiVersion, "/") {
			path = "/api/" + apiVersion
		}

		var discovered discoveryResourceList
		if err := r.k.get(ctx, path, &discovered); err == nil {
			list = listableResources(discovered)
		}

		r.lists[apiVersion] = list
	}

	i := slices.IndexFunc(list, func(res kubeBrowserResource) bool { return res.Kind == kind })
	if i < 0 {
		return kubeBrowserResource{}, false
	}

	return list[i], true
}

// byGroup is the resource of kind at the preferred version of group.
func (r *kindResolver) byGroup(ctx context.Context, group, kind string) (kubeBrowserResource, bool) {
	var g struct {
		PreferredVersion struct {
			GroupVersion string `json:"groupVersion"`
		} `json:"preferredVersion"`
	}

	if err := r.k.get(ctx, "/apis/"+group, &g); err != nil || g.PreferredVersion.GroupVersion == "" {
		return kubeBrowserResource{}, false
	}

	return r.byAPIVersion(ctx, g.PreferredVersion.GroupVersion, kind)
}

// resolveOwners lists the ownerReferences (the controller first), then the GitOps tool or
// Helm release that manages the object, each with its resource when discovery knows it.
func resolveOwners(ctx context.Context, r *kindResolver, obj map[string]any, s kubeObjectSummary) []kubeOwner {
	owners := []kubeOwner{}

	for _, ref := range ownerReferences(mapAt(obj, "metadata")) {
		owner := kubeOwner{Via: ownerViaReference, Kind: ref.kind, Name: ref.name, Controller: ref.controller, Verbs: []string{}}
		if res, ok := r.byAPIVersion(ctx, ref.apiVersion, ref.kind); ok {
			owner = withResource(owner, res, s.Namespace)
		}

		owners = append(owners, owner)
	}

	slices.SortStableFunc(owners, func(a, b kubeOwner) int {
		switch {
		case a.Controller == b.Controller:
			return 0
		case a.Controller:
			return -1
		default:
			return 1
		}
	})

	for _, m := range managers(s) {
		if m.Via == ownerViaArgo && m.Namespace == "" {
			// The object itself is the Application its label names: no need to look it up.
			if m.Kind == s.Kind && m.Name == s.Name {
				continue
			}

			m.Namespace = findArgoApplicationNamespace(ctx, r.k, m.Name)
		}

		if m.Kind == s.Kind && m.Name == s.Name && m.Namespace == s.Namespace {
			continue // An Application that manages itself (app of apps).
		}

		// A Helm release has no resource; a kind the server does not serve stays untappable.
		if m.Via != ownerViaHelm {
			if res, ok := r.byGroup(ctx, m.Group, m.Kind); ok {
				m = withResource(m, res, m.Namespace)
			}
		}

		owners = append(owners, m)
	}

	return owners
}

func withResource(o kubeOwner, res kubeBrowserResource, namespace string) kubeOwner {
	o.Group, o.Version, o.Resource, o.Namespaced, o.Verbs = res.Group, res.Version, res.Resource, res.Namespaced, nonNil(res.Verbs)
	o.Scalable = res.Scalable
	o.Namespace = ""

	if res.Namespaced {
		o.Namespace = namespace
	}

	return o
}

type ownerReference struct {
	apiVersion, kind, name string
	controller             bool
}

func ownerReferences(meta map[string]any) []ownerReference {
	raw, _ := meta["ownerReferences"].([]any)
	refs := make([]ownerReference, 0, len(raw))

	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		controller, _ := m["controller"].(bool)
		refs = append(refs, ownerReference{stringAt(m, "apiVersion"), stringAt(m, "kind"), stringAt(m, "name"), controller})
	}

	return refs
}

// managers are what Flux, Argo CD or Helm write on the objects they apply. An Argo CD
// Application's namespace is known only when the tracking id or label carries it
// ("argocd_web"); it is looked up otherwise. A Helm release is listed only when no Flux
// HelmRelease stands for it.
func managers(s kubeObjectSummary) []kubeOwner {
	out := []kubeOwner{}
	manager := func(via, group, kind, namespace, name string) kubeOwner {
		return kubeOwner{Via: via, Group: group, Kind: kind, Namespace: namespace, Name: name, Namespaced: true, Verbs: []string{}}
	}

	if name, ns := s.Labels["kustomize.toolkit.fluxcd.io/name"], s.Labels["kustomize.toolkit.fluxcd.io/namespace"]; name != "" && ns != "" {
		out = append(out, manager(ownerViaFlux, "kustomize.toolkit.fluxcd.io", "Kustomization", ns, name))
	}

	fluxHelm := false
	if name, ns := s.Labels["helm.toolkit.fluxcd.io/name"], s.Labels["helm.toolkit.fluxcd.io/namespace"]; name != "" && ns != "" {
		out = append(out, manager(ownerViaFlux, "helm.toolkit.fluxcd.io", "HelmRelease", ns, name))
		fluxHelm = true
	}

	if app := argoInstance(s); app != "" {
		ns, name, found := strings.Cut(app, "_")
		if !found {
			ns, name = "", app
		}

		out = append(out, manager(ownerViaArgo, "argoproj.io", "Application", ns, name))
	}

	if name, ns := s.Annotations["meta.helm.sh/release-name"], s.Annotations["meta.helm.sh/release-namespace"]; name != "" && ns != "" && !fluxHelm {
		out = append(out, manager(ownerViaHelm, "", "Release", ns, name))
	}

	return out
}

// argoInstance is the Application named by the tracking id ("app:group/Kind:ns/name") or,
// with label tracking, by the instance label.
func argoInstance(s kubeObjectSummary) string {
	if id := s.Annotations["argocd.argoproj.io/tracking-id"]; id != "" {
		app, _, _ := strings.Cut(id, ":")

		return app
	}

	return s.Labels["argocd.argoproj.io/instance"]
}

// findArgoApplicationNamespace is the namespace of the Application name, "" when none or
// several share it.
func findArgoApplicationNamespace(ctx context.Context, k *kubeClient, name string) string {
	if !kubeNamePattern.MatchString(name) {
		return ""
	}

	path := "/apis/argoproj.io/v1alpha1/applications?fieldSelector=" + url.QueryEscape("metadata.name="+name)

	apps, err := listObjects[struct {
		Metadata struct {
			Namespace string `json:"namespace"`
		} `json:"metadata"`
	}](ctx, k, path)
	if err != nil || len(apps) != 1 {
		return ""
	}

	return apps[0].Metadata.Namespace
}
