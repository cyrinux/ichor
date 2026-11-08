package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

const (
	hetznerTestToken = `{"token":"sample-hetzner-token"}`
	hetznerServerID  = int64(4711)
)

// fakeHetzner is the part of Hetzner's API the app calls. It records every write.
type fakeHetzner struct {
	mu sync.Mutex

	readOnly     bool
	actionStatus string
	imageArch    string
	failCreate   int // the status POST /servers answers, after creating when >= 500
	postBody     map[string]any
	created      bool
	deleted      []string
	onRunning    func()
}

func startFakeHetzner(t *testing.T, f *fakeHetzner) {
	t.Helper()

	if f.actionStatus == "" {
		f.actionStatus = "success"
	}

	if f.imageArch == "" {
		f.imageArch = hetznerArchX86
	}

	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)

	client, endpoint := cloudHTTP, hetznerEndpoint
	cloudHTTP = func() *http.Client { return srv.Client() }
	hetznerEndpoint = srv.URL

	t.Cleanup(func() { cloudHTTP, hetznerEndpoint = client, endpoint })
}

func hetznerFail(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":{"code":%q,"message":%q}}`, code, message)
}

func (f *fakeHetzner) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("Authorization") != "Bearer sample-hetzner-token" {
		hetznerFail(w, http.StatusUnauthorized, "unauthorized", "unable to authenticate")

		return
	}

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/ssh_keys":
		if f.readOnly {
			hetznerFail(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		} else {
			hetznerFail(w, http.StatusUnprocessableEntity, "invalid_input", "name is required")
		}
	case r.Method == http.MethodPost && r.URL.Path == "/servers":
		f.createServer(w, r)
	case r.Method == http.MethodDelete && r.URL.Path == fmt.Sprintf("/servers/%d", hetznerServerID):
		f.deleted = append(f.deleted, r.URL.Path)
		fmt.Fprint(w, `{"action":{"id":9,"status":"running"}}`)
	case r.URL.Path == fmt.Sprintf("/servers/%d", hetznerServerID):
		if f.onRunning != nil {
			f.onRunning()
			f.onRunning = nil
		}

		fmt.Fprint(w, `{"server":{"id":4711,"status":"running","public_net":{"ipv4":{"ip":"192.0.2.20"},"ipv6":{"ip":"2001:db8::/64"}}}}`)
	case r.URL.Path == "/servers":
		f.listServers(w, r)
	case r.URL.Path == "/actions/7":
		fmt.Fprintf(w, `{"action":{"id":7,"status":%q,"error":{"code":"action_failed","message":"no capacity"}}}`, f.actionStatus)
	case r.URL.Path == "/pricing":
		fmt.Fprint(w, `{"pricing":{"currency":"EUR","vat_rate":"19.00"}}`)
	case r.URL.Path == "/server_types":
		fmt.Fprint(w, hetznerTypesPage(r))
	case r.URL.Path == "/locations":
		fmt.Fprint(w, `{"locations":[{"name":"fsn1","description":"Falkenstein","city":"Falkenstein","country":"DE"}],"meta":{"pagination":{"next_page":null}}}`)
	case r.URL.Path == "/images":
		fmt.Fprint(w, `{"images":[
			{"id":1,"description":"old backup base","labels":{},"created":"2026-09-01T00:00:00+00:00","architecture":"x86","os_flavor":"debian"},
			{"id":2,"description":"Talos v1.14 older","labels":{},"created":"2026-08-01T00:00:00+00:00","architecture":"x86","os_flavor":"unknown"},
			{"id":3,"description":"node image","labels":{"os":"talos"},"created":"2026-10-01T00:00:00+00:00","architecture":"arm","os_flavor":"unknown"}
		],"meta":{"pagination":{"next_page":null}}}`)
	case r.URL.Path == "/images/3":
		fmt.Fprintf(w, `{"image":{"id":3,"architecture":%q}}`, f.imageArch)
	default:
		hetznerFail(w, http.StatusNotFound, "not_found", r.URL.Path)
	}
}

func (f *fakeHetzner) createServer(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(data, &f.postBody)

	if f.failCreate != 0 {
		f.created = f.failCreate >= 500
		hetznerFail(w, f.failCreate, "server_error", "something broke")

		return
	}

	f.created = true

	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, `{"server":{"id":4711,"status":"initializing"},"action":{"id":7,"status":"running"},"next_actions":[],"root_password":"never-shown"}`)
}

func (f *fakeHetzner) listServers(w http.ResponseWriter, r *http.Request) {
	selector := r.URL.Query().Get("label_selector")

	labels, _ := f.postBody["labels"].(map[string]any)
	if selector != "" && (!f.created || selector != fmt.Sprintf("ichor-run=%v", labels["ichor-run"])) {
		fmt.Fprint(w, `{"servers":[],"meta":{"pagination":{"next_page":null}}}`)

		return
	}

	fmt.Fprint(w, `{"servers":[{"id":4711,"status":"running"}],"meta":{"pagination":{"next_page":null}}}`)
}

// hetznerTypesPage answers two pages, or the one type asked by name.
func hetznerTypesPage(r *http.Request) string {
	price := `{"location":"fsn1","price_hourly":{"net":"0.0060000000","gross":"0.0071"},"price_monthly":{"net":"3.4900000000","gross":"4.15"}}`
	x86 := `{"name":"cx23","cores":2,"memory":4,"disk":40,"architecture":"x86","deprecation":null,"prices":[` + price + `],"locations":[{"name":"fsn1","deprecation":null}]}`
	old := `{"name":"cx11","cores":1,"memory":2,"disk":20,"architecture":"x86","deprecation":{"announced":"2026-01-01T00:00:00+00:00"},"prices":[` + price + `]}`
	gone := `{"name":"cx12","cores":1,"memory":2,"disk":20,"architecture":"x86","deprecation":null,"prices":[` + price + `],"locations":[{"name":"fsn1","deprecation":{"announced":"2026-01-01T00:00:00+00:00"}}]}`

	if r.URL.Query().Get("name") != "" {
		return `{"server_types":[` + x86 + `],"meta":{"pagination":{"next_page":null}}}`
	}

	if r.URL.Query().Get("page") == "2" {
		return `{"server_types":[` + gone + `],"meta":{"pagination":{"next_page":null}}}`
	}

	return `{"server_types":[` + x86 + `,` + old + `],"meta":{"pagination":{"next_page":2}}}`
}

func TestHetznerTest(t *testing.T) {
	f := &fakeHetzner{}
	startFakeHetzner(t, f)

	out, err := HetznerTest(hetznerTestToken)
	if r := decodeJSON[hetznerTestResult](t, out, err); r.ReadOnly || r.Servers != 1 {
		t.Errorf("read-write token = %+v", r)
	}

	f.readOnly = true

	out, err = HetznerTest(hetznerTestToken)
	if r := decodeJSON[hetznerTestResult](t, out, err); !r.ReadOnly {
		t.Errorf("read-only token = %+v", r)
	}

	for _, token := range []string{`{"token":"wrong-token-value"}`, `{"token":"`, ""} {
		_, err := HetznerTest(token)
		if err == nil || strings.Contains(err.Error(), "wrong-token-value") {
			t.Errorf("token %q: err = %v", token, err)
		}
	}
}

func TestHetznerOptions(t *testing.T) {
	startFakeHetzner(t, &fakeHetzner{})

	cfg := newFakeTalos().start(t, joinTemplateNode)

	out, err := HetznerOptions(cfg, "fake", hetznerTestToken)
	o := decodeJSON[hetznerOptions](t, out, err)

	if o.Currency != "EUR" || len(o.Locations) != 1 {
		t.Errorf("options = %+v", o)
	}

	if len(o.ServerTypes) != 1 || o.ServerTypes[0].Name != "cx23" || o.ServerTypes[0].Arch != bootArchAMD64 {
		t.Fatalf("types = %+v, want cx23 only (both pages read, the deprecated ones hidden)", o.ServerTypes)
	}

	if p := o.ServerTypes[0].Prices[0]; p.Hourly != "0.0060" || p.Monthly != "3.49" {
		t.Errorf("price = %+v", p)
	}

	var ids []int64
	for _, s := range o.Snapshots {
		ids = append(ids, s.ID)
	}

	if !slices.Equal(ids, []int64{3, 2, 1}) || !o.Snapshots[0].Talos || o.Snapshots[2].Talos || o.Snapshots[0].Arch != bootArchARM64 {
		t.Errorf("snapshots = %+v", o.Snapshots)
	}
}

func TestHetznerDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := HetznerOptions(cfg, "", "")
	if o := decodeJSON[hetznerOptions](t, out, err); len(o.ServerTypes) == 0 || len(o.Snapshots) == 0 || !o.Snapshots[0].Talos {
		t.Errorf("demo options = %+v", o)
	}

	rec := newJoinRecorder()
	StartHetznerCreate(cfg, "", "", `{"hostname":"node-c"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo create = %q", msg)
	}
}

func hetznerTestRun(cfg, role string, rec *joinRecorder) *hetznerCreate {
	return &hetznerCreate{
		join: &nodeJoin{
			config: cfg, context: "fake", listener: maskedJoinListener{rec}, interval: joinPollInterval,
			req: joinConfigRequest{template: joinTemplateNode, role: role, hostname: "node-c", installDisk: "/dev/sda"},
		},
		spec: hetznerCreateSpec{
			ServerType: "cx23", Location: "fsn1", SnapshotID: 3, Hostname: "node-c", Role: role,
			TemplateNode: joinTemplateNode, InstallDisk: "/dev/sda",
		},
		tokenJSON: hetznerTestToken, poll: joinPollInterval,
	}
}

// hetznerNodeJoins plays the node once its server is asked for: it boots with the cluster's
// identity, becomes ready, and its Kubernetes node Ready.
func hetznerNodeJoins(t *testing.T, f *fakeTalos, typ machine.Type) func() {
	t.Helper()

	return func() {
		go func() {
			time.Sleep(joinStepDelay)
			f.addNode(t, joinNewNode, "v1.14.2", typ)
			f.setStage(joinNewNode, runtime.MachineStageBooting, false)

			time.Sleep(joinStepDelay)
			f.setStage(joinNewNode, runtime.MachineStageRunning, true)

			time.Sleep(joinStepDelay)

			status := k8s.NewNodeStatus(k8s.NamespaceName, "node-c")
			status.TypedSpec().Nodename, status.TypedSpec().NodeReady = "node-c", true
			f.put(joinNewNode, status)
		}()
	}
}

func TestHetznerCreateJoins(t *testing.T) {
	withDataDir(t)

	talos, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeHetzner{imageArch: hetznerArchX86, onRunning: hetznerNodeJoins(t, talos, machine.TypeWorker)}
	startFakeHetzner(t, f)

	rec := newJoinRecorder()
	hetznerTestRun(cfg, joinRoleWorker, rec).start(time.Minute)

	node, msg, events := rec.wait(t)
	if msg != "" || node != joinNewNode {
		t.Fatalf("node %q, error %q, events %+v", node, msg, events)
	}

	want := []string{hetznerPhaseCreating, hetznerPhaseStarting, joinPhaseRebooting, joinPhaseBooted, joinPhaseJoining, joinPhaseRegistering, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	b := f.postBody
	userData, _ := b["user_data"].(string)
	labels, _ := b["labels"].(map[string]any)

	if b["name"] != "node-c" || b["image"] != "3" || b["server_type"] != "cx23" || b["location"] != "fsn1" ||
		!strings.Contains(userData, "node-c") || labels["ichor-run"] == nil || labels["ichor-cluster"] == "fake" {
		t.Errorf("POST /servers = %+v", b)
	}

	if len(f.deleted) != 0 {
		t.Errorf("deleted = %v after a success", f.deleted)
	}

	if len(talos.applies) != 0 {
		t.Errorf("applies = %+v: the config came as user data", talos.applies)
	}

	for _, e := range events {
		if strings.Contains(e.Message, "never-shown") || strings.Contains(e.Message, "sample-hetzner-token") {
			t.Errorf("a progress line carries a secret: %q", e.Message)
		}
	}

	entries := readAudit(t, "fake", "hetzner-server-create")
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "server=4711") ||
		strings.Contains(entries[0].Params, "sample-hetzner-token") || strings.Contains(entries[0].Params, "v1alpha1") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestHetznerCreateFailures(t *testing.T) {
	cases := []struct {
		name        string
		fake        *fakeHetzner
		role        string
		wantInError string
		wantPost    bool
		wantDeleted bool
	}{
		{
			name: "the create action fails: the server is deleted", fake: &fakeHetzner{actionStatus: hetznerStatusError}, role: joinRoleWorker,
			wantInError: "no capacity", wantPost: true, wantDeleted: true,
		},
		{
			name: "read-only token: refused before any write", fake: &fakeHetzner{readOnly: true}, role: joinRoleWorker,
			wantInError: "read-only",
		},
		{
			name: "snapshot of another architecture: refused before any write", fake: &fakeHetzner{imageArch: hetznerArchARM}, role: joinRoleWorker,
			wantInError: "arm64",
		},
		{
			name: "a refused POST created nothing", fake: &fakeHetzner{failCreate: http.StatusUnprocessableEntity}, role: joinRoleWorker,
			wantInError: "something broke", wantPost: true,
		},
		{
			name: "a POST without a clear answer: found by its label and deleted", fake: &fakeHetzner{failCreate: http.StatusBadGateway}, role: joinRoleWorker,
			wantInError: "was deleted", wantPost: true, wantDeleted: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)

			f := c.fake
			startFakeHetzner(t, f)

			rec := newJoinRecorder()
			hetznerTestRun(cfg, c.role, rec).start(time.Minute)

			_, msg, _ := rec.wait(t)
			if !strings.Contains(msg, c.wantInError) {
				t.Errorf("error = %q, want %q in it", msg, c.wantInError)
			}

			if (f.postBody != nil) != c.wantPost {
				t.Errorf("POST /servers sent = %v, want %v", f.postBody != nil, c.wantPost)
			}

			if (len(f.deleted) == 1) != c.wantDeleted {
				t.Errorf("deleted = %v, want deleted %v", f.deleted, c.wantDeleted)
			}
		})
	}
}

func TestHetznerCreateUserDataCap(t *testing.T) {
	if err := checkUserDataSize(cloudUserData{Size: cloudUserDataLimit}); err != nil {
		t.Errorf("at the limit: %v", err)
	}

	if err := checkUserDataSize(cloudUserData{Size: cloudUserDataLimit + 1}); err == nil {
		t.Error("over the limit was accepted")
	}
}

func TestHetznerCreateStopDeletes(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	var run *JoinRun

	started := make(chan struct{})
	f := &fakeHetzner{onRunning: func() { close(started) }}
	startFakeHetzner(t, f)

	rec := newJoinRecorder()
	run = hetznerTestRun(cfg, joinRoleWorker, rec).start(time.Minute)

	<-started
	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "was deleted") || len(f.deleted) != 1 {
		t.Errorf("error = %q, deleted = %v", msg, f.deleted)
	}
}

// A control plane that answered may already be an etcd member: its server stays.
func TestHetznerCreateKeepsAControlPlaneThatAnswered(t *testing.T) {
	withDataDir(t)

	talos, cfg, _ := joinCluster(t, machine.TypeControlPlane)

	answered := make(chan struct{})
	f := &fakeHetzner{onRunning: func() {
		talos.addNode(t, joinNewNode, "v1.14.2", machine.TypeControlPlane)
		close(answered)
	}}
	startFakeHetzner(t, f)

	rec := newJoinRecorder()
	run := hetznerTestRun(cfg, joinRoleControlPlane, rec).start(time.Minute)

	<-answered
	time.Sleep(5 * joinPollInterval)
	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "is kept") || len(f.deleted) != 0 {
		t.Errorf("error = %q, deleted = %v", msg, f.deleted)
	}
}

func TestHetznerWaitOmni(t *testing.T) {
	rec := newJoinRecorder()
	h := hetznerTestRun("", "", rec)

	calls := 0
	h.omniHostnames = func(context.Context, *session) ([]string, error) {
		calls++
		if calls < 3 {
			return []string{"other"}, nil
		}

		return []string{"other", "NODE-C"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.waitOmni(ctx, nil); err != nil || calls != 3 {
		t.Errorf("waitOmni = %v after %d calls", err, calls)
	}
}

func TestOmniMachineHostnames(t *testing.T) {
	useMemAuthStore(t)

	f := &fakeOmniAPI{machineStatuses: []string{"network:\n  hostname: node-c\n", "connected: true\n"}}
	endpoint := startFakeOmniAPI(t, f)
	f.key = signInTestAccount(t, endpoint)

	cfgCtx, err := omniInstanceContext(endpoint, testUserIdentity)
	if err != nil {
		t.Fatal(err)
	}

	s := &session{context: cfgCtx, signing: &omniSigning{method: omniMethodName(cfgCtx), signer: omniSigner{identity: testUserIdentity, key: f.key}}}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	names, err := omniMachineHostnames(ctx, s)
	if err != nil || !slices.Equal(names, []string{"node-c"}) {
		t.Errorf("hostnames = %v, %v", names, err)
	}
}
