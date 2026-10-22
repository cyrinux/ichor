package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

const (
	esoCRD     = "../../plugin/testdata/crds/external-secrets.io_externalsecrets.yaml"
	widgetsCRD = "../../plugin/testdata/crds/widgets.demo.io_widgetpools.yaml"
)

func TestPluginFromCRD(t *testing.T) {
	var stdout, stderr bytes.Buffer

	// kube-green's SleepInfo has a built-in plugin, External Secrets too.
	for crd, by := range map[string]string{sleepInfoCRD: "built-in plugin kube-green", esoCRD: "built-in plugin external-secrets"} {
		err := pluginFromCRD([]string{crd}, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "already covered by "+by) {
			t.Errorf("%s: %v, want covered by %s", crd, err, by)
		}
	}

	stdout.Reset()
	stderr.Reset()

	if err := pluginFromCRD([]string{sleepInfoCRD, "-force"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}

	if _, err := plugin.Load("draft.yaml", stdout.Bytes()); err != nil {
		t.Errorf("the draft on stdout does not load: %v", err)
	}

	var report struct {
		OK       bool   `json:"ok"`
		ID       string `json:"id"`
		Warnings int    `json:"warnings"`
	}

	if err := json.Unmarshal(stderr.Bytes(), &report); err != nil || !report.OK || report.ID != "kube-green" {
		t.Errorf("stderr report %q (%v)", stderr.String(), err)
	}

	// A CRD nothing reads yet, written to a file; twice the same bytes.
	out := filepath.Join(t.TempDir(), "widgets.yaml")
	for range 2 {
		stdout.Reset()
		stderr.Reset()

		if err := pluginFromCRD([]string{widgetsCRD, "-out", out, "-name", "Widgets"}, &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
	}

	written, err := os.ReadFile(out)
	if err != nil || stdout.Len() != 0 || !bytes.Contains(written, []byte("name: Widgets")) {
		t.Errorf("-out: %v, stdout %q", err, stdout.String())
	}

	// The written draft passes plugin-validate.
	if err := validatePluginFiles([]string{out}, &stdout, &stderr); err != nil {
		t.Errorf("plugin-validate on the draft: %v\n%s", err, stderr.String())
	}
}

func TestPluginFromCRDRefusals(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if err := pluginFromCRD([]string{"testdata/plugins/missing.yaml"}, &stdout, &stderr); err == nil {
		t.Error("a missing file was read")
	}

	// A plugin file is not a CRD.
	if err := pluginFromCRD([]string{builtinKubeGreen}, &stdout, &stderr); err == nil {
		t.Error("a plugin file was taken for a CRD")
	}

	if err := pluginFromCRD(nil, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "usage: plugin-from-crd") {
		t.Errorf("no manifest: %v", err)
	}
}
