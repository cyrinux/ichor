package ichorgo

import (
	"encoding/json"
	"slices"
	"testing"
)

// omniKubeconfig is a kubeconfig like `omnictl kubeconfig` writes, for an invented instance:
// kubelogin against Omni's OIDC issuer, the cluster as an extra scope.
const omniKubeconfig = `apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://acme.kubernetes.omni.example.com
  name: acme-demo
contexts:
- context:
    cluster: acme-demo
    namespace: default
    user: acme-demo-someone@example.com
  name: acme-demo
current-context: acme-demo
users:
- name: acme-demo-someone@example.com
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      args:
      - oidc-login
      - get-token
      - --oidc-issuer-url=https://acme.omni.example.com/oidc
      - --oidc-client-id=native
      - --oidc-extra-scope=cluster:demo
      command: kubectl
      env: null
      provideClusterInfo: false
`

func TestOmniKubeconfigSignsInWithOIDC(t *testing.T) {
	out, err := ParseKubeconfig(omniKubeconfig)
	if err != nil {
		t.Fatal(err)
	}

	var summary kubeconfigSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}

	ctx := summary.Contexts[0]
	if ctx.Auth != authOIDC || ctx.SignIn != authOIDC || ctx.Problem != "" {
		t.Fatalf("summary = %+v, want an OIDC sign-in the app can do", ctx)
	}

	doc, err := loadKubeconfigDoc(omniKubeconfig)
	if err != nil {
		t.Fatal(err)
	}

	user, _ := doc.user("acme-demo-someone@example.com")

	m, err := newOIDCMethod(user)
	if err != nil {
		t.Fatal(err)
	}

	// Omni's "native" client registers kubelogin's loopback redirects: the app uses them too.
	if m.issuer != "https://acme.omni.example.com/oidc" || m.clientID != "native" ||
		!slices.Contains(m.scopes, "cluster:demo") || !slices.Equal(m.listen, kubeloginListen) {
		t.Fatalf("method = %+v", m)
	}
}
