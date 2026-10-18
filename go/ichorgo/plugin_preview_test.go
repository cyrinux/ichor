package ichorgo

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestKubePluginPreviewRefusesABrokenPlugin(t *testing.T) {
	broken := "schemaVersion: ichor.dev/plugin/v1alpha1\nid: x\nbogus: 1\n"

	if _, err := KubePluginPreview("cfg", "ctx", "", broken, "", ""); err == nil || !strings.Contains(err.Error(), "plugin.yaml:3: unknown key") {
		t.Errorf("a broken plugin: %v", err)
	}
}
