package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

// pluginRestoreResult is PluginUserRestore's answer.
type pluginRestoreResult struct {
	Restored []string `json:"restored"`
	Errors   []struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	} `json:"errors"`
}

func exportUserPlugins(t *testing.T) pluginUserExport {
	t.Helper()

	out, err := PluginUserExport()
	if err != nil {
		t.Fatal(err)
	}

	var got pluginUserExport
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return got
}

func restoreUserPlugins(t *testing.T, payload string) pluginRestoreResult {
	t.Helper()

	out, err := PluginUserRestore(payload)
	if err != nil {
		t.Fatal(err)
	}

	var got pluginRestoreResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return got
}

// Two plugins, one off, go into the backup with their files and flags, and come back so on
// another phone: the restored store lists them as they were.
func TestPluginUserExportRestoreRoundTrip(t *testing.T) {
	withPluginStore(t)

	on := kubeGreenCopy(t)
	off := strings.Replace(on, "\nid: kube-green-copy\n", "\nid: kube-green-off\n", 1)

	importUserPlugin(t, on)
	importUserPlugin(t, off)

	if err := PluginUserSetEnabled("kube-green-off", false); err != nil {
		t.Fatal(err)
	}

	exported := exportUserPlugins(t)
	if len(exported.Plugins) != 2 || exported.Plugins[0].ID != "kube-green-copy" || !exported.Plugins[0].Enabled ||
		exported.Plugins[1].ID != "kube-green-off" || exported.Plugins[1].Enabled || exported.Plugins[0].YAML != on {
		t.Fatalf("export %+v", exported)
	}

	payload, err := json.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}

	// A new phone: another data directory.
	withPluginStore(t)

	got := restoreUserPlugins(t, string(payload))
	if strings.Join(got.Restored, ",") != "kube-green-copy,kube-green-off" || len(got.Errors) != 0 {
		t.Fatalf("restore %+v", got)
	}

	list := userPluginList(t)
	if len(list) != 2 || !list[0].Enabled || list[1].Enabled || list[1].Error != "" {
		t.Fatalf("restored list %+v", list)
	}
}

// A plugin that no longer validates, one that takes a built-in's id and one with no file are
// reported by id, not stored; the others restore. Nothing at all is an empty answer.
func TestPluginUserRestoreReportsBadPlugins(t *testing.T) {
	withPluginStore(t)

	good := kubeGreenCopy(t)
	builtin := strings.Replace(good, "\nid: kube-green-copy\n", "\nid: kube-green\n", 1)

	payload, err := json.Marshal(pluginUserExport{Plugins: []pluginUserExportItem{
		{ID: "kube-green-copy", Enabled: true, YAML: good},
		{ID: "broken", Enabled: true, YAML: "schemaVersion: ichor.dev/plugin/v1alpha1\nid: broken\n"},
		{ID: "kube-green", Enabled: true, YAML: builtin},
		{ID: "empty", Enabled: false},
	}})
	if err != nil {
		t.Fatal(err)
	}

	got := restoreUserPlugins(t, string(payload))
	if strings.Join(got.Restored, ",") != "kube-green-copy" || len(got.Errors) != 3 {
		t.Fatalf("restore %+v", got)
	}

	for i, id := range []string{"broken", "kube-green", "empty"} {
		if got.Errors[i].ID != id || got.Errors[i].Error == "" {
			t.Errorf("error %d: %+v, want %s", i, got.Errors[i], id)
		}
	}

	if list := userPluginList(t); len(list) != 1 || list[0].ID != "kube-green-copy" {
		t.Fatalf("list %+v", list)
	}

	if got := restoreUserPlugins(t, `{"plugins":[]}`); len(got.Restored) != 0 || len(got.Errors) != 0 {
		t.Fatalf("empty restore %+v", got)
	}

	if _, err := PluginUserRestore("not json"); err == nil {
		t.Fatal("bad JSON restored")
	}
}

// With nothing imported the export is an empty list, not null.
func TestPluginUserExportEmpty(t *testing.T) {
	withPluginStore(t)

	out, err := PluginUserExport()
	if err != nil || out != `{"plugins":[]}` {
		t.Fatalf("got %s, %v", out, err)
	}
}
