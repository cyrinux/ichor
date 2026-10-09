package ichorgo

import (
	"context"
	"errors"
	"io"
	"sort"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
)

type imageInfo struct {
	Name    string `json:"name"`
	Digest  string `json:"digest"`
	Size    int64  `json:"size"`    // bytes
	Created int64  `json:"created"` // unix ms, 0 when unknown
}

// NodeImages lists the container images in node's CRI (Kubernetes) containerd namespace,
// like `talosctl image list` (os:reader).
func NodeImages(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return demoRead("NodeImages", configYAML, contextName, node)
	}

	return withNodeSession(configYAML, contextName, node, callTimeout, func(ctx context.Context, s *session) (string, error) {
		msgs, err := listImagesLegacy(ctx, s.client)
		if isUnavailableAPI(err) {
			// MachineService.ImageList is deprecated for the ImageService (Talos 1.13+) and will go.
			msgs, err = listImagesService(ctx, s.client)
		}

		if err != nil {
			return "", s.friendlyErr(node, err)
		}

		return toJSON(mapImages(msgs))
	})
}

func listImagesLegacy(ctx context.Context, c *client.Client) ([]*machineapi.ImageListResponse, error) {
	//lint:ignore SA1019 the ImageService replacement is not in every supported Talos version
	stream, err := c.ImageList(ctx, common.ContainerdNamespace_NS_CRI)
	if err != nil {
		return nil, err
	}

	var msgs []*machineapi.ImageListResponse

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return msgs, nil
		}

		if err != nil {
			return nil, err
		}

		// The one-to-many proxy reports a node failure as metadata on a message.
		if e := metaError(msg.GetMetadata()); e != "" {
			return nil, errors.New(e)
		}

		msgs = append(msgs, msg)
	}
}

func listImagesService(ctx context.Context, c *client.Client) ([]*machineapi.ImageListResponse, error) {
	stream, err := c.ImageClient.List(ctx, &machineapi.ImageServiceListRequest{
		Containerd: &common.ContainerdInstance{Driver: common.ContainerDriver_CRI, Namespace: common.ContainerdNamespace_NS_CRI},
	})
	if err != nil {
		return nil, err
	}

	var msgs []*machineapi.ImageListResponse

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return msgs, nil
		}

		if err != nil {
			return nil, err
		}

		msgs = append(msgs, &machineapi.ImageListResponse{
			Name: msg.GetName(), Digest: msg.GetDigest(), Size: msg.GetSize(), CreatedAt: msg.GetCreatedAt(),
		})
	}
}

func mapImages(in []*machineapi.ImageListResponse) []imageInfo {
	out := make([]imageInfo, 0, len(in))

	for _, img := range in {
		info := imageInfo{Name: img.GetName(), Digest: img.GetDigest(), Size: img.GetSize()}
		if ts := img.GetCreatedAt(); ts != nil {
			info.Created = ts.AsTime().UnixMilli()
		}

		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	return out
}
