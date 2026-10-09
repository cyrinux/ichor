package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Deleting any object from the browser: the app first asks KubeObjectDeletePreview what the
// delete would touch (finalizers holding it, objects owned by it, whether it is protected),
// then deletes with a propagation policy and the resourceVersion it showed, so an object
// changed in between is not deleted on the strength of an old preview.
//
// A few objects are protected: deleting them breaks the cluster or more than the object, so
// they are refused unless the user forces it (after typing the name in the app):
//   - the Namespaces Kubernetes itself needs: kube-system, kube-public, kube-node-lease, default;
//   - Nodes (the node leaves Kubernetes until its kubelet registers again; use the node actions);
//   - CustomResourceDefinitions (every object of the kind goes with them);
//   - the bootstrap-token-* Secrets of kube-system: Talos writes its cluster token there, and
//     nodes join the cluster with it.

// deleteDependentsMax caps the dependents a preview lists; deleteDependents.More counts the rest.
const deleteDependentsMax = 50

// systemNamespaces are the Namespaces Kubernetes needs.
var systemNamespaces = []string{"kube-system", "kube-public", "kube-node-lease", "default"}

type kubeDeleteDependent struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// kubeDeletePreview is what deleting an object would do. ResourceVersion is the version the
// preview read, to pass back to KubeObjectDelete; Deleting: a deletion is already pending
// (finalizers hold it).
type kubeDeletePreview struct {
	Protected       bool                  `json:"protected"`
	Reason          string                `json:"reason,omitempty"`
	ClusterScoped   bool                  `json:"clusterScoped"`
	Finalizers      []string              `json:"finalizers"`
	Dependents      []kubeDeleteDependent `json:"dependents"`
	MoreDependents  int                   `json:"moreDependents,omitempty"`
	ResourceVersion string                `json:"resourceVersion,omitempty"`
	Deleting        bool                  `json:"deleting,omitempty"`
}

// KubeObjectDeletePreview reads the object and returns a JSON kubeDeletePreview: whether it is
// protected and why, the finalizers that would hold its deletion, and for a workload the
// objects it owns (ReplicaSets and their Pods, Pods, Jobs) that the propagation policy
// decides about. Read-only.
func KubeObjectDeletePreview(configYAML, contextName, kubeServer, group, version, resource, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	ref, path, err := deleteTarget(group, version, resource, namespace, name)
	if err != nil {
		return "", err
	}

	base := kubeDeletePreview{ClusterScoped: namespace == "", Finalizers: []string{}, Dependents: []kubeDeleteDependent{}}
	if reason := deleteProtection(ref, namespace, name); reason != "" {
		base.Protected, base.Reason = true, reason
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() kubeDeletePreview { return base },
		func(ctx context.Context, k *kubeClient) (kubeDeletePreview, error) {
			var obj deleteObject
			if err := k.get(ctx, path, &obj); err != nil {
				return kubeDeletePreview{}, deleteError(err)
			}

			p := base
			p.ResourceVersion = obj.Metadata.ResourceVersion
			p.Deleting = obj.Metadata.DeletionTimestamp != ""
			p.Finalizers = append(p.Finalizers, obj.Metadata.Finalizers...)
			p.Dependents = deleteDependents(ctx, k, ref, namespace, obj)

			if len(p.Dependents) > deleteDependentsMax {
				p.MoreDependents = len(p.Dependents) - deleteDependentsMax
				p.Dependents = p.Dependents[:deleteDependentsMax]
			}

			return p, nil
		})
}

// KubeObjectDelete deletes the object like `kubectl delete`, with propagation "Background"
// (the default when ""), "Foreground" or "Orphan" for what it owns. gracePeriodSec < 0 keeps
// the object's own grace period. resourceVersion, when set, must still be the object's (the
// preview's): otherwise nothing is deleted. A protected object (see above) is refused unless
// force. Refused in the demo.
func KubeObjectDelete(configYAML, contextName, kubeServer, group, version, resource, namespace, name, propagation, resourceVersion string, gracePeriodSec int, force bool) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	policy, policyErr := deletePropagation(propagation)

	defer recordAction(&err, configYAML, contextName, auditAction{
		Action: "delete", Namespace: namespace, Object: resource + "/" + name, Params: deleteParams(policy, force),
	})

	if policyErr != nil {
		return policyErr
	}

	ref, path, err := deleteTarget(group, version, resource, namespace, name)
	if err != nil {
		return err
	}

	if reason := deleteProtection(ref, namespace, name); reason != "" && !force {
		return fmt.Errorf("protected: %s", reason)
	}

	body, err := deleteOptions(policy, strings.TrimSpace(resourceVersion), gracePeriodSec)
	if err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return deleteError(k.do(ctx, http.MethodDelete, path, "application/json", body, nil))
	})
}

// deleteTarget checks the object's coordinates and returns its resource and API path.
func deleteTarget(group, version, resource, namespace, name string) (resourceRef, string, error) {
	ref, err := newResourceRef(group, version, resource)
	if err != nil {
		return resourceRef{}, "", err
	}

	if name == "" || (!kubeNamePattern.MatchString(name) && !kubeResourcePattern.MatchString(name)) || strings.Contains(name, "..") {
		return resourceRef{}, "", fmt.Errorf("invalid name %q", name)
	}

	if err := validateNamespace(namespace); err != nil {
		return resourceRef{}, "", err
	}

	return ref, ref.path(namespace) + "/" + url.PathEscape(name), nil
}

// deleteProtection is why deleting the object is refused without force, "" when it is not.
func deleteProtection(ref resourceRef, namespace, name string) string {
	switch {
	case ref.group == "" && ref.resource == "namespaces" && slices.Contains(systemNamespaces, name):
		return fmt.Sprintf("Kubernetes needs the %s namespace: deleting it deletes everything in it and breaks the cluster", name)
	case ref.group == "" && ref.resource == "nodes":
		return "deleting a Node removes it from Kubernetes and evicts nothing first: drain it or use the node actions instead"
	case ref.group == "apiextensions.k8s.io" && ref.resource == "customresourcedefinitions":
		return "deleting a CustomResourceDefinition deletes every object of its kind in the cluster"
	case ref.group == "" && ref.resource == "secrets" && namespace == "kube-system" && strings.HasPrefix(name, "bootstrap-token-"):
		return "Talos keeps the cluster's bootstrap token in this Secret: nodes join the cluster with it"
	}

	return ""
}

// deletePropagation is the DeleteOptions propagationPolicy for what the app passed.
func deletePropagation(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "background":
		return "Background", nil
	case "foreground":
		return "Foreground", nil
	case "orphan":
		return "Orphan", nil
	}

	return "", fmt.Errorf("unknown propagation policy %q (Background, Foreground, Orphan)", s)
}

func deleteParams(policy string, force bool) string {
	if policy == "" {
		policy = "?"
	}

	params := "propagation=" + policy
	if force {
		params += ", force"
	}

	return params
}

// deleteOptions is the body of the DELETE request.
func deleteOptions(policy, resourceVersion string, gracePeriodSec int) ([]byte, error) {
	opts := map[string]any{"kind": "DeleteOptions", "apiVersion": "v1", "propagationPolicy": policy}

	if gracePeriodSec >= 0 {
		opts["gracePeriodSeconds"] = gracePeriodSec
	}

	if resourceVersion != "" {
		opts["preconditions"] = map[string]string{"resourceVersion": resourceVersion}
	}

	body, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("encode delete options: %w", err)
	}

	return body, nil
}

func deleteError(err error) error {
	switch kubeCode(err) {
	case http.StatusConflict:
		return errors.New("the object changed since you looked at it: review it again before deleting")
	case http.StatusNotFound:
		return errors.New("the object no longer exists")
	}

	return err
}

// deleteObject is what a preview reads of the object and of its dependents.
type deleteObject struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name              string           `json:"name"`
		Namespace         string           `json:"namespace"`
		UID               string           `json:"uid"`
		ResourceVersion   string           `json:"resourceVersion"`
		DeletionTimestamp string           `json:"deletionTimestamp"`
		Finalizers        []string         `json:"finalizers"`
		OwnerReferences   []deleteOwnerRef `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Selector *struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
	} `json:"spec"`
}

type deleteOwnerRef struct {
	UID string `json:"uid"`
}

func (o deleteObject) ownedBy(uids []string) bool {
	return slices.ContainsFunc(o.Metadata.OwnerReferences, func(r deleteOwnerRef) bool { return slices.Contains(uids, r.UID) })
}

// deleteChild is a kind a workload owns: its collection under apiPath.
type deleteChild struct {
	apiPath, resource, kind string
}

var (
	podChild        = deleteChild{"/api/v1", "pods", "Pod"}
	replicaSetChild = deleteChild{"/apis/apps/v1", "replicasets", "ReplicaSet"}
	jobChild        = deleteChild{"/apis/batch/v1", "jobs", "Job"}
)

// deleteChildren are the kinds each workload owns, by "group/resource": the second kind (a
// Deployment's Pods) is owned by the first.
var deleteChildren = map[string][]deleteChild{
	"apps/deployments":  {replicaSetChild, podChild},
	"apps/replicasets":  {podChild},
	"apps/statefulsets": {podChild},
	"apps/daemonsets":   {podChild},
	"batch/jobs":        {podChild},
	"batch/cronjobs":    {jobChild},
}

// deleteDependents lists the objects of the workload obj owns in its namespace, following
// their ownerReferences; nothing for other kinds or when they cannot be listed (best effort:
// the delete does not depend on it). The workload's selector narrows the lists when it has one.
func deleteDependents(ctx context.Context, k *kubeClient, ref resourceRef, namespace string, obj deleteObject) []kubeDeleteDependent {
	children := deleteChildren[ref.group+"/"+ref.resource]
	if namespace == "" || obj.Metadata.UID == "" || len(children) == 0 {
		return []kubeDeleteDependent{}
	}

	selector := ""
	if obj.Spec.Selector != nil && len(obj.Spec.Selector.MatchLabels) > 0 {
		selector = "?labelSelector=" + url.QueryEscape(matchLabelsSelector(obj.Spec.Selector.MatchLabels))
	}

	out := []kubeDeleteDependent{}
	owners := []string{obj.Metadata.UID}

	for _, child := range children {
		var list kubeList[deleteObject]
		if err := getList(ctx, k, scopedPath(child.apiPath, namespace, child.resource)+selector, &list); err != nil {
			break
		}

		var next []string

		for _, item := range list.Items {
			if !item.ownedBy(owners) {
				continue
			}

			next = append(next, item.Metadata.UID)
			out = append(out, kubeDeleteDependent{Kind: child.kind, Namespace: namespace, Name: item.Metadata.Name})
		}

		if len(next) == 0 {
			break
		}

		owners = next
	}

	return out
}
