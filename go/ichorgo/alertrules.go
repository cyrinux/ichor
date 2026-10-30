package ichorgo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// Alert rules: the apps store quiet hours, a minimum severity and a digest switch, once as a
// default and per cluster, and ask AlertRulesApply what to do with the alerts a background
// check is about to post. The rule is written (and tested) once instead of once per app. It
// calls no cluster and keeps no state, like FleetSummary.

// The actions of a decision.
const (
	alertNotify = "notify"
	alertMute   = "mute"
	alertDigest = "digest"
)

// The reasons of a decision that is not "notify".
const (
	alertReasonSnoozed  = "snoozed"
	alertReasonSeverity = "severity"
	alertReasonQuiet    = "quiet"
	alertReasonDigest   = "digest"
)

// The severities of an alert, lowest first: the order the minimum severity compares on.
var alertSeverities = []string{"info", "warning", "critical"}

const (
	alertWarning  = "warning"
	alertCritical = "critical"
)

// alertCriticalKinds are the kinds whose alerts are critical when the app sends no severity:
// a cluster or a node that does not answer, and etcd.
var alertCriticalKinds = []string{"unreachable", "node", "etcd"}

// alertDigestKeys is how many alert keys a digest names.
const alertDigestKeys = 5

// alertQuietHours is a daily window, wall-clock time in an IANA zone. start == end (both empty
// included) is no window: a cluster sets that to turn off the default's quiet hours.
type alertQuietHours struct {
	Start          string `json:"start"`
	End            string `json:"end"`
	TZ             string `json:"tz"`
	ExceptCritical bool   `json:"exceptCritical"`
}

// alertRule is one rule; an absent field (nil) takes the default rule's.
type alertRule struct {
	QuietHours  *alertQuietHours `json:"quietHours"`
	MinSeverity *string          `json:"minSeverity"`
	Digest      *bool            `json:"digest"`
}

type alertRules struct {
	Default  alertRule            `json:"default"`
	Clusters map[string]alertRule `json:"clusters"`
}

// alertIn is one alert as the apps build it from a check's outcome.
type alertIn struct {
	Cluster  string `json:"cluster"`
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"`
	Problem  bool   `json:"problem"`
	Snoozed  bool   `json:"snoozed"`
}

type alertDecision struct {
	Cluster string `json:"cluster"`
	Key     string `json:"key"`
	Action  string `json:"action"`
	Reason  string `json:"reason"`
	Until   int64  `json:"until"`
}

type alertDigestGroup struct {
	Cluster   string   `json:"cluster"`
	Count     int      `json:"count"`
	Critical  int      `json:"critical"`
	Warning   int      `json:"warning"`
	FirstKeys []string `json:"firstKeys"`
}

type alertRulesResult struct {
	Decisions []alertDecision    `json:"decisions"`
	Digests   []alertDigestGroup `json:"digests"`
}

// quietWindow is a parsed quiet-hours window: minutes since midnight in loc.
type quietWindow struct {
	start, end     int
	loc            *time.Location
	exceptCritical bool
}

// resolvedRule is a cluster's rule with the default filled in and its window parsed.
type resolvedRule struct {
	quiet       *quietWindow
	minSeverity string
	digest      bool
}

// AlertRulesApply decides, for each alert of alertsJSON ([{cluster, key, kind, severity,
// problem, snoozed}…]), whether the app posts it now ("notify"), drops it ("mute") or adds
// it to the cluster's digest ("digest"), under rulesJSON ({"default":{rule},
// "clusters":{"<fingerprint>":{rule}}}, rule = {quietHours:{start,end,tz,exceptCritical},
// minSeverity: all|warning|critical, digest}) as of nowMillis (unix ms).
//
// A cluster's rule overrides the default field by field. The checks run in this order: a
// snoozed alert is muted ("snoozed"); a problem below the minimum severity is muted
// ("severity"; a recovery, problem false, always passes the floor); inside the quiet hours an
// alert is muted ("quiet", until = the window's end in unix ms) unless the window lets
// critical alerts through and the alert is critical (a recovery of a critical alert too);
// then a digest cluster's alert goes to its digest ("digest"), except the recovery of a
// critical alert, which is posted on its own. A missing severity is
// "warning", or "critical" for the unreachable, node and etcd kinds; an unknown one is a
// warning. The answer lists one decision per alert in order, and one digest per cluster that
// has digest decisions, in the order of their first alert, with at most five keys.
//
// Unknown rule fields, a time that is not HH:MM and a zone that is not IANA are errors naming
// the cluster ("default" for the default rule); the app passes the phone's zone, Go in the
// apps has none. The result echoes the app's own fingerprints and keys, which it matches back,
// so it is not masked (only the error is).
func AlertRulesApply(rulesJSON, alertsJSON string, nowMillis int64) (out string, err error) {
	defer maskErr(&err)

	rules, err := decodeAlertRules(rulesJSON)
	if err != nil {
		return "", err
	}

	var alerts []alertIn
	if err := json.Unmarshal([]byte(alertsJSON), &alerts); err != nil {
		return "", fmt.Errorf("invalid alerts: %w", err)
	}

	resolved, err := resolveAlertRules(rules, alerts)
	if err != nil {
		return "", err
	}

	return toJSON(applyAlertRules(resolved, alerts, time.UnixMilli(nowMillis)))
}

func decodeAlertRules(rulesJSON string) (alertRules, error) {
	var rules alertRules

	if strings.TrimSpace(rulesJSON) == "" {
		return rules, nil
	}

	dec := json.NewDecoder(bytes.NewReader([]byte(rulesJSON)))
	dec.DisallowUnknownFields()

	if err := dec.Decode(&rules); err != nil {
		return rules, fmt.Errorf("invalid alert rules: %w", err)
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return rules, errors.New("invalid alert rules: data after the rules object")
	}

	return rules, nil
}

// resolveAlertRules resolves the default and every cluster rule the rules or the alerts name,
// so a bad cluster rule is reported even when that cluster has no alert this time.
func resolveAlertRules(rules alertRules, alerts []alertIn) (map[string]resolvedRule, error) {
	base, err := resolveAlertRule("default", alertRule{}, rules.Default)
	if err != nil {
		return nil, err
	}

	out := map[string]resolvedRule{"": base}

	names := make([]string, 0, len(rules.Clusters))
	for name := range rules.Clusters {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		r, err := resolveAlertRule(name, rules.Default, rules.Clusters[name])
		if err != nil {
			return nil, err
		}

		out[name] = r
	}

	for _, a := range alerts {
		if _, ok := out[a.Cluster]; !ok {
			out[a.Cluster] = base
		}
	}

	return out, nil
}

func resolveAlertRule(name string, base, over alertRule) (resolvedRule, error) {
	merged := base
	if over.QuietHours != nil {
		merged.QuietHours = over.QuietHours
	}

	if over.MinSeverity != nil {
		merged.MinSeverity = over.MinSeverity
	}

	if over.Digest != nil {
		merged.Digest = over.Digest
	}

	out := resolvedRule{minSeverity: "all"}

	if merged.MinSeverity != nil && *merged.MinSeverity != "" {
		switch s := *merged.MinSeverity; s {
		case "all", alertWarning, alertCritical:
			out.minSeverity = s
		default:
			return out, fmt.Errorf("alert rule %s: minSeverity %q: want all, warning or critical", name, s)
		}
	}

	if merged.Digest != nil {
		out.digest = *merged.Digest
	}

	if merged.QuietHours != nil {
		w, err := parseQuietWindow(*merged.QuietHours)
		if err != nil {
			return out, fmt.Errorf("alert rule %s: quiet hours: %w", name, err)
		}

		out.quiet = w
	}

	return out, nil
}

// parseQuietWindow parses a window; nil when start == end (no window).
func parseQuietWindow(q alertQuietHours) (*quietWindow, error) {
	if q.Start == q.End {
		return nil, nil
	}

	start, err := parseClockMinutes(q.Start)
	if err != nil {
		return nil, fmt.Errorf("start %w", err)
	}

	end, err := parseClockMinutes(q.End)
	if err != nil {
		return nil, fmt.Errorf("end %w", err)
	}

	if q.TZ == "" {
		return nil, errors.New("no time zone (pass the phone's IANA zone)")
	}

	loc, err := time.LoadLocation(q.TZ)
	if err != nil || q.TZ == "Local" {
		return nil, fmt.Errorf("time zone %q is not an IANA zone", q.TZ)
	}

	return &quietWindow{start: start, end: end, loc: loc, exceptCritical: q.ExceptCritical}, nil
}

// parseClockMinutes reads "HH:MM" (24 h) as minutes since midnight.
func parseClockMinutes(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != len("15:04") {
		return 0, fmt.Errorf("%q: want HH:MM", s)
	}

	return t.Hour()*60 + t.Minute(), nil
}

// quietUntil is the end of the window now falls in, or the zero time when it is outside.
// It compares wall-clock minutes, so a DST day keeps the window at the same clock times.
func (w quietWindow) quietUntil(now time.Time) time.Time {
	local := now.In(w.loc)
	mins := local.Hour()*60 + local.Minute()
	y, m, d := local.Date()
	endOn := func(day int) time.Time {
		return time.Date(y, m, day, w.end/60, w.end%60, 0, 0, w.loc)
	}

	switch {
	case w.start < w.end && mins >= w.start && mins < w.end:
		return endOn(d)
	case w.start > w.end && mins >= w.start:
		return endOn(d + 1)
	case w.start > w.end && mins < w.end:
		return endOn(d)
	default:
		return time.Time{}
	}
}

func applyAlertRules(rules map[string]resolvedRule, alerts []alertIn, now time.Time) alertRulesResult {
	out := alertRulesResult{Decisions: make([]alertDecision, 0, len(alerts)), Digests: []alertDigestGroup{}}

	for _, a := range alerts {
		severity := alertSeverityOf(a)
		d := decideAlert(rules[a.Cluster], a, severity, now)
		out.Decisions = append(out.Decisions, d)

		if d.Action == alertDigest {
			out.Digests = addToDigest(out.Digests, a, severity)
		}
	}

	return out
}

func decideAlert(rule resolvedRule, a alertIn, severity string, now time.Time) alertDecision {
	d := alertDecision{Cluster: a.Cluster, Key: a.Key, Action: alertNotify}

	switch {
	case a.Snoozed:
		d.Action, d.Reason = alertMute, alertReasonSnoozed
	case a.Problem && belowAlertFloor(severity, rule.minSeverity):
		d.Action, d.Reason = alertMute, alertReasonSeverity
	case rule.quiet != nil && !(rule.quiet.exceptCritical && severity == alertCritical):
		if until := rule.quiet.quietUntil(now); !until.IsZero() {
			d.Action, d.Reason, d.Until = alertMute, alertReasonQuiet, until.UnixMilli()
		}
	}

	// The recovery of a critical alert is not folded into the digest: it says the page is over.
	if d.Action == alertNotify && rule.digest && (a.Problem || severity != alertCritical) {
		d.Action, d.Reason = alertDigest, alertReasonDigest
	}

	return d
}

// alertSeverityOf is the alert's severity in lower case, defaulted by kind.
func alertSeverityOf(a alertIn) string {
	s := strings.ToLower(strings.TrimSpace(a.Severity))

	switch {
	case slices.Contains(alertSeverities, s):
		return s
	case s == "" && slices.Contains(alertCriticalKinds, a.Kind):
		return alertCritical
	default:
		return alertWarning
	}
}

func belowAlertFloor(severity, minSeverity string) bool {
	if minSeverity == "all" {
		return false
	}

	return slices.Index(alertSeverities, severity) < slices.Index(alertSeverities, minSeverity)
}

func addToDigest(digests []alertDigestGroup, a alertIn, severity string) []alertDigestGroup {
	i := slices.IndexFunc(digests, func(g alertDigestGroup) bool { return g.Cluster == a.Cluster })
	if i < 0 {
		digests = append(digests, alertDigestGroup{Cluster: a.Cluster, FirstKeys: []string{}})
		i = len(digests) - 1
	}

	g := &digests[i]
	g.Count++

	switch severity {
	case alertCritical:
		g.Critical++
	case alertWarning:
		g.Warning++
	}

	if len(g.FirstKeys) < alertDigestKeys {
		g.FirstKeys = append(g.FirstKeys, a.Key)
	}

	return digests
}
