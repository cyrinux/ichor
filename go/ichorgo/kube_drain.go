package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Drain like `kubectl drain --ignore-daemonsets --delete-emptydir-data`, but never forced:
// pods leave through the Eviction API, so PodDisruptionBudgets are honoured, and a budget
// that allows no disruption makes the drain wait (it never deletes around it). See
// plans/roadmap/devops/01-node-maintenance.md (M1, M2).

const (
	mirrorPodAnnotation = "kubernetes.io/config.mirror"
	drainPollInterval   = 5 * time.Second
)

// Drain pod kinds: what a drain does with a pod.
const (
	drainEvict     = "evict"     // evicted (a controller recreates it elsewhere)
	drainBare      = "bare"      // no controller: evicted only when allowed, it is not recreated
	drainDaemonSet = "daemonset" // left alone: its DaemonSet would recreate it on the node
	drainStatic    = "static"    // left alone: a static pod of the node (control plane)
)

// Drain pod states during a run.
const (
	podPending  = "pending"  // not evicted yet
	podEvicting = "evicting" // eviction accepted, waiting for the pod to go
	podBlocked  = "blocked"  // a PodDisruptionBudget refuses the eviction for now
	podGone     = "gone"
)

// drainPod is one pod of the node in a maintenance plan or run.
type drainPod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Owner     string `json:"owner"` // "ReplicaSet/web-5d8f", "" when none
	Kind      string `json:"kind"`  // evict, bare, daemonset, static
	// EmptyDir: the pod keeps data in an emptyDir volume, lost when it is evicted.
	EmptyDir bool `json:"emptyDir"`
	// PDB is the PodDisruptionBudget covering the pod ("" when none) and PDBAllowed the
	// disruptions it allows right now.
	PDB        string `json:"pdb"`
	PDBAllowed int    `json:"pdbAllowed"`
	State      string `json:"state,omitempty"`
	Reason     string `json:"reason,omitempty"`

	uid string
}

type drainPodObject struct {
	Metadata struct {
		Name              string            `json:"name"`
		Namespace         string            `json:"namespace"`
		UID               string            `json:"uid"`
		Labels            map[string]string `json:"labels"`
		Annotations       map[string]string `json:"annotations"`
		DeletionTimestamp *time.Time        `json:"deletionTimestamp"`
		OwnerReferences   []struct {
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			Controller *bool  `json:"controller"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		NodeName string `json:"nodeName"`
		Volumes  []struct {
			EmptyDir *struct{} `json:"emptyDir"`
		} `json:"volumes"`
	} `json:"spec"`
}

type pdbObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Selector *labelSelector `json:"selector"`
	} `json:"spec"`
	Status struct {
		DisruptionsAllowed int `json:"disruptionsAllowed"`
	} `json:"status"`
}

// drainPods lists the pods of kubeNode with what a drain does with each, the ones to evict
// first, sorted by namespace and name.
func drainPods(ctx context.Context, k *kubeClient, kubeNode string) ([]drainPod, error) {
	var pods struct {
		Items []drainPodObject `json:"items"`
	}

	if err := k.get(ctx, "/api/v1/pods?fieldSelector="+url.QueryEscape("spec.nodeName="+kubeNode), &pods); err != nil {
		return nil, fmt.Errorf("pods of %s: %w", kubeNode, err)
	}

	var pdbs struct {
		Items []pdbObject `json:"items"`
	}

	// Without the budgets (RBAC, old API) the drain still works: the evictions honour them.
	_ = k.get(ctx, "/apis/policy/v1/poddisruptionbudgets", &pdbs) //nolint:errcheck

	out := make([]drainPod, 0, len(pods.Items))
	for _, obj := range pods.Items {
		out = append(out, classifyDrainPod(obj, pdbs.Items))
	}

	sort.Slice(out, func(i, j int) bool {
		if a, b := out[i].Kind == drainEvict || out[i].Kind == drainBare, out[j].Kind == drainEvict || out[j].Kind == drainBare; a != b {
			return a
		}

		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}

		return out[i].Name < out[j].Name
	})

	return out, nil
}

func classifyDrainPod(obj drainPodObject, pdbs []pdbObject) drainPod {
	m := obj.Metadata
	p := drainPod{Namespace: m.Namespace, Name: m.Name, Kind: drainBare, PDBAllowed: -1, uid: m.UID}

	for _, o := range m.OwnerReferences {
		if o.Controller != nil && *o.Controller {
			p.Owner = o.Kind + "/" + o.Name
			p.Kind = drainEvict

			if o.Kind == "DaemonSet" {
				p.Kind = drainDaemonSet
			}
		}
	}

	if _, mirror := m.Annotations[mirrorPodAnnotation]; mirror {
		p.Kind = drainStatic
	}

	for _, v := range obj.Spec.Volumes {
		if v.EmptyDir != nil {
			p.EmptyDir = true
		}
	}

	labels := labelSet(m.Labels)

	for _, b := range pdbs {
		if b.Metadata.Namespace == m.Namespace && b.Spec.Selector != nil && b.Spec.Selector.matches(labels) {
			p.PDB, p.PDBAllowed = b.Metadata.Name, b.Status.DisruptionsAllowed

			break
		}
	}

	return p
}

// toEvict keeps the pods the drain evicts: controller pods, and bare ones when includeBare.
func toEvict(pods []drainPod, includeBare bool) []drainPod {
	var out []drainPod

	for _, p := range pods {
		if p.Kind == drainEvict || p.Kind == drainBare && includeBare {
			p.State = podPending
			out = append(out, p)
		}
	}

	return out
}

// errDrainStopped is returned when the run is cancelled during the drain.
var errDrainStopped = errors.New("stopped during the drain")

// drain evicts pods until every one is gone, retrying those a PodDisruptionBudget blocks;
// report gets the pods on every change. It stops at ctx's end.
func drain(ctx context.Context, k *kubeClient, pods []drainPod, report func([]drainPod), interval time.Duration) error {
	for {
		changed, pending := false, 0

		for i := range pods {
			before := pods[i]

			if err := drainStep(ctx, k, &pods[i]); err != nil {
				return err
			}

			if pods[i].State != podGone {
				pending++
			}

			changed = changed || pods[i].State != before.State || pods[i].Reason != before.Reason
		}

		if changed {
			report(pods)
		}

		if pending == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return errDrainStopped
		case <-time.After(interval):
		}
	}
}

// drainStep moves one pod forward: evict it, or check whether an evicted pod is gone.
func drainStep(ctx context.Context, k *kubeClient, p *drainPod) error {
	if ctx.Err() != nil {
		return errDrainStopped
	}

	path := podPath(p.Namespace, p.Name)

	switch p.State {
	case podGone:
		return nil
	case podEvicting:
		var obj drainPodObject

		err := k.get(ctx, path, &obj)

		switch {
		case isKubeNotFound(err):
			p.State, p.Reason = podGone, ""
		case err != nil:
			p.Reason = err.Error() // transient: checked again next round
		case obj.Metadata.UID != p.uid:
			p.State, p.Reason = podGone, "" // recreated under the same name (StatefulSet)
		}

		return nil
	}

	eviction := map[string]any{
		"apiVersion": "policy/v1",
		"kind":       "Eviction",
		"metadata":   map[string]string{"name": p.Name, "namespace": p.Namespace},
	}

	err := k.post(ctx, path+"/eviction", eviction, nil)

	var apiErr *kubeAPIError

	switch {
	case err == nil:
		p.State, p.Reason = podEvicting, ""
	case isKubeNotFound(err):
		p.State, p.Reason = podGone, ""
	case errors.As(err, &apiErr) && apiErr.Code == 429:
		p.State, p.Reason = podBlocked, blockedReason(*p, apiErr.Message)
	case errors.As(err, &apiErr) && (apiErr.Code == 401 || apiErr.Code == 403):
		return fmt.Errorf("evicting %s/%s: %w", p.Namespace, p.Name, err)
	case errors.As(err, &apiErr) && strings.Contains(apiErr.Message, "more than one PodDisruptionBudget"):
		// Never evictable until the budgets are fixed: kubectl drain fails too.
		return fmt.Errorf("evicting %s/%s: %w", p.Namespace, p.Name, err)
	default:
		p.Reason = err.Error() // transient (500, network): retried next round
	}

	return nil
}

func blockedReason(p drainPod, message string) string {
	if p.PDB != "" {
		return "PodDisruptionBudget " + p.PDB + " allows no disruption now"
	}

	if message != "" {
		return message
	}

	return "a PodDisruptionBudget allows no disruption now"
}

func isKubeNotFound(err error) bool {
	var apiErr *kubeAPIError

	return errors.As(err, &apiErr) && apiErr.Code == 404
}

// setUnschedulable cordons (true) or uncordons kubeNode, like `kubectl cordon`/`uncordon`.
func setUnschedulable(ctx context.Context, k *kubeClient, kubeNode string, on bool) error {
	patch := map[string]any{"spec": map[string]any{"unschedulable": on}}

	if err := k.patch(ctx, "/api/v1/nodes/"+url.PathEscape(kubeNode), "application/merge-patch+json", patch, nil); err != nil {
		verb := "uncordon"
		if on {
			verb = "cordon"
		}

		return fmt.Errorf("%s %s: %w", verb, kubeNode, err)
	}

	return nil
}
