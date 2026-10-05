package ichorgo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

const (
	// A phone chart shows a handful of lines; a query by pod can return thousands.
	promMaxSeries = 20
	promMaxPoints = 1000
	// Labels shown per series, and their length.
	promMaxLabels     = 30
	promMaxLabelValue = 256
	promMaxWarnings   = 5
)

// promGrid is the timestamps (unix ms) every series of a result shares: a range query's
// start + i*step, or an instant query's one time.
type promGrid struct {
	start, step int64 // seconds
	n           int
}

// promResult is the app's view of a query: series aligned on Times, a missing or
// non-finite sample being null, so a chart breaks the line there.
type promResult struct {
	ResultType string       `json:"resultType"`
	Times      []int64      `json:"times"`
	Series     []promSeries `json:"series"`
	Warnings   []string     `json:"warnings"`
	// Truncated is set when series were dropped beyond promMaxSeries.
	Truncated bool `json:"truncated"`
	// Total is the number of series Prometheus returned.
	Total int `json:"total"`
}

type promSeries struct {
	// Name is metric{label="value",...}, the default legend.
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Values []*float64        `json:"values"`
}

type promEnvelope struct {
	Status    string          `json:"status"`
	ErrorType string          `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings"`
	Data      json.RawMessage `json:"data"`
}

type promData struct {
	ResultType string          `json:"resultType"`
	Result     json.RawMessage `json:"result"`
}

type promRawSeries struct {
	Metric     map[string]string `json:"metric"`
	Values     []promPair        `json:"values"`
	Value      *promPair         `json:"value"`
	Histogram  json.RawMessage   `json:"histogram"`
	Histograms json.RawMessage   `json:"histograms"`
}

// promPair is a [unix seconds, "value"] sample, decoded without boxing each element.
type promPair struct {
	t   float64
	v   string
	bad bool
}

func (p *promPair) UnmarshalJSON(b []byte) error {
	// Not an error: one odd sample should not lose the whole answer.
	p.bad = json.Unmarshal(b, &[]any{&p.t, &p.v}) != nil

	return nil
}

// parsePromAnswer reads a query API answer of any HTTP status: Prometheus reports a bad
// query (400, 422) and a failed one (503) in the same JSON envelope.
func parsePromAnswer(status int, body []byte, grid promGrid) (promResult, error) {
	var env promEnvelope
	if err := json.Unmarshal(body, &env); err != nil || env.Status == "" {
		return promResult{}, promHTTPError(status, body)
	}

	if env.Status != "success" {
		msg := strings.TrimSpace(env.Error)
		if msg == "" {
			msg = http.StatusText(status)
		}

		if env.ErrorType != "" {
			msg = env.ErrorType + ": " + msg
		}

		return promResult{}, errors.New(clipUTF8(msg, 500))
	}

	var data promData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return promResult{}, errors.New("the query answer has no data")
	}

	res := promResult{ResultType: data.ResultType, Times: grid.times(), Series: []promSeries{}, Warnings: promWarnings(env.Warnings)}

	// Each series stays raw until kept: a query by pod can return thousands.
	var items []json.RawMessage

	switch data.ResultType {
	case "matrix", "vector":
		if err := json.Unmarshal(data.Result, &items); err != nil {
			return promResult{}, fmt.Errorf("bad %s in the query answer", data.ResultType)
		}
	case "scalar":
		items = []json.RawMessage{[]byte(`{"metric":{},"value":` + string(data.Result) + `}`)}
	default:
		return promResult{}, fmt.Errorf("a %s result cannot be charted", data.ResultType)
	}

	res.Total = len(items)
	histograms := false

	for i, item := range items {
		if len(res.Series) == promMaxSeries {
			res.Truncated = i < len(items)

			break
		}

		var r promRawSeries
		if err := json.Unmarshal(item, &r); err != nil {
			return promResult{}, fmt.Errorf("bad series in the query answer")
		}

		if r.Value == nil && len(r.Values) == 0 && (r.Histogram != nil || r.Histograms != nil) {
			histograms = true

			continue
		}

		res.Series = append(res.Series, promSeriesOf(r, grid))
	}

	if histograms {
		res.Warnings = append(res.Warnings, "native histogram series are not charted: use histogram_quantile() or histogram_count()")
	}

	return res, nil
}

func promSeriesOf(r promRawSeries, grid promGrid) promSeries {
	values := make([]*float64, grid.n)

	samples := r.Values
	if r.Value != nil {
		samples = append(samples, *r.Value)
	}

	for _, s := range samples {
		v, ok := promValue(s)
		if i := grid.index(s.t); ok && i >= 0 {
			values[i] = v
		}
	}

	labels := promLabels(r.Metric)

	return promSeries{Name: promSeriesName(labels), Labels: labels, Values: values}
}

// promRefused marks a query the backend turned down (HTTP 401 or 403): the apps match it
// to offer editing the tenant and credentials (Kotlin and Swift PROM_REFUSED).
const promRefused = "refused (credentials or tenant)"

func promHTTPError(status int, body []byte) error {
	text := clipUTF8(strings.TrimSpace(string(body)), 300)

	switch {
	case status == http.StatusOK:
		return errors.New("the answer is not from a Prometheus query API: check the path prefix")
	case status == http.StatusNotFound:
		return fmt.Errorf("HTTP 404 %s: check the path prefix (Mimir: /prometheus)", text)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Errorf("HTTP %d, %s: %s", status, promRefused, text)
	case text == "":
		return fmt.Errorf("HTTP %d", status)
	default:
		return fmt.Errorf("HTTP %d: %s", status, text)
	}
}

// promValue decodes a sample's value: a NaN or infinite one is nil.
func promValue(s promPair) (*float64, bool) {
	if s.bad {
		return nil, false
	}

	v, err := strconv.ParseFloat(s.v, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, true
	}

	return &v, true
}

func promLabels(metric map[string]string) map[string]string {
	keys := make([]string, 0, len(metric))
	for k := range metric {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	labels := make(map[string]string, min(len(keys), promMaxLabels))
	for _, k := range keys[:min(len(keys), promMaxLabels)] {
		labels[clipUTF8(k, promMaxLabelValue)] = clipUTF8(metric[k], promMaxLabelValue)
	}

	return labels
}

// promSeriesName writes labels the way Prometheus does: name{a="1",b="2"}.
func promSeriesName(labels map[string]string) string {
	keys := make([]string, 0, len(labels))

	for k := range labels {
		if k != "__name__" {
			keys = append(keys, k)
		}
	}

	slices.Sort(keys)

	var b bytes.Buffer
	b.WriteString(labels["__name__"])

	if len(keys) == 0 {
		if b.Len() == 0 {
			return "{}"
		}

		return b.String()
	}

	b.WriteByte('{')

	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}

		fmt.Fprintf(&b, "%s=%q", k, labels[k])
	}

	b.WriteByte('}')

	return b.String()
}

func promWarnings(warnings []string) []string {
	out := []string{}
	for _, w := range warnings[:min(len(warnings), promMaxWarnings)] {
		out = append(out, clipUTF8(w, 300))
	}

	return out
}

func (g promGrid) times() []int64 {
	times := make([]int64, g.n)
	for i := range times {
		times[i] = (g.start + int64(i)*g.step) * 1000
	}

	return times
}

// index is the grid slot of a sample time, -1 outside the grid.
func (g promGrid) index(t float64) int {
	if g.n == 1 {
		return 0
	}

	i := int(math.Round((t - float64(g.start)) / float64(g.step)))
	if i < 0 || i >= g.n {
		return -1
	}

	return i
}
