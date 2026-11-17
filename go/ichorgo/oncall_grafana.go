package ichorgo

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/internal/httpx"
)

// Grafana IRM's OnCall HTTP API, https://grafana.com/docs/oncall/latest/oncall-api-reference/:
// the base URL is the stack's (…/api/plugins/grafana-irm-app/resources/api/v1, or the legacy
// …/oncall/api/v1), pasted by the user, so both work. An alert group is the incident: new
// (ringing) or acknowledged; its last alert's payload is what Alertmanager sent, labels included.

const (
	grafanaMaxBody  = 8 << 20
	grafanaMaxPages = 10 // per state: 25 groups a page
)

// grafanaHTTPClient makes the client; the tests give their TLS server's.
var grafanaHTTPClient = func() *http.Client {
	return httpx.New(httpx.Opts{Timeout: onCallCallTimeout, FollowRedirects: true})
}

// grafanaStates are the alert group states listed, and the incident status each one becomes.
var grafanaStates = []struct{ query, status string }{
	{"new", onCallStatusTriggered},
	{"acknowledged", onCallStatusAcknowledged},
}

type grafanaIRM struct {
	cfg    onCallConfig
	base   *url.URL
	client *http.Client
}

func newGrafanaIRM(c onCallConfig) *grafanaIRM {
	base, _ := url.Parse(c.BaseURL) //nolint:errcheck // checked by parseOnCallConfig

	return &grafanaIRM{cfg: c, base: base, client: grafanaHTTPClient()}
}

// checkGrafanaBaseURL tells what is wrong with a pasted base URL, "" when it is usable.
func checkGrafanaBaseURL(raw string) string {
	u, err := url.Parse(raw)

	switch {
	case raw == "":
		return "add the Grafana IRM base URL"
	case err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil:
		return "the Grafana IRM base URL is not valid: paste https://<stack>.grafana.net/api/plugins/grafana-irm-app/resources/api/v1"
	case u.Scheme != "https":
		return "the Grafana IRM base URL must start with https://"
	}

	return ""
}

type grafanaGroup struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	CreatedAt  string `json:"created_at"`
	Permalinks struct {
		Web string `json:"web"`
	} `json:"permalinks"`
	LastAlert struct {
		Payload json.RawMessage `json:"payload"`
	} `json:"last_alert"`
}

func (g grafanaGroup) toIncident(status string) onCallIncident {
	out := onCallIncident{ID: g.ID, Title: g.Title, Status: status, URL: g.Permalinks.Web, Assignees: []string{}}

	if at, err := time.Parse(time.RFC3339Nano, g.CreatedAt); err == nil {
		out.CreatedAt = at.UnixMilli()
	}

	out.Details = grafanaPayloadDetails(g.LastAlert.Payload)

	return out
}

// grafanaPayloadDetails writes the firing alerts of an Alertmanager webhook payload as the
// "Labels:" blocks the matching reads (see onCallLabelBlocks).
func grafanaPayloadDetails(raw json.RawMessage) []string {
	var payload struct {
		Alerts []struct {
			Status string            `json:"status"`
			Labels map[string]string `json:"labels"`
		} `json:"alerts"`
	}

	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}

	var b strings.Builder

	for _, a := range payload.Alerts {
		if a.Status == "resolved" || len(a.Labels) == 0 {
			continue
		}

		b.WriteString("Labels:\n")

		names := make([]string, 0, len(a.Labels))
		for name := range a.Labels {
			names = append(names, name)
		}

		slices.Sort(names)

		for _, name := range names {
			b.WriteString(" - " + name + " = " + a.Labels[name] + "\n")
		}

		b.WriteString("Annotations:\n")
	}

	if b.Len() == 0 {
		return nil
	}

	return []string{clipUTF8(b.String(), pagerDutyMaxDetail)}
}

func (g *grafanaIRM) incidents(ctx context.Context) ([]onCallIncident, error) {
	var out []onCallIncident

	for _, state := range grafanaStates {
		next := g.endpoint("alert_groups/", url.Values{"state": {state.query}})

		for page := 0; next != nil && page < grafanaMaxPages; page++ {
			var list struct {
				Results []grafanaGroup `json:"results"`
				Next    *string        `json:"next"`
			}

			if err := g.getJSON(ctx, next, &list); err != nil {
				return nil, err
			}

			for _, group := range list.Results {
				if onCallID.MatchString(group.ID) {
					out = append(out, group.toIncident(state.status))
				}
			}

			var err error
			if next, err = g.nextPage(list.Next); err != nil {
				return nil, err
			}
		}
	}

	return out, nil
}

// nextPage is the next link, only on the stack's own host: the token goes with the request.
func (g *grafanaIRM) nextPage(next *string) (*url.URL, error) {
	if next == nil || *next == "" {
		return nil, nil
	}

	u, err := url.Parse(*next)
	if err != nil || u.Scheme != g.base.Scheme || u.Host != g.base.Host {
		return nil, errors.New("the next page Grafana IRM sent is on another address: it was not read")
	}

	return u, nil
}

func (g *grafanaIRM) act(ctx context.Context, id, verb string) (int, error) {
	code, answer, err := g.send(ctx, http.MethodPost, g.endpoint("alert_groups/"+url.PathEscape(id)+"/"+verb, nil))
	if err != nil {
		return 0, fmt.Errorf("no answer from Grafana IRM (it may have been applied: refresh before trying again): %w", err)
	}

	if code/100 != 2 {
		return 0, g.refusal(code, answer, true)
	}

	// Alert groups have no number: the log names the ID.
	return 0, nil
}

func (g *grafanaIRM) identity(ctx context.Context) (onCallIdentity, error) {
	var me struct {
		Username string `json:"username"`
		Email    string `json:"email"`
	}

	if err := g.getJSON(ctx, g.endpoint("users/current/", nil), &me); err != nil {
		return onCallIdentity{}, err
	}

	stack, _, _ := strings.Cut(g.base.Hostname(), ".")

	return onCallIdentity{User: cmp.Or(me.Username, me.Email), Account: stack}, nil
}

// endpoint is path under the base URL, with query.
func (g *grafanaIRM) endpoint(path string, query url.Values) *url.URL {
	u := *g.base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + path
	u.RawPath = ""
	u.RawQuery = query.Encode()

	return &u
}

func (g *grafanaIRM) getJSON(ctx context.Context, u *url.URL, into any) error {
	code, answer, err := g.send(ctx, http.MethodGet, u)
	if err != nil {
		return fmt.Errorf("no answer from Grafana IRM: %w", err)
	}

	if code/100 != 2 {
		return g.refusal(code, answer, false)
	}

	if json.Unmarshal(answer, into) != nil {
		return errors.New("the answer from Grafana IRM is unreadable: check the base URL")
	}

	return nil
}

// send makes one request. The error it returns is the cause alone, not the *url.Error around it.
func (g *grafanaIRM) send(ctx context.Context, method string, u *url.URL) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(nil))
	if err != nil {
		return 0, nil, errors.New("the request could not be built")
	}

	req.Header.Set("Authorization", g.cfg.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}

		return 0, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	answer, err := io.ReadAll(io.LimitReader(resp.Body, grafanaMaxBody))
	if err != nil {
		return 0, nil, err
	}

	return resp.StatusCode, answer, nil
}

// refusal words a non-2xx answer. write: the request was a change to an alert group.
func (g *grafanaIRM) refusal(code int, answer []byte, write bool) error {
	var parsed struct {
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(answer, &parsed) //nolint:errcheck // the status alone still says it

	switch {
	case code == http.StatusUnauthorized:
		return errors.New("the Grafana IRM token was refused")
	case code == http.StatusForbidden && write:
		return errors.New("this token has no permission to change alert groups")
	case code == http.StatusForbidden:
		return errors.New("this token has no permission to read alert groups")
	case code == http.StatusNotFound && write:
		return errors.New("this alert group no longer exists in Grafana IRM: refresh")
	case code == http.StatusNotFound:
		return errors.New("not found on Grafana IRM: check the base URL")
	case code == http.StatusTooManyRequests:
		return errors.New("too many requests for Grafana IRM: try again in a minute")
	case parsed.Detail != "":
		return fmt.Errorf("refused by Grafana IRM (%d): %s", code, clipUTF8(parsed.Detail, onCallMaxTextLen))
	}

	return fmt.Errorf("refused by Grafana IRM (%d)", code)
}
