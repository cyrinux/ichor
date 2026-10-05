package ichorgo

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"
)

type kubeNamespaceList struct {
	Namespaces []string `json:"namespaces"`
	// Forbidden: the credentials may not list namespaces (namespace-scoped RBAC). Not an
	// error: the app asks for a namespace to type instead.
	Forbidden bool `json:"forbidden"`
	// ContextNamespace is the kubeconfig context's namespace, "" when it sets none.
	ContextNamespace string `json:"contextNamespace"`
}

// KubeNamespaces lists the cluster's namespace names, sorted, for the apps to pick the scope
// of their Kubernetes lists (os:admin): {"namespaces":[...],"forbidden","contextNamespace"}.
// Read page by page as a Table, the smallest answer there is. kubeServer: see KubePods.
func KubeNamespaces(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoNamespaces, listNamespaces)
}

func listNamespaces(ctx context.Context, k *kubeClient) (kubeNamespaceList, error) {
	names := []string{}

	err := k.listAll(ctx, "/api/v1/namespaces", pageQuery{table: true}, func() { names = names[:0] }, func(page kubePage) error {
		got, err := namespaceNames(page)
		names = append(names, got...)

		return err
	})

	var apiErr *kubeAPIError
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusForbidden {
		return kubeNamespaceList{Namespaces: []string{}, Forbidden: true, ContextNamespace: k.namespace}, nil
	}

	if err != nil {
		return kubeNamespaceList{}, err
	}

	sort.Strings(names)
	// The picker shows them masked in screenshot mode: the one picked must map back.
	privacy.learnNamespaces(names)

	return kubeNamespaceList{Namespaces: names, ContextNamespace: k.namespace}, nil
}

// namespaceNames reads the names of a Table page (each row's metadata, else its Name
// column) or of a JSON one.
func namespaceNames(page kubePage) ([]string, error) {
	if t := page.table; t != nil {
		col := t.column("Name")
		names := make([]string, 0, len(t.Rows))

		for _, row := range t.Rows {
			name := row.Object.Metadata.Name
			if name == "" {
				name = row.text(col)
			}

			if name != "" {
				names = append(names, name)
			}
		}

		return names, nil
	}

	objs, err := decodeItems[struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}](page)

	names := make([]string, 0, len(objs))
	for _, obj := range objs {
		names = append(names, obj.Metadata.Name)
	}

	return names, err
}

// demoNamespaces are the namespaces of the demo inventory's pods, workloads and CronJobs.
func demoNamespaces() kubeNamespaceList {
	seen := map[string]bool{}

	for _, p := range demoPods() {
		seen[p.Namespace] = true
	}

	for _, w := range demoKubeWorkloads() {
		seen[w.Namespace] = true
	}

	for _, c := range demoCronJobs(time.Now()) {
		seen[c.Namespace] = true
	}

	names := make([]string, 0, len(seen))
	for ns := range seen {
		names = append(names, ns)
	}

	sort.Strings(names)

	return kubeNamespaceList{Namespaces: names, ContextNamespace: "default"}
}
