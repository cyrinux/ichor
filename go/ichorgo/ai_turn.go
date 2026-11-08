package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// One call to a provider may end with the model asking for tools instead of answering. These are
// the provider-neutral shapes of that exchange; ai_anthropic.go and ai_openai.go translate them.

const (
	// Bounds what a model's request may make the phone hold: more is a broken or hostile server.
	maxToolCallsPerTurn = 16
	maxToolArgsPerTurn  = 64 << 10

	answerTooManyToolCalls = "the model asked for too much at once"
)

// aiToolDef is one tool the model is told about.
type aiToolDef struct {
	name        string
	description string
	schema      map[string]any
}

// aiToolCall is one request from the model. Everything in it is untrusted.
type aiToolCall struct {
	id   string
	name string
	args string // JSON, as the model wrote it
}

// aiToolResult is what goes back to the model for one call.
type aiToolResult struct {
	text    string
	isError bool
}

// aiToolRound is one exchange already done: the provider's own form of the model's message, what
// it asked for, and the answers, in the same order.
type aiToolRound struct {
	assistant json.RawMessage
	calls     []aiToolCall
	results   []aiToolResult
}

// aiTurn is how one call ended: no calls means the answer is complete.
type aiTurn struct {
	assistant json.RawMessage
	calls     []aiToolCall
}

// streamTurn asks the model and calls emit with each piece of text as it arrives.
func streamTurn(ctx context.Context, req aiRequest, emit func(string)) (aiTurn, error) {
	switch req.provider.ID {
	case aiDevice:
		return aiTurn{}, streamDevice(ctx, req, emit)
	case aiAnthropic:
		return streamAnthropic(ctx, req, emit)
	default:
		if req.model == "" {
			model, err := firstServerModel(ctx, req)
			if err != nil {
				return aiTurn{}, err
			}

			req.model = model
		}

		return streamOpenAI(ctx, req, emit)
	}
}

// firstServerModel is the model a server with no model chosen answers with: the first of its
// /v1/models (newest first), as the settings' picker lists them.
func firstServerModel(ctx context.Context, req aiRequest) (string, error) {
	models, err := openAIModels(ctx, req)
	if err != nil {
		return "", err
	}

	if len(models) == 0 {
		return "", errors.New(req.provider.Name + " has no model yet: pull one first")
	}

	return models[0].ID, nil
}

// aiStatusError is a provider's refusal with its HTTP status, worded for the UI by Error.
type aiStatusError struct {
	error
	status  int
	message string
}

// toolsRefused tells a server that does not take the tools parameter from any other refusal.
func toolsRefused(err error) bool {
	var refused aiStatusError
	if !errors.As(err, &refused) || refused.status != 400 && refused.status != 422 {
		return false
	}

	message := strings.ToLower(refused.message)

	return strings.Contains(message, "tool") || strings.Contains(message, "function")
}

// toolCallBuffer gathers the fragments of one turn's calls, within the bounds above.
type toolCallBuffer struct {
	calls []aiToolCall
	size  int
}

// add starts a call and returns its position.
func (b *toolCallBuffer) add(id, name string) (int, error) {
	if len(b.calls) >= maxToolCallsPerTurn || len(name) > maxToolArgsPerTurn {
		return 0, providerError{errors.New(answerTooManyToolCalls)}
	}

	b.calls = append(b.calls, aiToolCall{id: id, name: name})

	return len(b.calls) - 1, nil
}

// appendArgs adds a fragment of a call's JSON arguments.
func (b *toolCallBuffer) appendArgs(i int, fragment string) error {
	b.size += len(fragment)
	if b.size > maxToolArgsPerTurn {
		return providerError{errors.New(answerTooManyToolCalls)}
	}

	b.calls[i].args += fragment

	return nil
}

// validArgsObject is args when it is a JSON object, else an empty one: a provider's API only
// takes an object back, whatever the model wrote.
func validArgsObject(args string) json.RawMessage {
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(args), &object) != nil || object == nil {
		return json.RawMessage("{}")
	}

	return json.RawMessage(args)
}
