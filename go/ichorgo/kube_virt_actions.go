package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// KubeVirt's virtual-machine actions, the way the virtctl CLI does them: start, stop, restart,
// migrate, pause and unpause are subresources the API server accepts before the VM moves, the
// controller carrying them out afterwards. A call returns as soon as the request is accepted:
// the apps refresh KubeVirtualMachines to watch the VM move, never poll here.

// The verbs KubeVMAction accepts, one subresource each.
const (
	vmActionStart   = "start"
	vmActionStop    = "stop"
	vmActionRestart = "restart"
	vmActionMigrate = "migrate"
	vmActionPause   = "pause"
	vmActionUnpause = "unpause"
)

// errKubeVirtAbsent is an action on a cluster that does not serve KubeVirt at all.
var errKubeVirtAbsent = errors.New("this cluster does not serve KubeVirt")

// What would not move the VM, said before asking rather than learnt from the API server's 409
// after it. The subresources answer a conflict the same way; saying it first keeps the app's
// message the same on every cluster.
var (
	errVMRunning    = &kubeAPIError{Code: 409, Reason: "VMAlreadyRunning", Message: "the virtual machine is already running"}
	errVMStarting   = &kubeAPIError{Code: 409, Reason: "VMAlreadyStarting", Message: "the virtual machine is already starting"}
	errVMStopped    = &kubeAPIError{Code: 409, Reason: "VMAlreadyStopped", Message: "the virtual machine is already stopped"}
	errVMNotRunning = &kubeAPIError{Code: 409, Reason: "VMNotRunning", Message: "the virtual machine is not running"}
	errVMHalted     = &kubeAPIError{Code: 409, Reason: "RestartNotSupported", Message: `runStrategy "Halted" does not support a manual restart: start the virtual machine instead`}
	errVMMigrating  = &kubeAPIError{Code: 409, Reason: "MigrationInProgress", Message: "a migration of this virtual machine is already in flight"}
	errVMNotPaused  = &kubeAPIError{Code: 409, Reason: "VMNotPaused", Message: "the virtual machine is not paused"}
)

// vmAction is what one of KubeVMAction's verbs does: the collection its subresource hangs off
// (the instance for pause and unpause, the virtual machine for the rest), and the audit
// action it records.
type vmAction struct {
	resource string
	audit    string
}

var vmActions = map[string]vmAction{
	vmActionStart:   {"virtualmachines", "vm-start"},
	vmActionStop:    {"virtualmachines", "vm-stop"},
	vmActionRestart: {"virtualmachines", "vm-restart"},
	vmActionMigrate: {"virtualmachines", "vm-migrate"},
	vmActionPause:   {"virtualmachineinstances", "vm-pause"},
	vmActionUnpause: {"virtualmachineinstances", "vm-unpause"},
}

// vmActionResult is what an action answers: the verb run ("stop-force" for the forced stop),
// the virtual machine, and the migration it started ("" when none).
type vmActionResult struct {
	Action    string `json:"action"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Migration string `json:"migration"`
}

// KubeVMAction runs action on the KubeVirt virtual machine namespace/name (os:admin), one of
// start, stop, restart, migrate, pause and unpause, like `virtctl start|stop|restart|migrate|
// pause|unpause`: a PUT of the subresource under subresources.kubevirt.io, after reading the
// VM. Refused when the VM's status already tells the request would not move it (see
// vmRefusal). Returns {"action","namespace","name","migration"} (see vmActionResult); for
// migrate, migration is the VirtualMachineInstanceMigration the call started, so the app can
// follow it. kubeServer: see KubePods.
func KubeVMAction(configYAML, contextName, kubeServer, namespace, name, action string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	action = strings.TrimSpace(action)

	a, known := vmActions[action]
	if !known {
		return "", fmt.Errorf("unknown virtual machine action %q", action)
	}

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: a.audit, Namespace: namespace, Object: "VirtualMachine/" + name})

	if err := validateKubeName("virtual machine", namespace, name); err != nil {
		return "", err
	}

	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		migration, err := runVMAction(ctx, k, namespace, name, action, nil)
		if err != nil {
			return err
		}

		out, err = toJSON(vmActionResult{Action: action, Namespace: namespace, Name: name, Migration: migration})

		return err
	})

	return out, err
}

// KubeVMStop stops the KubeVirt virtual machine namespace/name hard, like `virtctl stop
// --force --grace-period N`: the stop subresource with the grace period the caller sets
// (0 kills the VM at once; a negative one is refused). Same gates as KubeVMAction's stop;
// audit action vm-stop-force. Returns vmActionResult with action "stop-force". kubeServer:
// see KubePods.
func KubeVMStop(configYAML, contextName, kubeServer, namespace, name string, gracePeriodSeconds int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	if gracePeriodSeconds < 0 {
		return "", fmt.Errorf("the grace period cannot be negative (%d)", gracePeriodSeconds)
	}

	defer recordAction(&err, configYAML, contextName, auditAction{
		Server: kubeServer, Action: "vm-stop-force", Namespace: namespace, Object: "VirtualMachine/" + name,
		Params: "gracePeriod=" + strconv.Itoa(gracePeriodSeconds),
	})

	if err := validateKubeName("virtual machine", namespace, name); err != nil {
		return "", err
	}

	err = kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		grace := gracePeriodSeconds

		migration, err := runVMAction(ctx, k, namespace, name, vmActionStop, &grace)
		if err != nil {
			return err
		}

		out, err = toJSON(vmActionResult{Action: "stop-force", Namespace: namespace, Name: name, Migration: migration})

		return err
	})

	return out, err
}

// runVMAction reads the VM, refuses when its status already tells the request would not move
// it, then PUTs the subresource. grace is the stop's grace period (nil for the bodyless
// verbs). It returns the migration's name for migrate, "" otherwise.
func runVMAction(ctx context.Context, k *kubeClient, namespace, name, action string, grace *int) (string, error) {
	vmVersion, subVersion, err := virtVersions(ctx, k)
	if err != nil {
		return "", err
	}

	var vm vmObject
	if err := k.get(ctx, vmPath(groupKubeVirt, vmVersion, namespace, "virtualmachines", name), &vm); err != nil {
		return "", err
	}

	var migrating bool

	var started []vmimObject
	if action == vmActionMigrate {
		before, err := readVMIMMigrations(ctx, k, vmVersion, namespace)
		if err != nil {
			return "", err
		}

		started, migrating = before, vmMigrationInFlight(before, namespace, name)
	}

	if err := vmRefusal(action, vm.Status.PrintableStatus, vmRunStrategy(vm), migrating, grace != nil); err != nil {
		return "", err
	}

	body := []byte("{}")
	if grace != nil {
		body = []byte(`{"gracePeriod":` + strconv.Itoa(*grace) + `}`)
	}

	// The migrate subresource answers with the VirtualMachineInstanceMigration it started.
	var answer struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}

	put := vmPath(groupKubeVirtSub, subVersion, namespace, vmActions[action].resource, name) + "/" + action
	if err := k.do(ctx, http.MethodPut, put, "application/json", body, &answer); err != nil {
		return "", err
	}

	if action != vmActionMigrate {
		return "", nil
	}

	if answer.Metadata.Name != "" {
		return answer.Metadata.Name, nil
	}

	// An older cluster that answers nothing: the migration the call created is the one the
	// list did not hold before. The request went through even if the follow-up list cannot be
	// read.
	migrations, err := readVMIMMigrations(ctx, k, vmVersion, namespace)
	if err != nil {
		return "", nil
	}

	return newMigrationName(started, migrations, namespace, name), nil
}

// vmRefusal is what refuses action on a VM in status with runStrategy, migrating telling
// whether a migration of its instance is in flight and forced whether a stop carries a grace
// period; nil when the request may go ahead.
func vmRefusal(action, status, runStrategy string, migrating, forced bool) error {
	switch action {
	case vmActionStart:
		if status == "Running" {
			return errVMRunning
		}

		if status == "Starting" || status == "Provisioning" {
			return errVMStarting
		}
	case vmActionStop:
		// A forced stop is what kills a VM stuck in Stopping.
		if status == "Stopped" || (status == "Stopping" && !forced) {
			return errVMStopped
		}
	case vmActionRestart:
		if runStrategy == "Halted" {
			return errVMHalted
		}

		if status != "Running" {
			return errVMNotRunning
		}
	case vmActionMigrate:
		if migrating {
			return errVMMigrating
		}

		if status != "Running" {
			return errVMNotRunning
		}
	case vmActionPause:
		// A VM being migrated is running: it may be paused.
		if status != "Running" && status != "Migrating" {
			return errVMNotRunning
		}
	case vmActionUnpause:
		if status != "Paused" {
			return errVMNotPaused
		}
	}

	return nil
}

// virtVersions is the served API version of KubeVirt's two groups (its virtual machines and
// their subresources), refusing when the cluster does not serve KubeVirt at all. Older
// clusters serve subresources.kubevirt.io/v1alpha3.
func virtVersions(ctx context.Context, k *kubeClient) (vm, sub string, err error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return "", "", err
	}

	vm, sub = groups[groupKubeVirt], groups[groupKubeVirtSub]
	if vm == "" || sub == "" {
		return "", "", errKubeVirtAbsent
	}

	return vm, sub, nil
}

// vmPath is the API path of the virtual machine (or instance) name in namespace, under
// group/version.
func vmPath(group, version, namespace, resource, name string) string {
	return scopedPath("/apis/"+group+"/"+version, namespace, resource) + "/" + url.PathEscape(name)
}

// readVMIMMigrations lists the VirtualMachineInstanceMigrations of namespace ("" for every
// one).
func readVMIMMigrations(ctx context.Context, k *kubeClient, version, namespace string) ([]vmimObject, error) {
	var list kubeList[vmimObject]
	if err := getList(ctx, k, scopedPath("/apis/"+groupKubeVirt+"/"+version, namespace, "virtualmachineinstancemigrations"), &list); err != nil {
		return nil, err
	}

	return list.Items, nil
}

// vmMigrationInFlight reports a migration of the VMI that has neither succeeded nor failed.
func vmMigrationInFlight(migrations []vmimObject, namespace, name string) bool {
	m, ok := newestMigrations(migrations)[namespace+"/"+name]
	if !ok {
		return false
	}

	return m.Status.Phase != "Succeeded" && m.Status.Phase != "Failed"
}

// newMigrationName is the newest migration of the VMI the after list holds and the before
// list did not: the one the call created, "" when none.
func newMigrationName(before, after []vmimObject, namespace, name string) string {
	known := make(map[string]bool, len(before))
	for _, m := range before {
		known[m.Metadata.Namespace+"/"+m.Metadata.Name] = true
	}

	var (
		newest vmimObject
		found  bool
	)

	for _, m := range after {
		if m.Metadata.Namespace != namespace || m.Spec.VMIName != name || known[m.Metadata.Namespace+"/"+m.Metadata.Name] {
			continue
		}

		if !found || unixMilli(m.Metadata.CreationTimestamp) > unixMilli(newest.Metadata.CreationTimestamp) {
			newest, found = m, true
		}
	}

	if !found {
		return ""
	}

	return newest.Metadata.Name
}
