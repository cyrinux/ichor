package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const (
	// Far more than a cluster exposes (kube-prometheus-stack: a few thousand); a server
	// listing without end is stopped.
	promMetricNamesMax      = 10000
	promMetricNamesMaxBytes = 512 << 10
)

// A metric name as PromQL accepts it.
var promMetricName = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// PromMetricNames lists the metric names the source sourceJSON knows (GET
// /api/v1/label/__name__/values), sorted, as JSON ["name", ...], for the panel assistant to
// write queries with metrics that exist. The demo cluster lists the usual node, container,
// kube-state and etcd metrics. kubeServer is as for PromQueryRange.
func PromMetricNames(configYAML, contextName, kubeServer, sourceJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	names, err := promMetricNames(kubeTarget{configYAML, contextName, kubeServer}, sourceJSON)
	if err != nil {
		return "", err
	}

	return toJSON(names)
}

func promMetricNames(target kubeTarget, sourceJSON string) ([]string, error) {
	src, err := parsePromSource(sourceJSON)
	if err != nil {
		return nil, err
	}

	if isDemoContext(target.config, target.context) {
		return demoPromMetricNames(), nil
	}

	status, body, err := promGet(target, src, "/api/v1/label/__name__/values", url.Values{})
	if err != nil {
		return nil, err
	}

	names, err := parsePromMetricNames(status, body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src.label(), err)
	}

	return names, nil
}

// parsePromMetricNames reads a label values answer, keeping the well-formed names, sorted
// and without duplicates (a federated source may list one twice), up to the caps.
func parsePromMetricNames(status int, body []byte) ([]string, error) {
	data, err := promEnvelopeData(status, body)
	if err != nil {
		return nil, err
	}

	var raw []string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.New("the metric names answer has no data")
	}

	names := make([]string, 0, len(raw))
	for _, n := range raw {
		if promMetricName.MatchString(n) {
			names = append(names, n)
		}
	}

	slices.Sort(names)
	names = slices.Compact(names)

	size := 0
	for i, n := range names {
		size += len(n) + 1
		if i == promMetricNamesMax || size > promMetricNamesMaxBytes {
			names = names[:i]

			break
		}
	}

	return names, nil
}

// promEnvelopeData is the data of a successful API answer of any HTTP status (other than
// the query one); the reason Prometheus gives for a failed one.
func promEnvelopeData(status int, body []byte) (json.RawMessage, error) {
	var env promEnvelope
	if err := json.Unmarshal(body, &env); err != nil || env.Status == "" {
		return nil, promHTTPError(status, body)
	}

	if env.Status != "success" {
		msg := strings.TrimSpace(env.Error)
		if msg == "" {
			msg = http.StatusText(status)
		}

		if env.ErrorType != "" {
			msg = env.ErrorType + ": " + msg
		}

		return nil, errors.New(clipUTF8(msg, 500))
	}

	return env.Data, nil
}
