package ichorgo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// threeControlPlanes is an etcd cluster of three control planes; the third one is down
// unless up is set.
func threeControlPlanes(t *testing.T, up bool) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.members = map[string]uint64{"192.0.2.51": 0xa1, "192.0.2.52": 0xa2, "192.0.2.53": 0xa3}
	f.leader = 0xa1

	for _, n := range []string{"192.0.2.51", "192.0.2.52", "192.0.2.53"} {
		if n != "192.0.2.53" || up {
			f.addNode(t, n, "v1.11.0", machine.TypeControlPlane)
		}
	}

	return f, f.start(t, "192.0.2.51", "192.0.2.52", "192.0.2.53")
}

func decodeJSON[T any](t *testing.T, s string, err error) T {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}

	var out T
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		t.Fatalf("%v: %s", err, s)
	}

	return out
}

func TestUpgradePlanFakeControlPlane(t *testing.T) {
	f, cfg := threeControlPlanes(t, true)
	f.putMachineConfig(t, "192.0.2.51")

	out, err := UpgradePlan(cfg, "fake", "", "192.0.2.51")
	plan := decodeJSON[upgradePlan](t, out, err)

	if !plan.ControlPlane || plan.CurrentVersion != "v1.11.0" || plan.Hostname != "host-192-0-2-51" || len(plan.Blockers) != 0 {
		t.Fatalf("plan = %s", out)
	}

	if e := plan.Etcd; e == nil || e.Members != 3 || e.Healthy != 3 || !e.ThisNodeMember || !e.QuorumAfterLoss {
		t.Errorf("etcd = %+v", plan.Etcd)
	}

	if _, err := UpgradePlan(cfg, "fake", "", "192.0.2.59"); err == nil {
		t.Error("a node outside the context must be refused")
	}
}

func TestUpgradePlanFakeEtcdMemberDown(t *testing.T) {
	_, cfg := threeControlPlanes(t, false)

	out, err := UpgradePlan(cfg, "fake", "", "192.0.2.51")
	plan := decodeJSON[upgradePlan](t, out, err)

	// Upgrading a second member while one is down loses quorum: an etcd blocker, forceable.
	if e := plan.Etcd; e == nil || e.Healthy != 2 || e.QuorumAfterLoss || len(plan.Blockers) == 0 || !plan.Forceable {
		t.Errorf("plan = %s", out)
	}
}

func TestEtcdMembersFake(t *testing.T) {
	f, cfg := threeControlPlanes(t, false)

	out, err := EtcdMemberPlan(cfg, "fake", "a3")
	plan := decodeJSON[etcdMemberPlan](t, out, err)

	if !plan.Found || plan.Healthy || plan.Members != 3 || !plan.QuorumAfter || len(plan.Blockers) != 0 {
		t.Errorf("plan = %s", out)
	}

	if err := EtcdRemoveMember(cfg, "fake", "192.0.2.51", "a3"); err != nil {
		t.Fatal(err)
	}

	if got := f.called("EtcdRemoveMemberByID"); !slices.Equal(got, []string{"EtcdRemoveMemberByID a3 192.0.2.51"}) {
		t.Errorf("removals = %v", got)
	}

	if err := EtcdRemoveMember(cfg, "fake", "192.0.2.51", "a1"); err == nil || !strings.Contains(err.Error(), "another control plane") {
		t.Errorf("err = %v, want its own member refused", err)
	}

	out, err = EtcdForfeitLeadership(cfg, "fake", "192.0.2.51")
	if got := decodeJSON[forfeitResult](t, out, err); got.Member != "host-new-leader" {
		t.Errorf("forfeit = %s", out)
	}

	if _, err := EtcdForfeitLeadership(cfg, "fake", "192.0.2.53"); err == nil {
		t.Error("a node that is down cannot forfeit")
	}
}

func TestResourcesFake(t *testing.T) {
	const node = "192.0.2.61"

	f := newFakeTalos()
	f.addNode(t, node, "v1.11.0", machine.TypeWorker)
	f.put(node,
		resourceDefinition(t, network.NewHostnameStatus(network.NamespaceName, network.HostnameID).ResourceDefinition()),
		resourceDefinition(t, (&config.MachineConfig{}).ResourceDefinition()),
	)

	cfg := f.start(t, node)

	out, err := ResourceTypes(cfg, "fake", node)
	types := decodeJSON[[]resourceTypeInfo](t, out, err)

	if len(types) != 2 || types[0].Type != "HostnameStatuses.net.talos.dev" || types[1].Sensitivity != "sensitive" {
		t.Errorf("types = %s", out)
	}

	out, err = ResourceList(cfg, "fake", node, "", "hostname")
	items := decodeJSON[resourceItems](t, out, err)

	if items.Type != "HostnameStatuses.net.talos.dev" || items.Namespace != "network" || len(items.Items) != 1 || items.Items[0].ID != "hostname" {
		t.Errorf("list = %s", out)
	}

	out, err = ResourceGet(cfg, "fake", node, "", "HostnameStatuses.net.talos.dev", "hostname")
	if y := decodeJSON[resourceYAML](t, out, err); !strings.Contains(y.YAML, "hostname: host-192-0-2-61") {
		t.Errorf("get = %s", out)
	}

	for _, tc := range []struct{ typ, id, want string }{
		{"hostname", "other", "not found"},
		{"", "hostname", "no resource type given"},
		{"Unknowns.example.com", "x", "not available"},
	} {
		if _, err := ResourceGet(cfg, "fake", node, "", tc.typ, tc.id); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("ResourceGet(%q, %q) = %v, want %q", tc.typ, tc.id, err, tc.want)
		}
	}
}

func TestStorageFake(t *testing.T) {
	const node = "192.0.2.63"

	f := newFakeTalos()
	f.addNode(t, node, "v1.7.0", machine.TypeWorker)

	cfg := f.start(t, node)

	out, err := NodeMounts(cfg, "fake", node)
	if m := decodeJSON[mountList](t, out, err); len(m.Mounts) != 1 || m.Mounts[0].MountedOn != "/var" || m.Mounts[0].UsedPercent != 40 {
		t.Errorf("mounts = %s", out)
	}

	// No VolumeStatus definition: the node's Talos is too old for volumes.
	out, err = NodeVolumes(cfg, "fake", node)
	if v := decodeJSON[volumeList](t, out, err); v.Supported || !strings.Contains(v.Reason, "v1.7.0") {
		t.Errorf("volumes = %s", out)
	}

	out, err = NodeDiskUsage(cfg, "fake", node, "/var/", 2)
	usage := decodeJSON[diskUsage](t, out, err)

	want := []usageEntry{
		{Path: "/var/logs", Size: 2000, IsDir: true},
		{Path: "/var/logs/app.log", Size: 2000},
		{Path: "/var/config", Size: 1000},
	}
	if usage.Path != "/var" || !slices.Equal(usage.Entries, want) {
		t.Errorf("usage = %s", out)
	}

	if _, err := NodeDiskUsage(cfg, "fake", node, "var", 2); err == nil {
		t.Error("a relative path must be refused")
	}
}

// A node without the deprecated MachineService.ImageList is listed through the ImageService.
func TestNodeImagesFake(t *testing.T) {
	const node = "192.0.2.64"

	f := newFakeTalos()
	f.addNode(t, node, "v1.13.0", machine.TypeWorker)

	out, err := NodeImages(f.start(t, node), "fake", node)
	images := decodeJSON[[]imageInfo](t, out, err)

	if len(images) != 1 || images[0] != (imageInfo{Name: "registry.k8s.io/pause:3.10", Digest: "sha256:abc", Size: 320 << 10}) {
		t.Errorf("images = %s", out)
	}
}

func TestContainerLogsFake(t *testing.T) {
	const node = "192.0.2.62"

	f := newFakeTalos()
	f.addNode(t, node, "v1.11.0", machine.TypeWorker)
	f.failLogs = "kube-system/gone:app:1"

	cfg := f.start(t, node)

	out, err := ContainerLogs(cfg, "fake", node, "kube-system/web:app:1", 50)
	if tail := decodeJSON[logTail](t, out, err); len(tail.Lines) != 1 || tail.Lines[0] != "log line of kube-system/web:app:1" {
		t.Errorf("logs = %s", out)
	}

	if _, err := ContainerLogs(cfg, "fake", node, " ", 50); err == nil {
		t.Error("an empty container id must be refused")
	}

	if _, err := ContainerLogs(cfg, "fake", node, "kube-system/gone:app:1", 50); err == nil {
		t.Error("a log the node cannot read must fail")
	}
}
