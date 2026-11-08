package ichorgo

import "github.com/cyrinux/ichor/go/kube"

// kubeCondition is a status condition as Kubernetes objects and operators report it.
type kubeCondition = kube.Condition

// kubeConditions is an object's status.conditions.
type kubeConditions []kubeCondition

// get is the condition of type typ, zero (Status "") when absent.
func (c kubeConditions) get(typ string) kubeCondition { return kube.Conditions(c).Get(typ) }

// is tells whether the condition typ is True.
func (c kubeConditions) is(typ string) bool { return kube.Conditions(c).Is(typ) }

// conditionStatus is the status ("True", "False") of a condition type, "" when absent.
func conditionStatus(conds []kubeCondition, typ string) string {
	return kubeConditions(conds).get(typ).Status
}

// readyCondition is the Ready condition, empty when absent.
func readyCondition(conds []kubeCondition) kubeCondition {
	return kubeConditions(conds).get("Ready")
}
