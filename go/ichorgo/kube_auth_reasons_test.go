package ichorgo

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionEndedReason(t *testing.T) {
	hoursAgo := func(h int) int64 { return time.Now().Add(-time.Duration(h) * time.Hour).Unix() }
	entra := "AADSTS700082: The refresh token has expired due to inactivity.\r\nTrace ID: 0000\r\nCorrelation ID: 1111"

	cases := []struct {
		name                             string
		source, code, subtype, describes string
		signedInAt                       int64
		want                             string
	}{
		{"EKS expired", authEKS, "expired_token", "", "Refresh token has expired", 0, reasonEKSSession},
		{"EKS invalid grant", authEKS, "invalid_grant", "", "", hoursAgo(9), reasonEKSSession + " Signed in 9 hours ago."},
		{"EKS registration", authEKS, "invalid_client", "", "", 0, reasonEKSRegistration},
		{"EKS other code", authEKS, "access_denied", "", "", 0, "access_denied"},
		{"Google session control", authGKE, "invalid_grant", "invalid_rapt", "", 0, reasonGoogleSession},
		{"Google refused", authGKE, "invalid_grant", "", "Token has been expired or revoked.", hoursAgo(72), reasonGoogleRefused + " Signed in 3 days ago."},
		{"Google refused, no code", authGKE, "", "", "", 0, reasonGoogleRefused},
		{"own client in Testing", gkeOwnClient, "invalid_grant", "", "", hoursAgo(7 * 24), reasonGoogleTesting + " Signed in 7 days ago."},
		{"own client after a week", gkeOwnClient, "invalid_grant", "", "", hoursAgo(30 * 24), reasonGoogleRefused + " Signed in 30 days ago."},
		{"own client, time unknown", gkeOwnClient, "invalid_grant", "", "", 0, reasonGoogleRefused},
		{"own client, session control", gkeOwnClient, "invalid_grant", "invalid_rapt", "", hoursAgo(2), reasonGoogleSession + " Signed in 2 hours ago."},
		{"workforce", gkeWorkforce, "invalid_grant", "", "", 0, reasonWorkforce},
		{"workforce, session control", gkeWorkforce, "invalid_grant", "invalid_rapt", "", 0, reasonGoogleSession},
		{"Entra unused", authAzure, "invalid_grant", "", entra, 0, reasonEntraUnused + " AADSTS700082: The refresh token has expired due to inactivity."},
		{"Entra password", authAzure, "invalid_grant", "", "AADSTS50173: The provided grant has expired due to it being revoked", 0, reasonEntraPassword + " AADSTS50173: The provided grant has expired due to it being revoked."},
		{"Entra other", authAzure, "invalid_grant", "", "AADSTS50078: Presented multi-factor authentication has expired", 0, reasonEntra + " AADSTS50078: Presented multi-factor authentication has expired."},
		{"Entra other code", authAzure, "interaction_required", "", "AADSTS50076: MFA needed\r\nTrace ID: 0", 0, "AADSTS50076: MFA needed"},
		{"OIDC", authOIDC, "invalid_grant", "", "Token is not active", hoursAgo(3), reasonOIDC + " Token is not active. Signed in 3 hours ago."},
		{"OIDC without description", authOIDC, "invalid_grant", "", "", 0, reasonOIDC},
		{"OIDC other code", authOIDC, "unauthorized_client", "", "Client not allowed", 0, "Client not allowed"},
		{"OIDC code only", authOIDC, "invalid_scope", "", "", 0, "invalid_scope"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionEndedReason(tc.source, tc.code, tc.subtype, tc.describes, tc.signedInAt)
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}

			if strings.ContainsAny(got, "\r\n") {
				t.Errorf("reason on several lines: %q", got)
			}
		})
	}
}

func TestSignedInAgo(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	at := func(d time.Duration) int64 { return now.Add(-d).Unix() }

	cases := []struct {
		signedInAt int64
		want       string
	}{
		{0, ""},
		{at(10 * time.Minute), "Signed in less than an hour ago."},
		{at(80 * time.Minute), "Signed in 1 hour ago."},
		{at(47 * time.Hour), "Signed in 47 hours ago."},
		{at(60 * time.Hour), "Signed in 3 days ago."},
		{at(90 * 24 * time.Hour), "Signed in 90 days ago."},
	}

	for _, tc := range cases {
		if got := signedInAgo(tc.signedInAt, now); got != tc.want {
			t.Errorf("signedInAgo(%d) = %q, want %q", tc.signedInAt, got, tc.want)
		}
	}
}

// tokenServer answers every token request with status and body.
func tokenServer(t *testing.T, status int, body string) (*httptest.Server, *tls.Config) {
	t.Helper()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	return srv, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

// refreshedOIDC is an OIDC method (named method, its session told as endedAs) whose token
// endpoint answers with status and body, and a state with a refresh token signed in at
// signedInAt.
func refreshedOIDC(t *testing.T, method, endedAs string, status int, body string, signedInAt int64) (*oidcMethod, kubeAuthState) {
	t.Helper()

	srv, tlsConfig := tokenServer(t, status, body)
	m := &oidcMethod{
		method: method, endedAs: endedAs, clientID: "client", issuer: srv.URL, useAccessToken: true,
		tls: tlsConfig, endpoints: &oidcDiscovery{TokenEndpoint: srv.URL + "/token"},
	}
	state := kubeAuthState{Method: method, SignedInAt: signedInAt}.withSession(map[string]string{"refresh": "old-refresh"})

	return m, state
}

func signInReason(t *testing.T, err error) string {
	t.Helper()

	var needSignIn *errSignInRequired
	if !errors.As(err, &needSignIn) {
		t.Fatalf("err %v, want a sign-in", err)
	}

	if strings.ContainsAny(err.Error(), "\r\n") {
		t.Errorf("message on several lines: %q", err)
	}

	return needSignIn.reason
}

func TestOIDCRefusalSaysWhy(t *testing.T) {
	threeHours := time.Now().Add(-3 * time.Hour).Unix()

	cases := []struct {
		name, method, endedAs, body string
		want                        string
	}{
		{"Keycloak", authOIDC, "", `{"error":"invalid_grant","error_description":"Token is not active"}`,
			reasonOIDC + " Token is not active. Signed in 3 hours ago."},
		{"Entra", authAzure, "", `{"error":"invalid_grant","error_description":"AADSTS700082: The refresh token has expired due to inactivity.\r\nTrace ID: 0"}`,
			reasonEntraUnused + " AADSTS700082: The refresh token has expired due to inactivity. Signed in 3 hours ago."},
		{"Google, own client", authGKE, gkeOwnClient, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`,
			reasonGoogleTesting + " Signed in 3 hours ago."},
		{"Google, session control", authGKE, gkeOwnClient, `{"error":"invalid_grant","error_subtype":"invalid_rapt","error_description":"reauth related error (invalid_rapt)"}`,
			reasonGoogleSession + " Signed in 3 hours ago."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, state := refreshedOIDC(t, tc.method, tc.endedAs, http.StatusBadRequest, tc.body, threeHours)

			_, _, next, err := m.mint(context.Background(), state)
			if got := signInReason(t, err); got != tc.want {
				t.Errorf("reason\n got  %q\n want %q", got, tc.want)
			}

			if next.session("refresh") != "" || next.SignedInAt != threeHours {
				t.Errorf("after the refusal: %+v", next)
			}
		})
	}
}

func TestOIDCSessionEndsFromRefreshExpiresIn(t *testing.T) {
	m, state := refreshedOIDC(t, authOIDC, "", http.StatusOK,
		`{"access_token":"at","expires_in":300,"refresh_token":"new-refresh","refresh_expires_in":1800}`, 0)

	_, _, next, err := m.mint(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}

	if d := time.Until(time.Unix(next.SessionExpires, 0)); d < 29*time.Minute || d > 31*time.Minute {
		t.Errorf("session ends in %v, want 30 minutes", d)
	}

	// A provider that does not say keeps what was known.
	m, state = refreshedOIDC(t, authOIDC, "", http.StatusOK, `{"access_token":"at","expires_in":300}`, 0)
	state.SessionExpires = 42

	if _, _, next, err = m.mint(context.Background(), state); err != nil || next.SessionExpires != 42 {
		t.Errorf("session expires %d, err %v", next.SessionExpires, err)
	}
}

func TestGoogleUserRefusalSaysWhyThenHow(t *testing.T) {
	cases := []struct {
		name, body string
		state      func(kubeAuthState) kubeAuthState
		want       string
	}{
		{"session control", `{"error":"invalid_grant","error_subtype":"invalid_rapt","error_description":"reauth related error (invalid_rapt)"}`,
			func(s kubeAuthState) kubeAuthState { return s },
			reasonGoogleSession + " To sign in again: " + gcpRenewHint + ". Signed in 5 hours ago."},
		{"revoked, gcloud's sign-in", `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`,
			func(s kubeAuthState) kubeAuthState {
				s.Secrets[gcpFieldGcloudSignIn] = "1"
				return s
			},
			reasonGoogleRefused + " To sign in again: " + gcpGcloudRenewHint + ". Signed in 5 hours ago."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withFakeGoogleToken(t, http.StatusBadRequest, tc.body)

			state := tc.state(kubeAuthState{
				Method:     authGKE,
				Secrets:    map[string]string{gcpFieldUserCredentials: adcJSON(gcpTypeAuthorizedUser, "r1")},
				SignedInAt: time.Now().Add(-5 * time.Hour).Unix(),
			})

			_, _, _, err := mintForGoogleUser(context.Background(), state)
			if got := signInReason(t, err); got != tc.want {
				t.Errorf("reason\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

// withFakeGoogleToken points Google's token endpoint at a server answering status and body.
func withFakeGoogleToken(t *testing.T, status int, body string) {
	t.Helper()

	srv := withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})

	token := gcpTokenTarget
	gcpTokenTarget = func(string) string { return srv.URL + "/token" }

	t.Cleanup(func() { gcpTokenTarget = token })
}

func TestEKSRefusalSaysHowLongTheSessionLasted(t *testing.T) {
	f := newSSORenewal(t)
	f.token = func(w http.ResponseWriter, _ int) bool {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Refresh token is invalid"}`)

		return true
	}

	state := signedInSSO("")
	state.SignedInAt = time.Now().Add(-8 * time.Hour).Unix()
	m := &eksMethod{cluster: "prod", region: "eu-west-3", http: f.Client()}

	_, _, _, err := m.mint(context.Background(), state)
	if got, want := signInReason(t, err), reasonEKSSession+" Signed in 8 hours ago."; got != want {
		t.Errorf("reason\n got  %q\n want %q", got, want)
	}
}

func TestSignedInAtStaysOutOfBackups(t *testing.T) {
	state := kubeAuthState{Method: authEKS, Secrets: map[string]string{"k": "v"}, SignedInAt: 1_700_000_000}

	backup, err := KubeAuthForBackup(mustJSON(t, state))
	if err != nil || backup == "" || strings.Contains(backup, "signedInAt") {
		t.Errorf("backup %s, err %v", backup, err)
	}
}
