package talosmobile

import (
	"context"
	"errors"
	"io"
	"sort"

	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
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

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		stream, err := s.client.ImageList(withNode(ctx, node), common.ContainerdNamespace_NS_CRI) //nolint:staticcheck // the ImageService replacement is not in every supported Talos version
		if err != nil {
			return "", errors.New(friendlyError(err))
		}

		var msgs []*machineapi.ImageListResponse

		for {
			msg, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}

			if err != nil {
				return "", errors.New(friendlyError(err))
			}

			// The one-to-many proxy reports a node failure as metadata on a message.
			if e := msg.GetMetadata().GetError(); e != "" {
				return "", errors.New(e)
			}

			msgs = append(msgs, msg)
		}

		return toJSON(mapImages(msgs))
	})
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
