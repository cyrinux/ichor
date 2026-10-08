package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// whiskerNode is the one "agent" of a Calico flow view: Whisker streams for every node.
	whiskerNode = "whisker"
	// whiskerBackfill is how far back the first stream starts, in seconds.
	whiskerBackfill = 60
	// whiskerRetry is the wait before streaming again after a failure.
	whiskerRetry = 5 * time.Second
	// whiskerErrorBody caps what is read of an error answer.
	whiskerErrorBody = 64 << 10
)

// followWhisker is followHubble for Calico: one stream from Whisker instead of one per agent.
func followWhisker(ctx context.Context, k *kubeClient, status ciliumStatus, filter hubbleFilter, emit func(hubbleSnapshot)) error {
	source := status
	source.Agents = []ciliumAgent{{Node: whiskerNode, Pod: status.Whisker.Pod, Ready: true}}
	agg := newHubbleAgg(source)

	var wg sync.WaitGroup

	wg.Go(func() { refreshPolicies(ctx, k, agg) })
	wg.Go(func() { superviseWhisker(ctx, k, *status.Whisker, filter, agg) })

	emitSnapshots(ctx, agg, emit)
	wg.Wait()

	return ctx.Err()
}

// superviseWhisker streams until ctx ends: again at once when Goldmane closes a stream
// (from the last flow seen), after whiskerRetry when one fails. When the API server got no
// answer from the Service, the next try takes the other scheme (a TLS port addressed plain,
// or the reverse), then back: a pod not ready yet answers on one of them once it is.
func superviseWhisker(ctx context.Context, k *kubeClient, w whiskerInfo, filter hubbleFilter, agg *hubbleAgg) {
	for ctx.Err() == nil {
		agg.setNode(whiskerNode, hubbleNodeConnecting, "")

		started := time.Now()
		noAnswer, err := streamWhisker(ctx, k, w, filter, agg)

		if ctx.Err() != nil {
			return
		}

		if noAnswer {
			w.TLS = !w.TLS
		}

		if err != nil {
			agg.setNode(whiskerNode, hubbleNodeError, err.Error())
		} else if time.Since(started) > time.Second {
			// A stream that lasted simply ended: follow again right away.
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(whiskerRetry):
		}
	}
}

// streamWhisker opens one stream and feeds agg until it ends: nil when Goldmane closed it,
// else the failure, worded; noAnswer tells the API server got nothing from the Service.
func streamWhisker(ctx context.Context, k *kubeClient, w whiskerInfo, filter hubbleFilter, agg *hubbleAgg) (noAnswer bool, err error) {
	path := whiskerFlowsPath(w, filter, agg.lastFlow(whiskerNode))

	resp, err := k.request(ctx, http.MethodGet, path, "text/event-stream", "", nil, nil)
	if err != nil {
		return false, kubeError(err)
	}

	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, whiskerErrorBody)) //nolint:errcheck // the status is what matters

		if isKubeStatus(body) {
			return resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable, whiskerProxyError(w, resp.StatusCode, body)
		}

		return false, whiskerBackendError(resp.StatusCode, body)
	}

	agg.setNode(whiskerNode, hubbleNodeLive, "")

	err = readWhiskerSSE(resp.Body, func(data []byte) {
		if f, ok := parseWhiskerFlow(data); ok && filter.keeps(f) {
			agg.add(whiskerNode, hubbleLine{flow: &f})
		}
	})

	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	return false, err
}

// whiskerFlowsPath is the proxy path of the stream: from since (unix ms: the bucket of the
// last flow seen, again, so a stream cut while Goldmane sent it misses none of it) or
// whiskerBackfill back, with the server-side filters.
func whiskerFlowsPath(w whiskerInfo, filter hubbleFilter, since int64) string {
	service := whiskerService + ":" + strconv.Itoa(w.Port)
	if w.TLS {
		service = "https:" + service
	}

	q := url.Values{}
	q.Set("watch", "true")

	if since > 0 {
		q.Set("startTimeGte", strconv.FormatInt(since/1000, 10))
	} else {
		q.Set("startTimeGte", strconv.Itoa(-whiskerBackfill))
	}

	if f := filter.whiskerFilters(); f != "" {
		q.Set("filters", f)
	}

	return serviceProxyPath(w.Namespace, service, "/whisker-backend/flows?"+q.Encode())
}

// whiskerFilters is what Whisker filters itself: the denied flows alone for a drops-only
// view. Namespace and pod stay on the app's side: Whisker ANDs its lists, the view wants a
// peer on either side.
func (f hubbleFilter) whiskerFilters() string {
	if !f.dropsOnly {
		return ""
	}

	return `{"actions":["Deny"]}`
}

// whiskerProxyError explains the API server's own answer to the proxy request.
func whiskerProxyError(w whiskerInfo, status int, body []byte) error {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return fmt.Errorf("the API server got no answer from the %s Service on port %d in %s: no ready whisker pod, not its HTTP port, or a policy (allow-tigera.whisker) refusing the API server",
			whiskerService, w.Port, w.Namespace)
	case http.StatusNotFound:
		return fmt.Errorf("no %s Service in %s: Whisker is not installed anymore", whiskerService, w.Namespace)
	default:
		return kubeStatusError(status, body)
	}
}

// whiskerBackendError reads Whisker's own report ({"error":"…"}).
func whiskerBackendError(status int, body []byte) error {
	var report struct {
		Error string `json:"error"`
	}

	msg := strings.TrimSpace(string(body))
	if json.Unmarshal(body, &report) == nil && report.Error != "" {
		msg = report.Error
	}

	if msg == "" {
		msg = http.StatusText(status)
	}

	return fmt.Errorf("Whisker answered HTTP %d: %s", status, msg)
}
