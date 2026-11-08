package ichorgo

import (
	"encoding/json"
	"fmt"
)

// The Chat Completions side of a tool exchange.

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// openAIToolCallDelta is a fragment of a call: the index ties it to the first one, which alone
// has the id and the name.
type openAIToolCallDelta struct {
	Index int `json:"index"`
	openAIToolCall
}

func openAITools(tools []aiToolDef) []openAITool {
	out := make([]openAITool, 0, len(tools))
	for _, t := range tools {
		out = append(out, openAITool{"function", openAIFunction{t.name, t.description, t.schema}})
	}

	return out
}

func openAIMessages(req aiRequest) []openAIMessage {
	out := []openAIMessage{{Role: "system", Content: req.system}}
	for _, m := range req.turns() {
		out = append(out, openAIMessage{Role: m.Role, Content: m.Content})
	}

	for _, round := range req.rounds {
		var assistant openAIMessage
		if json.Unmarshal(round.assistant, &assistant) == nil {
			out = append(out, assistant)
		}

		for i, call := range round.calls {
			out = append(out, openAIMessage{Role: "tool", ToolCallID: call.id, Content: round.results[i].text})
		}
	}

	return out
}

// openAICalls gathers the fragments of a turn's calls.
type openAICalls struct {
	buffer  toolCallBuffer
	byIndex map[int]int
}

func newOpenAICalls() *openAICalls { return &openAICalls{byIndex: map[int]int{}} }

func (c *openAICalls) add(deltas []openAIToolCallDelta) error {
	for _, d := range deltas {
		call, known := c.byIndex[d.Index]
		if !known {
			var err error

			call, err = c.buffer.add(d.ID, d.Function.Name)
			if err != nil {
				return err
			}

			c.byIndex[d.Index] = call
			// A server that sends no id still needs its result tied to the call.
			if d.ID == "" {
				c.buffer.calls[call].id = fmt.Sprintf("call_%d", call)
			}
		}

		if err := c.buffer.appendArgs(call, d.Function.Arguments); err != nil {
			return err
		}
	}

	return nil
}

// turn is the model's message in the API's form, with the calls it holds.
func (c *openAICalls) turn(text string) (aiTurn, error) {
	if len(c.buffer.calls) == 0 {
		return aiTurn{}, nil
	}

	message := openAIMessage{Role: "assistant", Content: text}

	for _, call := range c.buffer.calls {
		wire := openAIToolCall{ID: call.id, Type: "function"}
		wire.Function.Name, wire.Function.Arguments = call.name, string(validArgsObject(call.args))
		message.ToolCalls = append(message.ToolCalls, wire)
	}

	raw, err := json.Marshal(message)
	if err != nil {
		return aiTurn{}, err
	}

	return aiTurn{assistant: raw, calls: c.buffer.calls}, nil
}
