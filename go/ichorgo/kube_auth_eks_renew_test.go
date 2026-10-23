package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ssoRenewal answers the Identity Center token and role-credentials calls as each case
// says, counting them; tokens and credentials otherwise as AWS gives them.
type ssoRenewal struct {
	*httptest.Server

	mu     sync.Mutex
	tokens int
	creds  int
	// token answers the nth /token call (from 1); nil: a fresh access token.
	token func(w http.ResponseWriter, n int) bool
	// role answers the nth /federation/credentials call; nil: credentials.
	role func(w http.ResponseWriter, n int) bool
}

func newSSORenewal(t *testing.T) *ssoRenewal {
	t.Helper()

	f := &ssoRenewal{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)

		f.mu.Lock()
		defer f.mu.Unlock()

		switch r.URL.Path {
		case "/oidc/token":
			f.tokens++
			if f.token != nil && f.token(w, f.tokens) {
				return
			}

			fmt.Fprintf(w, `{"accessToken":"sso-access-%d","refreshToken":"sso-refresh","expiresIn":28800}`, f.tokens)
		case "/portal/federation/credentials":
			f.creds++
			if f.role != nil && f.role(w, f.creds) {
				return
			}

			fmt.Fprintf(w, `{"roleCredentials":{"accessKeyId":"ASIASSO","secretAccessKey":"x","sessionToken":"y","expiration":%d}}`, time.Now().Add(time.Hour).UnixMilli())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)

	oidc, portal := awsSSOOIDCEndpoint, awsSSOPortalEndpoint
	awsSSOOIDCEndpoint = func(string) string { return f.URL + "/oidc" }
	awsSSOPortalEndpoint = func(string) string { return f.URL + "/portal" }

	t.Cleanup(func() { awsSSOOIDCEndpoint, awsSSOPortalEndpoint = oidc, portal })

	return f
}

func (f *ssoRenewal) counts() (tokens, creds int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.tokens, f.creds
}

// resetConnection drops the connection without an answer, as a phone losing its network does.
func resetConnection(w http.ResponseWriter) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

// signedInSSO is an Identity Center sign-in (invented start URL, account and role) whose
// role credentials ran out; access is the cached access token, "" when it ran out too.
func signedInSSO(access string) kubeAuthState {
	state := kubeAuthState{
		Method: authEKS,
		Secrets: map[string]string{
			awsFieldSSOStartURL: "https://example.awsapps.com/start", awsFieldSSORegion: "eu-west-1",
			awsFieldAccountID: "123456789012", awsFieldRoleName: "ReadOnly",
		},
	}

	expiry := "1"
	if access != "" {
		expiry = fmt.Sprint(time.Now().Add(time.Hour).Unix())
	}

	return state.withSession(map[string]string{
		"ssoClientId": "cid", "ssoClientSecret": "csecret", "ssoRefresh": "sso-refresh",
		"ssoAccess": access, "ssoAccessExpiry": expiry,
	})
}

func TestEKSRenewalFailureIsNotSignInAgain(t *testing.T) {
	cases := []struct {
		name  string
		token func(w http.ResponseWriter, n int) bool
		// signIn: the failure asks for a new sign-in (the refresh token refused).
		signIn bool
	}{
		{"connection reset", func(w http.ResponseWriter, n int) bool {
			if n == 1 {
				resetConnection(w)
			}

			return n == 1
		}, false},
		{"AWS failing", func(w http.ResponseWriter, n int) bool {
			if n == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":"InternalServerException"}`)
			}

			return n == 1
		}, false},
		{"AWS failing without JSON", func(w http.ResponseWriter, n int) bool {
			if n == 1 {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = io.WriteString(w, "<html>bad gateway</html>")
			}

			return n == 1
		}, false},
		{"refresh token expired", func(w http.ResponseWriter, _ int) bool {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"expired_token","error_description":"Refresh token has expired"}`)

			return true
		}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSSORenewal(t)
			f.token = tc.token
			m := &eksMethod{cluster: "prod", region: "eu-west-3", http: f.Client()}

			_, _, next, err := m.mint(context.Background(), signedInSSO(""))

			var needSignIn *errSignInRequired
			if got := errors.As(err, &needSignIn); got != tc.signIn || err == nil {
				t.Fatalf("err %v, sign-in %v", err, got)
			}

			if !tc.signIn {
				if !strings.HasPrefix(err.Error(), "renew the IAM Identity Center token: ") {
					t.Errorf("err %q", err)
				}

				// The store keeps the state of the last good mint; the failed one changes
				// nothing either, so the next try renews with the same refresh token.
				if next.session("ssoRefresh") != "sso-refresh" {
					t.Fatalf("refresh token lost: %v", next.Session)
				}

				if _, _, _, err := m.mint(context.Background(), next); err != nil {
					t.Fatalf("next mint: %v", err)
				}

				return
			}

			if needSignIn.reason != awsSessionEnded {
				t.Errorf("reason %q", needSignIn.reason)
			}

			if next.session("ssoRefresh") != "" {
				t.Errorf("refresh token kept: %v", next.Session)
			}
		})
	}
}

func TestEKSRefusedCachedTokenIsRenewedOnce(t *testing.T) {
	f := newSSORenewal(t)
	// The cached access token is refused once (revoked, or the phone's clock off).
	f.role = func(w http.ResponseWriter, n int) bool {
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"Session token not found or invalid"}`)
		}

		return n == 1
	}
	m := &eksMethod{cluster: "prod", region: "eu-west-3", http: f.Client()}

	token, _, next, err := m.mint(context.Background(), signedInSSO("sso-access-cached"))
	if err != nil || !strings.HasPrefix(token, eksTokenPrefix) {
		t.Fatalf("token %q, err %v", token, err)
	}

	if tokens, creds := f.counts(); tokens != 1 || creds != 2 {
		t.Errorf("%d token and %d credentials calls, want 1 and 2", tokens, creds)
	}

	if next.session("ssoAccess") != "sso-access-1" {
		t.Errorf("access %q", next.session("ssoAccess"))
	}

	// Refused again after a fresh token: the session is over, no loop.
	f.role = func(w http.ResponseWriter, _ int) bool {
		w.WriteHeader(http.StatusUnauthorized)

		return true
	}

	_, _, _, err = m.mint(context.Background(), signedInSSO("sso-access-cached"))

	var needSignIn *errSignInRequired
	if !errors.As(err, &needSignIn) {
		t.Fatalf("err %v", err)
	}

	if tokens, creds := f.counts(); tokens != 2 || creds != 4 {
		t.Errorf("%d token and %d credentials calls, want 2 and 4", tokens, creds)
	}
}

func TestAWSSessionOver(t *testing.T) {
	for code, want := range map[string]bool{
		"expired_token": true, "invalid_grant": true, "invalid_client": true, "unauthorized_client": true, "access_denied": true,
		"": false, "authorization_pending": false, "slow_down": false, "InternalServerException": false, "invalid_request": false,
	} {
		if got := awsSessionOver(code); got != want {
			t.Errorf("awsSessionOver(%q) = %v", code, got)
		}
	}
}
