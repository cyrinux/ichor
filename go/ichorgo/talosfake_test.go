package ichorgo

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	cosiv1alpha1 "github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeTalos is the Talos API of a few nodes behind one endpoint: like apid, it routes each
// call by its "node" metadata. It serves the machine, image, lifecycle and cluster services
// and the COSI state; scripts replace the default answers where a test needs to.
type fakeTalos struct {
	mu        sync.Mutex
	versions  map[string]string              // node -> Talos tag; a node without one is down
	resources map[string][]resource.Resource // node -> its COSI resources
	calls     []string                       // "Method node", in order
	// members are the etcd members (node -> member id); nil: each node is its own only member.
	members map[string]uint64
	leader  uint64

	// Scripts (nil: the default answer).
	upgrade          func(node string, req *machineapi.UpgradeRequest) error
	pull             func(stream grpc.ServerStreamingServer[machineapi.ImageServicePullResponse]) error
	lifecycleUpgrade func(stream grpc.ServerStreamingServer[machineapi.LifecycleServiceUpgradeResponse]) error
	health           func(node string, stream grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error
	snapshot         func(stream grpc.ServerStreamingServer[common.Data]) error
	rebootErr        error
	resets           []*machineapi.ResetRequest // the Reset requests received, in order
	disarmErr        error
	// alarms are the active etcd alarms; a disarm clears them.
	alarms    []*machineapi.EtcdMemberAlarm
	defragErr map[string]error // node -> its defragmentation's error
	failLogs  string           // the service whose logs cannot be read
	// mounts are a node's mounted filesystems (none: /dev/sda6 on /var, 40 % used).
	mounts map[string][]*machineapi.MountStat
	// systemImages are the images of the system containerd namespace (the CRI one has pause).
	systemImages []*machineapi.ImageServiceListResponse
	// debugRun answers a debug container run of spec on node (nil: Unimplemented).
	debugRun func(node string, spec *machineapi.DebugContainerRunRequestSpec) (output string, exitCode int32)
}

func newFakeTalos() *fakeTalos {
	return &fakeTalos{versions: map[string]string{}, resources: map[string][]resource.Resource{}}
}

// addNode adds a running, ready node of the given type with its hostname.
func (f *fakeTalos) addNode(t *testing.T, node, version string, typ machine.Type) {
	t.Helper()

	mt := config.NewMachineType()
	mt.SetMachineType(typ)

	hs := network.NewHostnameStatus(network.NamespaceName, network.HostnameID)
	hs.TypedSpec().Hostname = "host-" + strings.ReplaceAll(node, ".", "-")

	f.mu.Lock()
	f.versions[node] = version
	f.mu.Unlock()

	f.put(node, mt, hs)
	f.setStage(node, runtime.MachineStageRunning, true)
}

// put adds or replaces resources of node.
func (f *fakeTalos) put(node string, rs ...resource.Resource) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, r := range rs {
		list := slices.DeleteFunc(f.resources[node], func(old resource.Resource) bool {
			return old.Metadata().Type() == r.Metadata().Type() && old.Metadata().ID() == r.Metadata().ID()
		})
		f.resources[node] = append(list, r)
	}
}

func (f *fakeTalos) setStage(node string, stage runtime.MachineStage, ready bool) {
	ms := runtime.NewMachineStatus()
	ms.TypedSpec().Stage = stage
	ms.TypedSpec().Status.Ready = ready
	f.put(node, ms)
}

func (f *fakeTalos) setVersion(node, version string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.versions[node] = version
}

// putMachineConfig gives node a generated machine config.
func (f *fakeTalos) putMachineConfig(t *testing.T, node string) {
	t.Helper()

	raw, err := generatedConfig()
	if err != nil {
		t.Fatal(err)
	}

	provider, err := configloader.NewFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}

	f.put(node, config.NewMachineConfigWithID(provider, config.ActiveID))
}

// enter records the call and tells the node it targets, or why it does not answer.
func (f *fakeTalos) enter(ctx context.Context, method string) (string, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	node := ""
	if v := md.Get("node"); len(v) > 0 {
		node = v[0]
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, method+" "+node)

	if _, up := f.versions[node]; !up {
		return node, status.Errorf(codes.Unavailable, "connection to %s refused", node)
	}

	return node, nil
}

func (f *fakeTalos) called(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []string

	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}

	return out
}

// start serves f over TLS on loopback and returns a talosconfig for nodes through it.
func (f *fakeTalos) start(t *testing.T, nodes ...string) string {
	t.Helper()

	endpoint, caB64 := startTLSServer(t, func(s *grpc.Server) {
		machineapi.RegisterMachineServiceServer(s, fakeTalosMachine{f: f})
		machineapi.RegisterImageServiceServer(s, fakeTalosImage{f: f})
		machineapi.RegisterLifecycleServiceServer(s, fakeTalosLifecycle{f: f})
		machineapi.RegisterDebugServiceServer(s, fakeTalosDebug{f: f})
		clusterapi.RegisterClusterServiceServer(s, fakeTalosCluster{f: f})
		cosiv1alpha1.RegisterStateServer(s, fakeTalosState{f: f})
	})

	var list strings.Builder
	for _, n := range nodes {
		fmt.Fprintf(&list, "            - %s\n", n)
	}

	return fmt.Sprintf("context: fake\ncontexts:\n    fake:\n        endpoints:\n            - %s\n        ca: %s\n        nodes:\n%s",
		endpoint, caB64, list.String())
}

type fakeTalosMachine struct {
	machineapi.UnimplementedMachineServiceServer

	f *fakeTalos
}

func (m fakeTalosMachine) Version(ctx context.Context, _ *emptypb.Empty) (*machineapi.VersionResponse, error) {
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

func (m fakeTalosMachine) Reset(ctx context.Context, req *machineapi.ResetRequest) (*machineapi.ResetResponse, error) {
	if _, err := m.f.enter(ctx, "Reset"); err != nil {
		return nil, err
	}

	m.f.mu.Lock()
	m.f.resets = append(m.f.resets, req)
	m.f.mu.Unlock()

	return &machineapi.ResetResponse{Messages: []*machineapi.Reset{{ActorId: "reset-1"}}}, nil
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

	id, leader := uint64(1), uint64(1)
	if m.f.members != nil {
		id, leader = m.f.members[node], m.f.leader
	}

	return &machineapi.EtcdStatusResponse{Messages: []*machineapi.EtcdStatus{{
		MemberStatus: &machineapi.EtcdMemberStatus{MemberId: id, Leader: leader, DbSize: 4 << 20},
	}}}, nil
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

func (m fakeTalosMachine) Containers(ctx context.Context, _ *machineapi.ContainersRequest) (*machineapi.ContainersResponse, error) {
	if _, err := m.f.enter(ctx, "Containers"); err != nil {
		return nil, err
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
	if _, err := m.f.enter(stream.Context(), "List"); err != nil {
		return err
	}

	return stream.Send(&machineapi.FileInfo{Name: req.GetRoot() + "/logs", IsDir: true})
}

type fakeTalosDebug struct {
	machineapi.UnimplementedDebugServiceServer

	f *fakeTalos
}

// ContainerRun reads the spec, then sends debugRun's output in two pieces and its exit code.
func (d fakeTalosDebug) ContainerRun(stream grpc.BidiStreamingServer[machineapi.DebugContainerRunRequest, machineapi.DebugContainerRunResponse]) error {
	node, err := d.f.enter(stream.Context(), "ContainerRun")
	if err != nil {
		return err
	}

	if d.f.debugRun == nil {
		return status.Error(codes.Unimplemented, "no debug containers")
	}

	req, err := stream.Recv()
	if err != nil {
		return err
	}

	output, code := d.f.debugRun(node, req.GetSpec())
	half := len(output) / 2

	for _, part := range []string{output[:half], output[half:]} {
		resp := &machineapi.DebugContainerRunResponse{Resp: &machineapi.DebugContainerRunResponse_StdoutData{StdoutData: []byte(part)}}
		if err := stream.Send(resp); err != nil {
			return err
		}
	}

	return stream.Send(&machineapi.DebugContainerRunResponse{Resp: &machineapi.DebugContainerRunResponse_ExitCode{ExitCode: code}})
}

type fakeTalosImage struct {
	machineapi.UnimplementedImageServiceServer

	f *fakeTalos
}

func (i fakeTalosImage) Pull(_ *machineapi.ImageServicePullRequest, stream grpc.ServerStreamingServer[machineapi.ImageServicePullResponse]) error {
	if _, err := i.f.enter(stream.Context(), "Pull"); err != nil {
		return err
	}

	if i.f.pull != nil {
		return i.f.pull(stream)
	}

	return stream.Send(&machineapi.ImageServicePullResponse{})
}

// List is the ImageService listing (MachineService.ImageList stays Unimplemented, like a
// Talos without the deprecated API).
// The system namespace holds systemImages.
func (i fakeTalosImage) List(req *machineapi.ImageServiceListRequest, stream grpc.ServerStreamingServer[machineapi.ImageServiceListResponse]) error {
	if _, err := i.f.enter(stream.Context(), "ImageList"); err != nil {
		return err
	}

	if req.GetContainerd().GetNamespace() == common.ContainerdNamespace_NS_SYSTEM {
		i.f.mu.Lock()
		images := slices.Clone(i.f.systemImages)
		i.f.mu.Unlock()

		for _, img := range images {
			if err := stream.Send(img); err != nil {
				return err
			}
		}

		return nil
	}

	return stream.Send(&machineapi.ImageServiceListResponse{Name: "registry.k8s.io/pause:3.10", Digest: "sha256:abc", Size: 320 << 10})
}

type fakeTalosLifecycle struct {
	machineapi.UnimplementedLifecycleServiceServer

	f *fakeTalos
}

func (l fakeTalosLifecycle) Upgrade(_ *machineapi.LifecycleServiceUpgradeRequest, stream grpc.ServerStreamingServer[machineapi.LifecycleServiceUpgradeResponse]) error {
	if _, err := l.f.enter(stream.Context(), "LifecycleUpgrade"); err != nil {
		return err
	}

	return l.f.lifecycleUpgrade(stream)
}

// installProgress is one LifecycleService.Upgrade answer: a message, or the exit code.
func installProgress(msg string, code int32, final bool) *machineapi.LifecycleServiceUpgradeResponse {
	p := &machineapi.LifecycleServiceInstallProgress{}
	if final {
		p.Response = &machineapi.LifecycleServiceInstallProgress_ExitCode{ExitCode: code}
	} else {
		p.Response = &machineapi.LifecycleServiceInstallProgress_Message{Message: msg}
	}

	return &machineapi.LifecycleServiceUpgradeResponse{Progress: p}
}

type fakeTalosCluster struct {
	clusterapi.UnimplementedClusterServiceServer

	f *fakeTalos
}

func (c fakeTalosCluster) HealthCheck(_ *clusterapi.HealthCheckRequest, stream grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error {
	node, err := c.f.enter(stream.Context(), "HealthCheck")
	if err != nil {
		return err
	}

	return c.f.health(node, stream)
}

type fakeTalosState struct {
	cosiv1alpha1.UnimplementedStateServer

	f *fakeTalos
}

func (s fakeTalosState) find(node, typ, id string) []resource.Resource {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()

	var out []resource.Resource

	for _, r := range s.f.resources[node] {
		if r.Metadata().Type() == typ && (id == "" || r.Metadata().ID() == id) {
			out = append(out, r)
		}
	}

	return out
}

func marshalResource(r resource.Resource) (*cosiv1alpha1.Resource, error) {
	p, err := protobuf.FromResource(r)
	if err != nil {
		return nil, err
	}

	return p.Marshal()
}

func (s fakeTalosState) Get(ctx context.Context, req *cosiv1alpha1.GetRequest) (*cosiv1alpha1.GetResponse, error) {
	node, err := s.f.enter(ctx, "Get "+req.GetType())
	if err != nil {
		return nil, err
	}

	found := s.find(node, req.GetType(), req.GetId())
	if len(found) == 0 {
		return nil, status.Errorf(codes.NotFound, "resource %s/%s not found", req.GetType(), req.GetId())
	}

	res, err := marshalResource(found[0])
	if err != nil {
		return nil, err
	}

	return &cosiv1alpha1.GetResponse{Resource: res}, nil
}

func (s fakeTalosState) List(req *cosiv1alpha1.ListRequest, stream grpc.ServerStreamingServer[cosiv1alpha1.ListResponse]) error {
	node, err := s.f.enter(stream.Context(), "List "+req.GetType())
	if err != nil {
		return err
	}

	for _, r := range s.find(node, req.GetType(), "") {
		res, err := marshalResource(r)
		if err != nil {
			return err
		}

		if err := stream.Send(&cosiv1alpha1.ListResponse{Resource: res}); err != nil {
			return err
		}
	}

	return nil
}
