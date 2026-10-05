package ichorgo

import (
	"context"
	"net/url"
	"strings"
)

// KubeWorkload reads one Deployment, StatefulSet or DaemonSet (kind) as KubeWorkloads maps it
// (os:admin): its replicas and state when the app shows it outside the Workloads list (a
// restart confirmation from Argo CD or Flux). kubeServer: see KubePods.
func KubeWorkload(configYAML, contextName, kubeServer, kind, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.revealName(strings.TrimSpace(name))

	wk, err := findWorkloadKind(kind)
	if err != nil {
		return "", err
	}

	if err := validateKubeName("workload", namespace, name); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubeWorkload {
			for _, w := range demoKubeWorkloads() {
				if w.Kind == wk.kind && w.Namespace == namespace && w.Name == name {
					return w
				}
			}

			return kubeWorkload{Kind: wk.kind, Namespace: namespace, Name: name, Images: []string{}}
		},
		func(ctx context.Context, k *kubeClient) (kubeWorkload, error) {
			var obj appsObject

			path := "/apis/apps/v1/namespaces/" + url.PathEscape(namespace) + "/" + wk.resource + "/" + url.PathEscape(name)
			if err := k.get(ctx, path, &obj); err != nil {
				return kubeWorkload{}, err
			}

			return mapWorkload(wk.kind, obj), nil
		})
}

// kubeWorkloadPage is one page of one workload kind, in the API server's order.
type kubeWorkloadPage struct {
	Workloads []kubeWorkload `json:"workloads"`
	pageCursor
}

// KubeWorkloadsPage lists one page of the Deployments, StatefulSets or DaemonSets (kind) of
// namespace ("" for every namespace), for the apps to load a large cluster page by page
// (os:admin): {"workloads":[...as KubeWorkloads],"continue","remaining","complete"}. The
// caller asks each kind and merges them. continueToken: the previous page's "continue", ""
// for the first page; limit 0 is 500. kubeServer: see KubePods.
//
// Always full objects, never a Table: the state (paused, observed generation, update
// strategy, old pods) and the last restart are not among the Table's columns, and the rows
// are few next to pods. Paging and the namespace still bound each answer.
func KubeWorkloadsPage(configYAML, contextName, kubeServer, kind, namespace, continueToken string, limit int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	wk, err := findWorkloadKind(kind)
	if err != nil {
		return "", err
	}

	args, err := newPageArgs(namespace, continueToken, limit)
	if err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubeWorkloadPage {
			rows := []kubeWorkload{}

			for _, w := range demoKubeWorkloads() {
				if w.Kind == wk.kind && (args.namespace == "" || w.Namespace == args.namespace) {
					rows = append(rows, w)
				}
			}

			return kubeWorkloadPage{Workloads: rows, pageCursor: completeCursor}
		},
		func(ctx context.Context, k *kubeClient) (kubeWorkloadPage, error) {
			page, err := listWorkloadsPage(ctx, k, wk, args.namespace, pageQuery{limit: args.limit, continueToken: args.continueToken})
			learnWorkloadNames(page.Workloads)

			return page, err
		})
}

func listWorkloadsPage(ctx context.Context, k *kubeClient, wk workloadKind, namespace string, q pageQuery) (kubeWorkloadPage, error) {
	page, err := k.getPage(ctx, scopedPath("/apis/apps/v1", namespace, wk.resource), q)
	if err != nil {
		return kubeWorkloadPage{}, err
	}

	objs, err := decodeItems[appsObject](page)
	if err != nil {
		return kubeWorkloadPage{}, err
	}

	rows := make([]kubeWorkload, 0, len(objs))
	for _, obj := range objs {
		rows = append(rows, mapWorkload(wk.kind, obj))
	}

	return kubeWorkloadPage{Workloads: rows, pageCursor: cursorOf(page)}, nil
}
