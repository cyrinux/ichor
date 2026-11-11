package ichorgo

import (
	"context"

	"github.com/cyrinux/ichor/go/kube"
)

// orEmpty is s, or an empty slice for nil (so it encodes as [], not null).
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}

	return s
}

// getList reads every item of the list at path into out, page by page: see kube.GetList.
func getList[T any](ctx context.Context, k *kubeClient, path string, out *kubeList[T]) error {
	return kube.GetList(ctx, k.api(), path, out)
}
