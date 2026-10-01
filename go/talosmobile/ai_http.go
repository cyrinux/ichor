package talosmobile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

const (
	maxAIErrorBody = 64 << 10
	maxAIListBody  = 8 << 20
	maxSSELine     = 4 << 20
)

var (
	// aiHTTPClient is replaced in tests by a TLS test server's own client.
	aiHTTPClient = &http.Client{CheckRedirect: refuseRedirect}
	// A busy provider (429, 5xx) gets one more try after this long.
	aiRetryDelay = 2 * time.Second
)

// refuseRedirect makes a redirect the final answer (an error for aiDo). These APIs do not
// redirect, and following one would carry the API key to wherever it points, even over
// plain http.
func refuseRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

// aiDo sends one request to a provider and returns its 200 response, whose body the caller
// closes. Anything else is an error already worded for the UI.
func aiDo(ctx context.Context, ai aiRequest, method, url string, header map[string]string, body []byte) (*http.Response, error) {
	provider := ai.provider

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, aiError(ctx, provider, 0, "", err)
		}

		for k, v := range header {
			req.Header.Set(k, v)
		}

		resp, err := aiHTTPClient.Do(req)
		if err != nil {
			return nil, aiError(ctx, provider, 0, "", err)
		}

		if resp.StatusCode == http.StatusOK {
			return resp, nil
		}

		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxAIErrorBody)) //nolint:errcheck
		resp.Body.Close()                                               //nolint:errcheck

		busy := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError
		if attempt > 0 || !busy {
			return nil, aiError(ctx, provider, resp.StatusCode, ai.scrub(errorBodyMessage(raw)), nil)
		}

		select {
		case <-time.After(aiRetryDelay):
		case <-ctx.Done():
			return nil, aiError(ctx, provider, 0, "", ctx.Err())
		}
	}
}

// aiGetJSON decodes a provider's JSON answer to a GET into out.
func aiGetJSON(ctx context.Context, ai aiRequest, url string, header map[string]string, out any) error {
	provider := ai.provider

	resp, err := aiDo(ctx, ai, http.MethodGet, url, header, nil)
	if err != nil {
		return err
	}

	defer resp.Body.Close() //nolint:errcheck

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAIListBody)).Decode(out); err != nil {
		return aiError(ctx, provider, 0, "", err)
	}

	return nil
}

// errorBodyMessage extracts the message of an {"error":{"message":"..."}} body, the shape
// both providers use.
func errorBodyMessage(raw []byte) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if json.Unmarshal(raw, &body) != nil {
		return ""
	}

	return body.Error.Message
}

// readSSE calls onData with the data of each server-sent event, until the stream ends or
// onData returns an error. Both providers send one JSON document per data line.
func readSSE(r io.Reader, onData func(data []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxSSELine)

	for sc.Scan() {
		data, ok := bytes.CutPrefix(sc.Bytes(), []byte("data:"))
		if !ok {
			continue
		}

		if err := onData(bytes.TrimSpace(data)); err != nil {
			return err
		}
	}

	return sc.Err()
}

// providerError is a failure the provider reported inside the stream, already worded.
type providerError struct{ error }

// streamError words what ended a stream early: the provider's own report, or the
// connection (or the user) cutting it.
func streamError(ctx context.Context, provider aiProvider, err error) error {
	var reported providerError
	if errors.As(err, &reported) && ctx.Err() == nil {
		return reported.error
	}

	return aiError(ctx, provider, 0, "", err)
}
