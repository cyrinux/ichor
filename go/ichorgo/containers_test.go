package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

func containerCluster(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.addNode(t, "192.0.2.71", "v1.11.0", machine.TypeControlPlane)

	return f, f.start(t, "192.0.2.71")
}

func TestNodeContainersListsSystemThenKubernetes(t *testing.T) {
	_, cfg := containerCluster(t)

	out, err := NodeContainers(cfg, "fake", "192.0.2.71")
	if err != nil {
		t.Fatal(err)
	}

	var list containerList
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, c := range list.Containers {
		got = append(got, c.Namespace+" "+c.PodNamespace+"/"+c.Pod+" "+c.Name+" "+c.ID)
	}

	want := []string{
		"system / apid apid",
		"system / trustd trustd",
		"k8s.io default/web-1 web c2",
		"k8s.io kube-system/kube-apiserver-cp kube-apiserver c1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("containers =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestContainerRestart(t *testing.T) {
	for _, tc := range []struct {
		namespace, id string
		driver        common.ContainerDriver
	}{
		{"system", "trustd", common.ContainerDriver_CONTAINERD},
		{" k8s.io ", "c2", common.ContainerDriver_CRI},
	} {
		t.Run(tc.id, func(t *testing.T) {
			withDataDir(t)

			f, cfg := containerCluster(t)

			if err := ContainerRestart(cfg, "fake", "192.0.2.71", tc.namespace, tc.id); err != nil {
				t.Fatal(err)
			}

			ns := strings.TrimSpace(tc.namespace)

			if len(f.restarts) != 1 {
				t.Fatalf("restarts = %v", f.restarts)
			}

			if r := f.restarts[0]; r.GetNamespace() != ns || r.GetId() != tc.id || r.GetDriver() != tc.driver {
				t.Errorf("request = %v", r)
			}

			if got := f.called("Restart"); len(got) != 1 || got[0] != "Restart 192.0.2.71" {
				t.Errorf("calls = %v", got)
			}

			entries := readAudit(t, "fake", "container-restart")
			if len(entries) != 1 || entries[0].Object != ns+"/"+tc.id || entries[0].Node != "192.0.2.71" || entries[0].Outcome != auditOK {
				t.Errorf("audit = %+v", entries)
			}
		})
	}
}

func TestContainerRestartRefusesBadInput(t *testing.T) {
	withDataDir(t)

	f, cfg := containerCluster(t)

	for _, tc := range []struct{ node, namespace, id, want string }{
		{"192.0.2.71", "default", "c2", "unknown container namespace"},
		{"192.0.2.71", "k8s.io", " ", "no container given"},
		{"192.0.2.99", "system", "apid", "not part of this context"},
	} {
		if err := ContainerRestart(cfg, "fake", tc.node, tc.namespace, tc.id); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s %q: %v", tc.node, tc.namespace, tc.id, err)
		}
	}

	if len(f.restarts) != 0 {
		t.Errorf("restarts = %v", f.restarts)
	}

	if entries := readAudit(t, "fake", "container-restart"); len(entries) != 3 || entries[0].Outcome != auditFailed {
		t.Errorf("audit = %+v", entries)
	}
}
