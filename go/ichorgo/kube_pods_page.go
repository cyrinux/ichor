package ichorgo

import (
	"context"
	"strings"
)

// kubePodPage is one page of pods, in the API server's order (namespace, then name).
type kubePodPage struct {
	Pods []kubePod `json:"pods"`
	pageCursor
}

// KubePodsPage lists one page of the pods of namespace ("" for every namespace), for the
// apps to load a large cluster page by page (os:admin):
// {"pods":[...as KubePods],"continue","remaining","complete"}. continueToken is the previous
// page's "continue", "" for the first page; limit 0 is 500 pods. table asks the server for
// its Table (the columns of `kubectl get pods` and each pod's metadata) instead of full
// objects, 10-20 times smaller: its pods have no images, container names nor last
// termination (KubePod reads one pod with them). A server without Table answers with
// objects, mapped in full. kubeServer: see KubePods.
func KubePodsPage(configYAML, contextName, kubeServer, namespace, continueToken string, limit int, table bool) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	args, err := newPageArgs(namespace, continueToken, limit)
	if err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubePodPage {
			return kubePodPage{Pods: inNamespace(demoPods(), args.namespace, func(p kubePod) string { return p.Namespace }), pageCursor: completeCursor}
		},
		func(ctx context.Context, k *kubeClient) (kubePodPage, error) {
			page, err := listPodsPage(ctx, k, args.namespace, pageQuery{limit: args.limit, continueToken: args.continueToken, table: table})
			learnPodNames(page.Pods)

			return page, err
		})
}

func listPodsPage(ctx context.Context, k *kubeClient, namespace string, q pageQuery) (kubePodPage, error) {
	page, err := k.getPage(ctx, scopedPath("/api/v1", namespace, "pods"), q)
	if err != nil {
		return kubePodPage{}, err
	}

	pods, err := mapPodPage(page)
	if err != nil {
		return kubePodPage{}, err
	}

	return kubePodPage{Pods: pods, pageCursor: cursorOf(page)}, nil
}

// mapPodPage maps the rows of a Table page, or the pods of a JSON one.
func mapPodPage(page kubePage) ([]kubePod, error) {
	if page.table != nil {
		return mapPodTable(page.table), nil
	}

	objs, err := decodeItems[podObject](page)
	if err != nil {
		return nil, err
	}

	pods := make([]kubePod, 0, len(objs))
	for _, obj := range objs {
		pods = append(pods, mapPod(obj))
	}

	return pods, nil
}

// mapPodTable reads the columns of `kubectl get pods` (Name, Ready, Status, Restarts, Node)
// and the owner and age from each row's metadata.
func mapPodTable(t *kubeTable) []kubePod {
	name, ready, status, restarts, node := t.column("Name"), t.column("Ready"), t.column("Status"), t.column("Restarts"), t.column("Node")
	pods := make([]kubePod, 0, len(t.Rows))

	for _, row := range t.Rows {
		meta := row.Object.Metadata
		p := kubePod{
			Namespace:      meta.Namespace,
			Name:           meta.Name,
			Status:         row.text(status),
			Restarts:       leadingInt(row.text(restarts)),
			Node:           row.text(node),
			Images:         []string{},
			ContainerNames: []string{},
		}

		if p.Name == "" {
			p.Name = row.text(name)
		}

		if p.Node == "<none>" {
			p.Node = ""
		}

		p.Ready, p.Containers = readyCount(row.text(ready))

		if !meta.CreationTimestamp.IsZero() {
			p.Created = meta.CreationTimestamp.UnixMilli()
		}

		if owners := meta.OwnerReferences; len(owners) > 0 {
			p.Owner = owners[0].Kind + "/" + owners[0].Name
		}

		p.Healthy = (p.Status == "Running" && p.Ready == p.Containers) || p.Status == "Completed"
		pods = append(pods, p)
	}

	return pods
}

// KubePod reads one pod in full (os:admin), as KubePods maps it: what a Table row of
// KubePodsPage lacks (images, container names, last termination). kubeServer: see KubePods.
func KubePod(configYAML, contextName, kubeServer, namespace, name string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.revealNamespace(strings.TrimSpace(namespace)), privacy.revealName(strings.TrimSpace(name))

	if err := validateKubeName("pod", namespace, name); err != nil {
		return "", err
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer},
		func() kubePod {
			for _, p := range demoPods() {
				if p.Namespace == namespace && p.Name == name {
					return p
				}
			}

			return kubePod{Namespace: namespace, Name: name, Images: []string{}, ContainerNames: []string{}}
		},
		func(ctx context.Context, k *kubeClient) (kubePod, error) {
			var obj podObject
			if err := k.get(ctx, podPath(namespace, name), &obj); err != nil {
				return kubePod{}, err
			}

			return mapPod(obj), nil
		})
}
