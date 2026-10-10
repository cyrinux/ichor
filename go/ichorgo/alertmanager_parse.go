package ichorgo

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	// amMaxAlerts bounds the alerts returned: a storm of thousands is unreadable on a phone
	// anyway, the counts still cover them all.
	amMaxAlerts = 1000
	// amMaxExpired bounds the expired silences returned, newest first (Alertmanager keeps
	// them 5 days by default).
	amMaxExpired = 100

	amSeverityCritical = "critical"
	amSeverityWarning  = "warning"
	amSeverityInfo     = "info"
	amSeverityOther    = "other"

	amStateActive     = "active"
	amStateSuppressed = "suppressed"
	amStatePending    = "pending"
	amStateExpired    = "expired"
)

// amSeverityAliases folds the usual spellings of the severity label into four levels.
var amSeverityAliases = map[string]string{
	"critical": amSeverityCritical, "crit": amSeverityCritical, "page": amSeverityCritical, "emergency": amSeverityCritical, "fatal": amSeverityCritical,
	"warning": amSeverityWarning, "warn": amSeverityWarning, "error": amSeverityWarning, "high": amSeverityWarning, "major": amSeverityWarning,
	"info": amSeverityInfo, "informational": amSeverityInfo, "notice": amSeverityInfo, "low": amSeverityInfo, "minor": amSeverityInfo, "none": amSeverityInfo,
}

var amSeverityOrder = []string{amSeverityCritical, amSeverityWarning, amSeverityInfo, amSeverityOther}

func amSeverity(label string) string {
	if s, ok := amSeverityAliases[strings.ToLower(strings.TrimSpace(label))]; ok {
		return s
	}

	return amSeverityOther
}

// amAPIAlert is an alert as Alertmanager's GET /api/v2/alerts returns it.
type amAPIAlert struct {
	Fingerprint  string            `json:"fingerprint"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     string            `json:"startsAt"`
	EndsAt       string            `json:"endsAt"`
	UpdatedAt    string            `json:"updatedAt"`
	GeneratorURL string            `json:"generatorURL"`
	Receivers    []struct {
		Name string `json:"name"`
	} `json:"receivers"`
	Status struct {
		State       string   `json:"state"`
		SilencedBy  []string `json:"silencedBy"`
		InhibitedBy []string `json:"inhibitedBy"`
	} `json:"status"`
}

// amAlert is one alert for the app. Times are unix ms (0: unset). State is active,
// suppressed (silenced or inhibited) or unprocessed (just received).
type amAlert struct {
	Fingerprint  string            `json:"fingerprint"`
	Alertname    string            `json:"alertname"`
	Severity     string            `json:"severity"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	Summary      string            `json:"summary,omitempty"`
	Description  string            `json:"description,omitempty"`
	RunbookURL   string            `json:"runbookURL,omitempty"`
	GeneratorURL string            `json:"generatorURL,omitempty"`
	StartsAt     int64             `json:"startsAt"`
	EndsAt       int64             `json:"endsAt"`
	UpdatedAt    int64             `json:"updatedAt"`
	Receivers    []string          `json:"receivers"`
	State        string            `json:"state"`
	SilencedBy   []string          `json:"silencedBy"`
	InhibitedBy  []string          `json:"inhibitedBy"`
}

// amAlertGroup gathers the alerts of one alertname; Severity is the worst among them,
// Active counts those neither silenced nor inhibited.
type amAlertGroup struct {
	Alertname string    `json:"alertname"`
	Severity  string    `json:"severity"`
	Count     int       `json:"count"`
	Active    int       `json:"active"`
	Alerts    []amAlert `json:"alerts"`
}

// amCounts is the overview: the active alerts by severity, plus the suppressed ones.
type amCounts struct {
	Critical   int `json:"critical"`
	Warning    int `json:"warning"`
	Info       int `json:"info"`
	Other      int `json:"other"`
	Suppressed int `json:"suppressed"`
}

type amAlertsResult struct {
	Groups    []amAlertGroup `json:"groups"`
	Counts    amCounts       `json:"counts"`
	Total     int            `json:"total"`
	Truncated bool           `json:"truncated"`
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}

	return m
}

func (a amAPIAlert) toAlert() amAlert {
	receivers := make([]string, 0, len(a.Receivers))
	for _, r := range a.Receivers {
		receivers = append(receivers, r.Name)
	}

	labels, annotations := nonNilMap(a.Labels), nonNilMap(a.Annotations)

	return amAlert{
		Fingerprint:  a.Fingerprint,
		Alertname:    labels["alertname"],
		Severity:     amSeverity(labels["severity"]),
		Labels:       labels,
		Annotations:  annotations,
		Summary:      annotations["summary"],
		Description:  cmp.Or(annotations["description"], annotations["message"]),
		RunbookURL:   cmp.Or(annotations["runbook_url"], annotations["runbook"]),
		GeneratorURL: a.GeneratorURL,
		StartsAt:     unixMilli(a.StartsAt),
		EndsAt:       unixMilli(a.EndsAt),
		UpdatedAt:    unixMilli(a.UpdatedAt),
		Receivers:    receivers,
		State:        cmp.Or(a.Status.State, amStateActive),
		SilencedBy:   nonNil(a.Status.SilencedBy),
		InhibitedBy:  nonNil(a.Status.InhibitedBy),
	}
}

// parseAMAlerts reads GET /api/v2/alerts into groups by alertname (worst severity first,
// then the most active), the active alerts first in each, newest first.
func parseAMAlerts(body []byte) (amAlertsResult, error) {
	var raw []amAPIAlert
	if err := json.Unmarshal(body, &raw); err != nil {
		return amAlertsResult{}, fmt.Errorf("not an Alertmanager answer: %w", err)
	}

	alerts := make([]amAlert, 0, len(raw))
	for _, a := range raw {
		alerts = append(alerts, a.toAlert())
	}

	return groupAMAlerts(alerts), nil
}

func groupAMAlerts(alerts []amAlert) amAlertsResult {
	res := amAlertsResult{Groups: []amAlertGroup{}, Total: len(alerts)}

	for _, a := range alerts {
		res.Counts = res.Counts.add(a)
	}

	slices.SortStableFunc(alerts, func(a, b amAlert) int {
		suppressed := func(a amAlert) int {
			if amSuppressed(a) {
				return 1
			}

			return 0
		}

		return cmp.Or(cmp.Compare(suppressed(a), suppressed(b)), cmp.Compare(b.StartsAt, a.StartsAt), cmp.Compare(a.Fingerprint, b.Fingerprint))
	})

	if len(alerts) > amMaxAlerts {
		alerts, res.Truncated = alerts[:amMaxAlerts], true
	}

	index := map[string]int{}

	for _, a := range alerts {
		i, ok := index[a.Alertname]
		if !ok {
			i = len(res.Groups)
			index[a.Alertname] = i
			res.Groups = append(res.Groups, amAlertGroup{Alertname: a.Alertname, Severity: amSeverityOther})
		}

		g := res.Groups[i]
		g.Alerts = append(g.Alerts, a)
		g.Count++

		if !amSuppressed(a) {
			g.Active++
		}

		if amSeverityRank(a.Severity) < amSeverityRank(g.Severity) {
			g.Severity = a.Severity
		}

		res.Groups[i] = g
	}

	slices.SortStableFunc(res.Groups, func(a, b amAlertGroup) int {
		return cmp.Or(cmp.Compare(amSeverityRank(a.Severity), amSeverityRank(b.Severity)), cmp.Compare(b.Active, a.Active), cmp.Compare(b.Count, a.Count), cmp.Compare(a.Alertname, b.Alertname))
	})

	return res
}

func amSuppressed(a amAlert) bool { return a.State == amStateSuppressed }

func amSeverityRank(s string) int { return slices.Index(amSeverityOrder, s) }

func (c amCounts) add(a amAlert) amCounts {
	if amSuppressed(a) {
		c.Suppressed++

		return c
	}

	switch a.Severity {
	case amSeverityCritical:
		c.Critical++
	case amSeverityWarning:
		c.Warning++
	case amSeverityInfo:
		c.Info++
	default:
		c.Other++
	}

	return c
}

// amAPISilence is a silence as GET /api/v2/silences returns it.
type amAPISilence struct {
	ID        string      `json:"id"`
	Matchers  []amMatcher `json:"matchers"`
	StartsAt  string      `json:"startsAt"`
	EndsAt    string      `json:"endsAt"`
	UpdatedAt string      `json:"updatedAt"`
	CreatedBy string      `json:"createdBy"`
	Comment   string      `json:"comment"`
	Status    struct {
		State string `json:"state"`
	} `json:"status"`
}

// amSilence is one silence for the app; times are unix ms, State active, pending or expired.
type amSilence struct {
	ID        string      `json:"id"`
	State     string      `json:"state"`
	Matchers  []amMatcher `json:"matchers"`
	CreatedBy string      `json:"createdBy"`
	Comment   string      `json:"comment"`
	StartsAt  int64       `json:"startsAt"`
	EndsAt    int64       `json:"endsAt"`
	UpdatedAt int64       `json:"updatedAt"`
}

type amSilencesResult struct {
	Silences []amSilence `json:"silences"`
}

var amSilenceStateOrder = []string{amStateActive, amStatePending, amStateExpired}

// parseAMSilences reads GET /api/v2/silences: the active ones first (ending soonest
// first), then the pending (starting soonest first), then, withExpired, the latest
// amMaxExpired expired ones.
func parseAMSilences(body []byte, withExpired bool) (amSilencesResult, error) {
	var raw []amAPISilence
	if err := json.Unmarshal(body, &raw); err != nil {
		return amSilencesResult{}, fmt.Errorf("not an Alertmanager answer: %w", err)
	}

	out := make([]amSilence, 0, len(raw))

	for _, s := range raw {
		if s.Status.State == amStateExpired && !withExpired {
			continue
		}

		out = append(out, amSilence{
			ID: s.ID, State: s.Status.State, Matchers: nonNil(s.Matchers), CreatedBy: s.CreatedBy, Comment: s.Comment,
			StartsAt: unixMilli(s.StartsAt), EndsAt: unixMilli(s.EndsAt), UpdatedAt: unixMilli(s.UpdatedAt),
		})
	}

	return amSilencesResult{Silences: sortAMSilences(out)}, nil
}

func sortAMSilences(silences []amSilence) []amSilence {
	rank := func(s amSilence) int {
		if i := slices.Index(amSilenceStateOrder, s.State); i >= 0 {
			return i
		}

		return len(amSilenceStateOrder)
	}

	slices.SortStableFunc(silences, func(a, b amSilence) int {
		byTime := cmp.Compare(a.EndsAt, b.EndsAt)

		switch a.State {
		case amStatePending:
			byTime = cmp.Compare(a.StartsAt, b.StartsAt)
		case amStateExpired:
			byTime = cmp.Compare(b.EndsAt, a.EndsAt)
		}

		return cmp.Or(cmp.Compare(rank(a), rank(b)), byTime, cmp.Compare(a.ID, b.ID))
	})

	expired := slices.IndexFunc(silences, func(s amSilence) bool { return s.State == amStateExpired })
	if expired >= 0 && len(silences)-expired > amMaxExpired {
		silences = silences[:expired+amMaxExpired]
	}

	return silences
}

// amErrorMessage is the reason Alertmanager gives for a refused request: a JSON string, a
// {code,message} object or plain text.
func amErrorMessage(status int, body []byte) string {
	var text string
	if json.Unmarshal(body, &text) == nil && text != "" {
		return clipUTF8(strings.TrimSpace(text), 300)
	}

	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &obj) == nil && obj.Message != "" {
		return clipUTF8(strings.TrimSpace(obj.Message), 300)
	}

	if text = strings.TrimSpace(string(body)); text != "" {
		return clipUTF8(text, 300)
	}

	return fmt.Sprintf("HTTP %d", status)
}
