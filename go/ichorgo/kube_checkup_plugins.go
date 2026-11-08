package ichorgo

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/cyrinux/ichor/go/plugin"
)

// findPluginFinding ("plugin") is a finding a plugin declares (docs/plugins.md "Findings"). Its words come from
// the plugin, already in the checkup's locale: the embedded pluginFinding carries the plugin
// (plugin, pluginName, pluginIcon), the finding id, title, detail, the object (ref, and
// Namespace and Name) and the card it opens.
const findPluginFinding = "plugin"

// checkupNativePlugins are the built-in plugins whose findings a section of the checkup
// already reports from its own reader (External Secrets: the secrets section): the plugins
// section leaves them out rather than name the same object twice. The monitor still gets
// them through KubePluginFindings.
var checkupNativePlugins = map[string]bool{"external-secrets": true}

// checkupPlugins is the plugins section: the findings of every plugin the cluster runs, in
// locale, each plugin read within pluginFindingsBudget. Absent when no plugin is active; a
// plugin that could not be read (in full or in part) is in the section's error.
func checkupPlugins(ctx context.Context, k *kubeClient, in checkupInput, locale string) checkupSection {
	plugins := slices.DeleteFunc(slices.Clone(knownPlugins()), func(c *plugin.Compiled) bool { return checkupNativePlugins[c.Plugin.ID] })
	if len(plugins) == 0 {
		return absentSection(checkPlugins)
	}

	ctx, cancel := context.WithTimeout(ctx, pluginFindingsBudget)
	defer cancel()

	probe := newPluginProbeFrom(ctx, k, plugins, nil, in.groups)

	return pluginsSection(runPluginFindings(ctx, k, probe, plugins, locale))
}

// pluginsSection closes the plugins section over the runs: findings grouped by plugin.
func pluginsSection(runs []pluginRun) checkupSection {
	slices.SortStableFunc(runs, func(a, b pluginRun) int {
		return cmp.Or(strings.Compare(a.c.Plugin.Name, b.c.Plugin.Name), strings.Compare(a.c.Plugin.ID, b.c.Plugin.ID))
	})

	served := false
	checked := 0
	findings := []checkupFinding{}
	var errs []string

	for _, run := range runs {
		served = served || run.active || len(run.errors) > 0
		checked += run.checked

		for _, e := range run.errors {
			errs = append(errs, pluginErrorText(e))
		}

		for _, f := range run.findings {
			pf := pluginFindingOf(run.c, f)
			finding := checkupFinding{Kind: findPluginFinding, Severity: pluginSeverity(f.Severity), pluginFinding: pf}

			if f.Ref != nil {
				finding.Namespace, finding.Name = f.Ref.Namespace, f.Ref.Name
			}

			findings = append(findings, finding)
		}
	}

	if !served {
		return absentSection(checkPlugins)
	}

	// The errors are already worded: sectionError would read them as transport errors.
	section := newSection(checkPlugins, checked, findings)
	if len(errs) > 0 {
		section.Error = strings.Join(slices.Compact(errs), "; ")
		if section.Status == healthOK {
			section.Status = checkUnknown
		}
	}

	return section
}

// pluginErrorText is "<plugin>: <resource>: <message>" for the section's error.
func pluginErrorText(e pluginFindingsError) string {
	if e.Resource == "" {
		return e.PluginName + ": " + e.Message
	}

	return e.PluginName + ": " + e.Resource + ": " + e.Message
}

// demoCheckupPlugins is the plugins section of the demo: the samples' findings.
func demoCheckupPlugins(locale string) checkupSection {
	runs := []pluginRun{}

	for _, c := range knownPlugins() {
		if !checkupNativePlugins[c.Plugin.ID] {
			runs = append(runs, *renderPluginFindings(c, demoPluginInput(c, "", locale)))
		}
	}

	return pluginsSection(runs)
}
