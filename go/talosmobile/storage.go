package talosmobile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// diskUsageTimeout covers walking a large tree: the node walks everything below the path
	// whatever the depth, and /var of a node with many container images takes minutes.
	diskUsageTimeout  = 3 * time.Minute
	maxUsageEntries   = 500
	defaultUsageDepth = 1
	maxUsageDepth     = 8
)

type mountList struct {
	Mounts []mountInfo `json:"mounts"`
}

type mountInfo struct {
	Filesystem  string  `json:"filesystem"`
	MountedOn   string  `json:"mountedOn"`
	Size        uint64  `json:"size"`      // bytes
	Available   uint64  `json:"available"` // bytes
	Used        uint64  `json:"used"`      // bytes
	UsedPercent float64 `json:"usedPercent"`
}

// NodeMounts lists every mounted filesystem of node with its usage, like `talosctl mounts`
// (os:reader). Unlike NodeResources' mounts it is not filtered: pseudo filesystems and
// per-pod volumes are included, sorted by mount point.
func NodeMounts(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeMounts", configYAML, contextName, node)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		resp, err := s.client.Mounts(withNode(ctx, node))
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		return toJSON(mountList{Mounts: mapMounts(first(resp.GetMessages()).GetStats())})
	})
}

func mapMounts(stats []*machineapi.MountStat) []mountInfo {
	out := make([]mountInfo, 0, len(stats))

	for _, m := range stats {
		info := mountInfo{
			Filesystem: m.GetFilesystem(),
			MountedOn:  m.GetMountedOn(),
			Size:       m.GetSize(),
			Available:  m.GetAvailable(),
		}

		if info.Size > 0 && info.Available <= info.Size {
			info.Used = info.Size - info.Available
			info.UsedPercent = math.Round(float64(info.Used)/float64(info.Size)*1000) / 10
		}

		out = append(out, info)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].MountedOn < out[j].MountedOn })

	return out
}

type volumeList struct {
	Supported bool         `json:"supported"`
	Reason    string       `json:"reason"` // why it is not supported, else ""
	Volumes   []volumeInfo `json:"volumes"`
}

type volumeInfo struct {
	ID         string `json:"id"`
	Phase      string `json:"phase"`
	Type       string `json:"type"`
	Location   string `json:"location"`
	Size       uint64 `json:"size"` // bytes
	Filesystem string `json:"filesystem"`
	Encryption string `json:"encryption"` // "" when not encrypted
	MountedOn  string `json:"mountedOn"`  // "" when not mounted
	Error      string `json:"error,omitempty"`
}

// NodeVolumes lists node's Talos volumes (STATE, EPHEMERAL, user volumes...) with where they
// are mounted, like `talosctl get volumestatus` + `get mountstatus` (os:reader). The
// resources exist since Talos 1.8: older nodes answer supported=false with a reason.
func NodeVolumes(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeVolumes", configYAML, contextName, node)
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		nodeCtx := withNode(ctx, node)

		known, err := s.hasResourceType(ctx, node, block.VolumeStatusType)
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		if !known {
			return toJSON(volumeList{
				Reason:  notAvailableOn(s.nodeVersion(ctx, node)) + ": needs Talos " + featureMinVersion("volumes") + " or newer",
				Volumes: []volumeInfo{},
			})
		}

		volumes, err := safe.StateListAll[*block.VolumeStatus](nodeCtx, s.client.COSI)
		if err != nil {
			return "", errors.New(s.friendly(node, err))
		}

		// Mount statuses came later than volume statuses: without them, only mountedOn is empty.
		var mounts []*block.MountStatus

		if list, err := safe.StateListAll[*block.MountStatus](nodeCtx, s.client.COSI); err == nil {
			mounts = safe.ToSlice(list, identity)
		}

		return toJSON(volumeList{Supported: true, Volumes: mapVolumes(safe.ToSlice(volumes, identity), mounts)})
	})
}

func mapVolumes(volumes []*block.VolumeStatus, mounts []*block.MountStatus) []volumeInfo {
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

	out := make([]volumeInfo, 0, len(volumes))

	for _, v := range volumes {
		spec := v.TypedSpec()

		info := volumeInfo{
			ID:         v.Metadata().ID(),
			Phase:      spec.Phase.String(),
			Type:       spec.Type.String(),
			Location:   spec.Location,
			Size:       spec.Size,
			Filesystem: spec.Filesystem.String(),
			MountedOn:  mountedOn[v.Metadata().ID()],
			Error:      spec.ErrorMessage,
		}

		if info.Location == "" {
			info.Location = spec.MountLocation
		}

		if info.Filesystem == block.FilesystemTypeNone.String() {
			info.Filesystem = ""
		}

		if spec.EncryptionProvider != block.EncryptionProviderNone {
			info.Encryption = spec.EncryptionProvider.String()
		}

		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })

	return out
}

type diskUsage struct {
	Path      string       `json:"path"`
	Entries   []usageEntry `json:"entries"`
	Truncated bool         `json:"truncated"` // more than maxUsageEntries: the smallest were dropped
}

type usageEntry struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"` // bytes, cumulative for directories
	IsDir bool   `json:"isDir"`
	Error string `json:"error,omitempty"`
}

// NodeDiskUsage returns the disk usage of path on node down to depth levels (1 = the direct
// children), files included, like `talosctl usage -a -d DEPTH PATH` (os:reader). Entries are
// sorted by size, largest first, and capped at 500. The node always walks the whole tree
// below path (depth only limits what is reported), so a big one such as /var can take
// minutes: the call gives up after 3 with an error asking to open a sub-directory (seen
// live: /var of a control plane holding ~2800 images does not finish in 3 minutes).
func NodeDiskUsage(configYAML, contextName, node, path string, depth int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeDiskUsage", configYAML, contextName, node, path)
	}
	path = privacy.unmaskText(path)

	return withSession(configYAML, contextName, diskUsageTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		root, err := cleanUsagePath(path)
		if err != nil {
			return "", err
		}

		nodeCtx := withNode(ctx, node)
		depth := clampUsageDepth(depth)

		var (
			wg      sync.WaitGroup
			dirs    map[string]bool
			dirsErr error
		)

		// The usage API does not say what is a directory: the (cheap) file listing does.
		wg.Go(func() { dirs, dirsErr = listDirectories(nodeCtx, s, root, depth) })

		all, err := readDiskUsage(nodeCtx, s, root, depth)

		wg.Wait()

		if err != nil {
			if ctx.Err() != nil || status.Code(err) == codes.DeadlineExceeded {
				return "", fmt.Errorf("%s is too large to measure within %s: open one of its sub-directories instead", root, diskUsageTimeout)
			}

			return "", errors.New(s.friendly(node, err))
		}

		if dirsErr != nil {
			dirs = nil
		}

		return toJSON(buildDiskUsage(root, all, dirs))
	})
}

func cleanUsagePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/", nil
	}

	if !strings.HasPrefix(p, "/") {
		return "", errors.New("the path must be absolute")
	}

	return path.Clean(p), nil
}

func clampUsageDepth(depth int) int {
	switch {
	case depth <= 0:
		return defaultUsageDepth
	case depth > maxUsageDepth:
		return maxUsageDepth
	default:
		return depth
	}
}

func readDiskUsage(ctx context.Context, s *session, root string, depth int) ([]*machineapi.DiskUsageInfo, error) {
	stream, err := s.client.DiskUsage(ctx, &machineapi.DiskUsageRequest{
		// The server counts the root as a level: N levels below it is N+1.
		RecursionDepth: int32(depth) + 1,
		All:            true,
		Paths:          []string{root},
	})
	if err != nil {
		return nil, err
	}

	var out []*machineapi.DiskUsageInfo

	for {
		info, err := stream.Recv()

		switch {
		case errors.Is(err, io.EOF):
			return out, nil
		case err != nil:
			return nil, err
		}

		if e := info.GetMetadata().GetError(); e != "" {
			return nil, errors.New(e)
		}

		out = append(out, info)
	}
}

// listDirectories returns the directories below root down to depth, like `talosctl ls -d`.
func listDirectories(ctx context.Context, s *session, root string, depth int) (map[string]bool, error) {
	stream, err := s.client.LS(ctx, &machineapi.ListRequest{
		Root:           root,
		Recurse:        true,
		RecursionDepth: int32(depth),
		Types:          []machineapi.ListRequest_Type{machineapi.ListRequest_DIRECTORY},
	})
	if err != nil {
		return nil, err
	}

	dirs := map[string]bool{}

	for {
		info, err := stream.Recv()

		switch {
		case errors.Is(err, io.EOF):
			return dirs, nil
		case err != nil:
			return nil, err
		}

		if e := info.GetMetadata().GetError(); e != "" {
			return nil, errors.New(e)
		}

		if info.GetIsDir() {
			dirs[path.Clean(info.GetName())] = true
		}
	}
}

// buildDiskUsage maps the walk's entries, dropping the root itself. dirs are the known
// directories; when nil (the listing failed) an entry is a directory when another one lies
// below it.
func buildDiskUsage(root string, all []*machineapi.DiskUsageInfo, dirs map[string]bool) diskUsage {
	isDir := dirs

	if isDir == nil {
		isDir = map[string]bool{}

		for _, e := range all {
			if parent := path.Dir(e.GetName()); parent != e.GetName() {
				isDir[parent] = true
			}
		}
	}

	out := diskUsage{Path: root, Entries: make([]usageEntry, 0, len(all))}

	for _, e := range all {
		name := e.GetName()
		if name == "" || path.Clean(name) == root {
			continue
		}

		out.Entries = append(out.Entries, usageEntry{Path: name, Size: e.GetSize(), IsDir: isDir[path.Clean(name)], Error: e.GetError()})
	}

	sort.SliceStable(out.Entries, func(i, j int) bool {
		if out.Entries[i].Size != out.Entries[j].Size {
			return out.Entries[i].Size > out.Entries[j].Size
		}

		return out.Entries[i].Path < out.Entries[j].Path
	})

	if len(out.Entries) > maxUsageEntries {
		out.Entries, out.Truncated = out.Entries[:maxUsageEntries], true
	}

	return out
}
