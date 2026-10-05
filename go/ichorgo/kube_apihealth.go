package ichorgo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// apiHealthWindow is how long apart the two /metrics scrapes are: long enough for the
// counters to move on a quiet cluster, short enough to stay well inside callTimeout.
const apiHealthWindow = 5 * time.Second

// The overall verdict of apiHealthReport.Status, worst first.
const (
	apiStatusUnhealthy  = "unhealthy"  // a readyz or livez check fails
	apiStatusThrottling = "throttling" // requests are being rejected (429)
	apiStatusBusy       = "busy"       // requests wait in queues, or a share of answers are 5xx
	apiStatusOK         = "ok"
)

// apiHealthReport is the API server's health and what puts pressure on it. Rates are per
// second over windowSeconds, or since the server started when windowSeconds is 0.
type apiHealthReport struct {
	Status         string          `json:"status"`
	Version        string          `json:"version,omitempty"`
	Ready          apiProbe        `json:"ready"`
	Live           apiProbe        `json:"live"`
	UptimeSeconds  int64           `json:"uptimeSeconds"`
	WindowSeconds  float64         `json:"windowSeconds"`
	RequestRate    float64         `json:"requestRate"`
	ErrorRate      float64         `json:"errorRate"`     // 5xx answers
	ThrottledRate  float64         `json:"throttledRate"` // 429 answers
	RejectedRate   float64         `json:"rejectedRate"`  // refused by API Priority and Fairness
	InflightRead   int             `json:"inflightRead"`
	InflightMutate int             `json:"inflightMutate"`
	Queued         int             `json:"queued"`
	Watches        int             `json:"watches"`
	WatchEventRate float64         `json:"watchEventRate"`
	EtcdLatencyMs  float64         `json:"etcdLatencyMs"`
	Clients        []apiFlow       `json:"clients"`
	Priorities     []apiPriority   `json:"priorities"`
	Requests       []apiRequestRow `json:"requests"`
	WatchedKinds   []apiCount      `json:"watchedKinds"`
	Objects        []apiCount      `json:"objects"`
	QueuedRequests []apiQueued     `json:"queuedRequests"`
	MetricsError   string          `json:"metricsError,omitempty"` // /metrics could not be read
}

// apiProbe is the answer of /readyz or /livez: every check, and whether all passed.
type apiProbe struct {
	OK     bool       `json:"ok"`
	Checks []apiCheck `json:"checks"`
	Error  string     `json:"error,omitempty"` // the probe itself got no answer
}

type apiCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// apiFlow is one flow schema, API Priority and Fairness's grouping of clients (nodes, the
// controller manager, service accounts...): who sends the requests.
type apiFlow struct {
	Name         string  `json:"name"`
	Priority     string  `json:"priority"`
	Rate         float64 `json:"rate"`
	RejectedRate float64 `json:"rejectedRate"`
	Queued       int     `json:"queued"`
	WaitMs       float64 `json:"waitMs"` // mean time queued before running
}

// apiPriority is one priority level: the seats its requests use out of its limit.
type apiPriority struct {
	Name         string  `json:"name"`
	Executing    float64 `json:"executing"`
	Limit        float64 `json:"limit"` // 0 for exempt
	Queued       int     `json:"queued"`
	RejectedRate float64 `json:"rejectedRate"`
}

// apiRequestRow is one verb on one resource ("" for non-resource paths such as /healthz).
type apiRequestRow struct {
	Verb      string  `json:"verb"`
	Resource  string  `json:"resource"`
	Rate      float64 `json:"rate"`
	ErrorRate float64 `json:"errorRate"`
	LatencyMs float64 `json:"latencyMs"`
}

type apiCount struct {
	Resource string `json:"resource"`
	Count    int    `json:"count"`
}

// apiQueued is a request waiting in an API Priority and Fairness queue right now.
type apiQueued struct {
	User       string `json:"user"`
	FlowSchema string `json:"flowSchema"`
	Priority   string `json:"priority"`
	Verb       string `json:"verb"`
	Path       string `json:"path"`
}

// KubeAPIHealth reports the Kubernetes API server's health and what loads it (os:admin):
// its readyz and livez checks, and from two /metrics scrapes a few seconds apart the
// request rate by client (flow schema), by verb and resource, the API Priority and
// Fairness queues and rejections, open watches and stored object counts, as JSON (see
// apiHealthReport). kubeServer: see KubePods.
func KubeAPIHealth(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoAPIHealth, readAPIHealth)
}

func readAPIHealth(ctx context.Context, k *kubeClient) (apiHealthReport, error) {
	return readAPIHealthOver(ctx, k, apiHealthWindow)
}

// readAPIHealthOver reads the probes, the version and the queued requests while the
// window between the two scrapes runs.
func readAPIHealthOver(ctx context.Context, k *kubeClient, window time.Duration) (apiHealthReport, error) {
	var (
		report  apiHealthReport
		queued  []apiQueued
		win     promWindow
		metrErr error
		wg      sync.WaitGroup
	)

	wg.Go(func() { report.Ready = readAPIProbe(ctx, k, "/readyz?verbose") })
	wg.Go(func() { report.Live = readAPIProbe(ctx, k, "/livez?verbose") })
	wg.Go(func() { report.Version = readAPIVersion(ctx, k) })
	wg.Go(func() { queued = readAPIQueued(ctx, k) })
	wg.Go(func() { win, metrErr = scrapeAPIWindow(ctx, k, window) })
	wg.Wait()

	if report.Ready.Error != "" && report.Live.Error != "" && metrErr != nil {
		return apiHealthReport{}, metrErr
	}

	if metrErr != nil {
		report.MetricsError = sectionError(metrErr)
	} else {
		report = fillAPIPressure(report, win)
	}

	report.QueuedRequests = nonNil(queued)
	report.Status = apiVerdict(report)

	return withEmptyAPILists(report), nil
}

// readAPIProbe reads a verbose /readyz or /livez. A failing server answers 500 with the
// same list, so the body is read whatever the status.
func readAPIProbe(ctx context.Context, k *kubeClient, path string) apiProbe {
	resp, data, err := k.send(ctx, http.MethodGet, path, "text/plain, */*", "", nil, nil)
	if err != nil {
		return apiProbe{Checks: []apiCheck{}, Error: kubeError(err).Error()}
	}

	checks := parseAPIChecks(string(data))
	if len(checks) == 0 && resp.StatusCode != http.StatusOK {
		return apiProbe{Checks: checks, Error: kubeStatusError(resp.StatusCode, data).Error()}
	}

	return apiProbe{OK: resp.StatusCode == http.StatusOK, Checks: checks}
}

// parseAPIChecks reads the "[+]name ok" and "[-]name failed: reason" lines of a verbose
// health answer.
func parseAPIChecks(text string) []apiCheck {
	checks := []apiCheck{}

	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(line, "[+]"):
			name, _, _ := strings.Cut(line[3:], " ")
			checks = append(checks, apiCheck{Name: name, OK: true})
		case strings.HasPrefix(line, "[-]"):
			name, reason, _ := strings.Cut(line[3:], " ")
			reason = strings.TrimPrefix(strings.TrimPrefix(reason, "failed"), ":")
			checks = append(checks, apiCheck{Name: name, Reason: strings.TrimSpace(reason)})
		}
	}

	return checks
}

func readAPIVersion(ctx context.Context, k *kubeClient) string {
	var version struct {
		GitVersion string `json:"gitVersion"`
	}

	if k.get(ctx, "/version", &version) != nil {
		return ""
	}

	return version.GitVersion
}

// scrapeAPIWindow scrapes /metrics twice, window apart. The window runs from the start of
// one request to the start of the other: the server takes each snapshot as it answers.
func scrapeAPIWindow(ctx context.Context, k *kubeClient, window time.Duration) (promWindow, error) {
	first := time.Now()

	before, err := scrapeAPIMetrics(ctx, k)
	if err != nil {
		return promWindow{}, err
	}

	select {
	case <-ctx.Done():
		return promWindow{}, ctx.Err()
	case <-time.After(time.Until(first.Add(window))):
	}

	second := time.Now()

	after, err := scrapeAPIMetrics(ctx, k)
	if err != nil {
		return promWindow{}, err
	}

	return newPromWindow(before, after, second.Sub(first), time.Now()), nil
}

func scrapeAPIMetrics(ctx context.Context, k *kubeClient) (promSamples, error) {
	var samples promSamples

	err := k.stream(ctx, "/metrics", "text/plain, */*", func(body io.Reader) error {
		var err error
		samples, err = parsePromText(body, apiMetricNames)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read API server metrics: %w", err)
	}

	return samples, nil
}

// Server errors make the server busy past both: a share of the requests, and a rate (one
// broken aggregated API or webhook answering 503 now and then is not load).
const (
	apiErrorShare = 0.01
	apiErrorFloor = 0.05
)

// apiVerdict sums the report up, worst finding first. Rates only count when measured over
// the window: averaged since the server started, an old burst would weigh forever.
func apiVerdict(r apiHealthReport) string {
	live := r.WindowSeconds > 0
	serverErrors := r.ErrorRate >= apiErrorFloor && r.ErrorRate >= apiErrorShare*r.RequestRate

	switch {
	case !r.Ready.OK || !r.Live.OK:
		return apiStatusUnhealthy
	case live && (r.RejectedRate > 0 || r.ThrottledRate > 0):
		return apiStatusThrottling
	case r.Queued > 0 || live && serverErrors:
		return apiStatusBusy
	default:
		return apiStatusOK
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}

	return s
}

// withEmptyAPILists turns nil lists into empty ones: the apps decode arrays, not null.
func withEmptyAPILists(r apiHealthReport) apiHealthReport {
	r.Clients = nonNil(r.Clients)
	r.Priorities = nonNil(r.Priorities)
	r.Requests = nonNil(r.Requests)
	r.WatchedKinds = nonNil(r.WatchedKinds)
	r.Objects = nonNil(r.Objects)
	r.QueuedRequests = nonNil(r.QueuedRequests)
	r.Ready.Checks = nonNil(r.Ready.Checks)
	r.Live.Checks = nonNil(r.Live.Checks)

	return r
}
