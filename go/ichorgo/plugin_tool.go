package ichorgo

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyrinux/ichor/go/plugin"
)

// A plugin's tools (tools.<id>: {label, hint, run: native:<id>}) are buttons of its screen,
// each run by one of these Go functions over what the plugin read: a check no expression can
// make (resolving names from the phone). The result is cards the apps draw as the plugin's.
type pluginNativeTool func(ctx context.Context, k *kubeClient, c *plugin.Compiled, in plugin.Input, locale string) (plugin.ToolResult, error)

// pluginToolFuncs are a tool's functions: run over the cluster's input, demo over the
// samples (k is nil); a tool without demo is unavailable in the demo context.
type pluginToolFuncs struct {
	run  pluginNativeTool
	demo pluginNativeTool
}

// pluginNativeTools are the tools by run id (native:<id>).
var pluginNativeTools = map[string]pluginToolFuncs{}

// KubePluginTool runs the tool toolID of the plugin pluginID over what the plugin reads in
// scope (a namespace, "" for all) and returns its result as JSON: {title, health, cards}, the
// cards as in the render tree. locale picks the plugin's translations. The demo context runs
// the tool's demo over the samples. kubeServer: see KubePods.
func KubePluginTool(configYAML, contextName, kubeServer, pluginID, toolID, scope, locale string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	c, err := findPlugin(strings.TrimSpace(pluginID))
	if err != nil {
		return "", err
	}

	funcs, err := pluginToolOf(c, strings.TrimSpace(toolID))
	if err != nil {
		return "", err
	}

	scope = privacy.reveal(strings.TrimSpace(scope))
	if err := validateNamespace(scope); err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		if funcs.demo == nil {
			return "", errDemoUnavailable
		}

		res, err := funcs.demo(context.Background(), nil, c, demoPluginInput(c, scope, locale), locale)
		if err != nil {
			return "", err
		}

		return toJSON(res)
	}

	res, err := withKube(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) (plugin.ToolResult, error) {
		in, err := fetchPluginInput(ctx, k, c, scope, true)
		if err != nil {
			return plugin.ToolResult{}, err
		}

		in.Locale = locale

		return funcs.run(ctx, k, c, in, locale)
	})
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

// pluginToolOf is the functions of c's tool id; an unknown run id is an error here, when
// the tool runs, as an unknown native source is when it is read.
func pluginToolOf(c *plugin.Compiled, id string) (pluginToolFuncs, error) {
	tool, ok := c.Plugin.Tools[id]
	if !ok {
		return pluginToolFuncs{}, fmt.Errorf("unknown tool %q of plugin %s", id, c.Plugin.ID)
	}

	runID, _ := strings.CutPrefix(tool.Run, "native:")

	funcs, ok := pluginNativeTools[runID]
	if !ok || funcs.run == nil {
		return pluginToolFuncs{}, fmt.Errorf("unknown native tool %q", runID)
	}

	return funcs, nil
}
