package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// Actions on Longhorn volumes and nodes. Settings Longhorn reads from its objects (a volume's
// replica count, a node's scheduling and eviction) are merge patches carrying the
// resourceVersion read just before, so a change made in between makes them fail instead of
// being overwritten. One-off operations (a backup, a filesystem trim) go to Longhorn's own
// API, the one its UI uses, through the API server's service proxy.

const (
	lhActionBackup         = "backup"         // volume: snapshot it and back the snapshot up
	lhActionTrim           = "trim"           // volume: give the space freed in its filesystem back
	lhActionReplicas       = "replicas"       // volume: set its replica count to value
	lhActionSchedulingOn   = "schedulingOn"   // node: accept new replicas (ends an eviction)
	lhActionSchedulingOff  = "schedulingOff"  // node: no new replicas, existing ones stay
	lhActionEvict          = "evict"          // node: no new replicas, move its replicas away
	lhActionCancelEviction = "cancelEviction" // node: stop moving replicas, scheduling stays off

	// lhManagerService is Longhorn's API (longhorn-manager), port 9500 in every release.
	lhManagerService = "longhorn-backend:9500"
	lhMaxReplicas    = 20 // Longhorn refuses more
)

var (
	lhVolumeActions = []string{lhActionBackup, lhActionTrim, lhActionReplicas}
	lhNodeActions   = []string{lhActionSchedulingOn, lhActionSchedulingOff, lhActionEvict, lhActionCancelEviction}

	errLonghornMissing  = &kubeAPIError{Code: 404, Reason: "NotFound", Message: "Longhorn is not installed"}
	errLonghornDetached = &kubeAPIError{Code: 409, Reason: "Detached", Message: "the volume is not attached: attach it (start its workload) first"}
)

type lhVolumeRef struct {
	Metadata struct {
		ResourceVersion string `json:"resourceVersion"`
	} `json:"metadata"`
	Spec struct {
		NumberOfReplicas int `json:"numberOfReplicas"`
	} `json:"spec"`
	Status struct {
		State string `json:"state"`
	} `json:"status"`
}

// KubeLonghornAction runs an action on the Longhorn volume or node namespace/name (os:admin);
// namespace is Longhorn's own, where its objects live. Volume actions: backup (a snapshot,
// then its backup to the volume's target), trim (fstrim of an attached volume) and replicas
// (value: the replica count, 1-20). Node actions: schedulingOn, schedulingOff, evict (moves
// every replica away; scheduling goes off) and cancelEviction. kubeServer: see KubePods.
func KubeLonghornAction(configYAML, contextName, kubeServer, namespace, name, action string, value int) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if !slices.Contains(lhVolumeActions, action) && !slices.Contains(lhNodeActions, action) {
		return fmt.Errorf("unsupported Longhorn action %q", action)
	}

	if err := validateKubeName("Longhorn object", namespace, name); err != nil {
		return err
	}

	if action == lhActionReplicas && (value < 1 || value > lhMaxReplicas) {
		return fmt.Errorf("replica count must be between 1 and %d", lhMaxReplicas)
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return longhornAction(ctx, k, namespace, name, action, value)
	})
}

func longhornAction(ctx context.Context, k *kubeClient, namespace, name, action string, value int) error {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return err
	}

	version, ok := groups[groupLonghorn]
	if !ok {
		return errLonghornMissing
	}

	base := "/apis/" + groupLonghorn + "/" + version + "/namespaces/" + url.PathEscape(namespace)

	if slices.Contains(lhNodeActions, action) {
		return longhornNodeAction(ctx, k, base+"/nodes/"+url.PathEscape(name), action)
	}

	path := base + "/volumes/" + url.PathEscape(name)

	var vol lhVolumeRef
	if err := k.get(ctx, path, &vol); err != nil {
		return err
	}

	if action == lhActionReplicas {
		return k.patch(ctx, path, "application/merge-patch+json", map[string]any{
			"metadata": map[string]any{"resourceVersion": vol.Metadata.ResourceVersion},
			"spec":     map[string]any{"numberOfReplicas": value},
		}, nil)
	}

	if vol.Status.State != "attached" {
		return errLonghornDetached
	}

	op := "snapshotBackup"
	if action == lhActionTrim {
		op = "trimFilesystem"
	}

	// An empty input: Longhorn names the snapshot and picks the volume's backup target.
	proxy := serviceProxyPath(namespace, lhManagerService, "/v1/volumes/"+url.PathEscape(name)+"?action="+op)

	return k.post(ctx, proxy, map[string]any{}, nil)
}

func longhornNodeAction(ctx context.Context, k *kubeClient, path, action string) error {
	var node lhNodeObject
	if err := k.get(ctx, path, &node); err != nil {
		return err
	}

	spec := map[string]any{}

	switch action {
	case lhActionSchedulingOn:
		spec["allowScheduling"] = true
		spec["evictionRequested"] = false
	case lhActionSchedulingOff:
		spec["allowScheduling"] = false
	case lhActionEvict:
		// Longhorn moves replicas off a node only once it takes no new ones.
		spec["allowScheduling"] = false
		spec["evictionRequested"] = true
	case lhActionCancelEviction:
		spec["evictionRequested"] = false
	}

	return k.patch(ctx, path, "application/merge-patch+json", map[string]any{
		"metadata": map[string]any{"resourceVersion": node.Metadata.ResourceVersion},
		"spec":     spec,
	}, nil)
}
