package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// nodeDiskNode is a node object; ready false leaves its kubelet unasked.
func nodeDiskNode(name string, ready, diskPressure bool) string {
	readyStatus, pressureStatus := "False", "False"
	if ready {
		readyStatus = "True"
	}

	if diskPressure {
		pressureStatus = "True"
	}

	return fmt.Sprintf(`{"metadata":{"name":%q},"status":{"conditions":[{"type":"Ready","status":%q},{"type":"DiskPressure","status":%q}]}}`,
		name, readyStatus, pressureStatus)
}

// nodeDiskSummary is a kubelet summary: nodefs of capacity with available left (used is
// capacity-available), and an imagefs used/capacity.
func nodeDiskSummary(capacity, available, imageUsed, imageCapacity int64) string {
	return fmt.Sprintf(`{"node":{"fs":{"availableBytes":%d,"capacityBytes":%d,"usedBytes":%d},`+
		`"runtime":{"imageFs":{"availableBytes":%d,"capacityBytes":%d,"usedBytes":%d}}}}`,
		available, capacity, capacity-available, imageCapacity-imageUsed, imageCapacity, imageUsed)
}

type nodeDiskAnswer struct {
	status int
	body   string
}

func TestKubeNodeDisk(t *testing.T) {
	const gi = int64(1 << 30)

	full := nodeDiskAnswer{http.StatusOK, nodeDiskSummary(100*gi, 9*gi, 30*gi, 100*gi)}
	half := nodeDiskAnswer{http.StatusOK, nodeDiskSummary(100*gi, 50*gi, 0, 0)}
	refused := nodeDiskAnswer{http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","code":403}`}
	broken := nodeDiskAnswer{http.StatusInternalServerError, `{"kind":"Status","code":500}`}

	cases := []struct {
		name    string
		nodes   []string
		answers map[string]nodeDiskAnswer
		want    kubeNodeDisk
	}{
		{
			name:    "one node refused, one at 91 %",
			nodes:   []string{nodeDiskNode("node-a", true, false), nodeDiskNode("node-b", true, false)},
			answers: map[string]nodeDiskAnswer{"node-a": refused, "node-b": full},
			want: kubeNodeDisk{Forbidden: true, Measured: 1, Nodes: []kubeNodeDiskUse{
				{Name: "node-b", UsedPercent: 91, AvailableBytes: 9 * gi, CapacityBytes: 100 * gi, ImageFsUsedPercent: 30},
			}},
		},
		{
			name:    "every kubelet refused",
			nodes:   []string{nodeDiskNode("node-a", true, false), nodeDiskNode("node-b", true, false)},
			answers: map[string]nodeDiskAnswer{"node-a": refused, "node-b": refused},
			want:    kubeNodeDisk{Forbidden: true, Nodes: []kubeNodeDiskUse{}},
		},
		{
			// A kubelet that fails, and a node not ready, are absent: never reported as full.
			name: "silent nodes are left out, DiskPressure kept",
			nodes: []string{
				nodeDiskNode("node-a", true, true), nodeDiskNode("node-b", true, false), nodeDiskNode("node-c", false, false),
			},
			answers: map[string]nodeDiskAnswer{"node-a": half, "node-b": broken, "node-c": full},
			want: kubeNodeDisk{Measured: 1, Nodes: []kubeNodeDiskUse{
				{Name: "node-a", UsedPercent: 50, AvailableBytes: 50 * gi, CapacityBytes: 100 * gi, DiskPressure: true},
			}},
		},
		{
			name:  "sorted by name",
			nodes: []string{nodeDiskNode("node-b", true, false), nodeDiskNode("node-a", true, false)},
			answers: map[string]nodeDiskAnswer{
				"node-a": half, "node-b": {http.StatusOK, nodeDiskSummary(100*gi, 100*gi, 0, 0)},
			},
			want: kubeNodeDisk{Measured: 2, Nodes: []kubeNodeDiskUse{
				{Name: "node-a", UsedPercent: 50, AvailableBytes: 50 * gi, CapacityBytes: 100 * gi},
				{Name: "node-b", UsedPercent: 0, AvailableBytes: 100 * gi, CapacityBytes: 100 * gi},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[` + strings.Join(tc.nodes, ",") + `]}`})
			for node, a := range tc.answers {
				f.answerWith("GET /api/v1/nodes/"+node+"/proxy/stats/summary", a.status, a.body)
			}

			got, err := readNodeDisk(context.Background(), openFakeKube(t, f))
			if err != nil {
				t.Fatal(err)
			}

			assertNodeDisk(t, got, tc.want)

			for _, r := range f.recorded() {
				if strings.Contains(r.path, "/node-c/") {
					t.Errorf("a node not ready was asked: %s", r.path)
				}
			}
		})
	}
}

func assertNodeDisk(t *testing.T, got, want kubeNodeDisk) {
	t.Helper()

	gotJSON, _ := json.Marshal(got)   //nolint:errcheck // plain structs
	wantJSON, _ := json.Marshal(want) //nolint:errcheck // plain structs

	if string(gotJSON) != string(wantJSON) {
		t.Errorf("got  %s\nwant %s", gotJSON, wantJSON)
	}
}

func TestKubeNodeDiskNodeListRefused(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{})
	f.answerWith("GET /api/v1/nodes", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","code":403}`)

	got, err := readNodeDisk(context.Background(), openFakeKube(t, f))
	if err != nil {
		t.Fatal(err)
	}

	assertNodeDisk(t, got, kubeNodeDisk{Forbidden: true, Nodes: []kubeNodeDiskUse{}})
}

func TestKubeNodeDiskSkipsLargeClusters(t *testing.T) {
	nodes := make([]string, nodeDiskMaxNodes+1)
	for i := range nodes {
		nodes[i] = nodeDiskNode(fmt.Sprintf("node-%03d", i), true, false)
	}

	f := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[` + strings.Join(nodes, ",") + `]}`})

	got, err := readNodeDisk(context.Background(), openFakeKube(t, f))
	if err != nil {
		t.Fatal(err)
	}

	assertNodeDisk(t, got, kubeNodeDisk{Skipped: true, Nodes: []kubeNodeDiskUse{}})

	for _, r := range f.recorded() {
		if strings.HasSuffix(r.path, "/stats/summary") {
			t.Fatalf("a kubelet was asked on a skipped cluster: %s", r.path)
		}
	}
}

func TestFsUsedPercent(t *testing.T) {
	cases := []struct {
		name string
		fs   kubeletFsStats
		want int
	}{
		{"no capacity", kubeletFsStats{}, 0},
		{"reserved blocks count as used", kubeletFsStats{CapacityBytes: 100, AvailableBytes: 5, UsedBytes: 90}, 95},
		{"used larger than capacity minus available", kubeletFsStats{CapacityBytes: 100, AvailableBytes: 50, UsedBytes: 60}, 60},
		{"clamped", kubeletFsStats{CapacityBytes: 100, UsedBytes: 120}, 100},
		{"rounded", kubeletFsStats{CapacityBytes: 1000, AvailableBytes: 94}, 91},
	}

	for _, tc := range cases {
		if got := fsUsedPercent(tc.fs); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestKubeNodeDiskDemo(t *testing.T) {
	noKubeDial(t)

	out, err := KubeNodeDisk(demoKubeconfigForTest(t), demoKubeContext, "")
	if err != nil {
		t.Fatal(err)
	}

	var d kubeNodeDisk
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatal(err)
	}

	if d.Forbidden || d.Skipped || d.Measured != len(d.Nodes) || len(d.Nodes) < 2 {
		t.Fatalf("demo %+v", d)
	}

	high := 0

	for _, n := range d.Nodes {
		if n.UsedPercent >= volumeWarnPercent {
			high++

			if n.UsedPercent != demoNodeDiskFull {
				t.Errorf("demo node %+v", n)
			}
		}

		if n.Name == "" || n.CapacityBytes <= 0 || n.DiskPressure {
			t.Errorf("demo node %+v", n)
		}
	}

	if high != 1 {
		t.Errorf("demo: %d nodes past the warning, want 1: %s", high, out)
	}
}
