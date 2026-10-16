package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// The findings of the tailscale section. Reason, Message: the row's condition, clipped to
// tsMessageLimit.
const (
	findTailscaleOperatorDown       = "tailscaleOperatorDown"       // Namespace, Name: the operator Deployment; Extra: "ready/desired"
	findTailscaleConnectorNotReady  = "tailscaleConnectorNotReady"  // Extra: its roles (tsRole*), comma-separated
	findTailscaleProxyGroupDown     = "tailscaleProxyGroupDown"     // Extra: its type; critical when no proxy runs, warning when some do
	findTailscaleProxyClassNotReady = "tailscaleProxyClassNotReady" // Count: the Connectors and ProxyGroups that name it
)

// A Connector's roles, as a finding's Extra names them.
const (
	tsRoleSubnetRouter = "subnet-router"
	tsRoleExitNode     = "exit-node"
	tsRoleAppConnector = "app-connector"
)

// tsMessageLimit bounds a condition message in a finding.
const tsMessageLimit = 300

// checkupTailscale finds an operator that is down or scaled to zero, the Connectors and
// ProxyGroups that are not ready, and the ProxyClasses in use that are not ready, through the
// Tailscale reader. A row still reconciling a changed spec is not a finding. Absent without
// the operator's API group.
func checkupTailscale(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	if in.groups[groupTailscale] == "" {
		return absentSection(checkTailscale)
	}

	r := readTailscaleWithin(ctx, k, in.groups)

	var err error
	if r.Error != "" {
		err = errors.New(r.Error)
	}

	findings := []checkupFinding{}
	checked := len(r.Connectors) + len(r.ProxyGroups) + len(r.ProxyClasses)

	if op := r.Operator; op != nil {
		checked++

		if op.Health != tsHealthOK {
			findings = append(findings, checkupFinding{
				Kind: findTailscaleOperatorDown, Severity: tsSeverity(op.Health), Namespace: op.Namespace, Name: op.Name,
				Extra: fmt.Sprintf("%d/%d", op.Ready, op.Desired),
			})
		}
	}

	for _, c := range r.Connectors {
		if tsStuck(c.tsState) {
			findings = append(findings, tsFinding(findTailscaleConnectorNotReady, c.Name, strings.Join(tsConnectorRoles(c), ", "), c.tsState))
		}
	}

	for _, g := range r.ProxyGroups {
		if tsStuck(g.tsState) {
			findings = append(findings, tsFinding(findTailscaleProxyGroupDown, g.Name, g.Type, g.tsState))
		}
	}

	for _, c := range r.ProxyClasses {
		if tsStuck(c.tsState) && c.UsedBy > 0 {
			f := tsFinding(findTailscaleProxyClassNotReady, c.Name, "", c.tsState)
			f.Severity, f.Count = sevWarning, c.UsedBy
			findings = append(findings, f)
		}
	}

	return newSection(checkTailscale, checked, findings, err)
}

// tsStuck is a row that is not ready for a reason of its own: one still catching up with a
// changed spec (Reconciling) is passing, not stuck.
func tsStuck(s tsState) bool {
	return s.Health != tsHealthOK && s.Reason != tsReasonReconciling
}

func tsFinding(kind, name, extra string, s tsState) checkupFinding {
	return checkupFinding{
		Kind: kind, Severity: tsSeverity(s.Health), Name: name, Extra: extra,
		Reason: s.Reason, Message: clipUTF8(s.Message, tsMessageLimit),
	}
}

func tsSeverity(health string) string {
	if health == tsHealthCritical {
		return sevCritical
	}

	return sevWarning
}

// tsConnectorRoles is what a Connector does: it routes subnets, is an exit node, or is an
// app connector (a Connector may be the first two at once).
func tsConnectorRoles(c tsConnector) []string {
	roles := []string{}
	if len(c.AdvertisedRoutes) > 0 || c.SubnetRoutes != "" {
		roles = append(roles, tsRoleSubnetRouter)
	}
	if c.ExitNode {
		roles = append(roles, tsRoleExitNode)
	}
	if c.AppConnector {
		roles = append(roles, tsRoleAppConnector)
	}

	return roles
}
