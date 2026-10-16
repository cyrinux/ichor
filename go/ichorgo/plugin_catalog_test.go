package ichorgo

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/plugin"
)

// kubeGreenPlugin is the kube-green example of the plugin plan, kept as a fixture of the
// plugin package.
func kubeGreenPlugin(t *testing.T) string {
	t.Helper()

	src, err := os.ReadFile("../plugin/testdata/plugins/valid.yaml")
	if err != nil {
		t.Fatal(err)
	}

	return string(src)
}

func decodePluginValidation(t *testing.T, js string) pluginValidation {
	t.Helper()

	var v pluginValidation
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatalf("%v: %s", err, js)
	}

	return v
}

func TestPluginValidate(t *testing.T) {
	src := kubeGreenPlugin(t)

	js, err := PluginValidate(src)
	if err != nil {
		t.Fatal(err)
	}

	if v := decodePluginValidation(t, js); !v.OK || v.ID != "kube-green" || len(v.Problems) != 0 {
		t.Fatalf("kube-green: %s", js)
	}

	// One unknown key: refused, at the key's line.
	broken := strings.Replace(src, "    watch: false\n", "    watch: false\n    wtach: true\n", 1)
	line := strings.Count(broken[:strings.Index(broken, "wtach")], "\n") + 1

	js, _ = PluginValidate(broken)
	if v := decodePluginValidation(t, js); v.OK || len(v.Problems) != 1 || v.Problems[0].Line != line ||
		!strings.Contains(v.Problems[0].Msg, "unknown key wtach") {
		t.Fatalf("unknown key at line %d: %s", line, js)
	}

	// A validation error is reported with its path.
	js, _ = PluginValidate(strings.Replace(src, "health: idle", "health: sleepy", 1))
	if v := decodePluginValidation(t, js); v.OK || v.ID != "kube-green" || len(v.Problems) != 1 || v.Problems[0].Path != "health.sleepinfo[2].health" {
		t.Fatalf("bad enum: %s", js)
	}

	for name, hostile := range map[string]string{
		"empty":     "",
		"too large": strings.Repeat("#", plugin.MaxSize+1),
		"not yaml":  "{[",
		"a list":    "- 1\n",
	} {
		js, err := PluginValidate(hostile)
		if v := decodePluginValidation(t, js); err != nil || v.OK || len(v.Problems) == 0 {
			t.Errorf("%s: %s, %v", name, js, err)
		}
	}
}

func TestPluginCatalog(t *testing.T) {
	js, err := PluginCatalog()
	if err != nil || !strings.HasPrefix(js, `{"items":[`) {
		t.Fatalf("%s, %v", js, err)
	}

	p, err := plugin.Load("valid.yaml", []byte(kubeGreenPlugin(t)))
	if err != nil {
		t.Fatal(err)
	}

	c, err := plugin.Compile(p)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := toJSON(pluginCatalogOf([]*plugin.Compiled{c}))

	want := `{"items":[{"id":"kube-green","name":"kube-green","icon":"kube-green","website":"https://kube-green.dev",` +
		`"category":"data-services","screen":"","version":"ichor.dev/plugin/v1alpha1",` +
		`"resources":[{"group":"kube-green.com","version":"v1alpha1","resource":"sleepinfos","kind":"SleepInfo"},` +
		`{"group":"","version":"v1","resource":"secrets","kind":"Secret"}],` +
		`"actions":[{"id":"wakeNow","label":"Wake up now","verb":"mergePatch"}]}]}`
	if got != want {
		t.Fatalf("catalog\n got %s\nwant %s", got, want)
	}
}
