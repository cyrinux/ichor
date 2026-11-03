package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// The Ollama plugin's actions (go/plugin/builtin/ollama.yaml, verb native): load a model into
// memory, unload it, delete it from disk. Each is one call to the model's server through the
// API server's proxy, as the readers make (plugin_native_ollama.go): POST /api/generate with
// an empty prompt (keep_alive -1 pins it, 0 unloads it) and DELETE /api/delete
// (https://github.com/ollama/ollama/blob/main/docs/api.md). The target is an OllamaModel as
// native:ollama-models lists it: metadata.namespace, metadata.name (the model, llama3.2:3b)
// and spec.server (the workload serving it).

const (
	// ollamaLoadTimeout bounds a load: Ollama answers once the model is in memory, which for a
	// large model read from a slow disk takes a minute or more.
	ollamaLoadTimeout = 120 * time.Second
	// ollamaUnloadTimeout bounds an unload: Ollama first finishes the requests in flight.
	ollamaUnloadTimeout = 30 * time.Second
)

// ollamaGenerate is the part of /api/generate's answer the actions read.
type ollamaGenerate struct {
	Done       bool   `json:"done"`
	DoneReason string `json:"done_reason"`
}

// runOllamaLoad loads the model, pinned until unloaded when params.pin (keep_alive -1), else
// for the server's keep_alive (5 minutes by default).
func runOllamaLoad(ctx context.Context, k *kubeClient, _ *plugin.Compiled, target, params map[string]any) error {
	s, model, err := ollamaActionServer(ctx, k, target)
	if err != nil {
		return err
	}

	body := map[string]any{"model": model, "stream": false}
	if pin, _ := params["pin"].(bool); pin {
		body["keep_alive"] = -1
	}

	var out ollamaGenerate
	if err := ollamaCall(ctx, k, ollamaLoadTimeout, http.MethodPost, s.target.prefix, "/api/generate", body, &out); err != nil {
		return ollamaActionError(err, s, "load", model)
	}

	return nil
}

// runOllamaUnload unloads the model (keep_alive 0); Ollama answers done_reason "unload".
func runOllamaUnload(ctx context.Context, k *kubeClient, _ *plugin.Compiled, target, _ map[string]any) error {
	s, model, err := ollamaActionServer(ctx, k, target)
	if err != nil {
		return err
	}

	var out ollamaGenerate

	body := map[string]any{"model": model, "keep_alive": 0, "stream": false}
	if err := ollamaCall(ctx, k, ollamaUnloadTimeout, http.MethodPost, s.target.prefix, "/api/generate", body, &out); err != nil {
		return ollamaActionError(err, s, "unload", model)
	}

	// An older Ollama says no reason; one that says another did not unload.
	if out.DoneReason != "" && out.DoneReason != "unload" {
		return &kubeAPIError{
			Code: http.StatusConflict, Reason: ollamaReason,
			Message: fmt.Sprintf("Ollama did not unload %s (it answered %q)", model, out.DoneReason),
		}
	}

	return nil
}

// runOllamaDelete deletes the model from the server's disk; a model already gone (404 from
// Ollama) is done, not an error.
func runOllamaDelete(ctx context.Context, k *kubeClient, _ *plugin.Compiled, target, _ map[string]any) error {
	s, model, err := ollamaActionServer(ctx, k, target)
	if err != nil {
		return err
	}

	err = ollamaCall(ctx, k, ollamaCallTimeout, http.MethodDelete, s.target.prefix, "/api/delete", map[string]any{"model": model}, nil)
	if ollamaAnswered(err, http.StatusNotFound) {
		return nil
	}

	if err != nil {
		return ollamaActionError(err, s, "delete", model)
	}

	return nil
}

// ollamaActionServer is the server of the model target (its namespace and spec.server),
// located as the readers locate it (a Service selecting its pods, else a ready pod), and the
// model's name.
func ollamaActionServer(ctx context.Context, k *kubeClient, target map[string]any) (ollamaServer, string, error) {
	meta := mapAt(target, "metadata")
	namespace, model := stringAt(meta, "namespace"), stringAt(meta, "name")
	name := stringAt(mapAt(target, "spec"), "server")

	if namespace == "" || model == "" || name == "" {
		return ollamaServer{}, "", errPluginActionInvalid(errors.New("the model names no server"))
	}

	s, err := ollamaServerNamed(ctx, k, namespace, name)
	if err != nil {
		return ollamaServer{}, "", err
	}

	return s, model, nil
}

// ollamaServerNamed is the Ollama workload namespace/name, located as the readers locate it
// (a Service selecting its pods, else a ready pod): a 404 when there is none, a 503 (403 when
// refused) when its API cannot be reached.
func ollamaServerNamed(ctx context.Context, k *kubeClient, namespace, name string) (ollamaServer, error) {
	s, err := ollamaWorkloadNamed(ctx, k, namespace, name)
	if err != nil {
		return ollamaServer{}, err
	}

	var services kubeList[ollamaServiceObject]
	servicesErr := getList(ctx, k, scopedPath("/api/v1", namespace, "services"), &services)

	locateOllama(ctx, k, &s, services.Items, servicesErr)

	if s.target.prefix == "" {
		code := http.StatusServiceUnavailable
		if s.forbidden {
			code = http.StatusForbidden
		}

		return ollamaServer{}, &kubeAPIError{Code: code, Reason: "Unavailable", Message: fmt.Sprintf("Ollama server %s: %s", name, s.err)}
	}

	return s, nil
}

// ollamaWorkloadNamed is the Ollama workload namespace/name, not located yet: a 404 when there
// is none.
func ollamaWorkloadNamed(ctx context.Context, k *kubeClient, namespace, name string) (ollamaServer, error) {
	workloads, err := listOllamaWorkloads(ctx, k, namespace)
	if err != nil {
		return ollamaServer{}, err
	}

	for _, w := range workloads {
		if w.name == name {
			return w, nil
		}
	}

	return ollamaServer{}, &kubeAPIError{Code: http.StatusNotFound, Reason: "NotFound", Message: fmt.Sprintf("no Ollama server %s in %s", name, namespace)}
}

// ollamaAnswered: err is Ollama's own answer (not the API server's) with that code.
func ollamaAnswered(err error, code int) bool {
	var apiErr *kubeAPIError

	return errors.As(err, &apiErr) && apiErr.Reason == ollamaReason && apiErr.Code == code
}

// ollamaActionError is a failed action in the user's words: a full queue (503) says to try
// later, Ollama's own message is kept (a model too large for the server says so), and the
// proxy's path is left out of the API server's.
func ollamaActionError(err error, s ollamaServer, verb, model string) error {
	var apiErr *kubeAPIError
	if !errors.As(err, &apiErr) {
		return err
	}

	msg := strings.ReplaceAll(apiErr.Message, s.target.prefix, "")

	if apiErr.Reason == ollamaReason && apiErr.Code == http.StatusServiceUnavailable {
		msg = fmt.Sprintf("Ollama server %s is busy (its request queue is full): try to %s %s again later", s.name, verb, model)
	}

	return &kubeAPIError{Code: apiErr.Code, Reason: apiErr.Reason, Message: msg}
}
