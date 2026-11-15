package ichorgo

import (
	"context"
	"slices"

	cosiv1alpha1 "github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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

func (c fakeTalosCluster) HealthCheck(req *clusterapi.HealthCheckRequest, stream grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error {
	node, err := c.f.enter(stream.Context(), "HealthCheck")
	if err != nil {
		return err
	}

	c.f.mu.Lock()
	c.f.healthInfos = append(c.f.healthInfos, req.GetClusterInfo())
	c.f.mu.Unlock()

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
	s.f.waitHold(ctx, s.f.holdFor(ctx, s.f.getHold))

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
