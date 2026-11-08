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
	// allowReveal lets a resource's reveal show a Secret's values: an installed plugin, outside
	// screenshot mode. A plugin previewed before it is installed never reveals.
	allowReveal bool

	mu     sync.Mutex
	served map[string]bool // group/version/resource → served
}

func newPluginProbe(ctx context.Context, k *kubeClient, plugins []*plugin.Compiled, hints hintSet) (*pluginProbe, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return nil, err
	}

	return newPluginProbeFrom(ctx, k, plugins, hints, groups), nil
}

// newPluginProbeFrom is newPluginProbe over API groups already read (the checkup's).
func newPluginProbeFrom(ctx context.Context, k *kubeClient, plugins []*plugin.Compiled, hints hintSet, groups map[string]string) *pluginProbe {
	p := &pluginProbe{groups: groups, hints: hints, served: map[string]bool{}}

	if detectsBy(plugins, func(d plugin.Detect) bool { return d.Pods != "" }) {
		// Pods that cannot be listed leave the hints (the inventory) to stand in.
		p.apps, _ = runningApps(ctx, k) //nolint:errcheck // not listable: hints decide
	}

	if detectsBy(plugins, func(d plugin.Detect) bool { return d.Service != "" }) {
		p.services = queryServices(ctx, k)
	}

	return p
}

func detectsBy(plugins []*plugin.Compiled, match func(plugin.Detect) bool) bool {
	return slices.ContainsFunc(plugins, func(c *plugin.Compiled) bool { return slices.ContainsFunc(c.Plugin.Detect, match) })
}

// detect is the first of the plugin's detections the cluster matches.
func (p *pluginProbe) detect(ctx context.Context, k *kubeClient, c *plugin.Compiled) plugin.Detection {
	f := p.detection(ctx, k, c)

	return plugin.Detection{Active: f.via != "", Via: f.via}
}

// detection is detect with what Settings shows of it: the version of the matching group, or
// where the matching pods or Services run.
func (p *pluginProbe) detection(ctx context.Context, k *kubeClient, c *plugin.Compiled) detection {
	for _, d := range c.Plugin.Detect {
		switch {
		case d.Group != "":
			if preferred, ok := p.groups[d.Group]; ok {
				version := detectVersion(c.Plugin, d, preferred)
				if d.Resource == "" || p.serves(ctx, k, d.Group, version, d.Resource) {
					return detection{via: detectedByAPI, version: version}
				}
			}
		case d.Pods != "":
			if f, ok := p.apps[d.Pods]; ok {
				return f
			}

			if p.apps == nil && p.hints[d.Pods] {
				return detection{via: detectedByInventory}
			}
		case d.Service != "":
			if f, ok := p.services[d.Service]; ok {
				return f
			}
		}
	}

	return detection{}
}

// detectVersion is the version a detect entry's resource is looked for at: the one the plugin
// declares for that resource, since a group several products share serves each one's
// resources at its own version (temporal.io: the operator's v1beta1, the worker controller's
// v1alpha1) and the preferred version is only one of them; else the group's preferred version.
func detectVersion(p *plugin.Plugin, d plugin.Detect, preferred string) string {
	if d.Resource == "" {
		return preferred
	}

	for _, r := range p.Resources {
		if r.Group == d.Group && r.Resource == d.Resource && r.Version != "" && r.Version != "preferred" {
			return r.Version
		}
	}

	return preferred
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

// errPluginNotServed is a resource of a group the cluster does not serve.
var errPluginNotServed = errors.New("the cluster does not serve")

// fetchPluginInput detects c on the cluster and, when it is there, lists its resources in
// scope (a namespace, "" for all; cluster-scoped resources ignore it). allowReveal: see
// pluginProbe.
func fetchPluginInput(ctx context.Context, k *kubeClient, c *plugin.Compiled, scope string, allowReveal bool) (plugin.Input, error) {
	probe, err := newPluginProbe(ctx, k, []*plugin.Compiled{c}, nil)
	if err != nil {
		return plugin.Input{}, err
	}

	probe.allowReveal = allowReveal

	return probe.fetch(ctx, k, c, scope), nil
}

// fetch lists every resource of c at once. A resource that cannot be read (forbidden, not
// served) is an entry of the input's errors, unless it is optional: the plugin still renders
// what it could read.
func (p *pluginProbe) fetch(ctx context.Context, k *kubeClient, c *plugin.Compiled, scope string) plugin.Input {
	in := plugin.Input{
		Objects: map[string][]map[string]any{}, APIVersions: map[string]string{}, Unread: map[string]bool{},
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
				objs, note, err = listPluginResource(ctx, k, p, decl, version, scope, p.allowReveal)

				return err
			})

			mu.Lock()
			defer mu.Unlock()

			// A Go reader may return what it read along with the error of what it could not
			// (objects and an error): part was read, so the resource stays readable (readable()
			// holds) and the error is still an entry of the tree's errors.
			in.Objects[key] = objs
			if err != nil && objs == nil {
				in.Unread[key] = true
			}

			switch {
			case err != nil && decl.Optional && (isForbidden(err) || isNotFound(err) || errors.Is(err, errPluginNotServed)):
				// An optional resource the account may not read, or the cluster does not serve: nothing to say.
			case err != nil:
				in.Errors = append(in.Errors, plugin.TreeError{Resource: resourceLabel(decl), Message: pluginReadError(err)})
			}

			if note != "" {
				in.Partial = true
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

// partialNotePrefix starts the errors entry of a list cut at its limit.
const partialNotePrefix = "showing the first "

// isPartialNote tells the entry of a list cut at its limit from a read that failed.
func isPartialNote(message string) bool { return strings.HasPrefix(message, partialNotePrefix) }

// errPluginListFull stops a list once it holds its limit.
var errPluginListFull = errors.New("plugin list holds its limit")

// listPluginResource reads at most list.limit objects (500 by default) of decl, page by page
// with the declared selectors; note says when more are left out. Secrets are masked unless
// the resource reveals them and allowReveal (see tidyPluginObject).
func listPluginResource(ctx context.Context, k *kubeClient, p *pluginProbe, decl plugin.Resource, version, scope string, allowReveal bool) ([]map[string]any, string, error) {
	namespace := ""
	if decl.Scope == "Namespaced" {
		namespace = scope
	}

	q := pageQuery{}
	if decl.List != nil {
		q = pageQuery{
			limit: decl.List.Limit, labelSelector: decl.List.LabelSelector, fieldSelector: decl.List.FieldSelector,
			metadataOnly: decl.List.MetadataOnly,
		}
	}

	q.limit = clampPageLimit(q.limit)

	// A Go reader's resource is never "not served": its group, version and resource only
	// name it.
	if decl.Source != "" {
		return listNativeResource(ctx, k, p, decl, namespace, q.limit, allowReveal)
	}

	if version == "" {
		return nil, "", fmt.Errorf("%w %s", errPluginNotServed, decl.Group)
	}

	ref, err := newResourceRef(decl.Group, version, decl.Resource)
	if err != nil {
		return nil, "", err
	}

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
		tidyPluginObject(obj, decl, allowReveal)
	}

	if more {
		return items, fmt.Sprintf("%s%d %s", partialNotePrefix, q.limit, decl.Resource), nil
	}

	return items, "", nil
}

// listNativeResource is listPluginResource for a resource its Go reader selects (source:
// native:<id>, see plugin_native.go): at most limit of the objects it keeps, and its error
// when it returned objects too (a partial read).
func listNativeResource(ctx context.Context, k *kubeClient, p *pluginProbe, decl plugin.Resource, namespace string, limit int, allowReveal bool) ([]map[string]any, string, error) {
	items, err := readNativeSource(ctx, k, p, decl, namespace)
	if err != nil && items == nil {
		return nil, "", err
	}

	note := ""
	if len(items) > limit {
		items, note = items[:limit], fmt.Sprintf("%s%d %s", partialNotePrefix, limit, decl.Resource)
	}

	for _, obj := range items {
		tidyPluginObject(obj, decl, allowReveal)
	}

	return items, note, err
}

// pluginReadError is the message of a resource's read error; the errors a Go reader joins
// (errors.Join: several lists refused) each say theirs.
func pluginReadError(err error) string {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return sectionError(joined.Unwrap()...)
	}

	return kubeError(err).Error()
}

// tidyPluginObject drops managedFields and masks a Secret's values. They show only when the
// resource reveals them, the plugin is installed (allowReveal) and screenshot mode is off:
// as for KubeConfigData (errSecretRevealScreenshot), a value in clear has no place in a
// screenshot, and masking would alter it.
func tidyPluginObject(obj map[string]any, decl plugin.Resource, allowReveal bool) {
	if meta, ok := obj["metadata"].(map[string]any); ok {
		delete(meta, "managedFields")
	}

	reveal := decl.Reveal && allowReveal && !privacy.isEnabled()
	if decl.Group == "" && decl.Resource == "secrets" && !reveal {
		maskSecretValues(obj)
	}
}
