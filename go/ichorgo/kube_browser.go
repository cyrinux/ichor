package ichorgo

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v4"
)

// The resource browser lists any kind the API server serves, CRDs included, the way
// `kubectl api-resources` and `kubectl get` do: discovery says what exists, the server-side
// Table says what to show. No code per kind.

type kubeBrowserResource struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Resource   string   `json:"resource"`
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs"`
	ShortNames []string `json:"shortNames"`
	Categories []string `json:"categories"`
}

type kubeBrowserResourceList struct {
	Resources []kubeBrowserResource `json:"resources"`
	// Failed lists the group versions discovery could not read (an aggregated API down).
	Failed []string `json:"failed"`
}

type discoveryResourceList struct {
	GroupVersion string `json:"groupVersion"`
	Resources    []struct {
		Name       string   `json:"name"`
		Kind       string   `json:"kind"`
		Namespaced bool     `json:"namespaced"`
		Verbs      []string `json:"verbs"`
		ShortNames []string `json:"shortNames"`
		Categories []string `json:"categories"`
	} `json:"resources"`
}

type discoveryGroupList struct {
	Groups []struct {
		Name             string `json:"name"`
		PreferredVersion struct {
			GroupVersion string `json:"groupVersion"`
			Version      string `json:"version"`
		} `json:"preferredVersion"`
	} `json:"groups"`
}

// discoveryParallel bounds the group versions read at once.
const discoveryParallel = 8

// KubeAPIResources lists the listable resources of the cluster (preferred version of each
// group), as a JSON kubeBrowserResourceList sorted by kind then group.
func KubeAPIResources(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoAPIResources, discoverResources)
}

func discoverResources(ctx context.Context, k *kubeClient) (kubeBrowserResourceList, error) {
	var groups discoveryGroupList
	if err := k.get(ctx, "/apis", &groups); err != nil {
		return kubeBrowserResourceList{}, err
	}

	paths := []string{"/api/v1"}
	for _, g := range groups.Groups {
		if gv := g.PreferredVersion.GroupVersion; gv != "" {
			paths = append(paths, "/apis/"+gv)
		}
	}

	lists := make([]discoveryResourceList, len(paths))
	errs := make([]error, len(paths))
	sem := make(chan struct{}, discoveryParallel)

	var wg sync.WaitGroup

	for i, path := range paths {
		wg.Add(1)

		go func() {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			errs[i] = k.get(ctx, path, &lists[i])
		}()
	}

	wg.Wait()

	if errs[0] != nil {
		return kubeBrowserResourceList{}, errs[0]
	}

	out := kubeBrowserResourceList{Resources: []kubeBrowserResource{}, Failed: []string{}}

	for i, list := range lists {
		if errs[i] != nil {
			out.Failed = append(out.Failed, strings.TrimPrefix(paths[i], "/apis/"))

			continue
		}

		out.Resources = append(out.Resources, listableResources(list)...)
	}

	slices.SortFunc(out.Resources, func(a, b kubeBrowserResource) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Group, b.Group), cmp.Compare(a.Resource, b.Resource))
	})

	return out, nil
}

// listableResources keeps the resources that can be listed, without subresources.
func listableResources(list discoveryResourceList) []kubeBrowserResource {
	group, version, found := strings.Cut(list.GroupVersion, "/")
	if !found {
		group, version = "", list.GroupVersion
	}

	out := []kubeBrowserResource{}

	for _, r := range list.Resources {
		if strings.Contains(r.Name, "/") || !slices.Contains(r.Verbs, "list") {
			continue
		}

		out = append(out, kubeBrowserResource{
			Group: group, Version: version, Resource: r.Name, Kind: r.Kind, Namespaced: r.Namespaced,
			Verbs: nonNil(r.Verbs), ShortNames: nonNil(r.ShortNames), Categories: nonNil(r.Categories),
		})
	}

	return out
}

// kubeResourcePage is one page of any resource as the server's Table shows it.
type kubeResourcePage struct {
	Columns []kubeResourceColumn `json:"columns"`
	Rows    []kubeResourceRow    `json:"rows"`
	// Continue asks for the next page, "" on the last one; Remaining as in other pages.
	Continue  string `json:"continue"`
	Remaining int64  `json:"remaining"`
}

type kubeResourceColumn struct {
	Name string `json:"name"`
	// Priority 0 columns are what `kubectl get` prints, higher ones what `-o wide` adds.
	Priority int    `json:"priority"`
	Type     string `json:"type"`
}

type kubeResourceRow struct {
	Name      string   `json:"name"`
	Namespace string   `json:"namespace,omitempty"`
	Cells     []string `json:"cells"`
	// Created is Unix seconds; Deleting when a deletion is pending (finalizers).
	Created  int64 `json:"created"`
	Deleting bool  `json:"deleting,omitempty"`
}

var (
	kubeGroupPattern    = regexp.MustCompile(`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*)?$`)
	kubeVersionPattern  = regexp.MustCompile(`^v[0-9]+((alpha|beta)[0-9]+)?$`)
	kubeResourcePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
)

// resourceRef names a resource of the API: "" group for the core one.
type resourceRef struct {
	group, version, resource string
}

func newResourceRef(group, version, resource string) (resourceRef, error) {
	ref := resourceRef{strings.TrimSpace(group), strings.TrimSpace(version), strings.TrimSpace(resource)}

	switch {
	case !kubeGroupPattern.MatchString(ref.group):
		return resourceRef{}, fmt.Errorf("invalid API group %q", ref.group)
	case !kubeVersionPattern.MatchString(ref.version):
		return resourceRef{}, fmt.Errorf("invalid API version %q", ref.version)
	case !kubeResourcePattern.MatchString(ref.resource):
		return resourceRef{}, fmt.Errorf("invalid resource %q", ref.resource)
	}

	return ref, nil
}

// path is the collection, in namespace when set.
func (r resourceRef) path(namespace string) string {
	prefix := "/apis/" + r.group + "/" + r.version
	if r.group == "" {
		prefix = "/api/" + r.version
	}

	if namespace != "" {
		prefix += "/namespaces/" + url.PathEscape(namespace)
	}

	return prefix + "/" + r.resource
}

// KubeResourcePage reads a page of any resource (all namespaces when namespace is ""), as a
// JSON kubeResourcePage of the server's Table columns. continueToken comes from the previous
// page; limit as in the other pages.
func KubeResourcePage(configYAML, contextName, kubeServer, group, version, resource, namespace, continueToken string, limit int) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	ref, err := newResourceRef(group, version, resource)
	if err != nil {
		return "", err
	}

	args, err := newPageArgs(namespace, continueToken, limit)
	if err != nil {
		return "", err
	}

	namespace = args.namespace
	demo := func() kubeResourcePage { return demoResourcePage(ref, namespace) }

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (kubeResourcePage, error) {
		page, err := k.getPage(ctx, ref.path(namespace), pageQuery{limit: args.limit, continueToken: args.continueToken, table: true})
		if err != nil {
			return kubeResourcePage{}, err
		}

		return mapResourcePage(page)
	})
}

func mapResourcePage(page kubePage) (kubeResourcePage, error) {
	cursor := cursorOf(page)
	out := kubeResourcePage{Columns: []kubeResourceColumn{}, Rows: []kubeResourceRow{}, Continue: cursor.Continue, Remaining: cursor.Remaining}

	if page.table == nil {
		// No Table from this server or resource: name and age from the objects themselves.
		out.Columns = []kubeResourceColumn{{Name: "Name", Type: "string"}}

		for _, raw := range page.items {
			var obj struct {
				Metadata kubeRowMeta `json:"metadata"`
			}

			if err := json.Unmarshal(raw, &obj); err != nil {
				return kubeResourcePage{}, fmt.Errorf("decode object: %w", err)
			}

			out.Rows = append(out.Rows, resourceRow(obj.Metadata, []string{obj.Metadata.Name}))
		}

		return out, nil
	}

	for _, c := range page.table.Columns {
		out.Columns = append(out.Columns, kubeResourceColumn{Name: c.Name, Priority: c.Priority, Type: c.Type})
	}

	for _, row := range page.table.Rows {
		cells := make([]string, len(row.Cells))
		for i := range row.Cells {
			cells[i] = row.text(i)
		}

		out.Rows = append(out.Rows, resourceRow(row.Object.Metadata, cells))
	}

	return out, nil
}

func resourceRow(meta kubeRowMeta, cells []string) kubeResourceRow {
	row := kubeResourceRow{Name: meta.Name, Namespace: meta.Namespace, Cells: cells, Deleting: meta.DeletionTimestamp != nil}
	if !meta.CreationTimestamp.IsZero() {
		row.Created = meta.CreationTimestamp.Unix()
	}

	return row
}

// KubeObjectYAML reads one object as YAML, without managedFields (noise on a phone). A
// Secret's values are replaced by their size unless reveal: a screen of base64 is easy to
// leak in a screenshot.
func KubeObjectYAML(configYAML, contextName, kubeServer, group, version, resource, namespace, name string, reveal bool) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))

	ref, err := newResourceRef(group, version, resource)
	if err != nil {
		return "", err
	}

	if !kubeResourcePattern.MatchString(name) && !kubeNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid name %q", name)
	}

	if namespace != "" {
		if err := validateNamespace(namespace); err != nil {
			return "", err
		}
	}

	if isDemoContext(configYAML, contextName) {
		return demoObjectYAML(ref, namespace, name), nil
	}

	return withKube(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) (string, error) {
		var obj map[string]any
		if err := k.get(ctx, ref.path(namespace)+"/"+url.PathEscape(name), &obj); err != nil {
			return "", err
		}

		return objectYAML(obj, ref, reveal)
	})
}

func objectYAML(obj map[string]any, ref resourceRef, reveal bool) (string, error) {
	if meta, ok := obj["metadata"].(map[string]any); ok {
		delete(meta, "managedFields")
	}

	if ref.group == "" && ref.resource == "secrets" && !reveal {
		for _, field := range []string{"data", "stringData"} {
			values, ok := obj[field].(map[string]any)
			if !ok {
				continue
			}

			for key, v := range values {
				s, _ := v.(string)
				values[key] = fmt.Sprintf("<hidden, %d characters>", len(s))
			}
		}

		if meta, ok := obj["metadata"].(map[string]any); ok {
			if annotations, ok := meta["annotations"].(map[string]any); ok {
				// kubectl apply keeps the whole Secret, values included, in this annotation.
				delete(annotations, "kubectl.kubernetes.io/last-applied-configuration")
			}
		}
	}

	data, err := yaml.Marshal(obj)
	if err != nil {
		return "", fmt.Errorf("encode YAML: %w", err)
	}

	return string(data), nil
}

func demoAPIResources() kubeBrowserResourceList {
	r := func(group, version, resource, kind string, namespaced bool, short ...string) kubeBrowserResource {
		return kubeBrowserResource{
			Group: group, Version: version, Resource: resource, Kind: kind, Namespaced: namespaced,
			Verbs: []string{"get", "list", "watch"}, ShortNames: nonNil(short), Categories: []string{},
		}
	}

	return kubeBrowserResourceList{Resources: []kubeBrowserResource{
		r("argoproj.io", "v1alpha1", "applications", "Application", true, "app"),
		r("", "v1", "configmaps", "ConfigMap", true, "cm"),
		r("apps", "v1", "deployments", "Deployment", true, "deploy"),
		r("", "v1", "namespaces", "Namespace", false, "ns"),
		r("", "v1", "nodes", "Node", false, "no"),
		r("", "v1", "pods", "Pod", true, "po"),
		r("", "v1", "services", "Service", true, "svc"),
	}, Failed: []string{}}
}

func demoResourcePage(ref resourceRef, namespace string) kubeResourcePage {
	now := time.Now()
	page := kubeResourcePage{
		Columns: []kubeResourceColumn{{Name: "Name", Type: "string"}, {Name: "Age", Type: "string"}},
		Rows:    []kubeResourceRow{},
	}

	for _, w := range demoKubeWorkloads() {
		if namespace != "" && w.Namespace != namespace {
			continue
		}

		if ref.resource == "deployments" && w.Kind != "Deployment" {
			continue
		}

		page.Rows = append(page.Rows, kubeResourceRow{Name: w.Name, Namespace: w.Namespace, Cells: []string{w.Name, "30d"}, Created: now.Add(-30 * 24 * time.Hour).Unix()})
	}

	return page
}

func demoObjectYAML(ref resourceRef, namespace, name string) string {
	apiVersion := ref.version
	if ref.group != "" {
		apiVersion = ref.group + "/" + ref.version
	}

	meta := "metadata:\n  name: " + name + "\n"
	if namespace != "" {
		meta += "  namespace: " + namespace + "\n"
	}

	return "apiVersion: " + apiVersion + "\n" + meta + "# Demo cluster: no real object behind this.\n"
}
