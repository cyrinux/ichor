package ichorgo

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testClientCert is a base64 PEM client certificate and key, as a kubeconfig embeds them.
func testClientCert(t *testing.T, cn string, groups []string, notAfter time.Time) (crt, key string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn, Organization: groups},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	b64 := func(kind string, b []byte) string {
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: b}))
	}

	return b64("CERTIFICATE", der), b64("EC PRIVATE KEY", keyDER)
}

func testJWT(sub string, exp int64) string {
	enc := base64.RawURLEncoding.EncodeToString
	payload, _ := json.Marshal(map[string]any{"sub": sub, "exp": exp})

	return enc([]byte(`{"alg":"RS256"}`)) + "." + enc(payload) + ".c2ln"
}

// testKubeconfigAllAuth has one context per sign-in method the preview tells apart.
func testKubeconfigAllAuth(t *testing.T) string {
	t.Helper()

	crt, key := testClientCert(t, "alice", []string{"system:masters"}, time.Unix(2000000000, 0))
	jwt := testJWT("system:serviceaccount:ops:ichor", 1900000000)

	return fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: sa
clusters:
- name: c
  cluster: {server: "https://k8s.example.org:6443"}
- name: plain
  cluster: {server: "http://k8s.example.org:8080"}
- name: proxied
  cluster: {server: "https://k8s.example.org:6443", proxy-url: "socks5://p:1080"}
- name: ca-file
  cluster: {server: "https://k8s.example.org:6443", certificate-authority: /etc/ca.crt}
users:
- {name: cert, user: {client-certificate-data: %s, client-key-data: %s}}
- {name: sa, user: {token: %s}}
- {name: eks, user: {exec: {apiVersion: client.authentication.k8s.io/v1beta1, command: aws, args: [--region, eu-west-1, eks, get-token, --cluster-name, prod]}}}
- {name: gke, user: {exec: {command: gke-gcloud-auth-plugin}}}
- {name: oidc, user: {exec: {command: kubectl, args: [oidc-login, get-token, "--oidc-issuer-url=https://id.example.org", --oidc-client-id=k8s]}}}
- {name: azure, user: {exec: {command: kubelogin, args: [get-token, --login, devicecode, --server-id, 6dae42f8, --tenant-id, t1]}}}
- {name: do, user: {exec: {command: doctl, args: [kubernetes, cluster, kubeconfig, exec-credential, abc]}}}
- {name: rancher, user: {exec: {command: rancher, args: [token, --server, rancher.example.org]}}}
- {name: legacy-oidc, user: {auth-provider: {name: oidc, config: {idp-issuer-url: "https://old.example.org"}}}}
- {name: custom, user: {exec: {command: /usr/local/bin/my-auth}}}
- {name: basic, user: {username: u, password: p}}
- {name: token-file, user: {tokenFile: /var/run/token}}
- {name: empty, user: {}}
contexts:
- {name: cert, context: {cluster: c, user: cert}}
- {name: sa, context: {cluster: c, user: sa, namespace: ops}}
- {name: eks, context: {cluster: c, user: eks}}
- {name: gke, context: {cluster: c, user: gke}}
- {name: oidc, context: {cluster: c, user: oidc}}
- {name: azure, context: {cluster: c, user: azure}}
- {name: do, context: {cluster: c, user: do}}
- {name: rancher, context: {cluster: c, user: rancher}}
- {name: legacy-oidc, context: {cluster: c, user: legacy-oidc}}
- {name: custom, context: {cluster: c, user: custom}}
- {name: basic, context: {cluster: c, user: basic}}
- {name: token-file, context: {cluster: c, user: token-file}}
- {name: empty, context: {cluster: c, user: empty}}
- {name: plain, context: {cluster: plain, user: sa}}
- {name: proxied, context: {cluster: proxied, user: sa}}
- {name: ca-file, context: {cluster: ca-file, user: sa}}
- {name: no-user, context: {cluster: c, user: nobody}}
- {name: no-cluster, context: {cluster: nowhere, user: sa}}
`, crt, key, jwt)
}

func parseKubeSummary(t *testing.T, yaml string) kubeconfigSummary {
	t.Helper()

	out, err := ParseKubeconfig(yaml)
	if err != nil {
		t.Fatal(err)
	}

	var s kubeconfigSummary
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}

	return s
}

func TestIsKubeconfig(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]bool{
		testKubeconfigAllAuth(t):                 true,
		"clusters: []\ncontexts: []\n":           true,
		demo:                                     false,
		testConfig(t, time.Now().Add(time.Hour)): false,
		"":                                       false,
		"clusters: {a: b}":                       false,
		"::: not yaml":                           false,
	}

	for yaml, want := range cases {
		if got := IsKubeconfig(yaml); got != want {
			t.Errorf("IsKubeconfig(%.40q) = %v, want %v", yaml, got, want)
		}
	}
}

func TestParseKubeconfigPreview(t *testing.T) {
	s := parseKubeSummary(t, testKubeconfigAllAuth(t))

	if s.Current != "sa" {
		t.Errorf("current = %q", s.Current)
	}

	type row struct{ auth, detail, problem string }

	want := map[string]row{
		"cert":        {authCert, "", ""},
		"sa":          {authToken, "", ""},
		"eks":         {authEKS, "prod", KubeProblemSignInLater},
		"gke":         {authGKE, "", KubeProblemSignInLater},
		"oidc":        {authOIDC, "https://id.example.org", ""},
		"azure":       {authAzure, "t1", KubeProblemSignInLater},
		"do":          {authDigitalOcean, "", KubeProblemSignInLater},
		"rancher":     {authRancher, "rancher.example.org", KubeProblemSignInLater},
		"legacy-oidc": {authOIDC, "https://old.example.org", KubeProblemInvalid}, // no client ID
		"custom":      {authExec, "my-auth", KubeProblemExec},
		"basic":       {authBasic, "", KubeProblemBasicAuth},
		"token-file":  {authToken, "", KubeProblemFilePath},
		"empty":       {authNone, "", KubeProblemNoCredentials},
		"plain":       {authToken, "", KubeProblemNotHTTPS},
		"proxied":     {authToken, "", KubeProblemProxy},
		"ca-file":     {authToken, "", KubeProblemFilePath},
		"no-user":     {authNone, "", KubeProblemUserMissing},
		"no-cluster":  {authToken, "", KubeProblemClusterMissing},
	}

	if len(s.Contexts) != len(want) {
		t.Fatalf("got %d contexts, want %d", len(s.Contexts), len(want))
	}

	for i, c := range s.Contexts {
		if i > 0 && s.Contexts[i-1].Name > c.Name {
			t.Errorf("contexts not sorted: %q before %q", s.Contexts[i-1].Name, c.Name)
		}

		w, ok := want[c.Name]
		if !ok {
			t.Errorf("unexpected context %q", c.Name)

			continue
		}

		if c.Kind != kindKube || c.Auth != w.auth || c.AuthDetail != w.detail || c.Problem != w.problem {
			t.Errorf("%s: kind %q auth %q detail %q problem %q, want auth %q detail %q problem %q",
				c.Name, c.Kind, c.Auth, c.AuthDetail, c.Problem, w.auth, w.detail, w.problem)
		}

		if len(c.Fingerprint) != fingerprintLength || len(c.ClusterID) != fingerprintLength {
			t.Errorf("%s: fingerprint %q cluster id %q", c.Name, c.Fingerprint, c.ClusterID)
		}
	}

	byName := map[string]kubeContextSummary{}
	for _, c := range s.Contexts {
		byName[c.Name] = c
	}

	if c := byName["cert"]; c.User != "alice" || c.CertNotAfter != 2000000000 || len(c.Roles) != 1 || c.Roles[0] != "system:masters" {
		t.Errorf("cert identity: %+v", c)
	}

	if c := byName["sa"]; c.User != "system:serviceaccount:ops:ichor" || c.CertNotAfter != 1900000000 || c.Namespace != "ops" {
		t.Errorf("token identity: %+v", c)
	}

	if c := byName["sa"]; len(c.Endpoints) != 1 || c.Endpoints[0] != "https://k8s.example.org:6443" {
		t.Errorf("endpoints: %v", c.Endpoints)
	}

	if byName["cert"].ClusterID != byName["sa"].ClusterID || byName["cert"].Fingerprint == byName["sa"].Fingerprint {
		t.Error("contexts of one cluster share its id, not their fingerprint")
	}
}

func TestParseKubeconfigRejectsBrokenFiles(t *testing.T) {
	for name, yaml := range map[string]string{
		"empty":       "",
		"no contexts": "apiVersion: v1\nkind: Config\nclusters: []\n",
		"duplicate":   "contexts: [{name: a, context: {}}, {name: a, context: {}}]",
		"unnamed":     "contexts: [{context: {}}]",
	} {
		if _, err := ParseKubeconfig(yaml); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestMergeKubeconfigImportsOnlyWhatWorks(t *testing.T) {
	merged, err := MergeKubeconfig("", "", testKubeconfigAllAuth(t), "")
	if err != nil {
		t.Fatal(err)
	}

	s := parseKubeSummary(t, merged)
	if len(s.Contexts) != 3 || s.Contexts[0].Name != "cert" || s.Contexts[1].Name != "oidc" || s.Contexts[2].Name != "sa" {
		t.Fatalf("merged contexts: %+v", s.Contexts)
	}

	if s.Contexts[1].SignIn != authOIDC || s.Contexts[0].SignIn != "" {
		t.Errorf("sign-in methods: %+v", s.Contexts)
	}

	if s.Current != "sa" {
		t.Errorf("current = %q, want the file's current", s.Current)
	}

	doc, err := loadKubeconfigDoc(merged)
	if err != nil {
		t.Fatal(err)
	}

	// Each context has a cluster and a user of its own, named after it.
	for _, c := range doc.Contexts {
		if c.Context.Cluster != c.Name || c.Context.User != c.Name {
			t.Errorf("%s: cluster %q user %q", c.Name, c.Context.Cluster, c.Context.User)
		}
	}

	if len(doc.Clusters) != 3 || len(doc.Users) != 3 {
		t.Errorf("%d clusters, %d users", len(doc.Clusters), len(doc.Users))
	}

	if doc.Contexts[2].Context.Namespace != "ops" {
		t.Error("namespace lost")
	}
}

func TestMergeKubeconfigSkipsAndRefusesEmpty(t *testing.T) {
	added := testKubeconfigAllAuth(t)

	// cert is index 1 and sa index 14 in ParseKubeconfig's order.
	s := parseKubeSummary(t, added)
	certIdx, saIdx, oidcIdx := -1, -1, -1

	for i, c := range s.Contexts {
		switch c.Name {
		case "cert":
			certIdx = i
		case "sa":
			saIdx = i
		case "oidc":
			oidcIdx = i
		}
	}

	merged, err := MergeKubeconfig("", "", added, fmt.Sprintf(`[{"index":%d,"skip":true},{"index":%d,"skip":true}]`, certIdx, oidcIdx))
	if err != nil {
		t.Fatal(err)
	}

	if got := parseKubeSummary(t, merged); len(got.Contexts) != 1 || got.Contexts[0].Name != "sa" {
		t.Fatalf("skip ignored: %+v", got.Contexts)
	}

	_, err = MergeKubeconfig("", "", added, fmt.Sprintf(`[{"index":%d,"skip":true},{"index":%d,"skip":true},{"index":%d,"skip":true}]`, certIdx, saIdx, oidcIdx))
	if err == nil {
		t.Fatal("importing nothing must fail")
	}
}

func TestMergeKubeconfigNamesAcrossStores(t *testing.T) {
	talos := testConfig(t, time.Now().Add(time.Hour))

	talosNames, err := ParseConfig(talos)
	if err != nil {
		t.Fatal(err)
	}

	var ts configSummary
	if err := json.Unmarshal([]byte(talosNames), &ts); err != nil {
		t.Fatal(err)
	}

	taken := ts.Contexts[0].Name
	added := strings.ReplaceAll(singleTokenKubeconfig("https://a.example.org:6443", "t"), "name: x\n", "name: "+taken+"\n")

	conflictsJSON, err := KubeImportConflicts("", talos, added)
	if err != nil {
		t.Fatal(err)
	}

	var conflicts []importConflict
	if err := json.Unmarshal([]byte(conflictsJSON), &conflicts); err != nil {
		t.Fatal(err)
	}

	if len(conflicts) != 1 || conflicts[0].Suggested != taken+"-1" || conflicts[0].SameAs != "" {
		t.Fatalf("conflicts %+v", conflicts)
	}

	merged, err := MergeKubeconfig("", talos, added, "")
	if err != nil {
		t.Fatal(err)
	}

	if s := parseKubeSummary(t, merged); s.Contexts[0].Name != taken+"-1" {
		t.Fatalf("a Talos name was reused: %q", s.Contexts[0].Name)
	}
}

func singleTokenKubeconfig(server, token string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: x
clusters:
- name: c
  cluster: {server: %q}
users:
- name: u
  user: {token: %q}
contexts:
- name: x
  context: {cluster: c, user: u}
`, server, token)
}

func TestMergeKubeconfigReplacesSameCluster(t *testing.T) {
	stored, err := MergeKubeconfig("", "", singleTokenKubeconfig("https://a.example.org:6443", "old"), "")
	if err != nil {
		t.Fatal(err)
	}

	// Same server (no CA): the user may replace the stored credentials.
	updated, err := MergeKubeconfig(stored, "", singleTokenKubeconfig("https://a.example.org:6443", "new"), `[{"index":0,"replace":true}]`)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := loadKubeconfigDoc(updated)
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.Contexts) != 1 || len(doc.Users) != 1 || doc.Users[0].User.Token != "new" {
		t.Fatalf("not replaced: %+v", doc)
	}

	// Another cluster under the same name cannot replace it.
	_, err = MergeKubeconfig(stored, "", singleTokenKubeconfig("https://b.example.org:6443", "new"), `[{"index":0,"replace":true}]`)
	if err == nil {
		t.Fatal("replacing another cluster must fail")
	}

	// Without a choice it is added under a free name.
	added, err := MergeKubeconfig(stored, "", singleTokenKubeconfig("https://b.example.org:6443", "new"), "")
	if err != nil {
		t.Fatal(err)
	}

	if s := parseKubeSummary(t, added); len(s.Contexts) != 2 || s.Contexts[1].Name != "x-1" {
		t.Fatalf("contexts %+v", s.Contexts)
	}
}

func TestRemoveAndExportKubeContext(t *testing.T) {
	stored, err := MergeKubeconfig("", "", testKubeconfigAllAuth(t), "")
	if err != nil {
		t.Fatal(err)
	}

	exported, err := ExportKubeContext(stored, "cert")
	if err != nil {
		t.Fatal(err)
	}

	if s := parseKubeSummary(t, exported); len(s.Contexts) != 1 || s.Contexts[0].Name != "cert" || s.Contexts[0].Problem != "" {
		t.Fatalf("export %+v", s.Contexts)
	}

	rest, err := RemoveKubeContext(stored, "cert")
	if err != nil {
		t.Fatal(err)
	}

	if rest, err = RemoveKubeContext(rest, "oidc"); err != nil {
		t.Fatal(err)
	}

	doc, err := loadKubeconfigDoc(rest)
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.Contexts) != 1 || len(doc.Clusters) != 1 || len(doc.Users) != 1 || doc.CurrentContext != "sa" {
		t.Fatalf("after remove: %+v", doc)
	}

	last, err := RemoveKubeContext(rest, "sa")
	if err != nil || last != "" {
		t.Fatalf("removing the last context: %q, %v", last, err)
	}

	if _, err := RemoveKubeContext(rest, "missing"); err == nil {
		t.Fatal("removing an unknown context must fail")
	}
}

func TestKubeconfigClusterUsesItsOwnServer(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /api/v1/namespaces": `{"items":[]}`})

	added := strings.Replace(f.kubeconfigFor(f.URL), "server: https://other.invalid:6443", "server: "+f.URL, 1)

	stored, err := MergeKubeconfig("", "", added, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := KubeNamespaces(stored, "admin@test", ""); err != nil {
		t.Fatal(err)
	}

	r := f.recorded()
	if len(r) == 0 || r[len(r)-1].auth != "Bearer secret-token" {
		t.Fatalf("requests %+v", r)
	}
}

func TestKubeconfigClusterHasNoTalosAPI(t *testing.T) {
	stored, err := MergeKubeconfig("", "", singleTokenKubeconfig("https://a.example.org:6443", "t"), "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := openSession(stored, "x"); !errors.Is(err, errTalosUnavailable) {
		t.Fatalf("openSession: %v", err)
	}

	if _, err := ParseConfig(stored); err == nil || !strings.Contains(err.Error(), "no Talos API") {
		t.Fatalf("ParseConfig: %v", err)
	}
}

func TestParseKubeconfigMasked(t *testing.T) {
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	out, err := ParseKubeconfig(singleTokenKubeconfig("https://prod-api.acme-corp.example:6443", "t"))
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, "acme-corp") || strings.Contains(out, "prod-api") {
		t.Fatalf("server not masked: %s", out)
	}
}

func TestKubeNodes(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/nodes": `{"items":[
		  {"metadata":{"name":"w1","labels":{"node-role.kubernetes.io/worker":""},"creationTimestamp":"2026-01-02T03:04:05Z"},
		   "spec":{"unschedulable":true},
		   "status":{"allocatable":{"cpu":"3500m","memory":"8Gi","pods":"110"},
		     "conditions":[{"type":"Ready","status":"True"},{"type":"DiskPressure","status":"True"},{"type":"MemoryPressure","status":"False"}],
		     "addresses":[{"type":"InternalIP","address":"10.0.0.5"},{"type":"Hostname","address":"w1"}],
		     "nodeInfo":{"kubeletVersion":"v1.34.1","architecture":"arm64"}}},
		  {"metadata":{"name":"cp1","labels":{"node-role.kubernetes.io/master":""}},
		   "status":{"conditions":[{"type":"Ready","status":"False"}]}}]}`,
	})

	stored, err := MergeKubeconfig("", "", strings.Replace(f.kubeconfigFor(f.URL), "https://other.invalid:6443", f.URL, 1), "")
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeNodes(stored, "admin@test", "")
	if err != nil {
		t.Fatal(err)
	}

	var got kubeNodesOverview
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if got.ServerVersion != "v1.34.0" || len(got.Nodes) != 2 {
		t.Fatalf("overview %+v", got)
	}

	cp, w := got.Nodes[0], got.Nodes[1]
	if cp.Name != "cp1" || cp.Ready || len(cp.Roles) != 1 || cp.Roles[0] != "control-plane" {
		t.Errorf("control plane first, legacy master role mapped: %+v", cp)
	}

	if !w.Ready || !w.Cordoned || w.CPU != 3.5 || w.Memory != 8<<30 || w.PodLimit != 110 || w.InternalIP != "10.0.0.5" ||
		w.Arch != "arm64" || len(w.Pressure) != 1 || w.Pressure[0] != "DiskPressure" || w.Created == 0 {
		t.Errorf("worker %+v", w)
	}
}

func TestKubeNodesForbidden(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)

			return
		}

		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"nodes is forbidden"}`)
	}))
	t.Cleanup(srv.Close)

	added := strings.Replace(singleTokenKubeconfig(srv.URL, "t"), "cluster: {server:", "cluster: {insecure-skip-tls-verify: true, server:", 1)

	stored, err := MergeKubeconfig("", "", added, "")
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeNodes(stored, "x", "")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"forbidden":true`) || !strings.Contains(out, `"serverVersion":"v1.34.0"`) {
		t.Fatalf("got %s", out)
	}
}

func TestTalosImportAvoidsKubeNames(t *testing.T) {
	talos := testConfig(t, time.Now().Add(time.Hour))

	var ts configSummary

	out, err := ParseConfig(talos)
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal([]byte(out), &ts); err != nil {
		t.Fatal(err)
	}

	name := ts.Contexts[0].Name

	kube, err := MergeKubeconfig("", "", strings.ReplaceAll(singleTokenKubeconfig("https://a.example.org:6443", "t"), "name: x\n", "name: "+name+"\n"), "")
	if err != nil {
		t.Fatal(err)
	}

	conflicts, err := TalosImportConflicts("", kube, talos)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(conflicts, `"suggested":"`+name+`-1"`) {
		t.Fatalf("conflicts %s", conflicts)
	}

	merged, err := MergeTalosconfig("", kube, talos, "")
	if err != nil {
		t.Fatal(err)
	}

	out, err = ParseConfig(merged)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(out, `"name":"`+name+`-1"`) || !strings.Contains(out, `"current":"`+name+`-1"`) {
		t.Fatalf("merged %s", out)
	}
}
