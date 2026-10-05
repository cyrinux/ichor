package ichorgo

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// getList reads every item of the list at path into out, page by page (kubePageLimit items
// a page), so that a large list never meets kubeMaxBody: the drop-in for k.get on a
// collection. path may carry a labelSelector and a fieldSelector query; other parameters
// are refused. A 410 Gone mid-way starts the list again (listAll). The errors are k.get's:
// a 404 on the first page is still isNotFound.
func getList[T any](ctx context.Context, k *kubeClient, path string, out *kubeList[T]) error {
	path, rawQuery, _ := strings.Cut(path, "?")

	q, err := listQuery(rawQuery)
	if err != nil {
		return err
	}

	items := []T{}

	err = k.listAll(ctx, path, q, func() { items = items[:0] }, func(page kubePage) error {
		objs, err := decodeItems[T](page)
		items = append(items, objs...)

		return err
	})
	if err != nil {
		return err
	}

	out.Items = items

	return nil
}

// listQuery reads the selectors of a list path's query.
func listQuery(rawQuery string) (pageQuery, error) {
	if rawQuery == "" {
		return pageQuery{}, nil
	}

	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return pageQuery{}, fmt.Errorf("bad Kubernetes list query %q: %w", rawQuery, err)
	}

	for key := range values {
		if key != "labelSelector" && key != "fieldSelector" {
			return pageQuery{}, fmt.Errorf("unsupported Kubernetes list parameter %q", key)
		}
	}

	return pageQuery{labelSelector: values.Get("labelSelector"), fieldSelector: values.Get("fieldSelector")}, nil
}
