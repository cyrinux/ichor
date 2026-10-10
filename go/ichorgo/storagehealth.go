package ichorgo

import (
	"context"
	"math"
	"strings"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// Health of a disk (a volume's fill takes the claim levels storageOK, storageWarning and
// storageCritical).
const (
	diskHealthOK      = "ok"
	diskHealthFailing = "failing"
	diskHealthUnknown = "unknown"
)

type clusterStorage struct {
	Context string        `json:"context"`
	Nodes   []nodeStorage `json:"nodes"`
}

type nodeStorage struct {
	Node     string          `json:"node"`
	Hostname string          `json:"hostname"`
	Volumes  []volumeFill    `json:"volumes"`
	Disks    []diskCondition `json:"disks"`
	Error    string          `json:"error,omitempty"` // the node could not be read: volumes and disks are empty
}

type volumeFill struct {
	Key         string  `json:"key"`   // "<node>|<name>", see storageVolumeKey
	Name        string  `json:"name"`  // EPHEMERAL, STATE, a user volume's name, else the mount point
	Mount       string  `json:"mount"` // where it is mounted
	UsedPercent float64 `json:"usedPercent"`
	FreeBytes   uint64  `json:"freeBytes"`
	SizeBytes   uint64  `json:"sizeBytes"`
	Level       string  `json:"level"` // ok | warning | critical at the default thresholds
}

type diskCondition struct {
	Key    string `json:"key"`    // "<node>|smart|<device>", see storageDiskKey
	Device string `json:"device"` // sda, nvme0n1...
	Model  string `json:"model,omitempty"`
	Health string `json:"health"`           // ok | failing | unknown (no SMART data)
	Reason string `json:"reason,omitempty"` // why it is failing
}

// ClusterStorageHealth reads, for every node of the context in parallel, the fill of its
// mounted volumes (EPHEMERAL, STATE, user volumes) and the SMART verdict of its disks: what
// the background monitors alert on. A node that does not answer is reported with an error
// instead of failing the call. Disks are empty on Talos older than 1.15 (no SMART API), and
// volumes are named by mount point before Talos 1.8 (no volume resources). A kubeconfig
// cluster has no Talos API: the call fails.
func ClusterStorageHealth(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return demoStorageHealth(configYAML, contextName)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		name, _, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		nodes := targetNodes(s.context)
		result := clusterStorage{Context: name, Nodes: make([]nodeStorage, len(nodes))}

		forEachNode(nodes, func(i int, node string) {
			result.Nodes[i] = readNodeStorage(ctx, s, node)
		})

		return toJSON(result)
	})
}

// readNodeStorage reads one node within nodeTimeout. Only the mounts are required: names,
// hostname and SMART are best effort.
func readNodeStorage(ctx context.Context, s *session, node string) nodeStorage {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)
	out := nodeStorage{Node: node, Hostname: node, Volumes: []volumeFill{}, Disks: []diskCondition{}}

	resp, err := s.client.Mounts(nodeCtx)
	if err != nil {
		out.Error = s.friendly(node, err)

		return out
	}

	if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, s.client.COSI, network.HostnameID); err == nil && hs.TypedSpec().Hostname != "" {
		out.Hostname = hs.TypedSpec().Hostname
	}

	out.Volumes = buildVolumeFill(node, diskMounts(first(resp.GetMessages()).GetStats()), readVolumeNames(nodeCtx, s, node))
	out.Disks = buildDiskConditions(node, readDiskHealth(nodeCtx, s, node))

	return out
}

// readVolumeNames maps mount points to Talos volume IDs; nil before Talos 1.8 or on error.
func readVolumeNames(ctx context.Context, s *session, node string) map[string]string {
	if known, err := s.hasResourceType(ctx, node, block.VolumeStatusType); err != nil || !known {
		return nil
	}

	volumes, err := safe.StateListAll[*block.VolumeStatus](ctx, s.client.COSI)
	if err != nil {
		return nil
	}

	mounts, err := safe.StateListAll[*block.MountStatus](ctx, s.client.COSI)
	if err != nil {
		return nil
	}

	names := map[string]string{}

	for _, v := range mapVolumes(safe.ToSlice(volumes, identity), safe.ToSlice(mounts, identity)) {
		if v.MountedOn != "" {
			names[v.MountedOn] = v.ID
		}
	}

	return names
}

// readDiskHealth is NodeDiskHealth's read, best effort: nil on Talos older than 1.15 or on error.
func readDiskHealth(ctx context.Context, s *session, node string) []diskSMART {
	if known, err := s.hasResourceType(ctx, node, smartStatusType); err != nil || !known {
		return nil
	}

	list, err := s.client.COSI.List(ctx, resource.NewMetadata(block.NamespaceName, smartStatusType, "", resource.VersionUndefined))
	if err != nil {
		return nil
	}

	statuses := make(map[string]map[string]any, len(list.Items))

	for _, r := range list.Items {
		if spec, err := resourceSpecMap(r); err == nil {
			statuses[r.Metadata().ID()] = spec
		}
	}

	var disks []diskInfo

	if cosiDisks, err := safe.StateListAll[*block.Disk](ctx, s.client.COSI); err == nil {
		disks = mapBlockDisks(safe.ToSlice(cosiDisks, identity))
	}

	return buildDiskHealth(disks, statuses).Disks
}

// buildVolumeFill turns the node's block-device mounts (diskMounts) into named volumes.
// Loop devices are skipped: the squashfs root and extensions are always full.
func buildVolumeFill(node string, mounts []mountUsage, names map[string]string) []volumeFill {
	out := make([]volumeFill, 0, len(mounts))

	for _, m := range mounts {
		if strings.HasPrefix(m.Filesystem, "/dev/loop") || m.Available > m.Size {
			continue
		}

		name := volumeName(m.MountedOn, names)
		used := m.Size - m.Available
		percent := math.Round(float64(used)/float64(m.Size)*1000) / 10

		out = append(out, volumeFill{
			Key: storageVolumeKey(node, name), Name: name, Mount: m.MountedOn,
			UsedPercent: percent, FreeBytes: m.Available, SizeBytes: m.Size,
			Level: volumeFillLevel(percent, 0, 0),
		})
	}

	return out
}

// wellKnownMounts name the system volumes of a node without volume resources (Talos < 1.8).
var wellKnownMounts = map[string]string{
	constants.EphemeralMountPoint: constants.EphemeralPartitionLabel,
	constants.StateMountPoint:     constants.StatePartitionLabel,
}

// volumeName is the Talos volume mounted at mount (a user volume without its "u-" prefix),
// else the system volume usually mounted there, else the mount point itself.
func volumeName(mount string, names map[string]string) string {
	if id, ok := names[mount]; ok {
		return strings.TrimPrefix(id, constants.UserVolumePrefix)
	}

	if name, ok := wellKnownMounts[mount]; ok {
		return name
	}

	return mount
}

func buildDiskConditions(node string, disks []diskSMART) []diskCondition {
	out := make([]diskCondition, 0, len(disks))

	for _, d := range disks {
		c := diskCondition{Key: storageDiskKey(node, d.Device), Device: d.Device, Model: d.Model, Health: diskHealthUnknown}

		switch {
		case (d.Healthy != nil && !*d.Healthy) || len(d.CriticalWarnings) > 0:
			c.Health = diskHealthFailing
			c.Reason = failingReason(d)
		case d.Healthy != nil:
			c.Health = diskHealthOK
		}

		out = append(out, c)
	}

	return out
}

// failingReason is the disk's critical warnings, else its SMART message.
func failingReason(d diskSMART) string {
	if len(d.CriticalWarnings) > 0 {
		return strings.Join(d.CriticalWarnings, ", ")
	}

	if d.Message != "" {
		return d.Message
	}

	return "SMART reports the disk unhealthy"
}

// volumeFillLevel classifies a volume's fill: critical from crit percent used, warning from
// warn. A threshold <= 0 takes the checkup's (85 and 95); warn is capped at crit.
func volumeFillLevel(usedPercent, warn, crit float64) string {
	if warn <= 0 {
		warn = volumeWarnPercent
	}

	if crit <= 0 {
		crit = volumeCriticalPercent
	}

	warn = min(warn, crit)

	switch {
	case usedPercent >= crit:
		return storageCritical
	case usedPercent >= warn:
		return storageWarning
	default:
		return storageOK
	}
}

// storageVolumeKey identifies a volume's fill issue across runs: "<node>|<volume>".
func storageVolumeKey(node, volume string) string {
	return node + "|" + volume
}

// storageDiskKey identifies a disk's SMART issue across runs: "<node>|smart|<device>".
func storageDiskKey(node, device string) string {
	return node + "|smart|" + device
}

// demoStorageHealth: EPHEMERAL of the first worker is 91 % full and the second worker's
// disk fails SMART; everything else is healthy, so both alerts show.
func demoStorageHealth(configYAML, contextName string) (string, error) {
	name, _, err := resolveContext(configYAML, contextName)
	if err != nil {
		return "", err
	}

	nodes := demoNodes()
	result := clusterStorage{Context: name, Nodes: make([]nodeStorage, 0, len(nodes))}

	for _, n := range nodes {
		ephemeralFree := uint64(72 << 30)
		if n.Hostname == "demo-worker-1" {
			ephemeralFree = 9 << 30
		}

		disk := diskSMART{Device: "nvme0n1", Model: "Demo NVMe", Healthy: new(true)}
		if n.Hostname == "demo-worker-2" {
			disk.Healthy, disk.CriticalWarnings = new(false), []string{nvmeCriticalWarnings[0]}
		}

		result.Nodes = append(result.Nodes, nodeStorage{
			Node: n.Node, Hostname: n.Hostname,
			Volumes: buildVolumeFill(n.Node, []mountUsage{
				{Filesystem: "/dev/nvme0n1p5", MountedOn: "/system/state", Size: 100 << 20, Available: 94 << 20},
				{Filesystem: "/dev/nvme0n1p6", MountedOn: "/var", Size: 100 << 30, Available: ephemeralFree},
			}, nil),
			Disks: buildDiskConditions(n.Node, []diskSMART{disk}),
		})
	}

	return toJSON(result)
}
