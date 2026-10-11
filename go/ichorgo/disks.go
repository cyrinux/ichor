package ichorgo

import (
	"context"
	"sort"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
)

// minFreeSegment is the smallest gap drawn as an "Unallocated" segment of a disk's bar:
// smaller gaps (partition alignment) only count in the disk's unallocated total.
const minFreeSegment = 1 << 20

const (
	encryptionEnabled  = "enabled"
	encryptionDisabled = "disabled"
	encryptionUnknown  = "unknown"
)

type diskLayoutList struct {
	Supported bool         `json:"supported"`
	Reason    string       `json:"reason"` // why it is not supported, else ""
	Disks     []diskLayout `json:"disks"`
}

type diskLayout struct {
	Name        string          `json:"name"`    // sda, nvme0n1...
	DevPath     string          `json:"devPath"` // /dev/sda
	Size        uint64          `json:"size"`    // bytes
	Type        string          `json:"type"`    // ssd | hdd | nvme | sd | unknown
	Transport   string          `json:"transport"`
	Model       string          `json:"model"`
	Serial      string          `json:"serial"`
	SystemDisk  bool            `json:"systemDisk"`
	Readonly    bool            `json:"readonly"`
	Unallocated uint64          `json:"unallocated"` // bytes not in any partition
	Partitions  []diskPartition `json:"partitions"`  // by partition index
	Segments    []diskSegment   `json:"segments"`    // the bar, by offset
}

type diskPartition struct {
	Index              uint   `json:"index"`
	DevPath            string `json:"devPath"`
	Label              string `json:"label"`    // "" when the partition has none
	VolumeID           string `json:"volumeId"` // the Talos volume on it (STATE...), else ""
	Offset             uint64 `json:"offset"`   // bytes
	Size               uint64 `json:"size"`     // bytes
	Filesystem         string `json:"filesystem"`
	Encryption         string `json:"encryption"`         // enabled | disabled | unknown
	EncryptionProvider string `json:"encryptionProvider"` // luks2..., "" when unknown
	UUID               string `json:"uuid"`               // filesystem UUID
	PartitionUUID      string `json:"partitionUuid"`
	PartitionType      string `json:"partitionType"`
	MountedOn          string `json:"mountedOn"`
	Phase              string `json:"phase"`
	Error              string `json:"error"`
}

type diskSegment struct {
	Kind   string `json:"kind"` // partition | free
	Index  uint   `json:"index,omitempty"`
	Label  string `json:"label,omitempty"`
	Offset uint64 `json:"offset"`
	Size   uint64 `json:"size"`
}

// NodeDisks lists node's physical disks with their partitions, free space and encryption
// state, like `talosctl get disks` + `get discoveredvolumes` + `get volumestatus`
// (os:reader). The bar segments are computed here so both apps draw the same thing. The
// resources exist since Talos 1.8: older nodes answer supported=false with a reason.
func NodeDisks(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeDisks", configYAML, contextName, node)
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		known, err := s.hasResourceType(ctx, node, block.DiscoveredVolumeType)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		if !known {
			return toJSON(diskLayoutList{
				Reason: notAvailableOn(s.nodeVersion(ctx, node)) + ": needs Talos " + featureMinVersion("disks") + " or newer",
				Disks:  []diskLayout{},
			})
		}

		disks, err := safe.StateListAll[*block.Disk](ctx, s.client.COSI)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		discovered, err := safe.StateListAll[*block.DiscoveredVolume](ctx, s.client.COSI)
		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		// Best effort: without volume statuses every partition's encryption is unknown,
		// without mount statuses (before 1.10) mountedOn is empty.
		var (
			volumes    []*block.VolumeStatus
			mounts     []*block.MountStatus
			systemDisk string
		)

		if list, err := safe.StateListAll[*block.VolumeStatus](ctx, s.client.COSI); err == nil {
			volumes = safe.ToSlice(list, identity)
		}

		if list, err := safe.StateListAll[*block.MountStatus](ctx, s.client.COSI); err == nil {
			mounts = safe.ToSlice(list, identity)
		}

		if sd, err := safe.StateGetByID[*block.SystemDisk](ctx, s.client.COSI, block.SystemDiskID); err == nil {
			systemDisk = sd.TypedSpec().DiskID
		}

		return toJSON(buildDisks(safe.ToSlice(disks, identity), safe.ToSlice(discovered, identity), volumes, mounts, systemDisk))
	})
}

// buildDisks joins each physical disk to its partitions (block.DiscoveredVolume) and the
// Talos volumes on them, and cuts the disk into bar segments.
func buildDisks(disks []*block.Disk, discovered []*block.DiscoveredVolume, volumes []*block.VolumeStatus, mounts []*block.MountStatus, systemDisk string) diskLayoutList {
	mountedOn := volumeMountPoints(mounts)

	sortedVolumes := append([]*block.VolumeStatus(nil), volumes...)
	sort.Slice(sortedVolumes, func(i, j int) bool { return sortedVolumes[i].Metadata().ID() < sortedVolumes[j].Metadata().ID() })

	out := make([]diskLayout, 0, len(disks))

	for _, d := range disks {
		spec := d.TypedSpec()
		name := d.Metadata().ID()

		if spec.CDROM || spec.Size == 0 || isPseudoDisk(name) {
			continue
		}

		devPath := spec.DevPath
		if devPath == "" {
			devPath = "/dev/" + name
		}

		partitions := diskPartitions(name, devPath, discovered, sortedVolumes, mountedOn)
		segments, unallocated := diskSegments(spec.Size, partitions)

		out = append(out, diskLayout{
			Name:        name,
			DevPath:     devPath,
			Size:        spec.Size,
			Type:        blockDiskType(spec),
			Transport:   spec.Transport,
			Model:       spec.Model,
			Serial:      spec.Serial,
			SystemDisk:  systemDisk != "" && name == systemDisk,
			Readonly:    spec.Readonly,
			Unallocated: unallocated,
			Partitions:  partitions,
			Segments:    segments,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].SystemDisk != out[j].SystemDisk {
			return out[i].SystemDisk
		}

		return out[i].Name < out[j].Name
	})

	return diskLayoutList{Supported: true, Disks: out}
}

// diskPartitions returns the partitions of the disk named name, by partition index.
func diskPartitions(name, devPath string, discovered []*block.DiscoveredVolume, volumes []*block.VolumeStatus, mountedOn map[string]string) []diskPartition {
	out := []diskPartition{}

	for _, dv := range discovered {
		spec := dv.TypedSpec()

		if spec.Type != "partition" || (spec.Parent != name && (spec.Parent != "" || spec.ParentDevPath != devPath)) {
			continue
		}

		p := diskPartition{
			Index:         spec.PartitionIndex,
			DevPath:       spec.DevPath,
			Label:         spec.PartitionLabel,
			Offset:        spec.Offset,
			Size:          spec.Size,
			Filesystem:    spec.Name,
			Encryption:    encryptionUnknown,
			UUID:          spec.UUID,
			PartitionUUID: spec.PartitionUUID,
			PartitionType: spec.PartitionType,
		}

		if p.Label == "" {
			p.Label = spec.Label
		}

		if p.Filesystem == "luks" {
			p.Encryption = encryptionEnabled
		}

		if v := partitionVolume(spec, devPath, volumes); v != nil {
			p = withVolume(p, v, mountedOn)
		}

		out = append(out, p)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Index < out[j].Index })

	return out
}

// partitionVolume returns the Talos volume on a partition: by its device path, else by its
// parent disk and index; nil when none (EFI, BIOS, foreign partitions).
func partitionVolume(spec *block.DiscoveredVolumeSpec, diskDevPath string, volumes []*block.VolumeStatus) *block.VolumeStatus {
	for _, v := range volumes {
		if vs := v.TypedSpec(); spec.DevPath != "" && vs.Location == spec.DevPath {
			return v
		}
	}

	for _, v := range volumes {
		vs := v.TypedSpec()
		if vs.ParentLocation != "" && vs.ParentLocation == diskDevPath && vs.PartitionIndex > 0 && uint(vs.PartitionIndex) == spec.PartitionIndex {
			return v
		}
	}

	return nil
}

func withVolume(p diskPartition, v *block.VolumeStatus, mountedOn map[string]string) diskPartition {
	vs := v.TypedSpec()

	p.VolumeID = v.Metadata().ID()
	p.Phase = vs.Phase.String()
	p.Error = vs.ErrorMessage
	p.MountedOn = mountedOn[p.VolumeID]

	if vs.EncryptionProvider != block.EncryptionProviderNone {
		p.Encryption, p.EncryptionProvider = encryptionEnabled, vs.EncryptionProvider.String()
	} else {
		p.Encryption, p.EncryptionProvider = encryptionDisabled, ""
	}

	// An encrypted partition probes as "luks": the volume knows the filesystem inside.
	if fs := vs.Filesystem; (p.Filesystem == "" || p.Filesystem == "luks") && fs != block.FilesystemTypeNone {
		p.Filesystem = fs.String()
	}

	return p
}

// diskSegments cuts a disk of size bytes into partition and free segments by offset, from
// the first partition (or 1 MiB, the usual alignment, when there is none) to the end of the
// disk. Free gaps under 1 MiB are not drawn but count in unallocated. Overlapping or
// out-of-range partitions are clamped; empty ones are left out.
func diskSegments(size uint64, partitions []diskPartition) (segments []diskSegment, unallocated uint64) {
	byOffset := append([]diskPartition(nil), partitions...)
	sort.SliceStable(byOffset, func(i, j int) bool { return byOffset[i].Offset < byOffset[j].Offset })

	segments = []diskSegment{}
	cursor := uint64(minFreeSegment)

	if len(byOffset) > 0 {
		cursor = byOffset[0].Offset
	}

	cursor = min(cursor, size)

	addGap := func(end uint64) {
		if end <= cursor {
			return
		}

		gap := end - cursor
		unallocated += gap

		if gap >= minFreeSegment {
			segments = append(segments, diskSegment{Kind: "free", Offset: cursor, Size: gap})
		}
	}

	for _, p := range byOffset {
		start := min(max(p.Offset, cursor), size)
		end := min(p.Offset+p.Size, size)

		if p.Offset+p.Size < p.Offset { // overflow
			end = size
		}

		addGap(start)
		cursor = max(cursor, start)

		if end > start {
			segments = append(segments, diskSegment{Kind: "partition", Index: p.Index, Label: p.Label, Offset: start, Size: end - start})
			cursor = end
		}
	}

	addGap(size)

	return segments, unallocated
}

// volumeMountPoints maps a volume ID to its shortest mount target.
func volumeMountPoints(mounts []*block.MountStatus) map[string]string {
	mountedOn := map[string]string{}

	for _, m := range mounts {
		spec := m.TypedSpec()

		id := spec.Spec.VolumeID
		if id == "" {
			id = m.Metadata().ID()
		}

		if prev, ok := mountedOn[id]; !ok || len(spec.Target) < len(prev) {
			mountedOn[id] = spec.Target
		}
	}

	return mountedOn
}
