package ichorgo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// trivyFixture is `trivy image --format json` output, trimmed to what the app reads.
const trivyFixture = `{
  "SchemaVersion": 2,
  "ArtifactName": "docker.io/library/nginx@sha256:abc",
  "Metadata": {"OS": {"Family": "debian", "Name": "12.4"}, "RepoDigests": ["nginx@sha256:abc"]},
  "Results": [
    {"Target": "nginx (debian 12.4)", "Class": "os-pkgs", "Type": "debian", "Vulnerabilities": [
      {"VulnerabilityID": "CVE-2024-1", "PkgName": "libssl3", "PkgIdentifier": {"PURL": "pkg:deb/debian/libssl3@3.0.11"},
       "InstalledVersion": "3.0.11", "FixedVersion": "3.0.13", "Severity": "HIGH", "SeveritySource": "debian",
       "Title": "openssl: slow DH", "PrimaryURL": "https://avd.aquasec.com/nvd/cve-2024-1",
       "CVSS": {"nvd": {"V3Vector": "CVSS:3.1/AV:N", "V3Score": 7.5}, "redhat": {"V3Score": 5.9}}},
      {"VulnerabilityID": "CVE-2024-2", "PkgName": "zlib1g", "InstalledVersion": "1.2.13", "Severity": "CRITICAL",
       "CVSS": {"ghsa": {"V40Vector": "CVSS:4.0/AV:N", "V40Score": 9.3}}},
      {"VulnerabilityID": "CVE-2024-3", "PkgName": "libexpat1", "InstalledVersion": "2.5.0", "FixedVersion": "2.5.1", "Severity": "CRITICAL"},
      {"VulnerabilityID": "CVE-2024-3", "PkgName": "libexpat1", "InstalledVersion": "2.5.0", "FixedVersion": "2.5.1", "Severity": "CRITICAL"}
    ]},
    {"Target": "usr/local/bin/app", "Class": "lang-pkgs", "Type": "gobinary", "Vulnerabilities": [
      {"VulnerabilityID": "GHSA-xxxx", "PkgName": "golang.org/x/net", "InstalledVersion": "v0.17.0", "FixedVersion": "0.23.0", "Severity": "medium"}
    ]},
    {"Target": "Python", "Class": "lang-pkgs", "Type": "python-pkg"}
  ]
}`

func TestParseTrivyReport(t *testing.T) {
	img, err := parseTrivyReport([]byte(trivyFixture), scannedImage{Image: "nginx:1.25", Pods: []string{"web/a"}})
	if err != nil {
		t.Fatal(err)
	}

	if img.OS != "debian 12.4" || img.Digest != "sha256:abc" || img.Image != "nginx:1.25" || len(img.Pods) != 1 {
		t.Fatalf("image %+v", img)
	}

	var ids []string
	for _, v := range img.Vulnerabilities {
		ids = append(ids, v.ID)
	}

	// Critical fixable first, then critical without a fix, high, medium; the duplicate dropped.
	if want := []string{"CVE-2024-3", "CVE-2024-2", "CVE-2024-1", "GHSA-xxxx"}; !slices.Equal(ids, want) {
		t.Fatalf("order %v", ids)
	}

	want := vulnSummary{Critical: 2, High: 1, Medium: 1, Fixable: 3, OS: 3}
	if img.Summary != want {
		t.Fatalf("summary %+v", img.Summary)
	}

	ssl := img.Vulnerabilities[2]
	if ssl.Score != 7.5 || ssl.Vector != "CVSS:3.1/AV:N" || ssl.PURL == "" || ssl.Target != "nginx (debian 12.4)" || ssl.Class != vulnClassOS {
		t.Fatalf("ssl %+v", ssl)
	}

	if z := img.Vulnerabilities[1]; z.Score != 9.3 || !strings.HasPrefix(z.Vector, "CVSS:4.0") {
		t.Fatalf("zlib %+v", z)
	}

	if img.Vulnerabilities[3].Severity != "MEDIUM" {
		t.Fatalf("severity %q", img.Vulnerabilities[3].Severity)
	}

	if _, err := parseTrivyReport([]byte(`{"SchemaVersion":1}`), scannedImage{}); err == nil {
		t.Fatal("schema 1 accepted")
	}

	if _, err := parseTrivyReport([]byte(`{`), scannedImage{}); err == nil {
		t.Fatal("broken JSON accepted")
	}
}

func TestPodScanTargets(t *testing.T) {
	var pod scanPod

	err := json.Unmarshal([]byte(`{
		"spec": {
			"initContainers": [{"name": "init", "image": "busybox:1.36"}],
			"containers": [{"name": "web", "image": "nginx:1.25"}, {"name": "app", "image": "ghcr.io/x/app:2"},
				{"name": "dev", "image": "local/built:dev"}, {"name": "web2", "image": "nginx:1.25"}, {"name": "new", "image": "redis"}]
		},
		"status": {
			"initContainerStatuses": [{"name": "init", "image": "docker.io/library/busybox:1.36", "imageID": "docker.io/library/busybox@sha256:111"}],
			"containerStatuses": [
				{"name": "web", "image": "docker.io/library/nginx:1.25", "imageID": "docker-pullable://nginx@sha256:222"},
				{"name": "web2", "image": "docker.io/library/nginx:1.25", "imageID": "docker-pullable://nginx@sha256:222"},
				{"name": "app", "image": "ghcr.io/x/app:2", "imageID": "ghcr.io/x/app@sha256:333"},
				{"name": "dev", "image": "local/built:dev", "imageID": "sha256:444"}
			]
		}}`), &pod)
	if err != nil {
		t.Fatal(err)
	}

	got := podScanTargets(pod)

	var refs []string
	for _, tg := range got {
		refs = append(refs, tg.image+" -> "+tg.ref+" "+tg.digest)
	}

	want := []string{
		"busybox:1.36 -> docker.io/library/busybox@sha256:111 sha256:111",
		"nginx:1.25 -> nginx@sha256:222 sha256:222",
		"ghcr.io/x/app:2 -> ghcr.io/x/app@sha256:333 sha256:333",
		"local/built:dev -> local/built:dev ",
		"redis -> redis ", // not started yet: as the spec names it
	}
	if !slices.Equal(refs, want) {
		t.Fatalf("targets\n%s", strings.Join(refs, "\n"))
	}
}

func dockerConfigJSON(t *testing.T, registry, user string) string {
	t.Helper()

	cfg := `{"auths":{"` + registry + `":{"auth":"` + base64.StdEncoding.EncodeToString([]byte(user+":pw")) + `"}}}`

	return base64.StdEncoding.EncodeToString([]byte(cfg))
}

func TestSecretAuths(t *testing.T) {
	auths, err := secretAuths(scanSecret{Type: "kubernetes.io/dockerconfigjson", Data: map[string]string{".dockerconfigjson": dockerConfigJSON(t, "ghcr.io", "a")}})
	if err != nil || len(auths) != 1 || auths["ghcr.io"] == nil {
		t.Fatalf("%v %v", auths, err)
	}

	legacy := base64.StdEncoding.EncodeToString([]byte(`{"quay.io":{"auth":"eDp5"}}`))

	auths, err = secretAuths(scanSecret{Type: "kubernetes.io/dockercfg", Data: map[string]string{".dockercfg": legacy}})
	if err != nil || auths["quay.io"] == nil {
		t.Fatalf("%v %v", auths, err)
	}

	if auths, err := secretAuths(scanSecret{Type: "Opaque"}); auths != nil || err != nil {
		t.Fatalf("opaque %v %v", auths, err)
	}

	if _, err := secretAuths(scanSecret{Type: "kubernetes.io/dockercfg", Data: map[string]string{".dockercfg": "!!"}}); err == nil {
		t.Fatal("bad base64 accepted")
	}
}

func TestTrivyError(t *testing.T) {
	cases := map[string]string{
		"2026-10-07T17:20:10Z\tFATAL\tFatal error\trun error: GET https://index.docker.io/v2/x: UNAUTHORIZED: authentication required": "the registry refused the pull",
		"FATAL\tunable to find the specified image: MANIFEST_UNKNOWN":                                                                  "the image is no longer in its registry",
		"error: TOOMANYREQUESTS: You have reached your pull rate limit":                                                                "the registry rate-limited",
		"dial tcp: lookup ghcr.io: no such host":                                                                                       "the cluster cannot reach",
		"scan error: context deadline exceeded":                                                                                        "the scan timed out",
		"something odd":                                                                                                                "something odd",
		"":                                                                                                                             "Trivy failed without a message",
	}

	for in, want := range cases {
		if got := trivyError(in); !strings.HasPrefix(got, want) {
			t.Errorf("trivyError(%q) = %q, want prefix %q", in, got, want)
		}
	}

	if got := trivyError("a\tb\tthe last field\n\n"); got != "the last field" {
		t.Fatalf("got %q", got)
	}
}

func testTargets() []scanTarget {
	return []scanTarget{
		{image: "nginx:1.25", ref: "nginx@sha256:abc", digest: "sha256:abc", pods: []string{"web/a"}},
		{image: "ghcr.io/x/app:2", ref: "ghcr.io/x/app@sha256:def", digest: "sha256:def", pods: []string{"web/a"}},
	}
}

// scanLog is what the Trivy pod prints for nonce: a report for the first image, an error for
// the second, with stray lines between.
func scanLog(nonce string) string {
	m := "@@" + nonce + " "

	return strings.Join([]string{
		m + "db", "stray stderr line",
		m + "scan 0", m + "result 0", trivyFixture, m + "end 0",
		m + "scan 1", m + "fail 1", "2026\tFATAL\tUNAUTHORIZED: authentication required", m + "end 1",
		"@@forged result 1", "",
	}, "\n")
}

func TestScanLogParser(t *testing.T) {
	targets := testTargets()
	report := imageScanReport{Images: pendingImages(targets)}

	var phases []string

	p := newScanLogParser("n0nce", targets, &report, func(pr imageScanProgress) {
		phases = append(phases, pr.Phase+":"+pr.Image)
	})

	if err := p.read(strings.NewReader(scanLog("n0nce"))); err != nil {
		t.Fatal(err)
	}

	if want := []string{"database:", "scanning:nginx:1.25", "scanning:ghcr.io/x/app:2"}; !slices.Equal(phases, want) {
		t.Fatalf("phases %v", phases)
	}

	first, second := report.Images[0], report.Images[1]
	if first.Error != "" || len(first.Vulnerabilities) != 4 || first.OS != "debian 12.4" || first.ScannedAt == 0 {
		t.Fatalf("first %+v", first)
	}

	if !strings.HasPrefix(second.Error, "the registry refused the pull") || len(second.Vulnerabilities) != 0 {
		t.Fatalf("second %+v", second)
	}

	if err := p.outcome(netPerfPod{}); err != nil {
		t.Fatalf("outcome %v", err)
	}
}

func TestScanLogParserFailures(t *testing.T) {
	targets := testTargets()
	m := "@@n "

	// The database could not be downloaded.
	report := imageScanReport{Images: pendingImages(targets)}
	p := newScanLogParser("n", targets, &report, func(imageScanProgress) {})
	_ = p.read(strings.NewReader(m + "db\n" + m + "dbfail\nFATAL\tcontext deadline exceeded\n" + m + "end db\n"))

	if err := p.outcome(netPerfPod{}); err == nil || !strings.Contains(err.Error(), "vulnerability database") {
		t.Fatalf("db %v", err)
	}

	// The pod was killed during the second image.
	report = imageScanReport{Images: pendingImages(targets)}
	p = newScanLogParser("n", targets, &report, func(imageScanProgress) {})
	_ = p.read(strings.NewReader(m + "scan 0\n" + m + "result 0\n" + trivyFixture + "\n" + m + "end 0\n" + m + "scan 1\n"))

	var pod netPerfPod
	_ = json.Unmarshal([]byte(`{"status":{"phase":"Failed","containerStatuses":[{"state":{"terminated":{"exitCode":137,"reason":"OOMKilled"}}}]}}`), &pod)

	err := p.outcome(pod)
	if err == nil || !strings.Contains(err.Error(), "after 1 of 2 images: OOMKilled (exit code 137)") {
		t.Fatalf("killed %v", err)
	}

	if report.Images[1].Error != "not scanned" {
		t.Fatalf("second %+v", report.Images[1])
	}

	// A report that is not JSON is that image's error.
	report = imageScanReport{Images: pendingImages(targets)}
	p = newScanLogParser("n", targets, &report, func(imageScanProgress) {})
	_ = p.read(strings.NewReader(m + "result 0\nnot json\n" + m + "end 0\n" + m + "end 7\n"))

	if !strings.HasPrefix(report.Images[0].Error, "unreadable Trivy report") {
		t.Fatalf("broken %+v", report.Images[0])
	}
}

func TestScanLogParserLongLine(t *testing.T) {
	targets := testTargets()[:1]
	report := imageScanReport{Images: pendingImages(targets)}
	p := newScanLogParser("n", targets, &report, func(imageScanProgress) {})

	long := strings.Repeat("x", imageScanMaxLine+10)
	if err := p.read(strings.NewReader("@@n result 0\n" + long + "\n@@n end 0\n")); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(report.Images[0].Error, "unreadable Trivy report") || p.ended != 1 {
		t.Fatalf("long line %+v", report.Images[0])
	}
}

func TestImageScanPodSpec(t *testing.T) {
	spec := imageScanPodSpec("n0nce", testTargets(), imageScanOptions{DBRepository: "registry.lan/trivy-db:2"}, "scan-x-auth")

	js, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}

	s := string(js)
	for _, want := range []string{
		`"image":"` + trivyImage + `"`, `"automountServiceAccountToken":false`, `"readOnlyRootFilesystem":true`,
		`"runAsNonRoot":true`, `"drop":["ALL"]`, `"activeDeadlineSeconds":2400`,
		`"name":"TRIVY_DB_REPOSITORY","value":"registry.lan/trivy-db:2"`, `"name":"DOCKER_CONFIG","value":"/docker"`,
		`"secretName":"scan-x-auth"`, `"mountPath":"/work"`, `"limits":{"memory":"4Gi"}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("spec lacks %s", want)
		}
	}

	command := spec["spec"].(map[string]any)["containers"].([]map[string]any)[0]["command"].([]string)
	if want := []string{"sh", "n0nce", "nginx@sha256:abc", "ghcr.io/x/app@sha256:def"}; !slices.Equal(command[3:], want) {
		t.Fatalf("command args %v", command[3:])
	}

	plain, _ := json.Marshal(imageScanPodSpec("n", testTargets(), imageScanOptions{}, ""))
	if strings.Contains(string(plain), "DOCKER_CONFIG") || strings.Contains(string(plain), "TRIVY_DB_REPOSITORY") {
		t.Fatalf("plain spec %s", plain)
	}

	if !strings.Contains(imageScanScript, "--image-src remote") || !strings.Contains(imageScanScript, "--skip-db-update") {
		t.Fatal("script flags")
	}
}

func TestDecodeImageScanOptions(t *testing.T) {
	if o, err := decodeImageScanOptions(""); err != nil || o != (imageScanOptions{}) {
		t.Fatalf("%+v %v", o, err)
	}

	if o, err := decodeImageScanOptions(`{"dbRepository":"r.lan/db:2","javaDBRepository":"r.lan/java:1"}`); err != nil || o.JavaDBRepository != "r.lan/java:1" {
		t.Fatalf("%+v %v", o, err)
	}

	for _, bad := range []string{`{`, `{"dbRepository":"x; rm -rf /"}`, `{"javaDBRepository":"$(id)"}`} {
		if _, err := decodeImageScanOptions(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

// fakeImageScanAPI serves the pods, a pull secret, and a Trivy pod whose log is scanLog.
type fakeImageScanAPI struct {
	*fakeKubeAPI

	mu        sync.Mutex
	nonce     string
	posted    []string // kinds or paths posted
	deleted   []string
	cutFollow bool   // a followed log stops halfway, the plain read has it all
	secret    string // the merged pull credentials posted
	job       string // the Job posted
	nsLabels  string // the scan namespace's labels when it exists, "" when it does not
}

func newFakeImageScanAPI(t *testing.T) *fakeImageScanAPI {
	t.Helper()

	f := &fakeImageScanAPI{fakeKubeAPI: &fakeKubeAPI{}}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)

	old := netPerfPoll
	netPerfPoll = time.Millisecond

	t.Cleanup(func() { netPerfPoll = old })

	return f
}

func (f *fakeImageScanAPI) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path := r.URL.Path

	f.mu.Lock()
	defer f.mu.Unlock()

	write := func(code int, s string) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, s)
	}

	switch {
	case path == "/version":
		write(200, `{"gitVersion":"v1.34.0"}`)
	case path == "/api/v1/namespaces/web/pods/a":
		write(200, `{"spec":{"containers":[{"name":"web","image":"nginx:1.25"},{"name":"app","image":"ghcr.io/x/app:2"}],"imagePullSecrets":[{"name":"other"},{"name":"ghcr"},{"name":"gone"}]},
			"status":{"containerStatuses":[{"name":"web","image":"docker.io/library/nginx:1.25","imageID":"docker.io/library/nginx@sha256:abc"},
			{"name":"app","image":"ghcr.io/x/app:2","imageID":"ghcr.io/x/app@sha256:def"}]}}`)
	case path == "/api/v1/namespaces/web/secrets/ghcr":
		write(200, `{"type":"kubernetes.io/dockerconfigjson","data":{".dockerconfigjson":"`+
			base64.StdEncoding.EncodeToString([]byte(`{"auths":{"ghcr.io":{"auth":"eDp5"}}}`))+`"}}`)
	case path == "/api/v1/namespaces/web/secrets/other":
		write(200, `{"type":"kubernetes.io/dockerconfigjson","data":{".dockerconfigjson":"`+
			base64.StdEncoding.EncodeToString([]byte(`{"auths":{"quay.io":{"auth":"cTp5"}}}`))+`"}}`)
	case path == "/api/v1/namespaces/"+imageScanNamespace && r.Method == http.MethodGet:
		if f.nsLabels == "" {
			write(404, `{"kind":"Status","reason":"NotFound","message":"not found"}`)

			return
		}

		write(200, `{"metadata":{"labels":`+f.nsLabels+`}}`)
	case r.Method == http.MethodPost:
		f.posted = append(f.posted, path)

		if strings.HasSuffix(path, "/secrets") {
			f.secret = string(body)
		}

		if path == "/api/v1/namespaces" {
			f.nsLabels = `{"app.kubernetes.io/managed-by":"ichor"}`
		}

		if strings.HasSuffix(path, "/jobs") {
			var job struct {
				Metadata struct{ Name string } `json:"metadata"`
				Spec     struct {
					Template struct {
						Spec struct {
							Containers []struct{ Command []string } `json:"containers"`
						} `json:"spec"`
					} `json:"template"`
				} `json:"spec"`
			}

			_ = json.Unmarshal(body, &job)
			f.job = job.Metadata.Name
			f.nonce = job.Spec.Template.Spec.Containers[0].Command[4]
			write(201, `{"metadata":{"name":"`+f.job+`","uid":"0f0e-uid"}}`)

			return
		}

		write(201, string(body))
	case r.Method == http.MethodDelete:
		f.deleted = append(f.deleted, path+"?"+r.URL.RawQuery)
		write(200, `{}`)
	case path == "/api/v1/namespaces/"+imageScanNamespace+"/pods":
		if r.URL.Query().Get("labelSelector") != imageScanRunLabel+"="+f.job {
			write(200, `{"items":[]}`)

			return
		}

		write(200, `{"items":[{"metadata":{"name":"scan"}}]}`)
	case strings.HasSuffix(path, "/pods/scan/log") && f.cutFollow && r.URL.Query().Get("follow") == "true":
		log := scanLog(f.nonce)
		w.Header().Set("Content-Length", strconv.Itoa(len(log)))
		w.WriteHeader(200)
		_, _ = io.WriteString(w, log[:len(log)/2]) // the connection drops halfway
	case strings.HasSuffix(path, "/pods/scan/log"):
		write(200, scanLog(f.nonce))
	case strings.HasSuffix(path, "/pods/scan"):
		write(200, `{"status":{"phase":"Succeeded"}}`)
	default:
		write(404, `{"kind":"Status","reason":"NotFound","message":"`+path+` not found"}`)
	}
}

func TestRunImageScan(t *testing.T) {
	f := newFakeImageScanAPI(t)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	var phases []string

	report, err := runImageScan(context.Background(), k, []routePod{{Namespace: "web", Pod: "a"}, {Namespace: "web", Pod: "gone"}},
		imageScanOptions{}, func(p imageScanProgress) { phases = append(phases, p.Phase) })
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Images) != 2 || report.Images[0].Ref != "docker.io/library/nginx@sha256:abc" || report.Images[0].Error != "" ||
		len(report.Images[0].Vulnerabilities) != 4 || report.Images[1].Error == "" || report.Finished == 0 {
		t.Fatalf("report %+v", report)
	}

	if want := []string{"preparing", "starting", "database", "scanning", "scanning", "cleaning"}; !slices.Equal(phases, want) {
		t.Fatalf("phases %v", phases)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// The namespace is created once, then the Job, then its credentials, owned by it.
	jobs := "/apis/batch/v1/namespaces/" + imageScanNamespace + "/jobs"
	if want := []string{"/api/v1/namespaces", jobs, "/api/v1/namespaces/" + imageScanNamespace + "/secrets"}; !slices.Equal(f.posted, want) {
		t.Fatalf("posted %v", f.posted)
	}

	if !strings.Contains(f.secret, `"ownerReferences":[{"apiVersion":"batch/v1","blockOwnerDeletion":false,"kind":"Job","name":"`+f.job+`","uid":"0f0e-uid"}]`) {
		t.Fatalf("secret %s", f.secret)
	}

	// The Job is deleted with its pod and credentials; the namespace stays.
	if want := []string{jobs + "/" + f.job + "?propagationPolicy=Background"}; !slices.Equal(f.deleted, want) {
		t.Fatalf("deleted %v", f.deleted)
	}

	// Only the scanned registries' credentials are copied: ghcr.io, not quay.io.
	var secret struct {
		Data map[string]string `json:"data"`
	}

	_ = json.Unmarshal([]byte(f.secret), &secret)
	cfg, _ := base64.StdEncoding.DecodeString(secret.Data["config.json"])

	if !strings.Contains(string(cfg), `"ghcr.io"`) || strings.Contains(string(cfg), "quay.io") {
		t.Fatalf("credentials %s", cfg)
	}
}

func TestRunImageScanCutFollow(t *testing.T) {
	f := newFakeImageScanAPI(t)
	f.cutFollow = true

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	report, err := runImageScan(context.Background(), k, []routePod{{Namespace: "web", Pod: "a"}}, imageScanOptions{}, func(imageScanProgress) {})
	if err != nil {
		t.Fatal(err)
	}

	if report.Images[0].Error != "" || len(report.Images[0].Vulnerabilities) != 4 || report.Images[1].Error == "not scanned" {
		t.Fatalf("report %+v", report.Images)
	}
}

func TestRegistryHost(t *testing.T) {
	for key, want := range map[string]string{
		"https://index.docker.io/v1/": "docker.io", "docker.io": "docker.io", "registry-1.docker.io": "docker.io",
		"GHCR.io": "ghcr.io", "http://localhost:5000": "localhost:5000", "quay.io/org": "quay.io",
	} {
		if got := registryHost(key); got != want {
			t.Errorf("registryHost(%q) = %q", key, got)
		}
	}

	got := targetRegistries([]scanTarget{{ref: "nginx@sha256:1"}, {ref: "ghcr.io/x/y:1"}})
	if !got["docker.io"] || !got["ghcr.io"] || len(got) != 2 {
		t.Fatalf("registries %v", got)
	}
}

func TestRunImageScanNoPods(t *testing.T) {
	f := newFakeImageScanAPI(t)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = runImageScan(context.Background(), k, []routePod{{Namespace: "web", Pod: "gone"}}, imageScanOptions{}, func(imageScanProgress) {})

	var refusal *netPerfRefusal
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v", err)
	}
}

type imageScanRecorder struct {
	mu       sync.Mutex
	progress []string
	done     chan [2]string
}

func (r *imageScanRecorder) OnProgress(js string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.progress = append(r.progress, js)
}

func (r *imageScanRecorder) OnDone(report, errMessage string) {
	r.done <- [2]string{report, errMessage}
}

func TestStartImageScanDemo(t *testing.T) {
	old := imageScanDemoStep
	imageScanDemoStep = time.Millisecond

	t.Cleanup(func() { imageScanDemoStep = old })

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	rec := &imageScanRecorder{done: make(chan [2]string, 1)}
	StartImageScan(cfg, "", "", `[{"namespace":"demo","pod":"web"}]`, "", rec)

	done := <-rec.done

	var report imageScanReport
	if err := json.Unmarshal([]byte(done[0]), &report); err != nil || done[1] != "" {
		t.Fatalf("%v %q", err, done[1])
	}

	if len(report.Images) != 2 || report.Images[0].Summary.Critical != 2 || report.Source != imageScanSourceScan {
		t.Fatalf("report %+v", report)
	}

	if len(rec.progress) != 6 {
		t.Fatalf("%d progress events", len(rec.progress))
	}

	ops, err := ImageScanOperatorReports(cfg, "", "", `[]`)
	if err != nil || !strings.Contains(ops, `"available":false`) {
		t.Fatalf("operator %v %s", err, ops)
	}
}

func TestStartImageScanCancelAndBadInput(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	rec := &imageScanRecorder{done: make(chan [2]string, 1)}
	StartImageScan(cfg, "", "", `[]`, "", rec).Cancel()

	if done := <-rec.done; done[1] != errImageScanStopped.Error() || !strings.Contains(done[0], `"images":[]`) {
		t.Fatalf("got %q %s", done[1], done[0])
	}

	rec = &imageScanRecorder{done: make(chan [2]string, 1)}
	StartImageScan(cfg, "", "", `not json`, "", rec)

	if done := <-rec.done; !strings.HasPrefix(done[1], "invalid pod list") {
		t.Fatalf("got %q", done[1])
	}
}

func TestImageScanDone(t *testing.T) {
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	if _, msg := imageScanDone(expired, imageScanReport{}, context.DeadlineExceeded); msg != errImageScanTimedOut.Error() {
		t.Fatalf("got %q", msg)
	}

	if js, msg := imageScanDone(context.Background(), imageScanReport{}, nil); msg != "" || !strings.Contains(js, `"images":[]`) {
		t.Fatalf("got %q %s", msg, js)
	}
}

func TestStartImageScanThroughAPI(t *testing.T) {
	f := newFakeImageScanAPI(t)
	useFakeKube(t, f.fakeKubeAPI)

	rec := &imageScanRecorder{done: make(chan [2]string, 1)}
	StartImageScan("cfg", "ctx", "", `[{"namespace":"web","pod":"a"}]`, `{"dbRepository":"r.lan/db:2"}`, rec)

	done := <-rec.done
	if done[1] != "" || !strings.Contains(done[0], `"ref":"docker.io/library/nginx@sha256:abc"`) {
		t.Fatalf("got %q %s", done[1], done[0])
	}

	// A refusal (no pod left) is reported as is.
	rec = &imageScanRecorder{done: make(chan [2]string, 1)}
	StartImageScan("cfg", "ctx", "", `[{"namespace":"web","pod":"gone"}]`, "", rec)

	if done := <-rec.done; !strings.HasPrefix(done[1], "none of these pods runs") {
		t.Fatalf("got %q", done[1])
	}

	rec = &imageScanRecorder{done: make(chan [2]string, 1)}
	StartImageScan("cfg", "ctx", "", `[]`, `{"dbRepository":"a b"}`, rec)

	if done := <-rec.done; !strings.HasPrefix(done[1], "invalid database repository") {
		t.Fatalf("got %q", done[1])
	}
}

func TestImageScanOperatorReportsAPI(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/web/pods/a": `{"spec":{"containers":[{"name":"web","image":"nginx:1.25"}]},
			"status":{"containerStatuses":[{"name":"web","image":"docker.io/library/nginx:1.25","imageID":"docker.io/library/nginx@sha256:abc"}]}}`,
		"GET /apis/aquasecurity.github.io/v1alpha1/namespaces/web/vulnerabilityreports": operatorReportsFixture,
	})
	useFakeKube(t, f)

	out, err := ImageScanOperatorReports("cfg", "ctx", "", `[{"namespace":"web","pod":"a"}]`)
	if err != nil || !strings.Contains(out, `"available":true`) || !strings.Contains(out, `"CVE-2024-1"`) {
		t.Fatalf("%v %s", err, out)
	}

	if _, err := ImageScanOperatorReports("cfg", "ctx", "", `{`); err == nil {
		t.Fatal("bad pods accepted")
	}
}

func TestVulnHelpers(t *testing.T) {
	for vector, want := range map[string]string{"CVSS:4.0/AV:N": "CVSSv4", "CVSS:3.1/AV:N": "CVSSv31", "CVSS:3.0/AV:N": "CVSSv3", "AV:N/AC:L": "CVSSv2", "": ""} {
		if got := cvssMethod(vector); got != want {
			t.Errorf("cvssMethod(%q) = %q", vector, got)
		}
	}

	for id, want := range map[string]string{"CVE-1": "nvd", "GHSA-x": "ghsa", "ALAS-2024": "alas"} {
		if got := vulnSource(id); got != want {
			t.Errorf("vulnSource(%q) = %q", id, got)
		}
	}

	if normalSeverity(" bogus ") != "UNKNOWN" || vulnSeverityRank("bogus") != len(vulnSeverities) || sarifLevel("LOW") != "note" {
		t.Fatal("severity helpers")
	}
}

func TestImageScanJob(t *testing.T) {
	job := imageScanJob("scan-abc", imageScanPodSpec("n", testTargets(), imageScanOptions{}, ""))

	js, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}

	s := string(js)
	for _, want := range []string{
		`"kind":"Job"`, `"backoffLimit":0`, `"ttlSecondsAfterFinished":600`, `"activeDeadlineSeconds":2400`,
		`"app.kubernetes.io/instance":"scan-abc"`, `"restartPolicy":"Never"`, `"automountServiceAccountToken":false`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("job lacks %s", want)
		}
	}

	if strings.Contains(s, `"nodeName"`) && !strings.Contains(s, `"nodeName":""`) {
		t.Fatalf("job pinned to a node: %s", s)
	}
}

func TestEnsureScanNamespace(t *testing.T) {
	f := newFakeImageScanAPI(t)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// Missing: created; there: kept as is.
	for range 2 {
		if err := ensureScanNamespace(context.Background(), k); err != nil {
			t.Fatal(err)
		}
	}

	if len(f.posted) != 1 {
		t.Fatalf("posted %v", f.posted)
	}

	// Someone else's namespace of that name is not used.
	f.nsLabels = `{"team":"x"}`

	var refusal *netPerfRefusal
	if err := ensureScanNamespace(context.Background(), k); !errors.As(err, &refusal) {
		t.Fatalf("got %v", err)
	}
}

func TestFindScanPodTimesOut(t *testing.T) {
	f := newFakeImageScanAPI(t)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	_, err = findScanPod(context.Background(), k, "scan-none", 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "started no pod in time") {
		t.Fatalf("got %v", err)
	}
}
