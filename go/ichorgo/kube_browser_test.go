package ichorgo

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func kubeStoreFor(t *testing.T, f *fakeKubeAPI) string {
	t.Helper()

	stored, err := MergeKubeconfig("", "", strings.Replace(f.kubeconfigFor(f.URL), "https://other.invalid:6443", f.URL, 1), "")
	if err != nil {
		t.Fatal(err)
	}

	return stored
}

func TestKubeAPIResources(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"apps","preferredVersion":{"groupVersion":"apps/v1","version":"v1"}},
		                         {"name":"broken.io","preferredVersion":{"groupVersion":"broken.io/v1","version":"v1"}}]}`,
		"GET /api/v1": `{"groupVersion":"v1","resources":[
		  {"name":"pods","kind":"Pod","namespaced":true,"verbs":["get","list"],"shortNames":["po"]},
		  {"name":"pods/log","kind":"Pod","namespaced":true,"verbs":["get"]},
		  {"name":"bindings","kind":"Binding","namespaced":true,"verbs":["create"]}]}`,
		"GET /apis/apps/v1": `{"groupVersion":"apps/v1","resources":[{"name":"deployments","kind":"Deployment","namespaced":true,"verbs":["list"]}]}`,
	})

	out, err := KubeAPIResources(kubeStoreFor(t, f), "admin@test", "")
	if err != nil {
		t.Fatal(err)
	}

	var got kubeBrowserResourceList
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if len(got.Resources) != 2 || got.Resources[0].Kind != "Deployment" || got.Resources[0].Group != "apps" ||
		got.Resources[1].Resource != "pods" || got.Resources[1].Group != "" || got.Resources[1].ShortNames[0] != "po" {
		t.Fatalf("resources %+v", got.Resources)
	}

	if len(got.Failed) != 1 || got.Failed[0] != "broken.io/v1" {
		t.Errorf("failed %v", got.Failed)
	}
}

func TestKubeResourcePageTable(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/cert-manager.io/v1/namespaces/web/certificates": `{"kind":"Table","apiVersion":"meta.k8s.io/v1",
		  "metadata":{"continue":"abc","remainingItemCount":3},
		  "columnDefinitions":[{"name":"Name","type":"string","priority":0},{"name":"Ready","type":"string","priority":0},{"name":"Secret","type":"string","priority":1}],
		  "rows":[{"cells":["site","True","site-tls"],"object":{"metadata":{"name":"site","namespace":"web","creationTimestamp":"2026-01-01T00:00:00Z"}}}]}`,
	})

	out, err := KubeResourcePage(kubeStoreFor(t, f), "admin@test", "", "cert-manager.io", "v1", "certificates", "web", "", 50)
	if err != nil {
		t.Fatal(err)
	}

	var got kubeResourcePage
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if len(got.Columns) != 3 || got.Columns[2].Priority != 1 || len(got.Rows) != 1 ||
		got.Rows[0].Cells[1] != "True" || got.Rows[0].Namespace != "web" || got.Rows[0].Created == 0 ||
		got.Continue == "" || got.Remaining != 3 {
		t.Fatalf("page %+v", got)
	}

	r := f.recorded()
	if q := r[len(r)-1].query; !strings.Contains(q, "limit=50") {
		t.Errorf("query %q", q)
	}
}

func TestKubeResourcePageValidates(t *testing.T) {
	for _, args := range [][3]string{{"../x", "v1", "pods"}, {"", "1", "pods"}, {"", "v1", "pods/../x"}, {"", "v1", ""}} {
		if _, err := KubeResourcePage("", "x", "", args[0], args[1], args[2], "", "", 0); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestKubeObjectYAMLHidesSecrets(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/web/secrets/db": `{"apiVersion":"v1","kind":"Secret",
		  "metadata":{"name":"db","namespace":"web","managedFields":[{"manager":"kubectl"}],
		    "annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{\"data\":{\"password\":\"aHVudGVyMg==\"}}","keep":"me"}},
		  "data":{"password":"aHVudGVyMg=="}}`,
	})

	stored := kubeStoreFor(t, f)

	hidden, err := KubeObjectYAML(stored, "admin@test", "", "", "v1", "secrets", "web", "db", false)
	if err != nil {
		t.Fatal(err)
	}

	for _, leak := range []string{"aHVudGVyMg==", "managedFields", "last-applied"} {
		if strings.Contains(hidden, leak) {
			t.Errorf("%q in %s", leak, hidden)
		}
	}

	if !strings.Contains(hidden, "<hidden, 12 characters>") || !strings.Contains(hidden, "keep: me") {
		t.Errorf("yaml %s", hidden)
	}

	shown, err := KubeObjectYAML(stored, "admin@test", "", "", "v1", "secrets", "web", "db", true)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(shown, "aHVudGVyMg==") {
		t.Errorf("reveal ignored: %s", shown)
	}
}

type recLogListener struct {
	mu    sync.Mutex
	lines []string
	done  chan string
}

func (l *recLogListener) OnLine(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.lines = append(l.lines, line)
}

func (l *recLogListener) OnDone(err string) { l.done <- err }

func TestStartPodLogFollow(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/web/pods/api-1/log": "one\ntwo\r\nthree",
	})

	l := &recLogListener{done: make(chan string, 1)}
	StartPodLogFollow(kubeStoreFor(t, f), "admin@test", "", "web", "api-1", "app", 20, l)

	select {
	case err := <-l.done:
		if err != "" {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no end")
	}

	if strings.Join(l.lines, "|") != "one|two|three" {
		t.Fatalf("lines %q", l.lines)
	}

	r := f.recorded()
	if q := r[len(r)-1].query; !strings.Contains(q, "follow=true") || !strings.Contains(q, "container=app") || !strings.Contains(q, "tailLines=20") {
		t.Errorf("query %q", q)
	}
}

func TestReadLogLinesCutsHugeLines(t *testing.T) {
	var got []string

	huge := strings.Repeat("x", kubeLogLineMax*2)
	if err := readLogLines(strings.NewReader(huge+"\nnext\n"), func(s string) { got = append(got, s) }); err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || len(got[0]) != kubeLogLineMax || got[1] != "next" {
		t.Fatalf("got %d lines, first %d bytes", len(got), len(got[0]))
	}
}

func TestStartPodLogFollowCancel(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	l := &recLogListener{done: make(chan string, 1)}
	run := StartPodLogFollow(demo, "Demo cluster", "", "web", "api-1", "", 10, l)

	time.Sleep(50 * time.Millisecond)
	run.Cancel()

	select {
	case err := <-l.done:
		if err != "" {
			t.Fatalf("cancel reported %q", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not end the follow")
	}
}

func TestKubeObjectUpdate(t *testing.T) {
	live := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cfg","namespace":"web","resourceVersion":"7"},"data":{"a":"1"}}`
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/web/configmaps/cfg": live,
		"PUT /api/v1/namespaces/web/configmaps/cfg": `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"cfg","namespace":"web","resourceVersion":"8"},"data":{"a":"2"}}`,
	})

	stored := kubeStoreFor(t, f)
	edited := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cfg\n  namespace: web\n  resourceVersion: \"7\"\ndata:\n  a: \"2\"\n"

	out, err := KubeObjectUpdatePreview(stored, "admin@test", "", "", "v1", "configmaps", "web", "cfg", edited)
	if err != nil {
		t.Fatal(err)
	}

	var preview kubeEditPreview
	if err := json.Unmarshal([]byte(out), &preview); err != nil {
		t.Fatal(err)
	}

	if !preview.Changed || !strings.Contains(preview.Diff, "-  a: \"1\"") || !strings.Contains(preview.Diff, "+  a: \"2\"") {
		t.Fatalf("preview %+v", preview)
	}

	if err := KubeObjectUpdate(stored, "admin@test", "", "", "v1", "configmaps", "web", "cfg", edited); err != nil {
		t.Fatal(err)
	}

	var puts []fakeKubeRequest
	for _, r := range f.recorded() {
		if r.method == "PUT" {
			puts = append(puts, r)
		}
	}

	if len(puts) != 2 || !strings.Contains(puts[0].query, "dryRun=All") || strings.Contains(puts[1].query, "dryRun") ||
		!strings.Contains(puts[1].body, `"resourceVersion":"7"`) {
		t.Fatalf("puts %+v", puts)
	}
}

func TestKubeObjectUpdateRefuses(t *testing.T) {
	base := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: s\n  namespace: web\n  resourceVersion: \"1\"\n"

	for name, edited := range map[string]string{
		"hidden values": base + "data:\n  k: <hidden, 4 characters>\n",
		"renamed":       strings.Replace(base, "name: s", "name: t", 1),
		"moved":         strings.Replace(base, "namespace: web", "namespace: other", 1),
		"no version":    strings.Replace(base, "  resourceVersion: \"1\"\n", "", 1),
		"no metadata":   "kind: Secret\n",
		"not yaml":      ": : :",
	} {
		if _, _, err := editTarget("", "v1", "secrets", "web", "s", edited); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
