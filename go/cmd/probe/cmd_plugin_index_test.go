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

// indexRepo is a repository root with one plugin file in drafts/: the path the index names.
func indexRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	t.Setenv("ICHOR_PROBE_CWD", root)

	if err := os.MkdirAll(filepath.Join(root, "drafts"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeDraft(t, root, "demo-sleep", "")

	return root
}

// writeDraft writes the kube-green built-in as id, with extra lines after its id.
func writeDraft(t *testing.T, root, id, extra string) {
	t.Helper()

	src, err := os.ReadFile(filepath.Join("..", "..", "plugin", "builtin", "kube-green.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	text := strings.Replace(string(src), "id: kube-green\n", "id: "+id+"\n"+extra, 1)
	if err := os.WriteFile(filepath.Join(root, "drafts", id+".yaml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readIndex(t *testing.T, root string) plugin.Index {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, "plugins", "index.json"))
	if err != nil {
		t.Fatal(err)
	}

	var index plugin.Index
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}

	return index
}

func TestPluginIndexWritesEveryDraft(t *testing.T) {
	root := indexRepo(t)
	writeDraft(t, root, "demo-extra", "description: Sleep schedules\nmaintainer: demo-maintainer\n")

	var out bytes.Buffer
	if err := pluginIndex([]string{"drafts"}, &out); err != nil {
		t.Fatal(err)
	}

	index := readIndex(t, root)
	if index.SchemaVersion != plugin.IndexSchemaVersion || index.GeneratedAt == "" || len(index.Plugins) != 2 {
		t.Fatalf("index: %+v", index)
	}

	extra, sleep := index.Plugins[0], index.Plugins[1]
	if extra.ID != "demo-extra" || sleep.ID != "demo-sleep" {
		t.Fatalf("not sorted by id: %+v", index.Plugins)
	}

	if extra.Description != "Sleep schedules" || extra.Maintainer != "demo-maintainer" {
		t.Fatalf("file's own words lost: %+v", extra)
	}

	if sleep.Maintainer != defaultIndexMaintainer {
		t.Fatalf("maintainer: %q", sleep.Maintainer)
	}

	wantURL := "https://raw.githubusercontent.com/cyrinux/ichor/main/drafts/demo-sleep.yaml"
	if sleep.URL != wantURL {
		t.Fatalf("url %q, want %q", sleep.URL, wantURL)
	}

	src, err := os.ReadFile(filepath.Join(root, "drafts", "demo-sleep.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if sleep.SHA256 != plugin.SHA256Hex(src) || len(sleep.Resources) == 0 {
		t.Fatalf("entry: %+v", sleep)
	}

	if !strings.Contains(out.String(), `"plugins":2`) {
		t.Fatalf("report: %s", out.String())
	}
}

func TestPluginIndexRef(t *testing.T) {
	root := indexRepo(t)

	if err := pluginIndex([]string{"-ref", "v1.2.3", "drafts"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	if got := readIndex(t, root).Plugins[0].URL; !strings.Contains(got, "/ichor/v1.2.3/drafts/") {
		t.Fatalf("url %q", got)
	}
}

func TestPluginIndexEmptyDirectoryIsAnEmptyIndex(t *testing.T) {
	root := indexRepo(t)

	if err := os.Remove(filepath.Join(root, "drafts", "demo-sleep.yaml")); err != nil {
		t.Fatal(err)
	}

	if err := pluginIndex([]string{"drafts"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, "plugins", "index.json"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(data), `"plugins": []`) {
		t.Fatalf("index: %s", data)
	}
}

// Writing twice changes nothing, generatedAt included, so -check can compare the file as it is.
func TestPluginIndexCheckFailsWhenStale(t *testing.T) {
	root := indexRepo(t)

	if err := pluginIndex([]string{"-check", "drafts"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "just plugin-index") {
		t.Fatalf("missing file: %v", err)
	}

	if err := pluginIndex([]string{"drafts"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "plugins", "index.json")

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	stamped := strings.Replace(string(before), readIndex(t, root).GeneratedAt, "2020-01-01T00:00:00Z", 1)
	if err := os.WriteFile(path, []byte(stamped), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := pluginIndex([]string{"drafts"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	if readIndex(t, root).GeneratedAt != "2020-01-01T00:00:00Z" {
		t.Fatal("an unchanged list got a new generatedAt")
	}

	if err := pluginIndex([]string{"-check", "drafts"}, &bytes.Buffer{}); err != nil {
		t.Fatalf("fresh: %v", err)
	}

	writeDraft(t, root, "demo-extra", "")

	if err := pluginIndex([]string{"-check", "drafts"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("new draft: %v", err)
	}

	if len(readIndex(t, root).Plugins) != 1 {
		t.Fatal("-check wrote the file")
	}

	// A changed file changes its digest: stale as well.
	if err := pluginIndex([]string{"drafts"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	writeDraft(t, root, "demo-extra", "description: changed\n")

	if err := pluginIndex([]string{"-check", "drafts"}, &bytes.Buffer{}); err == nil {
		t.Fatal("a changed file is not stale")
	}
}

func TestPluginIndexRefuses(t *testing.T) {
	root := indexRepo(t)

	if err := pluginIndex(nil, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage") {
		t.Fatalf("no directory: %v", err)
	}

	if err := pluginIndex([]string{"../elsewhere"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "repository") {
		t.Fatalf("outside the repository: %v", err)
	}

	if err := pluginIndex([]string{"drafts", "drafts"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("same id twice: %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "drafts", "broken.yaml"), []byte("id: broken\nnonsense: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := pluginIndex([]string{"drafts"}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "broken.yaml") {
		t.Fatalf("a broken file: %v", err)
	}
}
