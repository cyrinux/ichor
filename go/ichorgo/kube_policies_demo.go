package ichorgo

import "slices"

// demoPolicies is what the demo cluster answers: a healthy engine, four policies and a
// namespace's worth of findings. Everything invented; nothing readable as a real cluster.
func demoPolicies() policiesReport {
	report := policiesReport{
		Installed: true,
		Version:   "v1",
		Engine: kyvernoEngine{
			Namespace: "kyverno",
			Controllers: []kyvernoController{
				{Name: "admission-controller", Ready: 1, Desired: 1},
				{Name: "background-controller", Ready: 1, Desired: 1},
				{Name: "cleanup-controller", Ready: 1, Desired: 1},
				{Name: "reports-controller", Ready: 1, Desired: 1},
			},
			Webhook: kyvernoWebhook{Present: true, FailurePolicy: "Fail"},
		},
		Policies: []kyvernoPolicy{
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
			{Namespace: "shop", Pass: 4, Fail: 7, Warn: 2},
			{Namespace: "billing", Pass: 9},
			{Namespace: "media", Pass: 3},
			{Namespace: "vault", Pass: 2},
		},
		ReportsRead: 6,
	}
	// In the order a live read gives: not ready first, then Enforce, Mixed, Audit.
	slices.SortFunc(report.Policies, kyvernoPolicyLess)

	return report
}
