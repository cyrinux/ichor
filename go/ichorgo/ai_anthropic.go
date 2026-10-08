package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

const (
	anthropicVersion = "2023-06-01"

	// Room for the model's thinking plus an answer; every current Claude model accepts it.
	anthropicMaxTokens = 32000

	// Lets the API re-run a request its safety classifiers declined on the model Anthropic
	// recommends for that kind of decline, in the same call.
	anthropicFallbackBeta = "server-side-fallback-2026-07-01"
)

// anthropicRequest is the body of a Messages API call. Thinking is left to the model's
// default (adaptive on current models), which every model accepts.
type anthropicRequest struct {
	Model        string                 `json:"model"`
	MaxTokens    int                    `json:"max_tokens"`
	Stream       bool                   `json:"stream"`
	System       string                 `json:"system"`
	Messages     []chatMessage          `json:"messages"`
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
	Fallbacks    string                 `json:"fallbacks,omitempty"`
}

type anthropicOutputConfig struct {
	Effort string `json:"effort"`
}

// anthropicEvent is the part of a stream event the answer is built from.
type anthropicEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// anthropicURL is base + "/v1/" + path; a base that already ends in /v1 is taken as it is.
func anthropicURL(req aiRequest, path string) string {
	base := anthropicAPIURL
	if req.baseURL != "" {
		base = req.baseURL
	}

	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}

	return base + "/" + path
}

func anthropicHeader(req aiRequest) map[string]string {
	header := map[string]string{"anthropic-version": anthropicVersion, "content-type": "application/json"}
	if req.apiKey != "" {
		header["x-api-key"] = req.apiKey
	}

	return header
}

// streamAnthropic asks a Claude model through the Messages API. The model is free text, so
// the tuning below only goes to the models documented to take it.
func streamAnthropic(ctx context.Context, req aiRequest, emit func(string)) error {
	body := anthropicRequest{
		Model:     req.model,
		MaxTokens: anthropicMaxTokens,
		Stream:    true,
		System:    req.system,
		Messages:  req.turns(),
	}

	// Claude Opus 5.5 defaults to medium effort; a diagnosis is worth the usual high.
	if strings.HasPrefix(req.model, "claude-opus-5-5") {
		body.OutputConfig = &anthropicOutputConfig{Effort: "high"}
	}

	header := anthropicHeader(req)

	if req.baseURL == "" && anthropicFallbackModel(req.model) {
		header["anthropic-beta"] = anthropicFallbackBeta
		body.Fallbacks = "default"
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}

	resp, err := aiDo(ctx, req, http.MethodPost, anthropicURL(req, "messages"), header, raw)
	if err != nil {
		return err
	}

	defer resp.Body.Close() //nolint:errcheck

	var stop string

	err = readSSE(resp.Body, func(data []byte) error {
		var ev anthropicEvent
		if json.Unmarshal(data, &ev) != nil {
			return nil
		}

		switch ev.Type {
		case "content_block_delta":
			// Thinking and the fallback marker are other kinds of blocks: only text is the answer.
			if ev.Delta.Type == "text_delta" {
				emit(ev.Delta.Text)
			}
		case "message_delta":
			stop = ev.Delta.StopReason
		case "error":
			return providerError{aiError(ctx, req.provider, http.StatusInternalServerError, req.scrub(ev.Error.Message), nil)}
		}

		return nil
	})

	switch {
	case err != nil:
		return streamError(ctx, req.provider, err)
	case stop == "refusal":
		return errors.New(answerDeclined)
	case stop == "":
		return errors.New(answerIncomplete)
	case stop == "max_tokens":
		emit(answerCutNotice)
	}

	return nil
}

// anthropicFallbackModel tells whether model takes the server-side refusal fallback: the
// models that run safety classifiers. Others would reject the parameter.
func anthropicFallbackModel(model string) bool {
	for _, prefix := range []string{"claude-opus-5", "claude-fable-5", "claude-sonnet-5-5"} {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}

	return false
}

// anthropicModels lists the models, which the API returns newest first.
func anthropicModels(ctx context.Context, req aiRequest) ([]aiModel, error) {
	var list struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}

	if err := aiGetJSON(ctx, req, anthropicURL(req, "models?limit=1000"), anthropicHeader(req), &list); err != nil {
		return nil, err
	}

	out := make([]aiModel, 0, len(list.Data))
	for _, m := range list.Data {
		out = append(out, aiModel{ID: m.ID, Name: m.DisplayName})
	}

	return out, nil
}
