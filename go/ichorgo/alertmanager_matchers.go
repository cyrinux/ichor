package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const (
	amMaxMatchers   = 32
	amMaxLabelName  = 256
	amMaxLabelValue = 1024
)

// amMatcher is one Alertmanager label matcher, as its v2 API and the app spell it: name =
// value, != with isEqual false, =~ / !~ with isRegex (anchored, RE2 like Prometheus).
type amMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual bool   `json:"isEqual"`
}

// amMatcherInput is what the app sends: isEqual left out means true, as in Alertmanager.
type amMatcherInput struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex bool   `json:"isRegex"`
	IsEqual *bool  `json:"isEqual"`
}

// parseAMMatchers decodes and checks the matchers the app sends ("" or "[]": none), with
// the privacy mask's fake names swapped back for the real ones.
func parseAMMatchers(matchersJSON string) ([]amMatcher, error) {
	if strings.TrimSpace(matchersJSON) == "" {
		return nil, nil
	}

	var in []amMatcherInput
	if err := json.Unmarshal([]byte(matchersJSON), &in); err != nil {
		return nil, errors.New("bad matchers: want a JSON array of {name,value,isRegex,isEqual}")
	}

	if len(in) > amMaxMatchers {
		return nil, fmt.Errorf("at most %d matchers", amMaxMatchers)
	}

	out := make([]amMatcher, 0, len(in))

	for _, m := range in {
		matcher := amMatcher{Name: strings.TrimSpace(m.Name), Value: privacy.reveal(m.Value), IsRegex: m.IsRegex, IsEqual: m.IsEqual == nil || *m.IsEqual}
		if err := matcher.validate(); err != nil {
			return nil, err
		}

		out = append(out, matcher)
	}

	return out, nil
}

func (m amMatcher) validate() error {
	switch {
	case m.Name == "" || len(m.Name) > amMaxLabelName || strings.ContainsFunc(m.Name, amBadNameRune):
		return fmt.Errorf("the label name %q is not valid", clipUTF8(m.Name, 40))
	case len(m.Value) > amMaxLabelValue:
		return fmt.Errorf("the value of %s is longer than %d bytes", m.Name, amMaxLabelValue)
	case strings.ContainsFunc(m.Value, unicode.IsControl):
		return fmt.Errorf("the value of %s has a control character", m.Name)
	}

	if m.IsRegex {
		if _, err := regexp.Compile("^(?:" + m.Value + ")$"); err != nil {
			return fmt.Errorf("the regular expression for %s is not valid: %v", m.Name, err)
		}
	}

	return nil
}

// amBadNameRune refuses what no label name holds (Alertmanager 0.27+ takes UTF-8 names).
func amBadNameRune(r rune) bool {
	return unicode.IsControl(r) || unicode.IsSpace(r) || strings.ContainsRune(`"{}=!~,`, r)
}

// matches reports whether labels satisfy m (a missing label is the empty value).
func (m amMatcher) matches(labels map[string]string) bool {
	v := labels[m.Name]

	ok := v == m.Value
	if m.IsRegex {
		re, err := regexp.Compile("^(?:" + m.Value + ")$")
		ok = err == nil && re.MatchString(v)
	}

	return ok == m.IsEqual
}

// filter is the matcher in Alertmanager's filter syntax: name="value", name=~"regex"...
func (m amMatcher) filter() string {
	op := map[[2]bool]string{{false, true}: "=", {false, false}: "!=", {true, true}: "=~", {true, false}: "!~"}[[2]bool{m.IsRegex, m.IsEqual}]

	return m.Name + op + strconv.Quote(m.Value)
}

// amMatchesEverything reports whether every matcher matches a missing label: Alertmanager
// refuses such a silence, which would mute every alert.
func amMatchesEverything(matchers []amMatcher) bool {
	return !slices.ContainsFunc(matchers, func(m amMatcher) bool { return !m.matches(nil) })
}

// amReplicaLabels tell the replicas of a highly available Prometheus apart: an alert fires
// from each with the same other labels, so a silence leaves them out to cover both.
var amReplicaLabels = []string{"prometheus_replica", "replica", "__replica__"}

// amSilenceMatchers are the matchers silencing one alert: every label it has, as equals,
// alertname first, but the HA replica labels and the empty values. Every label rather than
// a few "identifying" ones: like Alertmanager's own "Silence" button, it mutes exactly this
// alert, never its siblings on other instances, pods or namespaces. instance, pod or
// endpoint change when a pod is replaced, which then ends the silence: the user widens it
// by removing those matchers in the form, a choice the app should not make for them.
func amSilenceMatchers(labels map[string]string) []amMatcher {
	names := make([]string, 0, len(labels))

	for name, value := range labels {
		if value != "" && !slices.Contains(amReplicaLabels, name) {
			names = append(names, name)
		}
	}

	slices.SortFunc(names, func(a, b string) int {
		switch {
		case a == "alertname":
			return -1
		case b == "alertname":
			return 1
		default:
			return strings.Compare(a, b)
		}
	})

	out := make([]amMatcher, 0, len(names))
	for _, name := range names {
		out = append(out, amMatcher{Name: name, Value: labels[name], IsEqual: true})
	}

	return out
}

// AlertmanagerSilenceMatchers proposes the matchers to silence one alert, from its labels
// (labelsJSON: {"name":"value"}, an alert's labels as AlertmanagerAlerts returns them):
// [{name,value,isRegex:false,isEqual:true}], alertname first, then every other label but
// the HA replica ones (prometheus_replica, replica). The app shows them in the silence form
// for the user to trim before AlertmanagerSilence.
func AlertmanagerSilenceMatchers(labelsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var labels map[string]string
	if err := json.Unmarshal([]byte(labelsJSON), &labels); err != nil {
		return "", errors.New("bad labels: want a JSON object of strings")
	}

	revealed := make(map[string]string, len(labels))
	for name, value := range labels {
		revealed[name] = privacy.reveal(value)
	}

	return toJSON(amSilenceMatchers(revealed))
}
