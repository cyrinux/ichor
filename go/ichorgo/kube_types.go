package ichorgo

// kubeCondition is a status condition as Kubernetes objects and operators report it.
type kubeCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason"`
	Message            string `json:"message"`
	LastTransitionTime string `json:"lastTransitionTime"`
}

// kubeConditions is an object's status.conditions.
type kubeConditions []kubeCondition

// get is the condition of type typ, zero (Status "") when absent.
func (c kubeConditions) get(typ string) kubeCondition {
	for _, x := range c {
		if x.Type == typ {
			return x
		}
	}

	return kubeCondition{}
}

// is tells whether the condition typ is True.
func (c kubeConditions) is(typ string) bool { return c.get(typ).Status == "True" }

// conditionStatus is the status ("True", "False") of a condition type, "" when absent.
func conditionStatus(conds []kubeCondition, typ string) string {
	return kubeConditions(conds).get(typ).Status
}

// readyCondition is the Ready condition, empty when absent.
func readyCondition(conds []kubeCondition) kubeCondition {
	return kubeConditions(conds).get("Ready")
}
