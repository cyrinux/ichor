package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The panel assistant: a conversation with the model the user chose (see ai.go) to write
// PromQL panels for the Metrics screen. Every panel the model proposes is run against the
// cluster's own metrics source before it is shown; when the source turns the query down,
// the error goes back to the model, which gets a couple of chances to fix it. Nothing is
// sent before the user asks, and only what the user wrote, the source's metric names and
// the panel being edited go out: never the cluster's names or addresses (the assistant is
// not anonymized; screenshot mode still masks what it shows).

const (
	// Extra rounds after a proposed query failed.
	promChatMaxFixes = 2
	// The conversation kept: the older turns go, in pairs.
	promChatMaxTurns        = 20
	promChatMaxHistoryBytes = 128 << 10
	// The check runs the query over the last minutes with few points: it only asks
	// whether the source accepts it.
	promChatVerifyRange = 15 * time.Minute
	promChatVerifyStep  = 60

	promChatBusy = "a question is already being answered"
)

// PromChatListener receives the model's explanation while it is written (never the panel
// block itself), then the outcome, once (implemented in Kotlin/Swift).
type PromChatListener interface {
	// OnAnswer gets the whole explanation so far, not only what is new.
	OnAnswer(text string)
	// OnDone is called exactly once. panelJSON is the proposed panel
	// {title,query,unit,legend,verified,empty,attempts,notice}, "" when the answer proposes
	// none; errMessage is empty on success and when cancelled.
	OnDone(panelJSON, errMessage string)
}

// promSuggestion is a proposed panel and how its check went.
type promSuggestion struct {
	promPreset
	// Verified: the source accepted the query; Empty: it answered no series.
	Verified bool `json:"verified"`
	Empty    bool `json:"empty"`
	// Attempts is how many answers it took (1: the first query passed).
	Attempts int `json:"attempts"`
	// Notice says why the query was not checked, or its last error when the fixes ran out.
	Notice string `json:"notice,omitempty"`

	queryErr error
}

// PromChat is a conversation about one metrics source. It lives in memory only.
type PromChat struct {
	target     kubeTarget
	sourceJSON string
	system     string

	mu      sync.Mutex
	busy    bool
	history []chatMessage
}

// NewPromChat starts a conversation to write panels for the source sourceJSON (see
// NormalizePromSource) of the cluster. metricNamesJSON is what PromMetricNames returned,
// "" when it could not be read; currentPanelJSON is the panel being edited
// ({title,query,unit,legend}), "" for a new one. kubeServer is as for PromQueryRange.
func NewPromChat(configYAML, contextName, kubeServer, sourceJSON, metricNamesJSON, currentPanelJSON string) (c *PromChat, err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)

	if _, err := parsePromSource(sourceJSON); err != nil {
		return nil, err
	}

	var names []string

	_ = json.Unmarshal([]byte(metricNamesJSON), &names) //nolint:errcheck // unreadable names: the model uses the usual ones

	var current *promPreset

	if strings.TrimSpace(currentPanelJSON) != "" {
		var p promPreset
		if json.Unmarshal([]byte(currentPanelJSON), &p) == nil && strings.TrimSpace(p.Query) != "" {
			current = &p
		}
	}

	listed, dropped := promptMetricNames(names)

	return &PromChat{
		target:     kubeTarget{configYAML, contextName, kubeServer},
		sourceJSON: sourceJSON,
		system:     promChatSystemPrompt(listed, dropped, current),
	}, nil
}

// Reset forgets the conversation; the source and its metric names stay.
func (c *PromChat) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.history = nil
}

// Turns is how many messages the conversation holds.
func (c *PromChat) Turns() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.history)
}

// Ask sends the operator's message, with the conversation so far, and streams the answer
// to listener; a proposed panel is checked against the source first (see the file
// comment). provider is "anthropic" or "openai"; an empty model means the provider's
// default, an empty baseURL its own API; language is the app's language tag ("fr").
func (c *PromChat) Ask(provider, apiKey, model, baseURL, language, message string, listener PromChatListener) *DiagnosisRun {
	listener = maskedPromChatListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), aiAnswerTimeout)

	go func() {
		defer cancel()
		defer onPanic(func(errMessage string) { listener.OnDone("", errMessage) })

		panelJSON, errMessage := c.ask(ctx, provider, apiKey, model, baseURL, language, message, listener)
		listener.OnDone(panelJSON, strings.ToValidUTF8(errMessage, "�"))
	}()

	return &DiagnosisRun{cancel: cancel}
}

func (c *PromChat) ask(ctx context.Context, provider, apiKey, model, baseURL, language, message string, listener PromChatListener) (string, string) {
	if !c.acquire() {
		return "", promChatBusy
	}
	defer c.release()

	message = strings.TrimSpace(message)
	if message == "" {
		return "", "the message is empty"
	}

	req, err := newAIRequest(provider, apiKey, model, baseURL)
	if err != nil {
		return "", err.Error()
	}

	req.system = c.system + "\n\n" + promChatLanguageNote(language)

	msgs := append(c.snapshot(), chatMessage{Role: "user", Content: neutralizeChatTags(message)})

	for round := 0; ; round++ {
		req.messages = msgs

		answer, errMessage := c.stream(ctx, req, listener)
		if errMessage != "" || ctx.Err() != nil {
			// Nothing is kept: the model never answered this turn.
			return "", errMessage
		}

		msgs = append(msgs, chatMessage{Role: "assistant", Content: answer})

		panel, ok := parsePanelBlock(answer)
		if !ok {
			c.commit(msgs)

			return "", ""
		}

		s := c.verify(panel)
		s.Attempts = round + 1

		if s.queryErr == nil || round == promChatMaxFixes {
			c.commit(msgs)

			out, err := toJSON(s)
			if err != nil {
				return "", err.Error()
			}

			return out, ""
		}

		if ctx.Err() != nil {
			return "", ""
		}

		msgs = append(msgs, chatMessage{Role: "user", Content: queryErrorMessage(s.queryErr)})
	}
}

// stream asks the model once, showing the explanation as it is written (see Diagnosis.ask
// for the pacing), and returns the whole answer, or why there is none ("" when cancelled).
func (c *PromChat) stream(ctx context.Context, req aiRequest, listener PromChatListener) (string, string) {
	ctx, stop := context.WithCancel(ctx)
	defer stop()

	var (
		answer  strings.Builder
		shown   string
		shownAt time.Time
		tooLong bool
	)

	show := func() {
		if text := displayText(answer.String()); text != shown {
			listener.OnAnswer(text)
			shown = text
		}

		shownAt = time.Now()
	}

	err := streamAnswer(ctx, req, func(piece string) {
		if tooLong {
			return
		}

		answer.WriteString(piece)

		if answer.Len() > maxAnswerBytes {
			tooLong = true

			stop()

			return
		}

		if time.Since(shownAt) >= answerUpdateEvery {
			show()
		}
	})

	show()

	switch {
	case tooLong:
		return "", "the answer is far too long, it was stopped"
	case errors.Is(err, context.Canceled):
		return "", ""
	case err != nil:
		return "", err.Error()
	case strings.TrimSpace(answer.String()) == "":
		return "", "the model returned an empty answer"
	default:
		return answer.String(), ""
	}
}

// verify runs the proposed query against the source. A query the source turns down is
// for the model to fix (queryErr); a source that does not answer leaves the panel
// unchecked, with a notice.
func (c *PromChat) verify(panel promPreset) promSuggestion {
	s := promSuggestion{promPreset: panel}

	end := time.Now().Unix()

	grid, err := promRangeGrid(end-int64(promChatVerifyRange/time.Second), end, promChatVerifyStep)
	if err != nil {
		s.Notice = "not checked: " + err.Error()

		return s
	}

	params := url.Values{
		"start": {strconv.FormatInt(grid.start, 10)},
		"end":   {strconv.FormatInt(grid.end(), 10)},
		"step":  {strconv.FormatInt(grid.step, 10)},
	}

	res, err := promQuery(c.target, c.sourceJSON, "/api/v1/query_range", panel.Query, params, grid)

	switch {
	case isPromQueryError(err):
		s.Notice, s.queryErr = err.Error(), err
	case err != nil:
		s.Notice = "not checked: " + err.Error()
	default:
		s.Verified, s.Empty = true, res.Total == 0
	}

	return s
}

func (c *PromChat) acquire() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.busy {
		return false
	}

	c.busy = true

	return true
}

func (c *PromChat) release() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.busy = false
}

func (c *PromChat) snapshot() []chatMessage {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]chatMessage(nil), c.history...)
}

// commit keeps msgs as the conversation, dropping the oldest turns beyond the caps. A
// cut always lands on a plain user message, so a query error never opens the history
// without the answer it was about.
func (c *PromChat) commit(msgs []chatMessage) {
	size := 0
	for _, m := range msgs {
		size += len(m.Content)
	}

	for len(msgs) > promChatMaxTurns || size > promChatMaxHistoryBytes && len(msgs) > 2 {
		drop := 1
		for drop < len(msgs) && !(msgs[drop].Role == "user" && !strings.HasPrefix(msgs[drop].Content, "<query_error>")) {
			drop++
		}

		if drop >= len(msgs) {
			break
		}

		for _, m := range msgs[:drop] {
			size -= len(m.Content)
		}

		msgs = msgs[drop:]
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.history = msgs
}
