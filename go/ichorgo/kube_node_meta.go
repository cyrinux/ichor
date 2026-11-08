package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Node taints and labels: read them, plan an edit (the diff, the apiserver's dry-run
// verdict, the pods a new NoExecute taint evicts) and apply it as one merge patch with a
// resourceVersion precondition. KubeNodeMeta* take the Kubernetes node name (kubeconfig
// clusters); NodeMeta* their Talos twins take the Talos address, like KubeCordon. The edits
// document is in kube_node_meta_edits.go.

// nodeMeta is the read of the editor.
type nodeMeta struct {
	Node            string          `json:"node"`
	ResourceVersion string          `json:"resourceVersion"`
	Taints          []nodeMetaTaint `json:"taints"`
	Labels          []nodeMetaLabel `json:"labels"`
}

type nodeMetaTaint struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Effect    string `json:"effect"`
	TimeAdded int64  `json:"timeAdded"` // Unix seconds, 0 when unknown
	Protected bool   `json:"protected"`
}

type nodeMetaLabel struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	Protected bool   `json:"protected"`
}

// nodeMetaPlan is what an edit would do.
type nodeMetaPlan struct {
	Node     string            `json:"node"`
	Labels   nodeMetaLabelDiff `json:"labels"`
	Taints   nodeMetaTaintDiff `json:"taints"`
	Evicts   []nodeMetaEvict   `json:"evicts"`
	Blockers []string          `json:"blockers"`
	Warnings []string          `json:"warnings"`
	NoChange bool              `json:"noChange"`
}

// KubeNodeMeta reads the taints and labels of the Kubernetes node kubeNode, as JSON
// {node, resourceVersion, taints: [{key, value, effect, timeAdded, protected}], labels:
// [{key, value, protected}]}, sorted by key; protected ones are under a well-known prefix.
// kubeServer: see KubePods.
func KubeNodeMeta(configYAML, contextName, kubeServer, kubeNode string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, kubeNode = unmaskTarget(configYAML, contextName, kubeNode)

	return readNodeMetaJSON(kubeTarget{configYAML, contextName, kubeServer}, kubeNode)
}

// KubeNodeMetaPlan plans the edit editsJSON ({setLabels: {key: value}, removeLabels: [key],
// taints: [{key, value, effect}] the full desired list or absent to keep them,
// resourceVersion, force}) of the Kubernetes node kubeNode, as JSON {node, labels: {added,
// removed, changed}, taints: {added, removed, changed}, evicts, blockers, warnings,
// noChange}. The apiserver checks it with a dry run. kubeServer: see KubePods.
func KubeNodeMetaPlan(configYAML, contextName, kubeServer, kubeNode, editsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, kubeNode = unmaskTarget(configYAML, contextName, kubeNode)

	return planNodeMetaJSON(kubeTarget{configYAML, contextName, kubeServer}, kubeNode, editsJSON)
}

// KubeNodeMetaApply applies the edit editsJSON (see KubeNodeMetaPlan) to the Kubernetes node
// kubeNode: one merge patch, refused when the node changed since its read or, without force,
// when it removes or changes a protected label or taint. kubeServer: see KubePods.
func KubeNodeMetaApply(configYAML, contextName, kubeServer, kubeNode, editsJSON string, force bool) (err error) {
	defer maskErr(&err)

	contextName, kubeNode = unmaskTarget(configYAML, contextName, kubeNode)

	var summary string

	return recordedRun(configYAML, contextName, func() auditAction {
		return auditAction{Server: kubeServer, Action: "node-meta", Node: kubeNode, Params: summary}
	}, func() error {
		return applyNodeMeta(kubeTarget{configYAML, contextName, kubeServer}, kubeNode, editsJSON, force, &summary)
	})
}

// NodeMeta is KubeNodeMeta for the Talos node node (its address), os:admin.
func NodeMeta(configYAML, contextName, kubeServer, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	kubeNode, err := talosKubeNodeName(configYAML, contextName, node)
	if err != nil {
		return "", err
	}

	return readNodeMetaJSON(kubeTarget{configYAML, contextName, kubeServer}, kubeNode)
}

// NodeMetaPlan is KubeNodeMetaPlan for the Talos node node (its address), os:admin.
func NodeMetaPlan(configYAML, contextName, kubeServer, node, editsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	kubeNode, err := talosKubeNodeName(configYAML, contextName, node)
	if err != nil {
		return "", err
	}

	return planNodeMetaJSON(kubeTarget{configYAML, contextName, kubeServer}, kubeNode, editsJSON)
}

// NodeMetaApply is KubeNodeMetaApply for the Talos node node (its address), os:admin.
func NodeMetaApply(configYAML, contextName, kubeServer, node, editsJSON string, force bool) (err error) {
	defer maskErr(&err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	var summary string

	return recordedRun(configYAML, contextName, func() auditAction {
		return auditAction{Action: "node-meta", Node: node, Params: summary}
	}, func() error {
		if isDemoContext(configYAML, contextName) {
			return errDemoUnavailable
		}

		kubeNode, err := talosKubeNodeName(configYAML, contextName, node)
		if err != nil {
			return err
		}

		return applyNodeMeta(kubeTarget{configYAML, contextName, kubeServer}, kubeNode, editsJSON, force, &summary)
	})
}

// talosKubeNodeName is the Kubernetes name of the Talos node node (the demo's hostname).
func talosKubeNodeName(configYAML, contextName, node string) (string, error) {
	if isDemoContext(configYAML, contextName) {
		return demoKubeNodeName(node)
	}

	kubeNode, err := withNodeSession(configYAML, contextName, node, callTimeout, func(nodeCtx context.Context, s *session) (string, error) {
		return kubeNodeNameOf(nodeCtx, s.client)
	})
	if errors.Is(err, errKubeNodeUnknown) {
		return "", fmt.Errorf("no Kubernetes node for %s: is the kubelet running?", node)
	}

	return kubeNode, err
}

func nodePath(kubeNode string) string { return "/api/v1/nodes/" + url.PathEscape(kubeNode) }

func readNodeMetaJSON(target kubeTarget, kubeNode string) (string, error) {
	return kubeReadJSON(target,
		func() nodeMeta { return nodeMetaOf(demoNodeMetaObject(kubeNode)) },
		func(ctx context.Context, k *kubeClient) (nodeMeta, error) {
			obj, err := getNodeMeta(ctx, k, kubeNode)

			return nodeMetaOf(obj), err
		})
}

func getNodeMeta(ctx context.Context, k *kubeClient, kubeNode string) (nodeMetaObject, error) {
	var obj nodeMetaObject

	if err := k.get(ctx, nodePath(kubeNode), &obj); err != nil {
		return nodeMetaObject{}, fmt.Errorf("node %s: %w", kubeNode, err)
	}

	return obj, nil
}

// nodeMetaOf maps a node to the editor's read.
func nodeMetaOf(obj nodeMetaObject) nodeMeta {
	m := nodeMeta{Node: obj.Metadata.Name, ResourceVersion: obj.Metadata.ResourceVersion, Taints: []nodeMetaTaint{}, Labels: []nodeMetaLabel{}}

	for _, key := range sortedStringKeys(obj.Metadata.Labels) {
		m.Labels = append(m.Labels, nodeMetaLabel{Key: key, Value: obj.Metadata.Labels[key], Protected: protectedNodeKey(key)})
	}

	for _, t := range obj.Spec.Taints {
		m.Taints = append(m.Taints, nodeMetaTaint{Key: t.Key, Value: t.Value, Effect: t.Effect, TimeAdded: t.unixAdded(), Protected: protectedNodeKey(t.Key)})
	}

	sort.SliceStable(m.Taints, func(i, j int) bool {
		if m.Taints[i].Key != m.Taints[j].Key {
			return m.Taints[i].Key < m.Taints[j].Key
		}

		return m.Taints[i].Effect < m.Taints[j].Effect
	})

	return m
}

func planNodeMetaJSON(target kubeTarget, kubeNode, editsJSON string) (string, error) {
	edits, err := parseNodeMetaEdits(editsJSON)
	if err != nil {
		return "", err
	}

	return kubeReadJSON(target,
		func() nodeMetaPlan { return demoNodeMetaPlan(kubeNode, edits) },
		func(ctx context.Context, k *kubeClient) (nodeMetaPlan, error) {
			return planNodeMeta(ctx, k, kubeNode, edits)
		})
}

// planNodeMeta reads the node, diffs the edit, lists the pods it evicts and has the
// apiserver dry-run the patch.
func planNodeMeta(ctx context.Context, k *kubeClient, kubeNode string, edits nodeMetaEdits) (nodeMetaPlan, error) {
	obj, err := getNodeMeta(ctx, k, kubeNode)
	if err != nil {
		return nodeMetaPlan{}, err
	}

	diff := diffNodeMeta(obj, edits)

	var pods []drainPod

	if len(diff.noExecuteTaints()) > 0 {
		if pods, err = drainPods(ctx, k, kubeNode); err != nil {
			return nodeMetaPlan{}, err
		}
	}

	plan := buildNodeMetaPlan(obj, edits, diff, pods)
	if plan.NoChange {
		return plan, nil
	}

	// The current version, not the edit's: a stale edit is already a blocker above.
	patch := nodeMetaPatch(diff, obj.Metadata.ResourceVersion)
	if err := k.patch(ctx, nodePath(kubeNode)+"?dryRun=All", "application/merge-patch+json", patch, nil); err != nil {
		var apiErr *kubeAPIError
		if !errors.As(err, &apiErr) {
			return nodeMetaPlan{}, fmt.Errorf("dry run on %s: %w", kubeNode, err)
		}

		plan.Blockers = append(plan.Blockers, dryRunBlocker(apiErr))
	}

	return plan, nil
}

func dryRunBlocker(apiErr *kubeAPIError) string {
	if apiErr.Code == http.StatusConflict {
		return nodeMetaChanged
	}

	if apiErr.Message != "" {
		return apiErr.Message
	}

	return apiErr.Error()
}

// buildNodeMetaPlan is the plan without the dry run: the diff, the evictions, and the
// blockers and warnings Go finds itself.
func buildNodeMetaPlan(obj nodeMetaObject, edits nodeMetaEdits, diff nodeMetaDiff, pods []drainPod) nodeMetaPlan {
	plan := nodeMetaPlan{
		Node: obj.Metadata.Name, Labels: diff.labels, Taints: diff.taints,
		Evicts:   evictedBy(pods, diff.noExecuteTaints()),
		Blockers: []string{}, Warnings: []string{}, NoChange: diff.empty(),
	}

	if edits.ResourceVersion != "" && edits.ResourceVersion != obj.Metadata.ResourceVersion {
		plan.Blockers = append(plan.Blockers, nodeMetaChanged)
		plan.Warnings = append(plan.Warnings, nodeMetaChanged)
	}

	if plan.NoChange {
		return plan
	}

	for _, change := range diff.protectedChanges() {
		if edits.Force {
			plan.Warnings = append(plan.Warnings, change)
		} else {
			plan.Blockers = append(plan.Blockers, change+": allow protected changes (force) to do it")
		}
	}

	if n := len(plan.Evicts); n > 0 {
		plan.Warnings = append(plan.Warnings, evictWarning(n))
	}

	for _, l := range diff.labels.Removed {
		if role, ok := strings.CutPrefix(l.Key, roleLabelPrefix); ok && role != "" {
			plan.Warnings = append(plan.Warnings, "the node leaves its role "+role)
		}
	}

	return plan
}

func evictWarning(n int) string {
	if n == 1 {
		return "1 pod will be evicted at once"
	}

	return fmt.Sprintf("%d pods will be evicted at once", n)
}

// nodeMetaPatch is the merge patch of a diff: the labels as deltas (null removes), the taints
// whole, and resourceVersion as the precondition ("" for none).
func nodeMetaPatch(diff nodeMetaDiff, resourceVersion string) map[string]any {
	metadata := map[string]any{}
	if resourceVersion != "" {
		metadata["resourceVersion"] = resourceVersion
	}

	if len(diff.patchLabels) > 0 {
		metadata["labels"] = diff.patchLabels
	}

	patch := map[string]any{"metadata": metadata}

	if diff.patchTaints != nil {
		patch["spec"] = map[string]any{"taints": diff.patchTaints}
	}

	return patch
}

// applyNodeMeta patches the node; summary gets the diff for the audit log.
func applyNodeMeta(target kubeTarget, kubeNode, editsJSON string, force bool, summary *string) error {
	edits, err := parseNodeMetaEdits(editsJSON)
	if err != nil {
		return err
	}

	force = force || edits.Force

	// A refusal found on the node is no failed request: kubeMutate must not call it one
	// that may have been applied.
	var refused error

	err = kubeMutate(target, func(ctx context.Context, k *kubeClient) error {
		obj, err := getNodeMeta(ctx, k, kubeNode)
		if err != nil {
			return err
		}

		if edits.ResourceVersion != "" && edits.ResourceVersion != obj.Metadata.ResourceVersion {
			refused = errNodeMetaChanged

			return nil
		}

		diff := diffNodeMeta(obj, edits)
		*summary = diff.summary()

		if protected := diff.protectedChanges(); len(protected) > 0 && !force {
			refused = fmt.Errorf("%s: allow protected changes (force) to do it", strings.Join(protected, "; "))

			return nil
		}

		if diff.empty() {
			return nil
		}

		// The edit's version is the precondition (a 409 when the node moved since its read);
		// without one, the version just read guards against a change in between.
		precondition := edits.ResourceVersion
		if precondition == "" {
			precondition = obj.Metadata.ResourceVersion
		}

		patch := nodeMetaPatch(diff, precondition)

		return k.patch(ctx, nodePath(kubeNode), "application/merge-patch+json", patch, nil)
	})
	var apiErr *kubeAPIError
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusConflict {
		return errNodeMetaChanged
	}

	if err != nil {
		return err
	}

	return refused
}

// demoNodeMetaObject is a demo node: three labels, its role taint and a stray maintenance
// taint left a day ago.
func demoNodeMetaObject(kubeNode string) nodeMetaObject {
	var obj nodeMetaObject

	obj.Metadata.Name, obj.Metadata.ResourceVersion = kubeNode, "1000"
	obj.Metadata.Labels = map[string]string{
		"kubernetes.io/hostname":          kubeNode,
		roleLabelPrefix + "control-plane": "",
		"example.com/pool":                "general",
	}
	obj.Spec.Taints = []nodeTaintObject{
		{Key: roleLabelPrefix + "control-plane", Effect: "NoSchedule"},
		{Key: "maintenance", Value: "true", Effect: "NoSchedule", TimeAdded: time.Now().Add(-26 * time.Hour).UTC().Format(time.RFC3339)},
	}

	return obj
}

// demoNodeMetaPlan plans an edit of the demo node, without a dry run; its pods are the demo
// drain plan's (they tolerate nothing).
func demoNodeMetaPlan(kubeNode string, edits nodeMetaEdits) nodeMetaPlan {
	obj := demoNodeMetaObject(kubeNode)
	diff := diffNodeMeta(obj, edits)

	return buildNodeMetaPlan(obj, edits, diff, demoKubeDrainPlan(kubeNode).Pods)
}
