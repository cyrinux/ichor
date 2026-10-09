package ichorgo

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
)

const (
	argoDiffApp = "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/web"
	argoDiffSHA = "4be1d0c9f2a7e3b18c6d5a0f9e8b7c6d5a4f3e2d"
)

// argoDiffAPI is a cluster with an Application "web" in argocd, a Redis pod and its password.
func argoDiffAPI(t *testing.T) *fakeKubeAPI {
	t.Helper()

	return newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET " + argoDiffApp: `{"metadata":{"name":"web","namespace":"argocd"},
			"spec":{"source":{"repoURL":"https://github.com/org/gitops.git","path":"apps/web","targetRevision":"main"},
				"destination":{"namespace":"web"},"syncPolicy":{"automated":{"prune":false}}},
			"status":{"sync":{"status":"OutOfSync","revision":"` + argoDiffSHA + `"},
				"history":[{"id":1,"revision":"91c0a7e6d5b4c3a29180f7e6d5c4b3a291807f6e"}]}}`,
		"GET /api/v1/pods": `{"items":[{"metadata":{"name":"argocd-redis-5c7f","namespace":"argocd"},"spec":{"containers":[{"name":"redis"}]},"status":{"phase":"Running"}}]}`,
		"GET /api/v1/namespaces/argocd/secrets/argocd-redis": `{"data":{"auth":"` + base64.StdEncoding.EncodeToString([]byte("s3cret")) + `"}}`,
	})
}

// argoCachedState is what the controller cached for "web": a drifted Deployment (with a
// field Argo CD ignores, so the predicted state keeps the live replicas), a new ConfigMap, a
// Secret whose values Argo CD hid, a Service dropped from Git, an unchanged Service, a hook.
func argoCachedState() []argoManagedResource {
	deployLive := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"web","labels":{"app.kubernetes.io/instance":"web"}},"spec":{"replicas":3,"template":{"spec":{"containers":[{"name":"web","image":"web:1"}]}}}}`
	deployWant := `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"web","labels":{"app.kubernetes.io/instance":"web"}},"spec":{"replicas":3,"template":{"spec":{"containers":[{"name":"web","image":"web:2"}]}}}}`
	cm := `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"web-settings","namespace":"web"},"data":{"level":"info"}}`
	secretLive := `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"web-credentials","namespace":"web"},"type":"Opaque","data":{"password":"++++++++"}}`
	secretWant := `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"web-credentials","namespace":"web"},"type":"Opaque","data":{"password":"++++++++","token":"+++++++++"}}`
	svc := `{"apiVersion":"v1","kind":"Service","metadata":{"name":"web-legacy","namespace":"web"},"spec":{"ports":[{"port":80}]}}`
	same := `{"apiVersion":"v1","kind":"Service","metadata":{"name":"web","namespace":"web"},"spec":{"ports":[{"port":80}]},"status":{"loadBalancer":{}}}`

	return []argoManagedResource{
		{Group: "apps", Kind: "Deployment", Namespace: "web", Name: "web", LiveState: deployLive, NormalizedLiveState: deployLive, TargetState: deployWant, PredictedLiveState: deployWant, Modified: true},
		{Kind: "ConfigMap", Namespace: "web", Name: "web-settings", TargetState: cm, PredictedLiveState: cm, Modified: true},
		{Kind: "Secret", Namespace: "web", Name: "web-credentials", LiveState: secretLive, NormalizedLiveState: secretLive, TargetState: secretWant, PredictedLiveState: secretWant, Modified: true},
		{Kind: "Service", Namespace: "web", Name: "web-legacy", LiveState: svc, NormalizedLiveState: svc, Modified: true},
		{Kind: "Service", Namespace: "web", Name: "web", LiveState: same, NormalizedLiveState: same, TargetState: same, PredictedLiveState: same},
		{Group: "batch", Kind: "Job", Namespace: "web", Name: "migrate", Hook: true, TargetState: `{"apiVersion":"batch/v1","kind":"Job"}`},
	}
}

// fakeRedis answers AUTH, SCAN and GET on one stream with the keys given, recording the
// commands. It is what the port-forward would reach.
type fakeRedis struct {
	password string
	keys     map[string][]byte
	commands []string
}

func (f *fakeRedis) serve(conn net.Conn) {
	defer conn.Close() //nolint:errcheck

	parser := &respClient{r: bufio.NewReader(conn), maxValue: 1 << 20}
	authed := f.password == ""

	for {
		reply, err := parser.reply()
		if err != nil {
			return
		}

		parts, _ := reply.([]any)
		args := make([]string, len(parts))

		for i, p := range parts {
			args[i], _ = p.(string)
		}

		f.commands = append(f.commands, strings.Join(args, " "))

		var out string

		switch {
		case len(args) == 0:
			out = "-ERR empty\r\n"
		case args[0] == "AUTH":
			switch {
			case f.password == "":
				out = "-ERR AUTH <password> called without any password configured for the default user\r\n"
			case len(args) == 2 && args[1] == f.password:
				authed = true
				out = "+OK\r\n"
			default:
				out = "-WRONGPASS invalid username-password pair\r\n"
			}
		case !authed:
			out = "-NOAUTH Authentication required.\r\n"
		case args[0] == "SCAN":
			prefix := strings.TrimSuffix(strings.ReplaceAll(args[3], "\\", ""), "*")

			var keys []string
			for k := range f.keys {
				if strings.HasPrefix(k, prefix) {
					keys = append(keys, k)
				}
			}

			out = "*2\r\n$1\r\n0\r\n*" + strconv.Itoa(len(keys)) + "\r\n"
			for _, k := range keys {
				out += "$" + strconv.Itoa(len(k)) + "\r\n" + k + "\r\n"
			}
		case args[0] == "GET":
			v, ok := f.keys[args[1]]
			if !ok {
				out = "$-1\r\n"
			} else {
				out = "$" + strconv.Itoa(len(v)) + "\r\n" + string(v) + "\r\n"
			}
		default:
			out = "-ERR unknown command\r\n"
		}

		if _, err := io.WriteString(conn, out); err != nil {
			return
		}
	}
}

// dialer gives diffArgoApp a stream to the fake, checking the pod it asked for.
func (f *fakeRedis) dialer(t *testing.T, wantNamespace, wantPod string) podDialer {
	return func(_ context.Context, namespace, pod string, port int) (io.ReadWriteCloser, error) {
		if namespace != wantNamespace || pod != wantPod || port != argoRedisPort {
			t.Errorf("dialed %s/%s:%d", namespace, pod, port)
		}

		client, server := net.Pipe()
		go f.serve(server)

		return client, nil
	}
}

func gzipJSON(t *testing.T, v any) []byte {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()

	return buf.Bytes()
}

func TestDiffArgoApp(t *testing.T) {
	f := argoDiffAPI(t)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	redis := &fakeRedis{password: "s3cret", keys: map[string][]byte{
		"app|managed-resources|web|1.8.3":       gzipJSON(t, argoCachedState()),
		"app|managed-resources|web-other|1.8.3": []byte("[]"),
		"app|resources-tree|web|1.8.3":          []byte("{}"),
	}}

	d, err := diffArgoApp(context.Background(), k, redis.dialer(t, "argocd", "argocd-redis-5c7f"), "argocd", "web")
	if err != nil {
		t.Fatal(err)
	}

	if d.Kind != "Application" || d.Revision != argoDiffSHA || d.Applied != "91c0a7e6d5b4c3a29180f7e6d5c4b3a291807f6e" {
		t.Fatalf("header %+v", d)
	}

	var got []string
	for _, r := range d.Resources {
		got = append(got, r.Change+" "+r.Kind+"/"+r.Namespace+"/"+r.Name)
	}

	want := []string{
		"created ConfigMap/web/web-settings",
		"changed Deployment/web/web",
		"changed Secret/web/web-credentials",
		"deleted Service/web/web-legacy",
		"ignored Job/web/migrate",
		"unchanged Service/web/web",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("resources:\n%s", strings.Join(got, "\n"))
	}

	for _, r := range d.Resources {
		switch r.Kind {
		case "Deployment":
			if r.Version != "v1" || !strings.Contains(r.Diff, "-        - image: web:1") || !strings.Contains(r.Diff, "+        - image: web:2") || strings.Contains(r.Diff, "replicas") {
				t.Fatalf("deployment diff (version %q):\n%s", r.Version, r.Diff)
			}
		case "Secret":
			// Argo CD hid the values; the kept key is context, the new key shows redacted.
			if strings.Contains(r.Diff, "++++") || !strings.Contains(r.Diff, "+  token: redacted hmac:") || strings.Count(r.Diff, "password") != 1 {
				t.Fatalf("secret diff:\n%s", r.Diff)
			}
		case "ConfigMap":
			if !strings.Contains(r.Diff, "+  level: info") {
				t.Fatalf("configmap diff:\n%s", r.Diff)
			}
		}
	}

	if len(d.Warnings) != 1 || !strings.Contains(d.Warnings[0], "prunes") {
		t.Fatalf("warnings %v", d.Warnings)
	}

	// Authenticated, then one SCAN for the app's key and one GET: nothing written.
	if len(redis.commands) != 3 || redis.commands[0] != "AUTH s3cret" || !strings.HasPrefix(redis.commands[1], "SCAN 0 MATCH app|managed-resources|web|*") || redis.commands[2] != "GET app|managed-resources|web|1.8.3" {
		t.Fatalf("commands %q", redis.commands)
	}
}

func TestDiffArgoAppErrors(t *testing.T) {
	f := argoDiffAPI(t)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// Not compared yet: no key.
	empty := &fakeRedis{password: "s3cret", keys: map[string][]byte{}}
	if _, err := diffArgoApp(context.Background(), k, empty.dialer(t, "argocd", "argocd-redis-5c7f"), "argocd", "web"); !errors.Is(err, errArgoNotCached) {
		t.Fatalf("not cached: %v", err)
	}

	// A wrong password is Redis' error.
	wrong := &fakeRedis{password: "other"}
	if _, err := diffArgoApp(context.Background(), k, wrong.dialer(t, "argocd", "argocd-redis-5c7f"), "argocd", "web"); err == nil || !strings.Contains(err.Error(), "WRONGPASS") {
		t.Fatalf("wrong password: %v", err)
	}

	// A Redis without a password: the Secret's AUTH is refused as pointless and ignored.
	open := &fakeRedis{keys: map[string][]byte{"app|managed-resources|web|1.8.3": []byte("[]")}}
	if _, err := diffArgoApp(context.Background(), k, open.dialer(t, "argocd", "argocd-redis-5c7f"), "argocd", "web"); err != nil {
		t.Fatalf("no password: %v", err)
	}

	// Garbage in the cache.
	bad := &fakeRedis{password: "s3cret", keys: map[string][]byte{"app|managed-resources|web|1.8.3": []byte("nope")}}
	if _, err := diffArgoApp(context.Background(), k, bad.dialer(t, "argocd", "argocd-redis-5c7f"), "argocd", "web"); err == nil || !strings.Contains(err.Error(), "unexpected cached state") {
		t.Fatalf("garbage: %v", err)
	}

	// No Redis pod at all.
	f.answerWith("GET /api/v1/pods", 200, `{"items":[]}`)

	if _, err := diffArgoApp(context.Background(), k, empty.dialer(t, "", ""), "argocd", "web"); !errors.Is(err, errArgoNoRedis) {
		t.Fatalf("no pod: %v", err)
	}
}

func TestArgoInstanceName(t *testing.T) {
	var app argoObject
	app.Metadata.Name, app.Metadata.Namespace = "shop", "team-a"

	if got := argoInstanceName(app, "argocd"); got != "team-a_shop" {
		t.Fatalf("apps in any namespace: %q", got)
	}

	if got := argoInstanceName(app, "team-a"); got != "shop" {
		t.Fatalf("control plane: %q", got)
	}
}

func TestDecodeArgoManagedResourcesPlainAndGzip(t *testing.T) {
	state := argoCachedState()

	plain, _ := json.Marshal(state)
	for _, raw := range [][]byte{plain, gzipJSON(t, state)} {
		got, err := decodeArgoManagedResources(raw)
		if err != nil || len(got) != len(state) || got[0].Kind != "Deployment" {
			t.Fatalf("decode: %v %d", err, len(got))
		}
	}
}

func TestRespClient(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close() //nolint:errcheck

	go func() {
		defer server.Close() //nolint:errcheck

		parser := &respClient{r: bufio.NewReader(server), maxValue: 1 << 20}
		// One command in, scripted replies out.
		for _, reply := range []string{"+PONG\r\n", ":42\r\n", "$5\r\nhello\r\n", "$-1\r\n", "*2\r\n$1\r\na\r\n$-1\r\n", "-ERR boom\r\n", "HTTP/1.1 400\r\n"} {
			if _, err := parser.reply(); err != nil {
				return
			}

			_, _ = io.WriteString(server, reply)
		}
	}()

	c := newRespClient(client, 1<<10)

	if v, err := c.call("PING"); err != nil || v != "PONG" {
		t.Fatalf("PING %v %v", v, err)
	}

	if v, err := c.call("DBSIZE"); err != nil || v != int64(42) {
		t.Fatalf("int %v %v", v, err)
	}

	if v, err := c.call("GET", "k"); err != nil || v != "hello" {
		t.Fatalf("bulk %v %v", v, err)
	}

	if _, err := c.call("GET", "missing"); !errors.Is(err, errRespNil) {
		t.Fatalf("nil bulk %v", err)
	}

	if v, err := c.call("MGET", "a", "b"); err != nil || len(v.([]any)) != 2 || v.([]any)[1] != nil {
		t.Fatalf("array %v %v", v, err)
	}

	if _, err := c.call("BAD"); err == nil || err.Error() != "ERR boom" {
		t.Fatalf("error reply %v", err)
	}

	if _, err := c.call("PING"); err == nil || !strings.Contains(err.Error(), "not a Redis server") {
		t.Fatalf("non-RESP %v", err)
	}

	if got := respGlobEscape("app|m[1]*?"); got != `app|m\[1\]\*\?` {
		t.Fatalf("escape %q", got)
	}
}

func TestDemoArgoDiff(t *testing.T) {
	d := demoArgoDiff("argocd", "web")

	if d.Kind != "Application" || len(d.Resources) == 0 || d.Revision == d.Applied {
		t.Fatalf("demo %+v", d)
	}

	for _, r := range d.Resources {
		if r.Change == diffChangeChanged && r.Diff == "" {
			t.Fatalf("%s/%s changed without a diff", r.Kind, r.Name)
		}
	}
}
