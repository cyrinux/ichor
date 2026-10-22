package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// untranslatedCopy writes the built-in kube-green plugin without its i18n section, the last
// of the file with the comment above it.
func untranslatedCopy(t *testing.T) string {
	t.Helper()

	src, err := os.ReadFile(builtinKubeGreen)
	if err != nil {
		t.Fatal(err)
	}

	i := bytes.Index(src, []byte("\ni18n:\n"))
	if i < 0 {
		t.Fatal("the built-in plugin has no i18n section")
	}

	if j := bytes.LastIndex(src[:i], []byte("\n#")); j >= 0 && !bytes.Contains(src[j:i], []byte("\n\n")) {
		i = j
	}

	path := filepath.Join(t.TempDir(), "kube-green.yaml")
	if err := os.WriteFile(path, append(bytes.TrimRight(src[:i], "\n"), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func runI18n(t *testing.T, args ...string) (i18nReport, string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	err := pluginI18n(args, &stdout, &stderr)

	var report i18nReport
	if stdout.Len() > 0 {
		if jsonErr := json.Unmarshal(stdout.Bytes(), &report); jsonErr != nil {
			t.Fatalf("stdout %q: %v", stdout.String(), jsonErr)
		}
	}

	return report, stderr.String(), err
}

func TestPluginI18nFillsFromTheGlossaryThenTheAnswer(t *testing.T) {
	path := untranslatedCopy(t)

	// Without the glossary every label of every language is missing, and nothing is written.
	before, _ := os.ReadFile(path)
	report, _, err := runI18n(t, path, "-no-glossary", "-dry-run")
	if err != nil || report.Filled != 0 || len(report.Missing) != 5 || len(report.Missing["fr"]) < 10 {
		t.Fatalf("dry run: %+v %v", report, err)
	}

	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("-dry-run rewrote the file")
	}

	// The glossary knows every label: kube-green itself is a built-in plugin.
	report, _, err = runI18n(t, path)
	if err != nil || report.Filled == 0 || len(report.Missing) != 0 {
		t.Fatalf("glossary: %+v %v", report, err)
	}

	after, _ := os.ReadFile(path)
	if !bytes.Contains(after, []byte("\ni18n:\n  fr:\n")) || !bytes.Contains(after, []byte("\n# Invented names.")) {
		t.Fatalf("rewritten file:\n%s", after)
	}

	// A run on a translated file changes nothing.
	report, _, err = runI18n(t, path)
	if err != nil || report.Filled != 0 {
		t.Fatalf("second run: %+v %v", report, err)
	}

	if again, _ := os.ReadFile(path); !bytes.Equal(after, again) {
		t.Fatal("a second run rewrote the file")
	}
}

func TestPluginI18nAppliesAndChecksAnAnswer(t *testing.T) {
	path := untranslatedCopy(t)
	dir := filepath.Dir(path)

	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		return p
	}

	good := write("good.json", `{"fr": {"Weekdays": "Jours de semaine"}}`)
	report, _, err := runI18n(t, path, "-no-glossary", "-fill", good)
	if err != nil || report.Filled != 1 || len(report.Missing["fr"]) == 0 {
		t.Fatalf("good answer: %+v %v", report, err)
	}

	// The flags may come before the file too.
	if _, _, err := runI18n(t, "-dry-run", "-fill", good, path); err != nil {
		t.Fatal(err)
	}

	after, _ := os.ReadFile(path)
	if !bytes.Contains(after, []byte(`"Weekdays": "Jours de semaine"`)) {
		t.Fatalf("not applied:\n%s", after)
	}

	cases := []struct{ name, content, want string }{
		{"bad.json", `["x"]`, "not a JSON object"},
		{"locale.json", `{"xx": {"Weekdays": "x"}}`, "not one of the apps' languages"},
		{"label.json", `{"fr": {"Nope": "Non"}}`, "not a label of the plugin"},
		{"hole.json", `{"fr": {"%d sleep schedule": "un planning de veille"}}`, "the holes differ"},
	}

	for _, c := range cases {
		_, _, err := runI18n(t, path, "-no-glossary", "-fill", write(c.name, c.content))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", c.name, err, c.want)
		}
	}
}

func TestPluginI18nIsRegistered(t *testing.T) {
	c, ok := lookup("plugin-i18n")
	if !ok || !c.noConfig {
		t.Fatalf("registered %v, noConfig %v", ok, c.noConfig)
	}
}
