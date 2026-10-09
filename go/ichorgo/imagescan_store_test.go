package ichorgo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// withScanStore gives the test an empty data directory and a fixed clock, returned as a setter.
func withScanStore(t *testing.T, at time.Time) (string, func(time.Time)) {
	t.Helper()

	dir := t.TempDir()
	SetDataDir(dir, testDataKey)
	t.Cleanup(func() { SetDataDir("", nil) })

	now := at
	imageScanNow = func() time.Time { return now }
	t.Cleanup(func() { imageScanNow = time.Now })

	return dir, func(next time.Time) { now = next }
}

func testVuln(id, severity string) imageVuln {
	return imageVuln{ID: id, Package: "libssl3", Installed: "3.0.11", Fixed: "3.0.13", Severity: severity, Class: vulnClassOS, Target: "app (debian 12.4)"}
}

func keptScanReport(started int64, digest string, vulns ...imageVuln) imageScanReport {
	img := withVulns(scannedImage{
		Image: "registry.example.com/app:1", Ref: "registry.example.com/app@" + digest, Digest: digest,
		Pods: []string{"web/app-0"}, ScannedAt: started + 10,
	}, vulns)

	return imageScanReport{Source: imageScanSourceScan, Scanner: "Trivy " + trivyVersion, Started: started, Finished: started + 20, Images: []scannedImage{img}}
}

func scanHistory(t *testing.T, cluster string) []savedScan {
	t.Helper()

	out, err := ImageScanHistory(cluster)
	if err != nil {
		t.Fatal(err)
	}

	var entries []savedScan
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return entries
}

func TestMarkNewFindings(t *testing.T) {
	prev := scannedImage{Digest: "sha256:a", ScannedAt: 100, Vulnerabilities: []imageVuln{testVuln("CVE-1", "HIGH"), testVuln("CVE-2", "LOW")}}
	previous := func(digest string) (scannedImage, bool) { return prev, digest == "sha256:a" }

	for _, tc := range []struct {
		name      string
		image     scannedImage
		wantNew   []string
		wantCount vulnSummary
		wantPrev  int64
	}{
		{"same digest, same findings", withVulns(scannedImage{Digest: "sha256:a"}, prev.Vulnerabilities), nil, vulnSummary{}, 100},
		{"a new CVE", withVulns(scannedImage{Digest: "sha256:a"}, []imageVuln{testVuln("CVE-1", "HIGH"), testVuln("CVE-3", "CRITICAL")}),
			[]string{"CVE-3"}, vulnSummary{Critical: 1, Fixable: 1, OS: 1}, 100},
		{"a fixed CVE gone", withVulns(scannedImage{Digest: "sha256:a"}, []imageVuln{testVuln("CVE-1", "HIGH")}), nil, vulnSummary{}, 100},
		{"never scanned", withVulns(scannedImage{Digest: "sha256:b"}, []imageVuln{testVuln("CVE-9", "HIGH")}), nil, vulnSummary{}, 0},
		{"image error", scannedImage{Digest: "sha256:a", Error: "not scanned"}, nil, vulnSummary{}, 0},
		{"no digest", withVulns(scannedImage{}, []imageVuln{testVuln("CVE-9", "HIGH")}), nil, vulnSummary{}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := imageScanReport{Images: []scannedImage{tc.image}}
			got := markNewFindings(report, previous).Images[0]

			var fresh []string
			for _, v := range got.Vulnerabilities {
				if v.New {
					fresh = append(fresh, v.ID)
				}
			}

			if !reflect.DeepEqual(fresh, tc.wantNew) || got.NewSummary != tc.wantCount || got.PreviousScannedAt != tc.wantPrev {
				t.Fatalf("new %v %+v prev %d", fresh, got.NewSummary, got.PreviousScannedAt)
			}

			// The report given is not changed.
			if len(tc.image.Vulnerabilities) > 0 && report.Images[0].Vulnerabilities[0].New {
				t.Fatal("input mutated")
			}
		})
	}
}

func TestImageScanStoreRoundTrip(t *testing.T) {
	dir, _ := withScanStore(t, time.UnixMilli(1_000_000))

	first := keepScan("prod", "web", keptScanReport(1_000_000, "sha256:a", testVuln("CVE-1", "HIGH")))
	if first.Images[0].PreviousScannedAt != 0 || first.Images[0].NewSummary != (vulnSummary{}) {
		t.Fatalf("first scan %+v", first.Images[0])
	}

	second := keepScan("prod", "web", keptScanReport(2_000_000, "sha256:a", testVuln("CVE-1", "HIGH"), testVuln("CVE-2", "HIGH")))
	img := second.Images[0]

	if img.PreviousScannedAt != 1_000_010 || img.NewSummary.High != 1 || img.Vulnerabilities[0].New == img.Vulnerabilities[1].New {
		t.Fatalf("second scan %+v", img)
	}

	history := scanHistory(t, "")
	if len(history) != 2 || history[0].Started != 2_000_000 || history[0].New.High != 1 || history[0].App != "web" ||
		history[0].Cluster != "prod" || history[0].Summary.High != 2 || !reflect.DeepEqual(history[0].Digests, []string{"sha256:a"}) {
		t.Fatalf("history %+v", history)
	}

	// The kept report is what OnDone got.
	saved, err := ImageScanSaved(history[0].ID)
	if err != nil {
		t.Fatal(err)
	}

	if done, _ := imageScanDone(t.Context(), second, nil); saved != done {
		t.Fatalf("saved %s\n done %s", saved, done)
	}

	// Encrypted on disk.
	raw, err := os.ReadFile(filepath.Join(dir, imageScanDir, history[0].ID+".json"))
	if err != nil || strings.Contains(string(raw), "CVE-2") {
		t.Fatalf("%v: plain file", err)
	}

	if got := scanHistory(t, "staging"); len(got) != 0 {
		t.Fatalf("other cluster %+v", got)
	}

	if err := ImageScanForget(history[0].ID); err != nil {
		t.Fatal(err)
	}

	if _, err := ImageScanSaved(history[0].ID); err == nil {
		t.Fatal("forgotten report still read")
	}

	if _, err := os.Stat(filepath.Join(dir, imageScanDir, history[0].ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("file left: %v", err)
	}

	if err := ImageScanClear("prod"); err != nil || len(scanHistory(t, "")) != 0 {
		t.Fatalf("clear %v", err)
	}
}

func TestImageScanStoreEvictsAndExpires(t *testing.T) {
	dir, setNow := withScanStore(t, time.UnixMilli(1_000_000))

	for i := range imageScanMaxSaved + 1 {
		keepScan("prod", "web", keptScanReport(int64(1_000_000+i), fmt.Sprintf("sha256:%d", i), testVuln("CVE-1", "LOW")))
	}

	history := scanHistory(t, "prod")
	if len(history) != imageScanMaxSaved || history[len(history)-1].Started != 1_000_001 {
		t.Fatalf("%d kept, oldest %d", len(history), history[len(history)-1].Started)
	}

	if files, _ := os.ReadDir(filepath.Join(dir, imageScanDir)); len(files) != imageScanMaxSaved {
		t.Fatalf("%d files", len(files))
	}

	setNow(time.UnixMilli(1_000_000).Add(imageScanRetention + time.Hour))

	if got := scanHistory(t, ""); len(got) != 0 {
		t.Fatalf("expired kept: %d", len(got))
	}
}

func TestImageScanStoreRefusals(t *testing.T) {
	SetDataDir("", nil)

	// Without a data directory nothing is kept, and nothing fails.
	report := keepScan("prod", "web", keptScanReport(1, "sha256:a", testVuln("CVE-1", "HIGH")))
	if len(report.Images) != 1 || len(scanHistory(t, "")) != 0 {
		t.Fatal("kept without a data directory")
	}

	withScanStore(t, time.UnixMilli(1_000_000))

	for _, id := range []string{"1-deadbeef", "../audit-log", "1-DEADBEEF", ""} {
		if _, err := ImageScanSaved(id); err == nil {
			t.Errorf("%q read", id)
		}
	}

	if err := ImageScanForget("../../x"); err == nil {
		t.Error("path id accepted")
	}

	// A report with no image scanned is not kept.
	failed := keptScanReport(5, "sha256:a")
	failed.Images[0].Error = "the image is no longer in its registry"

	if keepScan("prod", "web", failed); len(scanHistory(t, "")) != 0 {
		t.Fatal("failed report kept")
	}
}

// A demo scan is kept like a real one; the same images again have nothing new.
func TestImageScanStoreDemo(t *testing.T) {
	withScanStore(t, time.Now())

	old := imageScanDemoStep
	imageScanDemoStep = time.Millisecond

	t.Cleanup(func() { imageScanDemoStep = old })

	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		rec := &imageScanRecorder{done: make(chan [2]string, 1)}
		StartImageScan(cfg, "", "", `[{"namespace":"demo","pod":"web"}]`, `{"app":"web"}`, rec)

		if done := <-rec.done; done[1] != "" {
			t.Fatal(done[1])
		}
	}

	history := scanHistory(t, "")
	if len(history) != 2 || history[0].App != "web" || history[0].New != (vulnSummary{}) || history[0].Summary.Critical == 0 {
		t.Fatalf("history %+v", history)
	}

	var report imageScanReport

	saved, err := ImageScanSaved(history[0].ID)
	if err != nil || json.Unmarshal([]byte(saved), &report) != nil || report.Images[0].PreviousScannedAt == 0 {
		t.Fatalf("%v %s", err, saved)
	}
}
