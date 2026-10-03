package ichorgo

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
)

// openAIRequest is the body of a Chat Completions call: the one OpenAI API that compatible
// servers (local models, gateways) implement too. No output limit is set, so each model
// uses its own.
type openAIRequest struct {
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Messages []chatMessage `json:"messages"`
}

// openAIChunk is the part of a stream chunk the answer is built from.
type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

type openAIModel struct {
	ID      string `json:"id"`
	Created int64  `json:"created"` // unix seconds
}

func openAIURL(req aiRequest, path string) string {
	if req.baseURL != "" {
		return req.baseURL + "/" + path
	}

	return openAIAPIURL + "/" + path
}

func openAIHeader(req aiRequest) map[string]string {
	header := map[string]string{"content-type": "application/json"}
	if req.apiKey != "" {
		header["authorization"] = "Bearer " + req.apiKey
	}

	return header
}

func streamOpenAI(ctx context.Context, req aiRequest, emit func(string)) error {
	raw, err := json.Marshal(openAIRequest{
		Model:    req.model,
		Stream:   true,
		Messages: []chatMessage{{Role: "system", Content: req.system}, {Role: "user", Content: req.user}},
	})
	if err != nil {
		return err
	}

	resp, err := aiDo(ctx, req, http.MethodPost, openAIURL(req, "chat/completions"), openAIHeader(req), raw)
	if err != nil {
		return err
	}

	defer resp.Body.Close() //nolint:errcheck

	var (
		finish  string
		refused bool
	)

	err = readSSE(resp.Body, func(data []byte) error {
		var chunk openAIChunk
		if bytes.Equal(data, []byte("[DONE]")) || json.Unmarshal(data, &chunk) != nil {
			return nil
		}

		if chunk.Error.Message != "" {
			return providerError{aiError(ctx, req.provider, http.StatusInternalServerError, req.scrub(chunk.Error.Message), nil)}
		}

		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				emit(choice.Delta.Content)
			}

			refused = refused || choice.Delta.Refusal != ""

			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
		}

		return nil
	})

	switch {
	case err != nil:
		return streamError(ctx, req.provider, err)
	case refused || finish == "content_filter":
		return errors.New(answerDeclined)
	case finish == "":
		return errors.New(answerIncomplete)
	case finish == "length":
		emit(answerCutNotice)
	}

	return nil
}

// openAIModels lists the models, newest first.
func openAIModels(ctx context.Context, req aiRequest) ([]aiModel, error) {
	var list struct {
		Data []openAIModel `json:"data"`
	}

	if err := aiGetJSON(ctx, req, openAIURL(req, "models"), openAIHeader(req), &list); err != nil {
		return nil, err
	}

	models := list.Data

	// OpenAI lists every kind of model; another server's list is taken as it is.
	if req.baseURL == "" {
		models = slices.DeleteFunc(models, func(m openAIModel) bool { return !openAIChatModel(m.ID) })
	}

	slices.SortStableFunc(models, func(a, b openAIModel) int {
		return cmp.Or(cmp.Compare(b.Created, a.Created), cmp.Compare(a.ID, b.ID))
	})

	out := make([]aiModel, 0, len(models))
	for _, m := range models {
		out = append(out, aiModel{ID: m.ID, Name: m.ID})
	}

	return out, nil
}

// Model families that answer text through Chat Completions, and the specialised variants
// of them that do not (speech, images, embeddings...).
var (
	openAIChatPrefixes = []string{"gpt-", "chatgpt-", "o1", "o3", "o4"}
	openAINonChatWords = []string{
		"audio", "realtime", "live", "transcribe", "tts", "image", "embedding", "moderation", "search", "instruct", "codex",
	}
)

func openAIChatModel(id string) bool {
	if !slices.ContainsFunc(openAIChatPrefixes, func(p string) bool { return strings.HasPrefix(id, p) }) {
		return false
	}

	return !slices.ContainsFunc(openAINonChatWords, func(w string) bool { return strings.Contains(id, w) })
}
