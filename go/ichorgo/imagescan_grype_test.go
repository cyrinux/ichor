package ichorgo

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func readGrypeFixture(t *testing.T) []byte {
	t.Helper()

	data, err := os.ReadFile("testdata/grype-alpine.json")
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func TestParseGrypeReport(t *testing.T) {
	img, err := parseGrypeReport(readGrypeFixture(t), scannedImage{Image: "app:1", Ref: "registry.example/team/app@sha256:abc", Pods: []string{"web/a"}})
	if err != nil {
		t.Fatal(err)
	}

	if img.OS != "alpine 3.18.0" || img.Digest != "sha256:abc" || img.Image != "app:1" || !slices.Equal(img.Pods, []string{"web/a"}) {
		t.Fatalf("image %+v", img)
	}

	want := vulnSummary{Critical: 1, High: 1, Low: 1, Fixable: 2, OS: 2}
	if img.Summary != want || len(img.Vulnerabilities) != 3 {
		t.Fatalf("summary %+v (%d)", img.Summary, len(img.Vulnerabilities))
	}

	lang, ssl, busybox := img.Vulnerabilities[0], img.Vulnerabilities[1], img.Vulnerabilities[2]

	// A language package: its file is the target, CVSS v4 when it has nothing else.
	if lang.ID != "GHSA-aaaa-bbbb-cccc" || lang.Severity != "CRITICAL" || lang.Class != "lang-pkgs" || lang.Type != "go-module" ||
		lang.Target != "usr/local/bin/app" || lang.Fixed != "0.23.0, 0.24.1" || lang.Score != 8.7 ||
		!strings.HasPrefix(lang.Vector, "CVSS:4.0/") || lang.PURL != "pkg:golang/golang.org/x/net@v0.17.0" ||
		lang.URL != "https://github.com/advisories/GHSA-aaaa-bbbb-cccc" || lang.Title != "x/net: unbounded header frames exhaust memory" {
		t.Fatalf("lang %+v", lang)
	}

	// An OS package: the distro is the target; the NVD record gives the description and the
	// v3 score over v2; the title is the description's first sentence.
	if ssl.ID != "CVE-2023-1111" || ssl.Severity != "HIGH" || ssl.Class != vulnClassOS || ssl.Target != "alpine 3.18.0" ||
		ssl.Installed != "3.1.0-r4" || ssl.Fixed != "3.1.4-r0" || ssl.Score != 7.5 || !strings.HasPrefix(ssl.Vector, "CVSS:3.1/") ||
		ssl.URL != "https://security.alpinelinux.org/vuln/CVE-2023-1111" ||
		ssl.Title != "A crafted certificate makes name checks read past a buffer." || !strings.HasSuffix(ssl.Description, "Clients may crash.") {
		t.Fatalf("ssl %+v", ssl)
	}

	// Negligible is Low; no fix, no score.
	if busybox.Severity != "LOW" || busybox.Fixed != "" || busybox.Score != 0 || busybox.Vector != "" {
		t.Fatalf("busybox %+v", busybox)
	}

	// A digest already known is kept.
	if kept, _ := parseGrypeReport(readGrypeFixture(t), scannedImage{Digest: "sha256:pulled"}); kept.Digest != "sha256:pulled" {
		t.Fatalf("digest %q", kept.Digest)
	}

	for _, bad := range []string{`{`, `{"matches":[]}`} {
		if _, err := parseGrypeReport([]byte(bad), scannedImage{}); err == nil || !strings.HasPrefix(err.Error(), "unreadable Grype report") {
			t.Errorf("%s: %v", bad, err)
		}
	}

	// No match at all is an empty report, not nil.
	empty, err := parseGrypeReport([]byte(`{"matches":[],"source":{"type":"image"},"distro":{"name":"","version":""}}`), scannedImage{})
	if err != nil || empty.Vulnerabilities == nil || len(empty.Vulnerabilities) != 0 || empty.OS != "" {
		t.Fatalf("empty %+v %v", empty, err)
	}
}

func TestGrypeTitle(t *testing.T) {
	for in, want := range map[string]string{
		"":                               "",
		"One sentence only":              "One sentence only",
		"First. Second.":                 "First.",
		"line one\nline two":             "line one",
		strings.Repeat("é", 130) + ". x": strings.Repeat("é", 120) + "…",
	} {
		if got := grypeTitle(in); got != want {
			t.Errorf("grypeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeImageScanOptionsScanner(t *testing.T) {
	for in, want := range map[string]string{"": "", `{}`: "", `{"scanner":"trivy"}`: "trivy", `{"scanner":"grype"}`: "grype"} {
		o, err := decodeImageScanOptions(in)
		if err != nil || o.Scanner != want {
			t.Errorf("%s: %+v %v", in, o, err)
		}
	}

	if o, err := decodeImageScanOptions(`{"scanner":"grype","grypeDBUpdateURL":"https://mirror.example/grype/databases"}`); err != nil ||
		o.GrypeDBUpdateURL != "https://mirror.example/grype/databases" {
		t.Fatalf("%+v %v", o, err)
	}

	for _, bad := range []string{
		`{"scanner":"x"}`, `{"scanner":"Grype"}`, `{"scanner":"grype; id"}`,
		`{"grypeDBUpdateURL":"ftp://mirror.example/db"}`, `{"grypeDBUpdateURL":"mirror.example/db"}`,
		`{"grypeDBUpdateURL":"https://mirror.example/$(id)"}`, `{"grypeDBUpdateURL":"https://"}`,
	} {
		if _, err := decodeImageScanOptions(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}

	// A refused scanner fails before any API call: the target is not even a kubeconfig.
	if _, err := startImageScan(t.Context(), kubeTarget{"not yaml", "ctx", ""}, `[{"namespace":"web","pod":"a"}]`, `{"scanner":"x"}`,
		func(imageScanProgress) {}); err == nil || !strings.Contains(err.Error(), `unknown scanner "x"`) {
		t.Fatalf("refused scanner %v", err)
	}
}

func TestImageScanPodSpecGrype(t *testing.T) {
	opts := imageScanOptions{Scanner: scannerGrype, GrypeDBUpdateURL: "https://mirror.example/db", DBRepository: "registry.lan/trivy-db:2"}
	spec := imageScanPodSpec("n0nce", testTargets(), opts, "scan-x-auth")

	js, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}

	s := string(js)
	for _, want := range []string{
		`"image":"` + grypeImage + `"`, `"name":"grype"`, `"automountServiceAccountToken":false`, `"readOnlyRootFilesystem":true`,
		`"runAsNonRoot":true`, `"drop":["ALL"]`, `"activeDeadlineSeconds":2400`,
		`"name":"GRYPE_DB_CACHE_DIR","value":"/work/cache"`, `"name":"GRYPE_DB_AUTO_UPDATE","value":"false"`,
		`"name":"GRYPE_CHECK_FOR_APP_UPDATE","value":"false"`, `"name":"GRYPE_DB_UPDATE_URL","value":"https://mirror.example/db"`,
		`"name":"GRYPE_DB_UPDATE_DOWNLOAD_TIMEOUT","value":"15m"`, `"name":"TMPDIR","value":"/work/tmp"`,
		`"name":"DOCKER_CONFIG","value":"/docker"`, `"secretName":"scan-x-auth"`, `"mountPath":"/work"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("spec lacks %s", want)
		}
	}

	if strings.Contains(s, "TRIVY_") {
		t.Errorf("Grype spec has Trivy settings: %s", s)
	}

	command := spec["spec"].(map[string]any)["containers"].([]map[string]any)[0]["command"].([]string)
	if command[0] != "/busybox/sh" || command[2] != grypeScanScript {
		t.Fatalf("command %v", command[:2])
	}

	if want := []string{"sh", "n0nce", "nginx@sha256:abc", "ghcr.io/x/app@sha256:def"}; !slices.Equal(command[3:], want) {
		t.Fatalf("command args %v", command[3:])
	}

	plain, _ := json.Marshal(imageScanPodSpec("n", testTargets(), imageScanOptions{Scanner: scannerGrype}, ""))
	if strings.Contains(string(plain), "GRYPE_DB_UPDATE_URL") || strings.Contains(string(plain), "DOCKER_CONFIG") {
		t.Fatalf("plain spec %s", plain)
	}

	// The script keeps the markers the log parser reads, and never lets an image read as a flag.
	for _, want := range []string{`"@@$n db"`, `"@@$n dbfail"`, `"@@$n scan $i"`, `"@@$n result $i"`, `"@@$n fail $i"`, `"@@$n end $i"`, `"registry:$img"`, "-o json --file /work/r.json"} {
		if !strings.Contains(grypeScanScript, want) {
			t.Errorf("script lacks %s", want)
		}
	}

	if !strings.Contains(grypeImage, "@sha256:") || !strings.Contains(grypeImage, ":v"+grypeVersion+"-debug@") {
		t.Fatalf("image %s not pinned", grypeImage)
	}
}

func TestGrypeError(t *testing.T) {
	text := "[0002] ERROR failed to catalog: errors occurred attempting to resolve 'registry.example/app:1':\n" +
		"  - oci-registry: failed to get image descriptor from registry: GET https://registry.example/v2/app/manifests/1: UNAUTHORIZED: authentication required\n"

	got := grypeScanner.errorOf(text)
	if !strings.HasPrefix(got, "the registry refused the pull: ") || !strings.Contains(got, ": oci-registry: failed to get image descriptor") {
		t.Fatalf("grype error %q", got)
	}

	if got := grypeScanner.errorOf("[0000] ERROR something odd\n"); got != "something odd" {
		t.Fatalf("plain %q", got)
	}

	if got := grypeScanner.errorOf(""); got != "Grype failed without a message" {
		t.Fatalf("empty %q", got)
	}
}

func TestScanLogParserGrype(t *testing.T) {
	targets := testTargets()
	report := imageScanReport{Images: pendingImages(targets)}
	p := newScanLogParser(grypeScanner, "n", targets, &report, func(imageScanProgress) {})

	_ = p.read(strings.NewReader(scanLogWith("n", string(readGrypeFixture(t)))))

	if first := report.Images[0]; first.Error != "" || len(first.Vulnerabilities) != 3 || first.OS != "alpine 3.18.0" {
		t.Fatalf("first %+v", first)
	}

	// The database could not be downloaded: Grype says so.
	report = imageScanReport{Images: pendingImages(targets)}
	p = newScanLogParser(grypeScanner, "n", targets, &report, func(imageScanProgress) {})
	_ = p.read(strings.NewReader("@@n db\n@@n dbfail\n[0001] ERROR unable to update db: context deadline exceeded\n@@n end db\n"))

	if err := p.outcome(netPerfPod{}); err == nil || !strings.HasPrefix(err.Error(), "Grype could not download its vulnerability database") {
		t.Fatalf("db %v", err)
	}
}

func TestStartImageScanDemoGrype(t *testing.T) {
	old := imageScanDemoStep
	imageScanDemoStep = time.Millisecond

	t.Cleanup(func() { imageScanDemoStep = old })

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	report, err := startImageScan(t.Context(), kubeTarget{cfg, "", ""}, `[{"namespace":"demo","pod":"web"}]`, `{"scanner":"grype"}`, func(imageScanProgress) {})
	if err != nil || report.Scanner != "Grype "+grypeVersion || len(report.Images) != 2 {
		t.Fatalf("demo %+v %v", report, err)
	}
}
