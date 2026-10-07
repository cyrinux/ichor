package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// testSchema is a small schema shaped like Talos's: two documents, refs, a map, a list.
const testSchema = `{
  "$defs": {
    "v1alpha1.Config": {"type": "object", "additionalProperties": false, "properties": {
      "version": {"enum": ["v1alpha1"], "title": "version", "description": "Schema version.\n"},
      "debug": {"type": "boolean", "title": "debug", "description": "Verbose logging.\n"},
      "machine": {"$ref": "#/$defs/v1alpha1.MachineConfig"}
    }},
    "v1alpha1.MachineConfig": {"type": "object", "additionalProperties": false, "properties": {
      "type": {"enum": ["controlplane", "worker"], "title": "type", "description": "Role of the node.\n"},
      "token": {"type": "string", "title": "token"},
      "certSANs": {"type": "array", "items": {"type": "string"}, "title": "certSANs"},
      "sysctls": {"type": "object", "patternProperties": {".*": {"type": "string"}}, "title": "sysctls"},
      "install": {"$ref": "#/$defs/v1alpha1.InstallConfig", "title": "install", "description": "How Talos is installed.\n"}
    }},
    "v1alpha1.InstallConfig": {"type": "object", "additionalProperties": false, "properties": {
      "disk": {"type": "string", "title": "disk"},
      "wipe": {"type": "boolean", "title": "wipe"}
    }},
    "network.HostnameConfigV1Alpha1": {"type": "object", "additionalProperties": false, "properties": {
      "apiVersion": {"enum": ["v1alpha1"], "title": "apiVersion"},
      "kind": {"enum": ["HostnameConfig"], "title": "kind"},
      "auto": {"enum": ["stable", "off"], "title": "auto", "description": "How the hostname is generated.\n"},
      "hostname": {"type": "string", "title": "hostname"}
    }}
  },
  "oneOf": [{"$ref": "#/$defs/network.HostnameConfigV1Alpha1"}, {"$ref": "#/$defs/v1alpha1.Config"}]
}`

func mustTestSchema(t *testing.T) *configSchema {
	t.Helper()

	schema, err := parseConfigSchema([]byte(testSchema))
	if err != nil {
		t.Fatal(err)
	}

	return schema
}

// schemaServer serves body for any version and counts the requests.
func schemaServer(t *testing.T, status int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body)) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	return srv, &hits
}

func testSchemaStore(srv *httptest.Server, dir string) *schemaStore {
	return newSchemaStore(func() string { return dir }, func(v string) string { return srv.URL + "/" + v }, srv.Client())
}

func TestSchemaIsFetchedOnce(t *testing.T) {
	srv, hits := schemaServer(t, http.StatusOK, testSchema)
	dir := t.TempDir()
	store := testSchemaStore(srv, dir)

	if store.cached("v1.13.6") != nil {
		t.Fatal("nothing is cached before the first fetch")
	}

	_, source, err := store.prepare(context.Background(), "v1.13.6")
	if err != nil || source != schemaSourceNetwork {
		t.Fatalf("first prepare: %q %v", source, err)
	}

	_, source, err = store.prepare(context.Background(), "v1.13.6")
	if err != nil || source != schemaSourceMemory {
		t.Fatalf("second prepare: %q %v", source, err)
	}

	// A new launch: memory is empty, the file is there.
	relaunched := testSchemaStore(srv, dir)

	_, source, err = relaunched.prepare(context.Background(), "v1.13.6")
	if err != nil || source != schemaSourceDisk {
		t.Fatalf("after relaunch: %q %v", source, err)
	}

	if testSchemaStore(srv, dir).cached("v1.13.6") == nil {
		t.Fatal("cached must read the file without the network")
	}

	if n := hits.Load(); n != 1 {
		t.Fatalf("the schema was fetched %d times, want 1", n)
	}

	if _, err := os.Stat(filepath.Join(dir, configSchemaDir, "config.schema.v1.13.6.json")); err != nil {
		t.Fatal(err)
	}

	// Another version is another file and another fetch.
	if _, _, err := store.prepare(context.Background(), "v1.14.2"); err != nil || hits.Load() != 2 {
		t.Fatalf("other version: %v, %d fetches", err, hits.Load())
	}
}

func TestSchemaWorksWithoutDataDir(t *testing.T) {
	srv, hits := schemaServer(t, http.StatusOK, testSchema)
	store := testSchemaStore(srv, "")

	for range 2 {
		if _, _, err := store.prepare(context.Background(), "v1.13.6"); err != nil {
			t.Fatal(err)
		}
	}

	if hits.Load() != 1 {
		t.Fatalf("fetched %d times, want 1 (memory)", hits.Load())
	}
}

func TestSchemaRejectsBadVersions(t *testing.T) {
	srv, hits := schemaServer(t, http.StatusOK, testSchema)
	store := testSchemaStore(srv, t.TempDir())

	for _, v := range []string{"", "1.13.6", "v1.13", "main", "v1.13.6/../../x", "v1.13.6?x=1", "v1.13.6 ", "../v1.13.6"} {
		if _, _, err := store.prepare(context.Background(), v); err == nil {
			t.Errorf("version %q must be refused", v)
		}

		if store.cached(v) != nil {
			t.Errorf("version %q must not be cached", v)
		}
	}

	if hits.Load() != 0 {
		t.Fatalf("a bad version reached the network %d times", hits.Load())
	}

	if _, _, err := store.prepare(context.Background(), "v1.14.0-beta.1"); err != nil {
		t.Fatalf("a pre-release tag is a version: %v", err)
	}
}

func TestSchemaFailuresAreNotStoredAndNotRetriedAtOnce(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"not found":    {http.StatusNotFound, "404: Not Found"},
		"not JSON":     {http.StatusOK, "<html>"},
		"not a schema": {http.StatusOK, `{"hello": "world"}`},
		"too large":    {http.StatusOK, `{"$defs": {"x": {"description": "` + strings.Repeat("a", configSchemaMaxBytes) + `"}}}`},
	} {
		t.Run(name, func(t *testing.T) {
			srv, hits := schemaServer(t, tc.status, tc.body)
			dir := t.TempDir()
			store := testSchemaStore(srv, dir)

			for range 3 {
				if _, _, err := store.prepare(context.Background(), "v1.13.6"); err == nil {
					t.Fatal("want an error")
				}
			}

			if hits.Load() != 1 {
				t.Fatalf("a failure was retried at once: %d fetches", hits.Load())
			}

			if files, _ := filepath.Glob(filepath.Join(dir, configSchemaDir, "*")); len(files) != 0 { //nolint:errcheck
				t.Fatalf("a failed fetch left files: %v", files)
			}
		})
	}
}

func TestSchemaCorruptFileIsFetchedAgain(t *testing.T) {
	srv, hits := schemaServer(t, http.StatusOK, testSchema)
	dir := t.TempDir()
	store := testSchemaStore(srv, dir)

	path := store.path("v1.13.6")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte("{half"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, source, err := store.prepare(context.Background(), "v1.13.6"); err != nil || source != schemaSourceNetwork || hits.Load() != 1 {
		t.Fatalf("%q %v %d", source, err, hits.Load())
	}
}

func TestSchemaKeepsOnlyRecentVersions(t *testing.T) {
	srv, _ := schemaServer(t, http.StatusOK, testSchema)
	dir := t.TempDir()
	store := testSchemaStore(srv, dir)

	for _, v := range []string{"v1.8.0", "v1.9.0", "v1.10.0", "v1.11.0", "v1.12.0", "v1.13.0", "v1.14.0", "v1.15.0"} {
		if _, _, err := store.prepare(context.Background(), v); err != nil {
			t.Fatal(err)
		}
	}

	files, err := filepath.Glob(filepath.Join(dir, configSchemaDir, "*.json"))
	if err != nil || len(files) != configSchemaKept {
		t.Fatalf("%d files kept, want %d (%v)", len(files), configSchemaKept, err)
	}
}

func TestSchemaPrepareDemo(t *testing.T) {
	srv, _ := schemaServer(t, http.StatusOK, testSchema)

	saved := configSchemas
	configSchemas = testSchemaStore(srv, t.TempDir())

	t.Cleanup(func() { configSchemas = saved })

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := MachineConfigSchemaPrepare(demo, "", demoNodes()[0].Node)
	if err != nil {
		t.Fatal(err)
	}

	var status schemaStatus
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		t.Fatal(err)
	}

	if status.Version != demoTalosVersion || !status.Available || status.Source != schemaSourceNetwork {
		t.Fatalf("%+v", status)
	}
}
