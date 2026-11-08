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

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

const (
	ovhTestCred     = `{"endpoint":"eu","clientId":"sample-id","clientSecret":"sample-ovh-secret","project":"scratch"}`
	ovhTestInstance = "instance-1"
)

// fakeOVH is the part of OVHcloud's API the app calls. It records every write.
type fakeOVH struct {
	mu sync.Mutex

	expireFirstToken bool
	noCreateRight    bool
	noCatalog        bool
	buildFails       bool
	createStatus     int
	tokens           int
	postBody         map[string]any
	deleted          []string
	onActive         func()
}

func startFakeOVH(t *testing.T, f *fakeOVH) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(srv.Close)

	client, sites := cloudHTTP, ovhSites
	cloudHTTP = func() *http.Client { return srv.Client() }
	ovhSites = map[string]ovhSite{ovhSiteEU: {api: srv.URL + "/1.0", token: srv.URL + "/token", catalog: "FR"}}

	t.Cleanup(func() { cloudHTTP, ovhSites = client, sites })
}

func ovhFail(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"message":%q}`, message)
}

func (f *fakeOVH) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch r.URL.Path {
	case "/token":
		f.token(w, r)

		return
	case "/1.0/order/catalog/public/cloud":
		if f.noCatalog {
			ovhFail(w, http.StatusBadGateway, "no catalog")

			return
		}

		fmt.Fprint(w, `{"locale":{"currencyCode":"EUR"},"addons":[{"planCode":"b3-8.consumption","pricings":[{"intervalUnit":"hour","price":5120000}]}]}`)

		return
	}

	if f.expireFirstToken && r.Header.Get("Authorization") == "Bearer token-1" {
		ovhFail(w, http.StatusUnauthorized, "expired")

		return
	}

	const project = "/1.0/cloud/project/scratch"

	switch {
	case r.URL.Path == "/1.0/cloud/project":
		fmt.Fprint(w, `["other","scratch"]`)
	case r.URL.Path == project+"/region":
		fmt.Fprint(w, `["GRA11","DE1"]`)
	case r.URL.Path == project+"/flavor":
		fmt.Fprint(w, `[
			{"id":"f-big","name":"b3-16","region":"GRA11","vcpus":4,"ram":16,"disk":100,"osType":"linux","available":true,"planCodes":{"hourly":"b3-16.consumption"}},
			{"id":"f-small","name":"b3-8","region":"GRA11","vcpus":2,"ram":8,"disk":50,"osType":"linux","available":true,"planCodes":{"hourly":"b3-8.consumption"}},
			{"id":"f-win","name":"win-b3-8","region":"GRA11","vcpus":2,"ram":8,"disk":50,"osType":"windows","available":true},
			{"id":"f-gone","name":"b2-7","region":"GRA11","vcpus":2,"ram":7,"disk":50,"osType":"linux","available":false}
		]`)
	case r.URL.Path == project+"/image":
		fmt.Fprint(w, `[
			{"id":"i-debian","name":"Debian 12","region":"GRA11","type":"linux","status":"active","visibility":"public"},
			{"id":"i-own","name":"node base","region":"GRA11","type":"linux","status":"active","visibility":"private"},
			{"id":"i-talos","name":"Talos v1.14","region":"GRA11","type":"linux","status":"active","visibility":"private"},
			{"id":"i-win","name":"Windows","region":"GRA11","type":"windows","status":"active","visibility":"public"}
		]`)
	case r.Method == http.MethodPost && r.URL.Path == project+"/instance":
		f.create(w, r)
	case r.Method == http.MethodDelete && r.URL.Path == project+"/instance/"+ovhTestInstance:
		f.deleted = append(f.deleted, r.URL.Path)
	case r.URL.Path == project+"/instance/"+ovhTestInstance:
		f.instance(w)
	default:
		ovhFail(w, http.StatusNotFound, r.URL.Path)
	}
}

func (f *fakeOVH) token(w http.ResponseWriter, r *http.Request) {
	id, secret, _ := r.BasicAuth()
	if id != "sample-id" || secret != "sample-ovh-secret" || r.FormValue("grant_type") != "client_credentials" {
		ovhFail(w, http.StatusUnauthorized, "bad secret sample-ovh-secret")

		return
	}

	f.tokens++
	fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":3600}`, f.tokens)
}

func (f *fakeOVH) create(w http.ResponseWriter, r *http.Request) {
	if f.noCreateRight {
		ovhFail(w, http.StatusForbidden, "action publicCloudProject:apiovh:instance/create denied")

		return
	}

	data, _ := io.ReadAll(r.Body)

	var body map[string]any
	_ = json.Unmarshal(data, &body)

	if len(body) == 0 {
		ovhFail(w, http.StatusBadRequest, "name is required")

		return
	}

	f.postBody = body

	if f.createStatus != 0 {
		ovhFail(w, f.createStatus, "something broke")

		return
	}

	fmt.Fprintf(w, `{"id":%q,"status":"BUILD"}`, ovhTestInstance)
}

func (f *fakeOVH) instance(w http.ResponseWriter) {
	if f.buildFails {
		fmt.Fprintf(w, `{"id":%q,"status":"ERROR"}`, ovhTestInstance)

		return
	}

	if f.onActive != nil {
		f.onActive()
		f.onActive = nil
	}

	fmt.Fprintf(w, `{"id":%q,"status":"ACTIVE","ipAddresses":[{"ip":"2001:db8::20","version":6,"type":"public"},{"ip":"10.0.0.4","version":4,"type":"private"},{"ip":"192.0.2.20","version":4,"type":"public"}]}`, ovhTestInstance)
}

func TestOVHTest(t *testing.T) {
	cases := []struct {
		name string
		cred string
		fake *fakeOVH
		want ovhTestResult
		fail string
	}{
		{name: "may create", cred: ovhTestCred, fake: &fakeOVH{}, want: ovhTestResult{Project: "scratch", CanCreate: true, Checked: true}},
		{name: "may not create", cred: ovhTestCred, fake: &fakeOVH{noCreateRight: true}, want: ovhTestResult{Project: "scratch", Checked: true}},
		{name: "a token that ended is renewed", cred: ovhTestCred, fake: &fakeOVH{expireFirstToken: true}, want: ovhTestResult{Project: "scratch", CanCreate: true, Checked: true}},
		{name: "a wrong secret", cred: `{"clientId":"sample-id","clientSecret":"wrong"}`, fake: &fakeOVH{}, fail: "check its id and secret"},
		{name: "no account", cred: "", fake: &fakeOVH{}, fail: "service account first"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			startFakeOVH(t, c.fake)

			out, err := OVHTest(c.cred)
			if c.fail != "" {
				if err == nil || !strings.Contains(err.Error(), c.fail) || strings.Contains(err.Error(), "secret sample") {
					t.Fatalf("err = %v, want %q in it", err, c.fail)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			var got ovhTestResult
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}

			if got.Project != c.want.Project || got.CanCreate != c.want.CanCreate || got.Checked != c.want.Checked || len(got.Projects) != 2 {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestOVHOptions(t *testing.T) {
	cases := []struct {
		name       string
		noCatalog  bool
		wantHourly string
		wantCur    string
	}{
		{name: "with the catalog", wantHourly: "0.0512", wantCur: "EUR"},
		{name: "without it, no price and no failure", noCatalog: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			startFakeOVH(t, &fakeOVH{noCatalog: c.noCatalog})

			out, err := OVHOptions("", "", ovhTestCred)
			if err != nil {
				t.Fatal(err)
			}

			var got ovhOptions
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}

			names := func(fn func(int) string, n int) []string {
				s := make([]string, n)
				for i := range n {
					s[i] = fn(i)
				}

				return s
			}

			flavors := names(func(i int) string { return got.Flavors[i].ID }, len(got.Flavors))
			if !slices.Equal(flavors, []string{"f-small", "f-big"}) {
				t.Errorf("flavors = %v", flavors)
			}

			images := names(func(i int) string { return got.Images[i].ID }, len(got.Images))
			if !slices.Equal(images, []string{"i-talos", "i-own", "i-debian"}) || !got.Images[0].Talos {
				t.Errorf("images = %+v", got.Images)
			}

			if got.Flavors[0].Hourly != c.wantHourly || got.Currency != c.wantCur || got.Flavors[1].Hourly != "" {
				t.Errorf("price %q %q, flavors %+v", got.Flavors[0].Hourly, got.Currency, got.Flavors)
			}

			if !slices.Equal(got.Regions, []string{"DE1", "GRA11"}) {
				t.Errorf("regions = %v", got.Regions)
			}
		})
	}
}

func TestOVHPolicyNamesTheProject(t *testing.T) {
	out := OVHServiceAccountPolicy("scratch")
	if !strings.Contains(out, "publicCloudProject:scratch") || !strings.Contains(out, "instance/create") {
		t.Errorf("policy = %s", out)
	}
}

func ovhTestRun(cfg, role string, rec *joinRecorder) *ovhCreate {
	return &ovhCreate{
		join: &nodeJoin{
			config: cfg, context: "fake", listener: maskedJoinListener{rec}, interval: joinPollInterval,
			req: joinConfigRequest{template: joinTemplateNode, role: role, hostname: "node-c", installDisk: "/dev/sda"},
		},
		spec: ovhCreateSpec{
			Region: "GRA11", FlavorID: "f-small", ImageID: "i-talos", Hostname: "node-c", Role: role,
			TemplateNode: joinTemplateNode, InstallDisk: "/dev/sda",
		},
		credJSON: ovhTestCred, poll: joinPollInterval,
	}
}

func TestOVHCreateJoins(t *testing.T) {
	withDataDir(t)

	talos, cfg, _ := joinCluster(t, machine.TypeWorker)
	f := &fakeOVH{onActive: hetznerNodeJoins(t, talos, machine.TypeWorker)}
	startFakeOVH(t, f)

	rec := newJoinRecorder()
	ovhTestRun(cfg, joinRoleWorker, rec).start(time.Minute)

	node, msg, events := rec.wait(t)
	if msg != "" || node != joinNewNode {
		t.Fatalf("node %q, error %q, events %+v", node, msg, events)
	}

	want := []string{cloudPhaseCreating, cloudPhaseStarting, joinPhaseRebooting, joinPhaseBooted, joinPhaseJoining, joinPhaseRegistering, joinPhaseDone}
	if got := joinPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	b := f.postBody
	userData, _ := b["userData"].(string)

	if b["name"] != "node-c" || b["region"] != "GRA11" || b["flavorId"] != "f-small" || b["imageId"] != "i-talos" ||
		b["monthlyBilling"] != false || !strings.Contains(userData, "node-c") {
		t.Errorf("POST /instance = %+v", b)
	}

	if len(f.deleted) != 0 || len(talos.applies) != 0 {
		t.Errorf("deleted = %v, applies = %+v after a success", f.deleted, talos.applies)
	}

	for _, e := range events {
		if strings.Contains(e.Message, "sample-ovh-secret") || strings.Contains(e.Message, "token-1") {
			t.Errorf("a progress line carries a secret: %q", e.Message)
		}
	}

	entries := readAudit(t, "fake", "ovh-instance-create")
	if len(entries) != 1 || entries[0].Outcome != auditOK || !strings.Contains(entries[0].Params, "instance="+ovhTestInstance) ||
		strings.Contains(entries[0].Params, "sample-ovh-secret") || strings.Contains(entries[0].Params, "v1alpha1") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestOVHCreateFailures(t *testing.T) {
	cases := []struct {
		name        string
		fake        *fakeOVH
		mutate      func(*ovhCreate)
		wantInError string
		wantPost    bool
		wantDeleted bool
	}{
		{
			name: "the build fails: the instance is deleted", fake: &fakeOVH{buildFails: true},
			wantInError: "was deleted", wantPost: true, wantDeleted: true,
		},
		{
			name: "no right to create: refused before any write", fake: &fakeOVH{noCreateRight: true},
			wantInError: "printed policy",
		},
		{
			name: "a flavor of another region: refused before any write", fake: &fakeOVH{},
			mutate:      func(h *ovhCreate) { h.spec.FlavorID = "nope" },
			wantInError: "cannot be ordered",
		},
		{
			name: "an image of another region: refused before any write", fake: &fakeOVH{},
			mutate:      func(h *ovhCreate) { h.spec.ImageID = "nope" },
			wantInError: "not in GRA11",
		},
		{
			name: "a refused POST created nothing", fake: &fakeOVH{createStatus: http.StatusBadRequest},
			wantInError: "something broke", wantPost: true,
		},
		{
			name: "a POST without a clear answer: nothing is deleted by name", fake: &fakeOVH{createStatus: http.StatusBadGateway},
			wantInError: "check the project", wantPost: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withDataDir(t)

			_, cfg, _ := joinCluster(t, machine.TypeWorker)
			startFakeOVH(t, c.fake)

			rec := newJoinRecorder()

			h := ovhTestRun(cfg, joinRoleWorker, rec)
			if c.mutate != nil {
				c.mutate(h)
			}

			h.start(time.Minute)

			_, msg, _ := rec.wait(t)
			if !strings.Contains(msg, c.wantInError) {
				t.Errorf("error = %q, want %q in it", msg, c.wantInError)
			}

			if (c.fake.postBody != nil) != c.wantPost {
				t.Errorf("POST sent = %v, want %v", c.fake.postBody != nil, c.wantPost)
			}

			if (len(c.fake.deleted) == 1) != c.wantDeleted {
				t.Errorf("deleted = %v, want deleted %v", c.fake.deleted, c.wantDeleted)
			}
		})
	}
}

func TestOVHCreateStopDeletes(t *testing.T) {
	withDataDir(t)

	_, cfg, _ := joinCluster(t, machine.TypeWorker)

	started := make(chan struct{})
	f := &fakeOVH{onActive: func() { close(started) }}
	startFakeOVH(t, f)

	rec := newJoinRecorder()
	run := ovhTestRun(cfg, joinRoleWorker, rec).start(time.Minute)

	<-started
	run.Cancel()

	_, msg, _ := rec.wait(t)
	if !strings.Contains(msg, "stopped") || !strings.Contains(msg, "Instance "+ovhTestInstance+" was deleted") || len(f.deleted) != 1 {
		t.Errorf("error = %q, deleted = %v", msg, f.deleted)
	}
}

func TestOVHDemo(t *testing.T) {
	withDataDir(t)

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := OVHOptions(cfg, "", "")
	if o := decodeJSON[ovhOptions](t, out, err); len(o.Flavors) == 0 || len(o.Images) == 0 || !o.Images[0].Talos {
		t.Errorf("demo options = %+v", o)
	}

	rec := newJoinRecorder()
	StartOVHCreate(cfg, "", ovhTestCred, `{"region":"GRA11","flavorId":"demo-b3-8","imageId":"demo-talos","hostname":"node-c"}`, rec)

	if _, msg, _ := rec.wait(t); msg != errDemoUnavailable.Error() {
		t.Errorf("demo create = %q", msg)
	}
}
