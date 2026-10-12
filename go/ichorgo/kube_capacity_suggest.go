package ichorgo

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// What to scale down after losing a node: the workloads least worth keeping, until what they
// free covers what the Pending pods ask for.

// shortfall is what the Pending pods ask and nothing has freed yet. Pods counts only the pods
// Pending for want of a slot: the others need CPU or memory, not a slot.
type shortfall struct {
	cpu, memory float64
	pods        int
}

const shortfallEpsilon = 1e-9

func (r shortfall) covered() bool {
	return r.cpu <= shortfallEpsilon && r.memory <= shortfallEpsilon && r.pods <= 0
}

// suggest walks the workloads, lowest priority first (no class first among equals), and
// proposes scaling each down by the fewest replicas that cover what is still short. It skips
// system classes, DaemonSets, bare pods and the Pending pods' own workloads. A BestEffort
// workload frees no requests but real memory: it comes first, scaled to 0.
func (s lossSim) suggest(pending []nodeLossPending) ([]nodeLossSuggestion, bool) {
	out := []nodeLossSuggestion{}
	if len(pending) == 0 {
		return out, true
	}

	var short shortfall

	blocked := map[string]bool{}

	for _, p := range pending {
		pod := s.findPod(p.Namespace, p.Name)
		short.cpu += pod.CPU
		short.memory += pod.Memory

		if p.Reason == pendingPods {
			short.pods++
		}

		kind, name, _ := strings.Cut(p.Workload, "/")
		blocked[kind+"/"+p.Namespace+"/"+name] = true
	}

	for _, w := range s.workloads {
		if short.covered() {
			break
		}

		if suggestable(w) && !blocked[w.Kind+"/"+w.Namespace+"/"+w.Name] {
			out = append(out, s.scaleDown(w, &short))
		}
	}

	return out, short.covered()
}

// suggestable: a workload a person may scale down, never a system class (by name and range).
func suggestable(w capacityWorkload) bool {
	return w.Kind != "DaemonSet" && w.Kind != "Pod" && w.Replicas > 0 &&
		w.Priority < systemPriority && !strings.HasPrefix(w.PriorityClassName, "system-")
}

// scaleDown is the suggestion for w, taking what it frees off short.
func (s lossSim) scaleDown(w capacityWorkload, short *shortfall) nodeLossSuggestion {
	sug := nodeLossSuggestion{
		Kind: w.Kind, Namespace: w.Namespace, Name: w.Name, Replicas: w.Replicas, ScaleTo: w.Replicas,
		Priority: w.Priority, PriorityClassName: w.PriorityClassName, NoPriorityClass: w.NoPriorityClass, PDB: w.PDB,
	}

	most := w.Replicas

	if w.PDB != "" {
		sug.PDBMinAvailable = pdbMinAvailable(s.pdbs[w.Namespace+"/"+w.PDB], w.Replicas, w.PDBAllowed)
		most = max(0, w.Replicas-sug.PDBMinAvailable)
	}

	if most == 0 {
		sug.PDBBlocks = true

		return sug
	}

	k := 1
	for _, need := range []float64{replicasFor(short.cpu, w.CPURequest), replicasFor(short.memory, w.MemoryRequest), float64(short.pods)} {
		if need > float64(k) {
			k = int(min(need, float64(most)))
		}
	}

	k = min(k, most)
	sug.ScaleTo = w.Replicas - k
	sug.Frees = nodeLossFrees{CPU: float64(k) * w.CPURequest, Memory: float64(k) * w.MemoryRequest, Pods: k}
	short.cpu -= sug.Frees.CPU
	short.memory -= sug.Frees.Memory
	short.pods -= k

	return sug
}

// replicasFor is how many replicas of per cover missing, +Inf when they cannot (per is 0).
func replicasFor(missing, per float64) float64 {
	switch {
	case missing <= shortfallEpsilon:
		return 0
	case per <= 0:
		return math.Inf(1)
	default:
		return math.Ceil(missing/per - shortfallEpsilon)
	}
}

func (s lossSim) findPod(namespace, name string) checkPod {
	for _, p := range s.pods {
		if p.Namespace == namespace && p.Name == name {
			return p
		}
	}

	return checkPod{}
}

// pdbMinAvailable is how many of replicas the budget keeps: its minAvailable, or replicas
// minus its maxUnavailable (a percentage rounds up, as the disruption controller does), else
// what its status allows right now.
func pdbMinAvailable(b pdbObject, replicas, allowed int) int {
	if n, ok := pdbBound(b.Spec.MinAvailable, replicas); ok {
		return min(n, replicas)
	}

	if n, ok := pdbBound(b.Spec.MaxUnavailable, replicas); ok {
		return max(0, replicas-n)
	}

	return max(0, replicas-max(0, allowed))
}

// pdbBound reads an int-or-percentage PDB field against replicas.
func pdbBound(raw json.RawMessage, replicas int) (int, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}

	var n int
	if json.Unmarshal(raw, &n) == nil {
		return max(0, n), true
	}

	var s string
	if json.Unmarshal(raw, &s) != nil {
		return 0, false
	}

	percent, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	if err != nil || !strings.HasSuffix(s, "%") {
		return 0, false
	}

	return int(math.Ceil(float64(replicas) * float64(percent) / 100)), true
}
