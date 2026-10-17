package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/cyrinux/ichor/go/plugin"
)

// pluginProbe is what the cluster says to the plugins' detections, read once for a whole
// catalog: the API groups (one GET /apis), and the running pods and the metrics Services only
// when a plugin detects by them.
type pluginProbe struct {
	groups   map[string]string
	apps     map[string]detection
	services map[string]detection
	hints    hintSet

	mu     sync.Mutex
	served map[string]bool // group/version/resource → served
}

func newPluginProbe(ctx context.Context, k *kubeClient, plugins []*plugin.Compiled, hints hintSet) (*pluginProbe, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return nil, err
	}

	p := &pluginProbe{groups: groups, hints: hints, served: map[string]bool{}}

	if detectsBy(plugins, func(d plugin.Detect) bool { return d.Pods != "" }) {
		// Pods that cannot be listed leave the hints (the inventory) to stand in.
		p.apps, _ = runningApps(ctx, k) //nolint:errcheck // not listable: hints decide
	}

	if detectsBy(plugins, func(d plugin.Detect) bool { return d.Service != "" }) {
		p.services = queryServices(ctx, k)
	}

	return p, nil
}

func detectsBy(plugins []*plugin.Compiled, match func(plugin.Detect) bool) bool {
	return slices.ContainsFunc(plugins, func(c *plugin.Compiled) bool { return slices.ContainsFunc(c.Plugin.Detect, match) })
}

// detect is the first of the plugin's detections the cluster matches.
func (p *pluginProbe) detect(ctx context.Context, k *kubeClient, c *plugin.Compiled) plugin.Detection {
	for _, d := range c.Plugin.Detect {
		switch {
		case d.Group != "":
			if version, ok := p.groups[d.Group]; ok && (d.Resource == "" || p.serves(ctx, k, d.Group, version, d.Resource)) {
				return plugin.Detection{Active: true, Via: detectedByAPI}
			}
		case d.Pods != "":
			if _, ok := p.apps[d.Pods]; ok {
				return plugin.Detection{Active: true, Via: detectedByPods}
			}

			if p.apps == nil && p.hints[d.Pods] {
				return plugin.Detection{Active: true, Via: detectedByInventory}
			}
		case d.Service != "":
			if _, ok := p.services[d.Service]; ok {
				return plugin.Detection{Active: true, Via: detectedByServices}
			}
		}
	}

	return plugin.Detection{}
}

// serves asks the group version for its resources, once per resource.
func (p *pluginProbe) serves(ctx context.Context, k *kubeClient, group, version, resource string) bool {
	key := group + "/" + version + "/" + resource

	p.mu.Lock()
	served, ok := p.served[key]
	p.mu.Unlock()

	if !ok {
		served = servesResource(ctx, k, group, version, resource)

		p.mu.Lock()
		p.served[key] = served
		p.mu.Unlock()
	}

	return served
}

// fetchPluginInput detects c on the cluster and, when it is there, lists its resources in
// scope (a namespace, "" for all; cluster-scoped resources ignore it).
func fetchPluginInput(ctx context.Context, k *kubeClient, c *plugin.Compiled, scope string) (plugin.Input, error) {
	probe, err := newPluginProbe(ctx, k, []*plugin.Compiled{c}, nil)
	if err != nil {
		return plugin.Input{}, err
	}

	return probe.fetch(ctx, k, c, scope), nil
}

// fetch lists every resource of c at once. A resource that cannot be read (forbidden, not
// served) is an entry of the input's errors: the plugin still renders what it could read.
func (p *pluginProbe) fetch(ctx context.Context, k *kubeClient, c *plugin.Compiled, scope string) plugin.Input {
	in := plugin.Input{
		Objects: map[string][]map[string]any{}, APIVersions: map[string]string{},
		Detected: p.detect(ctx, k, c), Heuristics: coreHeuristics{},
	}

	if !in.Detected.Active {
		return in
	}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for _, key := range c.Resources {
		decl := c.Plugin.Resources[key]
		version := p.version(decl)
		in.APIVersions[key] = version

		wg.Go(func() {
			var (
				objs []map[string]any
				note string
			)

			// A panic here must not reach gomobile: it is this resource's error.
			err := safeCall(func() (err error) {
				objs, note, err = listPluginResource(ctx, k, decl, version, scope)

				return err
			})

			mu.Lock()
			defer mu.Unlock()

			in.Objects[key] = objs

			switch {
			case err != nil:
				in.Errors = append(in.Errors, plugin.TreeError{Resource: resourceLabel(decl), Message: kubeError(err).Error()})
			case note != "":
				in.Errors = append(in.Errors, plugin.TreeError{Resource: resourceLabel(decl), Message: note})
			}
		})
	}

	wg.Wait()
	slices.SortFunc(in.Errors, func(a, b plugin.TreeError) int { return strings.Compare(a.Resource, b.Resource) })

	return in
}

// version resolves version: preferred to the group's preferred version (v1 for the core group).
func (p *pluginProbe) version(decl plugin.Resource) string {
	if decl.Version != "preferred" {
		return decl.Version
	}

	if decl.Group == "" {
		return "v1"
	}

	return p.groups[decl.Group]
}

func resourceLabel(decl plugin.Resource) string {
	if decl.Group == "" {
		return decl.Resource
	}

	return decl.Resource + "." + decl.Group
}

// errPluginListFull stops a list once it holds its limit.
var errPluginListFull = errors.New("plugin list holds its limit")

// listPluginResource reads at most list.limit objects (500 by default) of decl, page by page
// with the declared selectors; note says when more are left out. Secrets are masked unless
// the resource reveals them.
func listPluginResource(ctx context.Context, k *kubeClient, decl plugin.Resource, version, scope string) ([]map[string]any, string, error) {
	if version == "" {
		return nil, "", fmt.Errorf("the cluster does not serve %s", decl.Group)
	}

	ref, err := newResourceRef(decl.Group, version, decl.Resource)
	if err != nil {
		return nil, "", err
	}

	namespace := ""
	if decl.Scope == "Namespaced" {
		namespace = scope
	}

	q := pageQuery{}
	if decl.List != nil {
		q = pageQuery{limit: decl.List.Limit, labelSelector: decl.List.LabelSelector, fieldSelector: decl.List.FieldSelector}
	}

	q.limit = clampPageLimit(q.limit)

	var (
		items []map[string]any
		more  bool
	)

	err = k.listAll(ctx, ref.path(namespace), q, func() { items = items[:0] }, func(page kubePage) error {
		objs, err := decodeItems[map[string]any](page)
		if err != nil {
			return err
		}

		items = append(items, objs...)
		if len(items) < q.limit {
			return nil
		}

		more = len(items) > q.limit || page.continueToken != ""
		items = items[:q.limit]

		return errPluginListFull
	})
	if err != nil && !errors.Is(err, errPluginListFull) {
		return nil, "", err
	}

	for _, obj := range items {
		tidyPluginObject(obj, decl)
	}

	if more {
		return items, fmt.Sprintf("showing the first %d %s", q.limit, decl.Resource), nil
	}

	return items, "", nil
}

// tidyPluginObject drops managedFields and masks a Secret's values unless revealed.
func tidyPluginObject(obj map[string]any, decl plugin.Resource) {
	if meta, ok := obj["metadata"].(map[string]any); ok {
		delete(meta, "managedFields")
	}

	if decl.Group == "" && decl.Resource == "secrets" && !decl.Reveal {
		maskSecretValues(obj)
	}
}
