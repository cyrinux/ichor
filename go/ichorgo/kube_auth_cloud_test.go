package ichorgo

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func withCloudServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()

	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)

	client, do := cloudHTTP, doAPIEndpoint
	cloudHTTP = func() *http.Client { return srv.Client() }
	doAPIEndpoint = srv.URL

	t.Cleanup(func() { cloudHTTP, doAPIEndpoint = client, do })

	return srv
}

func execKubeconfig(f *fakeKubeAPI, command string, args ...string) string {
	quoted, _ := json.Marshal(args)

	return strings.Replace(f.kubeconfigFor(f.URL), "    token: secret-token\n", "    exec:\n      command: "+command+"\n      args: "+string(quoted)+"\n", 1)
}

func TestGKEServiceAccount(t *testing.T) {
	withAuthStore(t)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	der, _ := x509.MarshalPKCS8PrivateKey(key)

	srv := withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()

		parts := strings.Split(r.Form.Get("assertion"), ".")
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || len(parts) != 3 {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		_, _ = io.WriteString(w, `{"access_token":"ya29.gke","expires_in":3599}`)
	})

	sa, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "viewer@proj.iam.gserviceaccount.com", "private_key_id": "k1",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": srv.URL + "/token",
	})

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", execKubeconfig(api, "gke-gcloud-auth-plugin"), "")
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeSetCredentials(stored, "admin@test", "{}"); err == nil {
		t.Fatal("empty credentials accepted")
	}

	secrets, _ := json.Marshal(map[string]string{gcpFieldServiceAccount: string(sa)})
	if err := KubeSetCredentials(stored, "admin@test", string(secrets)); err != nil {
		t.Fatal(err)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	r := api.recorded()
	if r[len(r)-1].auth != "Bearer ya29.gke" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, "viewer@proj") {
		t.Errorf("info %s", info)
	}
}

func TestDigitalOceanToken(t *testing.T) {
	withAuthStore(t)

	withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/kubernetes/clusters/1f2e-abc/credentials" {
			http.NotFound(w, r)

			return
		}

		if r.Header.Get("Authorization") != "Bearer dop_v1_good" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"id":"Unauthorized","message":"Unable to authenticate you"}`)

			return
		}

		fmt.Fprintf(w, `{"token":"do-cluster-token","expires_at":%q}`, time.Now().Add(7*24*time.Hour).UTC().Format(time.RFC3339))
	})

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", execKubeconfig(api, "doctl", "kubernetes", "cluster", "kubeconfig", "exec-credential", "--version=v1beta1", "--context=default", "1f2e-abc"), "")
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeSetCredentials(stored, "admin@test", `{"doApiToken":"dop_v1_bad"}`); err == nil || !strings.Contains(err.Error(), "Unable to authenticate") {
		t.Fatalf("bad token: %v", err)
	}

	if err := KubeSetCredentials(stored, "admin@test", `{"doApiToken":"dop_v1_good"}`); err != nil {
		t.Fatal(err)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer do-cluster-token" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}
}

func TestRancherAPIKey(t *testing.T) {
	store := withAuthStore(t)
	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", execKubeconfig(api, "rancher", "token", "--server", "rancher.example.org", "--user", "u-abc"), "")
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeSetCredentials(stored, "admin@test", `{"rancherApiKey":"nope"}`); err == nil {
		t.Fatal("malformed key accepted")
	}

	if err := KubeSetCredentials(stored, "admin@test", `{"rancherApiKey":"token-ab12c:s3cr3t"}`); err != nil {
		t.Fatal(err)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if r := api.recorded(); r[len(r)-1].auth != "Bearer token-ab12c:s3cr3t" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	// A typed key is a secret: backed up.
	if backup, _ := KubeAuthForBackup(store.Load(stateKeyOf(t, stored, "admin@test"))); !strings.Contains(backup, "token-ab12c") {
		t.Errorf("backup %s", backup)
	}
}

func azureKubeconfig(f *fakeKubeAPI, login string) string {
	return execKubeconfig(f, "kubelogin", "get-token", "--login", login, "--server-id", "6dae42f8-4368-4678-94ff-3960e28e3630",
		"--client-id", "80faf920-1908-4b52-b5ef-a8e7bedfc67a", "--tenant-id", "tenant-1", "--environment", "AzurePublicCloud")
}

func withFakeEntra(t *testing.T) *fakeIdP {
	t.Helper()

	idp := newFakeIdP(t)
	authority, tlsConfig := azureAuthority, azureTLS
	azureAuthority = func(string, string) string { return idp.URL }
	azureTLS = func() *tls.Config {
		return &tls.Config{RootCAs: idp.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs}
	}

	t.Cleanup(func() { azureAuthority, azureTLS = authority, tlsConfig })

	return idp
}

func TestAzureDeviceCode(t *testing.T) {
	withAuthStore(t)

	old := deviceMinInterval
	deviceMinInterval = 10 * time.Millisecond

	t.Cleanup(func() { deviceMinInterval = old })

	idp := withFakeEntra(t)
	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", azureKubeconfig(api, "devicecode"), "")
	if err != nil {
		t.Fatal(err)
	}

	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, `"method":"azure"`) || !strings.Contains(info, `"kind":"browser"`) {
		t.Fatalf("info %s", info)
	}

	rec := recSignIn{prompts: make(chan signInPrompt, 1), done: make(chan string, 1)}
	StartKubeSignIn(stored, "admin@test", rec)

	if p := <-rec.prompts; p.Kind != "device" {
		t.Fatalf("prompt %+v", p)
	}

	if msg := <-rec.done; msg != "" {
		t.Fatalf("sign-in: %s", msg)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	// AKS takes the access token, not the ID token.
	if r := api.recorded(); r[len(r)-1].auth != "Bearer at" {
		t.Fatalf("bearer %q", r[len(r)-1].auth)
	}

	_ = idp
}

func TestAzureServicePrincipal(t *testing.T) {
	withAuthStore(t)

	idp := withFakeEntra(t)
	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	stored, err := MergeKubeconfig("", "", azureKubeconfig(api, "spn"), "")
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeSetCredentials(stored, "admin@test", `{"azureClientId":"app-1","azureClientSecret":"s3cret"}`); err != nil {
		t.Fatal(err)
	}

	// The client ID comes back to fill the form after a rotated secret; the secret never does.
	if info, _ := KubeSignInInfo(stored, "admin@test"); !strings.Contains(info, `"values":{"azureClientId":"app-1"}`) || strings.Contains(info, "s3cret") {
		t.Fatalf("info %s", info)
	}

	if _, err := KubeNodes(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	if g := idp.grantList(); len(g) != 1 || g[0] != "client_credentials" {
		t.Fatalf("grants %v", g)
	}
}
