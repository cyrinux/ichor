package ichorgo

import (
	"context"

	"github.com/cyrinux/ichor/go/kube"
)

// getList reads every item of the list at path into out, page by page: see kube.GetList.
func getList[T any](ctx context.Context, k *kubeClient, path string, out *kubeList[T]) error {
	return kube.GetList(ctx, k.api(), path, out)
}
