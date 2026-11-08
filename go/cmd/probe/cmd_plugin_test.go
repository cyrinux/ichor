package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	builtinKubeGreen = "../../plugin/builtin/kube-green.yaml"
	strippedPlugin   = "testdata/plugins/kube-green-stripped.yaml"
	sleepInfoCRD     = "testdata/plugins/kube-green.com_sleepinfos.yaml"
)

// brokenCopy writes a copy of the built-in kube-green plugin with one change, and returns its
// path and the line of the change.
func brokenCopy(t *testing.T, old, replacement string) (string, int) {
	t.Helper()

	src, err := os.ReadFile(builtinKubeGreen)
	if err != nil {
		t.Fatal(err)
	}

	i := bytes.Index(src, []byte(old))
	if i < 0 {
		t.Fatalf("%q is not in the built-in plugin", old)
	}

	line := bytes.Count(src[:i], []byte("\n")) + 1
	path := filepath.Join(t.TempDir(), "kube-green.yaml")

	if err := os.WriteFile(path, bytes.Replace(src, []byte(old), []byte(replacement), 1), 0o600); err != nil {
		t.Fatal(err)
	}

	return path, line
}

func TestPluginCommandsReadNoConfig(t *testing.T) {
	for _, name := range []string{"plugin-validate", "plugin-lint", "plugin-render", "plugin-findings", "plugin-tool", "plugin-list", "plugin-from-crd"} {
		if c, ok := lookup(name); !ok || !c.noConfig {
			t.Errorf("%s: registered %v, noConfig %v", name, ok, c.noConfig)
		}
	}
}

func TestPluginValidate(t *testing.T) {
	unknownKey, unknownLine := brokenCopy(t, "catalogId: kube-green", "catalogue: kube-green")
	badExpr, exprLine := brokenCopy(t, `asleep: fields.operation == "SLEEP"`, `asleep: fields.operaton == "SLEEP"`)
	badSample, sampleLine := brokenCopy(t, `excludeCount: len($.spec.excludeRef)`, `excludeCount: len($.spec.excludeRef) + "x"`)

	cases := []struct {
		name    string
		files   []string
		reports int
		stderr  []string
	}{
		{"built-in", []string{builtinKubeGreen}, 1, nil},
		{"stripped", []string{strippedPlugin}, 1, nil},
		{"unknown key", []string{unknownKey}, 0, []string{unknownKey + ":" + strconv.Itoa(unknownLine) + ": unknown key catalogue"}},
		{"bad expression", []string{badExpr}, 0, []string{badExpr + ":" + strconv.Itoa(exprLine) + ": fields.sleepinfo.asleep: column 20: unknown fields.operaton"}},
		{"type error", []string{badSample}, 0, []string{badSample + ":" + strconv.Itoa(sampleLine) + ": fields.sleepinfo.excludeCount"}},
		{"one of two", []string{builtinKubeGreen, unknownKey}, 1, []string{unknownKey + ":" + strconv.Itoa(unknownLine) + ":"}},
		{"missing file", []string{"testdata/plugins/nope.yaml"}, 0, []string{"testdata/plugins/nope.yaml:0: open"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			err := validatePluginFiles(tc.files, &stdout, &stderr)
			if (err != nil) != (tc.stderr != nil) {
				t.Fatalf("error %v, stderr %q", err, stderr.String())
			}

			lines := strings.Fields(stdout.String())
			if len(lines) != tc.reports {
				t.Fatalf("stdout %q", stdout.String())
			}

			for _, line := range lines {
				var r pluginReport
				if err := json.Unmarshal([]byte(line), &r); err != nil || !r.OK || r.ID == "" || r.Cards == 0 || r.Resources == 0 {
					t.Errorf("report %s (%v)", line, err)
				}
			}

			for _, want := range tc.stderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr %q, want %q", stderr.String(), want)
				}
			}
		})
	}
}

func TestPluginValidateReport(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := validatePluginFiles([]string{builtinKubeGreen}, &stdout, &stderr); err != nil {
		t.Fatal(err, stderr.String())
	}

	want := `{"ok":true,"id":"kube-green","cards":2,"findings":1,"resources":2,"actions":0}` + "\n"
	if stdout.String() != want || stderr.Len() != 0 {
		t.Errorf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
}

func TestPluginLint(t *testing.T) {
	broken, _ := brokenCopy(t, `asleep: fields.operation == "SLEEP"`, `asleep: fields.operaton == "SLEEP"`)

	cases := []struct {
		name     string
		args     []string
		fail     bool
		warnings int
		stderr   []string
		summary  string
	}{
		{"with the CRD", []string{strippedPlugin, "-crd", sleepInfoCRD}, false, 4, []string{
			strippedPlugin + ":13: warning: resources.sleepinfo: printer column \"Wake Up At\" (.spec.wakeUpAt) is not used",
			"printer column \"Last Schedule\" (.status.lastScheduleTime)",
			"no i18n: the 5 labels stay English",
			"card list has no empty text",
		}, ""},
		{"flags first", []string{"--crd", sleepInfoCRD, strippedPlugin}, false, 4, []string{"Wake Up At"}, ""},
		{"without the CRD", []string{strippedPlugin}, false, 2, []string{"no i18n"}, ""},
		{"strict", []string{strippedPlugin, "--strict"}, true, 2, []string{"no empty text"}, "plugin-lint: 0 errors, 2 warnings (-strict)"},
		// The built-in plugin shows every printer column; two of its rules have no sample yet.
		{"built-in", []string{builtinKubeGreen, "-crd", sleepInfoCRD}, false, 2, []string{"health.sleepinfo[0]: no sample triggers this rule"}, ""},
		{"broken", []string{broken}, true, -1, []string{"unknown fields.operaton"}, "plugin-lint: 1 error, 0 warnings"},
		{"bad CRD", []string{strippedPlugin, "-crd", strippedPlugin}, true, -1, nil, ""},
		{"no file", []string{"-strict"}, true, -1, nil, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			err := lintPluginFile(tc.args, &stdout, &stderr)
			if (err != nil) != tc.fail {
				t.Fatalf("error %v, stderr %q", err, stderr.String())
			}

			if err != nil && tc.summary != "" && err.Error() != tc.summary {
				t.Errorf("summary %q, want %q", err, tc.summary)
			}

			if tc.warnings >= 0 {
				var r pluginReport
				if err := json.Unmarshal(stdout.Bytes(), &r); err != nil || r.Warnings == nil || *r.Warnings != tc.warnings {
					t.Errorf("report %q (%v), want %d warnings; stderr %q", stdout.String(), err, tc.warnings, stderr.String())
				}
			}

			for _, want := range tc.stderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr %q, want %q", stderr.String(), want)
				}
			}
		})
	}
}

func TestPluginRenderDemo(t *testing.T) {
	broken, line := brokenCopy(t, "catalogId: kube-green", "catalogue: kube-green")
	// The demo reads no talosconfig: a read of this path would exit the test binary.
	e := env{configPath: filepath.Join(t.TempDir(), "no-talosconfig")}

	for _, target := range []string{"kube-green", builtinKubeGreen, strippedPlugin} {
		out, err := pluginRender(e, []string{target, "-demo", "-locale", "fr"})
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}

		var tree struct {
			Plugin      struct{ ID string }
			DetectedVia string
			Cards       []struct{ Rows []any }
		}
		if err := json.Unmarshal([]byte(out), &tree); err != nil || tree.DetectedVia != "demo" || len(tree.Cards) == 0 || len(tree.Cards[0].Rows) == 0 {
			t.Errorf("%s: %v %s", target, err, out)
		}
	}

	// Validated before any config is read, with or without -demo.
	for _, args := range [][]string{{broken, "-demo"}, {broken}} {
		if _, err := pluginRender(e, args); err == nil || !strings.Contains(err.Error(), broken+":"+strconv.Itoa(line)+": unknown key") {
			t.Errorf("a broken file %v: %v", args, err)
		}
	}

	if _, err := pluginRender(e, []string{"-demo"}); err == nil {
		t.Error("no target accepted")
	}
}

func TestPluginTool(t *testing.T) {
	// No config is read for a built-in's unknown tool: a read of this path would exit the test binary.
	e := env{configPath: filepath.Join(t.TempDir(), "no-talosconfig")}

	for _, args := range [][]string{{"kube-green", "nothing"}, {"kube-green", "nothing", "-demo"}} {
		if _, err := pluginTool(e, args); err == nil || !strings.Contains(err.Error(), `unknown tool "nothing"`) {
			t.Errorf("%v: %v", args, err)
		}
	}

	for _, args := range [][]string{{}, {"kube-green"}, {"kube-green", "check", "extra"}} {
		if _, err := pluginTool(e, args); err == nil || !strings.Contains(err.Error(), "usage: plugin-tool") {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestPluginList(t *testing.T) {
	out, err := pluginList()
	if err != nil {
		t.Fatal(err)
	}

	var found bool

	for _, line := range strings.Split(out, "\n") {
		var item pluginListItem
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			t.Fatalf("%v: %q", err, line)
		}

		if item.ID == "kube-green" {
			found = item.Resources == 2 && item.Version == "ichor.dev/plugin/v1alpha1" && item.File == "go/plugin/builtin/kube-green.yaml"
		}
	}

	if !found {
		t.Errorf("kube-green is not listed as expected: %s", out)
	}
}

func TestArgPath(t *testing.T) {
	t.Setenv("ICHOR_PROBE_CWD", "/repo")

	if got := argPath("go/plugin/builtin/kube-green.yaml"); got != "/repo/go/plugin/builtin/kube-green.yaml" {
		t.Errorf("relative: %s", got)
	}

	if got := argPath("/abs/p.yaml"); got != "/abs/p.yaml" {
		t.Errorf("absolute: %s", got)
	}
}
