package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
)

const (
	// unhealthyPodsPerNamespace is how many namespaces unhealthyPods reads one by one; past
	// it one cluster-wide list costs fewer calls.
	unhealthyPodsPerNamespace = 16
	// unhealthyPodsParallel bounds the namespaces read at once.
	unhealthyPodsParallel = 4
	// unfinishedPods leaves out the pods that ran to completion (healthy by definition).
	unfinishedPods = "status.phase!=Succeeded"
)

// unhealthyPods reads the pods of namespaces that are not healthy, for the Argo CD and Flux
// apps in trouble: page by page, without the finished ones, and namespace by namespace when
// they are few, so a large cluster is never read whole (one that fails is skipped unless all
// do). In namespace then name order, as listPods.
func unhealthyPods(ctx context.Context, k *kubeClient, namespaces []string) ([]kubePod, error) {
	wanted := map[string]bool{}

	for _, ns := range namespaces {
		if ns != "" {
			wanted[ns] = true
		}
	}

	if len(wanted) == 0 {
		return []kubePod{}, nil
	}

	if len(wanted) > unhealthyPodsPerNamespace {
		pods, err := readUnhealthyPods(ctx, k, "")

		return slices.DeleteFunc(pods, func(p kubePod) bool { return !wanted[p.Namespace] }), err
	}

	sorted := slices.Sorted(maps.Keys(wanted))
	results := make([][]kubePod, len(sorted))
	errs := make([]error, len(sorted))
	forEachLimit(sorted, unhealthyPodsParallel, func(i int, ns string) {
		results[i], errs[i] = readUnhealthyPods(ctx, k, ns)
	})

	// A namespace the user may not read leaves the others shown.
	if !slices.Contains(errs, nil) {
		return nil, errors.Join(errs...)
	}

	return slices.Concat(results...), nil
}

// readUnhealthyPods reads every page of the unfinished pods of namespace ("" for all) and
// keeps those not healthy, sorted.
func readUnhealthyPods(ctx context.Context, k *kubeClient, namespace string) ([]kubePod, error) {
	pods := []kubePod{}

	err := k.listAll(ctx, scopedPath("/api/v1", namespace, "pods"), pageQuery{fieldSelector: unfinishedPods}, func() { pods = pods[:0] }, func(page kubePage) error {
		mapped, err := mapPodPage(page)
		for _, p := range mapped {
			if !p.Healthy {
				pods = append(pods, p)
			}
		}

		return err
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(pods, func(a, b kubePod) int {
		return cmp.Or(strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name))
	})

	return pods, nil
}
