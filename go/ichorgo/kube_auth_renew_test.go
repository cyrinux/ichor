package ichorgo

import (
	"encoding/json"
	"slices"
	"strconv"
	"testing"
	"time"
)

// seedOIDCSession stores a signed-in OIDC state for the "sso" context of stored: a refresh
// token and a bearer that ends in bearerLeft.
func seedOIDCSession(t *testing.T, stored, refresh string, bearerLeft time.Duration) string {
	t.Helper()

	sc, err := signInContext(stored, "sso")
	if err != nil {
		t.Fatal(err)
	}

	exp := time.Now().Add(bearerLeft).Unix()
	kubeAuth.save(sc.key, kubeAuthState{
		Method:  sc.method.name(),
		Session: map[string]string{"refresh": refresh, "bearer": "old-bearer", "bearerExpiry": strconv.FormatInt(exp, 10)},
		Expires: exp,
	})

	return sc.key
}

func decodeRenewals(t *testing.T, out string) []signInRenewal {
	t.Helper()

	var got []signInRenewal
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("report %s: %v", out, err)
	}

	return got
}

func TestKubeRenewSignIns(t *testing.T) {
	cases := []struct {
		name       string
		bearerLeft time.Duration
		// idpRefresh is the refresh token the provider accepts; the state holds "refresh-0".
		idpRefresh string
		offline    bool
		wantStatus string
		wantGrants int
	}{
		{name: "bearer about to end is renewed", bearerLeft: 30 * time.Second, idpRefresh: "refresh-0", wantStatus: renewRenewed, wantGrants: 1},
		{name: "bearer with time left is kept", bearerLeft: 50 * time.Minute, idpRefresh: "refresh-0", wantStatus: renewValid, wantGrants: 0},
		{name: "session ended at the provider", bearerLeft: 30 * time.Second, idpRefresh: "refresh-other", wantStatus: renewSignInRequired, wantGrants: 1},
		{name: "provider unreachable", bearerLeft: 30 * time.Second, idpRefresh: "refresh-0", offline: true, wantStatus: renewError, wantGrants: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withAuthStore(t)

			idp := newFakeIdP(t)
			idp.refresh = tc.idpRefresh
			api := newFakeKubeAPI(t, map[string]string{})

			stored, err := MergeKubeconfig("", "", oidcKubeconfig(api, idp), "")
			if err != nil {
				t.Fatal(err)
			}

			key := seedOIDCSession(t, stored, "refresh-0", tc.bearerLeft)

			if tc.offline {
				idp.Close()
			}

			out, err := KubeRenewSignIns(stored, "")
			if err != nil {
				t.Fatal(err)
			}

			got := decodeRenewals(t, out)
			if len(got) != 1 || got[0].Context != "sso" || got[0].Kind != kindKube || got[0].Status != tc.wantStatus {
				t.Fatalf("report %s, want one sso %s", out, tc.wantStatus)
			}

			if !tc.offline {
				if grants := idp.grantList(); len(grants) != tc.wantGrants || (tc.wantGrants > 0 && !slices.Equal(grants, []string{"refresh_token"})) {
					t.Errorf("grants %v, want %d refresh", grants, tc.wantGrants)
				}
			}

			state := kubeAuth.load(key)

			switch tc.wantStatus {
			case renewRenewed:
				if got[0].Expires <= time.Now().Add(time.Minute).Unix() || state.session("refresh") == "refresh-0" {
					t.Errorf("renewed: expires %d, refresh %q", got[0].Expires, state.session("refresh"))
				}
			case renewError, renewValid:
				// A failed transport must not cost the user the session.
				if state.session("refresh") != "refresh-0" {
					t.Errorf("refresh token %q after %s", state.session("refresh"), tc.wantStatus)
				}
			}
		})
	}
}

func TestKubeRenewSignInsSkipsNeverSignedIn(t *testing.T) {
	withAuthStore(t)

	idp := newFakeIdP(t)
	api := newFakeKubeAPI(t, map[string]string{})

	stored, err := MergeKubeconfig("", "", oidcKubeconfig(api, idp), "")
	if err != nil {
		t.Fatal(err)
	}

	// An empty or unreadable store (a locked phone) is no list of "sign in again".
	for _, cfg := range []string{"", stored} {
		out, err := KubeRenewSignIns(cfg, "")
		if err != nil || out != "[]" {
			t.Fatalf("KubeRenewSignIns(%d bytes) = %s, %v; want []", len(cfg), out, err)
		}
	}

	if grants := idp.grantList(); len(grants) != 0 {
		t.Errorf("grants %v without a sign-in", grants)
	}
}

func TestKubeRenewSignInsDemoNeedsNoCall(t *testing.T) {
	withAuthStore(t)

	out, err := KubeRenewSignIns(demoKubeconfigForTest(t), "")
	if err != nil || out != "[]" {
		t.Fatalf("demo: %s, %v; want [] (static token, nothing to renew)", out, err)
	}
}
