package plugin

import (
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// rbacResourcePattern is rbac.resource: a resource, or resource/subresource.
var rbacResourcePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?(/[a-z][a-z0-9-]*)?$`)

// actionRequest checks what an action's request is built from: its resource can be a path
// segment, its verb's own keys (method and subresource for subresource, propagationPolicy
// for delete) are set only with that verb, its body has the verb's shape, and rbac names a
// group and resource the API server could know.
func (v *validator) actionRequest(path string, a Action, onOK bool) {
	if onOK {
		decl := v.p.Resources[a.On]
		if !apiGroupPattern.MatchString(decl.Group) || !apiResourcePattern.MatchString(decl.Resource) {
			v.errorf(path+".on", "%s's group %q and resource %q must be API path segments: the request is built from them", a.On, decl.Group, decl.Resource)
		}
	}

	if a.Subresource != "" && !subresourcePattern.MatchString(a.Subresource) {
		v.errorf(path+".subresource", "%q: a subresource is lowercase letters, digits and dashes", a.Subresource)
	}

	v.enum(path+".method", a.Method, Action{}, "Method")
	v.enum(path+".propagationPolicy", a.PropagationPolicy, Action{}, "PropagationPolicy")

	if a.Method != "" && a.Verb != "subresource" {
		v.errorf(path+".method", "only applies with verb subresource")
	}

	if a.PropagationPolicy != "" && a.Verb != "delete" {
		v.errorf(path+".propagationPolicy", "only applies with verb delete")
	}

	v.bodyShape(path+".body", a.Verb, a.Body)

	if a.RBAC != nil {
		if !apiGroupPattern.MatchString(a.RBAC.Group) {
			v.errorf(path+".rbac.group", "%q is not an API group", a.RBAC.Group)
		}

		if a.RBAC.Resource != "" && !rbacResourcePattern.MatchString(a.RBAC.Resource) {
			v.errorf(path+".rbac.resource", "%q: write resource or resource/subresource", a.RBAC.Resource)
		}

		for i, verb := range a.RBAC.Verbs {
			if !subresourcePattern.MatchString(verb) {
				v.errorf(path+".rbac.verbs["+strconv.Itoa(i)+"]", "%q is not an RBAC verb", verb)
			}
		}

		// A tag error is already reported by the verb's own enum check.
		verbs, _ := tagEnum(Action{}, "Verb")
		if onOK && a.RBAC.Resource != "" && slices.Contains(verbs, a.Verb) {
			v.rbacMatches(path+".rbac", a, v.p.Resources[a.On])
		}
	}
}

// rbacMatches: rbac asks for what the request needs, so that the can-I row
// (KubeActionAccess) answers for the request the action sends.
func (v *validator) rbacMatches(path string, a Action, decl Resource) {
	verb, resource := requestRBAC(a, decl)

	if a.RBAC.Group != decl.Group || a.RBAC.Resource != resource || !slices.Contains(a.RBAC.Verbs, verb) {
		v.errorf(path, "the request needs %s on %s (group %q): rbac asks for %s on %s (group %q)",
			verb, resource, decl.Group, strings.Join(a.RBAC.Verbs, ", "), a.RBAC.Resource, a.RBAC.Group)
	}
}

// requestRBAC is the RBAC verb and resource ("resource/subresource") of an action's request.
func requestRBAC(a Action, decl Resource) (verb, resource string) {
	switch a.Verb {
	case "delete":
		return "delete", decl.Resource
	case "create":
		return "create", decl.Resource
	case "subresource":
		switch a.Method {
		case http.MethodPost:
			verb = "create"
		case http.MethodPatch:
			verb = "patch"
		default:
			verb = "update"
		}

		return verb, decl.Resource + "/" + a.Subresource
	default:
		return "patch", decl.Resource
	}
}

// bodyShape: mergePatch, annotate and create take a map, jsonPatch a list of operations,
// subresource a map or nothing, delete nothing (its options are propagationPolicy).
func (v *validator) bodyShape(path, verb string, body any) {
	switch verb {
	case "mergePatch", "create":
		if _, ok := body.(map[string]any); !ok {
			v.errorf(path, "verb %s takes a map", verb)
		}
	case "annotate":
		v.annotations(path, body)
	case "jsonPatch":
		v.jsonPatch(path, body)
	case "subresource":
		if _, ok := body.(map[string]any); body != nil && !ok {
			v.errorf(path, "verb subresource takes a map, or no body")
		}
	case "delete":
		if body != nil {
			v.errorf(path, "verb delete takes no body (propagationPolicy sets its options)")
		}
	}
}

// annotations: an annotate body maps annotation keys to strings (templates), or null to
// remove one.
func (v *validator) annotations(path string, body any) {
	m, ok := body.(map[string]any)
	if !ok || len(m) == 0 {
		v.errorf(path, "verb annotate takes a map of annotations")

		return
	}

	for _, k := range sortedKeys(m) {
		if _, ok := m[k].(string); !ok && m[k] != nil {
			v.errorf(path+"."+k, "an annotation's value is a string (quote it), or null to remove it")
		}
	}
}

// jsonPatchOps are RFC 6902's operations.
var jsonPatchOps = []string{"add", "remove", "replace", "move", "copy", "test"}

func (v *validator) jsonPatch(path string, body any) {
	ops, ok := body.([]any)
	if !ok || len(ops) == 0 {
		v.errorf(path, "verb jsonPatch takes a list of operations ({op, path, value})")

		return
	}

	for i, raw := range ops {
		opath := path + "[" + strconv.Itoa(i) + "]"
		op, ok := raw.(map[string]any)

		if !ok {
			v.errorf(opath, "an operation is a map {op, path, value}")

			continue
		}

		if name, _ := op["op"].(string); !slices.Contains(jsonPatchOps, name) {
			v.errorf(opath+".op", "%q is not one of %s", op["op"], strings.Join(jsonPatchOps, ", "))
		}

		v.pointer(opath+".path", op["path"], true)

		name, _ := op["op"].(string)
		v.pointer(opath+".from", op["from"], name == "move" || name == "copy")
	}
}

// pointer: an operation's path and from are literal JSON pointers (RFC 6901), never
// templates: they say where the patch writes.
func (v *validator) pointer(path string, value any, required bool) {
	s, isString := value.(string)

	switch {
	case value == nil && !required:
	case !isString || !jsonPointer(s):
		v.errorf(path, "a literal JSON pointer (/spec/size; ~0 and ~1 escape ~ and /)")
	case strings.Contains(s, "{{"):
		v.errorf(path, "is literal: a template only writes a value")
	}
}

// jsonPointer: "/" then segments where ~ is only ~0 or ~1.
func jsonPointer(s string) bool {
	if !strings.HasPrefix(s, "/") {
		return false
	}

	for i := 0; i < len(s); i++ {
		if s[i] == '~' && (i+1 == len(s) || (s[i+1] != '0' && s[i+1] != '1')) {
			return false
		}
	}

	return true
}

// paramDefault: a default has the parameter's type (and is among an enum's values).
func (v *validator) paramDefault(path string, p Param) {
	// A tag error is already reported by the type's own enum check.
	types, _ := tagEnum(Param{}, "Type")
	known := slices.Contains(types, p.Type)
	if p.Default == nil || !known || (p.Type == "enum" && len(p.Values) == 0) {
		return
	}

	if _, err := paramValue(p, p.Default); err != nil {
		v.errorf(path+".default", "%s", err)
	}
}
