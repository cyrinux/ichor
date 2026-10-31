package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

// The AI diagnosis sends a report about the cluster to a model the user chose, with their
// own API key: Anthropic's Claude API, OpenAI's API, or any server speaking one of those
// two protocols when a base URL is given (a gateway, a local model). The "device" provider
// keeps it on the phone instead: a model file run by the app itself (see ai_device.go). It is off until the
// user turns it on, nothing is sent before they ask, and the key only passes through: it
// is never stored, logged or put in an error message here.
//
// Both APIs are called with net/http rather than the vendors' Go SDKs: together those add
// 18 MB to the library of every ABI, for two streaming requests and a model list.

const (
	aiAnthropic = "anthropic"
	aiOpenAI    = "openai"

	defaultAnthropicModel = "claude-opus-5-5"
	defaultOpenAIModel    = "gpt-6-astra"

	// A hard problem can take the model minutes; the app shows the answer as it is written.
	aiAnswerTimeout = 10 * time.Minute
	aiModelsTimeout = 20 * time.Second

	maxProviderMessage = 200

	answerCutNotice  = "\n\n[The answer stops here: the model reached its output limit.]"
	answerDeclined   = "the model declined to answer this request; try another model"
	answerIncomplete = "the answer was cut off before the end; try again"
)

// Names only a local network serves.
var localNameSuffixes = []string{".local", ".internal", ".lan", ".localhost"}

// The providers' own APIs; tests point them at a fake.
var (
	anthropicAPIURL = "https://api.anthropic.com"
	openAIAPIURL    = "https://api.openai.com/v1"
)

type aiProvider struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	DefaultModel string `json:"defaultModel"`
	KeyURL       string `json:"keyUrl"` // where the user creates an API key
}

var aiProviders = []aiProvider{
	{ID: aiAnthropic, Name: "Anthropic (Claude)", DefaultModel: defaultAnthropicModel, KeyURL: "https://console.anthropic.com/settings/keys"},
	{ID: aiOpenAI, Name: "OpenAI", DefaultModel: defaultOpenAIModel, KeyURL: "https://platform.openai.com/api-keys"},
	// No key, no server: DefaultModel is filled in from the catalog (AIProviders).
	{ID: aiDevice, Name: "This phone"},
}

type aiModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// aiRequest is one question to a model.
type aiRequest struct {
	provider aiProvider
	apiKey   string
	model    string
	baseURL  string // empty = the provider's own API; no trailing slash
	system   string
	user     string
	// messages, when set, is the whole conversation sent instead of the single user turn
	// (system stays apart); the last one is the user's.
	messages []chatMessage
	// tools are what the model may ask for, rounds what it already asked and got. With
	// forceAnswer, the tools stay declared (the rounds refer to them) but it may not use them.
	tools       []aiToolDef
	rounds      []aiToolRound
	forceAnswer bool
}

// turns is what the model gets after the system prompt.
func (r aiRequest) turns() []chatMessage {
	if len(r.messages) > 0 {
		return r.messages
	}

	return []chatMessage{{Role: "user", Content: r.user}}
}

// scrub removes the API key from text a server sent back (some echo the credential).
func (r aiRequest) scrub(text string) string {
	if r.apiKey == "" {
		return text
	}

	return strings.ReplaceAll(text, r.apiKey, "[API key]")
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AIProviders lists the providers the diagnosis can use, as JSON
// [{"id","name","defaultModel","keyUrl"}], so both apps show the same defaults. "device"
// is listed in every build: an app without the engine leaves it out of its settings.
func AIProviders() (out string, err error) {
	defer maskResult(&out, &err)

	return toJSON(listedProviders())
}

func listedProviders() []aiProvider {
	out := slices.Clone(aiProviders)

	for i := range out {
		if out[i].ID == aiDevice {
			out[i].DefaultModel = defaultLocalModel()
		}
	}

	return out
}

// AIModels lists the models the key can use, newest first, as JSON [{"id","name"}], to pick
// from instead of typing an id. It also tells whether the key and base URL work.
func AIModels(provider, apiKey, baseURL string) (out string, err error) {
	defer maskResult(&out, &err)

	req, err := newAIRequest(provider, apiKey, "", baseURL)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), aiModelsTimeout)
	defer cancel()

	var models []aiModel

	switch req.provider.ID {
	case aiDevice:
		models = localModelList(req.model)
	case aiAnthropic:
		models, err = anthropicModels(ctx, req)
	default:
		models, err = openAIModels(ctx, req)
	}

	if err != nil {
		return "", err
	}

	return toJSON(models)
}

// newAIRequest validates what the user typed in the settings. An empty model means the
// provider's default; the key may only be empty with a base URL (local servers need none).
// The device provider takes neither: whatever is set for them is ignored.
func newAIRequest(provider, apiKey, model, baseURL string) (aiRequest, error) {
	req := aiRequest{apiKey: strings.TrimSpace(apiKey), model: strings.TrimSpace(model)}

	for _, p := range listedProviders() {
		if p.ID == provider {
			req.provider = p
		}
	}

	if req.provider.ID == "" {
		return aiRequest{}, fmt.Errorf("unknown AI provider %q", provider)
	}

	if req.provider.ID == aiDevice {
		req.apiKey = ""
		if req.model == "" {
			req.model = req.provider.DefaultModel
		}

		if req.model == "" {
			return aiRequest{}, errors.New("no model on this phone yet: download or import one in Settings, AI diagnosis")
		}

		return req, nil
	}

	baseURL = strings.TrimSpace(baseURL)
	if baseURL != "" {
		u, err := url.Parse(baseURL)
		if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" {
			return aiRequest{}, errors.New("the base URL must look like https://host/path")
		}

		// A key never travels in clear: plain http is for local servers that need none.
		if u.Scheme == "http" && req.apiKey != "" {
			return aiRequest{}, errors.New("an API key is only sent over https: use an https base URL, or leave the key empty")
		}

		if u.Scheme == "http" && !onLocalNetwork(u.Hostname()) {
			return aiRequest{}, errors.New("a report only travels over https; plain http is for a server on your own network")
		}

		req.baseURL = strings.TrimRight(baseURL, "/")
	}

	if req.apiKey == "" && req.baseURL == "" {
		return aiRequest{}, errors.New("no API key set for " + req.provider.Name)
	}

	if req.model == "" {
		req.model = req.provider.DefaultModel
	}

	return req, nil
}

// onLocalNetwork tells whether a host is on the phone's own network, the only place a report
// may cross in clear: loopback, link-local, a private range, or a name only such a network serves.
func onLocalNetwork(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		addr = addr.Unmap()

		return addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsPrivate()
	}

	host = strings.ToLower(strings.TrimSuffix(host, "."))

	return host == "localhost" || slices.ContainsFunc(localNameSuffixes, func(suffix string) bool { return strings.HasSuffix(host, suffix) })
}

// streamAnswer asks the model and calls emit with each piece of the answer as it arrives.
func streamAnswer(ctx context.Context, req aiRequest, emit func(string)) error {
	_, err := streamTurn(ctx, req, emit)

	return err
}

// aiError turns a failed call into a short message for the UI. status is the HTTP status
// of the provider's answer (0 when the call never got one) and message what it said.
func aiError(ctx context.Context, provider aiProvider, status int, message string, err error) error {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return context.Canceled
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return errors.New(provider.Name + " took too long to answer")
	case status == 0:
		return fmt.Errorf("could not reach %s: %s", provider.Name, friendlyError(transportCause(err)))
	}

	var reason string

	switch {
	case status == 401:
		// The provider's message may quote part of the key: leave it out.
		return errors.New(provider.Name + " rejected the API key")
	case status == 402:
		reason = "billing problem on the account"
	case status == 403:
		reason = "the API key may not use this model"
	case status == 404:
		reason = "model not found (check the model name)"
	case status == 413:
		reason = "the report is too large for this model"
	case status == 429:
		reason = "rate limit or credit exhausted, try again later"
	case status >= 500:
		reason = "the service is overloaded or unavailable, try again later"
	default:
		reason = fmt.Sprintf("the request was refused (HTTP %d)", status)
	}

	if message = strings.TrimSpace(message); message != "" {
		reason += ": " + clipUTF8(message, maxProviderMessage)
	}

	return aiStatusError{fmt.Errorf("%s: %s", provider.Name, reason), status, message}
}

// transportCause drops the method and URL net/http wraps around a failed request.
func transportCause(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}

	return err
}
