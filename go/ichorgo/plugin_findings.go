package ichorgo

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// pluginFindingsBudget bounds the plugins' reads of one KubePluginFindings call (the
// background monitor) and of the checkup's plugins section: a plugin that has not answered by
// then is an errors entry. A variable so that the tests shorten it.
var pluginFindingsBudget = 10 * time.Second

// pluginFindingsMax caps the findings of one KubePluginFindings call; Truncated counts the rest.
const pluginFindingsMax = 200

// pluginFinding is one finding of a plugin as the monitor and the checkup carry it: who
// raised it, its own words (already in the caller's locale), what it is about and where it
// leads. Key is stable across reads (see pluginFindingKey): the monitor dedupes on it. Every
// field is omitempty: embedded in checkupFinding, none shows on the other kinds.
type pluginFinding struct {
	Key        string      `json:"key,omitempty"`
	Plugin     string      `json:"plugin,omitempty"`
	PluginName string      `json:"pluginName,omitempty"`
	PluginIcon string      `json:"pluginIcon,omitempty"`
	ID         string      `json:"id,omitempty"`
	Title      string      `json:"title,omitempty"`
	Detail     string      `json:"detail,omitempty"`
	Ref        *plugin.Ref `json:"ref,omitempty"`
	Opens      string      `json:"opens,omitempty"`
	// User: the plugin was imported (not built in). The apps notify on its findings only when
	// the user said so: a plugin pasted in should not page anyone by itself.
	User bool `json:"user,omitempty"`
}

// pluginFindingItem is a KubePluginFindings finding: severity info, warning or critical.
type pluginFindingItem struct {
	Severity string `json:"severity"`
	pluginFinding
	// CheckupNative: the checkup reports this finding from a section of its own (see
	// checkupNativePlugins); an app that also watches the checkup skips it, or it alerts twice.
	CheckupNative bool `json:"checkupNative,omitempty"`
}

// pluginFindingsError is a plugin that could not be read, in full or in part: a resource it
// may not list (Resource set), or the whole plugin (no answer within the budget, an internal
// error). Its other findings are still reported.
type pluginFindingsError struct {
	Plugin     string `json:"plugin"`
	PluginName string `json:"pluginName"`
	Resource   string `json:"resource,omitempty"`
	Message    string `json:"message"`
}

// pluginFindingsReport is KubePluginFindings' answer. Plugins counts the active plugins read.
type pluginFindingsReport struct {
	Plugins   int                   `json:"plugins"`
	Findings  []pluginFindingItem   `json:"findings"`
	Truncated int                   `json:"truncated,omitempty"`
	Errors    []pluginFindingsError `json:"errors"`
}

// KubePluginFindings reads the findings of every plugin the cluster runs, for the background
// monitor: one /apis read detects them all, then each active plugin lists its resources (at
// most its list.limit) and renders only to evaluate its findings, all of it within 10 s.
// A plugin that cannot be read (a resource forbidden, no answer in time) is an errors entry
// and hides none of the others; the call fails only when the API server answers nothing.
// Titles and details are in locale ("" is English). hints: see KubePluginDetect. The demo
// context reports the plugins' samples. kubeServer: see KubePods.
func KubePluginFindings(configYAML, contextName, kubeServer, hints, locale string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	target := kubeTarget{configYAML, contextName, kubeServer}

	if isDemoContext(configYAML, contextName) {
		return toJSON(demoPluginFindings(locale))
	}

	ctx, cancel := context.WithTimeout(context.Background(), pluginFindingsBudget)
	defer cancel()

	report, err := withKubeContext(ctx, target, func(ctx context.Context, k *kubeClient) (pluginFindingsReport, error) {
		plugins := knownPlugins()

		probe, err := newPluginProbe(ctx, k, plugins, parseHints(hints))
		if err != nil {
			return pluginFindingsReport{}, err
		}

		return findingsReport(runPluginFindings(ctx, k, probe, plugins, locale)), nil
	})
	if err != nil {
		return "", err
	}

	return toJSON(report)
}

// pluginRun is what one active plugin found: its findings, the objects it looked at and what
// it could not read.
type pluginRun struct {
	c *plugin.Compiled
	// active: the plugin answered (a plugin cut off by the budget did not).
	active   bool
	findings []plugin.TreeFind
	checked  int
	errors   []pluginFindingsError
}

// runPluginFindings reads every active plugin of plugins in parallel and renders its
// findings in locale; plugins that are not active are left out. It returns once every plugin
// answered or ctx is done (plus a short grace for the reads to unwind): a plugin still
// reading then is an errors entry, and its late answer is dropped.
func runPluginFindings(ctx context.Context, k *kubeClient, probe *pluginProbe, plugins []*plugin.Compiled, locale string) []pluginRun {
	type answer struct {
		i   int
		run *pluginRun // nil: not active
	}

	answers := make(chan answer, len(plugins))

	for i, c := range plugins {
		go func() {
			var run *pluginRun

			// A panic in one plugin must not reach gomobile: that plugin is an errors entry.
			if err := safeCall(func() error {
				run = readPluginFindings(ctx, k, probe, c, locale)

				return nil
			}); err != nil {
				run = &pluginRun{c: c, active: true, errors: []pluginFindingsError{pluginError(c, "", err.Error())}}
			}

			answers <- answer{i, run}
		}()
	}

	runs := make([]*pluginRun, len(plugins))
	answered := make([]bool, len(plugins))
	timeout := waitPastDone(ctx, time.Second)

collect:
	for range plugins {
		select {
		case a := <-answers:
			runs[a.i], answered[a.i] = a.run, true
		case <-timeout:
			break collect
		}
	}

	out := []pluginRun{}

	for i, c := range plugins {
		switch {
		case !answered[i]:
			msg := fmt.Sprintf("no answer within %s", pluginFindingsBudget)
			out = append(out, pluginRun{c: c, errors: []pluginFindingsError{pluginError(c, "", msg)}})
		case runs[i] != nil:
			out = append(out, *runs[i])
		}
	}

	return out
}

// waitPastDone fires grace after ctx is done.
func waitPastDone(ctx context.Context, grace time.Duration) <-chan struct{} {
	fired := make(chan struct{})

	go func() {
		<-ctx.Done()
		time.Sleep(grace)
		close(fired)
	}()

	return fired
}

// readPluginFindings lists c's resources and renders its findings; nil when the cluster
// does not run c.
func readPluginFindings(ctx context.Context, k *kubeClient, probe *pluginProbe, c *plugin.Compiled, locale string) *pluginRun {
	in := probe.fetch(ctx, k, c, "")
	if !in.Detected.Active {
		return nil
	}

	in.Locale = locale

	return renderPluginFindings(c, in)
}

// renderPluginFindings evaluates c over in. The run's errors are the resources that could
// not be read; a list cut at its limit is no error (the findings cover the first objects),
// nor is a value that failed to evaluate (the plugin's screen reports those).
func renderPluginFindings(c *plugin.Compiled, in plugin.Input) *pluginRun {
	run := &pluginRun{c: c, active: true, errors: []pluginFindingsError{}}

	for _, objs := range in.Objects {
		run.checked += len(objs)
	}

	for _, e := range in.Errors {
		if !isPartialNote(e.Message) {
			run.errors = append(run.errors, pluginError(c, e.Resource, e.Message))
		}
	}

	tree, _ := plugin.Render(c, in, pluginNow()) //nolint:errcheck // errors are in the tree
	run.findings = tree.Findings

	return run
}

func pluginError(c *plugin.Compiled, resource, message string) pluginFindingsError {
	return pluginFindingsError{Plugin: c.Plugin.ID, PluginName: c.Plugin.Name, Resource: resource, Message: message}
}

// pluginFindingOf is f as the monitor and the checkup carry it.
func pluginFindingOf(c *plugin.Compiled, f plugin.TreeFind) pluginFinding {
	return pluginFinding{
		Key: pluginFindingKey(c.Plugin.ID, f), Plugin: c.Plugin.ID, PluginName: c.Plugin.Name, PluginIcon: c.Plugin.Icon,
		ID: f.ID, Title: f.Title, Detail: f.Detail, Ref: f.Ref, Opens: f.Opens, User: !builtinIDs()[c.Plugin.ID],
	}
}

// pluginFindingKey is "plugin:<plugin id>:<finding id>:<namespace>/<name>" ("…:<name>" for
// a cluster-scoped object, "plugin:<plugin id>:<finding id>" without an object): the same
// problem of the same object keeps its key from one read to the next.
func pluginFindingKey(pluginID string, f plugin.TreeFind) string {
	key := "plugin:" + pluginID + ":" + f.ID

	switch {
	case f.Ref == nil:
		return key
	case f.Ref.Namespace == "":
		return key + ":" + f.Ref.Name
	default:
		return key + ":" + f.Ref.Namespace + "/" + f.Ref.Name
	}
}

// pluginSeverity maps a finding's severity onto the checkup's; the plugin schema allows only
// these three, anything else reads as a warning.
func pluginSeverity(s string) string {
	switch s {
	case sevCritical, sevWarning, sevInfo:
		return s
	default:
		return sevWarning
	}
}

// findingsReport merges the runs: findings worst first then by key, one per key (the worst),
// capped; errors by plugin.
func findingsReport(runs []pluginRun) pluginFindingsReport {
	report := pluginFindingsReport{Findings: []pluginFindingItem{}, Errors: []pluginFindingsError{}}
	byKey := map[string]int{}

	for _, run := range runs {
		if run.active {
			report.Plugins++
		}

		report.Errors = append(report.Errors, run.errors...)

		for _, f := range run.findings {
			item := pluginFindingItem{
				Severity: pluginSeverity(f.Severity), pluginFinding: pluginFindingOf(run.c, f),
				CheckupNative: checkupNativePlugins[run.c.Plugin.ID],
			}

			if i, ok := byKey[item.Key]; ok {
				if severityRank[item.Severity] < severityRank[report.Findings[i].Severity] {
					report.Findings[i] = item
				}

				continue
			}

			byKey[item.Key] = len(report.Findings)
			report.Findings = append(report.Findings, item)
		}
	}

	slices.SortStableFunc(report.Findings, func(a, b pluginFindingItem) int {
		return cmp.Or(cmp.Compare(severityRank[a.Severity], severityRank[b.Severity]), strings.Compare(a.Key, b.Key))
	})
	slices.SortStableFunc(report.Errors, func(a, b pluginFindingsError) int {
		return cmp.Or(strings.Compare(a.Plugin, b.Plugin), strings.Compare(a.Resource, b.Resource))
	})

	if len(report.Findings) > pluginFindingsMax {
		report.Truncated = len(report.Findings) - pluginFindingsMax
		report.Findings = report.Findings[:pluginFindingsMax]
	}

	return report
}

// demoPluginFindings are the plugins' samples' findings.
func demoPluginFindings(locale string) pluginFindingsReport {
	runs := []pluginRun{}

	for _, c := range knownPlugins() {
		runs = append(runs, *renderPluginFindings(c, demoPluginInput(c, "", locale)))
	}

	return findingsReport(runs)
}
