package ichorgo

import (
	"context"
	"net/url"
	"strings"
	"sync"
)

// vpaTargetsParallel is how many target workloads one read GETs at a time.
const vpaTargetsParallel = 8

// vpaWorkloadPaths are the target kinds whose pod template holds the live requests.
var vpaWorkloadPaths = map[string]string{
	"Deployment":  "deployments",
	"StatefulSet": "statefulsets",
	"DaemonSet":   "daemonsets",
}

// vpaQuantities is a recommendation bound or a container's requests: cpu and memory.
type vpaQuantities map[string]string

type vpaObject struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		TargetRef struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"targetRef"`
		UpdatePolicy *struct {
			UpdateMode string `json:"updateMode"`
		} `json:"updatePolicy"`
	} `json:"spec"`
	Status struct {
		Recommendation *struct {
			ContainerRecommendations []struct {
				ContainerName string        `json:"containerName"`
				Target        vpaQuantities `json:"target"`
				LowerBound    vpaQuantities `json:"lowerBound"`
				UpperBound    vpaQuantities `json:"upperBound"`
			} `json:"containerRecommendations"`
		} `json:"recommendation"`
		Conditions kubeConditions `json:"conditions"`
	} `json:"status"`
}

// podRequests is a workload's pod-template requests by container name.
type podRequests map[string]vpaQuantities

// vpaTargetObject is the part of a Deployment, StatefulSet or DaemonSet the VPA rows read.
type vpaTargetObject struct {
	Spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Name      string `json:"name"`
					Resources struct {
						Requests vpaQuantities `json:"requests"`
					} `json:"resources"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

// readVPAs lists the VPAs and GETs each distinct target once (at most autoscalingTargetsMax
// of them) for its live requests. A target that cannot be read only leaves its requests "".
func readVPAs(ctx context.Context, k *kubeClient, version string) ([]vpaObject, map[string]podRequests, error) {
	var list kubeList[vpaObject]
	if err := getList(ctx, k, "/apis/"+groupVPA+"/"+version+"/verticalpodautoscalers", &list); err != nil {
		return nil, nil, err
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		requests = map[string]podRequests{}
		slots    = make(chan struct{}, vpaTargetsParallel)
	)

	for _, key := range vpaTargetKeys(list.Items) {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			req, ok := readTargetRequests(ctx, k, key)
			if !ok {
				return
			}

			mu.Lock()
			requests[key] = req
			mu.Unlock()
		})
	}

	wg.Wait()

	return list.Items, requests, nil
}

// vpaTargetKey is "namespace/Kind/name", the key of a target's requests.
func vpaTargetKey(namespace, kind, name string) string {
	return namespace + "/" + kind + "/" + name
}

// vpaTargetKeys is the distinct readable targets of the VPAs, at most autoscalingTargetsMax.
func vpaTargetKeys(vpas []vpaObject) []string {
	seen := map[string]bool{}
	var keys []string

	for _, v := range vpas {
		ref := v.Spec.TargetRef
		if vpaWorkloadPaths[ref.Kind] == "" || ref.Name == "" {
			continue
		}

		key := vpaTargetKey(v.Metadata.Namespace, ref.Kind, ref.Name)
		if seen[key] || len(keys) >= autoscalingTargetsMax {
			continue
		}

		seen[key] = true
		keys = append(keys, key)
	}

	return keys
}

// readTargetRequests GETs one target workload and returns its containers' requests.
func readTargetRequests(ctx context.Context, k *kubeClient, key string) (podRequests, bool) {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) != 3 {
		return nil, false
	}

	path := "/apis/apps/v1/namespaces/" + url.PathEscape(parts[0]) + "/" + vpaWorkloadPaths[parts[1]] + "/" + url.PathEscape(parts[2])

	var obj vpaTargetObject
	if err := k.get(ctx, path, &obj); err != nil {
		return nil, false
	}

	out := podRequests{}
	for _, c := range obj.Spec.Template.Spec.Containers {
		out[c.Name] = c.Resources.Requests
	}

	return out, true
}

// vpaRows maps the VPAs; requests holds the live requests readTargetRequests found.
func vpaRows(vpas []vpaObject, requests map[string]podRequests) []vpaRow {
	out := make([]vpaRow, 0, len(vpas))
	for _, v := range vpas {
		out = append(out, vpaRowOf(v, requests[vpaTargetKey(v.Metadata.Namespace, v.Spec.TargetRef.Kind, v.Spec.TargetRef.Name)]))
	}

	return out
}

func vpaRowOf(v vpaObject, live podRequests) vpaRow {
	row := vpaRow{
		Namespace:  v.Metadata.Namespace,
		Name:       v.Metadata.Name,
		Target:     autoscaleTarget{Kind: v.Spec.TargetRef.Kind, Name: v.Spec.TargetRef.Name},
		UpdateMode: "Auto", // the VPA's default
		Containers: []vpaContainer{},
	}
	if v.Spec.UpdatePolicy != nil && v.Spec.UpdatePolicy.UpdateMode != "" {
		row.UpdateMode = v.Spec.UpdatePolicy.UpdateMode
	}

	if rec := v.Status.Recommendation; rec != nil {
		for _, c := range rec.ContainerRecommendations {
			req := live[c.ContainerName]
			row.Containers = append(row.Containers, vpaContainer{
				Name:          c.ContainerName,
				CPUTarget:     c.Target["cpu"],
				MemoryTarget:  c.Target["memory"],
				CPULower:      c.LowerBound["cpu"],
				CPUUpper:      c.UpperBound["cpu"],
				MemoryLower:   c.LowerBound["memory"],
				MemoryUpper:   c.UpperBound["memory"],
				CPURequest:    req["cpu"],
				MemoryRequest: req["memory"],
			})
		}
	}

	// A fresh recommender has nothing yet: that is "no recommendation", never zero.
	cond := v.Status.Conditions.get("RecommendationProvided")
	row.RecommendationProvided = cond.Status == "True" || (cond.Status == "" && len(row.Containers) > 0)
	if !row.RecommendationProvided {
		row.Message = cond.Message
		row.State = autoscaleNotReady
	} else {
		row.State = autoscaleOK
	}

	return row
}
