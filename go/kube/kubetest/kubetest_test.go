package kubetest

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cyrinux/ichor/go/kube"
)

func TestFakeAPIAnswersFromItsTable(t *testing.T) {
	f := New(t, map[string]string{"GET /api/v1/pods": `{"items":[]}`})
	k := f.KubeClient(t)

	var list kube.List[struct{}]
	if err := k.Get(context.Background(), "/api/v1/pods", &list); err != nil || list.Items == nil {
		t.Fatalf("%+v %v", list, err)
	}

	if err := k.Probe(context.Background()); err != nil {
		t.Fatalf("/version answers without an entry: %v", err)
	}

	err := k.Get(context.Background(), "/api/v1/nope", nil)
	if !kube.IsNotFound(err) || !strings.Contains(err.Error(), "/api/v1/nope not found") {
		t.Fatalf("no answer: %v", err)
	}

	f.AnswerWith("GET /api/v1/nope", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","message":"no"}`)

	if err := k.Get(context.Background(), "/api/v1/nope", nil); !kube.IsForbidden(err) {
		t.Fatalf("answer with a status: %v", err)
	}

	f.Mu.Lock()
	delete(f.Answers, "GET /api/v1/nope")
	f.Mu.Unlock()

	if err := k.Get(context.Background(), "/api/v1/nope", nil); !kube.IsNotFound(err) {
		t.Fatalf("answer removed: %v", err)
	}
}

func TestFakeAPIRecordsRequests(t *testing.T) {
	f := New(t, map[string]string{"POST /things": `{}`})
	k := f.KubeClient(t)

	var seen atomic.Int32

	f.OnRequest = func(Request) { seen.Add(1) }

	if err := k.Do(context.Background(), http.MethodPost, "/things?a=b", "application/json", []byte(`{"x":1}`), nil); err != nil {
		t.Fatal(err)
	}

	want := []Request{{Method: "POST", Path: "/things", ContentType: "application/json", Body: `{"x":1}`, Query: "a=b"}}
	if got := f.Recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}

	if seen.Load() != 1 {
		t.Fatalf("OnRequest saw %d", seen.Load())
	}

	f.ForgetRequests()

	if got := f.Recorded(); len(got) != 0 {
		t.Fatalf("after forgetting: %+v", got)
	}
}

func TestFakeAPIHooks(t *testing.T) {
	f := New(t, map[string]string{"GET /a": `"table"`, "POST /a": `"table"`})
	k := f.KubeClient(t)

	var before atomic.Int32

	f.BeforeAnswer = func(*http.Request) { before.Add(1) }
	f.AnswerQuery = func(_, query string) (string, bool) { return `"query"`, query == "q=1" }
	f.AnswerFunc = func(method, _, body string) (string, bool) { return `"body"`, method == http.MethodPost && body == "b" }

	for _, c := range []struct {
		method, path, body, want string
	}{
		{http.MethodGet, "/a", "", "table"},
		{http.MethodGet, "/a?q=1", "", "query"},
		{http.MethodPost, "/a", "other", "table"},
		{http.MethodPost, "/a", "b", "body"},
	} {
		var got string
		if err := k.Do(context.Background(), c.method, c.path, "", []byte(c.body), &got); err != nil || got != c.want {
			t.Errorf("%s %s: got %q %v, want %q", c.method, c.path, got, err, c.want)
		}
	}

	if before.Load() != 4 {
		t.Fatalf("BeforeAnswer ran %d times", before.Load())
	}
}
