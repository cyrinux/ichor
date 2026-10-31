package ichorgo

import (
	"encoding/json"
	"strings"
)

// The Messages API's side of a tool exchange. The model's message goes back as it came, thinking
// included: the API refuses a tool_result whose turn lost its thinking blocks.

type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
}

type anthropicToolResult struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error,omitempty"`
}

type anthropicEventBlock struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Data string `json:"data"` // redacted_thinking
}

type anthropicEventDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	StopReason  string `json:"stop_reason"`
	PartialJSON string `json:"partial_json"`
	Thinking    string `json:"thinking"`
	Signature   string `json:"signature"`
}

func anthropicTools(tools []aiToolDef) []anthropicTool {
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, anthropicTool{Name: t.name, Description: t.description, InputSchema: t.schema})
	}

	return out
}

func anthropicMessages(req aiRequest) []anthropicMessage {
	out := []anthropicMessage{}
	for _, m := range req.turns() {
		out = append(out, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	for _, round := range req.rounds {
		results := make([]anthropicToolResult, 0, len(round.calls))
		for i, call := range round.calls {
			results = append(results, anthropicToolResult{
				Type: "tool_result", ToolUseID: call.id, Content: round.results[i].text, IsError: round.results[i].isError,
			})
		}

		out = append(out,
			anthropicMessage{Role: "assistant", Content: round.assistant},
			anthropicMessage{Role: "user", Content: results},
		)
	}

	return out
}

// anthropicBlock is one content block being received. Blocks of a kind not listed here (the
// fallback marker) are not sent back.
type anthropicBlock struct {
	kind      string
	id        string
	name      string
	data      string
	text      strings.Builder
	signature strings.Builder
	call      int
}

// anthropicBlocks rebuilds the model's message from the stream; off when no tools were offered.
type anthropicBlocks struct {
	on      bool
	order   []*anthropicBlock
	byIndex map[int]*anthropicBlock
	buffer  toolCallBuffer
}

func newAnthropicBlocks(on bool) *anthropicBlocks {
	return &anthropicBlocks{on: on, byIndex: map[int]*anthropicBlock{}}
}

func (b *anthropicBlocks) start(index int, block anthropicEventBlock) error {
	if !b.on {
		return nil
	}

	kept := &anthropicBlock{kind: block.Type, id: block.ID, name: block.Name, data: block.Data}

	switch block.Type {
	case "tool_use":
		call, err := b.buffer.add(block.ID, block.Name)
		if err != nil {
			return err
		}

		kept.call = call
	case "text", "thinking", "redacted_thinking":
	default:
		return nil
	}

	b.order = append(b.order, kept)
	b.byIndex[index] = kept

	return nil
}

func (b *anthropicBlocks) delta(index int, delta anthropicEventDelta) error {
	block := b.byIndex[index]
	if !b.on || block == nil {
		return nil
	}

	switch delta.Type {
	case "text_delta":
		block.text.WriteString(delta.Text)
	case "thinking_delta":
		block.text.WriteString(delta.Thinking)
	case "signature_delta":
		block.signature.WriteString(delta.Signature)
	case "input_json_delta":
		if block.kind == "tool_use" {
			return b.buffer.appendArgs(block.call, delta.PartialJSON)
		}
	}

	return nil
}

// turn is the model's message in the API's form, with the calls it holds.
func (b *anthropicBlocks) turn() (aiTurn, error) {
	content := make([]map[string]any, 0, len(b.order))

	for _, block := range b.order {
		switch block.kind {
		case "text":
			// The API refuses an empty text block.
			if block.text.Len() > 0 {
				content = append(content, map[string]any{"type": "text", "text": block.text.String()})
			}
		case "thinking":
			content = append(content, map[string]any{"type": "thinking", "thinking": block.text.String(), "signature": block.signature.String()})
		case "redacted_thinking":
			content = append(content, map[string]any{"type": "redacted_thinking", "data": block.data})
		case "tool_use":
			content = append(content, map[string]any{
				"type": "tool_use", "id": block.id, "name": block.name, "input": validArgsObject(b.buffer.calls[block.call].args),
			})
		}
	}

	raw, err := json.Marshal(content)
	if err != nil {
		return aiTurn{}, err
	}

	return aiTurn{assistant: raw, calls: b.buffer.calls}, nil
}
