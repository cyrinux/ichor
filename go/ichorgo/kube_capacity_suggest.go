package ichorgo

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// What to scale down after losing a node: the workloads least worth keeping, until what they
// free covers what the Pending pods ask for.

// shortfall is what the Pending pods ask beyond the room still free on the remaining nodes,
// and that nothing has freed yet.
type shortfall struct {
	cpu, memory float64
	pods        int
}

const shortfallEpsilon = 1e-9

func (r shortfall) covered() bool {
	return r.cpu <= shortfallEpsilon && r.memory <= shortfallEpsilon && r.pods <= 0
}

// shortfallOf is what pending ask beyond what room has free in total. When the total would
// do but no single node has enough (the room is split), each pod's short resource counts.
func (s lossSim) shortfallOf(pending []nodeLossPending, room []simNode) shortfall {
	var asked, free shortfall

	for _, p := range pending {
		pod := s.findPod(p.Namespace, p.Name)
		asked.cpu += pod.CPU
		asked.memory += pod.Memory
		asked.pods++
	}

	for _, n := range room {
		free.cpu += max(0, n.cpu)
		free.memory += max(0, n.memory)
		free.pods += max(0, n.slots)
	}

	short := shortfall{cpu: max(0, asked.cpu-free.cpu), memory: max(0, asked.memory-free.memory), pods: max(0, asked.pods-free.pods)}
	if !short.covered() {
		return short
	}

	for _, p := range pending {
		pod := s.findPod(p.Namespace, p.Name)

		switch p.Reason {
		case pendingCPU:
			short.cpu += pod.CPU
		case pendingMemory:
			short.memory += pod.Memory
		default:
			short.pods++
		}
	}

	return short
}

// suggest walks the workloads, lowest priority first (no class first among equals), and
// proposes scaling each down by the fewest replicas that cover what is still short. It skips
// system classes, DaemonSets, bare pods, the Pending pods' own workloads and workloads whose
// replicas free nothing that is short. A BestEffort workload frees no requests but real
// memory: it is proposed at 0.
func (s lossSim) suggest(pending []nodeLossPending, room []simNode) ([]nodeLossSuggestion, bool) {
	out := []nodeLossSuggestion{}
	if len(pending) == 0 {
		return out, true
	}

	short := s.shortfallOf(pending, room)
	blocked := map[string]bool{}

	for _, p := range pending {
		kind, name, _ := strings.Cut(p.Workload, "/")
		blocked[kind+"/"+p.Namespace+"/"+name] = true
	}

	for _, w := range s.workloads {
		if short.covered() {
			break
		}

		if !suggestable(w) || blocked[w.Kind+"/"+w.Namespace+"/"+w.Name] {
			continue
		}

		if sug, ok := s.scaleDown(w, &short); ok {
			out = append(out, sug)
		}
	}

	return out, short.covered()
}

// suggestable: a workload a person may scale down, never a system class (by name and range).
func suggestable(w capacityWorkload) bool {
	return w.Kind != "DaemonSet" && w.Kind != "Pod" && w.Replicas > 0 &&
		w.Priority < systemPriority && !strings.HasPrefix(w.PriorityClassName, "system-")
}

// scaleDown is the suggestion for w, taking what it frees off short; ok is false when it
// frees nothing short is missing. Its unscheduled replicas go first and free nothing.
func (s lossSim) scaleDown(w capacityWorkload, short *shortfall) (nodeLossSuggestion, bool) {
	bestEffort := w.CPURequest == 0 && w.MemoryRequest == 0
	helps := short.cpu > shortfallEpsilon && w.CPURequest > 0 || short.memory > shortfallEpsilon && w.MemoryRequest > 0 || short.pods > 0
	if !helps && !bestEffort {
		return nodeLossSuggestion{}, false
	}

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

		return sug, true
	}

	unscheduled := s.unscheduled[w.Kind+"/"+w.Namespace+"/"+w.Name]

	need := 1.0
	if bestEffort {
		need = float64(most)
	}

	for _, n := range []float64{replicasFor(short.cpu, w.CPURequest), replicasFor(short.memory, w.MemoryRequest), float64(short.pods)} {
		need = max(need, n)
	}

	k := int(min(float64(unscheduled)+need, float64(most)))
	freed := max(0, k-unscheduled)

	sug.ScaleTo = w.Replicas - k
	sug.Frees = nodeLossFrees{CPU: float64(freed) * w.CPURequest, Memory: float64(freed) * w.MemoryRequest, Pods: freed}
	short.cpu -= sug.Frees.CPU
	short.memory -= sug.Frees.Memory
	short.pods -= freed

	return sug, true
}

// replicasFor is how many replicas of per cover missing; 0 when nothing is missing or per
// frees none of it (another resource then decides).
func replicasFor(missing, per float64) float64 {
	if missing <= shortfallEpsilon || per <= 0 {
		return 0
	}

	return math.Ceil(missing/per - shortfallEpsilon)
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
