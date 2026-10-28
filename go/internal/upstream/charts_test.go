package upstream

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const sampleIndex = `apiVersion: v1
entries:
  app:
    - version: 6.7.0
    - version: 6.7.1
    - version: 6.7.2
      deprecated: true
    - version: 6.8.0-rc.1
    - version: 6.9.0
    - version: 7.0.0
  other:
    - version: 1.0.0
`

func TestUpstreamChartIndex(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/charts/index.yaml" {
			http.NotFound(w, r)

			return
		}

		if user, pass, ok := r.BasicAuth(); ok && (user != "reader" || pass != "pw") {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		_, _ = w.Write([]byte(sampleIndex)) //nolint:errcheck
	})
	c := f.client()
	auth := Auth{Helm: &BasicAuth{Username: "reader", Password: "pw"}}

	got, err := c.Lookup(context.Background(), Target{Kind: "chart", RepoURL: f.URL + "/charts/", Name: "app", Version: "6.7.0"}, auth)
	if err != nil {
		t.Fatal(err)
	}

	want := Candidate{Current: "6.7.0", Patch: "6.7.1", Minor: "6.9.0", Major: "7.0.0", Shape: "{core}", Source: f.URL + "/charts/index.yaml"}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}

	// One index.yaml holds every chart of the repository: read once.
	other, err := c.Lookup(context.Background(), Target{Kind: "chart", RepoURL: f.URL + "/charts", Name: "other", Version: "1.0.0"}, auth)
	if err != nil || other.Current != "1.0.0" || other.Patch != "" {
		t.Fatalf("other: %+v, %v", other, err)
	}

	if n := f.hits.Load(); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}

	if _, err := c.Lookup(context.Background(), Target{Kind: "chart", RepoURL: f.URL + "/charts", Name: "missing", Version: "1.0.0"}, auth); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("a chart missing from the index: %v", err)
	}
}

func TestUpstreamChartIndexTooLarge(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("entries:\n")) //nolint:errcheck

		chunk := []byte(strings.Repeat("#", 1<<20) + "\n")
		for range 9 {
			_, _ = w.Write(chunk) //nolint:errcheck
		}
	})

	_, err := f.client().Lookup(context.Background(), Target{Kind: "chart", RepoURL: f.URL, Name: "app", Version: "1.0.0"}, Auth{})
	if err == nil || !strings.Contains(err.Error(), "8 MiB") {
		t.Errorf("err = %v", err)
	}
}

func TestUpstreamOCIChart(t *testing.T) {
	f := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/example-org/charts/app/tags/list" {
			http.NotFound(w, r)

			return
		}

		writeJSON(w, tagsList{Tags: []string{"1.2.0", "1.2.1_build.7", "1.3.0", "sha256-abc.sig"}})
	})

	got, err := f.client().Lookup(context.Background(), Target{Kind: "chart", RepoURL: "oci://" + f.host() + "/example-org/charts", Name: "app", Version: "1.2.0"}, Auth{})
	if err != nil {
		t.Fatal(err)
	}

	want := Candidate{Current: "1.2.0", Patch: "1.2.1+build.7", Minor: "1.3.0", Shape: "{core}", Source: f.URL + "/v2/example-org/charts/app/tags/list"}
	if got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}
