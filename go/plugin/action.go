package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cyrinux/ichor/go/plugin/expr"
)

// Actions at run time: the request an action sends (ActionBody), its parameters
// (ActionParams), and whether it needs more than its own object to decide (ActionNeedsContext).
// The request's path is built from the declared resource and the target the caller names,
// both checked here; a template only ever writes into the body.

// ActionTarget is the object an action runs on: the version its resource is served at (the
// declared one, or the group's preferred one resolved by the caller), its namespace ("" for
// a cluster-scoped resource) and its name.
type ActionTarget struct {
	Version   string
	Namespace string
	Name      string
}

// ActionRequest is the one request an action sends: Path is below the API server's root
// (/apis/<group>/<version>/namespaces/<ns>/<resource>/<name>[/<subresource>]), Body is
// JSON (nil for none).
type ActionRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
	// Pinned: the request carries the checked object's resourceVersion; a conflict means the
	// object changed since enabledWhen was checked.
	Pinned bool
}

// Content types of the requests.
const (
	contentJSON       = "application/json"
	contentMergePatch = "application/merge-patch+json"
	contentJSONPatch  = "application/json-patch+json"
)

// maxParamLen is the longest string parameter: a label or a short value, never a document.
const maxParamLen = 1024

// ActionDisabledError is an action whose enabledWhen does not hold on the object as the
// cluster has it now; Reason is its disabledReason.
type ActionDisabledError struct {
	Action string
	Reason string
}

func (e *ActionDisabledError) Error() string {
	return fmt.Sprintf("action %s is not available: %s", e.Action, e.Reason)
}

// ParamError is a parameter the action refuses; Param names it.
type ParamError struct {
	Param string
	Msg   string
}

func (e *ParamError) Error() string { return fmt.Sprintf("parameter %q: %s", e.Param, e.Msg) }

// Path segments: the shapes the API server accepts, so that nothing in a target can add a
// segment, a query or a fragment.
var (
	apiGroupPattern    = regexp.MustCompile(`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*)?$`)
	apiVersionPattern  = regexp.MustCompile(`^v[0-9]+((alpha|beta)[0-9]+)?$`)
	apiResourcePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
	apiNamePattern     = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	subresourcePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// action is the declared action id, or an error naming the ones there are.
func (c *Compiled) action(id string) (Action, error) {
	a, ok := c.Plugin.Actions[id]
	if !ok {
		return Action{}, fmt.Errorf("plugin %s has no action %q", c.Plugin.ID, id)
	}

	return a, nil
}

// ActionParams checks the parameters given to action id against its declaration and returns
// them typed, with the defaults filled in: an undeclared parameter, a missing one without a
// default, or a value of the wrong type (or not among an enum's values) is a *ParamError.
func (c *Compiled) ActionParams(id string, given map[string]any) (map[string]any, error) {
	a, err := c.action(id)
	if err != nil {
		return nil, err
	}

	for _, name := range sortedKeys(given) {
		if !slices.ContainsFunc(a.Params, func(p Param) bool { return p.Name == name }) {
			return nil, &ParamError{Param: name, Msg: "the action declares no such parameter"}
		}
	}

	out := make(map[string]any, len(a.Params))

	for _, p := range a.Params {
		v, ok := given[p.Name]
		if !ok || v == nil {
			if p.Default == nil {
				return nil, &ParamError{Param: p.Name, Msg: "is required"}
			}

			v = p.Default
		}

		typed, err := paramValue(p, v)
		if err != nil {
			return nil, &ParamError{Param: p.Name, Msg: err.Error()}
		}

		out[p.Name] = typed
	}

	return out, nil
}

// paramValue is v as p's type: a string (at most maxParamLen bytes), an int64, a bool, or
// one of an enum's values.
func paramValue(p Param, v any) (any, error) {
	switch p.Type {
	case "string":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("want a string, got %s", describe(v))
		}

		if len(s) > maxParamLen {
			return nil, fmt.Errorf("longer than %d bytes", maxParamLen)
		}

		return s, nil
	case "int":
		return paramInt(v)
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("want true or false, got %s", describe(v))
		}

		return b, nil
	case "enum":
		s, ok := v.(string)
		if !ok || !slices.Contains(p.Values, s) {
			return nil, fmt.Errorf("want one of %s, got %s", strings.Join(p.Values, ", "), describe(v))
		}

		return s, nil
	default:
		return nil, fmt.Errorf("unknown type %q", p.Type)
	}
}

// paramInt reads an integer: a JSON number without a fraction, or a YAML int.
func paramInt(v any) (any, error) {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n, nil
		}
	case float64:
		if n := int64(x); float64(n) == x && x >= -(1<<53) && x <= 1<<53 {
			return n, nil
		}
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case uint64:
		if x <= 1<<63-1 {
			return int64(x), nil
		}
	}

	return nil, fmt.Errorf("want an integer, got %s", describe(v))
}

func describe(v any) string {
	switch x := v.(type) {
	case string:
		if len(x) > 40 {
			x = x[:40] + "…"
		}

		return fmt.Sprintf("%q", x)
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", x)
	}
}

// ActionNeedsContext reports whether action id's enabledWhen, disabledReason or body may
// read more than its own object: a data service, or a field of its resource that joins
// another object. The caller then reads the plugin's resources, not only the object.
func (c *Compiled) ActionNeedsContext(id string) bool {
	a, err := c.action(id)
	if err != nil {
		return false
	}

	alone := expr.Scope{Names: []string{"fields", "health", "conditions", "now", "params"}}
	if !a.EnabledWhen.IsZero() && (a.EnabledWhen.Node() == nil || len(a.EnabledWhen.Node().Check(alone)) > 0) {
		return true
	}

	if tpl := a.DisabledReason.Parsed(); tpl != nil && len(tpl.Check(alone)) > 0 {
		return true
	}

	fieldAlone := expr.Scope{Names: []string{"fields", "conditions", "now", "left"}, Vars: []string{}}

	for _, f := range c.Fields[a.On] {
		if f.Expr.Node() == nil || len(f.Expr.Node().Check(fieldAlone)) > 0 {
			return true
		}
	}

	return false
}

// ActionBody builds the request action id sends to target. in holds the objects read (at
// least target itself, as the cluster has it now, under the action's resource): its fields
// and health are evaluated, enabledWhen must hold (else an *ActionDisabledError with the
// disabledReason), and the body's templates render with the object, its fields, params
// (from ActionParams), name, namespace and now. Values under metadata.annotations and
// metadata.labels are always strings; elsewhere a template that is one hole keeps its type,
// and one that reads null is an error. With an enabledWhen, a patch is pinned to the
// object's resourceVersion (see pinned). A create's apiVersion, kind and namespace must be
// the resource's and the target's.
func (c *Compiled) ActionBody(id string, target ActionTarget, in Input, params map[string]any, now time.Time) (ActionRequest, error) {
	a, err := c.action(id)
	if err != nil {
		return ActionRequest{}, err
	}

	decl := c.Plugin.Resources[a.On]

	collection, err := collectionPath(decl, target)
	if err != nil {
		return ActionRequest{}, err
	}

	r := newRenderer(c, in, now)
	r.indexObjects()
	r.evalObjects()
	r.evalDataServices()

	o := r.find(a.On, target.Namespace, target.Name)
	if o == nil {
		return ActionRequest{}, fmt.Errorf("%s %s was not read", decl.Kind, objectName(target))
	}

	if err := r.checkEnabled(id, a, o); err != nil {
		return ActionRequest{}, err
	}

	env := expr.Env{
		Object: o.raw, Fields: o.fields, Now: now, Params: params,
		Vars: map[string]expr.Value{"name": target.Name, "namespace": target.Namespace},
	}

	body, err := renderActionBody(a, env)
	if err != nil {
		return ActionRequest{}, fmt.Errorf("actions.%s.body: %w", id, err)
	}

	if a.Verb == "create" {
		if body, err = createBody(body, decl, target); err != nil {
			return ActionRequest{}, fmt.Errorf("actions.%s.body: %w", id, err)
		}
	}

	req, err := actionRequest(a, collection, target.Name, body, o.raw)
	if err != nil || a.EnabledWhen.IsZero() {
		return req, err
	}

	return pinned(req, o.raw)
}

func objectName(t ActionTarget) string {
	if t.Namespace == "" {
		return t.Name
	}

	return t.Namespace + "/" + t.Name
}

// collectionPath is the declared resource's collection in target's namespace, every segment
// checked: the group, resource and scope come from the plugin, the version, namespace and
// name from the caller.
func collectionPath(decl Resource, t ActionTarget) (string, error) {
	switch {
	case !apiGroupPattern.MatchString(decl.Group):
		return "", fmt.Errorf("invalid API group %q", decl.Group)
	case !apiVersionPattern.MatchString(t.Version):
		return "", fmt.Errorf("invalid API version %q", t.Version)
	case !apiResourcePattern.MatchString(decl.Resource):
		return "", fmt.Errorf("invalid resource %q", decl.Resource)
	case !validName(t.Name):
		return "", fmt.Errorf("invalid Kubernetes name %q", t.Name)
	case decl.Scope == "Namespaced" && !validName(t.Namespace):
		return "", fmt.Errorf("invalid Kubernetes namespace %q", t.Namespace)
	case decl.Scope != "Namespaced" && t.Namespace != "":
		return "", fmt.Errorf("%s is cluster-scoped: no namespace (%q given)", decl.Resource, t.Namespace)
	}

	path := "/apis/" + decl.Group + "/" + t.Version
	if decl.Group == "" {
		path = "/api/" + t.Version
	}

	if t.Namespace != "" {
		path += "/namespaces/" + url.PathEscape(t.Namespace)
	}

	return path + "/" + decl.Resource, nil
}

func validName(s string) bool { return apiNamePattern.MatchString(s) && !strings.Contains(s, "..") }

// find is the object of res named namespace/name.
func (r *renderer) find(res, namespace, name string) *object {
	for _, o := range r.objects[res] {
		meta, _ := o.raw["metadata"].(map[string]any)
		ns, _ := meta["namespace"].(string)
		n, _ := meta["name"].(string)

		if n == name && ns == namespace {
			return o
		}
	}

	return nil
}

// checkEnabled evaluates enabledWhen on o; a failing evaluation refuses too.
func (r *renderer) checkEnabled(id string, a Action, o *object) error {
	if a.EnabledWhen.IsZero() {
		return nil
	}

	if a.EnabledWhen.Node() == nil {
		return fmt.Errorf("actions.%s.enabledWhen does not parse", id)
	}

	env := r.objectEnv(o)

	v, err := a.EnabledWhen.Node().Eval(env)
	if err != nil {
		return fmt.Errorf("actions.%s.enabledWhen: %w", id, err)
	}

	if expr.Truthy(v) {
		return nil
	}

	reason := r.template("actions."+id+".disabledReason", a.DisabledReason, env)
	if reason == "" {
		reason = "its condition does not hold on the object"
	}

	return &ActionDisabledError{Action: id, Reason: reason}
}

// renderActionBody renders the body's templates. An annotate body is the annotations map
// itself, so its values are strings as under metadata.annotations.
func renderActionBody(a Action, env expr.Env) (any, error) {
	var at []string
	if a.Verb == "annotate" {
		at = []string{"metadata", "annotations"}
	}

	return renderBody(a.Body, env, at, a.Verb == "jsonPatch")
}

// renderBody renders every string of body as a template. at are the map keys from the root
// down to body ("[]" for a list element), each kept whole (an annotation key holds dots),
// for the strings-only maps: a value of metadata.annotations or metadata.labels is a string.
// In a jsonPatch body, an operation's op, path and from are literal (Validate refuses holes
// there). A template that is one hole and evaluates to null is an error: a null in the body
// (which deletes a field in a merge patch) is only ever a literal null of the YAML.
func renderBody(body any, env expr.Env, at []string, jsonPatch bool) (any, error) {
	switch b := body.(type) {
	case string:
		if jsonPatch && len(at) == 2 && at[0] == "[]" && slices.Contains(jsonPatchLiteral, at[1]) {
			return b, nil
		}

		return renderString(b, env, stringsOnly(at))
	case map[string]any:
		out := make(map[string]any, len(b))

		for k, v := range b {
			rendered, err := renderBody(v, env, append(slices.Clip(at), k), jsonPatch)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}

			out[k] = rendered
		}

		return out, nil
	case []any:
		out := make([]any, len(b))

		for i, v := range b {
			rendered, err := renderBody(v, env, append(slices.Clip(at), "[]"), jsonPatch)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}

			out[i] = rendered
		}

		return out, nil
	default:
		return b, nil
	}
}

// jsonPatchLiteral are the keys of a JSON patch operation that say where it applies.
var jsonPatchLiteral = []string{"op", "path", "from"}

// renderString renders one template: its typed value, or its text when strs.
func renderString(src string, env expr.Env, strs bool) (any, error) {
	tpl, err := expr.ParseTemplate(src)
	if err != nil {
		return nil, err
	}

	v, err := tpl.Value(env)
	if err != nil {
		return nil, err
	}

	if v == nil {
		return nil, fmt.Errorf("%s is null: write a literal null in the YAML to send one", src)
	}

	if strs {
		return expr.Display(v), nil
	}

	return v, nil
}

// stringsOnly: at is the place of a value of metadata.annotations or metadata.labels.
func stringsOnly(at []string) bool {
	n := len(at)

	return n >= 3 && at[n-3] == "metadata" && (at[n-2] == "annotations" || at[n-2] == "labels")
}

// createBody checks a new object against the target: its apiVersion, kind and namespace,
// when written, are the resource's and the target's; absent, apiVersion and kind are set.
func createBody(body any, decl Resource, t ActionTarget) (any, error) {
	m, ok := body.(map[string]any)
	if !ok {
		return nil, errors.New("verb create takes a map")
	}

	apiVersion := t.Version
	if decl.Group != "" {
		apiVersion = decl.Group + "/" + t.Version
	}

	out := maps.Clone(m)

	for _, kv := range [][2]string{{"apiVersion", apiVersion}, {"kind", decl.Kind}} {
		got, present := m[kv[0]]

		switch {
		case !present:
			out[kv[0]] = kv[1]
		case got != kv[1]:
			return nil, fmt.Errorf("%s is %v: the action creates a %s (%s)", kv[0], got, decl.Kind, apiVersion)
		}
	}

	meta, _ := m["metadata"].(map[string]any)
	if ns, present := meta["namespace"]; present && ns != t.Namespace {
		return nil, fmt.Errorf("metadata.namespace is %v: the object is created in %q", ns, t.Namespace)
	}

	return out, nil
}

// pinned ties a patch to the version of the object enabledWhen was checked on, so that an
// object changed in between is refused by the API server (a 409, or a failed test
// operation) rather than changed on a stale check: a merge patch carries
// metadata.resourceVersion, a JSON patch starts with a test of it. A delete has its uid
// precondition; a subresource PUT or POST and a create have no object version to carry.
func pinned(req ActionRequest, obj map[string]any) (ActionRequest, error) {
	meta, _ := obj["metadata"].(map[string]any)

	rv, _ := meta["resourceVersion"].(string)
	if rv == "" || req.Method != http.MethodPatch {
		return req, nil
	}

	var body any
	if err := json.Unmarshal(req.Body, &body); err != nil {
		return ActionRequest{}, fmt.Errorf("pin the body: %w", err)
	}

	switch b := body.(type) {
	case []any:
		test := map[string]any{"op": "test", "path": "/metadata/resourceVersion", "value": rv}
		body = append([]any{test}, b...)
	case map[string]any:
		m := maps.Clone(b)

		md, _ := m["metadata"].(map[string]any)
		md = maps.Clone(md)

		if md == nil {
			md = map[string]any{}
		}

		md["resourceVersion"] = rv
		m["metadata"] = md
		body = m
	}

	out, err := jsonRequest(req.Method, req.Path, req.ContentType, body)
	out.Pinned = err == nil

	return out, err
}

// actionRequest is the request of verb a.Verb: collection is the resource's collection path,
// already checked, name the object's.
func actionRequest(a Action, collection, name string, body any, obj map[string]any) (ActionRequest, error) {
	objectPath := collection + "/" + url.PathEscape(name)

	switch a.Verb {
	case "mergePatch":
		return jsonRequest(http.MethodPatch, objectPath, contentMergePatch, body)
	case "jsonPatch":
		return jsonRequest(http.MethodPatch, objectPath, contentJSONPatch, body)
	case "annotate":
		return jsonRequest(http.MethodPatch, objectPath, contentMergePatch, map[string]any{"metadata": map[string]any{"annotations": body}})
	case "subresource":
		return subresourceRequest(a, objectPath, body)
	case "delete":
		return jsonRequest(http.MethodDelete, objectPath, contentJSON, deleteOptions(a, obj))
	case "create":
		return jsonRequest(http.MethodPost, collection, contentJSON, body)
	default:
		return ActionRequest{}, fmt.Errorf("unknown verb %q", a.Verb)
	}
}

func subresourceRequest(a Action, objectPath string, body any) (ActionRequest, error) {
	if !subresourcePattern.MatchString(a.Subresource) {
		return ActionRequest{}, fmt.Errorf("invalid subresource %q", a.Subresource)
	}

	if body == nil {
		body = map[string]any{}
	}

	path := objectPath + "/" + a.Subresource

	switch a.Method {
	case "", http.MethodPut:
		return jsonRequest(http.MethodPut, path, contentJSON, body)
	case http.MethodPost:
		return jsonRequest(http.MethodPost, path, contentJSON, body)
	case http.MethodPatch:
		return jsonRequest(http.MethodPatch, path, contentMergePatch, body)
	default:
		return ActionRequest{}, fmt.Errorf("invalid method %q", a.Method)
	}
}

// deleteOptions are a delete's: its propagation policy, and the uid of the object
// enabledWhen was checked on as a precondition, so that an object deleted and created again
// in between is not the one deleted.
func deleteOptions(a Action, obj map[string]any) any {
	opts := map[string]any{"apiVersion": "v1", "kind": "DeleteOptions"}

	if a.PropagationPolicy != "" {
		opts["propagationPolicy"] = a.PropagationPolicy
	}

	meta, _ := obj["metadata"].(map[string]any)
	if uid, _ := meta["uid"].(string); uid != "" {
		opts["preconditions"] = map[string]any{"uid": uid}
	}

	return opts
}

func jsonRequest(method, path, contentType string, body any) (ActionRequest, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return ActionRequest{}, fmt.Errorf("encode the body: %w", err)
	}

	return ActionRequest{Method: method, Path: path, ContentType: contentType, Body: data}, nil
}

// SetAllowed sets every action button's allowed, the can-I answer of its action id; allowed
// returns nil when it does not know (the button then carries no answer). The tree is the
// caller's, just rendered.
func (t *Tree) SetAllowed(allowed func(action string) *bool) {
	for _, card := range t.Cards {
		for _, row := range card.Rows {
			for _, s := range row.Detail {
				items, ok := s.Items.([]TreeAction)
				if !ok {
					continue
				}

				for i := range items {
					items[i].Allowed = allowed(items[i].ID)
				}
			}
		}
	}
}

// HasActions reports whether any row shows an action button.
func (t *Tree) HasActions() bool {
	for _, card := range t.Cards {
		for _, row := range card.Rows {
			for _, s := range row.Detail {
				if items, ok := s.Items.([]TreeAction); ok && len(items) > 0 {
					return true
				}
			}
		}
	}

	return false
}
