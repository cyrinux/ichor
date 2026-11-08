package ichorgo

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Pulling a model onto an Ollama server from the phone: POST /api/pull {"model", "stream":
// true} through the API server's proxy, as the plugin's actions call it
// (plugin_ollama_actions.go), answered by a stream of JSON lines
// (https://github.com/ollama/ollama/blob/main/docs/api.md#pull-a-model): "pulling manifest",
// then one "pulling <digest>" line per layer with its total and the bytes done so far, then
// "verifying sha256 digest", "writing manifest", "success"; or {"error": "…"}. A pull takes
// minutes and gigabytes, so it is a run with a listener, not a plugin action.
//
// The API server's proxy streams the answer as it comes: its proxy handler
// (k8s.io/apimachinery/pkg/util/proxy, UpgradeAwareHandler) copies the backend's answer with a
// flush interval, so the lines arrive while the pull runs. Not checked against a live Ollama.

const (
	// ollamaPullLineMax bounds one line of the stream; a progress line is about 150 bytes.
	ollamaPullLineMax = 64 << 10
	// ollamaPullModelMax bounds a model name (a library name or a registry path with a tag).
	ollamaPullModelMax = 128
	// ollamaPullAudit is the audit entry's action.
	ollamaPullAudit = "ollama.pull"
	// ollamaPullSuccess is the last line of a pull that worked.
	ollamaPullSuccess = "success"
)

// ollamaPullEvery is the shortest pause between two progress callbacks: the apps redraw a
// bar, not every chunk. A change of step (a new status) is sent at once.
var ollamaPullEvery = 250 * time.Millisecond

// ollamaModelName is a model name a pull accepts: "qwen2.5:0.5b", "llama3.2",
// "hf.co/org/model:q4_k_m".
var ollamaModelName = regexp.MustCompile(`^[A-Za-z0-9._/-]+(:[A-Za-z0-9._-]+)?$`)

// OllamaPullListener follows a model pull (implemented in Kotlin/Swift).
type OllamaPullListener interface {
	// OnProgress gets Ollama's step ("pulling manifest", "pulling 6a0746a1ec1a", "verifying
	// sha256 digest", "success") and the bytes done of total over the layers seen so far
	// (0 and 0 before the first layer); at most about 4 times a second.
	OnProgress(status string, completed, total int64)
	// OnDone is called exactly once; errMessage is empty when the model is on the server, and
	// after Cancel; else Ollama's own message (a name it does not know, a full disk) or why
	// the call failed.
	OnDone(errMessage string)
}

// OllamaPullRun is a handle on a running pull.
type OllamaPullRun struct {
	cancel context.CancelFunc
}

// Cancel stops the pull: the connection is closed and Ollama stops downloading. The layers
// already downloaded stay on the server and a new pull of the model resumes from them.
func (r *OllamaPullRun) Cancel() { r.cancel() }

// StartOllamaPull pulls model onto the Ollama server namespace/server (the Deployment or
// StatefulSet running it, as the plugin's server rows name it), through the API server's
// proxy of its Service (else of a ready pod). It needs create on services/proxy (pods/proxy):
// a credential without it is refused before the call. One audit entry (ollama.pull) when the
// run ends. The demo cluster refuses it. kubeServer: see KubePods.
func StartOllamaPull(configYAML, contextName, kubeServer, namespace, server, model string, listener OllamaPullListener) *OllamaPullRun {
	contextName = unmaskContext(configYAML, contextName)
	namespace, server = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(server))
	model = strings.TrimSpace(model)

	listener = maskedOllamaPullListener{listener}

	// No deadline: a pull of a large model takes as long as the server's link needs.
	ctx, cancel := context.WithCancel(context.Background())
	target := kubeTarget{configYAML, contextName, kubeServer}

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)
		defer holdCalls(configYAML, contextName)()

		var progress ollamaPullProgress

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{
				Server: kubeServer, Action: ollamaPullAudit, Namespace: namespace,
				Object: "OllamaModel/" + model, Params: "server=" + server,
			}
		}, func() error {
			err := runOllamaPull(ctx, target, namespace, server, model, func(p ollamaPullProgress) {
				progress = p
				listener.OnProgress(p.status, p.completed, p.total)
			})
			if err != nil && ctx.Err() != nil {
				return partialOutcome{fmt.Sprintf("stopped after %d of %d bytes", progress.completed, progress.total)}
			}

			return err
		})
		clearPartial(&err)

		listener.OnDone(errText(err))
	}()

	return &OllamaPullRun{cancel: cancel}
}

// validateOllamaModel refuses a name Ollama could not pull, before any call.
func validateOllamaModel(model string) error {
	if model == "" || len(model) > ollamaPullModelMax || !ollamaModelName.MatchString(model) || strings.Contains(model, "..") {
		return fmt.Errorf("invalid model name %q: use name:tag as the library lists it", model)
	}

	return nil
}

// runOllamaPull checks the names, the demo and the credentials, then pulls; emit gets the
// coalesced progress.
func runOllamaPull(ctx context.Context, target kubeTarget, namespace, server, model string, emit func(ollamaPullProgress)) error {
	if err := validateKubeName("Ollama server", namespace, server); err != nil {
		return err
	}

	if err := validateOllamaModel(model); err != nil {
		return err
	}

	if isDemoContext(target.config, target.context) {
		return errDemoUnavailable
	}

	k, _, err := kubeClients.get(target)
	if err != nil {
		return err
	}

	s, err := ollamaPullServer(ctx, k, namespace, server)
	if err != nil {
		return kubeError(err)
	}

	resp, err := k.request(ctx, http.MethodPost, s.target.prefix+"/api/pull", "application/x-ndjson, application/json", "application/json",
		[]byte(fmt.Sprintf(`{"model":%q,"stream":true}`, model)), nil)
	if err != nil {
		return kubeError(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, ollamaPullLineMax)) //nolint:errcheck

		return ollamaActionError(ollamaStatusError(resp.StatusCode, answer), s, "pull", model)
	}

	return readOllamaPull(resp.Body, time.Now, emit)
}

// ollamaPullServer locates the server (bounded by callTimeout: only the pull itself is not)
// and checks the credentials may create on its proxy, so a refusal comes before any byte.
// A review that cannot be asked leaves the decision to the API server.
func ollamaPullServer(ctx context.Context, k *kubeClient, namespace, name string) (ollamaServer, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	s, err := ollamaServerNamed(ctx, k, namespace, name)
	if err != nil {
		return ollamaServer{}, err
	}

	resource, sub, _ := strings.Cut(s.target.resource, "/")

	access, err := canDo(ctx, k, ssarAttributes{Namespace: namespace, Verb: "create", Resource: resource, Subresource: sub})
	if err == nil && !access.Allowed {
		return ollamaServer{}, &kubeAPIError{
			Code: http.StatusForbidden, Reason: "Forbidden",
			Message: ollamaPullRefusal(s.target.resource, namespace, access.Reason),
		}
	}

	return s, nil
}

// ollamaPullRefusal says the credentials may not use the proxy, with the review's reason.
func ollamaPullRefusal(resource, namespace, reason string) string {
	msg := fmt.Sprintf("these credentials may not create %s in %s", resource, namespace)
	if reason != "" {
		msg += " (" + reason + ")"
	}

	return msg
}

// ollamaPullLine is one line of the stream.
type ollamaPullLine struct {
	Status    string `json:"status"`
	Digest    string `json:"digest"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Error     string `json:"error"`
}

// ollamaPullProgress is what the listener hears: the step and the bytes over every layer.
type ollamaPullProgress struct {
	status           string
	completed, total int64
}

// ollamaPullState sums the layers seen so far and decides when to tell the listener.
type ollamaPullState struct {
	layers map[string][2]int64 // digest → completed, total
	order  []string
	last   ollamaPullProgress
	sent   time.Time
	any    bool
}

// add takes one line; it returns the progress to send, if it is time to send one.
func (s *ollamaPullState) add(line ollamaPullLine, now time.Time) (ollamaPullProgress, bool) {
	if line.Digest != "" {
		if _, ok := s.layers[line.Digest]; !ok {
			s.order = append(s.order, line.Digest)
		}

		s.layers[line.Digest] = [2]int64{line.Completed, line.Total}
	}

	p := ollamaPullProgress{status: line.Status}
	for _, d := range s.order {
		p.completed += s.layers[d][0]
		p.total += s.layers[d][1]
	}

	stepChanged := !s.any || p.status != s.last.status
	if !stepChanged && (p == s.last || now.Sub(s.sent) < ollamaPullEvery) {
		return p, false
	}

	s.last, s.sent, s.any = p, now, true

	return p, true
}

// readOllamaPull reads the stream line by line until "success" (nil), an {"error"} line (its
// message) or the end of the stream (an error: the pull did not finish). The last progress
// before the end is always sent.
func readOllamaPull(r io.Reader, now func() time.Time, emit func(ollamaPullProgress)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), ollamaPullLineMax)

	state := &ollamaPullState{layers: map[string][2]int64{}}

	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}

		var line ollamaPullLine
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			return fmt.Errorf("decode Ollama pull progress: %w", err)
		}

		if line.Error != "" {
			return &kubeAPIError{Code: http.StatusInternalServerError, Reason: ollamaReason, Message: line.Error}
		}

		p, send := state.add(line, now())
		if line.Status == ollamaPullSuccess {
			emit(p)

			return nil
		}

		if send {
			emit(p)
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("an Ollama pull line is longer than %d bytes", ollamaPullLineMax)
		}

		return kubeError(err)
	}

	return errors.New("the pull ended before Ollama reported success")
}
