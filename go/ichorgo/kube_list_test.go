package ichorgo

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGetListReadsEveryPage(t *testing.T) {
	var expired atomic.Bool

	api, k := newPagingKubeAPI(t, func(w http.ResponseWriter, _ string, q url.Values) string {
		switch q.Get("continue") {
		case "":
			return `{"metadata":{"continue":"t1"},"items":[{"name":"a"}]}`
		case "t1":
			// The first pass expires: the list starts again and drops what it had.
			if !expired.Swap(true) {
				w.WriteHeader(http.StatusGone)

				return `{"kind":"Status","reason":"Expired","message":"too old"}`
			}

			return `{"metadata":{"continue":"t2"},"items":[{"name":"b"}]}`
		default:
			return `{"metadata":{},"items":[{"name":"c"}]}`
		}
	})

	var list kubeList[struct {
		Name string `json:"name"`
	}]

	if err := getList(context.Background(), k, "/apis/example.io/v1/widgets?labelSelector=app%3Dweb", &list); err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(list.Items))
	for _, it := range list.Items {
		names = append(names, it.Name)
	}

	if strings.Join(names, ",") != "a,b,c" {
		t.Fatalf("got %v", names)
	}

	for _, r := range api.requests() {
		if r.path != "/apis/example.io/v1/widgets" || r.query.Get("limit") != "500" || r.query.Get("labelSelector") != "app=web" {
			t.Fatalf("request %+v", r)
		}
	}
}

func TestGetListErrorsAsGet(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis/example.io/v1/empty": `{"items":[]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	var list kubeList[map[string]any]
	if err := getList(context.Background(), k, "/apis/example.io/v1/missing", &list); !isNotFound(err) {
		t.Fatalf("missing kind: %v", err)
	}

	if err := getList(context.Background(), k, "/apis/example.io/v1/empty", &list); err != nil || list.Items == nil || len(list.Items) != 0 {
		t.Fatalf("empty list: %v %#v", err, list.Items)
	}

	if err := getList(context.Background(), k, "/apis/example.io/v1/empty?watch=true", &list); err == nil {
		t.Fatal("unsupported parameter accepted")
	}
}

// A CRD list read through getList: Argo CD's Applications over two pages.
func TestReadArgoCDPaged(t *testing.T) {
	app := func(name string) string {
		return `{"metadata":{"name":"` + name + `","namespace":"argocd"},"spec":{"destination":{"namespace":"` + name + `"}},` +
			`"status":{"health":{"status":"Healthy"},"sync":{"status":"Synced"}}}`
	}

	api, k := newPagingKubeAPI(t, func(w http.ResponseWriter, path string, q url.Values) string {
		switch {
		case path == "/apis":
			return `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`
		case path == "/apis/argoproj.io/v1alpha1/applications" && q.Get("continue") == "":
			return `{"metadata":{"continue":"next"},"items":[` + app("web") + `]}`
		case path == "/apis/argoproj.io/v1alpha1/applications":
			return `{"metadata":{},"items":[` + app("db") + `]}`
		default:
			return `{"items":[]}`
		}
	})

	s, err := readArgoCD(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Apps) != 2 || s.Apps[0].Name != "db" || s.Apps[1].Name != "web" {
		t.Fatalf("apps %+v", s.Apps)
	}

	calls := 0

	for _, r := range api.requests() {
		if r.path == "/apis/argoproj.io/v1alpha1/applications" {
			calls++

			if r.query.Get("limit") != "500" {
				t.Fatalf("request %+v", r)
			}
		}
	}

	if calls != 2 {
		t.Fatalf("%d application pages read", calls)
	}
}
