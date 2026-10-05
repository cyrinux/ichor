package ichorgo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// pagingKubeAPI serves lists page by page: handle answers each request but /version and
// sees its query; the requests are recorded with their query and Accept header.
type pagingKubeAPI struct {
	*fakeKubeAPI

	mu   sync.Mutex
	seen []pagingRequest
}

type pagingRequest struct {
	path   string
	query  url.Values
	accept string
}

func newPagingKubeAPI(t *testing.T, handle func(w http.ResponseWriter, path string, query url.Values) string) (*pagingKubeAPI, *kubeClient) {
	t.Helper()

	p := &pagingKubeAPI{fakeKubeAPI: newFakeKubeAPI(t, map[string]string{})}
	p.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)

			return
		}

		p.mu.Lock()
		p.seen = append(p.seen, pagingRequest{r.URL.Path, r.URL.Query(), r.Header.Get("Accept")})
		p.mu.Unlock()

		_, _ = io.WriteString(w, handle(w, r.URL.Path, r.URL.Query()))
	})

	k, err := openKubeClient(context.Background(), p.kubeconfigFor(p.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return p, k
}

func (p *pagingKubeAPI) requests() []pagingRequest {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]pagingRequest(nil), p.seen...)
}

func jsonPod(namespace, name, image string) string {
	return `{"metadata":{"name":"` + name + `","namespace":"` + namespace + `"},"spec":{"containers":[{"name":"c","image":"` + image + `"}]},` +
		`"status":{"phase":"Running","containerStatuses":[{"ready":true,"state":{"running":{}}}]}}`
}

func TestListPodsReadsEveryPage(t *testing.T) {
	pages := map[string]string{
		"":   `{"metadata":{"continue":"t1","remainingItemCount":2},"items":[` + jsonPod("b", "x", "nginx") + `]}`,
		"t1": `{"metadata":{"continue":"t2","remainingItemCount":1},"items":[` + jsonPod("a", "y", "redis") + `]}`,
		"t2": `{"metadata":{},"items":[` + jsonPod("a", "z", "envoy") + `]}`,
	}

	api, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, _ string, q url.Values) string { return pages[q.Get("continue")] })

	list, err := listPods(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, p := range list.Pods {
		names = append(names, p.Namespace+"/"+p.Name)
	}

	// Sorted as before paging, with the images of full objects.
	if strings.Join(names, ",") != "a/y,a/z,b/x" || list.Pods[0].Images[0] != "redis" {
		t.Fatalf("got %v", list.Pods)
	}

	reqs := api.requests()
	if len(reqs) != 3 || reqs[0].query.Get("limit") != "500" || reqs[2].query.Get("continue") != "t2" || reqs[0].accept != "application/json" {
		t.Fatalf("requests %+v", reqs)
	}
}

const podTable = `{"kind":"Table","apiVersion":"meta.k8s.io/v1",
 "metadata":{"continue":"next-token","remainingItemCount":1500},
 "columnDefinitions":[{"name":"Name"},{"name":"Ready"},{"name":"Status"},{"name":"Restarts"},{"name":"Age"},{"name":"IP"},{"name":"Node"}],
 "rows":[
  {"cells":["web-1","1/2","Running","3 (5m ago)","2d","10.0.0.1","w1"],
   "object":{"kind":"PartialObjectMetadata","metadata":{"name":"web-1","namespace":"shop","creationTimestamp":"2026-01-02T03:04:05Z",
     "ownerReferences":[{"kind":"ReplicaSet","name":"web-5d8f"}]}}},
  {"cells":["job-1","0/1","Completed",0,"1h","<none>","<none>"],
   "object":{"metadata":{"name":"job-1","namespace":"shop"}}},
  {"cells":["db-0","1/1","Running",1,"5d","10.0.0.2","w2"],
   "object":{"metadata":{"name":"db-0","namespace":"shop"}}}
 ]}`

func TestPodsPageTable(t *testing.T) {
	api, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, _ string, _ url.Values) string { return podTable })

	page, err := listPodsPage(context.Background(), k, "shop", pageQuery{limit: 3, table: true})
	if err != nil {
		t.Fatal(err)
	}

	web, job, db := page.Pods[0], page.Pods[1], page.Pods[2]
	if web.Name != "web-1" || web.Namespace != "shop" || web.Ready != 1 || web.Containers != 2 || web.Restarts != 3 ||
		web.Node != "w1" || web.Owner != "ReplicaSet/web-5d8f" || web.Healthy || web.Created == 0 || len(web.Images) != 0 {
		t.Errorf("web %+v", web)
	}

	if !job.Healthy || job.Node != "" || job.Restarts != 0 || !db.Healthy || db.Restarts != 1 {
		t.Errorf("job %+v db %+v", job, db)
	}

	if page.Complete || page.Remaining != 1500 || page.Continue != hex.EncodeToString([]byte("next-token")) {
		t.Errorf("cursor %+v", page.pageCursor)
	}

	req := api.requests()[0]
	if req.path != "/api/v1/namespaces/shop/pods" || req.accept != kubeTableAccept ||
		req.query.Get("includeObject") != "Metadata" || req.query.Get("limit") != "3" {
		t.Errorf("request %+v", req)
	}
}

func TestPodsPageFallsBackToObjects(t *testing.T) {
	// A server (or aggregated API) without Table answers with the objects themselves.
	_, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, _ string, _ url.Values) string {
		return `{"kind":"PodList","metadata":{},"items":[` + jsonPod("a", "x", "nginx:1") + `]}`
	})

	page, err := listPodsPage(context.Background(), k, "", pageQuery{table: true})
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Pods) != 1 || page.Pods[0].Images[0] != "nginx:1" || !page.Complete || page.Remaining != -1 || page.Continue != "" {
		t.Fatalf("got %+v", page)
	}
}

func TestListAllRestartsWhenExpired(t *testing.T) {
	var mu sync.Mutex

	expired := false

	api, k := newPagingKubeAPI(t, func(w http.ResponseWriter, _ string, q url.Values) string {
		mu.Lock()
		defer mu.Unlock()

		switch q.Get("continue") {
		case "":
			return `{"metadata":{"continue":"t1"},"items":[` + jsonPod("a", "x", "i") + `]}`
		case "t1":
			if !expired {
				expired = true

				w.WriteHeader(http.StatusGone)

				return `{"kind":"Status","reason":"Expired","message":"The provided continue parameter is too old"}`
			}

			return `{"metadata":{},"items":[` + jsonPod("a", "y", "i") + `]}`
		}

		return `{}`
	})

	list, err := listPods(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	// Started again from the first page, without keeping its rows twice.
	if len(list.Pods) != 2 || len(api.requests()) != 4 {
		t.Fatalf("pods %v, %d requests", list.Pods, len(api.requests()))
	}
}

func TestPageExpiredIsTyped(t *testing.T) {
	_, k := newPagingKubeAPI(t, func(w http.ResponseWriter, _ string, _ url.Values) string {
		w.WriteHeader(http.StatusGone)

		return `{"kind":"Status","reason":"Expired","message":"too old resource version"}`
	})

	_, err := listPodsPage(context.Background(), k, "", pageQuery{continueToken: "old", table: true})
	if !isKubeExpired(err) || !strings.HasPrefix(err.Error(), kubeListExpired) {
		t.Fatalf("got %v", err)
	}

	// What the apps match, through the privacy mask too.
	SetPrivacyMask(true, "")
	defer SetPrivacyMask(false, "")

	masked := err
	maskErr(&masked)

	if !strings.HasPrefix(masked.Error(), kubeListExpired) {
		t.Fatalf("masked %v", masked)
	}
}

func TestGetPageWithoutRemainingCount(t *testing.T) {
	// The API server never counts what remains of a list with a selector.
	api, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, _ string, _ url.Values) string {
		return `{"metadata":{"continue":"t1"},"items":[]}`
	})

	page, err := k.getPage(context.Background(), "/api/v1/pods", pageQuery{labelSelector: "app=web", fieldSelector: "spec.nodeName=w1"})
	if err != nil {
		t.Fatal(err)
	}

	if page.remaining != -1 || page.continueToken != "t1" {
		t.Fatalf("got %+v", page)
	}

	q := api.requests()[0].query
	if q.Get("labelSelector") != "app=web" || q.Get("fieldSelector") != "spec.nodeName=w1" {
		t.Fatalf("query %v", q)
	}
}

func TestListNamespaces(t *testing.T) {
	_, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, _ string, q url.Values) string {
		if q.Get("continue") == "" {
			return `{"kind":"Table","metadata":{"continue":"t1"},"columnDefinitions":[{"name":"Name"},{"name":"Status"}],
				"rows":[{"cells":["shop","Active"],"object":{"metadata":{"name":"shop"}}}]}`
		}

		// No metadata object: the Name column.
		return `{"kind":"Table","metadata":{},"columnDefinitions":[{"name":"Name"}],"rows":[{"cells":["argocd"]}]}`
	})

	list, err := listNamespaces(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Join(list.Namespaces, ",") != "argocd,shop" || list.Forbidden {
		t.Fatalf("got %+v", list)
	}
}

func TestListNamespacesForbidden(t *testing.T) {
	p := &pagingKubeAPI{fakeKubeAPI: newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces": `{}`,
	})}
	p.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)

			return
		}

		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"namespaces is forbidden"}`)
	})

	kubeconfig := strings.Replace(p.kubeconfigFor(p.URL), "context: {cluster: test, user: admin@test}", "context: {cluster: test, user: admin@test, namespace: team-a}", 1)

	k, err := openKubeClient(context.Background(), kubeconfig, nil, "")
	if err != nil {
		t.Fatal(err)
	}

	list, err := listNamespaces(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	if !list.Forbidden || len(list.Namespaces) != 0 || list.ContextNamespace != "team-a" {
		t.Fatalf("got %+v", list)
	}
}

func TestWorkloadsPageScoped(t *testing.T) {
	api, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, _ string, _ url.Values) string {
		return `{"metadata":{"continue":"t1","remainingItemCount":7},"items":` + fakeDeployments[strings.Index(fakeDeployments, "["):len(fakeDeployments)-1] + `}`
	})

	wk, _ := findWorkloadKind("deployment")

	page, err := listWorkloadsPage(context.Background(), k, wk, "shop", pageQuery{continueToken: "t0"})
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Workloads) != 4 || page.Workloads[0].Kind != "Deployment" || page.Remaining != 7 || page.Complete {
		t.Fatalf("got %+v", page)
	}

	req := api.requests()[0]
	if req.path != "/apis/apps/v1/namespaces/shop/deployments" || req.query.Get("continue") != "t0" || req.accept != "application/json" {
		t.Fatalf("request %+v", req)
	}
}

func TestCronJobsPageKeepsItsRuns(t *testing.T) {
	_, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, path string, _ url.Values) string {
		switch path {
		case "/apis/batch/v1/namespaces/ops/cronjobs":
			return `{"metadata":{"continue":"t1"},"items":[{"metadata":{"name":"backup","namespace":"ops"},"spec":{"schedule":"0 3 * * *"}}]}`
		case "/apis/batch/v1/namespaces/ops/jobs":
			return `{"metadata":{},"items":[
				{"metadata":{"name":"backup-1","namespace":"ops","ownerReferences":[{"kind":"CronJob","name":"backup"}]},"status":{"conditions":[{"type":"Complete","status":"True"}]}},
				{"metadata":{"name":"other-1","namespace":"ops","ownerReferences":[{"kind":"CronJob","name":"other"}]}}]}`
		}

		return `{}`
	})

	page, err := listCronJobsPage(context.Background(), k, "ops", pageQuery{}, time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	if len(page.CronJobs) != 1 || len(page.CronJobs[0].Runs) != 1 || page.CronJobs[0].State != cronRunSucceeded || page.Complete {
		t.Fatalf("got %+v", page)
	}
}

func TestPageFunctionsDemoAndArguments(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubePodsPage(cfg, "", "", "", "", 0, true)
	if err != nil {
		t.Fatal(err)
	}

	var pods kubePodPage
	if err := json.Unmarshal([]byte(out), &pods); err != nil || len(pods.Pods) == 0 || !pods.Complete {
		t.Fatalf("demo pods %v %s", err, out)
	}

	ns := pods.Pods[0].Namespace

	out, err = KubePodsPage(cfg, "", "", ns, "", 0, true)
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal([]byte(out), &pods); err != nil {
		t.Fatal(err)
	}

	for _, p := range pods.Pods {
		if p.Namespace != ns {
			t.Fatalf("pod %s outside %s", p.Name, ns)
		}
	}

	for name, call := range map[string]func() (string, error){
		"namespaces": func() (string, error) { return KubeNamespaces(cfg, "", "") },
		"workloads":  func() (string, error) { return KubeWorkloadsPage(cfg, "", "", "Deployment", "", "", 0) },
		"cronjobs":   func() (string, error) { return KubeCronJobsPage(cfg, "", "", "", "", 0) },
		"pod":        func() (string, error) { return KubePod(cfg, "", "", ns, pods.Pods[0].Name) },
	} {
		if out, err := call(); err != nil || !strings.HasPrefix(out, "{") {
			t.Errorf("%s: %v %s", name, err, out)
		}
	}

	if _, err := KubePodsPage(cfg, "", "", "../x", "", 0, true); err == nil {
		t.Error("bad namespace accepted")
	}

	if _, err := KubePodsPage(cfg, "", "", "", "not-hex", 0, true); err == nil {
		t.Error("bad continue token accepted")
	}

	if _, err := KubeWorkloadsPage(cfg, "", "", "ReplicaSet", "", "", 0); err == nil {
		t.Error("bad kind accepted")
	}
}

func TestLeadingIntAndReady(t *testing.T) {
	if leadingInt("3 (5m ago)") != 3 || leadingInt("") != 0 || leadingInt("x") != 0 || leadingInt(" 12") != 12 {
		t.Error("leadingInt")
	}

	if r, n := readyCount("2/3"); r != 2 || n != 3 {
		t.Errorf("ready %d/%d", r, n)
	}

	if r, n := readyCount("bad"); r != 0 || n != 0 {
		t.Errorf("ready %d/%d", r, n)
	}

	if clampPageLimit(0) != kubePageLimit || clampPageLimit(1e6) != kubePageMaxLimit || clampPageLimit(42) != 42 {
		t.Error("clampPageLimit")
	}
}
