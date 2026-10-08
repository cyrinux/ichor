package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOmniHostIsOneFormPerInstance(t *testing.T) {
	want := "acme.eu-central-1.omni.example.com"

	for _, endpoint := range []string{
		"https://acme.eu-central-1.omni.example.com",
		"https://ACME.eu-central-1.omni.example.com:443",
		"acme.eu-central-1.omni.example.com",
		"acme.eu-central-1.omni.example.com:443",
		"https://acme.eu-central-1.omni.example.com/",
	} {
		ctx, err := omniInstanceContext(endpoint, testUserIdentity)
		if err != nil {
			t.Fatal(err)
		}

		if got := omniHost(ctx); got != want {
			t.Errorf("%s: host %q, want %q", endpoint, got, want)
		}
	}

	self, _ := omniInstanceContext("https://omni.lab.example.net:8443", testUserIdentity)
	if got := omniHost(self); got != "omni.lab.example.net:8443" {
		t.Fatalf("a port of its own stays: %q", got)
	}
}

func TestOmniSummaryHasItsAuthKey(t *testing.T) {
	cfg := omniTalosconfigYAML(testUserIdentity, testOmniEndpoint, "")

	out, err := ParseConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	var summary configSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}

	_, ctx, _ := resolveContext(cfg, "")
	if got := summary.Contexts[0].AuthKey; got == "" || got != omniAuthKey(ctx) {
		t.Fatalf("authKey = %q, want the key the sign-in is stored under", got)
	}
}

func TestOmniAuthRequestRefusalIsExplained(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, omniOIDCRedirect+"?error=invalid_scope&error_description=unknown+cluster", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	httpc := srv.Client()
	httpc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	_, err := omniAuthRequest(context.Background(), httpc, srv.URL+"/oidc/authorize")
	if !strings.Contains(fmt.Sprint(err), "invalid_scope unknown cluster") {
		t.Fatalf("err = %v, want Omni's reason", err)
	}
}

func TestForgetPrefixDropsTheSources(t *testing.T) {
	useMemAuthStore(t)

	kubeAuth.source(omniKubeSourcePrefix+"key\x00demo", &omniKubeTokens{})
	kubeAuth.save(omniKubeSourcePrefix+"key\x00demo", kubeAuthState{Method: omniKubeMethod})
	kubeAuth.source("other", &omniKubeTokens{})

	kubeAuth.forgetPrefix(omniKubeSourcePrefix + "key\x00")

	if kubeAuth.load(omniKubeSourcePrefix+"key\x00demo").Method != "" {
		t.Fatal("the source's state must go")
	}

	kubeAuth.mu.Lock()
	_, kept := kubeAuth.sources["other"]
	kubeAuth.mu.Unlock()

	if !kept {
		t.Fatal("another source must stay")
	}
}
