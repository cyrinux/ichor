package ichorgo

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The checkup's Policies section: is the admission-policy engine healthy, is every policy
// ready, and which workload's pods does a policy refuse. Kyverno through its reader
// (readPolicies), Gatekeeper only through its denial events.

const (
	// findPolicyEngineDown is the engine unable to admit: a controller at 0 ready, or its
	// resource webhook with failurePolicy Fail and a Service nothing answers for. Name: the
	// controller or webhook, Extra: its namespace (the webhook's Service as ns/name),
	// Message: what happens to the workloads' requests.
	findPolicyEngineDown = "policyEngineDown"
	// findPolicyNotReady is a policy whose Ready condition is False. Name: the policy,
	// Reason and Message: the condition's, Severity: critical for Enforce, warning for Audit.
	findPolicyNotReady = "policyNotReady"
	// findPolicyBlocks is a workload whose pods a policy denies, read from its ReplicaSet's
	// FailedCreate events. Name: the workload (the ReplicaSet's owner when known), Extra: the
	// policy, Reason: the rule or constraint, Count: the events, Since: first seen.
	findPolicyBlocks = "policyBlocks"
	// findPolicyFailures is an Audit policy whose background scans keep failing: the same
	// workloads would be refused in Enforce mode. Name: the policy, Count: the fails,
	// Extra: the namespace with the most.
	findPolicyFailures = "policyFailures"
)

// policyOwnerLookups bounds how many ReplicaSets are read for their owner Deployment.
const policyOwnerLookups = 20

// checkupPolicies reads the Kyverno policies and engine (the Policies screen's reader) and
// the FailedCreate denial events. Absent when the cluster runs neither Kyverno nor
// Gatekeeper: a Gatekeeper-only cluster still gets its denial events.
func checkupPolicies(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	var (
		wg      sync.WaitGroup
		report  policiesReport
		readErr error
		events  []eventObject
		evErr   error
	)

	wg.Go(func() { report, readErr = readPolicies(ctx, k) })
	wg.Go(func() {
		events, evErr = listObjects[eventObject](ctx, k, "/api/v1/events?fieldSelector="+url.QueryEscape("type!=Normal,reason=FailedCreate"))
	})
	wg.Wait()

	evFindings := parsePolicyEventFindings(ctx, events, in.now, k)

	if !report.Installed && len(evFindings) == 0 {
		if readErr != nil {
			return newSection(checkPolicies, 0, nil, readErr)
		}

		return absentSection(checkPolicies)
	}

	findings := policyEngineFindings(ctx, k, report)
	findings = append(findings, policyNotReadyFindings(report)...)
	findings = append(findings, evFindings...)
	findings = append(findings, policyFailureFindings(report)...)

	return newSection(checkPolicies, len(report.Policies)+len(events), findings, readErr, evErr)
}

// policyEngineFindings says when the engine cannot admit: a controller at 0 ready, or the
// resource webhook refusing (failurePolicy Fail) with no ready endpoint behind it.
func policyEngineFindings(ctx context.Context, k *kubeClient, report policiesReport) []checkupFinding {
	var findings []checkupFinding

	for _, c := range report.Engine.Controllers {
		if c.Ready > 0 {
			continue
		}

		message := "policies are skipped"
		if report.Engine.Webhook.FailurePolicy != "Ignore" {
			message = "every create/update it matches is refused"
		}

		findings = append(findings, checkupFinding{
			Kind: findPolicyEngineDown, Severity: sevCritical,
			Name: c.Name, Extra: report.Engine.Namespace, Message: message,
		})
	}

	if w := report.Engine.Webhook; w.Present && w.FailurePolicy == "Fail" && w.Service != "" {
		ns, name, _ := strings.Cut(w.Service, "/")
		if ready, err := serviceHasReadyEndpoint(ctx, k, ns, name); err == nil && !ready {
			findings = append(findings, checkupFinding{
				Kind: findPolicyEngineDown, Severity: sevCritical,
				Name: "kyverno-resource-validating-webhook-cfg", Extra: w.Service,
				Message: "every create/update it matches is refused",
			})
		}
	}

	return findings
}

// policyNotReadyFindings is one finding per policy whose Ready condition is False, critical
// for an Enforce policy (it refuses until it is back), warning for an Audit one.
func policyNotReadyFindings(report policiesReport) []checkupFinding {
	var findings []checkupFinding

	for _, p := range report.Policies {
		if p.Ready {
			continue
		}

		severity := sevWarning
		if p.Action != policyActionAudit {
			severity = sevCritical
		}

		findings = append(findings, checkupFinding{
			Kind: findPolicyNotReady, Severity: severity,
			Namespace: p.Namespace, Name: p.Name,
			Reason: p.ReadyReason, Message: p.ReadyMessage,
		})
	}

	return findings
}

// policyBlockKey groups the denial events by workload and policy.
type policyBlockKey struct{ namespace, workload, policy string }

// parsePolicyEventFindings turns the FailedCreate denial events into one finding per
// (workload, policy). The workload is the event object's owner Deployment when its
// ReplicaSet can be read (bounded), the ReplicaSet otherwise.
func parsePolicyEventFindings(ctx context.Context, events []eventObject, now time.Time, k *kubeClient) []checkupFinding {
	since := now.Add(-checkupEventWindow).UnixMilli()

	blocks := map[policyBlockKey]*checkupFinding{}
	order := []policyBlockKey{}
	owners := map[string]string{}

	for _, obj := range events {
		e := mapEvent(obj)
		if e.Last < since {
			continue
		}

		denials := parsePolicyDenials(e.Message)
		if len(denials) == 0 {
			continue
		}

		workload := e.Name
		if k != nil && e.Kind == "ReplicaSet" {
			key := e.Namespace + "/" + e.Name
			owner, resolved := owners[key]
			if !resolved && len(owners) < policyOwnerLookups {
				owner = replicaSetOwner(ctx, k, e.Namespace, e.Name)
				owners[key] = owner
				resolved = true
			}

			if resolved && owner != "" {
				workload = owner
			}
		}

		for _, d := range denials {
			key := policyBlockKey{e.Namespace, workload, d.Policy}
			f := blocks[key]
			if f == nil {
				f = &checkupFinding{
					Kind: findPolicyBlocks, Severity: sevCritical,
					Namespace: e.Namespace, Name: workload,
					Extra: d.Policy, Reason: d.Rule, Message: d.Message,
				}
				blocks[key] = f
				order = append(order, key)
			}

			f.Count += e.Count
			if e.First > 0 && (f.Since == 0 || e.First < f.Since) {
				f.Since = e.First
			}
		}
	}

	findings := make([]checkupFinding, 0, len(order))
	for _, key := range order {
		findings = append(findings, *blocks[key])
	}

	return findings
}

// replicaSetOwner reads one ReplicaSet's owner Deployment name, "" when it has none.
func replicaSetOwner(ctx context.Context, k *kubeClient, namespace, name string) string {
	var rs struct {
		Metadata struct {
			OwnerReferences []struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"ownerReferences"`
		} `json:"metadata"`
	}

	path := "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/replicasets/" + url.PathEscape(name)
	if err := k.get(ctx, path, &rs); err != nil {
		return ""
	}

	for _, o := range rs.Metadata.OwnerReferences {
		if o.Kind == "Deployment" {
			return o.Name
		}
	}

	return ""
}

// policyFailureFindings is one finding per Audit policy whose background scans keep
// failing: in Enforce mode those workloads would be refused.
func policyFailureFindings(report policiesReport) []checkupFinding {
	var findings []checkupFinding

	for _, p := range report.Policies {
		if p.Action != policyActionAudit || p.Fail == 0 {
			continue
		}

		ns, best := "", 0
		for _, byNS := range p.ByNamespace {
			if byNS.Fail > best {
				ns, best = byNS.Namespace, byNS.Fail
			}
		}

		findings = append(findings, checkupFinding{
			Kind: findPolicyFailures, Severity: sevWarning,
			Namespace: p.Namespace, Name: p.Name,
			Count: p.Fail, Extra: ns,
			Message: "would be refused in Enforce mode",
		})
	}

	return findings
}
