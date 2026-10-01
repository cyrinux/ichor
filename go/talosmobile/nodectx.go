package talosmobile

import (
	"context"

	"github.com/siderolabs/talos/pkg/machinery/client"
)

func withNode(ctx context.Context, node string) context.Context {
	return client.WithNode(ctx, node)
}
