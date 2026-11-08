package kube

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// What an API group, a version and a resource may look like, checked before they enter a path.
var (
	GroupPattern    = regexp.MustCompile(`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*)?$`)
	VersionPattern  = regexp.MustCompile(`^v[0-9]+((alpha|beta)[0-9]+)?$`)
	ResourcePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)
)

// Resource names a resource of the API: "" Group for the core one.
type Resource struct {
	Group, Version, Name string
}

// NewResource checks the three parts of a resource's name.
func NewResource(group, version, name string) (Resource, error) {
	r := Resource{strings.TrimSpace(group), strings.TrimSpace(version), strings.TrimSpace(name)}

	switch {
	case !GroupPattern.MatchString(r.Group):
		return Resource{}, fmt.Errorf("invalid API group %q", r.Group)
	case !VersionPattern.MatchString(r.Version):
		return Resource{}, fmt.Errorf("invalid API version %q", r.Version)
	case !ResourcePattern.MatchString(r.Name):
		return Resource{}, fmt.Errorf("invalid resource %q", r.Name)
	}

	return r, nil
}

// Path is the collection, in namespace when set.
func (r Resource) Path(namespace string) string {
	prefix := "/apis/" + r.Group + "/" + r.Version
	if r.Group == "" {
		prefix = "/api/" + r.Version
	}

	if namespace != "" {
		prefix += "/namespaces/" + url.PathEscape(namespace)
	}

	return prefix + "/" + r.Name
}

// ScopedPath is the collection of resource in namespace ("" for every namespace) under group
// ("/api/v1", "/apis/apps/v1").
func ScopedPath(group, namespace, resource string) string {
	if namespace == "" {
		return group + "/" + resource
	}

	return group + "/namespaces/" + url.PathEscape(namespace) + "/" + resource
}
