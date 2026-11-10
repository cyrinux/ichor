package ichorgo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	timeapi "github.com/siderolabs/talos/pkg/machinery/api/time"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeTalosMachine struct {
	machineapi.UnimplementedMachineServiceServer

	f *fakeTalos
}

func (m fakeTalosMachine) Version(ctx context.Context, _ *emptypb.Empty) (*machineapi.VersionResponse, error) {
	m.f.waitVersion(ctx)

	node, err := m.f.enter(ctx, "Version")
	if err != nil {
		return nil, err
	}

	m.f.mu.Lock()
	tag := m.f.versions[node]
	m.f.mu.Unlock()

	return &machineapi.VersionResponse{Messages: []*machineapi.Version{{
		Version:  &machineapi.VersionInfo{Tag: tag, Sha: "abc123", Arch: "amd64", Os: "linux", GoVersion: "go1.25"},
		Platform: &machineapi.PlatformInfo{Name: "metal", Mode: "metal"},
	}}}, nil
}

func (m fakeTalosMachine) Upgrade(ctx context.Context, req *machineapi.UpgradeRequest) (*machineapi.UpgradeResponse, error) {
	node, err := m.f.enter(ctx, "Upgrade")
	if err != nil {
		return nil, err
	}

	if m.f.upgrade == nil {
		return nil, status.Error(codes.Unimplemented, "unknown method Upgrade for service machine.MachineService")
	}

	if err := m.f.upgrade(node, req); err != nil {
		return nil, err
	}

	return &machineapi.UpgradeResponse{Messages: []*machineapi.Upgrade{{Ack: "upgrade started"}}}, nil
}

func (m fakeTalosMachine) Reboot(ctx context.Context, _ *machineapi.RebootRequest) (*machineapi.RebootResponse, error) {
	if _, err := m.f.enter(ctx, "Reboot"); err != nil {
		return nil, err
	}

	if m.f.rebootErr != nil {
		return nil, m.f.rebootErr
	}

	return &machineapi.RebootResponse{Messages: []*machineapi.Reboot{{}}}, nil
}

func (m fakeTalosMachine) ImagePull(ctx context.Context, req *machineapi.ImagePullRequest) (*machineapi.ImagePullResponse, error) {
	if m.f.imagePull == nil {
		return nil, status.Error(codes.Unimplemented, "unknown method ImagePull")
	}

	node, err := m.f.enter(ctx, "ImagePull")
	if err != nil {
		return nil, err
	}

	if err := m.f.imagePull(node, req); err != nil {
		return nil, err
	}

	return &machineapi.ImagePullResponse{Messages: []*machineapi.ImagePull{{}}}, nil
}

func (m fakeTalosMachine) Restart(ctx context.Context, req *machineapi.RestartRequest) (*machineapi.RestartResponse, error) {
	if _, err := m.f.enter(ctx, "Restart"); err != nil {
		return nil, err
	}

	m.f.mu.Lock()
	m.f.restarts = append(m.f.restarts, req)
	m.f.mu.Unlock()

	return &machineapi.RestartResponse{Messages: []*machineapi.Restart{{}}}, nil
}

// MetaWrite sets the node's MetaKey resource, as Talos does.
func (m fakeTalosMachine) MetaWrite(ctx context.Context, req *machineapi.MetaWriteRequest) (*machineapi.MetaWriteResponse, error) {
	node, err := m.f.enter(ctx, "MetaWrite")
	if err != nil {
		return nil, err
	}

	k := runtime.NewMetaKey(runtime.NamespaceName, runtime.MetaKeyTagToID(uint8(req.GetKey())))
	k.TypedSpec().Value = string(req.GetValue())
	m.f.put(node, k)

	return &machineapi.MetaWriteResponse{Messages: []*machineapi.MetaWrite{{}}}, nil
}

// MetaDelete removes the node's MetaKey resource.
func (m fakeTalosMachine) MetaDelete(ctx context.Context, req *machineapi.MetaDeleteRequest) (*machineapi.MetaDeleteResponse, error) {
	node, err := m.f.enter(ctx, "MetaDelete")
	if err != nil {
		return nil, err
	}

	id := runtime.MetaKeyTagToID(uint8(req.GetKey()))

	m.f.mu.Lock()
	m.f.resources[node] = slices.DeleteFunc(m.f.resources[node], func(r resource.Resource) bool {
		return r.Metadata().Type() == runtime.MetaKeyType && r.Metadata().ID() == id
	})
	m.f.mu.Unlock()

	return &machineapi.MetaDeleteResponse{Messages: []*machineapi.MetaDelete{{}}}, nil
}

func (m fakeTalosMachine) Reset(ctx context.Context, req *machineapi.ResetRequest) (*machineapi.ResetResponse, error) {
	if _, err := m.f.enter(ctx, "Reset"); err != nil {
		return nil, err
	}

	m.f.mu.Lock()
	m.f.resets = append(m.f.resets, req)
	m.f.mu.Unlock()

	return &machineapi.ResetResponse{Messages: []*machineapi.Reset{{ActorId: "reset-1"}}}, nil
}

// fakeTalosTime answers `talosctl time` with the node's clockOffsets entry.
type fakeTalosTime struct {
	timeapi.UnimplementedTimeServiceServer

	f *fakeTalos
}

func (t fakeTalosTime) Time(ctx context.Context, _ *emptypb.Empty) (*timeapi.TimeResponse, error) {
	node, err := t.f.enter(ctx, "Time")
	if err != nil {
		return nil, err
	}

	t.f.mu.Lock()
	offset, ok := t.f.clockOffsets[node]
	t.f.mu.Unlock()

	if !ok {
		return nil, status.Error(codes.Unimplemented, "no time")
	}

	local := time.Unix(1700000000, 0)

	return &timeapi.TimeResponse{Messages: []*timeapi.Time{{
		Server:     "time.example.test",
		Localtime:  timestamppb.New(local),
		Remotetime: timestamppb.New(local.Add(time.Duration(offset) * time.Millisecond)),
	}}}, nil
}

func (m fakeTalosMachine) PacketCapture(_ *machineapi.PacketCaptureRequest, stream grpc.ServerStreamingServer[common.Data]) error {
	node, err := m.f.enter(stream.Context(), "PacketCapture")
	if err != nil {
		return err
	}

	m.f.mu.Lock()
	data, hold := m.f.pcaps[node], m.f.pcapHold[node]
	m.f.mu.Unlock()

	if err := stream.Send(&common.Data{Bytes: data}); err != nil {
		return err
	}

	if hold {
		<-stream.Context().Done()
	}

	return nil
}

func (m fakeTalosMachine) EtcdSnapshot(_ *machineapi.EtcdSnapshotRequest, stream grpc.ServerStreamingServer[common.Data]) error {
	if _, err := m.f.enter(stream.Context(), "EtcdSnapshot"); err != nil {
		return err
	}

	return m.f.snapshot(stream)
}

func (m fakeTalosMachine) EtcdAlarmDisarm(ctx context.Context, _ *emptypb.Empty) (*machineapi.EtcdAlarmDisarmResponse, error) {
	if _, err := m.f.enter(ctx, "EtcdAlarmDisarm"); err != nil {
		return nil, err
	}

	if m.f.disarmErr != nil {
		return nil, m.f.disarmErr
	}

	m.f.mu.Lock()
	m.f.alarms = nil
	m.f.mu.Unlock()

	return &machineapi.EtcdAlarmDisarmResponse{Messages: []*machineapi.EtcdAlarmDisarm{{}}}, nil
}

func (m fakeTalosMachine) EtcdAlarmList(ctx context.Context, _ *emptypb.Empty) (*machineapi.EtcdAlarmListResponse, error) {
	if _, err := m.f.enter(ctx, "EtcdAlarmList"); err != nil {
		return nil, err
	}

	m.f.mu.Lock()
	defer m.f.mu.Unlock()

	return &machineapi.EtcdAlarmListResponse{Messages: []*machineapi.EtcdAlarm{{MemberAlarms: m.f.alarms}}}, nil
}

func (m fakeTalosMachine) EtcdDefragment(ctx context.Context, _ *emptypb.Empty) (*machineapi.EtcdDefragmentResponse, error) {
	node, err := m.f.enter(ctx, "EtcdDefragment")
	if err != nil {
		return nil, err
	}

	if err := m.f.defragErr[node]; err != nil {
		return nil, err
	}

	return &machineapi.EtcdDefragmentResponse{Messages: []*machineapi.EtcdDefragment{{}}}, nil
}

func (m fakeTalosMachine) EtcdStatus(ctx context.Context, _ *emptypb.Empty) (*machineapi.EtcdStatusResponse, error) {
	node, err := m.f.enter(ctx, "EtcdStatus")
	if err != nil {
		return nil, err
	}

	m.f.mu.Lock()
	defer m.f.mu.Unlock()

	if m.f.etcdDown[node] {
		return nil, status.Errorf(codes.Unavailable, "etcd is not running on %s", node)
	}

	id, leader := uint64(1), uint64(1)
	if m.f.members != nil {
		id, leader = m.f.members[node], m.f.leader
	}

	return &machineapi.EtcdStatusResponse{Messages: []*machineapi.EtcdStatus{{
		MemberStatus: &machineapi.EtcdMemberStatus{MemberId: id, Leader: leader, DbSize: 4 << 20, Errors: m.f.etcdErrors[node]},
	}}}, nil
}

// EtcdRecover keeps the uploaded database of the node.
func (m fakeTalosMachine) EtcdRecover(stream grpc.ClientStreamingServer[common.Data, machineapi.EtcdRecoverResponse]) error {
	node, err := m.f.enter(stream.Context(), "EtcdRecover")
	if err != nil {
		return err
	}

	var data []byte

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return err
		}

		data = append(data, msg.GetBytes()...)
	}

	m.f.mu.Lock()
	if m.f.recovered == nil {
		m.f.recovered = map[string][]byte{}
	}

	m.f.recovered[node] = data
	m.f.mu.Unlock()

	return stream.SendAndClose(&machineapi.EtcdRecoverResponse{Messages: []*machineapi.EtcdRecover{{}}})
}

// Bootstrap starts etcd on the node again.
func (m fakeTalosMachine) Bootstrap(ctx context.Context, req *machineapi.BootstrapRequest) (*machineapi.BootstrapResponse, error) {
	node, err := m.f.enter(ctx, "Bootstrap")
	if err != nil {
		return nil, err
	}

	m.f.mu.Lock()
	defer m.f.mu.Unlock()

	m.f.bootstraps = append(m.f.bootstraps, req)
	delete(m.f.etcdDown, node)

	return &machineapi.BootstrapResponse{Messages: []*machineapi.Bootstrap{{}}}, nil
}

// Kubeconfig streams the node's admin kubeconfig, gzip+tar wrapped as Talos does (the
// client unwraps it): the bytes come from fake.kubeconfigs.
func (m fakeTalosMachine) Kubeconfig(_ *emptypb.Empty, stream grpc.ServerStreamingServer[common.Data]) error {
	node, err := m.f.enter(stream.Context(), "Kubeconfig")
	if err != nil {
		return err
	}

	m.f.mu.Lock()
	kube, ok := m.f.kubeconfigs[nodeKey(node)]
	m.f.mu.Unlock()

	if !ok {
		return status.Error(codes.Unavailable, "the Kubernetes API server is not up on "+node)
	}

	var buf bytes.Buffer

	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	if err := tw.WriteHeader(&tar.Header{Name: "kubeconfig", Mode: 0o600, Size: int64(len(kube))}); err != nil {
		return err
	}

	if _, err := tw.Write(kube); err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return err
	}

	if err := zw.Close(); err != nil {
		return err
	}

	return stream.Send(&common.Data{Bytes: buf.Bytes()})
}

func (m fakeTalosMachine) EtcdMemberList(ctx context.Context, _ *machineapi.EtcdMemberListRequest) (*machineapi.EtcdMemberListResponse, error) {
	node, err := m.f.enter(ctx, "EtcdMemberList")
	if err != nil {
		return nil, err
	}

	m.f.mu.Lock()
	defer m.f.mu.Unlock()

	members := m.f.members
	if members == nil {
		members = map[string]uint64{node: 1}
	}

	var out []*machineapi.EtcdMember
	for n, id := range members {
		out = append(out, &machineapi.EtcdMember{
			Id: id, Hostname: "host-" + strings.ReplaceAll(n, ".", "-"),
			PeerUrls: []string{"https://" + n + ":2380"}, ClientUrls: []string{"https://" + n + ":2379"},
		})
	}

	slices.SortFunc(out, func(a, b *machineapi.EtcdMember) int { return int(a.GetId()) - int(b.GetId()) })

	return &machineapi.EtcdMemberListResponse{Messages: []*machineapi.EtcdMembers{{Members: out}}}, nil
}

func (m fakeTalosMachine) EtcdRemoveMemberByID(ctx context.Context, req *machineapi.EtcdRemoveMemberByIDRequest) (*machineapi.EtcdRemoveMemberByIDResponse, error) {
	if _, err := m.f.enter(ctx, fmt.Sprintf("EtcdRemoveMemberByID %x", req.GetMemberId())); err != nil {
		return nil, err
	}

	return &machineapi.EtcdRemoveMemberByIDResponse{Messages: []*machineapi.EtcdRemoveMemberByID{{}}}, nil
}

func (m fakeTalosMachine) EtcdForfeitLeadership(ctx context.Context, _ *machineapi.EtcdForfeitLeadershipRequest) (*machineapi.EtcdForfeitLeadershipResponse, error) {
	if _, err := m.f.enter(ctx, "EtcdForfeitLeadership"); err != nil {
		return nil, err
	}

	return &machineapi.EtcdForfeitLeadershipResponse{Messages: []*machineapi.EtcdForfeitLeadership{{Member: "host-new-leader"}}}, nil
}

func (m fakeTalosMachine) ServiceList(ctx context.Context, _ *emptypb.Empty) (*machineapi.ServiceListResponse, error) {
	if _, err := m.f.enter(ctx, "ServiceList"); err != nil {
		return nil, err
	}

	return &machineapi.ServiceListResponse{Messages: []*machineapi.ServiceList{{Services: []*machineapi.ServiceInfo{
		{Id: "apid", State: "Running", Health: &machineapi.ServiceHealth{Healthy: true}},
		{Id: "kubelet", State: "Running", Health: &machineapi.ServiceHealth{Healthy: false, LastMessage: "probe failed"}},
	}}}}, nil
}

func (m fakeTalosMachine) Logs(req *machineapi.LogsRequest, stream grpc.ServerStreamingServer[common.Data]) error {
	if _, err := m.f.enter(stream.Context(), "Logs"); err != nil {
		return err
	}

	if req.GetId() == m.f.failLogs {
		return status.Error(codes.NotFound, "log not found")
	}

	return stream.Send(&common.Data{Bytes: []byte("log line of " + req.GetId() + "\n")})
}

func (m fakeTalosMachine) Dmesg(_ *machineapi.DmesgRequest, stream grpc.ServerStreamingServer[common.Data]) error {
	if _, err := m.f.enter(stream.Context(), "Dmesg"); err != nil {
		return err
	}

	return stream.Send(&common.Data{Bytes: []byte("[    0.000000] Linux version 6.12\n")})
}

func (m fakeTalosMachine) Containers(ctx context.Context, req *machineapi.ContainersRequest) (*machineapi.ContainersResponse, error) {
	if _, err := m.f.enter(ctx, "Containers"); err != nil {
		return nil, err
	}

	if req.GetNamespace() == "system" {
		if req.GetDriver() != common.ContainerDriver_CONTAINERD {
			return nil, status.Error(codes.InvalidArgument, "system containers are listed through containerd")
		}

		return &machineapi.ContainersResponse{Messages: []*machineapi.Container{{Containers: []*machineapi.ContainerInfo{
			{Id: "trustd", PodId: "trustd", Name: "trustd", Image: "talos/trustd", Status: "RUNNING", Pid: 104},
			{Id: "apid", PodId: "apid", Image: "talos/apid", Status: "RUNNING", Pid: 102},
		}}}}, nil
	}

	return &machineapi.ContainersResponse{Messages: []*machineapi.Container{{Containers: []*machineapi.ContainerInfo{
		{Id: "c1", PodId: "kube-system/kube-apiserver-cp", Name: "kube-apiserver", Image: "registry.k8s.io/kube-apiserver:v1.34.0", Status: "CONTAINER_RUNNING"},
		{Id: "c2", PodId: "default/web-1", Name: "web", Image: "nginx:1.27", Status: "CONTAINER_RUNNING"},
		{Id: "kube-system/kube-apiserver-cp", PodId: "kube-system/kube-apiserver-cp", Image: "registry.k8s.io/pause:3.10"},
	}}}}, nil
}

func (m fakeTalosMachine) Mounts(ctx context.Context, _ *emptypb.Empty) (*machineapi.MountsResponse, error) {
	node, err := m.f.enter(ctx, "Mounts")
	if err != nil {
		return nil, err
	}

	m.f.mu.Lock()
	stats, scripted := m.f.mounts[node]
	m.f.mu.Unlock()

	if scripted {
		return &machineapi.MountsResponse{Messages: []*machineapi.Mounts{{Stats: stats}}}, nil
	}

	return &machineapi.MountsResponse{Messages: []*machineapi.Mounts{{Stats: []*machineapi.MountStat{
		{Filesystem: "/dev/sda6", Size: 100 << 30, Available: 60 << 30, MountedOn: "/var"},
	}}}}, nil
}

func (m fakeTalosMachine) Processes(ctx context.Context, _ *emptypb.Empty) (*machineapi.ProcessesResponse, error) {
	if _, err := m.f.enter(ctx, "Processes"); err != nil {
		return nil, err
	}

	return &machineapi.ProcessesResponse{Messages: []*machineapi.Process{{Processes: []*machineapi.ProcessInfo{
		{Pid: 1, State: "S", Threads: 3, Command: "machined", Args: "/sbin/init"},
	}}}}, nil
}

// DiskUsage walks a fixed tree below the requested path: one directory holding one file.
func (m fakeTalosMachine) DiskUsage(req *machineapi.DiskUsageRequest, stream grpc.ServerStreamingServer[machineapi.DiskUsageInfo]) error {
	if _, err := m.f.enter(stream.Context(), "DiskUsage"); err != nil {
		return err
	}

	root := req.GetPaths()[0]

	for _, e := range []*machineapi.DiskUsageInfo{
		{Name: root, Size: 3000},
		{Name: root + "/logs", Size: 2000},
		{Name: root + "/logs/app.log", Size: 2000},
		{Name: root + "/config", Size: 1000},
	} {
		if err := stream.Send(e); err != nil {
			return err
		}
	}

	return nil
}

func (m fakeTalosMachine) List(req *machineapi.ListRequest, stream grpc.ServerStreamingServer[machineapi.FileInfo]) error {
	node, err := m.f.enter(stream.Context(), "List")
	if err != nil {
		return err
	}

	m.f.mu.Lock()
	tree, scripted := m.f.files[node]
	m.f.mu.Unlock()

	if !scripted {
		return stream.Send(&machineapi.FileInfo{Name: req.GetRoot() + "/logs", IsDir: true})
	}

	children := fakeChildren(tree, req.GetRoot())
	if children == nil {
		return status.Errorf(codes.Unknown, "lstat %s: no such file or directory", req.GetRoot())
	}

	// Like Talos, the root comes first as ".", then its direct children (no recursion).
	if err := stream.Send(&machineapi.FileInfo{Name: req.GetRoot(), RelativeName: ".", IsDir: true}); err != nil {
		return err
	}

	for _, name := range slices.Sorted(maps.Keys(children)) {
		if err := stream.Send(&machineapi.FileInfo{Name: req.GetRoot() + "/" + name, RelativeName: name, IsDir: children[name]}); err != nil {
			return err
		}
	}

	return nil
}

// fakeChildren are the entries directly below dir in tree (name -> is a directory), nil
// when dir does not exist.
func fakeChildren(tree map[string]string, dir string) map[string]bool {
	var out map[string]bool

	for p := range tree {
		rest, ok := strings.CutPrefix(p, dir+"/")
		if !ok {
			continue
		}

		if out == nil {
			out = map[string]bool{}
		}

		name, _, isDir := strings.Cut(rest, "/")
		out[name] = out[name] || isDir
	}

	return out
}

func (m fakeTalosMachine) Read(req *machineapi.ReadRequest, stream grpc.ServerStreamingServer[common.Data]) error {
	node, err := m.f.enter(stream.Context(), "Read")
	if err != nil {
		return err
	}

	m.f.mu.Lock()
	content, ok := m.f.files[node][req.GetPath()]
	m.f.mu.Unlock()

	if !ok {
		return status.Errorf(codes.Unknown, "open %s: no such file or directory", req.GetPath())
	}

	return stream.Send(&common.Data{Bytes: []byte(content)})
}

// Copy sends the node's files entry as is: a test stores there the tar.gz Talos would send.
func (m fakeTalosMachine) Copy(req *machineapi.CopyRequest, stream grpc.ServerStreamingServer[common.Data]) error {
	node, err := m.f.enter(stream.Context(), "Copy")
	if err != nil {
		return err
	}

	m.f.mu.Lock()
	content, ok := m.f.files[node][req.GetRootPath()]
	m.f.mu.Unlock()

	if !ok {
		return status.Errorf(codes.Unknown, "lstat %s: no such file or directory", req.GetRootPath())
	}

	return stream.Send(&common.Data{Bytes: []byte(content)})
}

// Events hands the stream to the events script with the nodes it targets ("nodes"
// metadata, as client.WithNodes sends, else "node").
func (m fakeTalosMachine) Events(req *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error {
	md, _ := metadata.FromIncomingContext(stream.Context())

	nodes := md.Get("nodes")
	if len(nodes) == 0 {
		nodes = md.Get("node")
	}

	m.f.mu.Lock()
	m.f.calls = append(m.f.calls, "Events "+strings.Join(nodes, ","))
	script := m.f.events
	m.f.mu.Unlock()

	if script == nil {
		return status.Error(codes.Unimplemented, "unknown method Events for service machine.MachineService")
	}

	return script(nodes, req, stream)
}

func (m fakeTalosMachine) ApplyConfiguration(ctx context.Context, req *machineapi.ApplyConfigurationRequest) (*machineapi.ApplyConfigurationResponse, error) {
	// The hold goes before anything at all is recorded (the call included): a run
	// cancelled while held leaves the fake untouched.
	m.f.waitHold(ctx, m.f.applyHold)

	node, err := m.f.enter(ctx, "ApplyConfiguration")
	if err != nil {
		return nil, err
	}

	provider, err := configloader.NewFromBytes(req.GetData())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid config: %v", err)
	}

	m.f.mu.Lock()
	m.f.applies = append(m.f.applies, fakeConfigApply{node: node, dryRun: req.GetDryRun(), mode: req.GetMode()})
	onApply := m.f.onApply
	applied := m.f.applied
	m.f.mu.Unlock()

	if !req.GetDryRun() {
		if applied != nil {
			select {
			case applied <- string(req.GetData()):
			default:
			}
		}

		m.f.put(node, config.NewMachineConfigWithID(provider, config.ActiveID))

		if onApply != nil {
			onApply(node)
		}
	}

	return &machineapi.ApplyConfigurationResponse{Messages: []*machineapi.ApplyConfiguration{{Mode: machineapi.ApplyConfigurationRequest_NO_REBOOT}}}, nil
}

func (m fakeTalosMachine) CPUFreqStats(ctx context.Context, _ *emptypb.Empty) (*machineapi.CPUFreqStatsResponse, error) {
	node, err := m.f.enter(ctx, "CPUFreqStats")
	if err != nil {
		return nil, err
	}

	m.f.mu.Lock()
	stats, ok := m.f.cpuFreq[node]
	m.f.mu.Unlock()

	if !ok {
		return nil, status.Error(codes.Unimplemented, "unknown method CPUFreqStats for service machine.MachineService")
	}

	return &machineapi.CPUFreqStatsResponse{Messages: []*machineapi.CPUsFreqStats{{CpuFreqStats: stats}}}, nil
}
