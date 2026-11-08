package ichorgo

import (
	"context"
	"fmt"
)

// The plugins' findings in the AI diagnosis and the support bundle: a system migrated to a
// built-in plugin (kube-green, Rook Ceph, Sealed Secrets…) keeps its place in the DATA
// SERVICES section, as a section of its own per plugin, in English.

// collectPluginFindings reads every active plugin's findings within pluginFindingsBudget, as
// the background monitor does (KubePluginFindings); nil when no plugin is active.
func collectPluginFindings(ctx context.Context, k *kubeClient) *pluginFindingsReport {
	ctx, cancel := context.WithTimeout(ctx, pluginFindingsBudget)
	defer cancel()

	plugins := knownPlugins()

	probe, err := newPluginProbe(ctx, k, plugins, hintSet{})
	if err != nil {
		return nil
	}

	return activePluginFindings(findingsReport(runPluginFindings(ctx, k, probe, plugins, "")))
}

// activePluginFindings is report, nil when the cluster runs none of the plugins.
func activePluginFindings(report pluginFindingsReport) *pluginFindingsReport {
	if report.Plugins == 0 && len(report.Errors) == 0 {
		return nil
	}

	return &report
}

// pluginReportSections are the report's sections of the plugins: one per plugin with a
// finding or a read error, in the report's order (worst finding first). A finding's key is its
// title (the object, by the plugins' convention), its detail the plugin's own words.
func pluginReportSections(report *pluginFindingsReport) []dsSection {
	if report == nil {
		return nil
	}

	var (
		order []string
		names = map[string]string{}
		items = map[string][]dsIssue{}
		errs  = map[string]string{}
	)

	add := func(plugin, name string) {
		if _, ok := names[plugin]; !ok {
			order = append(order, plugin)
			names[plugin] = name
		}
	}

	for _, f := range report.Findings {
		add(f.Plugin, f.PluginName)
		items[f.Plugin] = append(items[f.Plugin], dsIssue{health: pluginReportHealth(f.Severity), key: f.Title, detail: f.Detail})
	}

	for _, e := range report.Errors {
		add(e.Plugin, e.PluginName)

		msg := e.Message
		if e.Resource != "" {
			msg = e.Resource + ": " + msg
		}

		if errs[e.Plugin] != "" {
			msg = errs[e.Plugin] + "; " + msg
		}

		errs[e.Plugin] = msg
	}

	out := make([]dsSection, 0, len(order))

	for _, plugin := range order {
		sec := newDSSection(names[plugin]+" (plugin)", "findings", errs[plugin], items[plugin])
		if sec.summary == "" && errs[plugin] == "" {
			continue
		}

		out = append(out, sec)
	}

	if report.Truncated > 0 && len(out) > 0 {
		last := &out[len(out)-1]
		last.summary += fmt.Sprintf(" (%d more findings left out)", report.Truncated)
	}

	return out
}

// pluginReportHealth is a finding's severity as the report's health: an info finding is
// counted, never listed.
func pluginReportHealth(severity string) string {
	switch severity {
	case sevCritical:
		return healthCritical
	case sevWarning:
		return healthWarning
	default:
		return healthIdle
	}
}
