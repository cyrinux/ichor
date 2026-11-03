package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The cluster's Ollama as the AI diagnosis's server: the AI client on the phone speaks to
// Ollama's OpenAI-compatible /v1 (https://docs.ollama.com/openai) through a port-forward to a
// ready pod of the server (kube_portforward.go), so neither a key nor the report leaves the
// cluster's network. The provider is "ollama-cluster" (ai.go); the apps keep which server it
// is (namespace and workload, as the plugin's server rows name it) and call KubeOllamaForward
// before each question and model list: it hands back the running forward's base URL, or
// starts one again when it died, its pod went away, or the app had none yet.
//
// There is one such forward in the process: the AI settings point at one server. Leaving its
// cluster (CancelCalls) stops it, as does StopOllamaForward when the user picks another
// provider.

const (
	// aiOllamaCluster is the provider whose server is an Ollama of the open cluster.
	aiOllamaCluster = "ollama-cluster"
	// ollamaForwardReady bounds the wait for the forward's local port.
	ollamaForwardReady = 20 * time.Second
)

// ollamaForwardInfo is what the apps get: the base URL to give the AI calls and the target,
// for the settings' "via port-forward to NS/POD".
type ollamaForwardInfo struct {
	BaseURL   string `json:"baseUrl"`
	Namespace string `json:"namespace"`
	Server    string `json:"server"`
	Pod       string `json:"pod"`
	Port      int    `json:"port"`
}

// ollamaForward is the running forward.
type ollamaForward struct {
	context string // the kubeconfig context it runs on (unmasked)
	info    ollamaForwardInfo
	run     *PortForwardRun
	done    chan struct{} // closed by OnDone
}

func (f *ollamaForward) alive() bool {
	select {
	case <-f.done:
		return false
	default:
		return true
	}
}

var (
	ollamaForwardMu  sync.Mutex
	ollamaForwardCur *ollamaForward

	// startForward is StartPortForward; tests replace it with a forward to a local server.
	startForward = StartPortForward
)

// KubeOllamaForward makes sure a port-forward runs to the Ollama server namespace/server (the
// Deployment or StatefulSet running it) of the open cluster, and returns it as JSON
// {"baseUrl": "http://127.0.0.1:PORT/v1", "namespace", "server", "pod", "port"}. A forward
// already running to a pod of that server that is still ready is kept; else the one running
// is stopped and a new one opens to the server's first ready pod. It needs create on
// pods/portforward: a credential without it is refused before the forward. The demo cluster
// refuses it. kubeServer: see KubePods.
func KubeOllamaForward(configYAML, contextName, kubeServer, namespace, server string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, server = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(server))

	if err := validateKubeName("Ollama server", namespace, server); err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	ollamaForwardMu.Lock()
	defer ollamaForwardMu.Unlock()

	target := kubeTarget{configYAML, contextName, kubeServer}

	if f := ollamaForwardCur; f != nil && f.context == contextName && f.info.Namespace == namespace && f.info.Server == server && f.alive() {
		ready, err := withKube(target, func(ctx context.Context, k *kubeClient) (bool, error) {
			return ollamaPodStillReady(ctx, k, namespace, f.info.Pod), nil
		})
		if err == nil && ready {
			return toJSON(f.info)
		}
	}

	stopOllamaForwardLocked()

	pick, err := withKube(target, func(ctx context.Context, k *kubeClient) (ollamaForwardInfo, error) {
		return ollamaForwardPod(ctx, k, namespace, server)
	})
	if err != nil {
		return "", err
	}

	f, err := openOllamaForward(target, pick)
	if err != nil {
		return "", err
	}

	ollamaForwardCur = f

	return toJSON(f.info)
}

// OllamaForwardState is the running forward as KubeOllamaForward gives it, "" when none runs
// (never started, stopped, or its connection to the API server ended).
func OllamaForwardState() (out string, err error) {
	defer maskResult(&out, &err)

	ollamaForwardMu.Lock()
	defer ollamaForwardMu.Unlock()

	if ollamaForwardCur == nil || !ollamaForwardCur.alive() {
		return "", nil
	}

	return toJSON(ollamaForwardCur.info)
}

// StopOllamaForward stops the forward, if one runs: the user picked another provider.
func StopOllamaForward() {
	ollamaForwardMu.Lock()
	defer ollamaForwardMu.Unlock()

	stopOllamaForwardLocked()
}

// stopOllamaForwardOf stops the forward when it runs on contextName: the app left that cluster.
func stopOllamaForwardOf(contextName string) {
	ollamaForwardMu.Lock()
	defer ollamaForwardMu.Unlock()

	if ollamaForwardCur != nil && ollamaForwardCur.context == contextName {
		stopOllamaForwardLocked()
	}
}

func stopOllamaForwardLocked() {
	if ollamaForwardCur != nil {
		ollamaForwardCur.run.Stop()
		ollamaForwardCur = nil
	}
}

// ollamaForwardPod is the server's first ready pod and the port its Ollama listens on, once
// the credentials may forward to it (a review that cannot be asked leaves it to the API server).
func ollamaForwardPod(ctx context.Context, k *kubeClient, namespace, server string) (ollamaForwardInfo, error) {
	s, err := ollamaWorkloadNamed(ctx, k, namespace, server)
	if err != nil {
		return ollamaForwardInfo{}, err
	}

	pod, err := ollamaReadyPod(ctx, k, &s)
	if err != nil {
		return ollamaForwardInfo{}, err
	}

	if pod == "" {
		return ollamaForwardInfo{}, &kubeAPIError{
			Code: http.StatusServiceUnavailable, Reason: "Unavailable",
			Message: fmt.Sprintf("Ollama server %s has no pod ready", server),
		}
	}

	access, err := canDo(ctx, k, ssarAttributes{Namespace: namespace, Verb: "create", Resource: "pods", Subresource: "portforward"})
	if err == nil && !access.Allowed {
		return ollamaForwardInfo{}, &kubeAPIError{
			Code: http.StatusForbidden, Reason: "Forbidden",
			Message: ollamaPullRefusal("pods/portforward", namespace, access.Reason),
		}
	}

	return ollamaForwardInfo{Namespace: namespace, Server: server, Pod: pod, Port: s.podPort}, nil
}

// ollamaPodStillReady: the forward's pod is still there and ready (not replaced by a rollout).
func ollamaPodStillReady(ctx context.Context, k *kubeClient, namespace, pod string) bool {
	var p ollamaPodObject

	err := k.get(ctx, "/api/v1/namespaces/"+url.PathEscape(namespace)+"/pods/"+url.PathEscape(pod), &p)

	return err == nil && ollamaPodReady(p)
}

// openOllamaForward starts the forward to pick and waits for its local port.
func openOllamaForward(target kubeTarget, pick ollamaForwardInfo) (*ollamaForward, error) {
	l := &ollamaForwardListener{ready: make(chan string, 1), done: make(chan struct{}), ended: make(chan string, 1)}
	run := startForward(target.config, target.context, target.server, pick.Namespace, pick.Pod, pick.Port, l)

	select {
	case address := <-l.ready:
		pick.BaseURL = "http://" + address + "/v1"

		return &ollamaForward{context: target.context, info: pick, run: run, done: l.done}, nil
	case msg := <-l.ended:
		if msg == "" {
			msg = "the forward ended"
		}

		return nil, fmt.Errorf("port-forward to %s/%s: %s", pick.Namespace, pick.Pod, msg)
	case <-time.After(ollamaForwardReady):
		run.Stop()

		return nil, errors.New("the port-forward to the Ollama server did not open in time")
	}
}

// ollamaForwardListener hears the forward: its local address, and its end.
type ollamaForwardListener struct {
	ready chan string
	done  chan struct{}
	ended chan string
}

func (l *ollamaForwardListener) OnReady(address string) { l.ready <- address }

// OnConnectionError: one connection failed (the AI call that made it says so); the forward goes on.
func (l *ollamaForwardListener) OnConnectionError(string) {}

func (l *ollamaForwardListener) OnDone(errMessage string) {
	l.ended <- errMessage
	close(l.done)
}
