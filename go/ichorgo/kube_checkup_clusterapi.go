package ichorgo

import (
	"context"
	"errors"
	"time"
)

// The findings of the clusterapi section. Namespace, Name: the object's (servers are
// cluster-scoped). Messages are clipped to capiMessageLimit.
const (
	findCapiClusterFailed       = "capiClusterFailed"       // Reason, Message: its failure, else its first condition not True
	findCapiClusterProvisioning = "capiClusterProvisioning" // Since: its creation; Extra: its phase
	findCapiMachineFailed       = "capiMachineFailed"       // Extra: its cluster; Reason, Message: its failure
	findCapiMachineStuck        = "capiMachineStuck"        // Extra: "<cluster>/<phase>"; Since: its creation, or its deletion while Deleting
	findCapiWorkersShort        = "capiWorkersShort"        // Extra: its cluster; Value: available, Limit: desired
	findSideroServerOff         = "sideroServerOff"         // Extra: its hostname
	findSideroServerUnaccepted  = "sideroServerUnaccepted"  // Extra: its hostname; the summary row: Name "", Count: the servers not listed
)

const (
	// capiMessageLimit bounds a failure or condition message in a finding.
	capiMessageLimit = 300
	// capiProvisioningGrace is how long a workload cluster may stay Pending or Provisioning.
	capiProvisioningGrace = time.Hour
	// sideroUnacceptedShown is how many unaccepted servers get a finding each; the rest one more.
	sideroUnacceptedShown = 5
)

// checkupClusterAPI finds, on a management cluster, the workload clusters that failed or stay
// provisioning past capiProvisioningGrace, the Machines that failed or are stuck, the
// MachineDeployments short of available replicas (not while they scale), and the Sidero
// servers in use but powered off or not accepted, through the Cluster API reader. Absent
// without cluster.x-k8s.io and metal.sidero.dev.
func checkupClusterAPI(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	if in.groups[groupClusterAPI] == "" && in.groups[groupSidero] == "" {
		return absentSection(checkClusterAPI)
	}

	r := readClusterAPIWithin(ctx, k, in.groups, in.now)

	var err error
	if r.Error != "" {
		err = errors.New(r.Error)
	}

	findings := []checkupFinding{}
	findings = append(findings, capiClusterFindings(r.Clusters, in.now)...)
	findings = append(findings, capiMachineFindings(r.Machines)...)
	findings = append(findings, capiWorkersFindings(r.MachineDeployments)...)
	findings = append(findings, sideroServerFindings(r.Servers)...)

	checked := len(r.Clusters) + len(r.Machines) + len(r.MachineDeployments) + len(r.Servers)

	return newSection(checkClusterAPI, checked, findings, err)
}

func capiClusterFindings(clusters []capiCluster, now time.Time) []checkupFinding {
	out := []checkupFinding{}

	for _, c := range clusters {
		switch {
		case c.Phase == capiPhaseFailed || c.FailureReason != "":
			reason, message := capiFailureOf(c.FailureReason, c.FailureMessage, c.Conditions)
			out = append(out, checkupFinding{
				Kind: findCapiClusterFailed, Severity: sevCritical, Namespace: c.Namespace, Name: c.Name, Reason: reason, Message: message,
			})
		case (c.Phase == capiPhasePending || c.Phase == capiPhaseProvisioning) && c.CreatedAt > 0 &&
			now.UnixMilli()-c.CreatedAt > capiProvisioningGrace.Milliseconds():
			reason, message := capiFailureOf("", "", c.Conditions)
			out = append(out, checkupFinding{
				Kind: findCapiClusterProvisioning, Severity: sevWarning, Namespace: c.Namespace, Name: c.Name, Extra: c.Phase,
				Reason: reason, Message: message, Since: c.CreatedAt,
			})
		}
	}

	return out
}

func capiMachineFindings(machines []capiMachine) []checkupFinding {
	out := []checkupFinding{}

	for _, m := range machines {
		switch {
		case m.Phase == capiPhaseFailed || m.FailureReason != "":
			reason, message := capiFailureOf(m.FailureReason, m.FailureMessage, m.Conditions)
			out = append(out, checkupFinding{
				Kind: findCapiMachineFailed, Severity: sevCritical, Namespace: m.Namespace, Name: m.Name, Extra: m.Cluster,
				Reason: reason, Message: message,
			})
		case m.Stuck:
			since := m.CreatedAt
			if m.Phase == capiPhaseDeleting {
				since = m.DeletedAt
			}

			reason, message := capiFailureOf("", "", m.Conditions)
			out = append(out, checkupFinding{
				Kind: findCapiMachineStuck, Severity: sevWarning, Namespace: m.Namespace, Name: m.Name, Extra: m.Cluster + "/" + m.Phase,
				Reason: reason, Message: message, Since: since,
			})
		}
	}

	return out
}

// capiWorkersFindings is a MachineDeployment with fewer replicas available than desired; one
// still scaling is on its way and says nothing.
func capiWorkersFindings(deployments []capiMachineDeployment) []checkupFinding {
	out := []checkupFinding{}

	for _, d := range deployments {
		if d.Desired == 0 || d.Available >= d.Desired || d.Phase == "ScalingUp" || d.Phase == "ScalingDown" {
			continue
		}

		out = append(out, checkupFinding{
			Kind: findCapiWorkersShort, Severity: sevWarning, Namespace: d.Namespace, Name: d.Name, Extra: d.Cluster,
			Value: float64(d.Available), Limit: float64(d.Desired),
		})
	}

	return out
}

// sideroServerFindings is a server in use but powered off, and the servers waiting to be
// accepted: one finding each for the first sideroUnacceptedShown, then one for the rest.
func sideroServerFindings(servers []sideroServer) []checkupFinding {
	out := []checkupFinding{}
	unaccepted := 0

	for _, s := range servers {
		if s.InUse && s.Power == sideroPowerOff {
			out = append(out, checkupFinding{Kind: findSideroServerOff, Severity: sevCritical, Name: s.Name, Extra: s.Hostname})
		}

		if s.Accepted {
			continue
		}

		unaccepted++
		if unaccepted <= sideroUnacceptedShown {
			out = append(out, checkupFinding{Kind: findSideroServerUnaccepted, Severity: sevWarning, Name: s.Name, Extra: s.Hostname})
		}
	}

	if rest := unaccepted - sideroUnacceptedShown; rest > 0 {
		out = append(out, checkupFinding{Kind: findSideroServerUnaccepted, Severity: sevWarning, Count: rest})
	}

	return out
}

// capiFailureOf is a row's failure, else its first condition that is not True, clipped.
func capiFailureOf(reason, message string, conditions []capiCondition) (string, string) {
	if reason == "" && message == "" && len(conditions) > 0 {
		c := conditions[0]

		reason, message = c.Type, c.Message
		if c.Reason != "" {
			reason += ": " + c.Reason
		}
	}

	return reason, clipUTF8(message, capiMessageLimit)
}
