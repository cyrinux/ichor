package ichorgo

import "fmt"

// demoCPReplacePlan is the replacement plan of a demo member as if its node had died: the
// member does not answer and its node is unreachable, the two others keep quorum.
func demoCPReplacePlan(yaml, name, memberID string) (string, error) {
	if _, _, err := resolveContext(yaml, name); err != nil {
		return "", err
	}

	in := cpReplaceInput{etcd: memberPlanInput{memberID: normalizeMemberID(memberID), leader: 0xa1}}

	for i, n := range demoNodes()[:3] {
		id := uint64(0xa1 + i)
		dead := hexID(id) == in.etcd.memberID

		in.etcd.members = append(in.etcd.members, memberState{id: id, hostname: n.Hostname, address: n.Node, healthy: !dead})

		if dead {
			in.node = n.Node
		}
	}

	if in.node == "" {
		return "", fmt.Errorf("etcd has no member %s in the demo cluster", in.etcd.memberID)
	}

	return toJSON(computeCPReplacePlan(in))
}
