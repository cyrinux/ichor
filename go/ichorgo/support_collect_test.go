package ichorgo

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// supportRecorder is a SupportListener safe to call from the bundle's goroutine.
type supportRecorder struct {
	progress chan string
	done     chan [2]string // path, error
}

func newSupportRecorder() *supportRecorder {
	return &supportRecorder{progress: make(chan string, 256), done: make(chan [2]string, 1)}
}

func (r *supportRecorder) OnProgress(json string) { r.progress <- json }

func (r *supportRecorder) OnDone(path string, _ int64, errMessage string) {
	r.done <- [2]string{path, errMessage}
}

// resourceDefinition is the definition node registers for a resource type.
func resourceDefinition(t *testing.T, spec meta.ResourceDefinitionSpec) *meta.ResourceDefinition {
	t.Helper()

	rd, err := meta.NewResourceDefinition(spec)
	if err != nil {
		t.Fatal(err)
	}

	return rd
}

func TestStartSupportBundleFake(t *testing.T) {
	const cp, down = "192.0.2.31", "192.0.2.32"

	f := newFakeTalos()
	f.addNode(t, cp, "v1.11.0", machine.TypeControlPlane)
	f.putMachineConfig(t, cp)
	f.put(cp,
		resourceDefinition(t, network.NewHostnameStatus(network.NamespaceName, network.HostnameID).ResourceDefinition()),
		resourceDefinition(t, (&config.MachineConfig{}).ResourceDefinition()),
	)
	f.failLogs = "kubelet"

	dest := filepath.Join(t.TempDir(), "support.zip")
	r := newSupportRecorder()

	StartSupportBundle(f.start(t, cp, down), "fake", "", "", dest, r)

	var done [2]string
	select {
	case done = <-r.done:
	case <-time.After(30 * time.Second):
		t.Fatal("OnDone not called")
	}

	if done != [2]string{dest, ""} {
		t.Fatalf("OnDone = %q", done)
	}

	files := readZip(t, dest)

	want := map[string]string{
		cp + "/version.txt":                                   "tag: v1.11.0\n",
		cp + "/dmesg.log":                                     "Linux version 6.12",
		cp + "/services.txt":                                  "kubelet  Running  unhealthy",
		cp + "/service-logs/apid.log":                         "log line of apid",
		cp + "/service_logs.error.txt":                        "kubelet: ",
		cp + "/containers.txt":                                "default      web-1",
		cp + "/mounts.txt":                                    "/dev/sda6",
		cp + "/processes.txt":                                 "/sbin/init",
		cp + "/machine-config.redacted.yaml":                  "machine:",
		cp + "/resources/HostnameStatuses.net.talos.dev.yaml": "hostname: host-192-0-2-31",
		cp + "/container-logs/kube-system_kube-apiserver-cp_kube-apiserver.log": "log line of c1",
		down + "/unreachable.txt": "connection to " + down + " refused",
		"cluster/etcd.json":       `"members"`,
		"summary.txt":             down + ": skipped",
	}

	for name, part := range want {
		if !strings.Contains(files[name], part) {
			t.Errorf("%s = %q, want it to contain %q", name, files[name], part)
		}
	}

	for name := range files {
		// The workload's own logs stay out, and so does every sensitive resource.
		if strings.Contains(name, "web-1_web") || strings.Contains(name, "MachineConfig") {
			t.Errorf("unexpected file %s", name)
		}
	}

	if got := f.called("List " + config.MachineConfigType); len(got) != 0 {
		t.Errorf("a sensitive type was read: %v", got)
	}

	// A down node is checked once, then skipped: none of its steps reach it.
	if got := f.called("Dmesg " + down); len(got) != 0 {
		t.Errorf("steps ran on the unreachable node: %v", got)
	}

	close(r.progress)

	var last supportProgress

	steps := 0
	for p := range r.progress {
		steps++

		if err := json.Unmarshal([]byte(p), &last); err != nil {
			t.Fatal(err)
		}
	}

	if steps < 10 || last != (supportProgress{Step: "done", Done: 2, Total: 2}) {
		t.Errorf("%d progress reports, last = %+v", steps, last)
	}
}

func TestStartSupportBundleFakeUnknownNode(t *testing.T) {
	f := newFakeTalos()
	f.addNode(t, "192.0.2.31", "v1.11.0", machine.TypeWorker)

	r := newSupportRecorder()
	StartSupportBundle(f.start(t, "192.0.2.31"), "fake", "", "192.0.2.40", filepath.Join(t.TempDir(), "s.zip"), r)

	select {
	case done := <-r.done:
		if !strings.Contains(done[1], "not part of this context") {
			t.Fatalf("OnDone = %q", done)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("OnDone not called")
	}
}

func TestJoinFailures(t *testing.T) {
	if err := joinFailures(nil); err != nil {
		t.Fatalf("no failure = %v", err)
	}

	if err := joinFailures([]string{"a: x", "b: y"}); err == nil || err.Error() != "a: x\nb: y" {
		t.Fatalf("err = %v", err)
	}
}
