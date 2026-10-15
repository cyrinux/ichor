package ichorgo

import (
	"context"
	"errors"
	"slices"
	"time"
)

// The findings of the crossplane section. Name: "<Kind>/<name>". Messages come from cloud
// APIs: clipped to xpMessageLimit, never logged.
const (
	findCrossplaneSyncFailed               = "crossplaneSyncFailed"               // Extra: its provider config, or a composite's composition; Reason, Message: its Synced condition's
	findCrossplaneProviderUnhealthy        = "crossplaneProviderUnhealthy"        // Extra: the package; Reason, Message: its failing condition's (Healthy, else Installed)
	findCrossplaneNotReady                 = "crossplaneNotReady"                 // Count: minutes since Since; Reason, Message: its Ready condition's
	findCrossplaneDefinitionNotEstablished = "crossplaneDefinitionNotEstablished" // Reason, Message: its Established condition's
)

const (
	// xpMessageLimit bounds a condition message: cloud errors run long and may name accounts.
	xpMessageLimit = 300
	// xpCheckupPerKind is how many managed resources of a kind the checkup reads (four pages):
	// it runs at every background check, on every cluster.
	xpCheckupPerKind = 2000
)

// checkupCrossplane finds the managed resources and composites that cannot sync, the
// packages that are unhealthy, what stays not ready past the grace, and the definitions
// that are not established. It reads through the Crossplane reader, at most xpCheckupPerKind
// managed resources of a kind; a paused resource is not a finding (someone asked for it).
// Absent without Crossplane.
func checkupCrossplane(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	status, err := readCrossplaneWithin(ctx, k, in.groups, in.now, xpCheckupPerKind)
	if err == nil && !status.Installed {
		return absentSection(checkCrossplane)
	}

	if status.Error != "" {
		err = errors.New(status.Error)
	}

	findings := []checkupFinding{}

	for _, p := range status.Packages {
		if p.Health != healthCritical {
			continue
		}

		c := p.Healthy
		if c.Status != "False" {
			c = p.Installed
		}

		findings = append(findings, xpFinding(findCrossplaneProviderUnhealthy, sevCritical, p.xpObjectRef, p.Package, c))
	}

	for _, d := range status.Definitions {
		if slices.Contains(d.Reasons, xpReasonNotEstablished) {
			findings = append(findings, xpFinding(findCrossplaneDefinitionNotEstablished, sevWarning, d.xpObjectRef, "", d.Established))
		}
	}

	for _, x := range status.Composites {
		findings = append(findings, xpObjectFindings(x, x.Composition, in.now)...)
	}

	for _, x := range status.Managed {
		findings = append(findings, xpObjectFindings(x, x.ProviderConfig, in.now)...)
	}

	checked := len(status.Packages) + len(status.Definitions) + len(status.Composites) + status.ManagedTotal

	return newSection(checkCrossplane, checked, findings, err)
}

// xpObjectFindings is what a composite or managed resource says: it cannot sync, or it has
// not been ready for longer than the grace. A paused one says nothing.
func xpObjectFindings(x xpObject, extra string, now time.Time) []checkupFinding {
	if x.Paused {
		return nil
	}

	if slices.Contains(x.Reasons, xpReasonSyncFailed) {
		return []checkupFinding{xpFinding(findCrossplaneSyncFailed, sevCritical, x.xpObjectRef, extra, x.Synced)}
	}

	if !slices.Contains(x.Reasons, xpReasonNotReady) || slices.Contains(x.Reasons, xpReasonDeleting) {
		return nil
	}

	f := xpFinding(findCrossplaneNotReady, sevWarning, x.xpObjectRef, extra, x.Ready)
	if x.LastTransition > 0 {
		f.Since = x.LastTransition
		f.Count = int(now.Sub(time.UnixMilli(x.LastTransition)).Minutes())
	}

	return []checkupFinding{f}
}

func xpFinding(kind, severity string, ref xpObjectRef, extra string, c xpCondition) checkupFinding {
	return checkupFinding{
		Kind: kind, Severity: severity, Namespace: ref.Namespace, Name: ref.Kind + "/" + ref.Name,
		Extra: extra, Reason: c.Reason, Message: clipUTF8(c.Message, xpMessageLimit),
	}
}
