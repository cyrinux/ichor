package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

// Prometheus' own view of its alerting and recording rules (CYR-35), read from the metrics
// source the app set (prom_source.go): which fire, which are pending, which fail to
// evaluate, and the PrometheusRule each operator-managed group comes from.

const (
	// promMaxRules bounds the rules returned (kube-prometheus-stack: about 700); the counts
	// still cover them all.
	promMaxRules     = 3000
	promMaxRuleQuery = 2000
	promMaxRuleError = 500

	promRuleAlerting  = "alerting"
	promRuleRecording = "recording"

	promStateFiring   = "firing"
	promStatePending  = "pending"
	promStateInactive = "inactive"

	promHealthOK      = "ok"
	promHealthErr     = "err"
	promHealthUnknown = "unknown"

	// The Prometheus Operator writes each PrometheusRule to a file below a directory named
	// <prometheus|thanos-ruler>-<name>-rulefiles-<n>.
	promOperatorRuleDir = "-rulefiles-"
)

// promRulesResult is the app's view of GET /api/v1/rules.
type promRulesResult struct {
	Groups    []promRuleGroup `json:"groups"`
	Counts    promRuleCounts  `json:"counts"`
	Truncated bool            `json:"truncated"`
}

// promRuleCounts counts the rules (Firing, Pending: alerting rules in that state; Errors:
// rules whose last evaluation failed), over every group, truncated or not.
type promRuleCounts struct {
	Groups  int `json:"groups"`
	Rules   int `json:"rules"`
	Firing  int `json:"firing"`
	Pending int `json:"pending"`
	Errors  int `json:"errors"`
}

type promRuleGroup struct {
	Name string `json:"name"`
	File string `json:"file"`
	// RuleNamespace, RuleName: the PrometheusRule the group comes from, "" when it is not
	// from one or that could not be told.
	RuleNamespace  string  `json:"ruleNamespace"`
	RuleName       string  `json:"ruleName"`
	Interval       float64 `json:"interval"`       // seconds
	EvaluationTime float64 `json:"evaluationTime"` // seconds
	LastEvaluation int64   `json:"lastEvaluation"` // unix ms, 0 before the first
	// LastError is the first failing rule's error, "" when every rule evaluates.
	LastError string     `json:"lastError"`
	Firing    int        `json:"firing"`
	Pending   int        `json:"pending"`
	Errors    int        `json:"errors"`
	Rules     []promRule `json:"rules"`
}

type promRule struct {
	Name string `json:"name"`
	Type string `json:"type"` // alerting or recording
	// State is firing, pending or inactive (a recording rule is always inactive).
	State     string  `json:"state"`
	Health    string  `json:"health"` // ok, err or unknown
	LastError string  `json:"lastError"`
	Query     string  `json:"query"`
	Severity  string  `json:"severity"` // the severity label, as set
	Duration  float64 `json:"duration"` // for: seconds
	// Alerts counts an alerting rule's active (pending or firing) alerts.
	Alerts         int   `json:"alerts"`
	LastEvaluation int64 `json:"lastEvaluation"` // unix ms
}

type promRawRuleGroup struct {
	Name           string        `json:"name"`
	File           string        `json:"file"`
	Interval       float64       `json:"interval"`
	EvaluationTime float64       `json:"evaluationTime"`
	LastEvaluation string        `json:"lastEvaluation"`
	Rules          []promRawRule `json:"rules"`
}

type promRawRule struct {
	Name           string            `json:"name"`
	Query          string            `json:"query"`
	Type           string            `json:"type"`
	State          string            `json:"state"`
	Health         string            `json:"health"`
	LastError      string            `json:"lastError"`
	Duration       float64           `json:"duration"`
	Labels         map[string]string `json:"labels"`
	Alerts         []json.RawMessage `json:"alerts"`
	LastEvaluation string            `json:"lastEvaluation"`
}

// PromRules lists the rules of the metrics source sourceJSON (GET /api/v1/rules), as JSON
// {groups:[{name,file,ruleNamespace,ruleName,interval,evaluationTime (s),lastEvaluation
// (unix ms),lastError,firing,pending,errors,rules:[{name,type:"alerting"|"recording",
// state:"firing"|"pending"|"inactive",health:"ok"|"err"|"unknown",lastError,query,severity,
// duration (s),alerts,lastEvaluation}]}],counts:{groups,rules,firing,pending,errors},
// truncated}: the groups and rules in trouble first. ruleNamespace and ruleName name the
// PrometheusRule a group comes from, looked up through the Kubernetes API when the
// Prometheus Operator wrote the group's file. kubeServer is as for PromQueryRange.
func PromRules(configYAML, contextName, kubeServer, sourceJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	target := kubeTarget{configYAML, contextName, kubeServer}

	src, err := parsePromSource(sourceJSON)
	if err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return toJSON(demoPromRules(time.Now()))
	}

	data, err := promAPIData(src, promAPIPathRules)(promSend(target, src, http.MethodGet, promAPIPathRules, nil))
	if err != nil {
		return "", err
	}

	res, err := parsePromRules(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", src.label(), err)
	}

	if res.fromOperator() {
		// Best effort: without the PrometheusRules, the groups keep their file.
		if owners, err := withKube(target, listRuleOwners); err == nil {
			res.setOwners(owners)
		}
	}

	return toJSON(res)
}

const (
	promAPIPathRules   = "/api/v1/rules"
	promAPIPathTargets = "/api/v1/targets?state=active"
)

// promNoAPIError is a source answering 404 for an API: a server without it (Mimir has no
// targets) or a wrong path prefix.
type promNoAPIError struct{ label, path string }

func (e promNoAPIError) Error() string {
	p, _, _ := strings.Cut(e.path, "?")

	return fmt.Sprintf("%s: %s is not served here (check the path prefix)", e.label, p)
}

// promAPIData reads the answer to a GET of an API other than the query one, as promSend or
// promSendWith returns it: the data of a successful answer.
func promAPIData(src promSource, apiPath string) func(int, []byte, error) (json.RawMessage, error) {
	return func(status int, body []byte, err error) (json.RawMessage, error) {
		switch {
		case errors.Is(err, errPromNoAnswer):
			return nil, fmt.Errorf("%s: no answer within %s", src.label(), callTimeout)
		case err != nil:
			return nil, err
		case status == http.StatusNotFound:
			return nil, promNoAPIError{src.label(), apiPath}
		}

		data, err := promEnvelopeData(status, body)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", src.label(), err)
		}

		return data, nil
	}
}

// parsePromRules reads the data of a rules answer.
func parsePromRules(data json.RawMessage) (promRulesResult, error) {
	var raw struct {
		Groups []promRawRuleGroup `json:"groups"`
	}

	if err := json.Unmarshal(data, &raw); err != nil || raw.Groups == nil {
		return promRulesResult{}, errors.New("the rules answer has no groups")
	}

	res := promRulesResult{Groups: []promRuleGroup{}}

	for _, rg := range raw.Groups {
		g := promRuleGroup{
			Name: rg.Name, File: rg.File, Interval: rg.Interval, EvaluationTime: rg.EvaluationTime,
			LastEvaluation: promTime(rg.LastEvaluation), Rules: []promRule{},
		}

		for _, rr := range rg.Rules {
			r := promRuleOf(rr)
			g.count(r)

			if res.Counts.Rules < promMaxRules {
				g.Rules = append(g.Rules, r)
			} else {
				res.Truncated = true
			}

			res.Counts.Rules++
		}

		slices.SortStableFunc(g.Rules, func(a, b promRule) int { return a.rank() - b.rank() })
		res.Counts.Firing += g.Firing
		res.Counts.Pending += g.Pending
		res.Counts.Errors += g.Errors
		res.Groups = append(res.Groups, g)
	}

	res.Counts.Groups = len(res.Groups)
	slices.SortStableFunc(res.Groups, func(a, b promRuleGroup) int { return a.rank() - b.rank() })

	return res, nil
}

func promRuleOf(rr promRawRule) promRule {
	r := promRule{
		Name: rr.Name, Type: promRuleRecording, State: promStateInactive, Health: promHealthUnknown,
		LastError: clipUTF8(strings.TrimSpace(rr.LastError), promMaxRuleError), Query: clipUTF8(rr.Query, promMaxRuleQuery),
		Severity: rr.Labels["severity"], Duration: rr.Duration, LastEvaluation: promTime(rr.LastEvaluation),
	}

	if rr.Type == promRuleAlerting {
		r.Type, r.Alerts = promRuleAlerting, len(rr.Alerts)

		if s := strings.ToLower(rr.State); s == promStateFiring || s == promStatePending {
			r.State = s
		}
	}

	if h := strings.ToLower(rr.Health); h == promHealthOK || h == promHealthErr {
		r.Health = h
	}

	return r
}

func (g *promRuleGroup) count(r promRule) {
	switch r.State {
	case promStateFiring:
		g.Firing++
	case promStatePending:
		g.Pending++
	}

	if r.Health == promHealthErr {
		g.Errors++

		if g.LastError == "" {
			g.LastError = cmp.Or(r.LastError, "evaluation failed")
		}
	}
}

// rank puts failing rules first, then the firing and the pending ones.
func (r promRule) rank() int {
	switch {
	case r.Health == promHealthErr:
		return 0
	case r.State == promStateFiring:
		return 1
	case r.State == promStatePending:
		return 2
	default:
		return 3
	}
}

func (g promRuleGroup) rank() int {
	switch {
	case g.Errors > 0:
		return 0
	case g.Firing > 0:
		return 1
	case g.Pending > 0:
		return 2
	default:
		return 3
	}
}

// fromOperator tells whether a group's file was written by the Prometheus Operator.
func (res promRulesResult) fromOperator() bool {
	return slices.ContainsFunc(res.Groups, func(g promRuleGroup) bool { return strings.Contains(g.File, promOperatorRuleDir) })
}

// setOwners names the PrometheusRule of each group the operator wrote.
func (res *promRulesResult) setOwners(owners []kubeRowMeta) {
	for i, g := range res.Groups {
		if o, ok := ruleFileOwner(g.File, owners); ok {
			res.Groups[i].RuleNamespace, res.Groups[i].RuleName = o.Namespace, o.Name
		}
	}
}

// listRuleOwners lists the metadata of the PrometheusRules of every namespace.
func listRuleOwners(ctx context.Context, k *kubeClient) ([]kubeRowMeta, error) {
	return listMetas(ctx, k, promOperatorPath+"prometheusrules")
}

// ruleFileOwner finds the PrometheusRule the operator wrote file from: it names the file
// <namespace>-<name>-<uid>.yaml (older ones <namespace>-<name>.yaml), the dashes making the
// name alone ambiguous.
func ruleFileOwner(file string, owners []kubeRowMeta) (kubeRowMeta, bool) {
	if !strings.Contains(path.Dir(file), promOperatorRuleDir) {
		return kubeRowMeta{}, false
	}

	base := path.Base(file)
	base = strings.TrimSuffix(base, path.Ext(base))

	for _, o := range owners {
		stem := o.Namespace + "-" + o.Name
		if base == stem || o.UID != "" && base == stem+"-"+o.UID {
			return o, true
		}
	}

	return kubeRowMeta{}, false
}

// promTime is an RFC 3339 time of the API in unix ms, 0 when unset (Go's zero time).
func promTime(s string) int64 {
	return max(0, unixMilli(s))
}
