package ichorgo

import (
	"context"
	"fmt"
	"slices"
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

	return cordonKubeNode(kubeTarget{configYAML, contextName, kubeServer}, kubeNode, on)
}

// cordonKubeNode is the cordon (on) or uncordon of the Kubernetes node kubeNode, shared by the
// clusters with and without Talos (KubeCordon resolves the name first).
func cordonKubeNode(target kubeTarget, kubeNode string, on bool) error {
	return kubeMutate(target, func(ctx context.Context, k *kubeClient) error {
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
	node, err := readKubeNodeObject(ctx, k, kubeNode)
	if err != nil {
		return maintenancePlan{}, err
	}

	return drainPlanFor(ctx, k, kubeNode, maintenancePlan{Node: kubeNode, Hostname: kubeNode, Cordoned: node.Spec.Unschedulable})
}

// drainPlanFor completes base, a plan holding the node's identity and the checks of a reboot
// (none on a cluster without Talos), with what a drain of the Kubernetes node kubeNode does:
// the pods it evicts or leaves, and the warnings they deserve (a bare pod is not recreated, a
// budget that allows no disruption makes the drain wait). The Talos node maintenance and the
// kube drain build their plans here.
func drainPlanFor(ctx context.Context, k *kubeClient, kubeNode string, base maintenancePlan) (maintenancePlan, error) {
	plan := base
	plan.KubeNode = kubeNode
	plan.Blockers, plan.Warnings, plan.Acknowledge = orEmpty(plan.Blockers), orEmpty(plan.Warnings), orEmpty(plan.Acknowledge)

	pods, err := drainPods(ctx, k, kubeNode)
	if err != nil {
		return plan, err
	}

	plan.Pods = pods

	for _, p := range pods {
		switch {
		case p.Kind == drainBare:
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s/%s has no controller: once evicted it is not recreated", p.Namespace, p.Name))
		case p.Kind == drainEvict && p.PDB != "" && p.PDBAllowed == 0:
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("PodDisruptionBudget %s/%s allows no disruption now: the drain waits for it", p.Namespace, p.PDB))
		}
	}

	plan.Warnings = slices.Compact(plan.Warnings)

	return plan, nil
}

// demoKubeDrainPlan is the demo maintenance plan without its Talos checks: the drain's
// warnings stay, the reboot's blockers and acknowledgments go.
func demoKubeDrainPlan(kubeNode string) maintenancePlan {
	plan := demoMaintenancePlan(kubeNode)
	plan.Node, plan.Hostname, plan.KubeNode = kubeNode, kubeNode, kubeNode
	plan.Blockers, plan.Acknowledge = []string{}, []string{}

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

		listener.OnDone(errText(runKubeDrain(ctx, kubeTarget{configYAML, contextName, kubeServer}, kubeNode, includeBare, listener)))
	}()

	return &MaintenanceRun{cancel: cancel}
}

func runKubeDrain(ctx context.Context, kube kubeTarget, kubeNode string, includeBare bool, listener MaintenanceListener) error {
	if isDemoContext(kube.config, kube.context) {
		return errDemoUnavailable
	}

	emit := progressEmitter(listener)

	return kubeDo(ctx, kube, func(ctx context.Context, k *kubeClient) error {
		pods, err := cordonAndDrain(ctx, k, kubeNode, includeBare, emit)
		if err != nil {
			return err
		}

		emit(phaseDrain, kubeNode+" is drained and stays cordoned", pods)

		return nil
	})
}
