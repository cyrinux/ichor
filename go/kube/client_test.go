package kube

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// newTestClient serves handler over TLS and returns a client for it below basePath.
func newTestClient(t *testing.T, basePath string, handler http.HandlerFunc) Client {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	base, err := url.Parse(srv.URL + basePath)
	if err != nil {
		t.Fatal(err)
	}

	return Client{Base: base, HTTP: srv.Client()}
}

func TestEndpointKeepsEscapedNamesAndQuery(t *testing.T) {
	base, _ := url.Parse("https://api.example.invalid:6443/k8s/clusters/c-1/")
	c := Client{Base: base}

	u, err := c.Endpoint("/api/v1/namespaces/team%2Fa/pods?limit=5")
	if err != nil {
		t.Fatal(err)
	}

	if got, want := u.String(), "https://api.example.invalid:6443/k8s/clusters/c-1/api/v1/namespaces/team%2Fa/pods?limit=5"; got != want {
		t.Fatalf("endpoint = %q, want %q", got, want)
	}

	if _, err := c.Endpoint("/api/%zz"); err == nil {
		t.Fatal("a bad escape is accepted")
	}
}

func TestRequestHeadersAndBearer(t *testing.T) {
	var got http.Header

	c := newTestClient(t, "", func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)
	})

	tests := []struct {
		name  string
		token func(context.Context) (string, error)
		want  string
	}{
		{"no token source", nil, ""},
		{"empty token (client certificate)", func(context.Context) (string, error) { return "", nil }, ""},
		{"bearer token", func(context.Context) (string, error) { return "t0ken", nil }, "Bearer t0ken"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.Token = tt.token
			if err := c.Probe(context.Background()); err != nil {
				t.Fatal(err)
			}

			if a := got.Get("Authorization"); a != tt.want {
				t.Fatalf("Authorization = %q, want %q", a, tt.want)
			}

			if ua := got.Get("User-Agent"); ua != "ichor" {
				t.Fatalf("User-Agent = %q", ua)
			}
		})
	}
}

func TestRequestTokenErrorStopsTheCall(t *testing.T) {
	called := false
	c := newTestClient(t, "", func(http.ResponseWriter, *http.Request) { called = true })
	refused := errors.New("sign in again")
	c.Token = func(context.Context) (string, error) { return "", refused }

	if err := c.Get(context.Background(), "/api", nil); !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the token error", err)
	}

	if called {
		t.Fatal("the request was sent without its token")
	}
}

func TestDoDecodesAndReportsStatus(t *testing.T) {
	c := newTestClient(t, "", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			if r.Method == http.MethodPatch && r.Header.Get("Content-Type") != "application/merge-patch+json" {
				http.Error(w, "content type", http.StatusBadRequest)

				return
			}

			_, _ = io.WriteString(w, `{"items":[{"name":"a"},{"name":"b"}]}`)
		case "/missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"NotFound","message":"pods \"x\" not found"}`)
		default:
			_, _ = io.WriteString(w, `not json`)
		}
	})

	ctx := context.Background()

	var list List[struct {
		Name string `json:"name"`
	}]
	if err := c.Get(ctx, "/ok", &list); err != nil || len(list.Items) != 2 || list.Items[1].Name != "b" {
		t.Fatalf("get = %+v, %v", list, err)
	}

	if err := c.Patch(ctx, "/ok", "application/merge-patch+json", map[string]any{"spec": nil}, nil); err != nil {
		t.Fatalf("patch: %v", err)
	}

	err := c.Post(ctx, "/missing", map[string]string{}, nil)
	if !IsNotFound(err) || err.Error() != `Kubernetes API: not found: pods "x" not found` {
		t.Fatalf("post = %v", err)
	}

	if err := c.Get(ctx, "/garbage", &list); err == nil || !strings.HasPrefix(err.Error(), "decode Kubernetes API answer") {
		t.Fatalf("decode error = %v", err)
	}
}

func TestGetRawGetTextAndStream(t *testing.T) {
	c := newTestClient(t, "", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proxy":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, "tenant="+r.Header.Get("X-Scope-OrgID"))
		case "/log":
			_, _ = io.WriteString(w, "line 1\nline 2\n")
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"kind":"Status","message":"no"}`)
		}
	})

	ctx := context.Background()

	status, ctype, body, err := c.GetRaw(ctx, "/proxy", map[string]string{"X-Scope-OrgID": "team-a"})
	if err != nil || status != http.StatusServiceUnavailable || ctype != "application/json" || string(body) != "tenant=team-a" {
		t.Fatalf("getRaw = %d %q %q %v", status, ctype, body, err)
	}

	if text, err := c.GetText(ctx, "/log"); err != nil || text != "line 1\nline 2\n" {
		t.Fatalf("getText = %q, %v", text, err)
	}

	if _, err := c.GetText(ctx, "/denied"); !IsForbidden(err) {
		t.Fatalf("getText denied = %v", err)
	}

	var read string

	err = c.Stream(ctx, "/log", "text/plain", func(r io.Reader) error {
		data, err := io.ReadAll(r)
		read = string(data)

		return err
	})
	if err != nil || read != "line 1\nline 2\n" {
		t.Fatalf("stream = %q, %v", read, err)
	}

	if err := c.Stream(ctx, "/denied", "text/plain", func(io.Reader) error { return nil }); !IsForbidden(err) {
		t.Fatalf("stream denied = %v", err)
	}
}

func TestSendCapsTheAnswer(t *testing.T) {
	c := newTestClient(t, "", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, MaxBody+1))
	})

	_, _, err := c.Send(context.Background(), http.MethodGet, "/big", "application/json", "", nil, nil)
	if Code(err) != http.StatusRequestEntityTooLarge {
		t.Fatalf("err = %v, want 413", err)
	}
}

func TestServiceProxyPath(t *testing.T) {
	got := ServiceProxyPath("monitoring ns", "prometheus:9090", "/api/v1/query")
	if want := "/api/v1/namespaces/monitoring%20ns/services/prometheus:9090/proxy/api/v1/query"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}
