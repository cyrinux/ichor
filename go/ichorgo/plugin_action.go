package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"

	"github.com/cyrinux/ichor/go/plugin"
)

// Plugin actions: a plugin declares an action (verb, body template, rbac, confirm,
// enabledWhen, params) instead of a Go function coding it; KubePluginAction runs it. Only an
// installed plugin's actions run (the built-in catalog, knownPlugins): a plugin file the
// user only previews (KubePluginPreview) has no way here. The request's path is built from
// the plugin's declared resource and the namespace and name given, each checked; the params
// and the templates only ever write the body (plugin.ActionBody).

// pluginActionKey is an action's key in kubeActionChecks and in the audit log.
func pluginActionKey(pluginID, actionID string) string { return "plugin:" + pluginID + ":" + actionID }

// maxPluginParams caps paramsJSON before it is recorded or decoded: params are a few short
// values, never a document.
const maxPluginParams = 16 << 10

// errPluginActionInvalid is the API server's answer shape for a request the plugin cannot
// build: a 422, so that it reads as a refusal and keeps the client.
func errPluginActionInvalid(err error) error {
	return &kubeAPIError{Code: http.StatusUnprocessableEntity, Reason: "Invalid", Message: err.Error()}
}

// KubePluginAction runs action actionID of the installed plugin pluginID on the object
// namespace/name (namespace "" for a cluster-scoped resource) with paramsJSON, a JSON object
// of the action's params ("" for none). It reads the object first and refuses, as a 409
// with the action's disabledReason, when the action's enabledWhen does not hold on it as the
// cluster has it now: the app's button is not the gate. The demo context refuses
// (errDemoUnavailable); a refusal of the API server (403, 404, 409…) is its error. Every
// call is recorded in the audit log as plugin:<pluginID>:<actionID> with the params.
// kubeServer: see KubePods.
func KubePluginAction(configYAML, contextName, kubeServer, pluginID, actionID, namespace, name, paramsJSON string) (err error) {
	defer maskErr(&err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, name = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(name))
	pluginID, actionID = strings.TrimSpace(pluginID), strings.TrimSpace(actionID)

	defer recordAction(&err, configYAML, contextName, auditAction{
		Server: kubeServer, Action: pluginActionKey(pluginID, actionID), Namespace: namespace,
		Object: pluginActionObject(pluginID, actionID, name), Params: compactParams(paramsJSON),
	})

	c, err := findPlugin(pluginID)
	if err != nil {
		return err
	}

	a, ok := c.Plugin.Actions[actionID]
	if !ok {
		return fmt.Errorf("plugin %s has no action %q", pluginID, actionID)
	}

	decl := c.Plugin.Resources[a.On]
	if err := validatePluginTarget(decl, namespace, name); err != nil {
		return err
	}

	// A Secret read in screenshot mode is masked: an action would be judged, and its body
	// written, from the masked values.
	if decl.Group == "" && decl.Resource == "secrets" && privacy.isEnabled() {
		return errors.New("actions on Secrets are off in screenshot mode")
	}

	given, err := decodeParams(paramsJSON)
	if err != nil {
		return err
	}

	params, err := c.ActionParams(actionID, given)
	if err != nil {
		return err
	}

	return kubeMutate(kubeTarget{configYAML, contextName, kubeServer}, func(ctx context.Context, k *kubeClient) error {
		return runPluginAction(ctx, k, c, actionID, namespace, name, params)
	})
}

// pluginActionObject is the audit entry's object, Kind/name.
func pluginActionObject(pluginID, actionID, name string) string {
	c, err := findPlugin(pluginID)
	if err != nil {
		return name
	}

	a, ok := c.Plugin.Actions[actionID]
	if !ok {
		return name
	}

	return c.Plugin.Resources[a.On].Kind + "/" + name
}

// compactParams is the params as the audit log keeps them: compact JSON, or as given when
// they are not JSON (the call is refused then); past maxPluginParams, only their size.
func compactParams(paramsJSON string) string {
	if len(paramsJSON) > maxPluginParams {
		return fmt.Sprintf("(%d bytes, over 16 KiB)", len(paramsJSON))
	}

	var b bytes.Buffer
	if err := json.Compact(&b, []byte(strings.TrimSpace(paramsJSON))); err != nil {
		return strings.TrimSpace(paramsJSON)
	}

	if b.String() == "{}" {
		return ""
	}

	return b.String()
}

// validatePluginTarget checks the namespace and name against the resource's scope: a
// namespaced object has both, a cluster-scoped one no namespace.
func validatePluginTarget(decl plugin.Resource, namespace, name string) error {
	if decl.Scope == "Namespaced" {
		return validateKubeName(strings.ToLower(decl.Kind), namespace, name)
	}

	if namespace != "" {
		return fmt.Errorf("%s is cluster-scoped: no namespace (%q given)", decl.Kind, namespace)
	}

	if !kubeNamePattern.MatchString(name) || strings.Contains(name, "..") {
		return fmt.Errorf("invalid Kubernetes name %q", name)
	}

	return nil
}

// decodeParams reads paramsJSON as an object, its numbers kept exact.
func decodeParams(paramsJSON string) (map[string]any, error) {
	if len(paramsJSON) > maxPluginParams {
		return nil, fmt.Errorf("the parameters are %d bytes: at most 16 KiB", len(paramsJSON))
	}

	paramsJSON = strings.TrimSpace(paramsJSON)
	if paramsJSON == "" {
		return nil, nil
	}

	dec := json.NewDecoder(strings.NewReader(paramsJSON))
	dec.UseNumber()

	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("the parameters are not a JSON object: %w", err)
	}

	if dec.More() {
		return nil, errors.New("the parameters are not a JSON object: more after it")
	}

	return out, nil
}

// runPluginAction reads the object fresh, builds the request from it and sends it.
func runPluginAction(ctx context.Context, k *kubeClient, c *plugin.Compiled, actionID, namespace, name string, params map[string]any) error {
	a := c.Plugin.Actions[actionID]
	decl := c.Plugin.Resources[a.On]

	version, err := pluginResourceVersion(ctx, k, decl)
	if err != nil {
		return err
	}

	ref, err := newResourceRef(decl.Group, version, decl.Resource)
	if err != nil {
		return errPluginActionInvalid(err)
	}

	collection := ref.path(namespace)

	var obj map[string]any
	if err := k.get(ctx, collection+"/"+url.PathEscape(name), &obj); err != nil {
		return err
	}

	tidyPluginObject(obj, decl, true)

	in, err := pluginActionInput(ctx, k, c, a.On, actionID, version, namespace, obj)
	if err != nil {
		return err
	}

	target := plugin.ActionTarget{Version: version, Namespace: namespace, Name: name}

	req, err := c.ActionBody(actionID, target, in, params, pluginNow())
	if err != nil {
		var disabled *plugin.ActionDisabledError
		if errors.As(err, &disabled) {
			return &kubeAPIError{Code: http.StatusConflict, Reason: "ActionDisabled", Message: disabled.Reason}
		}

		return errPluginActionInvalid(err)
	}

	// The plugin built the path from the same resource: anything else is a bug, never sent.
	if !onObjectPath(req.Path, collection, name, a.Verb) {
		return errPluginActionInvalid(fmt.Errorf("the request path %q is not the object's", req.Path))
	}

	err = k.do(ctx, req.Method, req.Path, req.ContentType, req.Body, nil)
	if req.Pinned && changedSinceChecked(err, a.Verb) {
		return &kubeAPIError{
			Code: http.StatusConflict, Reason: "Conflict",
			Message: fmt.Sprintf("%s %s changed since it was checked: refresh and try again", decl.Kind, name),
		}
	}

	return err
}

// changedSinceChecked: the pinned request was refused because the object's resourceVersion
// moved (a 409, or a JSON patch whose test of it failed).
func changedSinceChecked(err error, verb string) bool {
	var apiErr *kubeAPIError
	if !errors.As(err, &apiErr) {
		return false
	}

	if apiErr.Code == http.StatusConflict {
		return true
	}

	msg := strings.ToLower(apiErr.Message)

	return verb == "jsonPatch" && apiErr.Code >= 400 && (strings.Contains(msg, "test") || strings.Contains(msg, "resourceversion"))
}

// onObjectPath: create posts to the collection; every other verb targets the object, or one
// subresource below it.
func onObjectPath(path, collection, name, verb string) bool {
	if verb == "create" {
		return path == collection
	}

	rest, ok := strings.CutPrefix(path, collection+"/"+url.PathEscape(name))

	return ok && (rest == "" || (verb == "subresource" && strings.Count(rest, "/") == 1 && len(rest) > 1))
}

// pluginResourceVersion is decl's version, preferred resolved from /apis.
func pluginResourceVersion(ctx context.Context, k *kubeClient, decl plugin.Resource) (string, error) {
	if decl.Version != "preferred" {
		return decl.Version, nil
	}

	if decl.Group == "" {
		return "v1", nil
	}

	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return "", err
	}

	if v := groups[decl.Group]; v != "" {
		return v, nil
	}

	return "", &kubeAPIError{Code: http.StatusNotFound, Reason: "NotFound", Message: "the cluster does not serve " + decl.Group}
}

// pluginActionInput is what the action is evaluated over: the object alone, or, when the
// action reads a data service or a join, the plugin's resources in the object's namespace
// with the object as just read in place of its listed copy.
func pluginActionInput(ctx context.Context, k *kubeClient, c *plugin.Compiled, res, actionID, version, namespace string, obj map[string]any) (plugin.Input, error) {
	if !c.ActionNeedsContext(actionID) {
		return plugin.Input{
			Objects: map[string][]map[string]any{res: {obj}}, APIVersions: map[string]string{res: version},
			Detected: plugin.Detection{Active: true, Via: detectedByAPI}, Heuristics: coreHeuristics{},
		}, nil
	}

	in, err := fetchPluginInput(ctx, k, c, namespace, true)
	if err != nil {
		return plugin.Input{}, err
	}

	name := stringAt(mapAt(obj, "metadata"), "name")
	kept := []map[string]any{obj}

	for _, o := range in.Objects[res] {
		meta := mapAt(o, "metadata")
		if stringAt(meta, "name") != name || stringAt(meta, "namespace") != namespace {
			kept = append(kept, o)
		}
	}

	objects := make(map[string][]map[string]any, len(in.Objects))
	for key, list := range in.Objects {
		objects[key] = list
	}

	objects[res] = kept
	in.Objects = objects

	return in, nil
}

// pluginActionChecks are the can-I rows of the plugins' actions, key plugin:<id>:<action>:
// each verb of the action's rbac, and the get of the object it reads first, namespaced as
// the action's resource is.
func pluginActionChecks(compiled []*plugin.Compiled) map[string][]accessNeed {
	out := map[string][]accessNeed{}

	for _, c := range compiled {
		for _, a := range c.Actions {
			if a.RBAC == nil {
				continue
			}

			decl := c.Plugin.Resources[a.On]
			namespaced := decl.Scope == "Namespaced"
			resource, subresource, _ := strings.Cut(a.RBAC.Resource, "/")

			needs := make([]accessNeed, 0, len(a.RBAC.Verbs)+1)
			for _, verb := range a.RBAC.Verbs {
				needs = append(needs, accessNeed{ssarAttributes{Verb: verb, Group: a.RBAC.Group, Resource: resource, Subresource: subresource}, namespaced})
			}

			needs = append(needs, accessNeed{ssarAttributes{Verb: "get", Group: decl.Group, Resource: decl.Resource}, namespaced})
			out[pluginActionKey(c.Plugin.ID, a.Name)] = needs
		}
	}

	return out
}

// actionChecks are kubeActionChecks and the enabled user plugins' actions: those come and go
// at run time, so they are never written into the shared table.
func actionChecks() map[string][]accessNeed {
	user := pluginActionChecks(enabledUserPlugins())
	if len(user) == 0 {
		return kubeActionChecks
	}

	out := maps.Clone(kubeActionChecks)
	maps.Copy(out, user)

	return out
}

// The built-in plugins' actions join the app's can-I rows: KubeActionAccess lists them. A
// built-in that does not compile (a bug the test suite catches) adds none rather than stop
// the library from loading.
func init() {
	_ = safeCall(func() error { //nolint:errcheck // no rows is the answer
		for key, needs := range pluginActionChecks(plugin.Builtins()) {
			kubeActionChecks[key] = needs
		}

		return nil
	})
}

// setPluginAllowed fills the render tree's action buttons with the can-I answers, from the
// access cache KubeActionAccess fills (asked when it holds no answer for this namespace).
// An installed plugin's action missing from the rows (a test catalog) carries no answer.
func setPluginAllowed(ctx context.Context, k *kubeClient, target kubeTarget, c *plugin.Compiled, tree *plugin.Tree, namespace string) {
	if !tree.HasActions() {
		return
	}

	access, err := actionAccess.get(target.key()+"\x00"+namespace, func() (kubeActionAccess, error) {
		return readActionAccess(ctx, k, namespace), nil
	})
	if err != nil {
		return
	}

	tree.SetAllowed(func(action string) *bool {
		a, ok := access.Actions[pluginActionKey(c.Plugin.ID, action)]
		if !ok || a.Unknown {
			return nil // unknown: the button stays, the API server decides
		}

		allowed := a.Allowed

		return &allowed
	})
}

// setAllPluginAllowed answers every button the same: false for a plugin that is not
// installed (KubePluginAction runs installed plugins only), true in the demo (as
// KubeActionAccess answers there; the demo refuses the action itself).
func setAllPluginAllowed(tree *plugin.Tree, allowed bool) {
	tree.SetAllowed(func(string) *bool { return &allowed })
}
