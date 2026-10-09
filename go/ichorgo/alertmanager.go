package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Alertmanager (CYR-34): the alerts it holds, its silences, and silencing or expiring from
// the phone. It is reached like a metrics source (prom_source.go), through the API server's
// service proxy or at a URL, with the same source JSON (kind "alertmanager"), and spoken to
// through its v2 API.

const (
	amMaxSilenceMinutes = 30 * 24 * 60
	amMaxComment        = 1000
	amMaxReceiver       = 256
	amCreatedBy         = "ichor"
)

var (
	amNow = time.Now
	// An Alertmanager silence ID is a UUID; allow any short token of its characters.
	amSilenceID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
)

// AlertmanagerAlerts lists the alerts of the Alertmanager at sourceJSON (see
// AlertmanagerDiscover), grouped by alertname:
// {groups:[{alertname,severity,count,active,alerts:[{fingerprint,alertname,severity,labels,
// annotations,summary,description,runbookURL,generatorURL,startsAt,endsAt,updatedAt (unix
// ms),receivers,state:"active"|"suppressed"|"unprocessed",silencedBy,inhibitedBy}]}],
// counts:{critical,warning,info,other,suppressed},total,truncated}. severity is critical,
// warning, info or other. active, silenced and inhibited pick the alerts in those states;
// receiver ("" for any) is a regular expression on the receiver names; matchersJSON ("" for
// none) narrows to the alerts matching every matcher [{name,value,isRegex,isEqual}].
// kubeServer is the API server address set for the cluster, for the proxy mode.
func AlertmanagerAlerts(configYAML, contextName, kubeServer, sourceJSON string, active, silenced, inhibited bool, receiver, matchersJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	src, err := parsePromSource(sourceJSON)
	if err != nil {
		return "", err
	}

	matchers, err := parseAMMatchers(matchersJSON)
	if err != nil {
		return "", err
	}

	receiver = strings.TrimSpace(receiver)
	if len(receiver) > amMaxReceiver {
		return "", errors.New("the receiver filter is too long")
	}

	var receiverRE *regexp.Regexp
	if receiver != "" {
		if receiverRE, err = regexp.Compile("^(?:" + receiver + ")$"); err != nil {
			return "", fmt.Errorf("the receiver filter is not a valid regular expression: %v", err)
		}
	}

	if isDemoContext(configYAML, contextName) {
		f := amAlertFilter{active: active, silenced: silenced, inhibited: inhibited, receiver: receiverRE, matchers: matchers}

		return toJSON(groupAMAlerts(f.apply(demoAMAlerts(amNow()))))
	}

	params := url.Values{
		"active":      {strconv.FormatBool(active)},
		"unprocessed": {strconv.FormatBool(active)},
		"silenced":    {strconv.FormatBool(silenced)},
		"inhibited":   {strconv.FormatBool(inhibited)},
	}

	if receiver != "" {
		params.Set("receiver", receiver)
	}

	for _, m := range matchers {
		params.Add("filter", m.filter())
	}

	body, err := amCall(kubeTarget{configYAML, contextName, kubeServer}, src, http.MethodGet, "/api/v2/alerts?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}

	res, err := parseAMAlerts(body)
	if err != nil {
		return "", fmt.Errorf("%s: %w", src.label(), err)
	}

	return toJSON(res)
}

// AlertmanagerSilences lists the silences of the Alertmanager at sourceJSON:
// {silences:[{id,state:"active"|"pending"|"expired",matchers:[{name,value,isRegex,
// isEqual}],createdBy,comment,startsAt,endsAt,updatedAt (unix ms)}]}, the active ones first
// (ending soonest first), then the pending ones; withExpired adds the latest 100 expired.
func AlertmanagerSilences(configYAML, contextName, kubeServer, sourceJSON string, withExpired bool) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	src, err := parsePromSource(sourceJSON)
	if err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return toJSON(amSilencesResult{Silences: sortAMSilences(demoAMSilences(amNow(), withExpired))})
	}

	body, err := amCall(kubeTarget{configYAML, contextName, kubeServer}, src, http.MethodGet, "/api/v2/silences", nil)
	if err != nil {
		return "", err
	}

	res, err := parseAMSilences(body, withExpired)
	if err != nil {
		return "", fmt.Errorf("%s: %w", src.label(), err)
	}

	return toJSON(res)
}

// AlertmanagerSilence creates a silence on the Alertmanager at sourceJSON for
// durationMinutes (1 to 30 days) from now, created by "ichor" with comment (required), and
// returns its ID. matchersJSON is [{name,value,isRegex,isEqual}] (see
// AlertmanagerSilenceMatchers to silence one alert); at least one must not match an empty
// value, so the silence cannot mute everything. Refused in the demo.
func AlertmanagerSilence(configYAML, contextName, kubeServer, sourceJSON, matchersJSON string, durationMinutes int, comment string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	src, srcErr := parsePromSource(sourceJSON)
	matchers, matchersErr := parseAMMatchers(matchersJSON)
	comment = strings.TrimSpace(comment)

	defer recordAction(&err, configYAML, contextName, amAuditAction(kubeServer, "alertmanager-silence", src,
		fmt.Sprintf("%s for %dm: %s", amMatchersText(matchers), durationMinutes, clipUTF8(comment, 200))))

	switch {
	case srcErr != nil:
		return "", srcErr
	case matchersErr != nil:
		return "", matchersErr
	case len(matchers) == 0:
		return "", errors.New("a silence needs at least one matcher")
	case amMatchesEverything(matchers):
		return "", errors.New("every matcher also matches an empty value: the silence would mute every alert")
	case durationMinutes < 1 || durationMinutes > amMaxSilenceMinutes:
		return "", errors.New("the silence must last from 1 minute to 30 days")
	case comment == "":
		return "", errors.New("a silence needs a comment: say why")
	case len(comment) > amMaxComment:
		return "", fmt.Errorf("the comment is longer than %d bytes", amMaxComment)
	case isDemoContext(configYAML, contextName):
		return "", errDemoUnavailable
	}

	now := amNow().UTC()
	body, err := json.Marshal(map[string]any{
		"matchers":  matchers,
		"startsAt":  now.Format(time.RFC3339),
		"endsAt":    now.Add(time.Duration(durationMinutes) * time.Minute).Format(time.RFC3339),
		"createdBy": amCreatedBy,
		"comment":   comment,
	})
	if err != nil {
		return "", fmt.Errorf("encode silence: %w", err)
	}

	answer, err := amCall(kubeTarget{configYAML, contextName, kubeServer}, src, http.MethodPost, "/api/v2/silences", body)
	if err != nil {
		return "", err
	}

	var created struct {
		SilenceID string `json:"silenceID"`
	}

	if json.Unmarshal(answer, &created) != nil || created.SilenceID == "" {
		return "", fmt.Errorf("%s: the silence was created but no ID came back: refresh the silences", src.label())
	}

	return created.SilenceID, nil
}

// AlertmanagerExpire ends the silence silenceID on the Alertmanager at sourceJSON now.
// Refused in the demo.
func AlertmanagerExpire(configYAML, contextName, kubeServer, sourceJSON, silenceID string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	src, srcErr := parsePromSource(sourceJSON)
	silenceID = strings.TrimSpace(silenceID)

	defer recordAction(&err, configYAML, contextName, amAuditAction(kubeServer, "alertmanager-expire", src, "silence="+silenceID))

	switch {
	case srcErr != nil:
		return srcErr
	case !amSilenceID.MatchString(silenceID):
		return errors.New("not a silence ID")
	case isDemoContext(configYAML, contextName):
		return errDemoUnavailable
	}

	_, err = amCall(kubeTarget{configYAML, contextName, kubeServer}, src, http.MethodDelete, "/api/v2/silence/"+url.PathEscape(silenceID), nil)

	return err
}

// amAuditAction describes an Alertmanager change: on the Service in proxy mode (so the
// optional Kubernetes Event lands on it), with the URL in the parameters otherwise.
func amAuditAction(kubeServer, action string, src promSource, params string) auditAction {
	a := auditAction{Server: kubeServer, Action: action, Params: params}

	switch {
	case src.Mode == promModeProxy && src.Service != "":
		a.Namespace, a.Object = src.Namespace, "Service/"+src.Service
	case src.URL != "":
		a.Params = "alertmanager=" + src.URL + " " + params
	}

	return a
}

// amMatchersText spells matchers as Alertmanager does: {alertname="X", pod=~"a.*"}.
func amMatchersText(matchers []amMatcher) string {
	parts := make([]string, 0, len(matchers))
	for _, m := range matchers {
		parts = append(parts, m.filter())
	}

	return "{" + strings.Join(parts, ", ") + "}"
}

// amCall makes one v2 API request and returns the body of a successful answer; the reason
// Alertmanager gives for any other. A change without an answer may have been applied.
func amCall(target kubeTarget, src promSource, method, pathQuery string, body []byte) ([]byte, error) {
	status, answer, err := promSend(target, src, method, pathQuery, body)

	switch {
	case errors.Is(err, errPromNoAnswer) && method != http.MethodGet:
		return nil, fmt.Errorf("%s: no answer within %s (it may have been applied: refresh before trying again)", src.label(), callTimeout)
	case errors.Is(err, errPromNoAnswer):
		return nil, fmt.Errorf("%s: no answer within %s", src.label(), callTimeout)
	case err != nil:
		return nil, err
	case status == http.StatusNotFound && method == http.MethodGet:
		return nil, fmt.Errorf("%s: no Alertmanager v2 API here (check the port and the path prefix)", src.label())
	case status/100 != 2:
		return nil, fmt.Errorf("%s: Alertmanager refused: %s", src.label(), amErrorMessage(status, answer))
	}

	return answer, nil
}
