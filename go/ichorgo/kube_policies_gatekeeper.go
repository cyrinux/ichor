package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Gatekeeper (OPA) next to Kyverno: each ConstraintTemplate defines a constraint kind, served
// under constraints.gatekeeper.sh once the template is created, and Gatekeeper's audit writes
// the violations it finds onto each constraint's status (no PolicyReports).

const (
	groupGatekeeperTemplates   = "templates.gatekeeper.sh"
	groupGatekeeperConstraints = "constraints.gatekeeper.sh"

	// gatekeeperNamespace is where the Helm chart installs it, when no Deployment says otherwise.
	gatekeeperNamespace = "gatekeeper-system"
	// gatekeeperWebhook is the chart's ValidatingWebhookConfiguration, and gatekeeperValidation
	// the webhook in it that checks the constraints (the other, check-ignore-label, only guards
	// the namespaces' ignore label and is Fail by default).
	gatekeeperWebhook    = "gatekeeper-validating-webhook-configuration"
	gatekeeperValidation = "validation.gatekeeper.sh"
	// gatekeeperListParallel bounds the constraint kinds listed at once.
	gatekeeperListParallel = 4
)

// gkTemplateObject is one ConstraintTemplate: the constraint kind it defines, whether
// Gatekeeper created that kind's CRD, and its library description.
type gkTemplateObject struct {
	Metadata struct {
		Name        string            `json:"name"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		CRD struct {
			Spec struct {
				Names struct {
					Kind string `json:"kind"`
				} `json:"names"`
			} `json:"spec"`
		} `json:"crd"`
	} `json:"spec"`
	Status struct {
		Created bool `json:"created"`
	} `json:"status"`
}

// gkConstraintObject is one constraint of a template's kind, with what the audit found.
type gkConstraintObject struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		EnforcementAction string `json:"enforcementAction"`
	} `json:"spec"`
	Status struct {
		AuditTimestamp  string `json:"auditTimestamp"`
		TotalViolations *int   `json:"totalViolations"`
		Violations      []struct {
			Kind              string `json:"kind"`
			Namespace         string `json:"namespace"`
			Name              string `json:"name"`
			Message           string `json:"message"`
			EnforcementAction string `json:"enforcementAction"`
		} `json:"violations"`
		ByPod []struct {
			ID       string `json:"id"`
			Enforced bool   `json:"enforced"`
			Errors   []struct {
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"byPod"`
	} `json:"status"`
}

// gatekeeperRead is what readGatekeeper found, merged into the report by mergeGatekeeper.
type gatekeeperRead struct {
	engine policyEngine
	rows   []policyRow
	err    string
}

// readGatekeeper lists the templates, then the constraints of every created kind, and the
// engine's Deployments and webhook. Each part fails soft into err.
func readGatekeeper(ctx context.Context, k *kubeClient, templatesVersion, constraintsVersion string) gatekeeperRead {
	var (
		templates   kubeList[gkTemplateObject]
		deployments kubeList[kyvernoDeployObject]
		webhookList kubeList[webhookConfig]
		templateErr error
		engineErr   error
		wg          sync.WaitGroup
	)

	wg.Go(func() {
		templateErr = getList(ctx, k, "/apis/"+groupGatekeeperTemplates+"/"+templatesVersion+"/constrainttemplates", &templates)
	})
	wg.Go(func() {
		engineErr = firstErr(
			getList(ctx, k, "/apis/apps/v1/deployments?labelSelector="+url.QueryEscape("gatekeeper.sh/operation"), &deployments),
			getList(ctx, k, "/apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations", &webhookList),
		)
	})
	wg.Wait()

	rows, constraintErr := readGatekeeperConstraints(ctx, k, constraintsVersion, templates.Items)

	return gatekeeperRead{
		engine: readGatekeeperEngine(deployments.Items, webhookList.Items),
		rows:   rows,
		err:    sectionError(templateErr, constraintErr, engineErr),
	}
}

// readGatekeeperConstraints lists the constraints of each created template's kind, a few kinds
// at a time. A kind that 404s (its template was just deleted) has no constraints left.
func readGatekeeperConstraints(ctx context.Context, k *kubeClient, version string, templates []gkTemplateObject) ([]policyRow, error) {
	if version == "" {
		return []policyRow{}, nil // no template created yet: the group is not served
	}

	var (
		mu      sync.Mutex
		rows    = []policyRow{}
		listErr error
		wg      sync.WaitGroup
		slots   = make(chan struct{}, gatekeeperListParallel)
	)

	for _, t := range templates {
		kind := t.Spec.CRD.Spec.Names.Kind
		if !t.Status.Created || kind == "" {
			continue
		}

		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()

			var list kubeList[gkConstraintObject]

			err := getList(ctx, k, "/apis/"+groupGatekeeperConstraints+"/"+version+"/"+strings.ToLower(kind), &list)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				if !isNotFound(err) && listErr == nil {
					listErr = err
				}

				return
			}

			for _, c := range list.Items {
				rows = append(rows, gatekeeperRowOf(kind, t.Metadata.Annotations["description"], c))
			}
		})
	}

	wg.Wait()

	return rows, listErr
}

// gatekeeperRowOf maps one constraint into a policy row: its action, the audit's violations
// (Gatekeeper keeps the first --constraint-violations-limit of them, 20 by default) and
// whether every Gatekeeper pod enforces it.
func gatekeeperRowOf(kind, description string, c gkConstraintObject) policyRow {
	row := policyRow{
		Name:        c.Metadata.Name,
		Engine:      policyEngineGatekeeper,
		Kind:        kind,
		Action:      gatekeeperAction(c.Spec.EnforcementAction),
		Background:  true, // the audit checks what already runs
		Ready:       true,
		Description: clipUTF8(description, policyDescriptionMax),
		RuleNames:   []string{},
		Failing:     []policyFailure{},
		ByNamespace: []policyNSRow{},
	}

	if at, err := time.Parse(time.RFC3339, c.Status.AuditTimestamp); err == nil {
		row.AuditAt = at.Unix()
	}

	byNS := map[string]*policyNSRow{}

	for _, v := range c.Status.Violations {
		row.Failing = append(row.Failing, policyFailure{Kind: v.Kind, Namespace: v.Namespace, Name: v.Name, Message: v.Message, At: row.AuditAt})

		if v.Namespace == "" {
			continue
		}

		if byNS[v.Namespace] == nil {
			byNS[v.Namespace] = &policyNSRow{Namespace: v.Namespace}
		}

		byNS[v.Namespace].Fail++
	}

	row.Fail = len(c.Status.Violations)
	if t := c.Status.TotalViolations; t != nil && *t > row.Fail {
		row.Fail, row.FailingCapped = *t, true
	}

	row.Failing = clipPolicyFailings(row.Failing, policyFailingMax)
	row.ByNamespace = sortedPolicyNSRows(byNS)

	// Ready once every Gatekeeper pod enforces it; no byPod yet is not a failure.
	var notEnforced int
	for _, p := range c.Status.ByPod {
		if p.Enforced {
			continue
		}

		notEnforced++

		if row.ReadyMessage == "" && len(p.Errors) > 0 {
			row.ReadyMessage = p.Errors[0].Message
		}
	}

	if notEnforced > 0 {
		row.Ready, row.ReadyReason = false, "NotEnforced"
		row.ReadyMessage = cmp.Or(row.ReadyMessage, fmt.Sprintf("%d of %d Gatekeeper pods do not enforce it", notEnforced, len(c.Status.ByPod)))
	}

	return row
}

// gatekeeperAction maps spec.enforcementAction: deny (the default) refuses, dryrun only
// audits, warn admits with a warning, scoped differs per enforcement point.
func gatekeeperAction(action string) string {
	switch strings.ToLower(action) {
	case "dryrun":
		return policyActionAudit
	case "warn":
		return policyActionWarn
	case "scoped":
		return policyActionMixed
	default:
		return policyActionEnforce
	}
}

// readGatekeeperEngine maps the controller-manager (the webhook) and audit Deployments, and
// the validating webhook's failurePolicy.
func readGatekeeperEngine(deployments []kyvernoDeployObject, webhooks []webhookConfig) policyEngine {
	out := policyEngine{Name: policyEngineGatekeeper, Namespace: gatekeeperNamespace, Controllers: []policyController{}}

	sorted := slices.Clone(deployments)
	slices.SortFunc(sorted, func(a, b kyvernoDeployObject) int { return strings.Compare(a.Metadata.Name, b.Metadata.Name) })

	for i, d := range sorted {
		if i == 0 {
			out.Namespace = d.Metadata.Namespace
		}

		desired := 1
		if d.Spec.Replicas != nil {
			desired = int(*d.Spec.Replicas)
		}

		out.Controllers = append(out.Controllers, policyController{Name: d.Metadata.Name, Ready: int(d.Status.ReadyReplicas), Desired: desired})
	}

	for _, c := range webhooks {
		if c.Metadata.Name != gatekeeperWebhook {
			continue
		}

		out.Webhook = policyWebhook{Name: c.Metadata.Name, Present: true}

		for _, w := range c.Webhooks {
			if w.Name != gatekeeperValidation {
				continue
			}

			// Gatekeeper's chart sets Ignore; Fail (or unset, the API default) refuses when it is down.
			out.Webhook.FailurePolicy = "Ignore"
			if w.FailurePolicy != "Ignore" {
				out.Webhook.FailurePolicy = "Fail"
			}

			if w.ClientConfig.Service != nil {
				out.Webhook.Service = w.ClientConfig.Service.Namespace + "/" + w.ClientConfig.Service.Name
			}
		}

		break
	}

	return out
}

// mergeGatekeeper adds Gatekeeper's engine, constraints and their namespace counts to the
// report (Kyverno's, or an empty one when only Gatekeeper runs).
func mergeGatekeeper(report *policiesReport, gk gatekeeperRead) {
	report.Installed, report.Gatekeeper = true, true
	report.GatekeeperError = gk.err
	report.Engines = append(report.Engines, gk.engine)
	report.Policies = append(report.Policies, gk.rows...)

	if report.Namespaces == nil {
		report.Namespaces = []policyNSRow{}
	}

	index := map[string]int{}
	for i, n := range report.Namespaces {
		index[n.Namespace] = i
	}

	for _, row := range gk.rows {
		for _, n := range row.ByNamespace {
			i, ok := index[n.Namespace]
			if !ok {
				i = len(report.Namespaces)
				index[n.Namespace] = i
				report.Namespaces = append(report.Namespaces, policyNSRow{Namespace: n.Namespace})
			}

			report.Namespaces[i].Fail += n.Fail
		}
	}
}
