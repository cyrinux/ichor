package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	veleroRestoreBackupJSON = `{"metadata":{"name":"apps-daily-20261002020000","namespace":"velero"},
 "spec":{"storageLocation":"default","includedNamespaces":["demo","monitoring"],"includeClusterResources":null,"snapshotVolumes":true},
 "status":{"phase":"Completed","completionTimestamp":"2026-10-02T02:05:00Z","expiration":"2026-11-01T02:00:00Z",
  "progress":{"totalItems":120,"itemsBackedUp":120},"volumeSnapshotsCompleted":2}}`

	veleroRestoreLocationsJSON = `{"items":[
  {"metadata":{"name":"default","namespace":"velero"},"spec":{"provider":"aws","default":true},"status":{"phase":"Available"}},
  {"metadata":{"name":"offsite","namespace":"velero"},"spec":{"provider":"aws"},"status":{"phase":"Unavailable"}}]}`

	veleroRestoresJSON = `{"items":[
  {"metadata":{"name":"apps-daily-20261002020000-20261003","namespace":"velero"},
   "spec":{"backupName":"apps-daily-20261002020000","includedNamespaces":["demo"],"namespaceMapping":{"demo":"demo-restored"},"existingResourcePolicy":"update"},
   "status":{"phase":"Completed","startTimestamp":"2026-10-03T09:00:00Z","completionTimestamp":"2026-10-03T09:04:00Z","progress":{"totalItems":120,"itemsRestored":118}}},
  {"metadata":{"name":"apps-daily-20261002020000-20261004","namespace":"velero"},
   "spec":{"backupName":"apps-daily-20261002020000"},
   "status":{"phase":"Completed","startTimestamp":"2026-10-04T09:00:00Z","completionTimestamp":"2026-10-04T09:03:00Z","progress":{"totalItems":120,"itemsRestored":120}}},
  {"metadata":{"name":"other-restore","namespace":"velero"},
   "spec":{"backupName":"another-backup"},
   "status":{"phase":"Completed"}}]}`
)

func veleroRestoreAPI(t *testing.T, backup, locations, restores string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	return veleroClient(t, map[string]string{
		"GET " + veleroNS + "backups/apps-daily-20261002020000": backup,
		"GET " + veleroNS + "backupstoragelocations":            locations,
		"GET " + veleroNS + "restores":                          restores,
		"POST " + veleroNS + "restores":                         `{"metadata":{"name":"apps-daily-20261002020000-20261004120000"}}`,
	})
}

func TestVeleroRestorePreview(t *testing.T) {
	f, k := veleroRestoreAPI(t, veleroRestoreBackupJSON, veleroRestoreLocationsJSON, veleroRestoresJSON)

	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	p, err := readVeleroRestorePreview(context.Background(), k, "velero", "apps-daily-20261002020000", now)
	if err != nil || p.Error != "" {
		t.Fatalf("%+v %v", p, err)
	}

	if p.Phase != "Completed" || !p.LocationAvailable || !p.CanRestore || len(p.Reasons) != 0 ||
		p.CompletedAt != time.Date(2026, 10, 2, 2, 5, 0, 0, time.UTC).UnixMilli() ||
		p.ExpiresAt != time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC).UnixMilli() ||
		p.TotalItems != 120 || p.ItemsBackedUp != 120 || p.VolumeSnapshots != 2 ||
		p.IncludeClusterResources != "unset" || p.SnapshotVolumes == nil || !*p.SnapshotVolumes ||
		!slices.Equal(p.IncludedNamespaces, []string{"demo", "monitoring"}) {
		t.Errorf("preview %+v", p)
	}

	// The Restores of this backup, newest first; the other backup's is left out.
	if len(p.Restores) != 2 || p.Restores[0].Name != "apps-daily-20261002020000-20261004" || p.Restores[1].Name != "apps-daily-20261002020000-20261003" {
		t.Fatalf("restores %+v", p.Restores)
	}

	if r := p.Restores[1]; r.Phase != "Completed" || r.ItemsRestored != 118 || r.TotalItems != 120 ||
		r.NamespaceMapping["demo"] != "demo-restored" || r.ExistingResourcePolicy != "update" || !slices.Equal(r.IncludedNamespaces, []string{"demo"}) {
		t.Errorf("restore detail %+v", r)
	}

	// The restores list is read from its own path.
	if !slices.ContainsFunc(f.recorded(), func(r fakeKubeRequest) bool { return strings.HasSuffix(r.path, "/restores") }) {
		t.Errorf("restores not listed in %+v", f.recorded())
	}
}

// TestVeleroRestorePreviewRunningRestore: a Restore of the backup still on its way is a reason
// not to start another.
func TestVeleroRestorePreviewRunningRestore(t *testing.T) {
	running := strings.Replace(veleroRestoresJSON, `"name":"apps-daily-20261002020000-20261004","namespace":"velero"},
   "spec":{"backupName":"apps-daily-20261002020000"},
   "status":{"phase":"Completed"`, `"name":"apps-daily-20261002020000-20261004","namespace":"velero"},
   "spec":{"backupName":"apps-daily-20261002020000"},
   "status":{"phase":"InProgress"`, 1)

	_, k := veleroRestoreAPI(t, veleroRestoreBackupJSON, veleroRestoreLocationsJSON, running)
	p, err := readVeleroRestorePreview(context.Background(), k, "velero", "apps-daily-20261002020000", time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))
	if err != nil || p.CanRestore || !slices.Contains(p.Reasons, "restoreRunning") {
		t.Fatalf("%+v %v", p, err)
	}
}

// TestVeleroRestorePreviewRefusals: the phases, an expired backup, a down location and a
// running restore each say why the backup cannot be restored.
func TestVeleroRestorePreviewRefusals(t *testing.T) {
	expired := strings.Replace(veleroRestoreBackupJSON, `"expiration":"2026-11-01T02:00:00Z"`, `"expiration":"2026-10-01T02:00:00Z"`, 1)
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name, backup, locations string
		want                    []string
		canRestore              bool
	}{
		{"in progress", strings.Replace(veleroRestoreBackupJSON, `"phase":"Completed"`, `"phase":"InProgress"`, 1), veleroRestoreLocationsJSON, nil, false},
		{"failed", strings.Replace(veleroRestoreBackupJSON, `"phase":"Completed"`, `"phase":"Failed"`, 1), veleroRestoreLocationsJSON, nil, false},
		{"partially failed", strings.Replace(veleroRestoreBackupJSON, `"phase":"Completed"`, `"phase":"PartiallyFailed"`, 1), veleroRestoreLocationsJSON, []string{"partiallyFailed"}, true},
		{"expired", expired, veleroRestoreLocationsJSON, []string{"expired"}, false},
		{"location down", veleroRestoreBackupJSON, strings.Replace(veleroRestoreLocationsJSON, `"name":"default","namespace":"velero"},"spec":{"provider":"aws","default":true},"status":{"phase":"Available"}`, `"name":"default","namespace":"velero"},"spec":{"provider":"aws","default":true},"status":{"phase":"Unavailable"}`, 1), []string{"locationUnavailable"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, k := veleroRestoreAPI(t, tc.backup, tc.locations, `{"items":[]}`)
			p, err := readVeleroRestorePreview(context.Background(), k, "velero", "apps-daily-20261002020000", now)
			if err != nil {
				t.Fatal(err)
			}

			if p.CanRestore != tc.canRestore {
				t.Errorf("%s: restorable %v", tc.name, p.CanRestore)
			}

			for _, reason := range tc.want {
				if !slices.Contains(p.Reasons, reason) {
					t.Errorf("%s: no %q in %+v", tc.name, reason, p.Reasons)
				}
			}
		})
	}

	// A backup of every namespace says so.
	_, k := veleroRestoreAPI(t, strings.Replace(veleroRestoreBackupJSON, `["demo","monitoring"]`, `["*"]`, 1), veleroRestoreLocationsJSON, `{"items":[]}`)
	p, err := readVeleroRestorePreview(context.Background(), k, "velero", "apps-daily-20261002020000", now)
	if err != nil || !slices.Contains(p.Reasons, "allNamespaces") {
		t.Fatalf("%+v %v", p, err)
	}
}

// TestVeleroRestorePreviewUnreadableLists: the restores or the locations unreadable leave the
// preview with an error, and an unreadable location counts as unavailable.
func TestVeleroRestorePreviewUnreadableLists(t *testing.T) {
	f, k := veleroRestoreAPI(t, veleroRestoreBackupJSON, veleroRestoreLocationsJSON, veleroRestoresJSON)
	f.answerWith("GET "+veleroNS+"restores", http.StatusForbidden, forbiddenStatus)

	p, err := readVeleroRestorePreview(context.Background(), k, "velero", "apps-daily-20261002020000", time.Now())
	if err != nil || p.Error == "" || len(p.Restores) != 0 {
		t.Fatalf("%+v %v", p, err)
	}

	_, k = veleroRestoreAPI(t, veleroRestoreBackupJSON, `{"items":[]}`, `{"items":[]}`)
	if p, err := readVeleroRestorePreview(context.Background(), k, "velero", "apps-daily-20261002020000", time.Now()); err != nil || p.LocationAvailable || p.CanRestore {
		t.Fatalf("no location: %+v %v", p, err)
	}
}

func TestVeleroRestore(t *testing.T) {
	f, k := veleroRestoreAPI(t, veleroRestoreBackupJSON, veleroRestoreLocationsJSON, `{"items":[]}`)

	name, err := restoreVeleroBackup(context.Background(), k, "velero", "apps-daily-20261002020000", veleroRestoreOptions{
		IncludedNamespaces:     []string{"demo"},
		NamespaceMapping:       map[string]string{"demo": "demo-restored"},
		ExistingResourcePolicy: "update",
	}, time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))
	if err != nil || name != "apps-daily-20261002020000-20261004120000" {
		t.Fatalf("%q %v", name, err)
	}

	var body struct {
		Kind     string
		Metadata struct {
			Name      string
			Namespace string
		}
		Spec map[string]any
	}
	if err := json.Unmarshal([]byte(lastPost(t, f).body), &body); err != nil {
		t.Fatal(err)
	}

	if body.Kind != "Restore" || body.Metadata.Name != "apps-daily-20261002020000-20261004120000" || body.Metadata.Namespace != "velero" ||
		body.Spec["backupName"] != "apps-daily-20261002020000" || body.Spec["existingResourcePolicy"] != "update" {
		t.Errorf("body %+v", body)
	}

	namespaces, _ := body.Spec["includedNamespaces"].([]any)
	mapping, _ := body.Spec["namespaceMapping"].(map[string]any)
	if len(namespaces) != 1 || namespaces[0] != "demo" || mapping["demo"] != "demo-restored" {
		t.Errorf("options %+v", body.Spec)
	}
}

// TestVeleroRestoreOmittedEmpties: unset options are not sent, so Velero keeps its defaults.
func TestVeleroRestoreOmittedEmpties(t *testing.T) {
	f, k := veleroRestoreAPI(t, veleroRestoreBackupJSON, veleroRestoreLocationsJSON, `{"items":[]}`)

	if _, err := restoreVeleroBackup(context.Background(), k, "velero", "apps-daily-20261002020000", veleroRestoreOptions{AllNamespaces: true}, time.Now()); err != nil {
		t.Fatal(err)
	}

	var body struct {
		Spec map[string]any
	}
	if err := json.Unmarshal([]byte(lastPost(t, f).body), &body); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"includedNamespaces", "namespaceMapping", "existingResourcePolicy", "restorePVs", "includeClusterResources"} {
		if _, ok := body.Spec[key]; ok {
			t.Errorf("empty %q sent: %+v", key, body.Spec)
		}
	}
}

func TestVeleroRestoreRefusals(t *testing.T) {
	running := `{"items":[{"metadata":{"name":"r1"},"spec":{"backupName":"apps-daily-20261002020000"},"status":{"phase":"InProgress"}}]}`

	for _, tc := range []struct {
		name, backup, locations, restores string
		want                              error
	}{
		{"in progress", strings.Replace(veleroRestoreBackupJSON, `"phase":"Completed"`, `"phase":"InProgress"`, 1), veleroRestoreLocationsJSON, `{"items":[]}`, errVeleroRestorePhase},
		{"failed validation", strings.Replace(veleroRestoreBackupJSON, `"phase":"Completed"`, `"phase":"FailedValidation"`, 1), veleroRestoreLocationsJSON, `{"items":[]}`, errVeleroRestorePhase},
		{"location down", veleroRestoreBackupJSON, strings.Replace(veleroRestoreLocationsJSON, `"name":"default","namespace":"velero"},"spec":{"provider":"aws","default":true},"status":{"phase":"Available"}`, `"name":"default","namespace":"velero"},"spec":{"provider":"aws","default":true},"status":{"phase":"Unavailable"}`, 1), `{"items":[]}`, errVeleroLocationDown},
		{"restore running", veleroRestoreBackupJSON, veleroRestoreLocationsJSON, running, errVeleroRestoreInProgress},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, k := veleroRestoreAPI(t, tc.backup, tc.locations, tc.restores)
			if _, err := restoreVeleroBackup(context.Background(), k, "velero", "apps-daily-20261002020000", veleroRestoreOptions{}, time.Now()); !errors.Is(err, tc.want) {
				t.Errorf("%s: %v", tc.name, err)
			}

			for _, r := range f.recorded() {
				if r.method == http.MethodPost {
					t.Errorf("%s: posted %s", tc.name, r.path)
				}
			}
		})
	}

	// A backup that does not exist is the API's 404.
	_, k := veleroRestoreAPI(t, veleroRestoreBackupJSON, veleroRestoreLocationsJSON, `{"items":[]}`)
	var apiErr *kubeAPIError
	if _, err := restoreVeleroBackup(context.Background(), k, "velero", "gone", veleroRestoreOptions{}, time.Now()); !errors.As(err, &apiErr) || apiErr.Code != 404 {
		t.Errorf("missing backup: %v", err)
	}
}

// TestVeleroRestoreOptionsValidation: the options are checked before anything is read.
func TestVeleroRestoreOptionsValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options string
	}{
		{"bad json", `{"includedNamespaces":`},
		{"bad namespace", `{"includedNamespaces":["demo/restored"]}`},
		{"empty mapping target", `{"namespaceMapping":{"demo":""}}`},
		{"bad policy", `{"existingResourcePolicy":"overwrite"}`},
	} {
		if _, err := KubeVeleroRestore("", "", "", "velero", "apps-daily-20261002020000", tc.options); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}

	if _, err := KubeVeleroRestore("", "", "", "velero", "apps-daily-20261002020000", ""); err == nil {
		t.Error("invalid backup name accepted")
	}
}

// TestVeleroRestoreOptionsNamespaces: Velero reads an empty includedNamespaces as every
// namespace, so restoring all of them must be asked for; names are DNS-1123 labels.
func TestVeleroRestoreOptionsNamespaces(t *testing.T) {
	long := strings.Repeat("a", 64)

	for _, tc := range []struct {
		name    string
		options veleroRestoreOptions
		ok      bool
	}{
		{"nothing picked", veleroRestoreOptions{}, false},
		{"empty list", veleroRestoreOptions{IncludedNamespaces: []string{}}, false},
		{"all namespaces flag", veleroRestoreOptions{AllNamespaces: true}, true},
		{"wildcard", veleroRestoreOptions{IncludedNamespaces: []string{"*"}}, true},
		{"one namespace", veleroRestoreOptions{IncludedNamespaces: []string{"demo"}}, true},
		{"mapped namespace", veleroRestoreOptions{IncludedNamespaces: []string{"demo"}, NamespaceMapping: map[string]string{"demo": "demo-restored"}}, true},
		{"flag and names", veleroRestoreOptions{AllNamespaces: true, IncludedNamespaces: []string{"demo"}}, false},
		{"wildcard and names", veleroRestoreOptions{IncludedNamespaces: []string{"*", "demo"}}, false},
		{"upper case", veleroRestoreOptions{IncludedNamespaces: []string{"Demo"}}, false},
		{"dot", veleroRestoreOptions{IncludedNamespaces: []string{"demo.restored"}}, false},
		{"64 characters", veleroRestoreOptions{IncludedNamespaces: []string{long}}, false},
		{"63 characters", veleroRestoreOptions{IncludedNamespaces: []string{long[:63]}}, true},
		{"dotted mapping target", veleroRestoreOptions{IncludedNamespaces: []string{"demo"}, NamespaceMapping: map[string]string{"demo": "demo.restored"}}, false},
		{"wildcard mapping", veleroRestoreOptions{AllNamespaces: true, NamespaceMapping: map[string]string{"*": "demo"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.options.validate(); (err == nil) != tc.ok {
				t.Errorf("validate() = %v, want ok %v", err, tc.ok)
			}
		})
	}

	// Through the entry point: nothing picked is refused before anything is read.
	if _, err := KubeVeleroRestore("", "", "", "velero", "apps-daily-20261002020000", `{"includedNamespaces":[]}`); err == nil || !strings.Contains(err.Error(), "namespace") {
		t.Errorf("empty pick: %v", err)
	}
}

// TestVeleroRestoreAllNamespacesSpec: restoring every namespace leaves includedNamespaces out
// of the Restore, which is how Velero spells "all".
func TestVeleroRestoreAllNamespacesSpec(t *testing.T) {
	for _, options := range []veleroRestoreOptions{{AllNamespaces: true}, {IncludedNamespaces: []string{"*"}}} {
		spec := veleroRestoreSpec("apps-daily-20261002020000", options)
		if ns, ok := spec["includedNamespaces"]; ok && !slices.Equal(ns.([]string), []string{"*"}) {
			t.Errorf("%+v: includedNamespaces %v", options, ns)
		}
	}

	if _, ok := veleroRestoreSpec("b", veleroRestoreOptions{AllNamespaces: true})["includedNamespaces"]; ok {
		t.Error("all namespaces flag sent includedNamespaces")
	}
}

// TestMapVeleroRestoresNewestByCreation: a Restore just created has no startTimestamp yet; it
// is still the newest, ahead of the veleroBackupsMax cut, so the apps follow it.
func TestMapVeleroRestoresNewestByCreation(t *testing.T) {
	restore := func(name, created, started string) veleroRestoreObject {
		var r veleroRestoreObject
		r.Metadata.Name, r.Metadata.CreationTimestamp = name, created
		r.Spec.BackupName = "nightly"
		r.Status.StartTimestamp = started
		r.Status.Phase = "Completed"

		return r
	}

	var restores []veleroRestoreObject
	for i := range veleroBackupsMax {
		at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour).Format(time.RFC3339)
		restores = append(restores, restore("nightly-old-"+at, at, at))
	}

	fresh := restore("nightly-20261004120000", "2026-10-04T12:00:00Z", "")
	fresh.Status.Phase = ""
	restores = append(restores, fresh)

	// No creationTimestamp: the start time still orders it.
	legacy := restore("nightly-legacy", "", "2026-10-01T09:00:00Z")
	restores = append(restores, legacy)

	got := mapVeleroRestores(restores, "nightly")
	if len(got) != veleroBackupsMax || got[0].Name != "nightly-20261004120000" || got[1].Name != "nightly-legacy" {
		names := make([]string, 0, 3)
		for _, r := range got[:min(3, len(got))] {
			names = append(names, r.Name)
		}

		t.Errorf("%d restores, first %v", len(got), names)
	}
}

func TestVeleroRestoreDemoAndValidation(t *testing.T) {
	cfg, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := KubeVeleroRestore(cfg, demoKubeContext, "", "velero", "apps-daily-20261002020000", `{"includedNamespaces":["demo"]}`); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo restore: %v", err)
	}

	out, err := KubeVeleroRestorePreview(cfg, demoKubeContext, "", "velero", "apps-daily-20261002020000")
	if err != nil {
		t.Fatal(err)
	}

	var p veleroRestorePreview
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	if !p.CanRestore || p.TotalItems == 0 || len(p.Restores) == 0 {
		t.Errorf("demo preview %+v", p)
	}

	failed := demoVeleroRestorePreview("velero", "before-upgrade", time.Now())
	if !failed.CanRestore || !slices.Contains(failed.Reasons, "partiallyFailed") {
		t.Errorf("demo failed preview %+v", failed)
	}
}
