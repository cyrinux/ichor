package upstream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRegistry is an https test server counting its requests.
type fakeRegistry struct {
	*httptest.Server
	hits atomic.Int64
}

func newFake(t *testing.T, h http.HandlerFunc) *fakeRegistry {
	t.Helper()

	f := &fakeRegistry{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(f.Close)

	return f
}

func (f *fakeRegistry) host() string { return strings.TrimPrefix(f.URL, "https://") }

func (f *fakeRegistry) client() *Client {
	return New(Options{HTTP: f.Client(), HubURL: f.URL, DockerRegistry: f.host()})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func image(ref string) Target { return Target{Kind: "image", Ref: ref} }

func TestUpstreamOCITwoPagesAndCache(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/example-org/app/tags/list" {
			http.NotFound(w, r)

			return
		}

		if r.URL.Query().Get("last") == "" {
			w.Header().Set("Link", `</v2/example-org/app/tags/list?n=1000&last=v1.19.2>; rel="next"`)
			writeJSON(w, tagsList{Tags: []string{"v1.19.0", "v1.19.2", "latest"}})

			return
		}

		writeJSON(w, tagsList{Tags: []string{"v1.20.1", "v2.0.0", "v2.1.0-rc.1"}})
	})
	c := f.client()

	got, err := c.Lookup(context.Background(), image(f.host()+"/example-org/app:v1.19.0"), Auth{})
	if err != nil {
		t.Fatal(err)
	}

	want := Candidate{Current: "v1.19.0", Patch: "v1.19.2", Minor: "v1.20.1", Major: "v2.0.0", Shape: "v{core}", Source: f.URL + "/v2/example-org/app/tags/list"}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}

	if n := f.hits.Load(); n != 2 {
		t.Fatalf("%d requests, want 2 pages", n)
	}

	// Another tag of the same repository, within 6 h: no request.
	if _, err := c.Lookup(context.Background(), image(f.host()+"/example-org/app:v1.20.1"), Auth{}); err != nil {
		t.Fatal(err)
	}

	if n := f.hits.Load(); n != 2 {
		t.Errorf("the second lookup made %d requests", n-2)
	}

	// Past 6 h the repository is asked again.
	c.cache.now = func() time.Time { return time.Now().Add(cacheTTL + time.Minute) }
	if _, err := c.Lookup(context.Background(), image(f.host()+"/example-org/app:v1.19.0"), Auth{}); err != nil {
		t.Fatal(err)
	}

	if n := f.hits.Load(); n != 4 {
		t.Errorf("after the TTL: %d requests, want 4", n)
	}
}

func TestUpstreamOCIPageCap(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`</v2/example-org/app/tags/list?last=%d>; rel="next"`, time.Now().UnixNano()))
		writeJSON(w, tagsList{Tags: []string{"1.0.0"}})
	})

	if _, err := f.client().Lookup(context.Background(), image(f.host()+"/example-org/app:1.0.0"), Auth{}); err != nil {
		t.Fatal(err)
	}

	if n := f.hits.Load(); n != ociPages {
		t.Errorf("%d pages read, want %d", n, ociPages)
	}
}

func TestUpstreamBearerToken(t *testing.T) {
	var realm string

	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/token":
			user, pass, ok := r.BasicAuth()
			if !ok || user != "robot" || pass != "s3cret" {
				w.WriteHeader(http.StatusUnauthorized)

				return
			}

			if r.URL.Query().Get("scope") != "repository:example-org/private:pull" || r.URL.Query().Get("service") != "registry.example.com" {
				t.Errorf("token query %s", r.URL.RawQuery)
			}

			writeJSON(w, map[string]any{"token": "tok-1", "expires_in": 300})
		case r.Header.Get("Authorization") == "Bearer tok-1":
			writeJSON(w, tagsList{Tags: []string{"1.0.0", "1.0.1"}})
		default:
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+realm+`",service="registry.example.com",scope="repository:example-org/private:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	realm = f.URL + "/token"
	c := f.client()
	auth := Auth{Auths: map[string]AuthEntry{"https://" + f.host() + "/v1/": {Auth: base64.StdEncoding.EncodeToString([]byte("robot:s3cret"))}}}

	got, err := c.Lookup(context.Background(), image(f.host()+"/example-org/private:1.0.0"), auth)
	if err != nil || got.Patch != "1.0.1" || got.Reason != "" {
		t.Fatalf("got %+v, %v", got, err)
	}

	// Without credentials: the token endpoint refuses, which is a reason, not an error.
	anon := f.client()

	got, err = anon.Lookup(context.Background(), image(f.host()+"/example-org/private:1.0.0"), Auth{})
	if err != nil || got.Reason != ReasonCredentials {
		t.Fatalf("anonymous: got %+v, %v", got, err)
	}
}

func TestUpstreamBasicOnlyRegistry(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	got, err := f.client().Lookup(context.Background(), image(f.host()+"/example-org/app:1.0.0"), Auth{})
	if err != nil || got.Reason != ReasonCredentials {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestUpstreamDockerHubPages(t *testing.T) {
	var base string

	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/repositories/library/nginx/tags" {
			http.NotFound(w, r)

			return
		}

		page := r.URL.Query().Get("page")
		tags := map[string][]string{
			"":  {"1.27.0-alpine", "1.27.3-alpine", "1.27.4", "latest"},
			"2": {"1.28.0-alpine", "1.28.0-alpine-slim", "mainline-alpine"},
			"3": {"1.29.1-alpine"},
			"4": {"2.0.0-alpine"}, // past the 3-page cap
		}[page]
		next := map[string]string{"": base + "?page=2", "2": base + "?page=3", "3": base + "?page=4"}[page]

		results := []map[string]string{}
		for _, tag := range tags {
			results = append(results, map[string]string{"name": tag})
		}

		writeJSON(w, map[string]any{"next": next, "results": results})
	})
	base = f.URL + "/v2/repositories/library/nginx/tags"

	got, err := f.client().Lookup(context.Background(), image("nginx:1.27.0-alpine"), Auth{})
	if err != nil {
		t.Fatal(err)
	}

	want := Candidate{Current: "1.27.0-alpine", Patch: "1.27.3-alpine", Minor: "1.29.1-alpine", Shape: "{core}-alpine", Source: base}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}

	if n := f.hits.Load(); n != hubPages {
		t.Errorf("%d pages, want %d", n, hubPages)
	}

	latest, err := f.client().Lookup(context.Background(), image("docker.io/library/nginx:latest"), Auth{})
	if err != nil || latest.Reason != ReasonNotVersion {
		t.Errorf("latest: %+v, %v", latest, err)
	}
}

func TestUpstreamDockerHubRateLimitFallsBackToRegistry(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v2/repositories/"):
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
		case r.URL.Path == "/v2/example-org/app/tags/list":
			writeJSON(w, tagsList{Tags: []string{"3.1.0", "3.2.0"}})
		default:
			http.NotFound(w, r)
		}
	})

	got, err := f.client().Lookup(context.Background(), image("example-org/app:3.1.0"), Auth{})
	if err != nil || got.Minor != "3.2.0" || got.Source != f.URL+"/v2/example-org/app/tags/list" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestUpstreamRateLimited(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		headers map[string]string
		want    string
	}{
		{name: "429 Retry-After", status: http.StatusTooManyRequests, headers: map[string]string{"Retry-After": "42"}, want: "retry after 42 s"},
		{name: "403 RateLimit headers", status: http.StatusForbidden, headers: map[string]string{"RateLimit-Remaining": "0", "RateLimit-Reset": "17"}, want: "retry after 17 s"},
		{name: "429 no header", status: http.StatusTooManyRequests, want: "retry after 60 s"},
		{name: "403 without headers", status: http.StatusForbidden, want: ReasonCredentials},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}

				w.WriteHeader(tc.status)
			})

			got, err := f.client().Lookup(context.Background(), image(f.host()+"/example-org/app:1.0.0"), Auth{})
			if err != nil || !strings.HasSuffix(got.Reason, tc.want) {
				t.Fatalf("got %+v, %v; want a reason ending in %q", got, err, tc.want)
			}

			if tc.status == http.StatusTooManyRequests && !strings.HasPrefix(got.Reason, "rate limited by "+f.host()) {
				t.Errorf("reason %q does not name the host", got.Reason)
			}
		})
	}
}

// refuseAll fails the test on any request.
type refuseAll struct{ t *testing.T }

func (r refuseAll) RoundTrip(req *http.Request) (*http.Response, error) {
	r.t.Errorf("unexpected request to %s", req.URL)

	return nil, fmt.Errorf("refused")
}

func TestUpstreamNoRequestNeeded(t *testing.T) {
	c := New(Options{HTTP: &http.Client{Transport: refuseAll{t}}})

	cases := []struct {
		target Target
		reason string
	}{
		{image("public.ecr.aws/example-org/app:1.2.3"), ReasonCredentials},
		{image("123456789012.dkr.ecr.eu-west-1.amazonaws.com/app:1.2.3"), ReasonCredentials},
		{image("ghcr.io/example-org/app@sha256:0123456789abcdef"), ReasonDigest},
		{image("ghcr.io/example-org/app:latest"), ReasonNotVersion},
		{image("ghcr.io/example-org/app"), ReasonNotVersion},
		{Target{Kind: "chart", RepoURL: "https://charts.example.com", Name: "app", Version: "main"}, ReasonNotVersion},
		{Target{Kind: "chart", RepoURL: "oci://public.ecr.aws/example-org", Name: "app", Version: "1.0.0"}, ReasonCredentials},
	}

	for _, tc := range cases {
		got, err := c.Lookup(context.Background(), tc.target, Auth{})
		if err != nil || got.Reason != tc.reason {
			t.Errorf("%+v: got %+v, %v; want %q", tc.target, got, err, tc.reason)
		}
	}

	if _, err := c.Lookup(context.Background(), Target{Kind: "helm"}, Auth{}); err == nil {
		t.Error("an unknown kind is accepted")
	}
}

func TestUpstreamFailureCachedFiveMinutes(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	c := f.client()
	target := image(f.host() + "/example-org/app:1.0.0")

	for range 2 {
		if _, err := c.Lookup(context.Background(), target, Auth{}); err == nil {
			t.Fatal("a 502 is not an error")
		}
	}

	if n := f.hits.Load(); n != 1 {
		t.Errorf("%d requests within the retry delay, want 1", n)
	}

	c.cache.now = func() time.Time { return time.Now().Add(cacheRetry + time.Second) }
	_, _ = c.Lookup(context.Background(), target, Auth{}) //nolint:errcheck

	if n := f.hits.Load(); n != 2 {
		t.Errorf("%d requests after the retry delay, want 2", n)
	}
}

func TestUpstreamOneFetchInFlight(t *testing.T) {
	release := make(chan struct{})
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		<-release
		writeJSON(w, tagsList{Tags: []string{"1.0.1"}})
	})
	c := f.client()

	var wg sync.WaitGroup

	for range 5 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			got, err := c.Lookup(context.Background(), image(f.host()+"/example-org/app:1.0.0"), Auth{})
			if err != nil || got.Patch != "1.0.1" {
				t.Errorf("got %+v, %v", got, err)
			}
		}()
	}

	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if n := f.hits.Load(); n != 1 {
		t.Errorf("%d requests for one repository, want 1", n)
	}
}

func TestUpstreamLookupAllOrder(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2/example-org/"), "/tags/list")
		writeJSON(w, tagsList{Tags: []string{"1.0.0", "1.0.1", "1.1.0-" + name}})
	})

	targets := []Target{}
	for i := range 9 {
		targets = append(targets, image(fmt.Sprintf("%s/example-org/app%d:1.0.0", f.host(), i)))
	}

	targets = append(targets, image("public.ecr.aws/example-org/app:1.0.0"))

	got := f.client().LookupAll(context.Background(), targets, Auth{})
	if len(got) != len(targets) {
		t.Fatalf("%d results for %d targets", len(got), len(targets))
	}

	for i, r := range got[:9] {
		if r.Err != nil || r.Candidate.Patch != "1.0.1" || !strings.Contains(r.Candidate.Source, fmt.Sprintf("/app%d/", i)) {
			t.Errorf("result %d: %+v", i, r)
		}
	}

	if got[9].Candidate.Reason != ReasonCredentials {
		t.Errorf("ECR result %+v", got[9])
	}
}

func TestUpstreamCacheBound(t *testing.T) {
	c := newCache()
	start := time.Now()

	for i := range cacheEntries + 10 {
		at := start.Add(time.Duration(i) * time.Second)
		c.now = func() time.Time { return at }

		if _, err := c.get(context.Background(), fmt.Sprint(i), func() (fetched, error) { return fetched{}, nil }); err != nil {
			t.Fatal(err)
		}
	}

	if len(c.entries) != cacheEntries {
		t.Errorf("%d entries, want %d", len(c.entries), cacheEntries)
	}

	if _, ok := c.entries["0"]; ok {
		t.Error("the oldest entry was kept")
	}

	if _, ok := c.entries[fmt.Sprint(cacheEntries+9)]; !ok {
		t.Error("the newest entry was dropped")
	}
}
