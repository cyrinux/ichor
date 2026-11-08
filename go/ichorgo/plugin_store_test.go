package ichorgo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// withPluginStore gives the test an empty data directory and a fixed clock.
func withPluginStore(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	SetDataDir(dir, testDataKey)
	t.Cleanup(func() { SetDataDir("", nil) })

	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	pluginNow = func() time.Time { return at }
	t.Cleanup(func() { pluginNow = time.Now })

	return dir
}

// kubeGreenCopy is the built-in kube-green plugin under another id: what a user would import.
func kubeGreenCopy(t *testing.T) string {
	t.Helper()

	src, err := os.ReadFile("../plugin/builtin/kube-green.yaml")
	if err != nil {
		t.Fatal(err)
	}

	return strings.Replace(string(src), "\nid: kube-green\n", "\nid: kube-green-copy\n", 1)
}

func importUserPlugin(t *testing.T, src string) pluginUserItem {
	t.Helper()

	return decodeUserItem(t)(PluginUserImport(src))
}

func checkUserPlugin(t *testing.T, src string) pluginUserItem {
	t.Helper()

	return decodeUserItem(t)(PluginUserCheck(src))
}

func decodeUserItem(t *testing.T) func(string, error) pluginUserItem {
	return func(out string, err error) pluginUserItem {
		t.Helper()

		if err != nil {
			t.Fatal(err)
		}

		var item pluginUserItem
		if err := json.Unmarshal([]byte(out), &item); err != nil {
			t.Fatalf("%v: %s", err, out)
		}

		return item
	}
}

func userPluginList(t *testing.T) []pluginUserItem {
	t.Helper()

	out, err := PluginUserList()
	if err != nil {
		t.Fatal(err)
	}

	var list pluginUserList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	return list.Items
}

func knownIDs() []string {
	var ids []string
	for _, c := range knownPlugins() {
		ids = append(ids, c.Plugin.ID)
	}

	return ids
}

func TestPluginStoreRoundTrip(t *testing.T) {
	dir := withPluginStore(t)

	// The check: the consent entry, nothing stored yet.
	item := checkUserPlugin(t, kubeGreenCopy(t))
	if item.ID != "kube-green-copy" || item.Enabled || item.Replaces || !item.User || item.Error != "" {
		t.Fatalf("checked: %+v", item)
	}

	// What it reads, the Secret masked, and no action.
	var resources []string
	for _, r := range item.Resources {
		resources = append(resources, fmt.Sprintf("%s/%s reveal=%v", r.Group, r.Resource, r.Reveal))
	}

	if want := []string{"kube-green.com/sleepinfos reveal=false", "/secrets reveal=false"}; !slices.Equal(resources, want) {
		t.Fatalf("resources %v, want %v", resources, want)
	}

	if len(item.Actions) != 0 {
		t.Fatalf("actions: %+v", item.Actions)
	}

	// Only checked: nothing stored, the cluster calls do not know it.
	if slices.Contains(knownIDs(), "kube-green-copy") || len(userPluginList(t)) != 0 {
		t.Fatal("a checked plugin was stored")
	}

	// The consent sheet's Enable: stored, on.
	item = importUserPlugin(t, kubeGreenCopy(t))
	if !item.Enabled || len(item.SHA256) != 64 || item.ImportedAt == 0 {
		t.Fatalf("imported: %+v", item)
	}

	if !slices.Contains(knownIDs(), "kube-green-copy") {
		t.Fatalf("enabled plugin is not known: %v", knownIDs())
	}

	// The file is sealed, under its id.
	raw, err := os.ReadFile(filepath.Join(dir, "plugins", "kube-green-copy.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(raw), "schemaVersion") {
		t.Fatal("the plugin file is stored in clear")
	}

	// A restart: the store is read again from the directory.
	SetDataDir("", nil)
	SetDataDir(dir, testDataKey)

	list := userPluginList(t)
	if len(list) != 1 || list[0].ID != "kube-green-copy" || !list[0].Enabled || list[0].Name != "kube-green" {
		t.Fatalf("after restart: %+v", list)
	}

	if !slices.Contains(knownIDs(), "kube-green-copy") {
		t.Fatal("enabled plugin lost on restart")
	}

	// The catalog lists it as a user plugin, after the built-ins.
	js, err := PluginCatalog()
	if err != nil {
		t.Fatal(err)
	}

	var catalog pluginCatalog
	if err := json.Unmarshal([]byte(js), &catalog); err != nil {
		t.Fatal(err)
	}

	last := catalog.Items[len(catalog.Items)-1]
	if last.ID != "kube-green-copy" || !last.User || catalog.Items[0].User {
		t.Fatalf("catalog: %s", js)
	}

	if err := PluginUserSetEnabled("kube-green-copy", false); err != nil {
		t.Fatal(err)
	}

	if slices.Contains(knownIDs(), "kube-green-copy") {
		t.Fatal("disabled plugin still known")
	}

	// Turned back on: it was consented to as stored.
	if err := PluginUserSetEnabled("kube-green-copy", true); err != nil || !slices.Contains(knownIDs(), "kube-green-copy") {
		t.Fatalf("enable again: %v", err)
	}

	if err := PluginUserRemove("kube-green-copy"); err != nil {
		t.Fatal(err)
	}

	if list := userPluginList(t); len(list) != 0 {
		t.Fatalf("after remove: %+v", list)
	}

	if _, err := os.Stat(filepath.Join(dir, "plugins", "kube-green-copy.yaml")); !os.IsNotExist(err) {
		t.Fatalf("the file is left: %v", err)
	}
}

func TestPluginStoreReimportReplaces(t *testing.T) {
	dir := withPluginStore(t)

	src := kubeGreenCopy(t)
	importUserPlugin(t, src)

	if err := PluginUserSetEnabled("kube-green-copy", false); err != nil {
		t.Fatal(err)
	}

	// The same id with other content: the check says it replaces one, and stores nothing (the
	// consent sheet's Cancel keeps the stored plugin as it is).
	changed := strings.Replace(src, "name: kube-green\n", "name: kube-green (copy)\n", 1)
	if item := checkUserPlugin(t, changed); !item.Replaces || item.Name != "kube-green (copy)" {
		t.Fatalf("check: %+v", item)
	}

	if list := userPluginList(t); len(list) != 1 || list[0].Name != "kube-green" || list[0].Enabled {
		t.Fatalf("after check: %+v", list)
	}

	// Enable: the new content, on.
	if item := importUserPlugin(t, changed); !item.Enabled || item.Name != "kube-green (copy)" {
		t.Fatalf("re-import: %+v", item)
	}

	files, _ := os.ReadDir(filepath.Join(dir, "plugins"))
	if len(files) != 1 || files[0].Name() != "kube-green-copy.yaml" {
		t.Fatalf("files: %v", files)
	}

	// After a restart the stored file matches its index.
	SetDataDir("", nil)
	SetDataDir(dir, testDataKey)

	if list := userPluginList(t); len(list) != 1 || !list[0].Enabled || list[0].Error != "" || list[0].Name != "kube-green (copy)" {
		t.Fatalf("after restart: %+v", list)
	}
}

func TestPluginStoreRefuses(t *testing.T) {
	withPluginStore(t)

	src := kubeGreenCopy(t)

	cases := []struct {
		name, yaml, want string
	}{
		{"built-in id", strings.Replace(src, "id: kube-green-copy", "id: kube-green", 1), "built-in"},
		{"unknown key", strings.Replace(src, "    scope: Namespaced\n", "    scope: Namespaced\n    wtach: true\n", 1), "plugin.yaml:"},
		{"other schema version", strings.Replace(src, "ichor.dev/plugin/v1alpha1", "ichor.dev/plugin/v2", 1), "not supported"},
		{"too large", src + "\n#" + strings.Repeat("x", plugin.MaxSize), "1 MiB"},
		{"alias bomb", "a: &a [x, x, x, x, x, x, x, x, x]\nb: &b [*a, *a, *a, *a, *a, *a, *a, *a, *a]\nc: &c [*b, *b, *b, *b, *b, *b, *b, *b, *b]\nd: [*c, *c, *c, *c, *c, *c, *c, *c, *c]\n", "plugin.yaml"},
		{"not yaml", "\x00\x01", "plugin.yaml"},
		{"id the store cannot name", strings.Replace(src, "id: kube-green-copy", "id: -copy", 1), "cannot be imported"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, call := range []func(string) (string, error){PluginUserCheck, PluginUserImport} {
				_, err := call(tc.yaml)
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("err %v, want %q", err, tc.want)
				}
			}
		})
	}

	if list := userPluginList(t); len(list) != 0 {
		t.Fatalf("a refused plugin was stored: %+v", list)
	}

	for _, id := range []string{"../etc", "Kube", "", "a/b"} {
		if err := PluginUserRemove(id); err == nil {
			t.Fatalf("remove %q: no error", id)
		}

		if err := PluginUserSetEnabled(id, true); err == nil {
			t.Fatalf("enable %q: no error", id)
		}
	}

	if err := PluginUserSetEnabled("never-imported", true); err == nil {
		t.Fatal("enable unknown: no error")
	}
}

func TestPluginStoreLimit(t *testing.T) {
	withPluginStore(t)

	src := kubeGreenCopy(t)
	for i := range maxUserPlugins {
		importUserPlugin(t, strings.Replace(src, "id: kube-green-copy", fmt.Sprintf("id: copy-%d", i), 1))
	}

	if _, err := PluginUserCheck(strings.Replace(src, "id: kube-green-copy", "id: one-too-many", 1)); err == nil || !strings.Contains(err.Error(), "64") {
		t.Fatalf("over the limit: %v", err)
	}

	// Replacing one already there is not a new one.
	importUserPlugin(t, strings.Replace(src, "id: kube-green-copy", "id: copy-3", 1))
}

func TestPluginStoreWithoutDataDir(t *testing.T) {
	SetDataDir("", nil)

	if _, err := PluginUserImport(kubeGreenCopy(t)); err == nil {
		t.Fatal("import without a data directory: no error")
	}

	if list := userPluginList(t); len(list) != 0 {
		t.Fatalf("list: %+v", list)
	}
}

// A stored plugin this app no longer reads (another schemaVersion, a built-in that took its id,
// a file changed on disk) is listed off with the reason, and never loaded.
func TestPluginStoreRevalidatesOnLoad(t *testing.T) {
	dir := withPluginStore(t)

	importUserPlugin(t, kubeGreenCopy(t))

	// The file now holds another schema version (as a newer app could have written it).
	_, aead := dataStore()
	other := strings.Replace(kubeGreenCopy(t), "ichor.dev/plugin/v1alpha1", "ichor.dev/plugin/v2", 1)

	if err := writeSealed(filepath.Join(dir, "plugins", "kube-green-copy.yaml"), []byte(other), aead); err != nil {
		t.Fatal(err)
	}

	SetDataDir("", nil)
	SetDataDir(dir, testDataKey)

	list := userPluginList(t)
	if len(list) != 1 || list[0].Enabled || !strings.Contains(list[0].Error, "changed") {
		t.Fatalf("tampered: %+v", list)
	}

	if slices.Contains(knownIDs(), "kube-green-copy") {
		t.Fatal("a tampered plugin is known")
	}

	if err := PluginUserSetEnabled("kube-green-copy", true); err == nil {
		t.Fatal("a broken plugin was enabled")
	}

	// Imported again with the new version under the same sha: it is refused at import.
	if _, err := PluginUserImport(other); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("import v2: %v", err)
	}
}

func TestUserPluginProblem(t *testing.T) {
	builtin := map[string]bool{"kube-green": true}

	if msg := userPluginProblem([]byte(kubeGreenCopy(t)), builtin); msg != "" {
		t.Fatalf("valid: %q", msg)
	}

	src := strings.Replace(kubeGreenCopy(t), "ichor.dev/plugin/v1alpha1", "ichor.dev/plugin/v9", 1)
	if msg := userPluginProblem([]byte(src), builtin); !strings.Contains(msg, "not supported") {
		t.Fatalf("v9: %q", msg)
	}

	src = strings.Replace(kubeGreenCopy(t), "id: kube-green-copy", "id: kube-green", 1)
	if msg := userPluginProblem([]byte(src), builtin); !strings.Contains(msg, "built-in") {
		t.Fatalf("built-in id: %q", msg)
	}
}

func TestPluginUserActionsJoinAccessChecks(t *testing.T) {
	withPluginStore(t)

	yaml := strings.Replace(kubeGreenPlugin(t), "\nid: kube-green\n", "\nid: es-copy\n", 1)
	item := importUserPlugin(t, yaml)

	if len(item.Actions) == 0 || len(item.Actions[0].RBAC) == 0 {
		t.Fatalf("actions: %+v", item.Actions)
	}

	key := pluginActionKey("es-copy", item.Actions[0].ID)
	if _, ok := actionChecks()[key]; !ok {
		t.Fatalf("no can-I row for %s", key)
	}

	// A cached answer from before the change is asked again.
	actionAccess.entries["stale"] = accessEntry{at: time.Now()}

	if err := PluginUserSetEnabled("es-copy", false); err != nil {
		t.Fatal(err)
	}

	if _, ok := actionChecks()[key]; ok {
		t.Fatal("a disabled plugin's action is checked")
	}

	if _, ok := actionAccess.entries["stale"]; ok {
		t.Fatal("the can-I answers were kept")
	}

	if err := PluginUserSetEnabled("es-copy", true); err != nil {
		t.Fatal(err)
	}

	if _, ok := kubeActionChecks[key]; ok {
		t.Fatal("the user plugin's row was written into the shared table")
	}

	if _, ok := allowAll("").Actions[key]; !ok {
		t.Fatal("the demo answer misses the user plugin's action")
	}
}

func TestPluginFetchURL(t *testing.T) {
	body := kubeGreenCopy(t)

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plugin.yaml":
			_, _ = w.Write([]byte(body))
		case "/big.yaml":
			_, _ = w.Write([]byte(strings.Repeat("#", plugin.MaxSize+1)))
		case "/plain":
			http.Redirect(w, r, "http://example.com/plugin.yaml", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	pluginFetchClient = srv.Client
	t.Cleanup(func() { pluginFetchClient = defaultPluginFetchClient })

	got, err := PluginFetchURL(srv.URL + "/plugin.yaml")
	if err != nil || got != body {
		t.Fatalf("fetch: %v (%d bytes)", err, len(got))
	}

	cases := []struct{ url, want string }{
		{"http://example.com/plugin.yaml", "https"},
		{"file:///etc/passwd", "https"},
		{"https://", "https"},
		{srv.URL + "/big.yaml", "1 MiB"},
		{srv.URL + "/missing.yaml", "404"},
		{srv.URL + "/plain", "https"},
	}

	for _, tc := range cases {
		if _, err := PluginFetchURL(tc.url); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err %v, want %q", tc.url, err, tc.want)
		}
	}
}

// The list the Settings screens decode, kept as a golden the Android and iOS tests read: one
// enabled plugin with an action and a revealed Secret, one stored file this app no longer reads.
func TestPluginUserListMatchesGolden(t *testing.T) {
	dir := withPluginStore(t)

	revealing := strings.Replace(kubeGreenPlugin(t), "    reveal: false\n", "    reveal: true\n", 1)
	importUserPlugin(t, strings.Replace(revealing, "\nid: kube-green\n", "\nid: sleep-copy\n", 1))

	if err := PluginUserSetEnabled("sleep-copy", true); err != nil {
		t.Fatal(err)
	}

	other := strings.Replace(kubeGreenCopy(t), "id: kube-green-copy", "id: old-format", 1)
	importUserPlugin(t, other)

	// A newer app wrote it in another format; this one re-validates it at launch.
	_, aead := dataStore()
	newer := strings.Replace(other, "ichor.dev/plugin/v1alpha1", "ichor.dev/plugin/v2", 1)

	if err := writeSealed(filepath.Join(dir, "plugins", "old-format.yaml"), []byte(newer), aead); err != nil {
		t.Fatal(err)
	}

	if err := writeUserIndexSHA(dir, "old-format", newer); err != nil {
		t.Fatal(err)
	}

	SetDataDir("", nil)
	SetDataDir(dir, testDataKey)

	out, err := PluginUserList()
	if err != nil {
		t.Fatal(err)
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(out), "", "  "); err != nil {
		t.Fatal(err)
	}

	pretty.WriteByte('\n')

	path := filepath.Join("..", "plugin", "testdata", "user", "plugin-user-list.json")
	if *updatePluginGolden {
		if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	} else if want, err := os.ReadFile(path); err != nil || !bytes.Equal(want, pretty.Bytes()) {
		t.Errorf("%s differs (go test ./ichorgo -run PluginUserListMatchesGolden -update-plugin-golden); got:\n%s", path, pretty.Bytes())
	}

	if !strings.Contains(pretty.String(), "not supported") {
		t.Fatalf("the newer format is not explained: %s", pretty.String())
	}
}

// writeUserIndexSHA sets the index's sha256 of id to src's, as if src had been imported.
func writeUserIndexSHA(dir, id, src string) error {
	userPlugins.Lock()
	defer userPlugins.Unlock()

	items := slices.Clone(loadUserPluginsLocked())
	for i := range items {
		if items[i].entry.ID == id {
			items[i].entry.SHA256 = sha256Hex([]byte(src))
		}
	}

	return writeUserPluginsLocked(items)
}
