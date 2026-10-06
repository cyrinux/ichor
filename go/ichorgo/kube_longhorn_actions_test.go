package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// lhEngines is an engine list in Longhorn's v1beta2 shape: a volume rebuilding two replicas
// and backing up, one restoring, one idle with finished operations left in its status.
const lhEngines = `{"items":[
 {"spec":{"volumeName":"vol-a"},"status":{
  "rebuildStatus":{"10.0.0.1:10000":{"isRebuilding":true,"progress":70,"state":"in_progress"},
                   "10.0.0.2:10000":{"isRebuilding":true,"progress":35,"state":"in_progress"},
                   "10.0.0.3:10000":{"isRebuilding":false,"progress":100,"state":"complete"}},
  "backupStatus":{"backup-1":{"progress":120,"state":"in_progress"},"backup-0":{"progress":100,"state":"complete"}}}},
 {"spec":{"volumeName":"vol-b"},"status":{"restoreStatus":{"10.0.0.4:10000":{"isRestoring":true,"progress":12}}}},
 {"spec":{"volumeName":"vol-c"},"status":{
  "rebuildStatus":{"10.0.0.5:10000":{"isRebuilding":false,"progress":100,"state":"complete"}},
  "backupStatus":{"backup-2":{"progress":40,"state":"error"}},
  "restoreStatus":{"10.0.0.5:10000":{"isRestoring":false,"progress":100}}}}]}`

func TestAddLonghornProgress(t *testing.T) {
	var engines kubeList[lhEngineObject]
	if err := json.Unmarshal([]byte(lhEngines), &engines); err != nil {
		t.Fatal(err)
	}

	vols := []longhornVolume{{Name: "vol-a", Rebuilding: 2}, {Name: "vol-b"}, {Name: "vol-c"}, {Name: "vol-d"}}
	addLonghornProgress(vols, engines.Items)

	a, b, c, d := vols[0], vols[1], vols[2], vols[3]

	if a.Rebuilding != 2 || a.RebuildProgress != 35 || !a.BackingUp || a.BackupProgress != 100 || a.Restoring {
		t.Errorf("vol-a: %+v", a)
	}

	if !b.Restoring || b.RestoreProgress != 12 || b.BackingUp || b.Rebuilding != 0 {
		t.Errorf("vol-b: %+v", b)
	}

	if c.Rebuilding != 0 || c.BackingUp || c.Restoring || c.RebuildProgress != 0 {
		t.Errorf("finished operations must not show: %+v", c)
	}

	if d.Rebuilding != 0 || d.BackingUp {
		t.Errorf("no engine: %+v", d)
	}
}

func TestAddLonghornProgressCountsARebuildTheReplicasMissed(t *testing.T) {
	var engines kubeList[lhEngineObject]
	if err := json.Unmarshal([]byte(lhEngines), &engines); err != nil {
		t.Fatal(err)
	}

	vols := []longhornVolume{{Name: "vol-a"}}
	addLonghornProgress(vols, engines.Items)

	if vols[0].Rebuilding != 1 || vols[0].RebuildProgress != 35 {
		t.Fatalf("rebuild from the engine only: %+v", vols[0])
	}
}

func TestMapLonghornVolumeConditions(t *testing.T) {
	var v lhVolumeObject
	v.Status.Conditions = []kubeCondition{
		{Type: "Scheduled", Status: "False", Reason: "ReplicaSchedulingFailure", Message: "insufficient storage"},
		{Type: "TooManySnapshots", Status: "True"},
	}

	out := mapLonghornVolume(v, nil)
	if out.ScheduleError != "insufficient storage" || !out.TooManySnapshots {
		t.Fatalf("conditions: %+v", out)
	}

	v.Status.Conditions = []kubeCondition{{Type: "Scheduled", Status: "False", Reason: "ReplicaSchedulingFailure"}}
	if out := mapLonghornVolume(v, nil); out.ScheduleError != "ReplicaSchedulingFailure" {
		t.Fatalf("reason as fallback: %q", out.ScheduleError)
	}

	v.Status.Conditions = []kubeCondition{{Type: "Scheduled", Status: "True"}, {Type: "TooManySnapshots", Status: "False"}}
	if out := mapLonghornVolume(v, nil); out.ScheduleError != "" || out.TooManySnapshots {
		t.Fatalf("healthy conditions: %+v", out)
	}
}

func TestMapLonghornNodeSpecAndReplicas(t *testing.T) {
	nodes := loadFixture[kubeList[lhNodeObject]](t, "longhorn/1.12/nodes.json").Items
	nodes[0].Spec.AllowScheduling = true
	nodes[1].Spec.EvictionRequested = true

	replicas := loadFixture[kubeList[lhReplicaObject]](t, "longhorn/1.12/replicas.json").Items
	out := mapLonghorn("v1beta2", nil, replicas, nodes, nil, "")

	byName := map[string]longhornNode{}
	for _, n := range out.Nodes {
		byName[n.Name] = n
	}

	if n := byName["node-1"]; !n.AllowScheduling || n.EvictionRequested || n.Namespace != "longhorn-system" {
		t.Errorf("node-1: %+v", n)
	}

	if n := byName["node-2"]; n.AllowScheduling || !n.EvictionRequested || n.Replicas != 1 {
		t.Errorf("node-2: %+v", n)
	}

	if n := byName["node-6"]; n.Replicas != 1 {
		t.Errorf("node-6 replicas: %+v", n)
	}
}

const (
	lhVolumePath = "/apis/longhorn.io/v1beta2/namespaces/storage/volumes/pvc-1"
	lhNodePath   = "/apis/longhorn.io/v1beta2/namespaces/storage/nodes/worker-1"
	lhProxyPath  = "/api/v1/namespaces/storage/services/longhorn-backend:9500/proxy/v1/volumes/pvc-1"
)

func longhornActionAPI(t *testing.T, volumeState string) (*fakeKubeAPI, *kubeClient) {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":             `{"groups":[{"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET " + lhVolumePath:   `{"metadata":{"resourceVersion":"41"},"spec":{"numberOfReplicas":3},"status":{"state":"` + volumeState + `"}}`,
		"PATCH " + lhVolumePath: `{}`,
		"GET " + lhNodePath:     `{"metadata":{"name":"worker-1","resourceVersion":"7"},"spec":{"allowScheduling":true}}`,
		"PATCH " + lhNodePath:   `{}`,
		"POST " + lhProxyPath:   `{"type":"volume"}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return f, k
}

func lastRequest(f *fakeKubeAPI) fakeKubeRequest {
	reqs := f.recorded()
	return reqs[len(reqs)-1]
}

func TestLonghornNodeActionPatches(t *testing.T) {
	cases := map[string]string{
		lhActionSchedulingOn:   `{"metadata":{"resourceVersion":"7"},"spec":{"allowScheduling":true,"evictionRequested":false}}`,
		lhActionSchedulingOff:  `{"metadata":{"resourceVersion":"7"},"spec":{"allowScheduling":false}}`,
		lhActionEvict:          `{"metadata":{"resourceVersion":"7"},"spec":{"allowScheduling":false,"evictionRequested":true}}`,
		lhActionCancelEviction: `{"metadata":{"resourceVersion":"7"},"spec":{"evictionRequested":false}}`,
	}

	for action, want := range cases {
		f, k := longhornActionAPI(t, "attached")

		if err := longhornAction(context.Background(), k, "storage", "worker-1", action, 0); err != nil {
			t.Fatalf("%s: %v", action, err)
		}

		last := lastRequest(f)
		if last.method != "PATCH" || last.path != lhNodePath || last.contentType != "application/merge-patch+json" || last.body != want {
			t.Errorf("%s: %+v", action, last)
		}
	}
}

func TestLonghornReplicaCountPatch(t *testing.T) {
	f, k := longhornActionAPI(t, "detached")

	if err := longhornAction(context.Background(), k, "storage", "pvc-1", lhActionReplicas, 2); err != nil {
		t.Fatal(err)
	}

	last := lastRequest(f)
	if last.method != "PATCH" || last.body != `{"metadata":{"resourceVersion":"41"},"spec":{"numberOfReplicas":2}}` {
		t.Fatalf("patch %+v", last)
	}
}

func TestLonghornBackupAndTrimGoThroughTheManager(t *testing.T) {
	for _, action := range []string{lhActionBackup, lhActionTrim} {
		f, k := longhornActionAPI(t, "attached")

		if err := longhornAction(context.Background(), k, "storage", "pvc-1", action, 0); err != nil {
			t.Fatalf("%s: %v", action, err)
		}

		last := lastRequest(f)
		if last.method != "POST" || last.path != lhProxyPath || last.body != `{}` {
			t.Errorf("%s: %+v", action, last)
		}
	}
}

func TestLonghornBackupRefusesADetachedVolume(t *testing.T) {
	f, k := longhornActionAPI(t, "detached")

	err := longhornAction(context.Background(), k, "storage", "pvc-1", lhActionBackup, 0)
	if !errors.Is(err, errLonghornDetached) {
		t.Fatalf("got %v", err)
	}

	if last := lastRequest(f); last.method != "GET" {
		t.Fatalf("nothing may be sent: %+v", last)
	}
}

func TestLonghornActionManagerError(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":           `{"groups":[{"name":"longhorn.io","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET " + lhVolumePath: `{"metadata":{"resourceVersion":"41"},"status":{"state":"attached"}}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	// The proxy is missing (no longhorn-backend service): the API server's answer comes back.
	if err := longhornAction(context.Background(), k, "storage", "pvc-1", lhActionBackup, 0); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
}

func TestLonghornActionWithoutLonghorn(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if err := longhornAction(context.Background(), k, "storage", "pvc-1", lhActionTrim, 0); !errors.Is(err, errLonghornMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestKubeLonghornActionValidates(t *testing.T) {
	cases := []struct {
		namespace, name, action string
		value                   int
		want                    string
	}{
		{"storage", "pvc-1", "delete", 0, "unsupported"},
		{"storage", "", lhActionBackup, 0, "no Longhorn object"},
		{"storage", "../x", lhActionBackup, 0, "invalid Kubernetes name"},
		{"storage", "pvc-1", lhActionReplicas, 0, "between 1 and 20"},
		{"storage", "pvc-1", lhActionReplicas, 21, "between 1 and 20"},
	}

	for _, c := range cases {
		err := KubeLonghornAction("", "", "", c.namespace, c.name, c.action, c.value)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: got %v", c, err)
		}
	}
}

func TestKubeLonghornActionDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeLonghornAction(cfg, "", "", "longhorn-system", "pvc-8a42", lhActionBackup, 0); !errors.Is(err, errDemoUnavailable) {
		t.Fatalf("got %v", err)
	}
}
