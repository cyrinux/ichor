package ichorgo

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyrinux/ichor/go/plugin"
)

// A plugin resource declared with source: native:<id> is read by one of these Go readers
// instead of a list call, for what no selector can pick (docs/plugins.md, "File format").
// Each is given the namespace to read ("" for all, always "" for a cluster-scoped resource)
// and the probe (p.groups, the served API versions), reads what it needs (every page) and
// returns the objects it selects or builds; the plugin's list.limit still caps what is kept.
// The resource's group, version and resource need not be served: they only name it.
type pluginNativeSource func(ctx context.Context, k *kubeClient, p *pluginProbe, decl plugin.Resource, scope string) ([]map[string]any, error)

var pluginNativeSources = map[string]pluginNativeSource{
	// The Deployments running the Sealed Secrets controller, by the catalog's images: an
	// install renamed or relabelled by hand is still found, and a cluster of many Deployments
	// is read whole (a list cut at its limit would miss the controller and call it down).
	"sealed-secrets-controller": catalogDeployments("sealed-secrets"),
}

// readNativeSource runs the reader source names (native:<id>).
func readNativeSource(ctx context.Context, k *kubeClient, p *pluginProbe, decl plugin.Resource, scope string) ([]map[string]any, error) {
	id, _ := strings.CutPrefix(decl.Source, "native:")

	read, ok := pluginNativeSources[id]
	if !ok {
		return nil, fmt.Errorf("unknown native source %q", id)
	}

	return read(ctx, k, p, decl, scope)
}

// catalogDeployments keeps the Deployments of decl (apps/v1 deployments) with a container
// whose image the app catalog identifies as app.
func catalogDeployments(app string) pluginNativeSource {
	return func(ctx context.Context, k *kubeClient, p *pluginProbe, decl plugin.Resource, scope string) ([]map[string]any, error) {
		version := p.version(decl)
		if version == "" {
			return nil, fmt.Errorf("%w %s", errPluginNotServed, decl.Group)
		}

		ref, err := newResourceRef(decl.Group, version, decl.Resource)
		if err != nil {
			return nil, err
		}

		catalog := loadAppCatalog()

		var out []map[string]any

		err = k.listAll(ctx, ref.path(scope), pageQuery{}, func() { out = nil }, func(page kubePage) error {
			objs, err := decodeItems[map[string]any](page)
			if err != nil {
				return err
			}

			for _, obj := range objs {
				if runsCatalogApp(catalog, obj, app) {
					out = append(out, obj)
				}
			}

			return nil
		})

		return out, err
	}
}

// runsCatalogApp: a container of the workload's pod template runs one of app's images.
func runsCatalogApp(catalog *appCatalog, workload map[string]any, app string) bool {
	containers, _ := dig(workload, "spec", "template", "spec", "containers").([]any)

	for _, c := range containers {
		image, _ := dig(c, "image").(string)
		if id := catalog.identify(parseImageRef(image)); id.app != nil && id.app.ID == app {
			return true
		}
	}

	return false
}

// dig is the value at path in nested maps, nil when a step is missing.
func dig(v any, path ...string) any {
	for _, key := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}

		v = m[key]
	}

	return v
}
