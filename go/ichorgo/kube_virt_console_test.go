package ichorgo

// The serial console is compared with `virtctl console -n NAMESPACE NAME`: the same
// subresource, subprotocol and raw byte stream. It is interactive, so there is no probe
// subcommand; these tests serve the console from a fake API instead.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

const (
	vmConsoleVMIPath     = "/apis/kubevirt.io/v1/namespaces/lab/virtualmachineinstances/k3s-sandbox"
	vmConsoleConsolePath = "/apis/subresources.kubevirt.io/v1/namespaces/lab/virtualmachineinstances/k3s-sandbox/console"
)

// fakeVMConsoleAPI serves /apis, the instance in phase ("" for none: 404) and its console:
// refusing the first notReady dials with 400, then echoing every frame it gets after a
// "login: " banner, until it reads "poweroff". It records the frames and the subprotocol.
type fakeVMConsoleAPI struct {
	*fakeKubeAPI

	mu       sync.Mutex
	received [][]byte
	protocol []string
	dials    atomic.Int32
}

func newFakeVMConsoleAPI(t *testing.T, phase string, notReady int32, consoleStatus int) *fakeVMConsoleAPI {
	t.Helper()

	f := &fakeVMConsoleAPI{}

	ws := websocket.Server{
		Handshake: func(cfg *websocket.Config, _ *http.Request) error {
			f.mu.Lock()
			f.protocol = cfg.Protocol
			f.mu.Unlock()

			return nil
		},
		Handler: func(conn *websocket.Conn) {
			conn.PayloadType = websocket.BinaryFrame
			_ = websocket.Message.Send(conn, []byte("login: "))

			for {
				var b []byte
				if err := websocket.Message.Receive(conn, &b); err != nil {
					return
				}

				f.mu.Lock()
				f.received = append(f.received, b)
				f.mu.Unlock()

				if string(b) == "poweroff" {
					return
				}

				_ = websocket.Message.Send(conn, b)
			}
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"gitVersion":"v1.34.0"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/apis", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(apisKubeVirtActions))
	})
	mux.HandleFunc(vmConsoleVMIPath, func(w http.ResponseWriter, _ *http.Request) {
		if phase == "" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"kind":"Status","reason":"NotFound"}`))

			return
		}

		_, _ = w.Write([]byte(`{"metadata":{"name":"k3s-sandbox","namespace":"lab"},"status":{"phase":"` + phase + `"}}`))
	})
	mux.HandleFunc(vmConsoleConsolePath, func(w http.ResponseWriter, r *http.Request) {
		n := f.dials.Add(1)

		switch {
		case consoleStatus != 0:
			w.WriteHeader(consoleStatus)
		case n <= notReady:
			w.WriteHeader(http.StatusBadRequest)
		default:
			ws.ServeHTTP(w, r)
		}
	})

	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	f.fakeKubeAPI = &fakeKubeAPI{Server: srv}

	return f
}

func (f *fakeVMConsoleAPI) frames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, 0, len(f.received))
	for _, b := range f.received {
		out = append(out, string(b))
	}

	return out
}

// consoleRecorder is a shellRecorder that also keeps the statuses.
type consoleRecorder struct {
	*shellRecorder

	statusMu sync.Mutex
	statuses []string
}

func newConsoleRecorder() *consoleRecorder {
	return &consoleRecorder{shellRecorder: newShellRecorder()}
}

func (l *consoleRecorder) OnStatus(message string) {
	l.statusMu.Lock()
	defer l.statusMu.Unlock()
	l.statuses = append(l.statuses, message)
}

func (l *consoleRecorder) out() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.output.String()
}

// waitFor polls cond for up to 5 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}

	t.Fatalf("timed out waiting for %s", what)
}

func fastConsoleRetry(t *testing.T) {
	t.Helper()

	retry, wait := vmConsoleRetry, vmConsoleWait
	vmConsoleRetry, vmConsoleWait = 10*time.Millisecond, 2*time.Second

	t.Cleanup(func() { vmConsoleRetry, vmConsoleWait = retry, wait })
}

// TestVMConsoleRoundTrip: typed bytes reach the guest unframed, its output reaches the
// terminal unchanged, a resize sends nothing, and the guest going away is "disconnected"
// with code 0, after a 400 while the console was not ready. The open is audited.
func TestVMConsoleRoundTrip(t *testing.T) {
	withDataDir(t)
	SetAuditEvents(false)
	fastConsoleRetry(t)

	f := newFakeVMConsoleAPI(t, "Running", 2, 0)
	l := newConsoleRecorder()

	d := StartVMConsole(f.kubeconfigFor(f.URL), "admin@test", "", "lab", "k3s-sandbox", l)

	waitFor(t, "the login banner", func() bool { return strings.Contains(l.out(), "login: ") })

	d.Resize(120, 40)
	d.Write([]byte("root\r"))
	waitFor(t, "the echo", func() bool { return strings.Contains(l.out(), "root\r") })

	d.Write([]byte("poweroff"))
	l.wait(t)

	if l.code != 0 || l.message != vmConsoleDisconnected {
		t.Fatalf("exit %d %q", l.code, l.message)
	}

	if got := l.out(); got != "login: root\r" {
		t.Errorf("output %q", got)
	}

	if got := f.frames(); len(got) != 2 || got[0] != "root\r" || got[1] != "poweroff" {
		t.Errorf("frames %q", got)
	}

	if f.dials.Load() != 3 {
		t.Errorf("dialled %d times, want 2 refusals then the console", f.dials.Load())
	}

	f.mu.Lock()
	protocol := f.protocol
	f.mu.Unlock()

	if len(protocol) != 1 || protocol[0] != "plain.kubevirt.io" {
		t.Errorf("subprotocol %q", protocol)
	}

	l.statusMu.Lock()
	statuses := strings.Join(l.statuses, "|")
	l.statusMu.Unlock()

	if !strings.Contains(statuses, "Connecting to the serial console of lab/k3s-sandbox") || !strings.Contains(statuses, "Waiting for the console") {
		t.Errorf("statuses %q", statuses)
	}

	entries := readAudit(t, "", "vm-console")
	if len(entries) != 1 || entries[0].Namespace != "lab" || entries[0].Object != "VirtualMachine/k3s-sandbox" || entries[0].Outcome != auditOK {
		t.Errorf("audit %+v", entries)
	}
}

// TestVMConsoleClose: closing the session ends it as "closed", like a pod shell.
func TestVMConsoleClose(t *testing.T) {
	withDataDir(t)
	SetAuditEvents(false)

	f := newFakeVMConsoleAPI(t, "Running", 0, 0)
	l := newConsoleRecorder()

	d := StartVMConsole(f.kubeconfigFor(f.URL), "admin@test", "", "lab", "k3s-sandbox", l)
	waitFor(t, "the login banner", func() bool { return strings.Contains(l.out(), "login: ") })

	d.Close()
	l.wait(t)

	if l.code != -1 || l.message != "closed" {
		t.Fatalf("exit %d %q", l.code, l.message)
	}
}

// TestVMConsoleRefusals: a VM without a running instance, a console the credentials may not
// open, one that never gets ready, the demo and a bad name all end at once with a message.
func TestVMConsoleRefusals(t *testing.T) {
	withDataDir(t)
	SetAuditEvents(false)
	fastConsoleRetry(t)

	vmConsoleWait = 100 * time.Millisecond

	for _, tc := range []struct {
		name, phase   string
		notReady      int32
		consoleStatus int
		want          string
		dials         int32
	}{
		{"stopped", "", 0, 0, "the VM is stopped: start it first", 0},
		{"scheduling", "Scheduling", 0, 0, "the VM is Scheduling: its console opens once it is Running", 0},
		{"forbidden", "Running", 0, http.StatusForbidden, "HTTP 403", 1},
		{"never ready", "Running", 1000, 0, "did not open within 30 s", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeVMConsoleAPI(t, tc.phase, tc.notReady, tc.consoleStatus)
			l := newConsoleRecorder()

			StartVMConsole(f.kubeconfigFor(f.URL), "admin@test", "", "lab", "k3s-sandbox", l)
			l.wait(t)

			if l.code != -1 || !strings.Contains(l.message, tc.want) {
				t.Errorf("exit %d %q, want %q", l.code, l.message, tc.want)
			}

			if tc.dials >= 0 && f.dials.Load() != tc.dials {
				t.Errorf("dialled the console %d times, want %d", f.dials.Load(), tc.dials)
			}

			if tc.dials < 0 && f.dials.Load() < 2 {
				t.Errorf("never asked again: %d dials", f.dials.Load())
			}
		})
	}

	// The failed opens are audited too.
	if entries := readAudit(t, "", "vm-console"); len(entries) != 4 || entries[0].Outcome == auditOK {
		t.Errorf("audit %+v", entries)
	}

	l := newConsoleRecorder()
	StartVMConsole(demoKubeconfigForTest(t), "", "", "lab", "k3s-sandbox", l)
	l.wait(t)

	if l.code != -1 || l.message != errDemoUnavailable.Error() {
		t.Errorf("demo: exit %d %q", l.code, l.message)
	}

	l = newConsoleRecorder()
	StartVMConsole("", "", "", "Bad NS", "k3s-sandbox", l)
	l.wait(t)

	if l.code != -1 || l.message == "" {
		t.Errorf("bad name: exit %d %q", l.code, l.message)
	}
}

func TestStatusPeekConn(t *testing.T) {
	for head, want := range map[string]int{
		"HTTP/1.1 400 Bad Request\r\n": 400,
		"HTTP/1.1 101 Switching":       101,
		"garbage":                      0,
		"":                             0,
	} {
		if got := (&statusPeekConn{head: []byte(head)}).status(); got != want {
			t.Errorf("%q: %d, want %d", head, got, want)
		}
	}

	if got := hostWithPort("api.example.test"); got != "api.example.test:443" {
		t.Errorf("host %q", got)
	}

	if got := hostWithPort("10.0.0.1:6443"); got != "10.0.0.1:6443" {
		t.Errorf("host %q", got)
	}
}

// TestVMConsoleAccessCheck: the console needs the console subresource and the instance.
func TestVMConsoleAccessCheck(t *testing.T) {
	needs := kubeActionChecks["vmConsole"]
	if len(needs) != 2 {
		t.Fatalf("vmConsole needs %+v", needs)
	}

	a := needs[0].attrs
	if a.Verb != "get" || a.Group != groupKubeVirtSub || a.Resource != "virtualmachineinstances" || a.Subresource != "console" || !needs[0].namespaced {
		t.Errorf("console check %+v", needs[0])
	}

	b := needs[1].attrs
	if b.Verb != "get" || b.Group != groupKubeVirt || b.Resource != "virtualmachineinstances" || !needs[1].namespaced {
		t.Errorf("instance check %+v", needs[1])
	}
}
