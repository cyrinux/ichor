package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"
)

// The monitoring section is what the cluster's Prometheus fails at: scraping and evaluating
// its rules. The checkup has no source the app chose, so it asks the first one discovery
// finds (PromDiscover), through the service proxy.

// The findings of the monitoring section.
const (
	// Namespace, Name: the Prometheus Service asked; Count: the targets down; Limit: all
	// active targets; Value: the percentage down; Extra: the scrape pool with the most down.
	// Critical above promTargetsDownCritical percent.
	findScrapeTargetsDown = "scrapeTargetsDown"
	// Namespace, Name: the PrometheusRule; Count: its rules failing to evaluate; Reason: the
	// first of them; Message: its error.
	findPromRuleErrors = "prometheusRuleErrors"
	// The same for a rule group of no PrometheusRule (a rule file of the configuration).
	// Name: the group; Extra: its file.
	findRuleGroupErrors = "ruleGroupErrors"
)

const promTargetsDownCritical = 25

// checkupMonitoring reads the targets and rules of the first Prometheus discovered, and
// the PrometheusRules to name the failing groups. Absent without a Prometheus; a source
// without one of the APIs (Mimir has no targets) leaves that part out.
func checkupMonitoring(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	found, err := discoverProm(ctx, k)
	if err != nil {
		return newSection(checkMonitoring, 0, nil, err)
	}

	if len(found.Sources) == 0 {
		return absentSection(checkMonitoring)
	}

	src := found.Sources[0]

	var (
		wg                 sync.WaitGroup
		targets            *promTargetsResult
		rules              *promRulesResult
		owners             []kubeRowMeta
		targetErr, ruleErr error
	)

	wg.Go(func() { targets, targetErr = checkupPromRead(ctx, k, src, promAPIPathTargets, parsePromTargets) })
	wg.Go(func() { rules, ruleErr = checkupPromRead(ctx, k, src, promAPIPathRules, parsePromRules) })

	if _, ok := in.groups[groupMonitoring]; ok {
		// Best effort: without them, the failing groups are named by their file.
		wg.Go(func() { owners, _ = listRuleOwners(ctx, k) }) //nolint:errcheck
	}

	wg.Wait()

	if rules != nil {
		rules.setOwners(owners)
	}

	checked := 0
	if targets != nil {
		checked += targets.Total
	}

	if rules != nil {
		checked += rules.Counts.Rules
	}

	return newSection(checkMonitoring, checked, monitoringFindings(src, targets, rules), targetErr, ruleErr)
}

// checkupPromRead reads one API of the source with parse: nil without an error when the
// source does not serve it.
func checkupPromRead[T any](ctx context.Context, k *kubeClient, src promSource, apiPath string, parse func(json.RawMessage) (T, error)) (*T, error) {
	data, err := promAPIData(src, apiPath)(promSendWith(ctx, k, src, http.MethodGet, apiPath))

	var noAPI promNoAPIError

	switch {
	case errors.As(err, &noAPI):
		return nil, nil
	case err != nil:
		return nil, err
	}

	res, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src.label(), err)
	}

	return &res, nil
}

// monitoringFindings are the down targets (one finding for them all) and the failing rules
// (one finding a PrometheusRule, or a group of none), from what could be read (nil: not).
func monitoringFindings(src promSource, targets *promTargetsResult, rules *promRulesResult) []checkupFinding {
	findings := []checkupFinding{}

	if targets != nil && targets.Down > 0 {
		f := checkupFinding{
			Kind: findScrapeTargetsDown, Severity: sevWarning, Namespace: src.Namespace, Name: src.Service,
			Count: targets.Down, Limit: float64(targets.Total), Value: percentOf(float64(targets.Down), float64(targets.Total)),
		}

		if len(targets.Pools) > 0 {
			f.Extra = targets.Pools[0].Pool
		}

		if f.Value > promTargetsDownCritical {
			f.Severity = sevCritical
		}

		findings = append(findings, f)
	}

	if rules != nil {
		findings = append(findings, ruleErrorFindings(rules.Groups)...)
	}

	return findings
}

// ruleErrorFindings gathers the failing rules by PrometheusRule (its groups may be several),
// else by group.
func ruleErrorFindings(groups []promRuleGroup) []checkupFinding {
	var (
		findings []checkupFinding
		index    = map[string]int{}
	)

	for _, g := range groups {
		if g.Errors == 0 {
			continue
		}

		f := checkupFinding{Kind: findPromRuleErrors, Severity: sevWarning, Namespace: g.RuleNamespace, Name: g.RuleName}
		if g.RuleName == "" {
			f = checkupFinding{Kind: findRuleGroupErrors, Severity: sevWarning, Name: g.Name, Extra: g.File}
		}

		key := f.Kind + "\x00" + f.Namespace + "\x00" + f.Name + "\x00" + f.Extra

		i, ok := index[key]
		if !ok {
			i = len(findings)
			index[key] = i

			if r := firstFailingRule(g.Rules); r != nil {
				f.Reason, f.Message = r.Name, r.LastError
			}

			findings = append(findings, f)
		}

		findings[i].Count += g.Errors
	}

	slices.SortStableFunc(findings, func(a, b checkupFinding) int { return cmp.Compare(b.Count, a.Count) })

	return findings
}

func firstFailingRule(rules []promRule) *promRule {
	for i := range rules {
		if rules[i].Health == promHealthErr {
			return &rules[i]
		}
	}

	return nil
}
