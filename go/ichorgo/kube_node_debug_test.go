package ichorgo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// fakeNodeDebugAPI plays the API server for a node debug run in "default": it lists a stale
// and a fresh debug pod, takes the new pod (or refuses it with refuse), reports it pending
// once then running (or never pulled with pullFailure), and serves its exec stream, which
// echoes stdin until "exit\r" and then reports exit code 0.
type fakeNodeDebugAPI struct {
	*fakeKubeAPI

	mu          sync.Mutex
	created     map[string]any
	name        string
	gets        int
	deleted     []string
	deleteQuery url.Values
	execQuery   url.Values
	refuse      string
	pullFailure bool
}

const nodeDebugPods = "/api/v1/namespaces/default/pods"

func newFakeNodeDebugAPI(t *testing.T) *fakeNodeDebugAPI {
	t.Helper()

	f := &fakeNodeDebugAPI{fakeKubeAPI: &fakeKubeAPI{}}

	exec := websocket.Server{
		Handshake: func(cfg *websocket.Config, r *http.Request) error {
			f.mu.Lock()
			f.execQuery = r.URL.Query()
			f.mu.Unlock()

			cfg.Protocol = []string{"v4.channel.k8s.io"}

			return nil
		},
		Handler: func(conn *websocket.Conn) {
			conn.PayloadType = websocket.BinaryFrame

			for {
				var fr []byte
				if err := websocket.Message.Receive(conn, &fr); err != nil {
					return
				}

				if fr[0] != execStdin {
					continue
				}

				if string(fr[1:]) == "exit\r" {
					_ = websocket.Message.Send(conn, frame(execStatus, `{"status":"Success"}`))

					return
				}

				_ = websocket.Message.Send(conn, frame(execStdout, string(fr[1:])))
			}
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`) })
	mux.HandleFunc("GET "+nodeDebugPods, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"items":[
			{"metadata":{"name":"ichor-node-debug-old00","creationTimestamp":"2020-01-01T00:00:00Z"},"status":{"phase":"Running"}},
			{"metadata":{"name":"ichor-node-debug-done0","creationTimestamp":"`+time.Now().UTC().Format(time.RFC3339)+`"},"status":{"phase":"Succeeded"}},
			{"metadata":{"name":"ichor-node-debug-live0","creationTimestamp":"`+time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339)+`"},"status":{"phase":"Running"}}]}`)
	})
	mux.HandleFunc("POST "+nodeDebugPods, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		f.mu.Lock()
		defer f.mu.Unlock()

		if f.refuse != "" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"`+f.refuse+`"}`)

			return
		}

		_ = json.Unmarshal(body, &f.created)
		f.name, _ = f.created["metadata"].(map[string]any)["name"].(string)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(body)
	})
	mux.HandleFunc("GET "+nodeDebugPods+"/{name}", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.gets++

		switch {
		case f.pullFailure:
			_, _ = io.WriteString(w, `{"status":{"phase":"Pending","containerStatuses":[{"state":{"waiting":{"reason":"ImagePullBackOff","message":"pull access denied"}}}]}}`)
		case f.gets == 1:
			_, _ = io.WriteString(w, `{"status":{"phase":"Pending","containerStatuses":[{"state":{"waiting":{"reason":"ContainerCreating"}}}]}}`)
		default:
			_, _ = io.WriteString(w, `{"status":{"phase":"Running"}}`)
		}
	})
	mux.HandleFunc("DELETE "+nodeDebugPods+"/{name}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.deleted = append(f.deleted, r.PathValue("name"))
		f.deleteQuery = r.URL.Query()
		_, _ = io.WriteString(w, `{}`)
	})
	mux.Handle(nodeDebugPods+"/{name}/exec", exec)

	f.Server = httptest.NewTLSServer(mux)
	t.Cleanup(f.Close)

	poll := netPerfPoll
	netPerfPoll = time.Millisecond

	t.Cleanup(func() { netPerfPoll = poll })

	return f
}

func (f *fakeNodeDebugAPI) deletedPods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.deleted)
}

func TestStartNodeDebugRunsAShellOnTheNodeThenDeletesItsPod(t *testing.T) {
	withDataDir(t)

	f := newFakeNodeDebugAPI(t)
	l := newShellRecorder()

	d := StartNodeDebug(f.kubeconfigFor(f.URL), "admin@test", "", "worker-1", "", "", 80, 24, l)
	d.Write([]byte("hostname\r"))
	d.Write([]byte("exit\r"))
	l.wait(t)

	if l.code != 0 || l.message != "" || !strings.Contains(l.output.String(), "hostname") {
		t.Fatalf("exit %d %q, output %q", l.code, l.message, l.output.String())
	}

	f.mu.Lock()
	created, name, execQuery := f.created, f.name, f.execQuery
	f.mu.Unlock()

	if !strings.HasPrefix(name, "ichor-node-debug-") || !kubeNamePattern.MatchString(name) {
		t.Fatalf("pod name %q", name)
	}

	spec := created["spec"].(map[string]any)
	c := spec["containers"].([]any)[0].(map[string]any)
	labels := created["metadata"].(map[string]any)["labels"].(map[string]any)

	if spec["nodeName"] != "worker-1" || spec["hostPID"] != true || spec["hostNetwork"] != true || spec["hostIPC"] != true ||
		c["securityContext"].(map[string]any)["privileged"] != true || c["image"] != podDebugDefaultImage ||
		labels["app.kubernetes.io/name"] != nodeDebugApp || labels["app.kubernetes.io/managed-by"] != "ichor" {
		t.Errorf("pod %v", created)
	}

	if tol := spec["tolerations"].([]any); len(tol) != 1 || tol[0].(map[string]any)["operator"] != "Exists" {
		t.Errorf("tolerations %v", tol)
	}

	if !slices.Equal(execQuery["command"], nodeDebugShell) || execQuery.Get("container") != nodeDebugContainer || execQuery.Get("tty") != "true" {
		t.Errorf("exec query %v", execQuery)
	}

	// The pod is already deleted when OnExit comes: nothing waits for it after.
	deleted := f.deletedPods()
	if !slices.Contains(deleted, name) || f.deleteQuery.Get("gracePeriodSeconds") != "0" {
		t.Errorf("deleted %v (pod %s), query %v", deleted, name, f.deleteQuery)
	}

	// The sweep: the pods past their deadline and the finished ones go; a shell open for two
	// hours (another phone's) stays.
	if !slices.Contains(deleted, "ichor-node-debug-old00") || !slices.Contains(deleted, "ichor-node-debug-done0") ||
		slices.Contains(deleted, "ichor-node-debug-live0") {
		t.Errorf("swept %v", deleted)
	}

	got := readAudit(t, "admin@test", "debug-node")
	if len(got) != 1 || got[0].Object != "Node/worker-1" || got[0].Namespace != "default" ||
		!strings.Contains(got[0].Params, "pod="+name) || !strings.Contains(got[0].Params, "image="+podDebugDefaultImage) {
		t.Errorf("audit %+v", got)
	}
}

func TestStartNodeDebugReportsAPodSecurityRefusalAsIs(t *testing.T) {
	withDataDir(t)

	f := newFakeNodeDebugAPI(t)
	f.refuse = `pods \"ichor-debug-worker-1-x\" is forbidden: violates PodSecurity \"restricted:latest\": privileged (container \"debug\" must not set securityContext.privileged=true)`
	l := newShellRecorder()

	StartNodeDebug(f.kubeconfigFor(f.URL), "admin@test", "", "worker-1", "", "", 80, 24, l)
	l.wait(t)

	if l.code != -1 || !strings.Contains(l.message, `violates PodSecurity "restricted:latest": privileged`) {
		t.Fatalf("exit %d %q", l.code, l.message)
	}

	if f.created != nil {
		t.Errorf("a pod was created: %v", f.created)
	}

	if got := readAudit(t, "admin@test", "debug-node"); len(got) != 1 || got[0].Outcome != auditFailed {
		t.Errorf("audit %+v", got)
	}
}

func TestStartNodeDebugDeletesAPodThatCannotStart(t *testing.T) {
	withDataDir(t)

	f := newFakeNodeDebugAPI(t)
	f.pullFailure = true
	l := newShellRecorder()

	StartNodeDebug(f.kubeconfigFor(f.URL), "admin@test", "", "worker-1", "", "registry.example/nope:1", 80, 24, l)
	l.wait(t)

	if l.code != -1 || !strings.Contains(l.message, "ImagePullBackOff pull access denied") {
		t.Fatalf("exit %d %q", l.code, l.message)
	}

	if name := f.name; !slices.Contains(f.deletedPods(), name) {
		t.Errorf("pod %s left behind: deleted %v", name, f.deletedPods())
	}
}

func TestStartNodeDebugRefusals(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, node, namespace, image, want string }{
		{"demo", "demo-worker-1", "", "", errDemoUnavailable.Error()},
		{"bad node", "../etc", "", "", "invalid Kubernetes node name"},
		{"bad namespace", "worker-1", "Not_A_Namespace", "", "namespace"},
		{"bad image", "worker-1", "", "-rm", "invalid image reference"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newShellRecorder()
			StartNodeDebug(cfg, "", "", tc.node, tc.namespace, tc.image, 80, 24, l)
			l.wait(t)

			if l.code != -1 || !strings.Contains(l.message, tc.want) {
				t.Errorf("exit %d %q, want %q", l.code, l.message, tc.want)
			}
		})
	}
}

func TestNodeDebugPodNamesAreValidAndDistinct(t *testing.T) {
	a, b := nodeDebugPodName(), nodeDebugPodName()
	if a == b || !kubeNamePattern.MatchString(a) || !strings.HasPrefix(a, "ichor-node-debug-") {
		t.Errorf("names %q %q", a, b)
	}
}

func TestNodeDebugStatusMasksTheNode(t *testing.T) {
	SetPrivacyMask(true, "worker-1")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	f := newFakeNodeDebugAPI(t)
	k, err := openKubeClient(t.Context(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	var statuses []string
	if err := startNodeDebugPod(t.Context(), k, "default", nodeDebugPodName(), "worker-1", podDebugDefaultImage,
		func(s string) { statuses = append(statuses, s) }); err != nil {
		t.Fatal(err)
	}

	if joined := strings.Join(statuses, "\n"); strings.Contains(joined, "worker-1") || !strings.Contains(joined, "Creating privileged pod") {
		t.Errorf("statuses %q", statuses)
	}
}
