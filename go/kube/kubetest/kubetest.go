// Package kubetest is the fake Kubernetes API server the tests of go/kube's domain packages
// and of the facade (go/ichorgo) answer from: a TLS server with a "METHOD /path" -> JSON table
// that records every request. It is imported from tests only; nothing in the apps needs it.
package kubetest

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/cyrinux/ichor/go/kube"
)

// FakeAPI is an API server answering from a path -> JSON table and recording requests.
//
// A path it has no answer for is a 404 Status, except GET /version, which answers a version:
// a client's probe passes on any FakeAPI.
type FakeAPI struct {
	*httptest.Server

	// Mu guards Answers and the recording; hold it to change Answers once the server serves.
	Mu sync.Locker
	// Answers is "GET /path" -> body.
	Answers map[string]string
	// BeforeAnswer, when set, runs just before each answer is written.
	BeforeAnswer func(*http.Request)
	// AnswerFunc, when set, answers a request from its body (a SelfSubjectAccessReview per
	// verb); ok false falls back to Answers.
	AnswerFunc func(method, path, body string) (answer string, ok bool)
	// AnswerQuery, when set, answers a GET from its path and query (a list by label selector);
	// ok false falls back to Answers.
	AnswerQuery func(path, query string) (answer string, ok bool)
	// OnRequest, when set, sees each request as it is recorded, with Mu held.
	OnRequest func(Request)

	statuses map[string]int // "GET /path" -> HTTP status, 200 when unset
	requests []Request
}

// Request is one request the server received.
type Request struct {
	Method, Path, ContentType, Auth, Body, Query string
}

// New starts a FakeAPI answering from answers (nil for none but /version), closed with the
// test.
func New(t *testing.T, answers map[string]string) *FakeAPI {
	t.Helper()

	f := &FakeAPI{Mu: &sync.Mutex{}, Answers: answers, statuses: map[string]int{}}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)

	return f
}

func (f *FakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	f.Mu.Lock()
	request := Request{r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Authorization"), string(body), r.URL.RawQuery}
	f.requests = append(f.requests, request)

	if f.OnRequest != nil {
		f.OnRequest(request)
	}

	answer, ok := f.Answers[r.Method+" "+r.URL.Path]
	status := f.statuses[r.Method+" "+r.URL.Path]
	answerFunc, answerQuery := f.AnswerFunc, f.AnswerQuery
	f.Mu.Unlock()

	if answerQuery != nil && r.Method == http.MethodGet {
		if a, found := answerQuery(r.URL.Path, r.URL.RawQuery); found {
			answer, ok, status = a, true, 0
		}
	}

	if answerFunc != nil {
		if a, found := answerFunc(r.Method, r.URL.Path, string(body)); found {
			answer, ok, status = a, true, 0
		}
	}

	if f.BeforeAnswer != nil {
		f.BeforeAnswer(r)
	}

	if r.URL.Path == "/version" && !ok {
		answer, ok = `{"gitVersion":"v1.34.0"}`, true
	}

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"kind":"Status","reason":"NotFound","message":"`+r.URL.Path+` not found"}`)

		return
	}

	if status != 0 {
		w.WriteHeader(status)
	}

	_, _ = io.WriteString(w, answer)
}

// AnswerWith makes f answer key ("GET /path") with status and body.
func (f *FakeAPI) AnswerWith(key string, status int, body string) {
	f.Mu.Lock()
	defer f.Mu.Unlock()

	f.Answers[key], f.statuses[key] = body, status
}

// Recorded is a copy of the requests received so far, oldest first.
func (f *FakeAPI) Recorded() []Request {
	f.Mu.Lock()
	defer f.Mu.Unlock()

	return append([]Request(nil), f.requests...)
}

// ForgetRequests drops the requests recorded so far.
func (f *FakeAPI) ForgetRequests() {
	f.Mu.Lock()
	defer f.Mu.Unlock()

	f.requests = nil
}

// KubeClient is a client of this server, trusting its certificate, without a token.
func (f *FakeAPI) KubeClient(t *testing.T) kube.Client {
	t.Helper()

	base, err := url.Parse(f.URL)
	if err != nil {
		t.Fatal(err)
	}

	return kube.Client{Base: base, HTTP: f.Client()}
}
