package ichorgo

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/client"
)

// etcdMembershipStatusType is EtcdMembershipStatus (pkg/machinery/resources/etcd/
// membership_status.go at v1.15.0-beta.1): one per control-plane node, id "etcd", where the node
// stands in joining etcd. Its spec is {phase, memberID, error}; phase is the text of
// MembershipPhase (membership_phase.go): preparing, joining, learner, member, leaving, left. Not
// in machinery v1.14: read raw by type string and decoded from the node's YAML.
const etcdMembershipStatusType = "EtcdMembershipStatuses.etcd.talos.dev"

// etcdNamespace is the etcd resources' namespace (etcd.NamespaceName).
const etcdNamespace = "etcd"

// etcdPhaseMember is the settled phase: a voting member. The apps show a chip for any other.
const etcdPhaseMember = "member"

// etcdMembershipErrorMax caps the error a node reports, in bytes.
const etcdMembershipErrorMax = 512

// etcdMembership is one node's EtcdMembershipStatus.
type etcdMembership struct {
	Phase    string
	MemberID string // hex as etcdNodeStatus.MemberID: Talos's zero padding dropped (normalizeMemberID)
	Error    string
}

// readEtcdMembership reads the EtcdMembershipStatus of each control-plane node that knows the
// type (Talos 1.15+), by node address. An older node, or one that does not answer, is left out:
// the etcd screen then shows what it showed before.
func readEtcdMembership(ctx context.Context, s *session, nodes []string) map[string]etcdMembership {
	found := make([]*etcdMembership, len(nodes))

	forEachNode(nodes, func(i int, node string) {
		if known, err := s.hasResourceType(ctx, node, etcdMembershipStatusType); err != nil || !known {
			return
		}

		specs, err := listRawSpecs(client.WithNode(ctx, node), s, etcdNamespace, etcdMembershipStatusType)
		if err != nil || len(specs) == 0 {
			return
		}

		m := mapEtcdMembership(specs[0])
		found[i] = &m
	})

	out := map[string]etcdMembership{}

	for i, m := range found {
		if m != nil {
			out[nodes[i]] = *m
		}
	}

	return out
}

func mapEtcdMembership(spec map[string]any) etcdMembership {
	return etcdMembership{
		Phase:    strings.ToLower(cleanLLDPText(specString(spec, "phase"))),
		MemberID: normalizeMemberID(specString(spec, "memberID")),
		Error:    clipUTF8(strings.Join(strings.Fields(specString(spec, "error")), " "), etcdMembershipErrorMax),
	}
}

// withEtcdMembership returns overview with each node status carrying its node's membership phase
// and error. A node etcd does not know yet (no member id) takes the id the membership names.
func withEtcdMembership(overview etcdOverview, byNode map[string]etcdMembership) etcdOverview {
	if len(byNode) == 0 {
		return overview
	}

	statuses := make([]etcdNodeStatus, 0, len(overview.Statuses))

	for _, st := range overview.Statuses {
		if m, ok := byNode[st.Node]; ok {
			st.MembershipPhase = m.Phase
			st.MembershipError = m.Error

			if st.MemberID == "" {
				st.MemberID = m.MemberID
			}
		}

		statuses = append(statuses, st)
	}

	overview.Statuses = statuses

	return overview
}

// membershipDetail describes the nodes not settled as members, for the replacement wait
// ("10.0.0.4: learner"), sorted by node; empty when every node that answered is a member.
func membershipDetail(byNode map[string]etcdMembership) string {
	parts := []string{}

	for node, m := range byNode {
		if m.Phase == etcdPhaseMember || m.Phase == "" {
			continue
		}

		part := fmt.Sprintf("%s: %s", node, m.Phase)
		if m.Error != "" {
			part += " (" + m.Error + ")"
		}

		parts = append(parts, part)
	}

	slices.Sort(parts)

	return strings.Join(parts, "; ")
}

// withMembershipDetail appends the unsettled nodes' phases to the replacement wait's detail.
func withMembershipDetail(detail string, byNode map[string]etcdMembership) string {
	phases := membershipDetail(byNode)
	if phases == "" {
		return detail
	}

	if detail == "" {
		return "etcd membership: " + phases
	}

	return detail + "; etcd membership: " + phases
}

// statusNodes lists the nodes of the overview's statuses.
func statusNodes(statuses []etcdNodeStatus) []string {
	out := make([]string, 0, len(statuses))

	for _, st := range statuses {
		out = append(out, st.Node)
	}

	return out
}
