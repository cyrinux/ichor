package ichorgo

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// zipNames is the member names of the zip at path, in order.
func zipNames(t *testing.T, path string) []string {
	t.Helper()

	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}

	defer zr.Close() //nolint:errcheck

	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}

	return names
}

func TestDiagExport(t *testing.T) {
	withDiagRing(t)
	withDataDir(t)
	SetPrivacyMask(false, "")
	privacy.learnConfig(diagTestConfig)
	privacy.learnHost(diagTestHost, "controlplane")

	recordOutcome("", diagTestContext, auditAction{
		Action: "scale", Node: diagTestHost, Namespace: "pecan-team", Object: "Deployment/pecan-api", Params: "replicas=3",
	}, errors.New(`deployments "pecan-api" is forbidden`))

	traceCounts("config.parse", time.Now(), 2, 0)
	diag(slog.LevelWarn, "kube", "list failed", errors.New("Get https://"+diagTestAddr+":6443/api/v1/namespaces/pecan-team/pods: refused"))

	dest := filepath.Join(t.TempDir(), "diag.zip")
	app := `{"version":"1.2.3","model":"Pixel","note":"token=` + diagTestToken + `"}`
	platform := `[{"reason":"crash","host":"` + diagTestHost + `","at":"` + diagTestAddr + `"}]`

	out, err := DiagExport(dest, app, platform)
	if err != nil {
		t.Fatal(err)
	}

	var summary diagExportSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}

	wantFiles := []string{"README.txt", "app.json", "log.jsonl", "platform.json", "actions.md"}
	names, members := zipNames(t, dest), readZip(t, dest)

	if !slices.Equal(names, wantFiles) || !slices.Equal(summary.Files, wantFiles) {
		t.Errorf("members %v, summary files %v, want %v in that order", names, summary.Files, wantFiles)
	}

	if summary.Path != dest || summary.Bytes == 0 || summary.Entries < 2 {
		t.Errorf("summary = %+v", summary)
	}

	if _, err := os.Stat(dest + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Error("the .part file was left behind")
	}

	for _, name := range []string{"app.json", "platform.json"} {
		if !json.Valid([]byte(members[name])) {
			t.Errorf("%s is not JSON: %s", name, members[name])
		}
	}

	if lines := strings.Split(strings.TrimSpace(members["log.jsonl"]), "\n"); len(lines) != summary.Entries ||
		!strings.Contains(members["log.jsonl"], `"step":"config.parse"`) {
		t.Errorf("log.jsonl (%d lines, %d entries):\n%s", len(lines), summary.Entries, members["log.jsonl"])
	}

	if !strings.Contains(members["actions.md"], "cluster-1") || !strings.Contains(members["actions.md"], "Deployment") {
		t.Errorf("actions.md:\n%s", members["actions.md"])
	}

	for name, body := range members {
		for _, leak := range []string{diagTestToken, diagTestHost, diagTestAddr, "marsh-ridge", "pecan-team", "pecan-api"} {
			if strings.Contains(body, leak) {
				t.Errorf("%s holds %q:\n%s", name, leak, body)
			}
		}
	}
}

func TestDiagExportEmptyDocuments(t *testing.T) {
	withDiagRing(t)

	dest := filepath.Join(t.TempDir(), "empty.zip")
	if _, err := DiagExport(dest, "", "not json "+diagTestAddr); err != nil {
		t.Fatal(err)
	}

	members := readZip(t, dest)

	if members["app.json"] != "{}" || !json.Valid([]byte(members["platform.json"])) ||
		strings.Contains(members["platform.json"], diagTestAddr) {
		t.Errorf("app.json %q, platform.json %q", members["app.json"], members["platform.json"])
	}
}

func TestDiagExportRefusesOtherDirectories(t *testing.T) {
	withDiagRing(t)
	withDataDir(t)
	SetPrivacyMask(false, "")

	cases := []string{
		"",
		"/diag.zip",
		filepath.Join(os.TempDir(), "..", "diag.zip"),
	}

	for _, dest := range cases {
		if _, err := DiagExport(dest, "{}", "{}"); !errors.Is(err, errDiagDest) {
			t.Errorf("DiagExport(%q) error = %v, want a refusal", dest, err)
		}
	}
}
