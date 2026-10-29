package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Removing one instance of a CloudNativePG cluster, like `kubectl cnpg destroy CLUSTER N`: its
// PVCs (data and WAL), then its pod. The operator then clones a fresh replica from the
// primary. It is how a cluster gets out of "Waiting for the instances to become active" when an
// instance's volume is pinned to a node that is gone (local-path, OpenEBS LocalPV...): the pod
// waits unscheduled forever, and the operator waits for it before recreating any other
// instance. Never the current or target primary, never the last instance holding data.

// Why an instance cannot be removed, for the app to word.
const (
	cnpgDestroyPrimary     = "primary"     // the current or target primary
	cnpgDestroyNoOtherData = "noOtherData" // the primary keeps no volume: this one may hold the only data
	cnpgDestroyNotFound    = "notFound"    // no pod and no volume of this instance
)

// What to know before removing an instance.
const (
	cnpgDestroyReady       = "ready"       // a healthy replica: removing it lowers redundancy until the new one catches up
	cnpgDestroyNodePresent = "nodePresent" // a volume's node still exists: the instance may come back on its own
)

// Where an instance's volume lives: the node it is pinned to and whether that node exists.
const (
	cnpgNodePresent = "present"
	cnpgNodeGone    = "gone"
	cnpgNodeUnknown = "unknown" // the PersistentVolume or the Node could not be read
	cnpgNodeNone    = "none"    // not pinned to a node (network storage)
)

type cnpgDestroyVolume struct {
	Name      string `json:"name"`
	Role      string `json:"role"`      // PG_DATA, PG_WAL, PG_TABLESPACE, "" before CNPG labelled it
	Node      string `json:"node"`      // the node the volume is pinned to, "" when none or unknown
	NodeState string `json:"nodeState"` // present|gone|unknown|none
}

type cnpgDestroyPreview struct {
	Namespace      string              `json:"namespace"`
	Name           string              `json:"name"`
	Instance       string              `json:"instance"`
	CurrentPrimary string              `json:"currentPrimary"`
	TargetPrimary  string              `json:"targetPrimary"`
	PodPhase       string              `json:"podPhase"` // "" when the instance has no pod
	PodNode        string              `json:"podNode"`  // "" while unscheduled
	Volumes        []cnpgDestroyVolume `json:"volumes"`
	Blockers       []string            `json:"blockers"`
	Warnings       []string            `json:"warnings"`
	CanDestroy     bool                `json:"canDestroy"`
}

type cnpgPVCObject struct {
	Metadata struct {
		Name            string            `json:"name"`
		UID             string            `json:"uid"`
		ResourceVersion string            `json:"resourceVersion"`
		Labels          map[string]string `json:"labels"`
		OwnerReferences []cnpgOwnerRef    `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		VolumeName string `json:"volumeName"`
	} `json:"spec"`
}

type cnpgOwnerRef struct {
	Kind string `json:"kind"`
	UID  string `json:"uid"`
}

type cnpgPVObject struct {
	Spec struct {
		NodeAffinity *struct {
			Required struct {
				NodeSelectorTerms []struct {
					MatchExpressions []struct {
						Key      string   `json:"key"`
						Operator string   `json:"operator"`
						Values   []string `json:"values"`
					} `json:"matchExpressions"`
				} `json:"nodeSelectorTerms"`
			} `json:"required"`
		} `json:"nodeAffinity"`
	} `json:"spec"`
}

// cnpgDestroyTarget is what the preview and the removal read: the instance's pod (nil without
// one) and the cluster's own PVCs, split into the instance's and the others'.
type cnpgDestroyTarget struct {
	cluster cnpgActionCluster
	pod     *cnpgActionPod
	mine    []cnpgPVCObject
	others  []cnpgPVCObject
}

// KubeCNPGDestroyPreview returns a JSON cnpgDestroyPreview: the instance's pod, its volumes and
// the node each one is pinned to, and what blocks or warns against removing it. Read-only.
func KubeCNPGDestroyPreview(configYAML, contextName, kubeServer, namespace, name, instance string) (out string, err error) {
	defer maskResult(&out, &err)
	contextName, namespace, name, instance, err = cnpgActionNames(configYAML, contextName, namespace, name, instance)
	contextName = unmaskContext(configYAML, contextName)
	if err != nil {
		return "", err
	}
	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, func() cnpgDestroyPreview {
		return demoCNPGDestroyPreview(namespace, name, instance)
	}, func(ctx context.Context, k *kubeClient) (cnpgDestroyPreview, error) {
		return readCNPGDestroyPreview(ctx, k, namespace, name, instance)
	})
}

// KubeCNPGDestroy removes the instance: it checks the blockers again, deletes the instance's
// PVCs (each only if unchanged since read) and then its pod. Refused in the demo.
func KubeCNPGDestroy(configYAML, contextName, kubeServer, namespace, name, instance string) (err error) {
	defer maskErr(&err)
	contextName, namespace, name, instance, err = cnpgActionNames(configYAML, contextName, namespace, name, instance)
	contextName = unmaskContext(configYAML, contextName)
	if err != nil {
		return err
	}
	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "cnpg-destroy", Namespace: namespace, Object: "Cluster/" + name, Params: "instance=" + instance})
	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return destroyCNPGInstance(ctx, k, namespace, name, instance)
	})
}

func demoCNPGDestroyPreview(namespace, name, instance string) cnpgDestroyPreview {
	r := cnpgDestroyPreview{Namespace: namespace, Name: name, Instance: instance, Volumes: []cnpgDestroyVolume{}, Blockers: []string{}, Warnings: []string{}}
	for _, c := range demoDataServices(time.Now()).CNPG.Clusters {
		if c.Namespace != namespace || c.Name != name {
			continue
		}
		r.CurrentPrimary, r.TargetPrimary = c.CurrentPrimary, c.TargetPrimary
		var pod *cnpgPod
		for i := range c.InstancePods {
			if c.InstancePods[i].Name == instance {
				pod = &c.InstancePods[i]
			}
		}
		if pod == nil {
			r.Blockers = append(r.Blockers, cnpgDestroyNotFound)
			return r
		}
		// A demo instance that is not running waits for a node that was removed.
		volume := cnpgDestroyVolume{Name: instance, Role: "PG_DATA", Node: pod.Node, NodeState: cnpgNodePresent}
		if !pod.Ready {
			volume.Node, volume.NodeState = "demo-worker-old", cnpgNodeGone
		}
		r.PodPhase, r.PodNode, r.Volumes = pod.Phase, pod.Node, []cnpgDestroyVolume{volume}
		r.Blockers, r.Warnings = cnpgDestroyChecks(c.CurrentPrimary, c.TargetPrimary, instance, pod.Role == "primary", len(c.InstancePods) > 1, pod.Ready, r.Volumes)
		r.CanDestroy = len(r.Blockers) == 0
		return r
	}
	r.Blockers = append(r.Blockers, cnpgDestroyNotFound)
	return r
}

// cnpgDestroyChecks: the blockers and warnings of removing instance; otherData says the
// primary, another instance, keeps its volume.
func cnpgDestroyChecks(currentPrimary, targetPrimary, instance string, primaryLabel, otherData, ready bool, volumes []cnpgDestroyVolume) (blockers, warnings []string) {
	blockers, warnings = []string{}, []string{}
	if instance == currentPrimary || instance == targetPrimary || primaryLabel {
		blockers = append(blockers, cnpgDestroyPrimary)
	}
	if !otherData {
		blockers = append(blockers, cnpgDestroyNoOtherData)
	}
	if ready {
		warnings = append(warnings, cnpgDestroyReady)
	}
	if slices.ContainsFunc(volumes, func(v cnpgDestroyVolume) bool { return v.NodeState == cnpgNodePresent }) {
		warnings = append(warnings, cnpgDestroyNodePresent)
	}
	return blockers, warnings
}

func readCNPGDestroyTarget(ctx context.Context, k *kubeClient, namespace, name, instance string) (cnpgDestroyTarget, error) {
	var t cnpgDestroyTarget
	c, _, err := readCNPGActionCluster(ctx, k, namespace, name)
	if err != nil {
		return t, err
	}
	if c.Metadata.UID == "" {
		return t, errors.New("the cluster has no uid")
	}
	t.cluster = c
	p, err := readCNPGActionPod(ctx, k, c, namespace, name, instance)
	switch {
	case kubeCode(err) == http.StatusNotFound:
	case err != nil:
		return t, err
	default:
		t.pod = &p
	}
	var pvcs kubeList[cnpgPVCObject]
	path := scopedPath("/api/v1", namespace, "persistentvolumeclaims") + "?labelSelector=" + url.QueryEscape("cnpg.io/cluster="+name)
	if err := getList(ctx, k, path, &pvcs); err != nil {
		return t, err
	}
	for _, pvc := range pvcs.Items {
		// Only the volumes the cluster owns: a look-alike PVC is never deleted.
		if !slices.ContainsFunc(pvc.Metadata.OwnerReferences, func(o cnpgOwnerRef) bool { return o.Kind == "Cluster" && o.UID == c.Metadata.UID }) {
			continue
		}
		if cnpgPVCInstance(pvc) == instance {
			t.mine = append(t.mine, pvc)
		} else {
			t.others = append(t.others, pvc)
		}
	}
	return t, nil
}

// cnpgPVCInstance is the instance a PVC belongs to: its label, or before CNPG labelled it the
// name (instance, instance-wal).
func cnpgPVCInstance(pvc cnpgPVCObject) string {
	if v := pvc.Metadata.Labels["cnpg.io/instanceName"]; v != "" {
		return v
	}
	return strings.TrimSuffix(pvc.Metadata.Name, "-wal")
}

func (t cnpgDestroyTarget) checks(instance string, volumes []cnpgDestroyVolume) ([]string, []string) {
	primaryLabel, ready := false, false
	if t.pod != nil {
		role := t.pod.Metadata.Labels["cnpg.io/instanceRole"]
		if role == "" {
			role = t.pod.Metadata.Labels["role"]
		}
		primaryLabel, ready = role == "primary", cnpgPodReady(*t.pod)
	}
	primary := t.cluster.Status.CurrentPrimary
	otherData := primary != "" && slices.ContainsFunc(t.others, func(p cnpgPVCObject) bool { return cnpgPVCInstance(p) == primary })
	blockers, warnings := cnpgDestroyChecks(primary, t.cluster.Status.TargetPrimary, instance, primaryLabel, otherData, ready, volumes)
	if primary == "" && !slices.Contains(blockers, cnpgDestroyPrimary) {
		// Without a known primary, nothing says which instance holds the newest data.
		blockers = append(blockers, cnpgDestroyPrimary)
	}
	if t.pod == nil && len(t.mine) == 0 {
		blockers = append(blockers, cnpgDestroyNotFound)
	}
	return blockers, warnings
}

func readCNPGDestroyPreview(ctx context.Context, k *kubeClient, namespace, name, instance string) (cnpgDestroyPreview, error) {
	r := cnpgDestroyPreview{Namespace: namespace, Name: name, Instance: instance, Volumes: []cnpgDestroyVolume{}}
	t, err := readCNPGDestroyTarget(ctx, k, namespace, name, instance)
	if err != nil {
		return r, err
	}
	r.CurrentPrimary, r.TargetPrimary = t.cluster.Status.CurrentPrimary, t.cluster.Status.TargetPrimary
	if t.pod != nil {
		r.PodPhase = t.pod.Status.Phase
		r.PodNode = t.pod.Spec.NodeName
	}
	nodes := map[string]string{}
	for _, pvc := range t.mine {
		r.Volumes = append(r.Volumes, readCNPGVolumeNode(ctx, k, pvc, nodes))
	}
	r.Blockers, r.Warnings = t.checks(instance, r.Volumes)
	r.CanDestroy = len(r.Blockers) == 0
	return r, nil
}

// readCNPGVolumeNode finds the node pvc's volume is pinned to and whether it exists; nodes
// caches the answers per node. A role that cannot read PVs or Nodes gets "unknown".
func readCNPGVolumeNode(ctx context.Context, k *kubeClient, pvc cnpgPVCObject, nodes map[string]string) cnpgDestroyVolume {
	v := cnpgDestroyVolume{Name: pvc.Metadata.Name, Role: pvc.Metadata.Labels["cnpg.io/pvcRole"], NodeState: cnpgNodeUnknown}
	if pvc.Spec.VolumeName == "" {
		return v
	}
	var pv cnpgPVObject
	if err := k.get(ctx, "/api/v1/persistentvolumes/"+url.PathEscape(pvc.Spec.VolumeName), &pv); err != nil {
		return v
	}
	v.Node = cnpgPVNode(pv)
	if v.Node == "" {
		v.NodeState = cnpgNodeNone
		return v
	}
	if state, ok := nodes[v.Node]; ok {
		v.NodeState = state
		return v
	}
	var node struct{}
	err := k.get(ctx, "/api/v1/nodes/"+url.PathEscape(v.Node), &node)
	switch {
	case err == nil:
		v.NodeState = cnpgNodePresent
	case kubeCode(err) == http.StatusNotFound:
		v.NodeState = cnpgNodeGone
	}
	nodes[v.Node] = v.NodeState
	return v
}

// cnpgPVNode is the hostname a local volume is pinned to, "" when it is not pinned to one node.
func cnpgPVNode(pv cnpgPVObject) string {
	if pv.Spec.NodeAffinity == nil {
		return ""
	}
	for _, term := range pv.Spec.NodeAffinity.Required.NodeSelectorTerms {
		for _, e := range term.MatchExpressions {
			if e.Key == "kubernetes.io/hostname" && e.Operator == "In" && len(e.Values) == 1 {
				return e.Values[0]
			}
		}
	}
	return ""
}

func destroyCNPGInstance(ctx context.Context, k *kubeClient, namespace, name, instance string) error {
	t, err := readCNPGDestroyTarget(ctx, k, namespace, name, instance)
	if err != nil {
		return err
	}
	if blockers, _ := t.checks(instance, nil); len(blockers) > 0 {
		return fmt.Errorf("cannot remove this instance: %s", strings.Join(blockers, ", "))
	}
	// The volumes first, as the cnpg plugin does: Kubernetes keeps a PVC until its pod is gone,
	// and the operator must not restart the pod on the old volume in between.
	for _, pvc := range t.mine {
		body, err := deleteOptions("Background", pvc.Metadata.ResourceVersion, -1)
		if err != nil {
			return err
		}
		path := scopedPath("/api/v1", namespace, "persistentvolumeclaims") + "/" + url.PathEscape(pvc.Metadata.Name)
		if err := k.do(ctx, http.MethodDelete, path, "application/json", body, nil); err != nil && kubeCode(err) != http.StatusNotFound {
			return fmt.Errorf("delete volume %s: %w", pvc.Metadata.Name, deleteError(err))
		}
	}
	if t.pod == nil {
		return nil
	}
	body, err := deleteOptions("Background", "", -1)
	if err != nil {
		return err
	}
	path := scopedPath("/api/v1", namespace, "pods") + "/" + url.PathEscape(instance)
	if err := k.do(ctx, http.MethodDelete, path, "application/json", body, nil); err != nil && kubeCode(err) != http.StatusNotFound {
		return fmt.Errorf("delete pod %s: %w", instance, deleteError(err))
	}
	return nil
}
