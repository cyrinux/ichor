package ichorgo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeSTS is Google STS for workforce identity sessions: the refresh token grant with the
// client in a Basic header (never in the form).
type fakeSTS struct {
	mu      sync.Mutex
	refresh string
	rotate  string
	grants  int
}

func (s *fakeSTS) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = r.ParseForm()
	user, pass, basic := r.BasicAuth()

	switch {
	case r.URL.Path != "/v1/oauthtoken":
		http.NotFound(w, r)
	case !basic || user != "wf-client" || pass != "wf-secret" || r.Form.Get("client_id") != "" || r.Form.Get("client_secret") != "":
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid_client"}`)
	case r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != s.refresh:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"The refresh token is expired."}`)
	default:
		s.grants++
		answer := map[string]any{"access_token": "ya29.workforce", "expires_in": 3600}

		if s.rotate != "" {
			answer["refresh_token"] = s.rotate
			s.refresh, s.rotate = s.rotate, ""
		}

		_ = json.NewEncoder(w).Encode(answer)
	}
}

func workforceJSON(tokenURL, refresh string) string {
	raw, _ := json.Marshal(map[string]string{
		"type":           "external_account_authorized_user",
		"audience":       "//iam.googleapis.com/locations/global/workforcePools/sample-pool/providers/sample-idp",
		"client_id":      "wf-client",
		"client_secret":  "wf-secret",
		"refresh_token":  refresh,
		"token_url":      tokenURL,
		"token_info_url": "https://sts.googleapis.com/v1/introspect",
	})

	return string(raw)
}

func TestGKEWorkforceSession(t *testing.T) {
	store := withAuthStore(t)
	sts := &fakeSTS{refresh: "wf-rt-1", rotate: "wf-rt-2"}
	srv := withCloudServer(t, sts.handle)

	stored, api := gkeUserCluster(t)

	if err := KubeSetCredentials(stored, "admin@test", userSecrets(gcpFieldUserCredentials, workforceJSON(srv.URL+"/v1/oauthtoken", "wf-rt-1"))); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
			t.Fatal(err)
		}
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer ya29.workforce" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	// The sign-in check only: the token is reused while it lasts.
	if sts.grants != 1 {
		t.Errorf("%d grants", sts.grants)
	}

	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, "sample-pool (workforce identity)") {
		t.Errorf("the pool is not shown: %s", info)
	}

	// The rotated refresh token is the one stored, sent next and kept by backups.
	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	for _, raw := range store.states {
		backup, err := KubeAuthForBackup(raw)
		if err != nil || !strings.Contains(backup, "wf-rt-2") || strings.Contains(backup, "ya29.workforce") {
			t.Errorf("backup %s (%v)", backup, err)
		}
	}

	// Expired at STS: sign in again, with the login-config command.
	sts.mu.Lock()
	sts.refresh = "something-else"
	sts.mu.Unlock()

	forgetTokens(t, store, stored, "admin@test")

	if _, err := KubeNodes(stored, "admin@test", ""); err == nil || !strings.HasPrefix(err.Error(), KubeSignInRequired) || !strings.Contains(err.Error(), "--login-config") {
		t.Fatalf("after expiry: %v", err)
	}
}

func TestGKEWorkforceRefusals(t *testing.T) {
	withAuthStore(t)
	sts := &fakeSTS{refresh: "wf-rt-1"}
	srv := withCloudServer(t, sts.handle)

	stored, _ := gkeUserCluster(t)

	external, _ := json.Marshal(map[string]any{
		"type": "external_account", "audience": "//iam.googleapis.com/locations/global/workforcePools/sample-pool/providers/sample-idp",
		"credential_source": map[string]string{"file": "/tmp/token"},
	})

	for _, tc := range []struct{ name, field, raw, want string }{
		{"no token_url", gcpFieldUserCredentials, workforceJSON("", "wf-rt-1"), "not a workforce identity session"},
		{"plain http token_url", gcpFieldUserCredentials, workforceJSON("http://sts.example.com/v1/oauthtoken", "wf-rt-1"), "not https"},
		{"login config", gcpFieldUserCredentials, string(external), "--login-config"},
		{"session as service account", gcpFieldServiceAccount, workforceJSON(srv.URL+"/v1/oauthtoken", "wf-rt-1"), "choose gcloud user credentials"},
		{"expired at sign-in", gcpFieldUserCredentials, workforceJSON(srv.URL+"/v1/oauthtoken", "wf-rt-old"), "--login-config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := KubeSetCredentials(stored, "admin@test", userSecrets(tc.field, tc.raw)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWorkforceLabel(t *testing.T) {
	for audience, want := range map[string]string{
		"//iam.googleapis.com/locations/global/workforcePools/sample-pool/providers/sample-idp": "sample-pool (workforce identity)",
		"": "Workforce identity",
		"//iam.googleapis.com/locations/global/workforcePools/": "Workforce identity",
	} {
		if got := workforceLabel(audience); got != want {
			t.Errorf("%q: %q, want %q", audience, got, want)
		}
	}
}
