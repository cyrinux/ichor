package ichorgo

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"strings"
	"sync"
)

// kyvernoPolicyObject is one ClusterPolicy (cluster-wide) or Policy (namespaced).
type kyvernoPolicyObject struct {
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		// ValidationFailureAction is the policy-level action, deprecated in Kyverno 1.13 in
		// favour of the rule-level failureAction.
		ValidationFailureAction string `json:"validationFailureAction"`
		Background              *bool  `json:"background"`
		Rules                   []struct {
			Name string `json:"name"`
			// Validate is present on a validate rule, and carries its own failureAction since
			// 1.13; the other blocks only tell the rule's kind.
			Validate *struct {
				FailureAction string `json:"failureAction"`
			} `json:"validate"`
			Mutate       *struct{} `json:"mutate"`
			Generate     *struct{} `json:"generate"`
			VerifyImages *struct{} `json:"verifyImages"`
		} `json:"rules"`
	} `json:"spec"`
	Status struct {
		// Ready is the deprecated bool Kyverno wrote before the conditions.
		Ready      bool           `json:"ready"`
		Conditions kubeConditions `json:"conditions"`
	} `json:"status"`
}

// policyReportObject is a wgpolicyk8s.io/v1alpha2 PolicyReport (namespaced) or
// ClusterPolicyReport: the results of one resource (Kyverno 1.11+) or one namespace (older).
type policyReportObject struct {
	Metadata struct {
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Results []struct {
		Source    string `json:"source"` // "kyverno" for Kyverno's own, "" for older writes
		Policy    string `json:"policy"` // the policy name, "namespace/name" for a Policy
		Rule      string `json:"rule,omitempty"`
		Result    string `json:"result,omitempty"` // pass, fail, warn, error, skip
		Message   string `json:"message,omitempty"`
		Severity  string `json:"severity,omitempty"`
		Resources []struct {
			Kind      string `json:"kind,omitempty"`
			Namespace string `json:"namespace,omitempty"`
			Name      string `json:"name,omitempty"`
		} `json:"resources,omitempty"`
		Timestamp *struct {
			Seconds int64 `json:"seconds"`
		} `json:"timestamp,omitempty"`
	} `json:"results,omitempty"`
}

// kyvernoDeployObject is one of the engine's Deployments.
type kyvernoDeployObject struct {
	Metadata struct {
		Namespace string            `json:"namespace"`
		Name      string            `json:"name"`
		Labels    map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Replicas *int32 `json:"replicas"`
	} `json:"spec"`
	Status struct {
		ReadyReplicas int32 `json:"readyReplicas"`
	} `json:"status"`
}

// readPolicies reads the engines the cluster runs, Kyverno and Gatekeeper side by side, into
// one report. Every part fails soft: a 403 on one of them is an error field, not a failed
// call. Neither engine: installed false, no error.
func readPolicies(ctx context.Context, k *kubeClient) (policiesReport, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return policiesReport{}, err
	}

	var (
		report policiesReport
		gk     gatekeeperRead
		wg     sync.WaitGroup
	)

	kyvernoVersion, templatesVersion := groups[groupKyverno], groups[groupGatekeeperTemplates]

	if kyvernoVersion != "" {
		wg.Go(func() { report = readKyverno(ctx, k, groups, kyvernoVersion) })
	}

	if templatesVersion != "" {
		wg.Go(func() { gk = readGatekeeper(ctx, k, templatesVersion, groups[groupGatekeeperConstraints]) })
	}

	wg.Wait()

	if templatesVersion != "" {
		mergeGatekeeper(&report, gk)
	}

	slices.SortFunc(report.Policies, policyRowLess)
	slices.SortFunc(report.Namespaces, func(a, b policyNSRow) int { return strings.Compare(a.Namespace, b.Namespace) })

	return report, nil
}

// readKyverno lists the Kyverno policies, the reports and the engine, and aggregates the
// reports into the policy rows.
func readKyverno(ctx context.Context, k *kubeClient, groups map[string]string, version string) policiesReport {
	var (
		clusterPolicies kubeList[kyvernoPolicyObject]
		policies        kubeList[kyvernoPolicyObject]
		deployments     kubeList[kyvernoDeployObject]
		webhookList     kubeList[webhookConfig]
		reports         policyReportRead
		clusterReports  policyReportRead
		policyErr       error
		engineErr       error
		reportsErr      error
		wg              sync.WaitGroup
	)

	wg.Go(func() {
		policyErr = listKyvernoPolicies(ctx, k, version, &clusterPolicies, &policies)
		// The preferred version may not serve the policies at all (Kyverno 1.15: v2
		// preferred, ClusterPolicy and Policy under v1). A 404 says this group version
		// does not serve them: retry the storage version. Only a 404 falls back — a 403
		// is a permission problem, not a missing resource.
		if isNotFound(policyErr) && version != kyvernoPoliciesVersion {
			policyErr = listKyvernoPolicies(ctx, k, kyvernoPoliciesVersion, &clusterPolicies, &policies)
		}
	})
	wg.Go(func() {
		engineErr = firstErr(
			getList(ctx, k, "/apis/apps/v1/deployments?labelSelector="+url.QueryEscape("app.kubernetes.io/part-of=kyverno"), &deployments),
			getList(ctx, k, "/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations", &webhookList),
		)
	})
	if rv := groups[groupPolicyReport]; rv != "" {
		wg.Go(func() {
			reportsErr = firstErr(
				reports.read(ctx, k, "/apis/"+groupPolicyReport+"/"+rv+"/policyreports"),
				clusterReports.read(ctx, k, "/apis/"+groupPolicyReport+"/"+rv+"/clusterpolicyreports"),
			)
		})
	}

	wg.Wait()

	report := policiesReport{
		Installed:        true,
		Kyverno:          true,
		Version:          version,
		Engines:          []policyEngine{readKyvernoEngine(deployments.Items, webhookList.Items)},
		Policies:         readKyvernoPolicies(clusterPolicies.Items, policies.Items),
		ReportsRead:      reports.readCount + clusterReports.readCount,
		ReportsTruncated: reports.truncated || clusterReports.truncated,
	}
	report.Error = sectionError(policyErr, engineErr)
	report.ReportsError = sectionError(reportsErr)

	aggregatePolicyReports(report.Policies, reports.items, clusterReports.items, &report)

	return report
}

// firstErr returns the first error of the given ones, in order.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	return nil
}

// listKyvernoPolicies lists the ClusterPolicies and the namespaced Policies at one group
// version. The two lists share the group version: if it does not serve the policies, both
// 404 together.
func listKyvernoPolicies(ctx context.Context, k *kubeClient, version string, clusterPolicies, policies *kubeList[kyvernoPolicyObject]) error {
	return firstErr(
		getList(ctx, k, "/apis/"+groupKyverno+"/"+version+"/clusterpolicies", clusterPolicies),
		getList(ctx, k, "/apis/"+groupKyverno+"/"+version+"/policies", policies),
	)
}

// readKyvernoPolicies maps the two policy kinds into rows, ClusterPolicies first.
func readKyvernoPolicies(cluster, namespaced []kyvernoPolicyObject) []policyRow {
	out := make([]policyRow, 0, len(cluster)+len(namespaced))

	for i := range cluster {
		out = append(out, kyvernoPolicyOf(cluster[i]))
	}

	for i := range namespaced {
		out = append(out, kyvernoPolicyOf(namespaced[i]))
	}

	return out
}

// kyvernoPolicyOf maps one policy object into its row: the action with the rule-level
// failureAction overrides applied, the rule counts, and the annotations the screen shows.
func kyvernoPolicyOf(o kyvernoPolicyObject) policyRow {
	row := policyRow{
		Namespace:   o.Metadata.Namespace,
		Name:        o.Metadata.Name,
		Engine:      policyEngineKyverno,
		Background:  o.Spec.Background == nil || *o.Spec.Background,
		RuleNames:   []string{},
		Description: clipUTF8(o.Metadata.Annotations["policies.kyverno.io/description"], policyDescriptionMax),
		Category:    o.Metadata.Annotations["policies.kyverno.io/category"],
		Severity:    o.Metadata.Annotations["policies.kyverno.io/severity"],
	}

	actions := map[string]bool{}
	for _, r := range o.Spec.Rules {
		row.RuleNames = append(row.RuleNames, r.Name)
		switch {
		case r.Validate != nil:
			row.Rules.Validate++
			actions[ruleAction(o.Spec.ValidationFailureAction, r.Validate.FailureAction)] = true
		case r.Mutate != nil:
			row.Rules.Mutate++
		case r.Generate != nil:
			row.Rules.Generate++
		case r.VerifyImages != nil:
			row.Rules.VerifyImages++
		}
	}

	row.Action = policyActionOf(actions, o.Spec.ValidationFailureAction)

	cond := o.Status.Conditions.get("Ready")
	row.Ready = cond.Status == "True"
	if cond.Status == "" {
		row.Ready = o.Status.Ready // the deprecated bool, for older writes
	}
	row.ReadyReason, row.ReadyMessage = cond.Reason, cond.Message

	return row
}

// ruleAction is one validate rule's action: the rule-level failureAction, else the
// policy-level one (which is deprecated but still set on most policies).
func ruleAction(policyLevel, ruleLevel string) string {
	if ruleLevel != "" {
		return ruleLevel
	}

	if policyLevel != "" {
		return policyLevel
	}

	return policyActionAudit // Kyverno's default when neither level sets one
}

// policyActionOf folds the rules' actions into the row's: one kind, or Mixed.
func policyActionOf(actions map[string]bool, policyLevel string) string {
	switch len(actions) {
	case 0:
		return ruleAction(policyLevel, "")
	case 1:
		for a := range actions {
			return a
		}
	}

	return policyActionMixed
}

// readKyvernoEngine maps the engine's Deployments and its resource webhook. A Deployment
// whose replicas are unset wants one.
func readKyvernoEngine(deployments []kyvernoDeployObject, webhooks []webhookConfig) policyEngine {
	out := policyEngine{Name: policyEngineKyverno, Namespace: "kyverno"}

	if len(deployments) > 0 {
		sorted := slices.Clone(deployments)
		slices.SortFunc(sorted, func(a, b kyvernoDeployObject) int { return strings.Compare(a.Metadata.Name, b.Metadata.Name) })
		out.Namespace = sorted[0].Metadata.Namespace
	}

	out.Controllers = []policyController{}
	for _, d := range deployments {
		name := d.Metadata.Labels["app.kubernetes.io/component"]
		if name == "" {
			name = d.Metadata.Name
		}

		desired := 1
		if d.Spec.Replicas != nil {
			desired = int(*d.Spec.Replicas)
		}

		out.Controllers = append(out.Controllers, policyController{Name: name, Ready: int(d.Status.ReadyReplicas), Desired: desired})
	}

	for _, c := range webhooks {
		if !isKyvernoResourceWebhook(c.Metadata.Name) {
			continue
		}

		out.Webhook.Present, out.Webhook.Name = true, c.Metadata.Name
		for _, w := range c.Webhooks {
			// Fail is the default; any non-Ignore webhook refuses the requests when the engine is down.
			if w.FailurePolicy != "Ignore" {
				out.Webhook.FailurePolicy = "Fail"
			}

			if w.ClientConfig.Service != nil && out.Webhook.Service == "" {
				out.Webhook.Service = w.ClientConfig.Service.Namespace + "/" + w.ClientConfig.Service.Name
			}
		}

		if out.Webhook.FailurePolicy == "" {
			out.Webhook.FailurePolicy = "Ignore"
		}

		break
	}

	return out
}

// isKyvernoResourceWebhook tells whether the configuration is Kyverno's resource webhook.
func isKyvernoResourceWebhook(name string) bool {
	return name == "kyverno-resource-validating-webhook-cfg" || strings.HasPrefix(name, "kyverno-resource-validating")
}

// policyReportRead walks one report listing, bounded at policyReportsMax objects.
type policyReportRead struct {
	items     []policyReportObject
	readCount int
	truncated bool
}

// read pages the reports at path in, keeping at most policyReportsMax of them; the ones
// beyond only say the read was truncated.
func (r *policyReportRead) read(ctx context.Context, k *kubeClient, path string) error {
	return k.listAll(ctx, path, pageQuery{}, func() { r.items = r.items[:0]; r.readCount = 0; r.truncated = false },
		func(page kubePage) error {
			objs, err := decodeItems[policyReportObject](page)
			if err != nil {
				return err
			}

			for _, o := range objs {
				if r.readCount >= policyReportsMax {
					r.truncated = true

					continue
				}

				r.readCount++
				r.items = append(r.items, o)
			}

			return nil
		})
}

// aggregatePolicyReports folds the report results into the policy rows (per policy, the
// counts of every rule) and into the per-namespace totals. The rows keep the first
// policyFailingMax failing resources, newest first.
func aggregatePolicyReports(rows []policyRow, reports []policyReportObject, clusterReports []policyReportObject, out *policiesReport) {
	rowByKey := map[string]int{}
	for i := range rows {
		row := &rows[i]
		if row.Namespace == "" {
			rowByKey[row.Name] = i

			continue
		}

		rowByKey[row.Namespace+"/"+row.Name] = i
	}

	nsIndex := map[string]int{}
	rowNS := make([]map[string]*policyNSRow, len(rows))

	for _, r := range append(slices.Clone(reports), clusterReports...) {
		for _, res := range r.Results {
			// Only Kyverno's own results; another writer's stay out of the counts.
			if res.Source != "" && res.Source != "kyverno" {
				continue
			}

			i, ok := rowByKey[res.Policy]
			if !ok && strings.Contains(res.Policy, "/") {
				_, name, _ := strings.Cut(res.Policy, "/")
				i, ok = rowByKey[name] // a namespaced policy written without its namespace
			}
			if !ok {
				continue
			}

			row := &rows[i]
			switch res.Result {
			case policyResultPass:
				row.Pass++
			case policyResultFail:
				row.Fail++
			case policyResultWarn:
				row.Warn++
			case policyResultError:
				row.ErrorCount++
			case policyResultSkip:
				row.Skip++
			}

			// Where it counts: the namespaced report's namespace or, in a ClusterPolicyReport, its
			// resource's ("" for a cluster-scoped one: the totals only).
			ns := r.Metadata.Namespace
			if ns == "" && len(res.Resources) > 0 {
				ns = res.Resources[0].Namespace
			}

			addPolicyNSResult(rowNS, i, ns, res.Result)

			if res.Result == policyResultFail || res.Result == policyResultError {
				f := policyFailure{Rule: res.Rule, Message: res.Message, Severity: res.Severity}
				if res.Timestamp != nil {
					f.At = res.Timestamp.Seconds
				}
				if len(res.Resources) > 0 {
					f.Kind, f.Namespace, f.Name = res.Resources[0].Kind, res.Resources[0].Namespace, res.Resources[0].Name
				}

				row.Failing = append(row.Failing, f)
			}
		}

		// Namespace totals come from the namespaced reports only: a ClusterPolicyReport's
		// results name resources in several namespaces, none of them the report's.
		if r.Metadata.Namespace != "" {
			ni, ok := nsIndex[r.Metadata.Namespace]
			if !ok {
				ni = len(out.Namespaces)
				nsIndex[r.Metadata.Namespace] = ni
				out.Namespaces = append(out.Namespaces, policyNSRow{Namespace: r.Metadata.Namespace})
			}

			for _, res := range r.Results {
				if res.Source != "" && res.Source != "kyverno" {
					continue
				}

				ns := &out.Namespaces[ni]
				switch res.Result {
				case policyResultPass:
					ns.Pass++
				case policyResultFail:
					ns.Fail++
				case policyResultWarn:
					ns.Warn++
				case policyResultError:
					ns.ErrorCount++
				}
			}
		}
	}

	for i := range rows {
		row := &rows[i]
		slices.SortFunc(row.Failing, func(a, b policyFailure) int { return cmp.Compare(b.At, a.At) }) // newest first
		row.Failing = clipPolicyFailings(row.Failing, policyFailingMax)
		row.ByNamespace = sortedPolicyNSRows(rowNS[i])
	}
}

// addPolicyNSResult counts one result of row i under namespace ns ("" counts nowhere).
func addPolicyNSResult(rowNS []map[string]*policyNSRow, i int, ns, result string) {
	if ns == "" {
		return
	}

	if rowNS[i] == nil {
		rowNS[i] = map[string]*policyNSRow{}
	}

	n := rowNS[i][ns]
	if n == nil {
		n = &policyNSRow{Namespace: ns}
		rowNS[i][ns] = n
	}

	switch result {
	case policyResultPass:
		n.Pass++
	case policyResultFail:
		n.Fail++
	case policyResultWarn:
		n.Warn++
	case policyResultError:
		n.ErrorCount++
	}
}

// sortedPolicyNSRows is one policy's namespace counts by namespace name, empty (not nil) without any.
func sortedPolicyNSRows(byNS map[string]*policyNSRow) []policyNSRow {
	out := make([]policyNSRow, 0, len(byNS))
	for _, n := range byNS {
		out = append(out, *n)
	}

	slices.SortFunc(out, func(a, b policyNSRow) int { return strings.Compare(a.Namespace, b.Namespace) })

	return out
}

// clipPolicyFailings keeps the first n failing resources.
func clipPolicyFailings(in []policyFailure, n int) []policyFailure {
	if len(in) <= n {
		return in
	}

	return in[:n]
}

// policyRowLess orders the rows: not ready first, then Enforce, Mixed, Warn and Audit, then
// the most failing first, then by name.
func policyRowLess(a, b policyRow) int {
	rank := func(p policyRow) int {
		switch p.Action {
		case policyActionEnforce:
			return 0
		case policyActionMixed:
			return 1
		case policyActionWarn:
			return 2
		default:
			return 3
		}
	}

	return cmp.Or(
		cmp.Compare(notReady(a.Ready), notReady(b.Ready)),
		cmp.Compare(rank(a), rank(b)),
		cmp.Compare(b.Fail, a.Fail),
		strings.Compare(a.Namespace+"/"+a.Name, b.Namespace+"/"+b.Name),
		// Gatekeeper names are unique per kind only.
		strings.Compare(a.Engine+"/"+a.Kind, b.Engine+"/"+b.Kind),
	)
}

// notReady orders false before true: not ready policies first.
func notReady(b bool) int {
	if b {
		return 1
	}

	return 0
}
