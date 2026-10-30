package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// On-call integration: per cluster, the open incidents of a pager, matched to the Alertmanager
// alerts the app lists, and acknowledging or resolving them. The settings (token included) come
// from the apps' sealed store as JSON on every call; they are never kept, logged or returned.

const (
	onCallKindPagerDuty = "pagerduty"

	onCallStatusTriggered    = "triggered"
	onCallStatusAcknowledged = "acknowledged"

	onCallVerbAcknowledge = "acknowledge"
	onCallVerbResolve     = "resolve"

	onCallConfidenceExact     = "exact"
	onCallConfidenceAlertname = "alertname"

	// onCallCallTimeout bounds one request; onCallListTimeout the incidents and their alerts.
	onCallCallTimeout = 15 * time.Second
	onCallListTimeout = 40 * time.Second
	onCallMaxTextLen  = 256
	onCallMaxServices = 50
)

var (
	onCallNow = time.Now
	// An incident or service ID as PagerDuty writes them; goes into a request path.
	onCallID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// onCallConfig is the settings of one cluster's integration. Region is "us" or "eu".
type onCallConfig struct {
	Kind       string   `json:"kind"`
	Token      string   `json:"token"`
	From       string   `json:"from"`
	ServiceIDs []string `json:"serviceIds"`
	Region     string   `json:"region"`
}

// onCallIncident is one open incident. Details holds its alerts' text for the matching; it stays
// in Go.
type onCallIncident struct {
	ID                  string   `json:"id"`
	Number              int      `json:"number"`
	Title               string   `json:"title"`
	Status              string   `json:"status"`
	Urgency             string   `json:"urgency"`
	CreatedAt           int64    `json:"createdAt"`
	URL                 string   `json:"url"`
	Service             string   `json:"service"`
	Assignees           []string `json:"assignees"`
	AcknowledgedBy      string   `json:"acknowledgedBy"`
	MatchedFingerprints []string `json:"matchedFingerprints"`
	Details             []string `json:"-"`
}

// onCallMatch is the incident behind one alert.
type onCallMatch struct {
	IncidentID string `json:"incidentId"`
	Status     string `json:"status"`
	Confidence string `json:"confidence"`
}

type onCallResult struct {
	Incidents []onCallIncident       `json:"incidents"`
	Matches   map[string]onCallMatch `json:"matches"`
}

// onCallAmAlert is what the matching reads of an Alertmanager alert.
type onCallAmAlert struct {
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels"`
}

// onCallIdentity answers OnCallTest; User is empty for an account-level token.
type onCallIdentity struct {
	User    string `json:"user"`
	Account string `json:"account"`
}

// onCallProvider is one pager's API. act returns the incident's number (0 when unknown).
type onCallProvider interface {
	incidents(ctx context.Context) ([]onCallIncident, error)
	act(ctx context.Context, id, verb string) (int, error)
	identity(ctx context.Context) (onCallIdentity, error)
}

// parseOnCallConfig reads the settings. The messages never quote the input: it holds the token.
func parseOnCallConfig(configJSON string) (onCallConfig, error) {
	var c onCallConfig
	if json.Unmarshal([]byte(configJSON), &c) != nil {
		return onCallConfig{}, errors.New("the on-call settings are not valid")
	}

	c.Token, c.From = strings.TrimSpace(c.Token), strings.TrimSpace(c.From)
	c.Region = cmp.Or(strings.ToLower(strings.TrimSpace(c.Region)), "us")

	switch {
	case c.Kind != onCallKindPagerDuty:
		return onCallConfig{}, errors.New("this on-call provider is not supported")
	case c.Token == "":
		return onCallConfig{}, errors.New("add the PagerDuty token")
	case len(c.Token) > onCallMaxTextLen || strings.ContainsFunc(c.Token, isHeaderUnsafe):
		return onCallConfig{}, errors.New("the PagerDuty token is not valid")
	case len(c.From) > onCallMaxTextLen || strings.ContainsFunc(c.From, isHeaderUnsafe):
		return onCallConfig{}, errors.New("the PagerDuty email is not valid")
	case c.Region != "us" && c.Region != "eu":
		return onCallConfig{}, errors.New("the PagerDuty region must be us or eu")
	case len(c.ServiceIDs) > onCallMaxServices:
		return onCallConfig{}, fmt.Errorf("at most %d PagerDuty services", onCallMaxServices)
	case slices.ContainsFunc(c.ServiceIDs, func(id string) bool { return !onCallID.MatchString(id) }):
		return onCallConfig{}, errors.New("a PagerDuty service ID is not valid")
	}

	return c, nil
}

// isHeaderUnsafe tells a character that cannot go in a request header (a pasted line end).
func isHeaderUnsafe(r rune) bool { return r < 0x20 || r == 0x7f }

func newOnCallProvider(c onCallConfig) onCallProvider { return newPagerDuty(c) }

// OnCallIncidents lists the open incidents of the cluster's integration (onCallJSON, see
// onCallConfig) and matches them to the Alertmanager alerts of amAlertsJSON
// ([{fingerprint,labels}], "" for none): {incidents:[{id,number,title,status,urgency,createdAt
// (unix ms),url,service,assignees,acknowledgedBy,matchedFingerprints}],matches:{"<fingerprint>":
// {incidentId,status,confidence:"exact"|"alertname"}}}. The demo answers with sample incidents,
// without settings or network.
func OnCallIncidents(configYAML, contextName, onCallJSON, amAlertsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	alerts, err := parseOnCallAlerts(amAlertsJSON)
	if err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return toJSON(matchOnCall(demoOnCall(onCallNow()), alerts))
	}

	cfg, err := parseOnCallConfig(onCallJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), onCallListTimeout)
	defer cancel()

	incidents, err := newOnCallProvider(cfg).incidents(ctx)
	if err != nil {
		return "", err
	}

	return toJSON(matchOnCall(incidents, alerts))
}

// OnCallAct acknowledges or resolves (verb) the incident incidentID. Refused in the demo; recorded
// in the action log with the incident's number.
func OnCallAct(configYAML, contextName, onCallJSON, incidentID, verb string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	incidentID = strings.TrimSpace(incidentID)
	number := 0

	action := func() auditAction {
		subject := "incident " + incidentID
		if number > 0 {
			subject = "incident #" + strconv.Itoa(number)
		}

		return auditAction{Action: "oncall." + verb, Params: subject}
	}

	return recordedRun(configYAML, contextName, action, func() error {
		switch {
		case verb != onCallVerbAcknowledge && verb != onCallVerbResolve:
			return errors.New("an incident can only be acknowledged or resolved")
		case !onCallID.MatchString(incidentID):
			return errors.New("not an incident ID")
		case isDemoContext(configYAML, contextName):
			return errDemoUnavailable
		}

		cfg, err := parseOnCallConfig(onCallJSON)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), onCallCallTimeout)
		defer cancel()

		number, err = newOnCallProvider(cfg).act(ctx, incidentID, verb)

		return err
	})
}

// OnCallTest checks the settings' token: {user,account}, for the settings sheet.
func OnCallTest(onCallJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	cfg, err := parseOnCallConfig(onCallJSON)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), onCallCallTimeout)
	defer cancel()

	who, err := newOnCallProvider(cfg).identity(ctx)
	if err != nil {
		return "", err
	}

	return toJSON(who)
}

func parseOnCallAlerts(amAlertsJSON string) ([]onCallAmAlert, error) {
	if strings.TrimSpace(amAlertsJSON) == "" {
		return nil, nil
	}

	var alerts []onCallAmAlert
	if json.Unmarshal([]byte(amAlertsJSON), &alerts) != nil {
		return nil, errors.New("the alerts to match are not valid")
	}

	return alerts, nil
}

// matchOnCall ties each alert to at most one incident: an incident naming every one of its labels
// first, else one naming its alertname; among equals a triggered one, then the first listed.
// Alertmanager groups alerts, so an incident can carry several fingerprints.
func matchOnCall(incidents []onCallIncident, alerts []onCallAmAlert) onCallResult {
	res := onCallResult{Incidents: make([]onCallIncident, len(incidents)), Matches: map[string]onCallMatch{}}
	rank := map[string]int{}

	blocks := make([][]map[string]string, len(incidents))
	for i, in := range incidents {
		in.MatchedFingerprints = []string{}
		in.Assignees = nonNil(in.Assignees)
		res.Incidents[i] = in
		blocks[i] = onCallLabelBlocks(in.Details)
	}

	for _, alert := range alerts {
		name := alert.Labels["alertname"]
		if alert.Fingerprint == "" || name == "" {
			continue
		}

		word := onCallNameWord(name)

		for i, in := range incidents {
			confidence := onCallConfidence(in, blocks[i], alert.Labels, word)
			if confidence == "" {
				continue
			}

			score := onCallMatchRank(confidence, in.Status)
			if score > rank[alert.Fingerprint] {
				rank[alert.Fingerprint] = score
				res.Matches[alert.Fingerprint] = onCallMatch{IncidentID: in.ID, Status: in.Status, Confidence: confidence}
			}
		}
	}

	for fingerprint, match := range res.Matches {
		i := slices.IndexFunc(res.Incidents, func(in onCallIncident) bool { return in.ID == match.IncidentID })
		res.Incidents[i].MatchedFingerprints = append(res.Incidents[i].MatchedFingerprints, fingerprint)
	}

	for i := range res.Incidents {
		slices.Sort(res.Incidents[i].MatchedFingerprints)
	}

	return res
}

func onCallMatchRank(confidence, status string) int {
	score := 1
	if confidence == onCallConfidenceExact {
		score = 3
	}

	if status == onCallStatusTriggered {
		score++
	}

	return score
}

// onCallNameWord finds an alertname as a whole word: KubePodCrash is not KubePodCrashLooping.
func onCallNameWord(name string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_:])` + regexp.QuoteMeta(name) + `($|[^A-Za-z0-9_:])`)
}

func onCallConfidence(in onCallIncident, blocks []map[string]string, labels map[string]string, word *regexp.Regexp) string {
	if slices.ContainsFunc(blocks, func(block map[string]string) bool { return onCallBlockHasLabels(block, labels) }) {
		return onCallConfidenceExact
	}

	if word.MatchString(in.Title) || slices.ContainsFunc(in.Details, word.MatchString) {
		return onCallConfidenceAlertname
	}

	return ""
}

func onCallBlockHasLabels(block, labels map[string]string) bool {
	for name, value := range labels {
		if got, ok := block[name]; !ok || got != value {
			return false
		}
	}

	return len(labels) > 0
}

// onCallLabelBlocks reads the alerts out of Alertmanager's PagerDuty details, whose default
// template writes each one as "Labels:" then " - name = value" lines, up to "Annotations:".
func onCallLabelBlocks(details []string) []map[string]string {
	var blocks []map[string]string

	for _, text := range details {
		var block map[string]string

		for line := range strings.Lines(text) {
			line = strings.TrimSpace(line)

			switch {
			case line == "Labels:":
				block = map[string]string{}
				blocks = append(blocks, block)
			case block == nil:
			case strings.HasPrefix(line, "- "):
				if name, value, ok := strings.Cut(strings.TrimPrefix(line, "- "), " = "); ok {
					block[name] = value
				}
			default:
				block = nil
			}
		}
	}

	return blocks
}
