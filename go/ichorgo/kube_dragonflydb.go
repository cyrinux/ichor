// Named dragonflydb, not dragonfly: a _dragonfly suffix is a GOOS build constraint.

package ichorgo

import (
	"context"
	"slices"
	"strings"
	"sync"
)

// groupDragonfly is the Dragonfly operator's API group (dragonflies, one per instance).
const groupDragonfly = "dragonflydb.io"

type dragonflyStatus struct {
	Version   string              `json:"version"`
	Error     string              `json:"error"`
	Instances []dragonflyInstance `json:"instances"`
}

type dragonflyInstance struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Phase is the operator's own word: Ready, or a step such as a rolling update.
	Phase     string         `json:"phase"`
	Health    string         `json:"health"`  // critical|warning|ok
	Reasons   []string       `json:"reasons"` // see dragonflyReason*
	Replicas  int            `json:"replicas"`
	ReadyPods int            `json:"readyPods"`
	Master    string         `json:"master"` // pod with role=master, "" when none
	Pods      []dragonflyPod `json:"pods"`
}

type dragonflyPod struct {
	Name  string `json:"name"`
	Node  string `json:"node"`
	Phase string `json:"phase"`
	Role  string `json:"role"` // the operator's "role" label: master or replica
	Ready bool   `json:"ready"`
}

// Reasons a Dragonfly instance is not ok, for the app to word.
const (
	dragonflyReasonNoReady  = "noReady"  // no pod ready: the instance is down
	dragonflyReasonNoMaster = "noMaster" // no pod has the master role
	dragonflyReasonMasters  = "masters"  // more than one master
	dragonflyReasonPods     = "pods"     // fewer ready pods than replicas
	dragonflyReasonNotReady = "notReady" // the operator's phase is not Ready
)

type dragonflyObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int `json:"replicas"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

// readDragonfly lists the Dragonfly instances and their pods (labelled by the operator with
// app.kubernetes.io/name=dragonfly, app=<instance> and role=master|replica).
func readDragonfly(ctx context.Context, k *kubeClient, version string) *dragonflyStatus {
	var (
		list lhList[dragonflyObject]
		pods []dsPod
		errs = make([]error, 2)
		wg   sync.WaitGroup
	)

	wg.Go(func() { errs[0] = k.get(ctx, "/apis/"+groupDragonfly+"/"+version+"/dragonflies", &list) })
	wg.Go(func() { pods, errs[1] = listDSPods(ctx, k, "app.kubernetes.io/name=dragonfly") })
	wg.Wait()

	out := mapDragonfly(list.Items, pods)
	out.Version = version
	out.Error = sectionError(errs...)

	return out
}

func mapDragonfly(objects []dragonflyObject, pods []dsPod) *dragonflyStatus {
	out := &dragonflyStatus{Instances: []dragonflyInstance{}}

	byInstance := map[string][]dragonflyPod{}

	for _, p := range pods {
		name := p.Metadata.Labels["app"]
		if name == "" {
			continue
		}

		key := p.Metadata.Namespace + "/" + name
		byInstance[key] = append(byInstance[key], dragonflyPod{
			Name: p.Metadata.Name, Node: p.Spec.NodeName, Phase: p.Status.Phase,
			Role: p.Metadata.Labels["role"], Ready: p.containerReady(""),
		})
	}

	for _, obj := range objects {
		out.Instances = append(out.Instances, mapDragonflyInstance(obj, byInstance[obj.Metadata.Namespace+"/"+obj.Metadata.Name]))
	}

	slices.SortFunc(out.Instances, func(a, b dragonflyInstance) int {
		if d := healthRank(a.Health) - healthRank(b.Health); d != 0 {
			return d
		}

		return strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name)
	})

	return out
}

func mapDragonflyInstance(obj dragonflyObject, pods []dragonflyPod) dragonflyInstance {
	inst := dragonflyInstance{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name, Phase: obj.Status.Phase,
		Replicas: 1, Pods: pods, Reasons: []string{},
	}

	if obj.Spec.Replicas != nil {
		inst.Replicas = *obj.Spec.Replicas
	}

	if inst.Pods == nil {
		inst.Pods = []dragonflyPod{}
	}

	// The master first, then by name.
	slices.SortFunc(inst.Pods, func(a, b dragonflyPod) int {
		if (a.Role == "master") != (b.Role == "master") {
			if a.Role == "master" {
				return -1
			}

			return 1
		}

		return strings.Compare(a.Name, b.Name)
	})

	masters := 0

	for _, p := range inst.Pods {
		if p.Ready {
			inst.ReadyPods++
		}

		if p.Role == "master" {
			masters++

			if inst.Master == "" {
				inst.Master = p.Name
			}
		}
	}

	inst.Health, inst.Reasons = dragonflyHealth(inst, masters)

	return inst
}

func dragonflyHealth(inst dragonflyInstance, masters int) (string, []string) {
	reasons := []string{}
	critical := false

	switch {
	case inst.Replicas > 0 && inst.ReadyPods == 0:
		reasons, critical = append(reasons, dragonflyReasonNoReady), true
	case inst.Replicas > 0 && masters == 0:
		reasons, critical = append(reasons, dragonflyReasonNoMaster), true
	}

	if masters > 1 {
		reasons = append(reasons, dragonflyReasonMasters)
	}

	if inst.ReadyPods > 0 && inst.ReadyPods < inst.Replicas {
		reasons = append(reasons, dragonflyReasonPods)
	}

	if inst.Phase != "" && !strings.EqualFold(inst.Phase, "ready") && len(reasons) == 0 {
		reasons = append(reasons, dragonflyReasonNotReady)
	}

	switch {
	case critical:
		return healthCritical, reasons
	case len(reasons) > 0:
		return healthWarning, reasons
	default:
		return healthOK, reasons
	}
}
