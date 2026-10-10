package ichorgo

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	promMaxQuery = 4 << 10
	promMaxBody  = 16 << 20
	promMaxRange = 31 * 24 * time.Hour
	// An automatic step aims at about this many points across the chart.
	promTargetPoints = 250
	promMinStep      = 15
	// Under callTimeout, so the server's own timeout error comes back first.
	promServerTimeout = "18s"
)

// PromQueryRange runs a PromQL range query from start to end (unix seconds) every step
// seconds (0: about 250 points, at least 15 s) against the metrics source sourceJSON (see
// NormalizePromSource), its secret included:
// {resultType,times:[ms],series:[{name,labels,values:[v|null]}],warnings,truncated,total}.
// kubeServer is the API server address set for the cluster, for the proxy mode.
func PromQueryRange(configYAML, contextName, kubeServer, sourceJSON, query string, start, end, step int64) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	grid, err := promRangeGrid(start, end, step)
	if err != nil {
		return "", err
	}

	params := url.Values{"start": {strconv.FormatInt(grid.start, 10)}, "end": {strconv.FormatInt(grid.end(), 10)}, "step": {strconv.FormatInt(grid.step, 10)}}

	return promRun(kubeTarget{configYAML, contextName, kubeServer}, sourceJSON, "/api/v1/query_range", query, params, grid)
}

// NormalizePromSource checks a metrics source the user set and returns it cleaned up,
// without its secret (the app keeps that apart): {mode:"proxy",namespace,service,port,
// pathPrefix,tenant} or {mode:"url",url,auth:""|"bearer"|"basic",username,ca,
// insecureSkipVerify,tenant}.
func NormalizePromSource(sourceJSON string) (out string, err error) {
	// The result is what the app stores: only the error is masked.
	defer maskErr(&err)

	src, err := parsePromSource(sourceJSON)
	if err != nil {
		return "", err
	}

	src.Secret = ""

	return toJSON(src)
}

func promRun(target kubeTarget, sourceJSON, apiPath, query string, params url.Values, grid promGrid) (string, error) {
	res, err := promQuery(target, sourceJSON, apiPath, query, params, grid)
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

// promQuery runs query against the source; an error about the query itself is a
// promQueryError, so a caller can tell it from a source that does not answer.
func promQuery(target kubeTarget, sourceJSON, apiPath, query string, params url.Values, grid promGrid) (promResult, error) {
	query = strings.TrimSpace(query)

	switch {
	case query == "":
		return promResult{}, promQueryError{errors.New("the query is empty")}
	case len(query) > promMaxQuery:
		return promResult{}, promQueryError{errors.New("the query is longer than 4 KiB")}
	case strings.ContainsRune(query, 0):
		return promResult{}, promQueryError{errors.New("the query has a NUL character")}
	}

	src, err := parsePromSource(sourceJSON)
	if err != nil {
		return promResult{}, err
	}

	if isDemoContext(target.config, target.context) {
		return demoPromResult(query, grid), nil
	}

	params.Set("query", query)

	status, body, err := promGet(target, src, apiPath, params)
	if err != nil {
		return promResult{}, err
	}

	res, err := parsePromAnswer(status, body, grid)
	if err != nil {
		return promResult{}, fmt.Errorf("%s: %w", src.label(), err)
	}

	return res, nil
}

func promRangeGrid(start, end, step int64) (promGrid, error) {
	switch {
	case start <= 0 || end <= start:
		return promGrid{}, errors.New("the time range ends before it starts")
	case end-start > int64(promMaxRange/time.Second):
		return promGrid{}, errors.New("the time range is longer than 31 days")
	}

	if step <= 0 {
		step = max(promMinStep, (end-start+promTargetPoints-1)/promTargetPoints)
	}

	// Aligned on the step, successive refreshes share their points (and the server's cache);
	// the end rounds up so the newest data is in, which also makes at least two points.
	start -= start % step
	end += (step - end%step) % step

	n := (end-start)/step + 1
	if n > promMaxPoints {
		return promGrid{}, fmt.Errorf("a %d s step makes too many points: at most %d", step, promMaxPoints)
	}

	return promGrid{start: start, step: step, n: int(n)}, nil
}

func (g promGrid) end() int64 { return g.start + int64(g.n-1)*g.step }

// promGet reads the query API whatever the HTTP status: its JSON envelope explains a
// failed query. An answer from the API server itself (no pod behind the Service) is an
// error here.
func promGet(target kubeTarget, src promSource, apiPath string, params url.Values) (int, []byte, error) {
	// The server gives up first, with its own timeout error, rather than our deadline.
	params.Set("timeout", promServerTimeout)

	status, body, err := promSend(target, src, http.MethodGet, apiPath+"?"+params.Encode(), nil)
	if errors.Is(err, errPromNoAnswer) {
		return 0, nil, fmt.Errorf("%s: no answer within %s: narrow the query or shorten the range", src.label(), callTimeout)
	}

	return status, body, err
}

// errPromNoAnswer is a request to a source that got no answer within callTimeout.
var errPromNoAnswer = errors.New("no answer")

// promSend makes one request to the source (pathQuery: the API path and query, below the
// path prefix) and returns the answer whatever its HTTP status, for the caller to read.
// body is sent as JSON, nil for none. An answer from the API server itself (no pod behind
// the Service) is an error here; a request left without an answer is errPromNoAnswer.
func promSend(target kubeTarget, src promSource, method, pathQuery string, body []byte) (int, []byte, error) {
	if src.Mode == promModeURL {
		ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
		defer cancel()

		return promHTTPDo(ctx, src, method, src.URL+pathQuery, promHeader(src), body)
	}

	a, err := withKube(target, func(ctx context.Context, k *kubeClient) (promProxyAnswer, error) {
		return promProxyCall(ctx, k, src, method, pathQuery, body)
	})
	if err != nil {
		return 0, nil, err
	}

	return a.result(src)
}

// promSendWith is promSend through a client already open (the checkup's), for a source in
// proxy mode.
func promSendWith(ctx context.Context, k *kubeClient, src promSource, method, pathQuery string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	a, err := promProxyCall(ctx, k, src, method, pathQuery, nil)
	if err != nil {
		return 0, nil, kubeError(err)
	}

	return a.result(src)
}

// promHeader is the headers every request to the source carries.
func promHeader(src promSource) map[string]string {
	header := map[string]string{}
	if src.Tenant != "" {
		header["X-Scope-OrgID"] = src.Tenant
	}

	return header
}

// promProxyAnswer is a service proxy answer, before result reads it.
type promProxyAnswer struct {
	status  int
	body    []byte
	kube    bool // the API server's own Status
	timeout bool
}

// promProxyCall makes one request to the source through the API server's service proxy.
func promProxyCall(ctx context.Context, k *kubeClient, src promSource, method, pathQuery string, body []byte) (promProxyAnswer, error) {
	path := serviceProxyPath(src.Namespace, fmt.Sprintf("%s:%d", url.PathEscape(src.Service), src.Port), src.PathPrefix+pathQuery)

	contentType := ""
	if body != nil {
		contentType = "application/json"
	}

	resp, data, err := k.send(ctx, method, path, "application/json", contentType, body, promHeader(src))

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		// A slow backend, not a dead client: keep it cached.
		return promProxyAnswer{timeout: true}, nil
	case err != nil:
		return promProxyAnswer{}, err
	case resp.StatusCode/100 == 2 || !isKubeStatus(data):
		return promProxyAnswer{status: resp.StatusCode, body: data}, nil
	case resp.StatusCode == http.StatusUnauthorized:
		// Expired credentials: the client is dropped as for any other call.
		return promProxyAnswer{}, kubeStatusError(resp.StatusCode, data)
	default:
		return promProxyAnswer{status: resp.StatusCode, body: data, kube: true}, nil
	}
}

// result is the answer as promSend returns it: the API server's own an error.
func (a promProxyAnswer) result(src promSource) (int, []byte, error) {
	switch {
	case a.timeout:
		return 0, nil, errPromNoAnswer
	case a.kube:
		return 0, nil, promProxyError(src, a.status, a.body)
	case len(a.body) > promMaxBody:
		return 0, nil, errPromTooLarge
	}

	return a.status, a.body, nil
}

var errPromTooLarge = errors.New("the answer is larger than 16 MiB: narrow the query (sum by, topk)")

// promProxyError explains the API server's own answer to a service proxy request.
func promProxyError(src promSource, status int, body []byte) error {
	switch status {
	case http.StatusServiceUnavailable, http.StatusBadGateway:
		var st struct {
			Message string `json:"message"`
		}

		_ = json.Unmarshal(body, &st)

		return fmt.Errorf("%s: no answer from the Service on port %d (no ready pod, or not its HTTP port): %s", src.label(), src.Port, clipUTF8(st.Message, 200))
	case http.StatusNotFound:
		return fmt.Errorf("%s: no such Service", src.label())
	default:
		return fmt.Errorf("%s: %w", src.label(), kubeStatusError(status, body))
	}
}

// promHTTPDo makes one request to a source in URL mode, body sent as JSON (nil for none).
func promHTTPDo(ctx context.Context, src promSource, method, target string, header map[string]string, body []byte) (int, []byte, error) {
	client, err := promHTTPClient(src)
	if err != nil {
		return 0, nil, err
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	for name, value := range header {
		req.Header.Set(name, value)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ichor")

	switch src.Auth {
	case promAuthBearer:
		req.Header.Set("Authorization", "Bearer "+src.Secret)
	case promAuthBasic:
		req.SetBasicAuth(src.Username, src.Secret)
	}

	resp, err := client.Do(req)
	if errors.Is(err, context.DeadlineExceeded) {
		return 0, nil, errPromNoAnswer
	}

	if err != nil {
		return 0, nil, fmt.Errorf("%s: %s", src.URL, kubeTransportError(err))
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		// Not followed: it would carry the credentials wherever it points.
		return 0, nil, fmt.Errorf("%s redirects to %q: set that address instead", src.URL, clipUTF8(resp.Header.Get("Location"), 200))
	}

	answer, err := io.ReadAll(io.LimitReader(resp.Body, promMaxBody+1))
	if err != nil {
		return 0, nil, fmt.Errorf("%s: %s", src.URL, kubeTransportError(err))
	}

	if len(answer) > promMaxBody {
		return 0, nil, errPromTooLarge
	}

	return resp.StatusCode, answer, nil
}

// promHTTPClient trusts the system authorities plus the source's own, refusing redirects.
func promHTTPClient(src promSource) (*http.Client, error) {
	var roots *x509.CertPool

	if src.CA != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			pool = x509.NewCertPool()
		}

		if !pool.AppendCertsFromPEM([]byte(src.CA)) {
			return nil, errors.New("the certificate authority is not a PEM certificate")
		}

		roots = pool
	}

	return newHTTPClient(httpClientOpts{tls: baseTLS(roots, src.InsecureSkipVerify)}), nil
}
