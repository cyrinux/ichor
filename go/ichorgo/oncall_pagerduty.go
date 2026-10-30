package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/internal/httpx"
)

// PagerDuty's REST API v2, https://developer.pagerduty.com/api-reference/.

const (
	pagerDutyAccept       = "application/vnd.pagerduty+json;version=2"
	pagerDutyMaxBody      = 8 << 20
	pagerDutyPageSize     = "100"
	pagerDutyMaxWithAlert = 30 // the newest incidents read in detail; others match on their title
	pagerDutyMaxDetail    = 64 << 10
)

// pagerDutyBases are the API addresses by region, a variable for the tests.
var pagerDutyBases = map[string]string{"us": "https://api.pagerduty.com", "eu": "https://api.eu.pagerduty.com"}

type pagerDuty struct {
	cfg    onCallConfig
	base   string
	client *http.Client
}

func newPagerDuty(c onCallConfig) *pagerDuty {
	return &pagerDuty{cfg: c, base: pagerDutyBases[c.Region], client: httpx.New(httpx.Opts{Timeout: onCallCallTimeout, FollowRedirects: true})}
}

// pdRef is the summary PagerDuty gives of a linked object.
type pdRef struct {
	Summary string `json:"summary"`
}

type pdIncident struct {
	ID          string `json:"id"`
	Number      int    `json:"incident_number"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Urgency     string `json:"urgency"`
	CreatedAt   string `json:"created_at"`
	HTMLURL     string `json:"html_url"`
	Service     pdRef  `json:"service"`
	Assignments []struct {
		Assignee pdRef `json:"assignee"`
	} `json:"assignments"`
	Acknowledgements []struct {
		Acknowledger pdRef `json:"acknowledger"`
	} `json:"acknowledgements"`
}

func (in pdIncident) toIncident() onCallIncident {
	out := onCallIncident{
		ID: in.ID, Number: in.Number, Title: in.Title, Status: in.Status, Urgency: in.Urgency,
		URL: in.HTMLURL, Service: in.Service.Summary, Assignees: []string{},
	}

	if at, err := time.Parse(time.RFC3339, in.CreatedAt); err == nil {
		out.CreatedAt = at.UnixMilli()
	}

	for _, a := range in.Assignments {
		out.Assignees = append(out.Assignees, a.Assignee.Summary)
	}

	if n := len(in.Acknowledgements); n > 0 {
		out.AcknowledgedBy = in.Acknowledgements[n-1].Acknowledger.Summary
	}

	return out
}

func (p *pagerDuty) incidents(ctx context.Context) ([]onCallIncident, error) {
	if len(p.cfg.ServiceIDs) == 0 {
		return nil, errors.New("add at least one PagerDuty service ID")
	}

	query := url.Values{
		"statuses[]":    {onCallStatusTriggered, onCallStatusAcknowledged},
		"service_ids[]": p.cfg.ServiceIDs,
		"limit":         {pagerDutyPageSize},
		"sort_by":       {"created_at:desc"},
	}

	var list struct {
		Incidents []pdIncident `json:"incidents"`
	}

	if err := p.getJSON(ctx, "/incidents?"+query.Encode(), &list); err != nil {
		return nil, err
	}

	out := make([]onCallIncident, 0, len(list.Incidents))

	for i, raw := range list.Incidents {
		in := raw.toIncident()
		if i < pagerDutyMaxWithAlert {
			in.Details = p.alertDetails(ctx, in.ID)
		}

		out = append(out, in)
	}

	return out, nil
}

// alertDetails is the text of the incident's alerts; an incident whose alerts cannot be read still
// lists, and matches on its title.
func (p *pagerDuty) alertDetails(ctx context.Context, id string) []string {
	var list struct {
		Alerts []struct {
			Body struct {
				Details json.RawMessage `json:"details"`
			} `json:"body"`
		} `json:"alerts"`
	}

	if p.getJSON(ctx, "/incidents/"+url.PathEscape(id)+"/alerts?limit="+pagerDutyPageSize, &list) != nil {
		return nil
	}

	var out []string
	for _, a := range list.Alerts {
		out = append(out, pagerDutyDetailTexts(a.Body.Details)...)
	}

	return out
}

// pagerDutyDetailTexts is the text in an alert's details: Alertmanager sends an object of strings
// (firing, resolved, num_firing…, or the receiver's own keys). The resolved list is left out: those
// alerts are over.
func pagerDutyDetailTexts(raw json.RawMessage) []string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []string{clipUTF8(text, pagerDutyMaxDetail)}
	}

	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}

	var out []string

	for key, value := range fields {
		if s, ok := value.(string); ok && key != "resolved" && !strings.HasPrefix(key, "num_") {
			out = append(out, clipUTF8(s, pagerDutyMaxDetail))
		}
	}

	return out
}

func (p *pagerDuty) act(ctx context.Context, id, verb string) (int, error) {
	status := onCallStatusAcknowledged
	if verb == onCallVerbResolve {
		status = "resolved"
	}

	body, err := json.Marshal(map[string]any{"incident": map[string]string{"type": "incident_reference", "status": status}})
	if err != nil {
		return 0, fmt.Errorf("encode the change: %w", err)
	}

	code, answer, err := p.send(ctx, http.MethodPut, "/incidents/"+url.PathEscape(id), body)
	if err != nil {
		return 0, fmt.Errorf("PagerDuty did not answer (it may have been applied: refresh before trying again): %w", err)
	}

	if code/100 != 2 {
		return 0, p.refusal(code, answer, true)
	}

	var updated struct {
		Incident pdIncident `json:"incident"`
	}

	// The change is made: a number that cannot be read only leaves the log with the ID.
	if json.Unmarshal(answer, &updated) != nil {
		return 0, nil
	}

	return updated.Incident.Number, nil
}

func (p *pagerDuty) identity(ctx context.Context) (onCallIdentity, error) {
	code, answer, err := p.send(ctx, http.MethodGet, "/users/me", nil)
	if err != nil {
		return onCallIdentity{}, fmt.Errorf("PagerDuty did not answer: %w", err)
	}

	// An account-level token has no user: PagerDuty refuses /users/me, /abilities tells it works.
	if code == http.StatusBadRequest || code == http.StatusForbidden || code == http.StatusNotFound {
		var abilities struct {
			Abilities []string `json:"abilities"`
		}

		if err := p.getJSON(ctx, "/abilities", &abilities); err != nil {
			return onCallIdentity{}, err
		}

		return onCallIdentity{}, nil
	}

	if code/100 != 2 {
		return onCallIdentity{}, p.refusal(code, answer, false)
	}

	var me struct {
		User struct {
			Name    string `json:"name"`
			HTMLURL string `json:"html_url"`
		} `json:"user"`
	}

	if json.Unmarshal(answer, &me) != nil {
		return onCallIdentity{}, errors.New("PagerDuty sent an unreadable answer")
	}

	who := onCallIdentity{User: me.User.Name}
	if u, err := url.Parse(me.User.HTMLURL); err == nil {
		who.Account, _, _ = strings.Cut(u.Hostname(), ".")
	}

	return who, nil
}

func (p *pagerDuty) getJSON(ctx context.Context, pathQuery string, into any) error {
	code, answer, err := p.send(ctx, http.MethodGet, pathQuery, nil)
	if err != nil {
		return fmt.Errorf("PagerDuty did not answer: %w", err)
	}

	if code/100 != 2 {
		return p.refusal(code, answer, false)
	}

	if json.Unmarshal(answer, into) != nil {
		return errors.New("PagerDuty sent an unreadable answer")
	}

	return nil
}

// send makes one request. The error it returns is the cause alone, not the *url.Error around it.
func (p *pagerDuty) send(ctx context.Context, method, pathQuery string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, p.base+pathQuery, bytes.NewReader(body))
	if err != nil {
		return 0, nil, errors.New("the request could not be built")
	}

	req.Header.Set("Authorization", "Token token="+p.cfg.Token)
	req.Header.Set("Accept", pagerDutyAccept)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if p.cfg.From != "" {
		req.Header.Set("From", p.cfg.From)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}

		return 0, nil, err
	}
	defer resp.Body.Close() //nolint:errcheck

	answer, err := io.ReadAll(io.LimitReader(resp.Body, pagerDutyMaxBody))
	if err != nil {
		return 0, nil, err
	}

	return resp.StatusCode, answer, nil
}

// refusal words a non-2xx answer. write: the request was a change to an incident.
func (p *pagerDuty) refusal(code int, answer []byte, write bool) error {
	var parsed struct {
		Error struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		} `json:"error"`
	}
	_ = json.Unmarshal(answer, &parsed) //nolint:errcheck // the status alone still says it

	reason := strings.TrimSpace(parsed.Error.Message + " " + strings.Join(parsed.Error.Errors, " "))
	lower := strings.ToLower(reason)

	switch {
	case code == http.StatusUnauthorized:
		return errors.New("the PagerDuty token was refused")
	case code == http.StatusForbidden && write:
		return errors.New("this token cannot change incidents: it needs the incidents write scope")
	case code == http.StatusForbidden:
		return errors.New("this token cannot read incidents: it needs the incidents read scope")
	case code == http.StatusBadRequest && write && (p.cfg.From == "" || strings.Contains(lower, "from") || strings.Contains(lower, "requester")):
		return errors.New("PagerDuty needs to know who acts: add your PagerDuty email")
	case code == http.StatusNotFound && write:
		return errors.New("PagerDuty no longer knows this incident: refresh")
	case code == http.StatusTooManyRequests:
		return errors.New("PagerDuty is limiting requests: try again in a minute")
	case reason != "":
		return fmt.Errorf("PagerDuty refused (%d): %s", code, clipUTF8(reason, onCallMaxTextLen))
	}

	return fmt.Errorf("PagerDuty refused (%d)", code)
}
