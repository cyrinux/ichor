package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
)

// groupCastAI is CAST AI's autoscaling API group; its Recommendation objects (one per workload
// the Workload Autoscaler manages) carry the requests and limits it would set.
const groupCastAI = "autoscaling.cast.ai"

// castAIVersion is the only version the Recommendation CRD serves.
const castAIVersion = "v1"

// Annotations the Workload Autoscaler writes on a Recommendation.
const (
	castAIAnnotationMode      = "autoscaling.cast.ai/recommendation-apply-mode"      // immediate or deferred
	castAIAnnotationFirstSeen = "autoscaling.cast.ai/first-seen-container-resources" // the requests before CAST AI
)

type castAIStatus struct {
	Version         string                 `json:"version"`
	Error           string                 `json:"error"`
	Recommendations []castAIRecommendation `json:"recommendations"`
	// Sum over every workload of recommended minus original requests: negative is a saving.
	CPUDeltaMilli    int64 `json:"cpuDeltaMilli"`
	MemoryDeltaBytes int64 `json:"memoryDeltaBytes"`
	// Workloads whose original requests are known (the deltas count them only).
	Compared int `json:"compared"`
	// Node consolidations (RebalancePlan), newest first, and the nodes they keep failing to remove.
	Plans []castAIPlan      `json:"plans"`
	Stuck []castAIStuckNode `json:"stuck"`
	// Why the plans could not be read; Error is the recommendations' only.
	PlansError string `json:"plansError"`
}

type castAIRecommendation struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// The workload the recommendation targets: its kind (Deployment, StatefulSet, CronJob) and name.
	Kind     string `json:"kind"`
	Workload string `json:"workload"`
	// Mode is how CAST AI applies it: immediate (pods restarted), deferred (in place or on the next
	// restart), "" when unknown.
	Mode string `json:"mode"`
	// ReadOnly: the autoscaler is told not to apply it (Message says why, in CAST AI's words).
	ReadOnly   bool              `json:"readOnly"`
	Health     string            `json:"health"`  // critical|warning|ok
	Reasons    []string          `json:"reasons"` // see castAIReason*
	Message    string            `json:"message"` // the failing condition's message, or the read-only reason
	Containers []castAIContainer `json:"containers"`
	// Deltas as on castAIStatus, for this workload (0 when the original requests are unknown).
	CPUDeltaMilli    int64 `json:"cpuDeltaMilli"`
	MemoryDeltaBytes int64 `json:"memoryDeltaBytes"`
	// A pod's requests as recommended, and before CAST AI (a container whose original is unknown
	// counts at its recommended value, so recommended minus original is the delta).
	CPUMilli            int64 `json:"cpuMilli"`
	MemoryBytes         int64 `json:"memoryBytes"`
	OriginalCPUMilli    int64 `json:"originalCpuMilli"`
	OriginalMemoryBytes int64 `json:"originalMemoryBytes"`
}

// castAIContainer is one container's requests and limits as CAST AI recommends them, with
// the requests it first saw ("" when unknown).
type castAIContainer struct {
	Name           string `json:"name"`
	CPU            string `json:"cpu"`
	Memory         string `json:"memory"`
	CPULimit       string `json:"cpuLimit"`
	MemoryLimit    string `json:"memoryLimit"`
	OriginalCPU    string `json:"originalCpu"`
	OriginalMemory string `json:"originalMemory"`
	// The recommended request as a share of the limit, 0 without a limit: CAST AI keeps limits,
	// so a request close to its memory limit leaves little room before an OOM kill.
	CPULimitPercent    int `json:"cpuLimitPercent"`
	MemoryLimitPercent int `json:"memoryLimitPercent"`
}

// Reasons a recommendation needs a look, for the app to word.
const (
	castAIReasonVPA      = "vpa"      // the VPAHealthy condition is not True
	castAIReasonHPA      = "hpa"      // the HPAHealthy condition is not True
	castAIReasonReadOnly = "readOnly" // apply policy is read-only
)

// castAIObject holds the fields of a Recommendation the reader uses.
type castAIObject struct {
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		TargetRef struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"targetRef"`
		Recommendation []castAIAdjustment `json:"recommendation"`
		ApplyPolicy    *struct {
			Readonly bool `json:"readonly"`
			Reasons  []struct {
				ID      string `json:"id"`
				Message string `json:"message"`
			} `json:"reasons"`
		} `json:"applyPolicy"`
	} `json:"spec"`
	Status struct {
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"conditions"`
	} `json:"status"`
}

type castAIAdjustment struct {
	ContainerName string                  `json:"containerName"`
	Requests      map[string]castQuantity `json:"requests"`
	Limits        map[string]castQuantity `json:"limits"`
}

// castQuantity is a Kubernetes quantity CAST AI writes as a string ("250m") or a bare number.
type castQuantity string

func (q *castQuantity) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*q = castQuantity(s)

		return nil
	}

	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("quantity %s: %w", b, err)
	}

	*q = castQuantity(n.String())

	return nil
}

// readCastAI lists the Recommendations of every namespace and the rebalance plans. Each kind
// may be missing (a cluster with only one of the two CAST AI components): read as none.
func readCastAI(ctx context.Context, k *kubeClient) *castAIStatus {
	var list kubeList[castAIObject]

	err := ignoreNotFound(getList(ctx, k, "/apis/"+groupCastAI+"/"+castAIVersion+"/recommendations", &list))
	plans, plansErr := readCastAIPlans(ctx, k)

	out := mapCastAI(list.Items)
	out.Plans, out.Stuck = plans, castAIStuckNodes(plans)
	out.Error, out.PlansError = sectionError(err), sectionError(plansErr)

	return out
}

func mapCastAI(objects []castAIObject) *castAIStatus {
	out := &castAIStatus{Version: castAIVersion, Recommendations: []castAIRecommendation{}, Plans: []castAIPlan{}, Stuck: []castAIStuckNode{}}

	for _, obj := range objects {
		rec := mapCastAIRecommendation(obj)
		out.Recommendations = append(out.Recommendations, rec)

		if rec.CPUDeltaMilli != 0 || rec.MemoryDeltaBytes != 0 {
			out.Compared++
			out.CPUDeltaMilli += rec.CPUDeltaMilli
			out.MemoryDeltaBytes += rec.MemoryDeltaBytes
		}
	}

	slices.SortFunc(out.Recommendations, byHealthThenKey(func(r castAIRecommendation) (string, string) { return r.Health, r.Namespace + "/" + r.Name }))

	return out
}

func mapCastAIRecommendation(obj castAIObject) castAIRecommendation {
	rec := castAIRecommendation{
		Namespace: obj.Metadata.Namespace, Name: obj.Metadata.Name,
		Kind: obj.Spec.TargetRef.Kind, Workload: obj.Spec.TargetRef.Name,
		Mode:    obj.Metadata.Annotations[castAIAnnotationMode],
		Reasons: []string{}, Containers: []castAIContainer{},
	}

	firstSeen := parseCastAIFirstSeen(obj.Metadata.Annotations[castAIAnnotationFirstSeen])

	for _, adj := range obj.Spec.Recommendation {
		c := castAIContainer{
			Name: adj.ContainerName,
			CPU:  string(adj.Requests["cpu"]), Memory: string(adj.Requests["memory"]),
			CPULimit: string(adj.Limits["cpu"]), MemoryLimit: string(adj.Limits["memory"]),
		}

		if orig, ok := firstSeen[adj.ContainerName]; ok {
			c.OriginalCPU, c.OriginalMemory = orig.CPU, orig.Memory
		}

		rec.Containers = append(rec.Containers, c)
	}

	slices.SortFunc(rec.Containers, func(a, b castAIContainer) int { return strings.Compare(a.Name, b.Name) })
	rec = withCastAITotals(rec)

	rec.Health, rec.Reasons, rec.Message = castAIHealth(obj)
	rec.ReadOnly = obj.Spec.ApplyPolicy != nil && obj.Spec.ApplyPolicy.Readonly

	return rec
}

// castAIFirstSeen is the shape of the first-seen-container-resources annotation: the requests
// and limits of each container before CAST AI touched them, keyed by container name.
type castAIFirstSeen struct {
	Requests map[string]castQuantity `json:"requests"`
}

type castAIOriginal struct{ CPU, Memory string }

func parseCastAIFirstSeen(annotation string) map[string]castAIOriginal {
	out := map[string]castAIOriginal{}
	if annotation == "" {
		return out
	}

	var seen map[string]castAIFirstSeen
	if err := json.Unmarshal([]byte(annotation), &seen); err != nil {
		return out
	}

	for name, s := range seen {
		out[name] = castAIOriginal{CPU: string(s.Requests["cpu"]), Memory: string(s.Requests["memory"])}
	}

	return out
}

// withCastAITotals fills what follows from the containers: each one's share of its limits, the
// pod's requests before and after, and the deltas over the containers whose originals are known.
func withCastAITotals(rec castAIRecommendation) castAIRecommendation {
	out := rec
	out.Containers = make([]castAIContainer, 0, len(rec.Containers))
	out.CPUDeltaMilli, out.MemoryDeltaBytes = 0, 0
	out.CPUMilli, out.MemoryBytes, out.OriginalCPUMilli, out.OriginalMemoryBytes = 0, 0, 0, 0

	for _, c := range rec.Containers {
		c.CPULimitPercent, c.MemoryLimitPercent = limitPercent(c.CPU, c.CPULimit), limitPercent(c.Memory, c.MemoryLimit)
		dCPU, dMem := milliDelta(c.OriginalCPU, c.CPU), bytesDelta(c.OriginalMemory, c.Memory)
		cpu, mem := int64(math.Round(parseQuantity(c.CPU)*1000)), int64(math.Round(parseQuantity(c.Memory)))
		// The original when known, else the recommended value (no change to count); a resource
		// CAST AI recommends nothing for keeps its original request.
		origCPU, origMem := cpu, mem
		if c.OriginalCPU != "" {
			origCPU = int64(math.Round(parseQuantity(c.OriginalCPU) * 1000))
			if c.CPU == "" {
				cpu = origCPU
			}
		}

		if c.OriginalMemory != "" {
			origMem = int64(math.Round(parseQuantity(c.OriginalMemory)))
			if c.Memory == "" {
				mem = origMem
			}
		}

		out.CPUDeltaMilli += dCPU
		out.MemoryDeltaBytes += dMem
		out.CPUMilli += cpu
		out.MemoryBytes += mem
		out.OriginalCPUMilli += origCPU
		out.OriginalMemoryBytes += origMem
		out.Containers = append(out.Containers, c)
	}

	return out
}

// limitPercent is request over limit in percent, 0 when either is unknown.
func limitPercent(request, limit string) int {
	l := parseQuantity(limit)
	if request == "" || l <= 0 {
		return 0
	}

	return int(math.Round(parseQuantity(request) / l * 100))
}

// milliDelta is to minus from in millicores, 0 when either is unknown.
func milliDelta(from, to string) int64 {
	if from == "" || to == "" {
		return 0
	}

	return int64(math.Round((parseQuantity(to) - parseQuantity(from)) * 1000))
}

// bytesDelta is to minus from in bytes, 0 when either is unknown.
func bytesDelta(from, to string) int64 {
	if from == "" || to == "" {
		return 0
	}

	return int64(math.Round(parseQuantity(to) - parseQuantity(from)))
}

// castAIHealth: a VPA that cannot sync is critical (the recommendation is not applied); an HPA
// problem or a read-only policy is a warning.
func castAIHealth(obj castAIObject) (health string, reasons []string, message string) {
	reasons = []string{}

	for _, c := range obj.Status.Conditions {
		if c.Status == "True" {
			continue
		}

		switch c.Type {
		case "VPAHealthy":
			reasons = append(reasons, castAIReasonVPA)
		case "HPAHealthy":
			reasons = append(reasons, castAIReasonHPA)
		default:
			continue
		}

		if message == "" {
			message = c.Message
		}
	}

	if p := obj.Spec.ApplyPolicy; p != nil && p.Readonly {
		reasons = append(reasons, castAIReasonReadOnly)

		if message == "" && len(p.Reasons) > 0 {
			message = p.Reasons[0].Message
		}
	}

	switch {
	case slices.Contains(reasons, castAIReasonVPA):
		return healthCritical, reasons, message
	case len(reasons) > 0:
		return healthWarning, reasons, message
	default:
		return healthOK, reasons, message
	}
}
