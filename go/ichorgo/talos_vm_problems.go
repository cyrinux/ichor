package ichorgo

import (
	"strings"
	"unicode"
)

// The problems of a node's Talos virtual machines: what the checkup row and the background
// alert report. One VM raises at most one problem (one incident, one notification).
const (
	vmProblemDown       = "vmDown"       // should run, is not running, and is not on its way up
	vmProblemError      = "vmError"      // the hypervisor reports an obstacle (stage error)
	vmProblemDiskFailed = "vmDiskFailed" // a disk is not ready and says why
)

const (
	vmStageReady   = "ready"
	vmStagePending = "pending"
	vmStageError   = "error"
	vmStageUnknown = "unknown"

	vmDiskNotReady = "notReady"

	// vmProblemDetailMax clips the node's error text, which ends up in a notification.
	vmProblemDetailMax = 200
)

type vmProblem struct {
	Node   string `json:"node"`
	VM     string `json:"vm"`
	Kind   string `json:"kind"` // vmDown | vmError | vmDiskFailed
	Detail string `json:"detail"`
}

// vmProblemsOf is the problems of node's vms, in the VMs' order; never nil.
func vmProblemsOf(node string, vms []talosVM) []vmProblem {
	out := []vmProblem{}

	for _, vm := range vms {
		if kind, detail, ok := vmProblemOf(vm); ok {
			out = append(out, vmProblem{Node: node, VM: vm.Name, Kind: kind, Detail: cleanVMProblemText(detail)})
		}
	}

	return out
}

// vmProblemOf judges one VM against the hypervisor's own reconciliation (virtual_machine_status.go
// in Talos v1.15.0-beta.1): the status's stage is computed against the effective intent, a power
// override from the API included, so "ready" means the VM is where it should be.
//
//   - A VM whose configuration says stopped, or that has no spec, raises nothing.
//   - Converged and not running: stopped on purpose for this boot (a power override), nothing.
//   - stage error: vmError, the obstacle as the detail.
//   - A disk notReady with an error (observationUnavailable is a transient loss): vmDiskFailed.
//   - stage pending (starting, waiting on a link or a disk being prepared): nothing yet.
//   - stage unknown and not running (the domain is not observed): vmDown.
//
// A VM without a status yet (stage "") raises nothing: it was not observed at all.
func vmProblemOf(vm talosVM) (kind, detail string, ok bool) {
	switch {
	case vm.WantedPowerState != vmPowerRunning:
		return "", "", false
	case vm.Stage == vmStageReady && vm.PowerState != vmPowerRunning:
		return "", "", false
	case vm.Stage == vmStageError:
		return vmProblemError, vm.Error, true
	}

	for _, d := range vm.Disks {
		if d.Phase == vmDiskNotReady && strings.TrimSpace(d.Error) != "" {
			return vmProblemDiskFailed, d.Name + ": " + d.Error, true
		}
	}

	if vm.Stage == vmStageUnknown && vm.PowerState != vmPowerRunning {
		return vmProblemDown, vm.Error, true
	}

	return "", "", false
}

// cleanVMProblemText drops control characters, collapses whitespace and clips to
// vmProblemDetailMax runes: the text comes from the node.
func cleanVMProblemText(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")

	if r := []rune(s); len(r) > vmProblemDetailMax {
		s = string(r[:vmProblemDetailMax-1]) + "…"
	}

	return s
}
