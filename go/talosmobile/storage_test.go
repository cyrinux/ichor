package talosmobile

import (
	"fmt"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
)

func TestMapMounts(t *testing.T) {
	got := mapMounts([]*machineapi.MountStat{
		{Filesystem: "/dev/nvme0n1p4", MountedOn: "/var", Size: 1000, Available: 250},
		{Filesystem: "tmpfs", MountedOn: "/run", Size: 0, Available: 0},
		{Filesystem: "weird", MountedOn: "/odd", Size: 10, Available: 20},
	})

	want := []mountInfo{
		{Filesystem: "weird", MountedOn: "/odd", Size: 10, Available: 20},
		{Filesystem: "tmpfs", MountedOn: "/run"},
		{Filesystem: "/dev/nvme0n1p4", MountedOn: "/var", Size: 1000, Available: 250, Used: 750, UsedPercent: 75},
	}

	if !equalJSON(t, got, want) {
		t.Error("mounts differ")
	}

	if !equalJSON(t, mountList{Mounts: mapMounts(nil)}, map[string]any{"mounts": []any{}}) {
		t.Error("empty mounts must be [] not null")
	}
}

func TestMapVolumes(t *testing.T) {
	ephemeral := block.NewVolumeStatus(block.NamespaceName, "EPHEMERAL")
	*ephemeral.TypedSpec() = block.VolumeStatusSpec{
		Phase: block.VolumePhaseReady, Type: block.VolumeTypePartition, Location: "/dev/nvme0n1p4",
		Size: 5 << 30, Filesystem: block.FilesystemTypeXFS, EncryptionProvider: block.EncryptionProviderLUKS2,
	}

	dir := block.NewVolumeStatus(block.NamespaceName, "/var/run")
	*dir.TypedSpec() = block.VolumeStatusSpec{Phase: block.VolumePhaseFailed, Type: block.VolumeTypeDirectory, MountLocation: "/var/run", ErrorMessage: "boom"}

	mount := block.NewMountStatus(block.NamespaceName, "EPHEMERAL")
	mount.TypedSpec().Spec.VolumeID = "EPHEMERAL"
	mount.TypedSpec().Target = "/var"

	got := mapVolumes([]*block.VolumeStatus{ephemeral, dir}, []*block.MountStatus{mount})

	want := []volumeInfo{
		{ID: "/var/run", Phase: "failed", Type: "directory", Location: "/var/run", Error: "boom"},
		{ID: "EPHEMERAL", Phase: "ready", Type: "partition", Location: "/dev/nvme0n1p4", Size: 5 << 30, Filesystem: "xfs", Encryption: "luks2", MountedOn: "/var"},
	}

	if !equalJSON(t, got, want) {
		t.Error("volumes differ")
	}
}

func usage(name string, size int64) *machineapi.DiskUsageInfo {
	return &machineapi.DiskUsageInfo{Name: name, Size: size}
}

func TestBuildDiskUsage(t *testing.T) {
	all := []*machineapi.DiskUsageInfo{
		usage("/var/log/a.log", 10), usage("/var/log", 4106), usage("/var/big.img", 9000),
		usage("/var/empty", 4096), usage("/var", 20000),
	}
	dirs := map[string]bool{"/var/log": true, "/var/empty": true, "/var": true}

	got := buildDiskUsage("/var", all, dirs)

	want := diskUsage{Path: "/var", Entries: []usageEntry{
		{Path: "/var/big.img", Size: 9000},
		{Path: "/var/log", Size: 4106, IsDir: true},
		{Path: "/var/empty", Size: 4096, IsDir: true},
		{Path: "/var/log/a.log", Size: 10},
	}}

	if !equalJSON(t, got, want) {
		t.Error("usage differs")
	}

	// Without the directory walk, a directory is what has something below it.
	guessed := buildDiskUsage("/var", all, nil)
	if !guessed.Entries[1].IsDir || guessed.Entries[2].IsDir || guessed.Entries[0].IsDir {
		t.Errorf("guessed = %+v", guessed.Entries)
	}

	var many []*machineapi.DiskUsageInfo
	for i := range 700 {
		many = append(many, usage(fmt.Sprintf("/f%03d", i), int64(i)))
	}

	capped := buildDiskUsage("/", many, nil)
	if len(capped.Entries) != 500 || !capped.Truncated || capped.Entries[0].Size != 699 || capped.Entries[499].Size != 200 {
		t.Errorf("capped: %d entries, truncated=%t", len(capped.Entries), capped.Truncated)
	}

	if out, _ := toJSON(buildDiskUsage("/", nil, nil)); out != `{"path":"/","entries":[],"truncated":false}` { //nolint:errcheck
		t.Errorf("empty usage shape: %s", out)
	}
}

func TestUsageArguments(t *testing.T) {
	for in, want := range map[string]string{"": "/", " /var/ ": "/var", "/var/lib/../log": "/var/log", "/": "/"} {
		if got, err := cleanUsagePath(in); err != nil || got != want {
			t.Errorf("cleanUsagePath(%q) = %q, %v", in, got, err)
		}
	}

	if _, err := cleanUsagePath("var"); err == nil {
		t.Error("relative path accepted")
	}

	if clampUsageDepth(0) != 1 || clampUsageDepth(-3) != 1 || clampUsageDepth(3) != 3 || clampUsageDepth(99) != maxUsageDepth {
		t.Error("clampUsageDepth")
	}
}
