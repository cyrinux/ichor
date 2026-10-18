package ichorgo

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// knownPlugins are every plugin the cluster calls know: the built-ins (user plugins join
// them once they can be imported). A variable so that the tests give their own.
var knownPlugins = plugin.Builtins

// pluginNow is the render's clock (now, age(), relativeTime()).
var pluginNow = time.Now

// pluginDetectList is KubePluginDetect's answer: one item per plugin, active or not.
type pluginDetectList struct {
	Items []pluginDetectItem `json:"items"`
}

type pluginDetectItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Category    string `json:"category"`
	Screen      string `json:"screen"`
	Active      bool   `json:"active"`
	DetectedVia string `json:"detectedVia"`
	// Summary is the tile of an active plugin, null otherwise.
	Summary *plugin.TreeSummary `json:"summary"`
}

// KubePluginDetect lists every plugin with whether the cluster runs its product and, for
// each one it runs, its summary tile (title, health, badges). One /apis read serves every
// plugin; the pods and the metrics Services are listed only when a plugin detects by them.
// hints (see KubeSupportedIntegrations) stand in for the pods when they cannot be listed.
// kubeServer: see KubePods.
func KubePluginDetect(configYAML, contextName, kubeServer, hints string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoPluginDetect, func(ctx context.Context, k *kubeClient) (pluginDetectList, error) {
		return readPluginDetect(ctx, k, parseHints(hints))
	})
}

func readPluginDetect(ctx context.Context, k *kubeClient, hints hintSet) (pluginDetectList, error) {
	plugins := knownPlugins()

	probe, err := newPluginProbe(ctx, k, plugins, hints)
	if err != nil {
		return pluginDetectList{}, err
	}

	probe.allowReveal = true // the catalog's plugins are installed

	items := make([]pluginDetectItem, len(plugins))

	var wg sync.WaitGroup

	for i, c := range plugins {
		wg.Go(func() {
			// A panic in one plugin must not reach gomobile: that plugin shows no tile.
			if err := safeCall(func() error {
				items[i] = detectItem(c, probe.fetch(ctx, k, c, ""))

				return nil
			}); err != nil {
				items[i] = detectItem(c, plugin.Input{})
			}
		})
	}

	wg.Wait()

	return pluginDetectList{Items: items}, nil
}

// detectItem is a plugin's line: its summary rendered when it is active.
func detectItem(c *plugin.Compiled, in plugin.Input) pluginDetectItem {
	p := c.Plugin
	item := pluginDetectItem{
		ID: p.ID, Name: p.Name, Icon: p.Icon, Category: orDefault(p.Category, "data-services"), Screen: p.Screen,
		Active: in.Detected.Active, DetectedVia: in.Detected.Via,
	}

	if item.Active {
		tree, _ := plugin.Render(c, in, pluginNow()) //nolint:errcheck // the summary still renders
		item.Summary = &tree.Summary
	}

	return item
}

func demoPluginDetect() pluginDetectList {
	plugins := knownPlugins()
	out := pluginDetectList{Items: make([]pluginDetectItem, 0, len(plugins))}

	for _, c := range plugins {
		out.Items = append(out.Items, detectItem(c, demoPluginInput(c, "", "")))
	}

	return out
}

// KubePluginData renders one plugin over the cluster as the JSON render tree (docs/plugins.md
// "Render tree"): its summary, cards, findings, and the resources it could not read in
// errors. scope is a namespace ("" for all); locale picks the plugin's translations ("" is
// English). The demo context renders the plugin's samples. kubeServer: see KubePods.
func KubePluginData(configYAML, contextName, kubeServer, pluginID, scope, locale string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	c, err := findPlugin(strings.TrimSpace(pluginID))
	if err != nil {
		return "", err
	}

	return kubePluginTree(configYAML, contextName, kubeServer, c, scope, locale, true)
}

// KubePluginPreview is KubePluginData for a plugin file that is not installed (the probe's
// plugin-render FILE, and an import screen's preview): pluginYAML is loaded, validated and
// compiled first, and a plugin that does not compile is an error with its file:line problems.
// A Secret's values stay masked whatever its reveal says: the plugin is not installed.
func KubePluginPreview(configYAML, contextName, kubeServer, pluginYAML, scope, locale string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	c, err := plugin.CompileSource("plugin.yaml", []byte(pluginYAML))
	if err != nil {
		return "", err
	}

	return kubePluginTree(configYAML, contextName, kubeServer, c, scope, locale, false)
}

// kubePluginTree renders c; contextName is already unmasked. allowReveal: see pluginProbe.
func kubePluginTree(configYAML, contextName, kubeServer string, c *plugin.Compiled, scope, locale string, allowReveal bool) (string, error) {
	scope = privacy.reveal(strings.TrimSpace(scope))

	if err := validateNamespace(scope); err != nil {
		return "", err
	}

	demo := func() plugin.Tree {
		tree, _ := plugin.Render(c, demoPluginInput(c, scope, locale), pluginNow()) //nolint:errcheck // errors are in the tree

		return tree
	}

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demo, func(ctx context.Context, k *kubeClient) (plugin.Tree, error) {
		in, err := fetchPluginInput(ctx, k, c, scope, allowReveal)
		if err != nil {
			return plugin.Tree{}, err
		}

		in.Locale = locale
		tree, _ := plugin.Render(c, in, pluginNow()) //nolint:errcheck // errors are in the tree

		return tree, nil
	})
}

func findPlugin(id string) (*plugin.Compiled, error) {
	for _, c := range knownPlugins() {
		if c.Plugin.ID == id {
			return c, nil
		}
	}

	return nil, fmt.Errorf("unknown plugin %q", id)
}

// demoPluginInput is the plugin's samples, the namespaced ones kept to scope when set.
func demoPluginInput(c *plugin.Compiled, scope, locale string) plugin.Input {
	in := plugin.SamplesInput(c, locale, coreHeuristics{}, pluginNow())
	if scope == "" {
		return in
	}

	for key, objs := range in.Objects {
		if c.Plugin.Resources[key].Scope != "Namespaced" {
			continue
		}

		kept := make([]map[string]any, 0, len(objs))
		for _, obj := range objs {
			if stringAt(mapAt(obj, "metadata"), "namespace") == scope {
				kept = append(kept, obj)
			}
		}

		in.Objects[key] = kept
	}

	return in
}

// coreHeuristics reads an object no health rule covers, and a condition, as the object
// summary does (objectHealth, conditionTone), in the plugin's tones.
type coreHeuristics struct{}

func (coreHeuristics) ObjectHealth(obj map[string]any) (string, string) {
	status := mapAt(obj, "status")
	deleting := mapAt(obj, "metadata")["deletionTimestamp"] != nil
	tone, reason := objectHealth(readConditions(status), stringAt(status, "phase"), deleting)

	if tone == toneNone {
		return "ok", reason
	}

	return pluginTone(tone), reason
}

func (coreHeuristics) ConditionTone(c map[string]any) string {
	return pluginTone(conditionTone(summaryCondition{Type: stringAt(c, "type"), Status: stringAt(c, "status"), Reason: stringAt(c, "reason")}))
}

// pluginTone maps the object summary's tones onto the plugin's.
func pluginTone(tone string) string {
	switch tone {
	case toneOK:
		return "ok"
	case toneWarn:
		return "warning"
	case toneBad:
		return "critical"
	default:
		return "unknown"
	}
}
