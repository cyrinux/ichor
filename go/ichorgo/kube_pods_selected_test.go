package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPodFieldSelector(t *testing.T) {
	cases := []struct{ base, phase, want string }{
		{"", "", ""},
		{"spec.nodeName=w1", "", "spec.nodeName=w1"},
		{"spec.nodeName=w1", "Running", "spec.nodeName=w1,status.phase=Running"},
		{"", "!Succeeded", "status.phase!=Succeeded"},
	}

	for _, c := range cases {
		if got, err := podFieldSelector(c.base, c.phase); err != nil || got != c.want {
			t.Errorf("%q %q: got %q %v", c.base, c.phase, got, err)
		}
	}

	for _, bad := range []string{"running", "Done", "!", "Running,spec.nodeName=x"} {
		if _, err := podFieldSelector("", bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestSelectorQuery(t *testing.T) {
	var s labelSelector
	if err := json.Unmarshal([]byte(`{"matchLabels":{"tier":"front","app":"web"},"matchExpressions":[
		{"key":"track","operator":"In","values":["stable","canary"]},
		{"key":"env","operator":"NotIn","values":["dev"]},
		{"key":"app.kubernetes.io/name","operator":"Exists"},
		{"key":"legacy","operator":"DoesNotExist"}]}`), &s); err != nil {
		t.Fatal(err)
	}

	got, ok := selectorQuery(s)
	if want := "app=web,tier=front,track in (stable,canary),env notin (dev),app.kubernetes.io/name,!legacy"; !ok || got != want {
		t.Fatalf("got %q %v\nwant %q", got, ok, want)
	}

	for _, raw := range []string{
		`{}`, // selects every pod: never asked
		`{"matchLabels":{"app":"a,b"}}`,
		`{"matchExpressions":[{"key":"x","operator":"In","values":[]}]}`,
		`{"matchExpressions":[{"key":"x","operator":"Gt","values":["1"]}]}`,
		`{"matchExpressions":[{"key":"x y","operator":"Exists"}]}`,
	} {
		var bad labelSelector
		if err := json.Unmarshal([]byte(raw), &bad); err != nil {
			t.Fatal(err)
		}

		if q, ok := selectorQuery(bad); ok {
			t.Errorf("%s: got %q", raw, q)
		}
	}
}

func TestWorkloadPodsPageUsesItsSelector(t *testing.T) {
	api, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, path string, _ url.Values) string {
		if path == "/apis/apps/v1/namespaces/shop/deployments/web" {
			return `{"metadata":{"name":"web","namespace":"shop"},"spec":{"selector":{"matchLabels":{"app":"web"},
				"matchExpressions":[{"key":"track","operator":"In","values":["stable"]}]}}}`
		}

		return `{"metadata":{"continue":"t1"},"items":[` + jsonPod("shop", "web-1", "nginx") + `]}`
	})

	q := pageQuery{limit: 50, fieldSelector: "status.phase=Running", table: false}

	page, err := listWorkloadPodsPage(context.Background(), k, workloadKinds[0], "shop", "web", q)
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Pods) != 1 || page.Pods[0].Name != "web-1" || page.Complete {
		t.Fatalf("page %+v", page)
	}

	reqs := api.requests()

	list := reqs[len(reqs)-1]
	if list.path != "/api/v1/namespaces/shop/pods" || list.query.Get("labelSelector") != "app=web,track in (stable)" ||
		list.query.Get("fieldSelector") != "status.phase=Running" || list.query.Get("limit") != "50" {
		t.Fatalf("pods request %+v", list)
	}
}

func TestWorkloadPodsPageEmptySelector(t *testing.T) {
	api, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, _ string, _ url.Values) string {
		return `{"metadata":{"name":"web","namespace":"shop"},"spec":{}}`
	})

	page, err := listWorkloadPodsPage(context.Background(), k, workloadKinds[1], "shop", "web", pageQuery{})
	if err != nil || len(page.Pods) != 0 || !page.Complete {
		t.Fatalf("got %+v %v", page, err)
	}

	// Only the workload was read: an empty selector would list every pod.
	if reqs := api.requests(); len(reqs) != 1 {
		t.Fatalf("requests %+v", reqs)
	}
}

func TestKubeNodePodsPageArgs(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeNodePodsPage(cfg, "", "", "demo-worker-1", "", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}

	var page kubePodPage
	if err := json.Unmarshal([]byte(out), &page); err != nil {
		t.Fatal(err)
	}

	if len(page.Pods) != 3 || !page.Complete {
		t.Fatalf("demo page %s", out)
	}

	for _, p := range page.Pods {
		if p.Node != "demo-worker-1" {
			t.Fatalf("pod of another node: %+v", p)
		}
	}

	if _, err := KubeNodePodsPage(cfg, "", "", "bad node", "", "", 0, false); err == nil {
		t.Fatal("bad node name accepted")
	}

	if _, err := KubeNodePodsPage(cfg, "", "", "w1", "Done", "", 0, false); err == nil {
		t.Fatal("bad phase accepted")
	}
}

func TestKubeNodeNameDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	name, err := KubeNodeName(cfg, "", "192.0.2.20")
	if err != nil || name != "demo-worker-1" {
		t.Fatalf("KubeNodeName = %q, %v", name, err)
	}

	// The name it gives lists that node's pods.
	out, err := KubeNodePodsPage(cfg, "", "", name, "", "", 0, true)
	if err != nil || !strings.Contains(out, "worker-6f4b8-uvwxy") {
		t.Fatalf("pods of %s: %s, %v", name, out, err)
	}

	if _, err := KubeNodeName(cfg, "", "192.0.2.99"); err == nil {
		t.Fatal("unknown node accepted")
	}
}

func TestKubeWorkloadPodsPageDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ kind, name, phase, want string }{
		{"Deployment", "worker", "", "worker-6f4b8-pqrst,worker-6f4b8-uvwxy"},
		{"StatefulSet", "postgres", "", "postgres-0"},
		{"Deployment", "hello-ichor", "!Running", ""},
	}

	for _, c := range cases {
		out, err := KubeWorkloadPodsPage(cfg, "", "", c.kind, "demo", c.name, c.phase, "", 0, true)
		if err != nil {
			t.Fatal(err)
		}

		var page kubePodPage
		if err := json.Unmarshal([]byte(out), &page); err != nil {
			t.Fatal(err)
		}

		names := make([]string, 0, len(page.Pods))
		for _, p := range page.Pods {
			names = append(names, p.Name)
		}

		if got := strings.Join(names, ","); got != c.want {
			t.Errorf("%s/%s %q: got %q", c.kind, c.name, c.phase, got)
		}
	}

	if _, err := KubeWorkloadPodsPage(cfg, "", "", "Job", "demo", "x", "", "", 0, false); err == nil {
		t.Fatal("unsupported kind accepted")
	}
}

func TestNodePodsQuery(t *testing.T) {
	api, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, _ string, _ url.Values) string {
		return `{"metadata":{},"items":[` + jsonPod("a", "x", "nginx") + `]}`
	})

	fields, err := podFieldSelector("spec.nodeName=w1", "!Succeeded")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := listPodsPage(context.Background(), k, "", pageQuery{fieldSelector: fields}); err != nil {
		t.Fatal(err)
	}

	r := api.requests()[0]
	if r.path != "/api/v1/pods" || r.query.Get("fieldSelector") != "spec.nodeName=w1,status.phase!=Succeeded" {
		t.Fatalf("request %+v", r)
	}
}

func TestUnhealthyPodsPerNamespace(t *testing.T) {
	crashing := `{"metadata":{"name":"bad","namespace":"%s"},"spec":{"containers":[{"name":"c","image":"x"}]},` +
		`"status":{"phase":"Running","containerStatuses":[{"ready":false,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}}`

	api, k := newPagingKubeAPI(t, func(w http.ResponseWriter, path string, _ url.Values) string {
		if path == "/api/v1/namespaces/locked/pods" {
			w.WriteHeader(http.StatusForbidden)

			return `{"kind":"Status","reason":"Forbidden","message":"no"}`
		}

		ns := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/namespaces/"), "/pods")

		return `{"metadata":{},"items":[` + jsonPod(ns, "ok", "nginx") + `,` + fmt.Sprintf(crashing, ns) + `]}`
	})

	pods, err := unhealthyPods(context.Background(), k, []string{"web", "", "locked", "db", "web"})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, p := range pods {
		got = append(got, p.Namespace+"/"+p.Name)
	}

	// The forbidden namespace is skipped, the others sorted.
	if strings.Join(got, ",") != "db/bad,web/bad" {
		t.Fatalf("got %v", got)
	}

	for _, r := range api.requests() {
		if r.path == "/api/v1/pods" || r.query.Get("fieldSelector") != unfinishedPods {
			t.Fatalf("request %+v", r)
		}
	}
}

func TestUnhealthyPodsManyNamespacesListOnce(t *testing.T) {
	api, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, _ string, _ url.Values) string {
		return `{"metadata":{},"items":[` +
			`{"metadata":{"name":"p","namespace":"ns-3"},"status":{"phase":"Pending"}},` +
			`{"metadata":{"name":"p","namespace":"other"},"status":{"phase":"Pending"}}]}`
	})

	var namespaces []string
	for i := range unhealthyPodsPerNamespace + 1 {
		namespaces = append(namespaces, fmt.Sprintf("ns-%d", i))
	}

	pods, err := unhealthyPods(context.Background(), k, namespaces)
	if err != nil || len(pods) != 1 || pods[0].Namespace != "ns-3" {
		t.Fatalf("got %+v %v", pods, err)
	}

	if reqs := api.requests(); len(reqs) != 1 || reqs[0].path != "/api/v1/pods" {
		t.Fatalf("requests %+v", reqs)
	}
}

func TestOwnedByWorkload(t *testing.T) {
	cases := []struct {
		owner, kind, name string
		want              bool
	}{
		{"ReplicaSet/web-5d8f", "Deployment", "web", true},
		{"ReplicaSet/web-api-5d8f", "Deployment", "web", false},
		{"ReplicaSet/web", "Deployment", "web", false},
		{"StatefulSet/db", "StatefulSet", "db", true},
		{"DaemonSet/db", "StatefulSet", "db", false},
		{"", "DaemonSet", "x", false},
	}

	for _, c := range cases {
		if got := ownedByWorkload(c.owner, c.kind, c.name); got != c.want {
			t.Errorf("%s %s/%s: got %v", c.owner, c.kind, c.name, got)
		}
	}
}
