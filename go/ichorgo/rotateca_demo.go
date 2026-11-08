package ichorgo

import "time"

// demoCARotationPlan is the plan of the demo cluster: every node ready, the rotation itself
// unavailable (nothing can change in the demo).
func demoCARotationPlan(yaml, name string) (string, error) {
	_, cfgCtx, err := resolveContext(yaml, name)
	if err != nil {
		return "", err
	}

	in := caRotationInput{context: name, endpoints: cfgCtx.Endpoints, discovered: true}

	for _, n := range demoNodes() {
		in.nodes = append(in.nodes, caRotationNode{Node: n.Node, Hostname: n.Hostname, Role: n.Role, Version: n.Version, Reachable: true})
	}

	issued := time.Date(2024, time.March, 12, 9, 0, 0, 0, time.UTC)
	in.ca = &caRotationCA{Subject: "O=talos", NotBefore: issued.Unix(), NotAfter: issued.AddDate(10, 0, 0).Unix(), Fingerprint: "3f9a1c7e2b8d4650"}

	plan := computeCARotationPlan(in)
	plan.Blockers = append(plan.Blockers, errDemoUnavailable.Error())
	plan.Ready = false

	return toJSON(plan)
}
