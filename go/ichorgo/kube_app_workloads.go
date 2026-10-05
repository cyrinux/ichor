package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
)

// appWorkloadsParallel bounds the pods and workloads KubeAppWorkloads reads at once.
const appWorkloadsParallel = 4

// workloadRef is one Deployment, StatefulSet or DaemonSet by kind, namespace and name.
type workloadRef struct {
	kind      workloadKind
	namespace string
	name      string
}

// KubeAppWorkloads finds the Deployments, StatefulSets and DaemonSets running the given pods
// (an app's, from the inventory; os:admin) through each pod's owner: a StatefulSet or DaemonSet
// directly, a Deployment through its ReplicaSet, named <deployment>-<pod-template-hash>. Pods
// without such an owner (static, Job, bare) and pods gone since are left out. Only what the
// pods lead to is read, never a cluster-wide list: the pods one by one when a namespace holds
// few of them (routePodsByName), else that namespace's list page by page; then each owner.
// pods: [{namespace,pod}]. {"workloads":[...as KubeWorkloads]}, in namespace, name, kind
// order. kubeServer: see KubePods.
func KubeAppWorkloads(configYAML, contextName, kubeServer, pods string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	refs, err := decodeRoutePods(pods)
	if err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubeWorkloadList { return demoAppWorkloads(refs) },
		func(ctx context.Context, k *kubeClient) (kubeWorkloadList, error) {
			list, err := appWorkloads(ctx, k, refs)
			learnWorkloadNames(list.Workloads)

			return list, err
		})
}

// KubeWorkloadsNamed reads the given Deployments, StatefulSets and DaemonSets as they are now
// (os:admin): those KubeAppWorkloads found, again after a restart (their pods replaced, the
// inventory's names no longer lead to them). One deleted since is left out. workloads:
// [{kind,namespace,name}]. {"workloads":[...as KubeWorkloads]}, in namespace, name, kind order.
// kubeServer: see KubePods.
func KubeWorkloadsNamed(configYAML, contextName, kubeServer, workloads string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	var named []struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
	}

	if err := json.Unmarshal([]byte(workloads), &named); err != nil {
		return "", fmt.Errorf("invalid workload list: %w", err)
	}

	refs := make([]workloadRef, 0, len(named))

	for _, n := range named {
		wk, err := findWorkloadKind(n.Kind)
		if err != nil {
			return "", err
		}

		ref := workloadRef{wk, privacy.revealNamespace(strings.TrimSpace(n.Namespace)), privacy.revealName(strings.TrimSpace(n.Name))}
		if err := validateKubeName("workload", ref.namespace, ref.name); err != nil {
			return "", err
		}

		refs = append(refs, ref)
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubeWorkloadList { return demoNamedWorkloads(refs) },
		func(ctx context.Context, k *kubeClient) (kubeWorkloadList, error) {
			list, err := readWorkloads(ctx, k, refs)
			learnWorkloadNames(list)

			return kubeWorkloadList{Workloads: list}, err
		})
}

func appWorkloads(ctx context.Context, k *kubeClient, pods []routePod) (kubeWorkloadList, error) {
	wanted := map[string]map[string]bool{} // namespace -> pod names

	for _, p := range pods {
		if validateKubeName("pod", p.Namespace, p.Pod) != nil {
			continue
		}

		if wanted[p.Namespace] == nil {
			wanted[p.Namespace] = map[string]bool{}
		}

		wanted[p.Namespace][p.Pod] = true
	}

	var owners []kubePod

	for _, ns := range slices.Sorted(maps.Keys(wanted)) {
		found, err := readOwnedPods(ctx, k, ns, wanted[ns])
		if err != nil {
			return kubeWorkloadList{}, err
		}

		owners = append(owners, found...)
	}

	workloads, err := readWorkloads(ctx, k, workloadRefsOf(owners))
	if err != nil {
		return kubeWorkloadList{}, err
	}

	return kubeWorkloadList{Workloads: workloads}, nil
}

// readOwnedPods reads the pods names of namespace with their owner: one GET each when they are
// few (a pod gone since is skipped), else the namespace's Table page by page.
func readOwnedPods(ctx context.Context, k *kubeClient, namespace string, names map[string]bool) ([]kubePod, error) {
	if len(names) > routePodsByName {
		var pods []kubePod

		err := k.listAll(ctx, scopedPath("/api/v1", namespace, "pods"), pageQuery{table: true}, func() { pods = nil }, func(page kubePage) error {
			mapped, err := mapPodPage(page)
			for _, p := range mapped {
				if names[p.Name] {
					pods = append(pods, p)
				}
			}

			return err
		})

		return pods, err
	}

	return readEach(ctx, slices.Sorted(maps.Keys(names)), func(ctx context.Context, name string) (kubePod, error) {
		var obj podObject
		err := k.get(ctx, podPath(namespace, name), &obj)

		return mapPod(obj), err
	})
}

// readWorkloads reads each workload of refs; one gone since is left out.
func readWorkloads(ctx context.Context, k *kubeClient, refs []workloadRef) ([]kubeWorkload, error) {
	workloads, err := readEach(ctx, refs, func(ctx context.Context, r workloadRef) (kubeWorkload, error) {
		var obj appsObject
		err := k.get(ctx, appsPath(r.kind, r.namespace, r.name), &obj)

		return mapWorkload(r.kind.kind, obj), err
	})

	slices.SortFunc(workloads, func(a, b kubeWorkload) int {
		return cmp.Or(strings.Compare(a.Namespace, b.Namespace), strings.Compare(a.Name, b.Name), strings.Compare(a.Kind, b.Kind))
	})

	return workloads, err
}

// readEach calls read for every key, appWorkloadsParallel at a time, and keeps what it found
// in the keys' order: a 404 is skipped; any other error cancels the reads left and is
// returned (the first one only).
func readEach[K, V any](ctx context.Context, keys []K, read func(context.Context, K) (V, error)) ([]V, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	values := make([]V, len(keys))
	found := make([]bool, len(keys))
	slots := make(chan struct{}, appWorkloadsParallel)

	var wg sync.WaitGroup

	for i, key := range keys {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}

			defer func() { <-slots }()

			if ctx.Err() != nil {
				return
			}

			v, err := read(ctx, key)

			switch {
			case err == nil:
				values[i], found[i] = v, true
			case !isNotFound(err):
				cancel(err) // only the first cause is kept
			}
		})
	}

	wg.Wait()

	if err := context.Cause(ctx); err != nil {
		return nil, err
	}

	out := make([]V, 0, len(keys))

	for i, v := range values {
		if found[i] {
			out = append(out, v)
		}
	}

	return out, nil
}

// workloadRefsOf are the workloads the owners of pods stand for, each once.
func workloadRefsOf(pods []kubePod) []workloadRef {
	seen := map[workloadRef]bool{}
	refs := []workloadRef{}

	for _, p := range pods {
		ref, ok := ownerWorkload(p.Namespace, p.Owner)
		if ok && !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}

	return refs
}

// ownerWorkload is the workload an owner reference ("ReplicaSet/web-5d8f") stands for; false
// for other kinds.
func ownerWorkload(namespace, owner string) (workloadRef, bool) {
	kind, name, _ := strings.Cut(owner, "/")
	if name == "" {
		return workloadRef{}, false
	}

	switch kind {
	case "StatefulSet", "DaemonSet":
		wk, err := findWorkloadKind(kind)

		return workloadRef{wk, namespace, name}, err == nil
	case "ReplicaSet":
		hash := strings.LastIndex(name, "-")
		if hash <= 0 {
			return workloadRef{}, false
		}

		return workloadRef{workloadKinds[0], namespace, name[:hash]}, true
	default:
		return workloadRef{}, false
	}
}

// demoAppWorkloads are the demo workloads owning pods.
func demoAppWorkloads(pods []routePod) kubeWorkloadList {
	wanted := map[string]bool{}
	for _, p := range pods {
		wanted[p.Namespace+"/"+p.Pod] = true
	}

	var owners []kubePod

	for _, p := range demoPods() {
		if wanted[p.Namespace+"/"+p.Name] {
			owners = append(owners, p)
		}
	}

	return demoNamedWorkloads(workloadRefsOf(owners))
}

// demoNamedWorkloads are the demo workloads of refs.
func demoNamedWorkloads(refs []workloadRef) kubeWorkloadList {
	wanted := map[string]bool{}
	for _, r := range refs {
		wanted[r.kind.kind+"/"+r.namespace+"/"+r.name] = true
	}

	out := []kubeWorkload{}

	for _, w := range demoKubeWorkloads() {
		if wanted[w.Kind+"/"+w.Namespace+"/"+w.Name] {
			out = append(out, w)
		}
	}

	return kubeWorkloadList{Workloads: out}
}
