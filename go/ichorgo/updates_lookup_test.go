package ichorgo

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/internal/upstream"
)

func TestUpdateCandidates(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/example-org/app/tags/list":
			_, _ = w.Write([]byte(`{"tags":["v1.19.0","v1.19.2","v1.20.1","v2.0.0"]}`)) //nolint:errcheck
		case "/charts/index.yaml":
			_, _ = w.Write([]byte("entries:\n  app:\n    - version: 1.0.0\n    - version: 1.1.0\n")) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "https://")
	saved := updatesClient
	updatesClient = upstream.New(upstream.Options{HTTP: srv.Client(), HubURL: srv.URL, DockerRegistry: host})

	t.Cleanup(func() { updatesClient = saved })

	targets := `[
		{"kind":"image","ref":"` + host + `/example-org/app:v1.19.0"},
		{"kind":"image","ref":"ghcr.io/example-org/app:latest"},
		{"kind":"chart","repoURL":"` + srv.URL + `/charts","name":"app","version":"1.0.0"},
		{"kind":"image","ref":"` + host + `/example-org/gone:1.0.0"},
		{"kind":"image","ref":"public.ecr.aws/example-org/app:1.0.0"}
	]`

	out, err := UpdateCandidates(targets, "")
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Candidates []struct {
			Target                              upstream.Target `json:"target"`
			Current, Patch, Minor, Major, Shape string
			Reason, Source                      string
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if len(got.Candidates) != 5 {
		t.Fatalf("%d candidates: %s", len(got.Candidates), out)
	}

	c := got.Candidates
	if c[0].Target.Ref != host+"/example-org/app:v1.19.0" || c[0].Patch != "v1.19.2" || c[0].Minor != "v1.20.1" || c[0].Major != "v2.0.0" || c[0].Shape != "v{core}" {
		t.Errorf("image: %+v", c[0])
	}

	if c[1].Reason != upstream.ReasonNotVersion {
		t.Errorf("latest: %+v", c[1])
	}

	if c[2].Minor != "1.1.0" || c[2].Source != srv.URL+"/charts/index.yaml" {
		t.Errorf("chart: %+v", c[2])
	}

	if c[3].Reason == "" {
		t.Errorf("a missing repository has no reason: %+v", c[3])
	}

	if c[4].Reason != upstream.ReasonCredentials {
		t.Errorf("ECR: %+v", c[4])
	}

	// Tags are not names: the privacy mask leaves the answer as it is.
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	masked, err := UpdateCandidates(targets, "")
	if err != nil || masked != out {
		t.Errorf("with the privacy mask:\n%s\nwithout:\n%s (%v)", masked, out, err)
	}
}

func TestUpdateCandidatesBadInput(t *testing.T) {
	for _, tc := range []struct{ targets, auth string }{
		{"not json", ""},
		{`[{"kind":"image","ref":"nginx:1.0"}]`, "{broken"},
		{"[" + strings.Repeat(`{"kind":"image","ref":"nginx:latest"},`, updatesMaxTargets) + `{"kind":"image"}]`, ""},
	} {
		if _, err := UpdateCandidates(tc.targets, tc.auth); err == nil {
			t.Errorf("UpdateCandidates(%.40q, %q) accepted", tc.targets, tc.auth)
		}
	}
}
