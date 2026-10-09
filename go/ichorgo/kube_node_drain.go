package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// Cordon and drain of a node by its Kubernetes name, for a cluster added from a kubeconfig
// (EKS, GKE, k3s…): no Talos API, so no reboot, no shutdown and no Talos checks. The Talos
// node maintenance (maintenance.go) runs the same cordon and drain.

// KubeNodeCordon cordons (on) or uncordons the Kubernetes node kubeNode, like `kubectl
// cordon`/`uncordon`. kubeServer: see KubePods.
func KubeNodeCordon(configYAML, contextName, kubeServer, kubeNode string, on bool) (err error) {
	defer maskErr(&err)

	contextName, kubeNode = unmaskTarget(configYAML, contextName, kubeNode)

	defer recordAction(&err, configYAML, contextName, auditAction{Action: auditVerb(on, "cordon", "uncordon"), Node: kubeNode})

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return setUnschedulable(ctx, k, kubeNode, on)
	})
}

// KubeDrainPlan reports what a drain of the Kubernetes node kubeNode would do, as a JSON
// maintenancePlan without Talos checks: whether it is cordoned, the pods it evicts or
// leaves, their PodDisruptionBudgets and emptyDir data. kubeServer: see KubePods.
func KubeDrainPlan(configYAML, contextName, kubeServer, kubeNode string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, kubeNode = unmaskTarget(configYAML, contextName, kubeNode)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() maintenancePlan { return demoKubeDrainPlan(kubeNode) },
		func(ctx context.Context, k *kubeClient) (maintenancePlan, error) {
			return gatherKubeDrainPlan(ctx, k, kubeNode)
		})
}

func gatherKubeDrainPlan(ctx context.Context, k *kubeClient, kubeNode string) (maintenancePlan, error) {
	var node struct {
		Spec struct {
			Unschedulable bool `json:"unschedulable"`
		} `json:"spec"`
	}

	if err := k.get(ctx, "/api/v1/nodes/"+url.PathEscape(kubeNode), &node); err != nil {
		return maintenancePlan{}, err
	}

	pods, err := drainPods(ctx, k, kubeNode)
	if err != nil {
		return maintenancePlan{}, err
	}

	return maintenancePlan{
		Node: kubeNode, Hostname: kubeNode, KubeNode: kubeNode, Cordoned: node.Spec.Unschedulable, Pods: pods,
		Blockers: []string{}, Warnings: []string{}, Acknowledge: []string{},
	}, nil
}

// demoKubeDrainPlan is the demo maintenance plan without its Talos checks.
func demoKubeDrainPlan(kubeNode string) maintenancePlan {
	plan := demoMaintenancePlan(kubeNode)
	plan.Hostname, plan.KubeNode = kubeNode, kubeNode
	plan.Blockers, plan.Warnings, plan.Acknowledge = []string{}, []string{}, []string{}

	return plan
}

// StartKubeDrain cordons the Kubernetes node kubeNode and drains it, like `kubectl drain`:
// evictions honour PodDisruptionBudgets, bare pods only with includeBare, DaemonSet and
// static pods stay. The node stays cordoned afterwards, and after a failure or Cancel.
// Reports to listener like StartNodeMaintenance (phases cordon and drain). kubeServer: see
// KubePods.
func StartKubeDrain(configYAML, contextName, kubeServer, kubeNode string, includeBare bool, listener MaintenanceListener) *MaintenanceRun {
	contextName, kubeNode = unmaskTarget(configYAML, contextName, kubeNode)

	listener = maskedMaintenanceListener{listener}

	// The drain's own bound, plus the cordon and the pod listing.
	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout+time.Minute)

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{Action: "drain", Node: kubeNode, Params: fmt.Sprintf("include-bare=%t", includeBare)}
		}, func() error {
			return runKubeDrain(ctx, kubeTarget{configYAML, contextName, kubeServer}, kubeNode, includeBare, listener)
		})

		listener.OnDone(errText(err))
	}()

	return &MaintenanceRun{cancel: cancel}
}

func runKubeDrain(ctx context.Context, kube kubeTarget, kubeNode string, includeBare bool, listener MaintenanceListener) error {
	if isDemoContext(kube.config, kube.context) {
		return errDemoUnavailable
	}

	emit := func(phase, message string, pods []drainPod) {
		emitJSON(maintenanceProgress{Phase: phase, Message: message, At: time.Now().UnixMilli(), Pods: pods}, listener.OnProgress)
	}

	return kubeDo(ctx, kube, func(ctx context.Context, k *kubeClient) error {
		pods, err := cordonAndDrain(ctx, k, kubeNode, includeBare, emit)
		if err != nil {
			return err
		}

		emit(phaseDrain, kubeNode+" is drained and stays cordoned", pods)

		return nil
	})
}
