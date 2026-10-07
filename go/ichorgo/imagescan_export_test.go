package ichorgo

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func testScanReport(t *testing.T) imageScanReport {
	t.Helper()

	img, err := parseTrivyReport([]byte(trivyFixture), scannedImage{Image: "nginx:1.25", Ref: "nginx@sha256:abc", Pods: []string{"web/a"}})
	if err != nil {
		t.Fatal(err)
	}

	failed := scannedImage{Image: "ghcr.io/x/app:2", Error: "the image is no longer in its registry", Vulnerabilities: []imageVuln{}}

	return imageScanReport{Source: imageScanSourceScan, Scanner: "Trivy " + trivyVersion, Finished: 1_700_000_000_000, Images: []scannedImage{img, failed}}
}

func TestSARIFExport(t *testing.T) {
	out, err := exportImageScan(testScanReport(t), "sarif", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	var sarif struct {
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Rules []struct {
						ID         string `json:"id"`
						Name       string `json:"name"`
						Properties struct {
							Severity string `json:"security-severity"`
						} `json:"properties"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID    string `json:"ruleId"`
				RuleIndex int    `json:"ruleIndex"`
				Level     string `json:"level"`
				Locations []struct {
					Physical struct {
						Artifact struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}

	if err := json.Unmarshal([]byte(out), &sarif); err != nil {
		t.Fatal(err)
	}

	run := sarif.Runs[0]
	if sarif.Version != "2.1.0" || len(run.Tool.Driver.Rules) != 4 || len(run.Results) != 4 {
		t.Fatalf("sarif %s", out)
	}

	for _, r := range run.Results {
		if run.Tool.Driver.Rules[r.RuleIndex].ID != r.RuleID || len(r.Locations) != 1 {
			t.Fatalf("result %+v", r)
		}
	}

	// CVE-2024-3 (critical, no score) gets the severity's stand-in; the Go library is placed at
	// its binary.
	if r := run.Tool.Driver.Rules[0]; r.ID != "CVE-2024-3" || r.Properties.Severity != "9.5" || r.Name != "OsPackageVulnerability" {
		t.Fatalf("rule %+v", r)
	}

	last := run.Results[3]
	if last.Level != "warning" || last.Locations[0].Physical.Artifact.URI != "library/nginx/usr/local/bin/app" {
		t.Fatalf("last %+v", last)
	}

	empty, err := exportImageScan(imageScanReport{}, "sarif", time.Now())
	if err != nil || !strings.Contains(empty, `"results":[]`) || !strings.Contains(empty, `"rules":[]`) {
		t.Fatalf("empty %v %s", err, empty)
	}
}

func TestCycloneDXExport(t *testing.T) {
	out, err := exportImageScan(testScanReport(t), "cyclonedx", time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	type component struct {
		BomRef     string      `json:"bom-ref"`
		Type       string      `json:"type"`
		Name       string      `json:"name"`
		Version    string      `json:"version"`
		PURL       string      `json:"purl"`
		Components []component `json:"components"`
	}

	var bom struct {
		BomFormat    string `json:"bomFormat"`
		SpecVersion  string `json:"specVersion"`
		SerialNumber string `json:"serialNumber"`
		Metadata     struct {
			Timestamp string `json:"timestamp"`
		} `json:"metadata"`
		Components      []component `json:"components"`
		Vulnerabilities []struct {
			ID      string `json:"id"`
			Ratings []struct {
				Severity string  `json:"severity"`
				Score    float64 `json:"score"`
				Method   string  `json:"method"`
			} `json:"ratings"`
			Recommendation string `json:"recommendation"`
			Affects        []struct {
				Ref string `json:"ref"`
			} `json:"affects"`
		} `json:"vulnerabilities"`
	}

	if err := json.Unmarshal([]byte(out), &bom); err != nil {
		t.Fatal(err)
	}

	if bom.BomFormat != "CycloneDX" || bom.SpecVersion != "1.6" || len(bom.SerialNumber) != 45 || bom.Metadata.Timestamp != "2026-10-07T12:00:00Z" {
		t.Fatalf("header %s", out)
	}

	if len(bom.Components) != 2 || bom.Components[0].Type != "container" || len(bom.Components[0].Components) != 4 ||
		!strings.HasPrefix(bom.Components[0].PURL, "pkg:oci/nginx@sha256%3Aabc?repository_url=docker.io/library/nginx") {
		t.Fatalf("components %+v", bom.Components)
	}

	refs := map[string]bool{}
	for _, c := range bom.Components[0].Components {
		refs[c.BomRef] = true
	}

	for _, v := range bom.Vulnerabilities {
		for _, a := range v.Affects {
			if !refs[a.Ref] {
				t.Fatalf("%s affects unknown %s", v.ID, a.Ref)
			}
		}
	}

	ssl := bom.Vulnerabilities[2]
	if ssl.ID != "CVE-2024-1" || ssl.Ratings[0].Severity != "high" || ssl.Ratings[0].Method != "CVSSv31" ||
		ssl.Recommendation != "Upgrade libssl3 to version 3.0.13" {
		t.Fatalf("ssl %+v", ssl)
	}
}

func TestCSVAndHTMLExport(t *testing.T) {
	report := testScanReport(t)
	report.Images[0].Vulnerabilities[0].Title = "=HYPERLINK(\"x\")"
	report.Images[0].Vulnerabilities[1].Title = `<script>alert(1)</script>`
	report.Images[0].Vulnerabilities[1].URL = "javascript:alert(1)"
	report.Images[0].Vulnerabilities[2].Installed = " -1+cmd|' /C calc'!A0"

	out, err := exportImageScan(report, "csv", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	rows, err := csv.NewReader(strings.NewReader(out)).ReadAll()
	if err != nil || len(rows) != 5 || rows[0][4] != "id" || rows[1][9] != `'=HYPERLINK("x")` || !strings.HasPrefix(rows[3][6], "'") {
		t.Fatalf("%v %q", err, rows)
	}

	page, err := exportImageScan(report, "html", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"<h2>nginx:1.25</h2>", "Critical 2", "3 of 4 fixable", "3 in the OS packages (debian 12.4)", `class="chip plain">Low 0`, "the image is no longer in its registry", "&lt;script&gt;", "#ZgotmplZ"} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}

	if strings.Contains(page, "<script>") || strings.Contains(page, "javascript:") {
		t.Fatal("page runs script")
	}

	if _, err := exportImageScan(report, "pdf", time.Now()); err == nil {
		t.Fatal("pdf accepted")
	}
}

func TestImageScanExportAPI(t *testing.T) {
	js, err := toJSON(testScanReport(t))
	if err != nil {
		t.Fatal(err)
	}

	for _, format := range []string{"sarif", "cyclonedx", "html", "csv", "json"} {
		if out, err := ImageScanExport(js, format); err != nil || out == "" {
			t.Errorf("%s: %v", format, err)
		}
	}

	if _, err := ImageScanExport("{", "json"); err == nil {
		t.Fatal("broken report accepted")
	}
}

const operatorReportsFixture = `{"items":[
  {"report":{"updateTimestamp":"2026-10-01T00:00:00Z","scanner":{"name":"Trivy","version":"0.70.0"},
   "registry":{"server":"index.docker.io"},"artifact":{"repository":"library/nginx","tag":"1.25"},
   "os":{"family":"debian","name":"12.4"},
   "vulnerabilities":[{"vulnerabilityID":"CVE-OLD","resource":"libc6","installedVersion":"1","severity":"LOW"}]}},
  {"report":{"updateTimestamp":"2026-10-05T00:00:00Z","scanner":{"name":"Trivy","version":"0.75.0"},
   "registry":{"server":"index.docker.io"},"artifact":{"repository":"library/nginx","tag":"1.25","digest":"sha256:abc"},
   "os":{"family":"debian","name":"12.4","eosl":true},
   "vulnerabilities":[
     {"vulnerabilityID":"CVE-2024-1","resource":"libssl3","installedVersion":"3.0.11","fixedVersion":"3.0.13","severity":"HIGH","score":7.5,"class":"os-pkgs","packagePURL":"pkg:deb/debian/libssl3@3.0.11"},
     {"vulnerabilityID":"CVE-2024-2","resource":"zlib1g","installedVersion":"1.2.13","severity":"CRITICAL"}]}},
  {"report":{"updateTimestamp":"2026-10-06T00:00:00Z","registry":{"server":"quay.io"},"artifact":{"repository":"other/app","tag":"1"}}}
]}`

func TestReadOperatorReports(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/web/pods/a": `{"metadata":{"ownerReferences":[{"kind":"ReplicaSet","name":"web-7d9c","controller":true}]},
			"spec":{"containers":[{"name":"web","image":"nginx:1.25"},{"name":"app","image":"ghcr.io/x/app:2"}]},
			"status":{"containerStatuses":[{"name":"web","image":"docker.io/library/nginx:1.25","imageID":"docker.io/library/nginx@sha256:abc"}]}}`,
		"GET /apis/aquasecurity.github.io/v1alpha1/namespaces/web/vulnerabilityreports": operatorReportsFixture,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	out, err := readOperatorReports(context.Background(), k, []routePod{{Namespace: "web", Pod: "a"}})
	if err != nil {
		t.Fatal(err)
	}

	if !out.Available || len(out.Report.Images) != 1 || out.Report.Source != imageScanSourceOperator {
		t.Fatalf("reports %+v", out)
	}

	selectorAsked := false
	for _, r := range f.recorded() {
		if strings.HasSuffix(r.path, "/vulnerabilityreports") &&
			strings.Contains(r.query, "trivy-operator.resource.kind%3DReplicaSet%2Ctrivy-operator.resource.name%3Dweb-7d9c") {
			selectorAsked = true
		}
	}

	if !selectorAsked {
		t.Fatalf("reports not read by owner: %+v", f.recorded())
	}

	img := out.Report.Images[0]
	if img.OS != "debian 12.4 (end of life)" || img.Summary.Critical != 1 || img.Summary.High != 1 || img.Vulnerabilities[1].Score != 7.5 ||
		out.Report.Scanner != "Trivy Operator (Trivy 0.75.0)" {
		t.Fatalf("image %+v %s", img, out.Report.Scanner)
	}

	// Without the CRD: not available, no error.
	bare := newFakeKubeAPI(t, map[string]string{"GET /api/v1/namespaces/web/pods/a": `{"spec":{"containers":[{"image":"nginx:1.25"}]}}`})

	kb, err := openKubeClient(context.Background(), bare.kubeconfigFor(bare.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if out, err := readOperatorReports(context.Background(), kb, []routePod{{Namespace: "web", Pod: "a"}}); err != nil || out.Available {
		t.Fatalf("bare %+v %v", out, err)
	}

	// Pods gone: nothing to read.
	if out, err := readOperatorReports(context.Background(), kb, []routePod{{Namespace: "web", Pod: "gone"}}); err != nil || len(out.Report.Images) != 0 {
		t.Fatalf("gone %+v %v", out, err)
	}
}

func TestNewestReport(t *testing.T) {
	var list kubeList[vulnerabilityReport]
	if err := json.Unmarshal([]byte(operatorReportsFixture), &list); err != nil {
		t.Fatal(err)
	}

	byOwner := map[string][]vulnerabilityReport{"web/ReplicaSet/web-7d9c": list.Items}
	target := func(image, digest string) scanTarget {
		return scanTarget{image: image, digest: digest, owners: []string{"web/ReplicaSet/web-7d9c"}}
	}

	// No digest known: the newest report on the same repository and tag.
	r, ok := newestReport(target("nginx:1.25", ""), byOwner)
	if !ok || r.Report.Artifact.Digest != "sha256:abc" {
		t.Fatalf("%v %+v", ok, r.Report.Artifact)
	}

	if _, ok := newestReport(target("nginx:1.26", ""), byOwner); ok {
		t.Fatal("other tag matched")
	}

	// A digest known: a report on the same tag but another digest is stale.
	if _, ok := newestReport(target("nginx:1.25", "sha256:new"), byOwner); ok {
		t.Fatal("stale report matched")
	}
}
