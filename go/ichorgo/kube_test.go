package ichorgo

import (
	"context"
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
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeKubeAPI is an API server answering from a path -> JSON table and recording requests.
type fakeKubeAPI struct {
	*httptest.Server

	mu       sync.Mutex
	answers  map[string]string // "GET /path" -> body
	requests []fakeKubeRequest
}

type fakeKubeRequest struct {
	method, path, contentType, auth, body, query string
}

func newFakeKubeAPI(t *testing.T, answers map[string]string) *fakeKubeAPI {
	t.Helper()

	f := &fakeKubeAPI{answers: answers}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		f.mu.Lock()
		f.requests = append(f.requests, fakeKubeRequest{r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("Authorization"), string(body), r.URL.RawQuery})
		answer, ok := f.answers[r.Method+" "+r.URL.Path]
		f.mu.Unlock()

		if r.URL.Path == "/version" && !ok {
			answer, ok = `{"gitVersion":"v1.34.0"}`, true
		}

		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"NotFound","message":"`+r.URL.Path+` not found"}`)

			return
		}

		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(f.Close)

	return f
}

func (f *fakeKubeAPI) recorded() []fakeKubeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]fakeKubeRequest(nil), f.requests...)
}

// kubeconfigFor is a token kubeconfig for f, with server as the API address.
func (f *fakeKubeAPI) kubeconfigFor(server string) string {
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.Certificate().Raw})

	return fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: admin@test
clusters:
- name: other
  cluster:
    server: https://other.invalid:6443
- name: test
  cluster:
    server: %s
    certificate-authority-data: %s
users:
- name: admin@test
  user:
    token: secret-token
contexts:
- name: other@test
  context: {cluster: other, user: admin@test}
- name: admin@test
  context: {cluster: test, user: admin@test}
`, server, base64.StdEncoding.EncodeToString(ca))
}

func TestParseKubeconfigCurrentContext(t *testing.T) {
	f := newFakeKubeAPI(t, nil)

	creds, err := parseKubeconfig(f.kubeconfigFor(f.URL))
	if err != nil {
		t.Fatal(err)
	}

	if creds.server.String() != f.URL || creds.token != "secret-token" || creds.tls.RootCAs == nil {
		t.Fatalf("unexpected credentials: server %s token %q", creds.server, creds.token)
	}
}

func TestParseKubeconfigClientCertificate(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "admin"}, NotAfter: time.Now().Add(time.Hour)}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	b64 := func(typ string, b []byte) string {
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b}))
	}

	cfg := fmt.Sprintf(`clusters:
- name: c
  cluster: {server: "https://10.0.0.1:6443"}
users:
- name: u
  user: {client-certificate-data: %s, client-key-data: %s}
contexts:
- name: x
  context: {cluster: c, user: u}
`, b64("CERTIFICATE", der), b64("EC PRIVATE KEY", keyDER))

	creds, err := parseKubeconfig(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if len(creds.tls.Certificates) != 1 || creds.token != "" {
		t.Fatalf("client certificate not loaded")
	}
}

func TestParseKubeconfigRejects(t *testing.T) {
	cases := map[string]string{
		"exec plugin": `clusters: [{name: c, cluster: {server: "https://a:6443"}}]
users: [{name: u, user: {exec: {command: kubelogin}}}]
contexts: [{name: x, context: {cluster: c, user: u}}]`,
		"plain http": `clusters: [{name: c, cluster: {server: "http://a:6443"}}]
users: [{name: u, user: {token: t}}]
contexts: [{name: x, context: {cluster: c, user: u}}]`,
		"missing user": `clusters: [{name: c, cluster: {server: "https://a:6443"}}]
contexts: [{name: x, context: {cluster: c, user: u}}]`,
		"no context": `clusters: []`,
	}

	for name, cfg := range cases {
		if _, err := parseKubeconfig(cfg); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestKubeServerCandidates(t *testing.T) {
	server, _ := url.Parse("https://vip.lan:6443")

	got := kubeServerCandidates(server, []string{"10.0.0.11", "10.0.0.11:50000", "[fd00::1]:50000", "vip.lan", ""})

	var hosts []string
	for _, u := range got {
		hosts = append(hosts, u.Host)
	}

	want := "vip.lan:6443 10.0.0.11:6443 [fd00::1]:6443"
	if strings.Join(hosts, " ") != want {
		t.Fatalf("got %v, want %s", hosts, want)
	}
}

func TestOpenKubeClientFallsBackToTalosEndpoint(t *testing.T) {
	f := newFakeKubeAPI(t, nil)
	port := f.Listener.Addr().(interface{ String() string }).String()
	port = port[strings.LastIndex(port, ":")+1:]

	// The kubeconfig names an address the phone cannot resolve; the Talos endpoint works.
	k, err := openKubeClient(context.Background(), f.kubeconfigFor("https://unreachable.invalid:"+port), []string{"127.0.0.1:50000"}, "")
	if err != nil {
		t.Fatal(err)
	}

	if k.base.Host != "127.0.0.1:"+port {
		t.Fatalf("picked %s", k.base.Host)
	}

	if r := f.recorded(); len(r) != 1 || r[0].auth != "Bearer secret-token" {
		t.Fatalf("unexpected requests %+v", r)
	}
}

func TestOpenKubeClientReportsUnreachable(t *testing.T) {
	f := newFakeKubeAPI(t, nil)

	_, err := openKubeClient(context.Background(), f.kubeconfigFor("https://unreachable.invalid:1"), nil, "")
	if err == nil || !strings.Contains(err.Error(), "not reachable from this device") {
		t.Fatalf("got %v", err)
	}
}

func TestKubeAPIErrorMessage(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	err = k.get(context.Background(), "/apis/apps/v1/nope", &struct{}{})

	var apiErr *kubeAPIError
	if !errors.As(err, &apiErr) || apiErr.Code != 404 || !strings.Contains(err.Error(), "/apis/apps/v1/nope not found") {
		t.Fatalf("got %v", err)
	}
}

const fakeDeployments = `{"items":[
 {"metadata":{"name":"web","namespace":"shop","generation":4,"creationTimestamp":"2026-01-02T03:04:05Z"},
  "spec":{"replicas":3,"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":"2026-09-30T10:00:00Z"}},"spec":{"containers":[{"image":"nginx:1.27"},{"image":"envoy:1"}]}}},
  "status":{"observedGeneration":4,"replicas":3,"readyReplicas":3,"updatedReplicas":3,"availableReplicas":3}},
 {"metadata":{"name":"api","namespace":"shop","generation":5},
  "spec":{"replicas":2,"template":{"spec":{"containers":[{"image":"api:2"}]}}},
  "status":{"observedGeneration":4,"readyReplicas":2,"updatedReplicas":2,"availableReplicas":2}},
 {"metadata":{"name":"idle","namespace":"a"},"spec":{"replicas":0},"status":{}},
 {"metadata":{"name":"frozen","namespace":"a"},"spec":{"replicas":1,"paused":true},"status":{"readyReplicas":1,"updatedReplicas":1}}
]}`

func TestListWorkloads(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/apps/v1/deployments": fakeDeployments,
		"GET /apis/apps/v1/statefulsets": `{"items":[{"metadata":{"name":"db","namespace":"shop"},
			"spec":{"template":{"spec":{"containers":[{"image":"postgres:17"}]}}},"status":{"readyReplicas":0,"updatedReplicas":1}}]}`,
		"GET /apis/apps/v1/daemonsets": `{"items":[{"metadata":{"name":"proxy","namespace":"kube-system"},
			"status":{"desiredNumberScheduled":3,"numberReady":3,"updatedNumberScheduled":3,"numberAvailable":3}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	list, err := listWorkloads(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, w := range list.Workloads {
		got = append(got, fmt.Sprintf("%s %s/%s %d/%d %s", w.Kind, w.Namespace, w.Name, w.Ready, w.Desired, w.State))
	}

	want := []string{
		"Deployment a/frozen 1/1 paused",
		"Deployment a/idle 0/0 scaledDown",
		"DaemonSet kube-system/proxy 3/3 ready",
		"Deployment shop/api 2/2 progressing", // generation not observed yet
		"StatefulSet shop/db 0/1 degraded",    // replicas defaults to 1
		"Deployment shop/web 3/3 ready",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	web := list.Workloads[5]
	if web.RestartedAt != time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC).UnixMilli() ||
		web.Created != time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).UnixMilli() ||
		strings.Join(web.Images, ",") != "nginx:1.27,envoy:1" {
		t.Fatalf("unexpected web workload %+v", web)
	}
}

func TestListWorkloadsFailsOnAnyKind(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis/apps/v1/deployments": `{"items":[]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := listWorkloads(context.Background(), k); err == nil {
		t.Fatal("expected the missing kinds to fail the list")
	}
}

func TestRolloutRestart(t *testing.T) {
	const path = "/apis/apps/v1/namespaces/shop/deployments/web"

	f := newFakeKubeAPI(t, map[string]string{
		"GET " + path:   `{"metadata":{"name":"web"},"spec":{"paused":false}}`,
		"PATCH " + path: `{}`,
		"PATCH /apis/apps/v1/namespaces/shop/statefulsets/db": `{}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

	if err := rolloutRestart(context.Background(), k, workloadKinds[0], "shop", "web", now); err != nil {
		t.Fatal(err)
	}

	if err := rolloutRestart(context.Background(), k, workloadKinds[1], "shop", "db", now); err != nil {
		t.Fatal(err)
	}

	reqs := f.recorded()[1:] // after the /version probe
	if len(reqs) != 3 || reqs[0].method != "GET" || reqs[1].method != "PATCH" || reqs[2].path != "/apis/apps/v1/namespaces/shop/statefulsets/db" {
		t.Fatalf("unexpected requests %+v", reqs)
	}

	patch := reqs[1]
	if patch.contentType != "application/strategic-merge-patch+json" {
		t.Fatalf("content type %q", patch.contentType)
	}

	var body struct {
		Spec struct {
			Template struct {
				Metadata struct {
					Annotations map[string]string `json:"annotations"`
				} `json:"metadata"`
			} `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal([]byte(patch.body), &body); err != nil {
		t.Fatal(err)
	}

	if got := body.Spec.Template.Metadata.Annotations[restartedAtAnnotation]; got != "2026-10-03T12:00:00Z" {
		t.Fatalf("restartedAt %q", got)
	}
}

func TestRolloutRestartRefusesPausedDeployment(t *testing.T) {
	const path = "/apis/apps/v1/namespaces/shop/deployments/web"

	f := newFakeKubeAPI(t, map[string]string{"GET " + path: `{"spec":{"paused":true}}`, "PATCH " + path: `{}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := rolloutRestart(context.Background(), k, workloadKinds[0], "shop", "web", time.Now()); !errors.Is(err, errPausedDeployment) {
		t.Fatalf("got %v", err)
	}

	for _, r := range f.recorded() {
		if r.method == "PATCH" {
			t.Fatal("a paused deployment was patched")
		}
	}
}

func TestKubeRolloutRestartValidates(t *testing.T) {
	if err := KubeRolloutRestart("", "", "", "CronJob", "ns", "x"); err == nil || !strings.Contains(err.Error(), "unsupported workload kind") {
		t.Fatalf("got %v", err)
	}

	if err := KubeRolloutRestart("", "", "", "deployment", " ", "x"); err == nil || !strings.Contains(err.Error(), "no workload") {
		t.Fatalf("got %v", err)
	}
}

func TestKubeDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeWorkloads(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var list kubeWorkloadList
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list.Workloads) == 0 {
		t.Fatalf("demo workloads: %v %s", err, out)
	}

	if err := KubeRolloutRestart(cfg, "", "", "Deployment", "demo", "hello-ichor"); !errors.Is(err, demoUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestWithKubeForgetsUnreachableClient(t *testing.T) {
	f := newFakeKubeAPI(t, nil)

	opens := 0
	cache := newKubeClientCache(func(kubeTarget) (*kubeClient, error) {
		opens++

		return openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	})

	saved := kubeClients
	kubeClients = cache

	t.Cleanup(func() { kubeClients = saved })

	call := func(path string) error {
		_, err := withKube(kubeTarget{"cfg", "ctx", ""}, func(ctx context.Context, k *kubeClient) (struct{}, error) {
			return struct{}{}, k.get(ctx, path, &struct{}{})
		})

		return err
	}

	_ = call("/missing") // 404: the client stays cached
	_ = call("/version")

	if opens != 1 {
		t.Fatalf("opened %d times after an API error", opens)
	}

	f.Close() // now unreachable: the client is dropped

	if err := call("/version"); err == nil {
		t.Fatal("expected an error")
	}

	if _, _, err := cache.get(kubeTarget{"cfg", "ctx", ""}); err == nil {
		t.Fatal("expected the dropped client to be reopened (and fail)")
	}

	if opens != 2 {
		t.Fatalf("opened %d times", opens)
	}
}

func TestKubeClientCacheOpensOnceForConcurrentCalls(t *testing.T) {
	var (
		mu    sync.Mutex
		opens int
	)

	release := make(chan struct{})
	cache := newKubeClientCache(func(kubeTarget) (*kubeClient, error) {
		mu.Lock()
		opens++
		mu.Unlock()
		<-release

		return &kubeClient{http: &http.Client{}}, nil
	})

	var wg sync.WaitGroup

	clients := make([]*kubeClient, 5)
	for i := range clients {
		wg.Go(func() { clients[i], _, _ = cache.get(kubeTarget{"cfg", "ctx", ""}) })
	}

	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	if opens != 1 {
		t.Fatalf("opened %d times", opens)
	}

	for _, k := range clients {
		if k != clients[0] || k == nil {
			t.Fatal("callers got different clients")
		}
	}

	// A stale client being forgotten leaves the current one alone.
	cache.forget(kubeTarget{"cfg", "ctx", ""}, &kubeClient{http: &http.Client{}})

	if k, fresh, _ := cache.get(kubeTarget{"cfg", "ctx", ""}); k != clients[0] || fresh {
		t.Fatal("the cached client was dropped by a stale forget")
	}
}

func TestOpenKubeClientPrefersKubeconfigServer(t *testing.T) {
	f := newFakeKubeAPI(t, nil)

	// Both answer: the kubeconfig's own address wins over the Talos endpoint.
	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), []string{"localhost"}, "")
	if err != nil {
		t.Fatal(err)
	}

	if k.base.String() != f.URL {
		t.Fatalf("picked %s", k.base)
	}
}

func TestOpenKubeClientNoFallbackWithoutVerification(t *testing.T) {
	f := newFakeKubeAPI(t, nil)
	port := f.URL[strings.LastIndex(f.URL, ":")+1:]
	cfg := strings.Replace(f.kubeconfigFor("https://unreachable.invalid:"+port), "    server: https://unreachable.invalid", "    insecure-skip-tls-verify: true\n    server: https://unreachable.invalid", 1)

	if _, err := openKubeClient(context.Background(), cfg, []string{"127.0.0.1"}, ""); err == nil {
		t.Fatal("an insecure kubeconfig fell back to another address")
	}
}

func TestWorkloadStateLikeRolloutStatus(t *testing.T) {
	w := kubeWorkload{Desired: 3, Ready: 3, Updated: 3, Available: 3}

	cases := []struct {
		want string
		w    kubeWorkload
		f    rolloutFacts
	}{
		{workloadReady, w, rolloutFacts{}},
		{workloadProgressing, w, rolloutFacts{oldPods: true}},                                                     // old pods still being replaced
		{workloadReady, kubeWorkload{Desired: 3, Ready: 3, Updated: 1, Available: 1}, rolloutFacts{manual: true}}, // OnDelete
		{workloadProgressing, w, rolloutFacts{manual: true, stale: true}},
		{workloadDegraded, kubeWorkload{Desired: 3, Ready: 2, Updated: 3, Available: 3}, rolloutFacts{}},
	}

	for _, c := range cases {
		if got := workloadState(c.w, c.f); got != c.want {
			t.Errorf("%+v %+v: got %s, want %s", c.w, c.f, got, c.want)
		}
	}

	var sts appsObject
	if err := json.Unmarshal([]byte(`{"spec":{"updateStrategy":{"type":"RollingUpdate","rollingUpdate":{"partition":2}}}}`), &sts); err != nil {
		t.Fatal(err)
	}

	if !manualUpdates("StatefulSet", sts) || manualUpdates("Deployment", sts) {
		t.Fatal("partition not recognized")
	}
}

func TestValidateKubeName(t *testing.T) {
	for _, bad := range []string{"..", "a/b", "A", "-a", "a..b", strings.Repeat("a", 254)} {
		if validateKubeName("pod", "ns", bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}

	if err := validateKubeName("pod", "kube-system", "coredns-5c6b7.abc"); err != nil {
		t.Fatal(err)
	}
}
