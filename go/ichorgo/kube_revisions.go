package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

// Rollout history, the pod template diff of a revision, and the undo of every workload kind:
// a Deployment keeps its revisions in ReplicaSets, a StatefulSet and a DaemonSet in
// ControllerRevisions. Pause and resume of a Deployment's rollout.

// workloadRevisions is a workload's rollout history, as KubeWorkloadRevisions answers it.
type workloadRevisions struct {
	Kind string `json:"kind"`
	// Manual: the pods only take a rolled back template when deleted (OnDelete, StatefulSet
	// partition).
	Manual bool `json:"manual"`
	// Paused: a paused Deployment, whose rollback is refused until its rollout resumes.
	Paused    bool                 `json:"paused"`
	Revisions []deploymentRevision `json:"revisions"`
}

// revisionEntry is one revision with the pod template it keeps (without the labels and
// directives the controllers add) and, for a ReplicaSet, its annotations.
type revisionEntry struct {
	revision    deploymentRevision
	template    map[string]any
	annotations map[string]string
}

// workloadHistory is a workload as read for its history: the object, its live pod template
// and its revisions, newest first.
type workloadHistory struct {
	kind     workloadKind
	obj      appsObject
	live     map[string]any
	revs     []revisionEntry
	manual   bool
	isPaused bool
}

func (h workloadHistory) list() workloadRevisions {
	out := workloadRevisions{Kind: h.kind.kind, Manual: h.manual, Paused: h.isPaused, Revisions: []deploymentRevision{}}
	for _, r := range h.revs {
		out.Revisions = append(out.Revisions, r.revision)
	}

	return out
}

func (h workloadHistory) find(revision int) (revisionEntry, bool) {
	for _, r := range h.revs {
		if r.revision.Revision == revision {
			return r, true
		}
	}

	return revisionEntry{}, false
}

// current is the revision the workload runs now, 0 when none is marked.
func (h workloadHistory) current() int {
	for _, r := range h.revs {
		if r.revision.Current {
			return r.revision.Revision
		}
	}

	return 0
}

// controllerRevision holds the fields of a ControllerRevision a history reads. Data is a
// partial object, {"spec":{"template":{...,"$patch":"replace"}}}: read as a map, never as
// typed structs, so nothing of the template is lost.
type controllerRevision struct {
	Metadata struct {
		Name              string            `json:"name"`
		CreationTimestamp time.Time         `json:"creationTimestamp"`
		Annotations       map[string]string `json:"annotations"`
		OwnerReferences   []struct {
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			Controller *bool  `json:"controller"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Revision int64          `json:"revision"`
	Data     map[string]any `json:"data"`
}

func (cr controllerRevision) ownedBy(kind, name string) bool {
	for _, o := range cr.Metadata.OwnerReferences {
		if o.Kind == kind && o.Name == name && o.Controller != nil && *o.Controller {
			return true
		}
	}

	return false
}

// template is the pod template the revision keeps, without the strategic merge directive.
func (cr controllerRevision) template() map[string]any {
	spec, _ := cr.Data["spec"].(map[string]any)
	t, _ := spec["template"].(map[string]any)

	out := make(map[string]any, len(t))
	for k, v := range t {
		if k != "$patch" {
			out[k] = v
		}
	}

	return out
}

// readWorkloadHistory reads the workload, its live template and its revisions.
func readWorkloadHistory(ctx context.Context, k *kubeClient, wk workloadKind, namespace, name string) (workloadHistory, error) {
	var raw json.RawMessage
	if err := k.get(ctx, appsPath(wk, namespace, name), &raw); err != nil {
		return workloadHistory{}, err
	}

	h := workloadHistory{kind: wk}

	var live struct {
		Spec struct {
			Template map[string]any `json:"template"`
		} `json:"spec"`
	}

	if err := json.Unmarshal(raw, &h.obj); err != nil {
		return workloadHistory{}, fmt.Errorf("read %s %s: %w", wk.kind, name, err)
	}

	if err := json.Unmarshal(raw, &live); err != nil {
		return workloadHistory{}, fmt.Errorf("read %s %s: %w", wk.kind, name, err)
	}

	h.live = live.Spec.Template
	h.manual = manualUpdates(wk.kind, h.obj)
	h.isPaused = wk.kind == "Deployment" && h.obj.Spec.Paused

	var err error
	if wk.kind == "Deployment" {
		h.revs, err = replicaSetHistory(ctx, k, h.obj, namespace, name)
	} else {
		h.revs, err = controllerRevisionHistory(ctx, k, wk, h.obj, namespace, name)
	}

	return h, err
}

func replicaSetHistory(ctx context.Context, k *kubeClient, dep appsObject, namespace, name string) ([]revisionEntry, error) {
	revs, byRevision, err := deploymentRevisionsOf(ctx, k, dep, namespace, name)
	if err != nil {
		return nil, err
	}

	out := make([]revisionEntry, 0, len(revs))
	for _, r := range revs {
		rs := byRevision[r.Revision]
		template, _ := rollbackTemplate(rs.Spec.Template) //nolint:errcheck // nil: no diff, a rollback says why
		out = append(out, revisionEntry{revision: r, template: template, annotations: rs.Metadata.Annotations})
	}

	return out, nil
}

// controllerRevisionHistory lists a StatefulSet's or DaemonSet's ControllerRevisions, like
// `kubectl rollout history`. The current one is the StatefulSet's updateRevision; a
// DaemonSet's is its newest (its controller renumbers a template it goes back to).
func controllerRevisionHistory(ctx context.Context, k *kubeClient, wk workloadKind, obj appsObject, namespace, name string) ([]revisionEntry, error) {
	path := "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/controllerrevisions"
	if selector, ok := selectorQuery(obj.Spec.Selector); ok {
		path += "?labelSelector=" + url.QueryEscape(selector)
	}

	var list kubeList[controllerRevision]
	if err := getList(ctx, k, path, &list); err != nil {
		return nil, err
	}

	out := []revisionEntry{}

	for _, cr := range list.Items {
		if !cr.ownedBy(wk.kind, name) || cr.Revision <= 0 {
			continue
		}

		template := cr.template()
		out = append(out, revisionEntry{
			revision: deploymentRevision{
				Revision:    int(cr.Revision),
				ReplicaSet:  cr.Metadata.Name,
				Created:     cr.Metadata.CreationTimestamp.UnixMilli(),
				Images:      mapTemplateImages(template),
				ChangeCause: cr.Metadata.Annotations[changeCauseAnnotation],
				Current:     wk.kind == "StatefulSet" && cr.Metadata.Name == obj.Status.UpdateRevision,
			},
			template: template,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].revision.Revision > out[j].revision.Revision })

	if len(out) > 0 && !slices.ContainsFunc(out, func(r revisionEntry) bool { return r.revision.Current }) {
		out[0].revision.Current = true
	}

	return out, nil
}

// mapTemplateImages is the images of a pod template's containers, in order.
func mapTemplateImages(template map[string]any) []string {
	spec, _ := template["spec"].(map[string]any)
	containers, _ := spec["containers"].([]any)

	images := []string{}

	for _, c := range containers {
		if m, ok := c.(map[string]any); ok {
			if image, ok := m["image"].(string); ok {
				images = append(images, image)
			}
		}
	}

	return images
}

// workloadRevisionsJSON is KubeWorkloadRevisions without its masking: contextName, namespace
// and name are unmasked.
func workloadRevisionsJSON(configYAML, contextName, kubeServer, kind, namespace, name string) (string, error) {
	wk, err := findWorkloadKind(kind)
	if err != nil {
		return "", err
	}

	if err := validateKubeName("workload", namespace, name); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() workloadRevisions { return demoWorkloadRevisions(wk.kind, namespace, name) },
		func(ctx context.Context, k *kubeClient) (workloadRevisions, error) {
			h, err := readWorkloadHistory(ctx, k, wk, namespace, name)

			return h.list(), err
		})
}

// KubeWorkloadRevisions lists the revisions of a Deployment (its ReplicaSets), StatefulSet or
// DaemonSet (their ControllerRevisions), newest first, like `kubectl rollout history
// KIND/NAME` (os:admin; RBAC: list replicasets.apps or controllerrevisions.apps):
// {"kind","manual","paused","revisions":[{revision,replicaSet,created,images,changeCause,
// replicas,current}]}. replicaSet names the ControllerRevision of a StatefulSet or
// DaemonSet. kubeServer: see KubePods.
func KubeWorkloadRevisions(configYAML, contextName, kubeServer, kind, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	return workloadRevisionsJSON(configYAML, contextName, kubeServer, kind, namespace, name)
}

// revisionDiff is the pod template diff from the current revision to an older one.
type revisionDiff struct {
	Diff         string         `json:"diff"` // unified, "" when the templates are equal
	From         string         `json:"from"` // "revision 7 (current)"
	To           string         `json:"to"`   // "revision 5"
	FromRevision int            `json:"fromRevision"`
	ToRevision   int            `json:"toRevision"`
	Images       revisionImages `json:"images"`
}

type revisionImages struct {
	From []string `json:"from"`
	To   []string `json:"to"`
}

// buildRevisionDiff compares the live template (of revision current) with the revision's.
func buildRevisionDiff(current int, live map[string]any, to revisionEntry) (revisionDiff, error) {
	if to.template == nil {
		return revisionDiff{}, fmt.Errorf("the pod template of revision %d cannot be read", to.revision.Revision)
	}

	a, err := templateYAML(live)
	if err != nil {
		return revisionDiff{}, err
	}

	b, err := templateYAML(to.template)
	if err != nil {
		return revisionDiff{}, err
	}

	from := fmt.Sprintf("revision %d (current)", current)
	if current == 0 {
		from = "current"
	}

	toName := fmt.Sprintf("revision %d", to.revision.Revision)

	return revisionDiff{
		Diff: unifiedDiff(a, b, from, toName), From: from, To: toName,
		FromRevision: current, ToRevision: to.revision.Revision,
		Images: revisionImages{From: mapTemplateImages(live), To: mapTemplateImages(to.template)},
	}, nil
}

func templateYAML(template map[string]any) (string, error) {
	if len(template) == 0 {
		return "", nil
	}

	data, err := yaml.Marshal(template)
	if err != nil {
		return "", fmt.Errorf("encode YAML: %w", err)
	}

	return string(data), nil
}

// KubeRevisionDiff is the pod template diff from what a Deployment, StatefulSet or DaemonSet
// runs now to one of its revisions, what a rollback to it would undo (os:admin):
// {"diff":"<unified>","from":"revision 7 (current)","to":"revision 5","fromRevision",
// "toRevision","images":{"from":[...],"to":[...]}}. Masked like the YAML views. kubeServer:
// see KubePods.
func KubeRevisionDiff(configYAML, contextName, kubeServer, kind, namespace, name string, revision int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	wk, err := findWorkloadKind(kind)
	if err != nil {
		return "", err
	}

	if err := validateKubeName("workload", namespace, name); err != nil {
		return "", err
	}

	var d revisionDiff
	if isDemoContext(configYAML, contextName) {
		d, err = demoRevisionDiff(wk.kind, namespace, name, revision)
	} else {
		d, err = withKube(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) (revisionDiff, error) {
			h, err := readWorkloadHistory(ctx, k, wk, namespace, name)
			if err != nil {
				return revisionDiff{}, err
			}

			to, ok := h.find(revision)
			if !ok {
				return revisionDiff{}, errRevisionGone(revision, name)
			}

			return buildRevisionDiff(h.current(), h.live, to)
		})
	}

	if err != nil {
		return "", err
	}

	return toJSON(d)
}

func errRevisionGone(revision int, name string) error {
	return fmt.Errorf("revision %d of %s is not kept anymore (see revisionHistoryLimit)", revision, name)
}

// KubeRollbackWorkload rolls a Deployment, StatefulSet or DaemonSet back to revision, like
// `kubectl rollout undo KIND/NAME --to-revision=N` (os:admin; RBAC: patch on the kind, list
// replicasets.apps or controllerrevisions.apps). A StatefulSet's or DaemonSet's pod template
// becomes the ControllerRevision's; with OnDelete its pods only take it when deleted. The
// current revision, and a paused Deployment, are refused. kubeServer: see KubePods.
func KubeRollbackWorkload(configYAML, contextName, kubeServer, kind, namespace, name string, revision int) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	kind = strings.TrimSpace(kind)

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "rollback-" + strings.ToLower(kind), Namespace: namespace, Object: kind + "/" + name, Params: fmt.Sprintf("revision=%d", revision)})

	wk, err := findWorkloadKind(kind)
	if err != nil {
		return err
	}

	if err := validateKubeName("workload", namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		if wk.kind == "Deployment" {
			return rollbackDeployment(ctx, k, namespace, name, revision)
		}

		return rollbackControllerRevision(ctx, k, wk, namespace, name, revision)
	})
}

// rollbackControllerRevision replaces a StatefulSet's or DaemonSet's pod template by the
// revision's, as `kubectl rollout undo` applies the ControllerRevision's data. The controller
// then renumbers that revision as the newest.
func rollbackControllerRevision(ctx context.Context, k *kubeClient, wk workloadKind, namespace, name string, revision int) error {
	h, err := readWorkloadHistory(ctx, k, wk, namespace, name)
	if err != nil {
		return err
	}

	to, ok := h.find(revision)
	if !ok {
		return errRevisionGone(revision, name)
	}

	if to.revision.Current {
		return fmt.Errorf("revision %d is already the current one", revision)
	}

	if len(to.template) == 0 {
		return errors.New("the revision's pod template cannot be read")
	}

	// A JSON patch replaces the whole template, as the revision's "$patch: replace" does.
	patch := []map[string]any{{"op": "replace", "path": "/spec/template", "value": to.template}}

	return k.patch(ctx, appsPath(wk, namespace, name), "application/json-patch+json", patch, nil)
}

// KubeRolloutPause pauses (paused) or resumes a Deployment's rollout, like `kubectl rollout
// pause|resume deployment/NAME` (os:admin; RBAC: patch deployments.apps): while paused, a
// template change starts no rolling update. kubeServer: see KubePods.
func KubeRolloutPause(configYAML, contextName, kubeServer, namespace, name string, paused bool) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: auditVerb(paused, "pause-rollout", "resume-rollout"), Namespace: namespace, Object: "Deployment/" + name})

	if err := validateKubeName("Deployment", namespace, name); err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return pauseRollout(ctx, k, namespace, name, paused)
	})
}

func pauseRollout(ctx context.Context, k *kubeClient, namespace, name string, paused bool) error {
	patch := map[string]any{"spec": map[string]any{"paused": paused}}

	return k.patch(ctx, appsPath(workloadKinds[0], namespace, name), "application/merge-patch+json", patch, nil)
}
