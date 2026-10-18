package plugin

import (
	"net/http"
	"slices"
	"strings"
)

// actionRBAC: an action's rbac should ask for what its request needs, so that the can-I row
// (KubeActionAccess) answers for the request the action sends. Validate only checks rbac is
// there and well formed.
func (l *linter) actionRBAC() {
	for _, id := range keys(l.p.src, "actions", l.p.Actions) {
		a := l.p.Actions[id]

		decl, ok := l.p.Resources[a.On]
		if !ok || a.RBAC == nil {
			continue
		}

		verb, resource := requestRBAC(a, decl)
		path := "actions." + id + ".rbac"

		if a.RBAC.Group != decl.Group || a.RBAC.Resource != resource || !slices.Contains(a.RBAC.Verbs, verb) {
			l.warn(path, "the request needs %s on %s (group %q): rbac asks for %s on %s (group %q)",
				verb, resource, decl.Group, strings.Join(a.RBAC.Verbs, ", "), a.RBAC.Resource, a.RBAC.Group)
		}
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
