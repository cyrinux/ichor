package ichorgo

import (
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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const hetznerIPID = int64(5150)

// fakeHetznerCluster is fakeHetzner with Primary IPs and their prices.
type fakeHetznerCluster struct {
	*fakeHetzner

	ipMu       sync.Mutex
	failIP     int // the status POST /primary_ips answers, after reserving when >= 500
	ipBody     map[string]any
	ipReserved bool
	ipDeletes  int
	// ipAssigned: DELETE /primary_ips answers primary_ip_assigned this many times first.
	ipAssigned int
}

func startFakeHetznerCluster(t *testing.T, f *fakeHetznerCluster) {
	t.Helper()

	if f.fakeHetzner == nil {
		f.fakeHetzner = &fakeHetzner{}
	}

	startFakeHetzner(t, f.fakeHetzner)

	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)

	client := cloudHTTP
	cloudHTTP = func() *http.Client { return srv.Client() }
	hetznerEndpoint = srv.URL

	t.Cleanup(func() { cloudHTTP = client })
}

func (f *fakeHetznerCluster) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer sample-hetzner-token" {
		f.fakeHetzner.handle(w, r)

		return
	}

	f.ipMu.Lock()
	defer f.ipMu.Unlock()

	ipPath := fmt.Sprintf("/primary_ips/%d", hetznerIPID)
	reserved := `{"id":5150,"ip":"` + newClusterHost + `"}`

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/primary_ips":
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, &f.ipBody)

		f.ipReserved = f.failIP == 0 || f.failIP >= 500
		if f.failIP != 0 {
			hetznerFail(w, f.failIP, "server_error", "something broke")

			return
		}

		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"primary_ip":`+reserved+`}`)
	case r.Method == http.MethodGet && r.URL.Path == "/primary_ips":
		items := ""
		if f.ipReserved && r.URL.Query().Get("label_selector") == fmt.Sprintf("ichor-run=%v", f.ipLabel()) {
			items = reserved
		}

		fmt.Fprint(w, `{"primary_ips":[`+items+`],"meta":{"pagination":{"next_page":null}}}`)
	case r.Method == http.MethodDelete && r.URL.Path == ipPath:
		if f.ipAssigned > 0 {
			f.ipAssigned--
			hetznerFail(w, http.StatusConflict, hetznerCodeIPAssigned, "primary ip is assigned")

			return
		}

		f.ipDeletes++
		f.ipReserved = false
		w.WriteHeader(http.StatusNoContent)
	case r.URL.Path == "/pricing":
		fmt.Fprint(w, `{"pricing":{"currency":"EUR","primary_ips":[{"type":"ipv4","prices":[{"location":"fsn1","price_hourly":{"net":"0.0008"},"price_monthly":{"net":"0.5000000000"}}]}]}}`)
	default:
		f.fakeHetzner.handle(w, r)
	}
}

func (f *fakeHetznerCluster) ipLabel() any {
	labels, _ := f.ipBody["labels"].(map[string]any)

	return labels["ichor-run"]
}

// state is what the fake holds: the IP reserved, the server created, deletes of each.
func (f *fakeHetznerCluster) state() (ipPosted, ipLeft bool, ipDeletes int, serverPosted bool, serverDeletes int) {
	f.ipMu.Lock()
	defer f.ipMu.Unlock()

	f.mu.Lock()
	defer f.mu.Unlock()

	return f.ipBody != nil, f.ipReserved, f.ipDeletes, f.postBody != nil, len(f.deleted)
}

// rawRecorder keeps every progress event as sent, for the talosconfig the secrets one carries.
type rawRecorder struct {
	*joinRecorder

	mu  sync.Mutex
	raw []string
}

func (r *rawRecorder) OnProgress(js string) {
	r.mu.Lock()
	r.raw = append(r.raw, js)
	r.mu.Unlock()

	r.joinRecorder.OnProgress(js)
}

func hetznerNewClusterSpecJSON(edit func(*hetznerNewClusterSpec)) string {
	spec := hetznerNewClusterSpec{
		Name: "lab", ServerType: "cx23", Location: "fsn1", SnapshotID: 3, TalosVersion: "v1.14.2", SSHKeyIDs: []int64{12},
	}
	if edit != nil {
		edit(&spec)
	}

	b, _ := json.Marshal(spec)

	return string(b)
}

// hetznerNewClusterRun is a run whose server boots into a fake apid: up tells whether it
// answers at all, kube whether its Kubernetes API does.
func hetznerNewClusterRun(t *testing.T, specJSON string, rec JoinListener, up, kube bool) (*hetznerNewCluster, *fakeTalos) {
	t.Helper()

	f := newFakeTalos()

	r := &hetznerNewCluster{
		tokenJSON: hetznerTestToken, listener: maskedJoinListener{rec},
		poll: joinPollInterval, interval: joinPollInterval, cleanupFor: 5 * time.Second,
	}

	if err := json.Unmarshal([]byte(specJSON), &r.spec); err != nil {
		t.Fatal(err)
	}

	r.created = func(r *hetznerNewCluster) {
		if !up {
			return
		}

		f.serveCA(t, r.draft.bundle)

		if kube {
			f.kubeconfigs = map[string][]byte{newClusterHost: []byte("apiVersion: v1\nkind: Config\n")}
		}

		f.setVersion(newClusterHost, "v1.14.2")
	}

	return r, f
}

func TestHetznerNewClusterPhases(t *testing.T) {
	fh := &fakeHetznerCluster{}
	startFakeHetznerCluster(t, fh)

	rec := &rawRecorder{joinRecorder: newJoinRecorder()}
	r, f := hetznerNewClusterRun(t, hetznerNewClusterSpecJSON(nil), rec, true, true)
	f.bootstrapErrs = []error{status.Error(codes.FailedPrecondition, "bootstrap is not available yet")}

	r.start(time.Minute)

	address, errMessage, events := rec.wait(t)
	if errMessage != "" || address != newClusterHost {
		t.Fatalf("got %q, %q", address, errMessage)
	}

	want := []string{newClusterPhaseIP, newClusterPhaseSecrets, hetznerPhaseCreating, hetznerPhaseStarting, newClusterPhaseBooted, newClusterPhaseBootstrapping, newClusterPhaseReady}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases %v, want %v", got, want)
	}

	checkSecretsEvent(t, rec.raw)

	fh.mu.Lock()
	public, _ := fh.postBody["public_net"].(map[string]any)
	userData, _ := fh.postBody["user_data"].(string)
	fh.mu.Unlock()

	if public["ipv4"] != float64(hetznerIPID) || !strings.Contains(userData, "type: controlplane") {
		t.Errorf("POST /servers public_net %v, user data %d bytes", public, len(userData))
	}

	if !strings.Contains(userData, "https://"+newClusterHost+":6443") {
		t.Error("the config's endpoint is not the Primary IP")
	}

	if fh.ipBody["location"] != "fsn1" || fh.ipBody["type"] != "ipv4" {
		t.Errorf("POST /primary_ips %v", fh.ipBody)
	}

	if _, ipLeft, ipDeletes, _, serverDeletes := fh.state(); !ipLeft || ipDeletes+serverDeletes != 0 {
		t.Errorf("a finished cluster lost something: ip kept %v, %d+%d deletes", ipLeft, ipDeletes, serverDeletes)
	}

	if len(f.bootstraps) != 2 {
		t.Errorf("%d bootstraps, want one refusal then one success", len(f.bootstraps))
	}
}

// checkSecretsEvent: one event carries the talosconfig, the secrets one, and its message
// does not.
func checkSecretsEvent(t *testing.T, raw []string) {
	t.Helper()

	carried := 0

	for _, js := range raw {
		var p newClusterProgress
		if err := json.Unmarshal([]byte(js), &p); err != nil {
			t.Fatal(err)
		}

		if p.Talosconfig == "" {
			continue
		}

		carried++

		if p.Phase != newClusterPhaseSecrets || !strings.Contains(p.Talosconfig, "endpoints:") || strings.Contains(p.Message, "endpoints") {
			t.Errorf("talosconfig on %q: %q", p.Phase, p.Message)
		}
	}

	if carried != 1 {
		t.Errorf("%d events carry the talosconfig, want 1", carried)
	}
}

// Everything that can be refused is refused before any write.
func TestHetznerNewClusterRefusedBeforeWrite(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		fake       *fakeHetzner
		edit       func(*hetznerNewClusterSpec)
	}{
		{name: "read-only token", want: "read-only", fake: &fakeHetzner{readOnly: true}},
		{name: "architecture", want: "pick another", fake: &fakeHetzner{imageArch: hetznerArchARM}},
		{name: "name", want: "not a cluster name", edit: func(s *hetznerNewClusterSpec) { s.Name = "my lab" }},
		{name: "snapshot", want: "pick a server type", edit: func(s *hetznerNewClusterSpec) { s.SnapshotID = 0 }},
		{name: "location", want: "not sold in", edit: func(s *hetznerNewClusterSpec) { s.Location = "hel1" }},
		{name: "kubernetes", want: "Kubernetes version", edit: func(s *hetznerNewClusterSpec) { s.KubernetesVersion = "latest" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fh := &fakeHetznerCluster{fakeHetzner: tc.fake}
			startFakeHetznerCluster(t, fh)

			rec := newJoinRecorder()
			r, _ := hetznerNewClusterRun(t, hetznerNewClusterSpecJSON(tc.edit), rec, true, true)
			r.start(time.Minute)

			_, errMessage, _ := rec.wait(t)
			if !strings.Contains(errMessage, tc.want) {
				t.Errorf("got %q, want %q", errMessage, tc.want)
			}

			if ipPosted, _, _, serverPosted, _ := fh.state(); ipPosted || serverPosted {
				t.Errorf("wrote before refusing: ip %v, server %v", ipPosted, serverPosted)
			}
		})
	}
}

// The server refused: the IP this run reserved is deleted, once its delete is accepted.
func TestHetznerNewClusterServerRefusedDeletesIP(t *testing.T) {
	fh := &fakeHetznerCluster{fakeHetzner: &fakeHetzner{failCreate: http.StatusUnprocessableEntity}, ipAssigned: 1}
	startFakeHetznerCluster(t, fh)

	rec := newJoinRecorder()
	r, _ := hetznerNewClusterRun(t, hetznerNewClusterSpecJSON(nil), rec, true, true)
	r.start(time.Minute)

	_, errMessage, events := rec.wait(t)
	if !strings.Contains(errMessage, "something broke") || !strings.Contains(errMessage, "are deleted") {
		t.Errorf("got %q", errMessage)
	}

	if _, ipLeft, ipDeletes, _, serverDeletes := fh.state(); ipLeft || ipDeletes != 1 || serverDeletes != 0 {
		t.Errorf("ip left %v, %d ip deletes, %d server deletes", ipLeft, ipDeletes, serverDeletes)
	}

	if got := joinPhasesOf(events); got[len(got)-1] != cloudPhaseCleanup {
		t.Errorf("phases %v end without the cleanup", got)
	}
}

// POST /primary_ips with no clear answer: the IP is found by the run's label and deleted.
func TestHetznerNewClusterUnansweredIP(t *testing.T) {
	fh := &fakeHetznerCluster{failIP: http.StatusBadGateway}
	startFakeHetznerCluster(t, fh)

	rec := newJoinRecorder()
	r, _ := hetznerNewClusterRun(t, hetznerNewClusterSpecJSON(nil), rec, true, true)
	r.start(time.Minute)

	if _, errMessage, _ := rec.wait(t); !strings.Contains(errMessage, "are deleted") {
		t.Errorf("got %q", errMessage)
	}

	if _, ipLeft, ipDeletes, serverPosted, _ := fh.state(); ipLeft || ipDeletes != 1 || serverPosted {
		t.Errorf("ip left %v, %d deletes, server posted %v", ipLeft, ipDeletes, serverPosted)
	}
}

// POST /servers with no clear answer: the server is found by the run's label; both go.
func TestHetznerNewClusterUnansweredServer(t *testing.T) {
	fh := &fakeHetznerCluster{fakeHetzner: &fakeHetzner{failCreate: http.StatusBadGateway}}
	startFakeHetznerCluster(t, fh)

	rec := newJoinRecorder()
	r, _ := hetznerNewClusterRun(t, hetznerNewClusterSpecJSON(nil), rec, true, true)
	r.start(time.Minute)

	if _, errMessage, _ := rec.wait(t); !strings.Contains(errMessage, "are deleted") {
		t.Errorf("got %q", errMessage)
	}

	if _, ipLeft, ipDeletes, _, serverDeletes := fh.state(); ipLeft || ipDeletes != 1 || serverDeletes != 1 {
		t.Errorf("ip left %v, %d ip deletes, %d server deletes", ipLeft, ipDeletes, serverDeletes)
	}
}

// The server never answers: at the deadline both the server and the IP are deleted.
func TestHetznerNewClusterNeverBootsDeletesBoth(t *testing.T) {
	fh := &fakeHetznerCluster{}
	startFakeHetznerCluster(t, fh)

	rec := newJoinRecorder()
	r, f := hetznerNewClusterRun(t, hetznerNewClusterSpecJSON(nil), rec, false, false)
	r.start(2 * time.Second)

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "was not ready within") || !strings.Contains(errMessage, "are deleted") {
		t.Errorf("got %q", errMessage)
	}

	if _, ipLeft, ipDeletes, _, serverDeletes := fh.state(); ipLeft || ipDeletes != 1 || serverDeletes != 1 {
		t.Errorf("ip left %v, %d ip deletes, %d server deletes", ipLeft, ipDeletes, serverDeletes)
	}

	if len(f.bootstraps) != 0 {
		t.Error("bootstrapped a node that never answered")
	}
}

// Bootstrapped, then Kubernetes never answers: the cluster exists and nothing is deleted.
func TestHetznerNewClusterKeptAfterBootstrap(t *testing.T) {
	fh := &fakeHetznerCluster{}
	startFakeHetznerCluster(t, fh)

	rec := newJoinRecorder()
	r, _ := hetznerNewClusterRun(t, hetznerNewClusterSpecJSON(nil), rec, true, false)
	r.start(3 * time.Second)

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "is kept") || !strings.Contains(errMessage, "server 4711") {
		t.Errorf("got %q", errMessage)
	}

	if _, ipLeft, ipDeletes, _, serverDeletes := fh.state(); !ipLeft || ipDeletes+serverDeletes != 0 {
		t.Errorf("deleted a bootstrapped cluster: ip kept %v, %d+%d deletes", ipLeft, ipDeletes, serverDeletes)
	}
}

// Cancel after the server was created: both are deleted.
func TestHetznerNewClusterCancelDeletes(t *testing.T) {
	fh := &fakeHetznerCluster{}
	startFakeHetznerCluster(t, fh)

	rec := newJoinRecorder()
	r, _ := hetznerNewClusterRun(t, hetznerNewClusterSpecJSON(nil), rec, false, false)

	var run *JoinRun

	created := make(chan struct{})
	r.created = func(*hetznerNewCluster) { close(created) }
	run = r.start(time.Minute)

	<-created
	run.Cancel()

	_, errMessage, _ := rec.wait(t)
	if !strings.HasPrefix(errMessage, "stopped") || !strings.Contains(errMessage, "are deleted") {
		t.Errorf("got %q", errMessage)
	}

	if _, ipLeft, ipDeletes, _, serverDeletes := fh.state(); ipLeft || ipDeletes != 1 || serverDeletes != 1 {
		t.Errorf("ip left %v, %d ip deletes, %d server deletes", ipLeft, ipDeletes, serverDeletes)
	}
}

// Delete refused for good: the message names what is still billed.
func TestHetznerNewClusterCleanupFailureNamesWhatIsBilled(t *testing.T) {
	fh := &fakeHetznerCluster{fakeHetzner: &fakeHetzner{failCreate: http.StatusUnprocessableEntity}, ipAssigned: 1000}
	startFakeHetznerCluster(t, fh)

	rec := newJoinRecorder()
	r, _ := hetznerNewClusterRun(t, hetznerNewClusterSpecJSON(nil), rec, true, true)
	r.cleanupFor = 200 * time.Millisecond
	r.start(time.Minute)

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "still billed: Primary IP 5150") {
		t.Errorf("got %q", errMessage)
	}
}

func hetznerDemoConfig(t *testing.T) string {
	t.Helper()
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	return cfg
}

func TestHetznerNewClusterRefusals(t *testing.T) {
	rec := newJoinRecorder()
	StartHetznerNewCluster(hetznerDemoConfig(t), "", hetznerTestToken, hetznerNewClusterSpecJSON(nil), rec)

	if _, errMessage, _ := rec.wait(t); errMessage != errDemoUnavailable.Error() {
		t.Errorf("demo: got %q", errMessage)
	}

	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	rec = newJoinRecorder()
	StartHetznerNewCluster("", "", hetznerTestToken, hetznerNewClusterSpecJSON(nil), rec)

	if _, errMessage, _ := rec.wait(t); errMessage != errJoinPrivacy.Error() {
		t.Errorf("privacy: got %q", errMessage)
	}
}

func TestHetznerNewClusterEstimate(t *testing.T) {
	startFakeHetznerCluster(t, &fakeHetznerCluster{})

	out, err := HetznerNewClusterEstimate("", "", hetznerTestToken, "cx23", "fsn1")
	if err != nil {
		t.Fatal(err)
	}

	var cost hetznerNewClusterCost
	if err := json.Unmarshal([]byte(out), &cost); err != nil {
		t.Fatal(err)
	}

	want := hetznerNewClusterCost{Currency: "EUR", ServerHourly: "0.0060", ServerMonthly: "3.49", PrimaryIPMonthly: "0.50", TotalMonthly: "3.99"}
	if cost != want {
		t.Errorf("got %+v, want %+v", cost, want)
	}

	if _, err := HetznerNewClusterEstimate("", "", hetznerTestToken, "cx23", "hel1"); err == nil || !strings.Contains(err.Error(), "not sold in") {
		t.Errorf("hel1: %v", err)
	}

	demo, err := HetznerNewClusterEstimate(hetznerDemoConfig(t), "", "", "cx23", "fsn1")
	if err != nil || !strings.Contains(demo, "totalMonthly") {
		t.Errorf("demo: %q, %v", demo, err)
	}
}
