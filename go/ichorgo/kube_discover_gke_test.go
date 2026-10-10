package ichorgo

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeGoogleCloud serves a Google account's token, its projects (Cloud Resource Manager, two
// pages) and each project's GKE clusters; refused projects answer 403.
type fakeGoogleCloud struct {
	google   fakeGoogle
	projects [][]string
	refused  map[string]bool

	mu       sync.Mutex
	crmCalls int
	headers  []string
}

func (f *fakeGoogleCloud) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/token" || r.URL.Path == "/userinfo" {
		f.google.handle(w, r)

		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.headers = append(f.headers, r.Header.Get("X-Goog-User-Project"))

	if r.Header.Get("Authorization") != "Bearer ya29.user" {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	if r.URL.Path == "/v1/projects" {
		f.crmCalls++

		if r.URL.Query().Get("filter") != "lifecycleState:ACTIVE" {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		page, next := 0, "p2"
		if r.URL.Query().Get("pageToken") == "p2" {
			page, next = 1, ""
		}

		var items []string
		for _, p := range f.projects[page] {
			items = append(items, fmt.Sprintf(`{"projectId":%q}`, p))
		}

		fmt.Fprintf(w, `{"projects":[%s],"nextPageToken":%q}`, strings.Join(items, ","), next)

		return
	}

	project, ok := strings.CutPrefix(r.URL.Path, "/v1/projects/")
	project, ok2 := strings.CutSuffix(project, "/locations/-/clusters")

	switch {
	case !ok || !ok2:
		http.NotFound(w, r)
	case f.refused[project]:
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"error":{"code":403,"message":"Kubernetes Engine API has not been used in project"}}`)
	default:
		fmt.Fprintf(w, `{"clusters":[{"name":"%s-gke","location":"europe-west1","endpoint":"10.0.0.1","masterAuth":{"clusterCaCertificate":"Q0E="}}]}`, project)
	}
}

func withFakeGoogleCloud(t *testing.T, f *fakeGoogleCloud) {
	t.Helper()

	srv := withCloudServer(t, f.handle)

	token, info, gke, crm := gcpTokenTarget, gcpUserInfoTarget, gkeEndpoint, gcpCRMEndpoint
	gcpTokenTarget = func(string) string { return srv.URL + "/token" }
	gcpUserInfoTarget = func() string { return srv.URL + "/userinfo" }
	gkeEndpoint, gcpCRMEndpoint = srv.URL, srv.URL

	t.Cleanup(func() { gcpTokenTarget, gcpUserInfoTarget, gkeEndpoint, gcpCRMEndpoint = token, info, gke, crm })
}

func newFakeGoogleCloud() *fakeGoogleCloud {
	return &fakeGoogleCloud{
		google:   fakeGoogle{refresh: "rt-1", email: "dev@example.com"},
		projects: [][]string{{"sample-proj-a", "sample-proj-b"}, {"sample-proj-c"}},
		refused:  map[string]bool{"sample-proj-b": true},
	}
}

func TestDiscoverGKEWithUserCredentials(t *testing.T) {
	f := newFakeGoogleCloud()
	withFakeGoogleCloud(t, f)

	s := discoverWith(t, discoverGKE, map[string]string{gcpFieldUserCredentials: adcJSON("authorized_user", "rt-1")})

	var names []string
	for _, c := range s.Contexts {
		names = append(names, c.Name)

		if c.Auth != authGKE {
			t.Errorf("%s signs in with %q", c.Name, c.Auth)
		}
	}

	// Both pages of projects read; the refused one skipped.
	if strings.Join(names, ",") != "sample-proj-a-gke.europe-west1.gke,sample-proj-c-gke.europe-west1.gke" {
		t.Fatalf("contexts %v", names)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.crmCalls != 2 {
		t.Errorf("projects listed in %d calls", f.crmCalls)
	}

	for _, h := range f.headers {
		if h != "sample-proj" {
			t.Errorf("quota project header %q", h)
		}
	}
}

func TestDiscoverGKEProjectsFilter(t *testing.T) {
	f := newFakeGoogleCloud()
	withFakeGoogleCloud(t, f)

	s := discoverWith(t, discoverGKE, map[string]string{
		gcpFieldUserCredentials: adcJSON("authorized_user", "rt-1"), gcpFieldProjects: "sample-proj-c, sample-proj-c",
	})
	if len(s.Contexts) != 1 || s.Contexts[0].Name != "sample-proj-c-gke.europe-west1.gke" {
		t.Fatalf("contexts %+v", s.Contexts)
	}

	if f.crmCalls != 0 {
		t.Errorf("projects listed although given")
	}

	raw, _ := json.Marshal(map[string]string{gcpFieldUserCredentials: adcJSON("authorized_user", "rt-1"), gcpFieldProjects: "Not/AProject"})
	if _, err := DiscoverClusters(discoverGKE, string(raw)); err == nil || !strings.Contains(err.Error(), "not a project ID") {
		t.Errorf("bad project ID: %v", err)
	}
}

func TestDiscoverGKEAllRefused(t *testing.T) {
	f := newFakeGoogleCloud()
	f.refused = map[string]bool{"sample-proj-a": true, "sample-proj-b": true, "sample-proj-c": true}
	withFakeGoogleCloud(t, f)

	raw, _ := json.Marshal(map[string]string{gcpFieldUserCredentials: adcJSON("authorized_user", "rt-1")})
	if _, err := DiscoverClusters(discoverGKE, string(raw)); err == nil || !strings.Contains(err.Error(), "3 refused") || !strings.Contains(err.Error(), "enter the project IDs") {
		t.Fatalf("all refused: %v", err)
	}

	raw, _ = json.Marshal(map[string]string{gcpFieldUserCredentials: adcJSON("service_account", "rt-1")})
	if _, err := DiscoverClusters(discoverGKE, string(raw)); err == nil || !strings.Contains(err.Error(), "choose Service account key") {
		t.Errorf("a service account key as user credentials: %v", err)
	}
}

// The discovery map also carries the project filter: only the credential is stored.
func TestGKEUserCredentialsDropTheProjectFilter(t *testing.T) {
	store := withAuthStore(t)
	withFakeGoogle(t, &fakeGoogle{refresh: "rt-1", email: "dev@example.com"})

	stored, _ := gkeUserCluster(t)

	secrets, _ := json.Marshal(map[string]string{gcpFieldUserCredentials: adcJSON("authorized_user", "rt-1"), gcpFieldProjects: "sample-proj-a"})
	if err := KubeSetCredentials(stored, "admin@test", string(secrets)); err != nil {
		t.Fatal(err)
	}

	for _, raw := range store.states {
		var state kubeAuthState
		if err := json.Unmarshal([]byte(raw), &state); err != nil || len(state.Secrets) != 1 || state.Secrets[gcpFieldUserCredentials] == "" {
			t.Errorf("stored secrets %v (%v)", state.Secrets, err)
		}
	}
}

// A service account key still lists its own project only, without a quota project.
func TestDiscoverGKEWithServiceAccount(t *testing.T) {
	f := newFakeGoogleCloud()
	withFakeGoogleCloud(t, f)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	der, _ := x509.MarshalPKCS8PrivateKey(key)
	sa, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "viewer@sample-proj-a.iam.gserviceaccount.com", "project_id": "sample-proj-a",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	})

	// The fake token endpoint answers a JWT grant with the user's token too.
	f.google.mu.Lock()
	f.google.jwt = true
	f.google.mu.Unlock()

	s := discoverWith(t, discoverGKE, map[string]string{gcpFieldServiceAccount: string(sa)})
	if len(s.Contexts) != 1 || s.Contexts[0].Name != "sample-proj-a-gke.europe-west1.gke" {
		t.Fatalf("contexts %+v", s.Contexts)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.crmCalls != 0 || len(f.headers) != 1 || f.headers[0] != "" {
		t.Errorf("crm calls %d, headers %q", f.crmCalls, f.headers)
	}
}

func TestKubeDiscoverOptions(t *testing.T) {
	raw, err := KubeDiscoverOptions()
	if err != nil {
		t.Fatal(err)
	}

	var options map[string][][]string
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		t.Fatal(err)
	}

	gke := options[discoverGKE]
	if len(gke) != 2 || gke[0][0] != gcpFieldServiceAccount || strings.Join(gke[1], ",") != gcpFieldUserCredentials+","+gcpFieldProjects {
		t.Errorf("options %v", options)
	}
}
