package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// The "device" provider: a model file on the phone, run by an engine the app embeds
// (llama.cpp, wrapped in Kotlin and Swift as LocalModel). It is chosen and downloaded by
// the user like any other provider; until then nothing is loaded and nothing is sent
// anywhere. Everything around the answer (the report shown first, the mask and its
// reversal, the size caps, the PromQL check) is the same code as for the HTTP providers:
// only streamAnswer routes differently.

const (
	aiDevice = "device"

	deviceUnavailable = "this build of the app has no on-device model"
	deviceTimedOut    = "the model on this phone took too long to answer"
)

// LocalModel runs a model file on the phone (implemented in Kotlin/Swift).
type LocalModel interface {
	// Generate answers the conversation with the model modelID (a catalog id, or "file:"
	// and the name of a file the user imported), blocking until the answer is complete.
	// optionsJSON is {contextTokens, answerTokens, temperature, assistantPrefix};
	// messagesJSON is [{role, content}] after the system prompt, the last one the user's.
	// Each piece of the answer goes to sink, one call at a time, in order. It returns ""
	// on success or when cancelled, "not-downloaded", "too-long <prompt tokens>
	// <context tokens>", "out-of-memory", "no-engine", or a message.
	Generate(modelID, optionsJSON, system, messagesJSON string, sink LocalModelSink) string
	// Cancel stops the running Generate as soon as possible; it returns at once.
	Cancel()
}

// LocalModelSink receives the answer while the model writes it.
type LocalModelSink interface {
	OnPiece(text string)
}

var (
	localModelMu sync.Mutex
	localModel   LocalModel
	// One answer at a time: the phone has room for one model, and a Cancel must not
	// reach the next run.
	localRunMu sync.Mutex
)

// SetLocalModel registers the app's engine, once at start; nil when the build has none.
func SetLocalModel(m LocalModel) {
	localModelMu.Lock()
	defer localModelMu.Unlock()

	localModel = m
}

func currentLocalModel() LocalModel {
	localModelMu.Lock()
	defer localModelMu.Unlock()

	return localModel
}

// localModelOptions is what the engine needs to know about a model besides its file.
type localModelOptions struct {
	ContextTokens   int     `json:"contextTokens"`
	AnswerTokens    int     `json:"answerTokens"`
	Temperature     float64 `json:"temperature"`
	AssistantPrefix string  `json:"assistantPrefix"`
}

func optionsFor(modelID string) localModelOptions {
	opts := localModelOptions{ContextTokens: defaultLocalContext, AnswerTokens: defaultLocalAnswer, Temperature: 0.3}

	if m, ok := localModelByID(modelID); ok {
		opts.ContextTokens, opts.AssistantPrefix = m.ContextTokens, m.AssistantPrefix
	}

	return opts
}

// localPiece receives the engine's pieces and passes on the answer without reasoning.
type localPiece struct {
	filter thinkFilter
	emit   func(string)
}

func (p *localPiece) OnPiece(text string) {
	if out := p.filter.feed(text); out != "" {
		p.emit(out)
	}
}

func streamDevice(ctx context.Context, req aiRequest, emit func(string)) error {
	m := currentLocalModel()
	if m == nil {
		return errors.New(deviceUnavailable)
	}

	opts, err := json.Marshal(optionsFor(req.model))
	if err != nil {
		return err
	}

	turns, err := json.Marshal(req.turns())
	if err != nil {
		return err
	}

	localRunMu.Lock()
	defer localRunMu.Unlock()

	if err := ctx.Err(); err != nil {
		return deviceCtxErr(err)
	}

	done, watched := make(chan struct{}), make(chan struct{})

	go func() {
		defer close(watched)

		select {
		case <-ctx.Done():
			m.Cancel()
		case <-done:
		}
	}()

	sink := &localPiece{emit: emit}
	result := m.Generate(req.model, string(opts), req.system, string(turns), sink)

	close(done)
	<-watched

	if rest := sink.filter.flush(); rest != "" {
		emit(rest)
	}

	if err := ctx.Err(); err != nil {
		return deviceCtxErr(err)
	}

	return deviceError(result)
}

func deviceCtxErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New(deviceTimedOut)
	}

	return context.Canceled
}

// deviceError turns the engine's result into a message for the UI.
func deviceError(result string) error {
	code, detail, _ := strings.Cut(strings.TrimSpace(result), " ")

	switch code {
	case "":
		return nil
	case "not-downloaded":
		return errors.New("the model is not on this phone yet: download it in Settings, AI diagnosis")
	case "too-long":
		var prompt, window int
		if _, err := fmt.Sscan(detail, &prompt, &window); err == nil {
			return fmt.Errorf("the question is too long for the model on this phone (%d tokens, it reads %d): "+
				"start a new conversation, ask with another provider, or share it with an assistant app", prompt, window)
		}

		return errors.New("the question is too long for the model on this phone: ask with another provider, or share it with an assistant app")
	case "out-of-memory":
		return errors.New("the phone does not have enough free memory for this model: close other apps, or pick a smaller model")
	case "no-engine":
		return errors.New(deviceUnavailable)
	default:
		return errors.New("the model on this phone failed: " + clipUTF8(strings.TrimSpace(result), maxProviderMessage))
	}
}

// thinkFilter drops the <think>…</think> block reasoning models write before the answer,
// and the blank lines around it, from a stream whose pieces may split the tags.
type thinkFilter struct {
	inThink bool
	started bool   // something of the answer was passed on
	pending string // the end of the last piece, which may be the start of a tag
}

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

func (f *thinkFilter) feed(piece string) string {
	buf := f.pending + piece
	f.pending = ""

	var out strings.Builder

	for buf != "" {
		tag := thinkOpen
		if f.inThink {
			tag = thinkClose
		}

		if i := strings.Index(buf, tag); i >= 0 {
			if !f.inThink {
				out.WriteString(buf[:i])
			}

			buf = buf[i+len(tag):]
			f.inThink = !f.inThink

			continue
		}

		keep := partialSuffix(buf, tag)
		if !f.inThink {
			out.WriteString(buf[:len(buf)-keep])
		}

		f.pending = buf[len(buf)-keep:]

		break
	}

	return f.pass(out.String())
}

// flush is what is left once the stream ended: a held-back "<thi" was text after all.
func (f *thinkFilter) flush() string {
	rest := f.pending
	f.pending = ""

	if f.inThink {
		return ""
	}

	return f.pass(rest)
}

func (f *thinkFilter) pass(text string) string {
	if !f.started {
		text = strings.TrimLeft(text, " \t\r\n")
		f.started = text != ""
	}

	return text
}

// partialSuffix is the length of the longest end of s that begins tag (but is not all of it).
func partialSuffix(s, tag string) int {
	for n := min(len(s), len(tag)-1); n > 0; n-- {
		if strings.HasSuffix(s, tag[:n]) {
			return n
		}
	}

	return 0
}
