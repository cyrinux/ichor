package ichorgo

import "slices"

// demoPolicies is what the demo cluster answers: both engines healthy, four Kyverno policies,
// two Gatekeeper constraints and a namespace's worth of findings. Everything invented; nothing
// readable as a real cluster.
func demoPolicies() policiesReport {
	report := policiesReport{
		Installed: true, Kyverno: true, Gatekeeper: true,
		Version: "v1",
		Engines: []policyEngine{{
			Name: policyEngineKyverno, Namespace: "kyverno",
			Controllers: []policyController{
				{Name: "admission-controller", Ready: 1, Desired: 1},
				{Name: "background-controller", Ready: 1, Desired: 1},
				{Name: "cleanup-controller", Ready: 1, Desired: 1},
				{Name: "reports-controller", Ready: 1, Desired: 1},
			},
			Webhook: policyWebhook{Name: "kyverno-resource-validating-webhook-cfg", Present: true, FailurePolicy: "Fail"},
		}, {
			Name: policyEngineGatekeeper, Namespace: gatekeeperNamespace,
			Controllers: []policyController{
				{Name: "gatekeeper-audit", Ready: 1, Desired: 1},
				{Name: "gatekeeper-controller-manager", Ready: 3, Desired: 3},
			},
			Webhook: policyWebhook{Name: gatekeeperWebhook, Present: true, FailurePolicy: "Ignore"},
		}},
		Policies: []policyRow{
			{
				Namespace: "", Name: "verify-image-signatures", Action: policyActionEnforce,
				Ready: false, ReadyReason: "WebhookNotConfigured", ReadyMessage: "the validating webhook configuration is missing: policies are not applied",
				Category: "Software Supply Chain", Severity: "high",
				Description: "Signatures on new images must be verified before a Pod is admitted",
				Rules:       policyRules{Validate: 1, VerifyImages: 1}, RuleNames: []string{"verify-signature"},
			},
			{
				Namespace: "", Name: "require-labels", Action: policyActionAudit,
				Ready: true, Category: "Best Practices", Severity: "medium",
				Description: "Every workload must carry its team and owner labels",
				Rules:       policyRules{Validate: 2}, RuleNames: []string{"require-team", "require-owner"},
				Fail:        7,
				ByNamespace: []policyNSRow{{Namespace: "shop", Fail: 7}},
				Failing: []policyFailure{
					{Kind: "Pod", Namespace: "shop", Name: "web-0", Rule: "require-team", Message: "Validation error: missing label team", Severity: "medium", At: 1760000002},
					{Kind: "Pod", Namespace: "shop", Name: "web-1", Rule: "require-team", Message: "Validation error: missing label team", Severity: "medium", At: 1760000001},
					{Kind: "Pod", Namespace: "shop", Name: "api-0", Rule: "require-owner", Message: "Validation error: missing label owner", Severity: "medium", At: 1760000000},
				},
			},
			{
				Namespace: "shop", Name: "restrict-image-registries", Action: policyActionMixed,
				Background: true, Ready: true, Category: "Best Practices", Severity: "medium",
				Description: "Images must come from the registry of this cluster",
				Rules:       policyRules{Validate: 3}, RuleNames: []string{"check-registry", "check-latest", "check-digest"},
				Pass: 3, Warn: 2,
				ByNamespace: []policyNSRow{{Namespace: "shop", Pass: 3, Warn: 2}},
			},
			{
				Namespace: "", Name: "disallow-privileged", Action: policyActionEnforce,
				Ready: true, Category: "Pod Security", Severity: "high",
				Description: "Privileged containers are not allowed",
				Rules:       policyRules{Validate: 1}, RuleNames: []string{"privileged-containers"},
				Pass:        15,
				ByNamespace: []policyNSRow{{Namespace: "billing", Pass: 9}, {Namespace: "media", Pass: 3}, {Namespace: "shop", Pass: 1}, {Namespace: "vault", Pass: 2}},
			},
		},
		Namespaces: []policyNSRow{
			{Namespace: "shop", Pass: 4, Fail: 9, Warn: 2},
			{Namespace: "billing", Pass: 9, Fail: 1},
			{Namespace: "media", Pass: 3},
			{Namespace: "vault", Pass: 2},
		},
		ReportsRead: 6,
	}

	for i := range report.Policies {
		report.Policies[i].Engine = policyEngineKyverno
	}

	// Gatekeeper's audit wrote these a minute before the demo's latest Kyverno result.
	const auditAt = 1760000060

	report.Policies = append(report.Policies,
		policyRow{
			Name: "must-have-team", Engine: policyEngineGatekeeper, Kind: "K8sRequiredLabels", Action: policyActionEnforce,
			Background: true, Ready: true, Description: "Requires resources to contain specified labels",
			RuleNames: []string{}, Fail: 3, AuditAt: auditAt,
			ByNamespace: []policyNSRow{{Namespace: "shop", Fail: 2}},
			Failing: []policyFailure{
				{Kind: "Namespace", Name: "sandbox", Message: `you must provide labels: {"team"}`, At: auditAt},
				{Kind: "Deployment", Namespace: "shop", Name: "cart", Message: `you must provide labels: {"team"}`, At: auditAt},
				{Kind: "Deployment", Namespace: "shop", Name: "search", Message: `you must provide labels: {"team"}`, At: auditAt},
			},
		},
		policyRow{
			Name: "psp-privileged-container", Engine: policyEngineGatekeeper, Kind: "K8sPSPPrivilegedContainer", Action: policyActionAudit,
			Background: true, Ready: true, Description: "Controls the ability of any container to enable privileged mode",
			RuleNames: []string{}, Fail: 1, AuditAt: auditAt,
			ByNamespace: []policyNSRow{{Namespace: "billing", Fail: 1}},
			Failing: []policyFailure{
				{Kind: "Pod", Namespace: "billing", Name: "ledger-backup-29318400-x7k2p", Message: "Privileged container is not allowed: backup", At: auditAt},
			},
		},
	)
	// In the order a live read gives: not ready first, then Enforce, Mixed, Audit.
	slices.SortFunc(report.Policies, policyRowLess)

	return report
}
