package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/cyrinux/ichor/go/plugin"
)

// A plugin action with verb native (actions.<id>: {verb: native, run: native:<id>}) is run
// by one of these Go functions instead of a request the plugin builds: a call no Kubernetes
// verb can express (Ollama's API through the Service proxy). KubePluginAction keeps
// everything else of an action: the demo refusal, the params checked against their
// declaration, enabledWhen checked on the object as read now, the can-I row of its rbac, the
// audit entry. Only built-in plugins run them: an imported plugin that declares one is
// refused at import (userNativeActionProblem).

// pluginNativeAction runs an action on target, the object as the plugin read it (its native
// source included), with params checked by ActionParams.
type pluginNativeAction func(ctx context.Context, k *kubeClient, c *plugin.Compiled, target, params map[string]any) error

// pluginNativeActions are the actions by run id (native:<id>).
var pluginNativeActions = map[string]pluginNativeAction{
	// Ollama: load a model into memory (pinned or not), unload it, delete it from disk
	// (plugin_ollama_actions.go).
	"ollama-load":   runOllamaLoad,
	"ollama-unload": runOllamaUnload,
	"ollama-delete": runOllamaDelete,
}

// maxNativeTargetName caps a native action's target name: a model name, not a Kubernetes one.
const maxNativeTargetName = 253

// validateNativeTarget checks a native action's target: a namespace as Kubernetes names it
// (none for a cluster-scoped resource), and a name that is the object's own as its reader
// gives it (an Ollama model, llama3.2:3b or hf.co/org/model:q4), printable and short.
func validateNativeTarget(decl plugin.Resource, namespace, name string) error {
	if decl.Scope == "Namespaced" {
		if namespace == "" {
			return fmt.Errorf("%s is namespaced: give its namespace", decl.Kind)
		}

		if err := validateNamespace(namespace); err != nil {
			return err
		}
	} else if namespace != "" {
		return fmt.Errorf("%s is cluster-scoped: no namespace (%q given)", decl.Kind, namespace)
	}

	if name == "" || len(name) > maxNativeTargetName || strings.IndexFunc(name, func(r rune) bool { return !unicode.IsPrint(r) || unicode.IsSpace(r) }) >= 0 {
		return fmt.Errorf("invalid %s name %q", decl.Kind, name)
	}

	return nil
}

// runNativePluginAction reads what the plugin reads in namespace, finds the target among
// its objects, checks enabledWhen on it and runs the action's Go function.
func runNativePluginAction(ctx context.Context, k *kubeClient, c *plugin.Compiled, actionID, namespace, name string, params map[string]any) error {
	a := c.Plugin.Actions[actionID]

	if !builtinIDs()[c.Plugin.ID] {
		return errPluginActionInvalid(fmt.Errorf("plugin %s is not built in: it cannot run Go code", c.Plugin.ID))
	}

	id, _ := strings.CutPrefix(a.Run, "native:")

	run, ok := pluginNativeActions[id]
	if !ok {
		return errPluginActionInvalid(fmt.Errorf("unknown native action %q", id))
	}

	in, err := fetchPluginInput(ctx, k, c, namespace, true)
	if err != nil {
		return err
	}

	obj, err := c.NativeActionObject(actionID, namespace, name, in, pluginNow())
	if err != nil {
		var (
			disabled  *plugin.ActionDisabledError
			ambiguous *plugin.ActionAmbiguousError
		)

		switch {
		case errors.As(err, &disabled):
			return &kubeAPIError{Code: http.StatusConflict, Reason: "ActionDisabled", Message: disabled.Reason}
		case errors.As(err, &ambiguous):
			return &kubeAPIError{Code: http.StatusConflict, Reason: "Ambiguous", Message: ambiguous.Error()}
		case errors.Is(err, plugin.ErrNotRead):
			return &kubeAPIError{Code: http.StatusNotFound, Reason: "NotFound", Message: err.Error()}
		default:
			return errPluginActionInvalid(err)
		}
	}

	return run(ctx, k, c, obj, params)
}

// userNativeActionProblem is why an imported plugin may not run: an action of verb native
// names Go code built into Ichor, which only its own plugins may call. "" when it has none.
func userNativeActionProblem(c *plugin.Compiled) string {
	for _, a := range c.Actions {
		if a.Verb == "native" {
			return fmt.Sprintf("actions.%s: verb native runs code built into Ichor: only built-in plugins may use it", a.Name)
		}
	}

	return ""
}
