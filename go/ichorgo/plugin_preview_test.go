package ichorgo

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// KubePluginPreview renders a plugin file that is not installed, from the cluster or from its
// samples, exactly as KubePluginData renders an installed one.
func TestKubePluginPreview(t *testing.T) {
	usePlugins(t, "valid.yaml")

	src, err := os.ReadFile(filepath.Join("..", "plugin", "testdata", "plugins", "valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	kubeGreenCluster(t, http.StatusForbidden, forbiddenStatus)

	installed, err := KubePluginData("cfg", "ctx", "", "kube-green", "", "fr")
	if err != nil {
		t.Fatal(err)
	}

	preview, err := KubePluginPreview("cfg", "ctx", "", string(src), "", "fr")
	if err != nil || preview != installed {
		t.Errorf("preview differs from the installed render (%v):\n%s\n%s", err, preview, installed)
	}
}

func TestKubePluginPreviewDemo(t *testing.T) {
	usePlugins(t, "valid.yaml")
	noKubeDial(t)

	widgets, err := os.ReadFile(filepath.Join("..", "plugin", "testdata", "plugins", "widgets.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubePluginPreview(demoKubeconfigForTest(t), demoKubeContext, "", string(widgets), "", "")

	tree := pluginTree(t, out, err)
	if tree.Plugin.ID != "demo-widgets" || tree.DetectedVia != "demo" || len(tree.Cards) == 0 {
		t.Errorf("demo preview %+v", tree)
	}
}

// revealingPlugin is the kube-green fixture with its state Secret revealed and one of its
// values shown on each row.
func revealingPlugin(t *testing.T) string {
	t.Helper()

	src, err := os.ReadFile(filepath.Join("..", "plugin", "testdata", "plugins", "valid.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	out := strings.Replace(string(src), "reveal: false", "reveal: true", 1)
	out = strings.Replace(out, `            - { label: "Weekdays", value: fields.weekdays }`,
		`            - { label: "Weekdays", value: fields.weekdays }
            - { label: "Key", value: fields.state.data.k | default("") }`, 1)

	if !strings.Contains(out, "reveal: true") || !strings.Contains(out, `label: "Key"`) {
		t.Fatal("the fixture changed: update revealingPlugin")
	}

	return out
}

// A revealed Secret shows its values for an installed plugin only, and never in screenshot
// mode.
func TestPluginRevealNeedsAnInstalledPluginOutsideScreenshotMode(t *testing.T) {
	usePlugins(t, "valid.yaml")

	src := revealingPlugin(t)

	c, err := plugin.CompileSource("reveal.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	knownPlugins = func() []*plugin.Compiled { return []*plugin.Compiled{c} }

	kubeGreenCluster(t, http.StatusOK, `{"items":[{"metadata":{"name":"sleepinfo-weekend","namespace":"demo-lab"},"type":"Opaque","data":{"k":"c2VjcmV0"}}]}`)

	installed, err := KubePluginData("cfg", "ctx", "", "kube-green", "", "")
	if err != nil || !strings.Contains(installed, `"c2VjcmV0"`) {
		t.Errorf("an installed plugin's reveal is not shown (%v): %s", err, installed)
	}

	preview, err := KubePluginPreview("cfg", "ctx", "", src, "", "")
	if err != nil || strings.Contains(preview, "c2VjcmV0") || !strings.Contains(preview, "hidden, 8 characters") {
		t.Errorf("a preview revealed a Secret (%v): %s", err, preview)
	}

	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	masked, err := KubePluginData("cfg", "ctx", "", "kube-green", "", "")
	if err != nil || strings.Contains(masked, "c2VjcmV0") || !strings.Contains(masked, "hidden, 8 characters") {
		t.Errorf("screenshot mode revealed a Secret (%v): %s", err, masked)
	}
}

func TestKubePluginPreviewRefusesABrokenPlugin(t *testing.T) {
	broken := "schemaVersion: ichor.dev/plugin/v1alpha1\nid: x\nbogus: 1\n"

	if _, err := KubePluginPreview("cfg", "ctx", "", broken, "", ""); err == nil || !strings.Contains(err.Error(), "plugin.yaml:3: unknown key") {
		t.Errorf("a broken plugin: %v", err)
	}
}
