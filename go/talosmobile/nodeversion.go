package talosmobile

import (
	"context"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/safe"
)

// versionLookupTimeout bounds the extra Version call made only to name the node's version in
// a "not available" error.
const versionLookupTimeout = 4 * time.Second

// nodeVersion returns node's Talos version tag ("v1.14.1"), asked once per session ("" when
// the node does not answer).
func (s *session) nodeVersion(ctx context.Context, node string) string {
	if v, ok := s.versions.Load(node); ok {
		return v.(string) //nolint:forcetypeassert
	}

	if s.client == nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	resp, err := s.client.Version(withNode(ctx, node))
	if err != nil {
		return ""
	}

	version := first(resp.GetMessages()).GetVersion().GetTag()
	s.rememberVersion(node, version)

	return version
}

// refreshNodeVersion asks node's version again instead of trusting the session's cache: a
// node upgraded during the session answers with a new one.
func (s *session) refreshNodeVersion(ctx context.Context, node string) string {
	s.versions.Delete(node)

	return s.nodeVersion(ctx, node)
}

// rememberVersion caches node's version. A changed version also drops the node's cached
// resource definitions, since another Talos version registers other resource types.
func (s *session) rememberVersion(node, version string) {
	if version == "" {
		return
	}

	if old, loaded := s.versions.Swap(node, version); loaded && old != version {
		s.definitions.Delete(node)
	}
}

// friendly is friendlyError for a call made to node: an API or resource type the node
// lacks is reported with the node's Talos version.
func (s *session) friendly(node string, err error) string {
	if !isUnavailableAPI(err) {
		return friendlyError(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), versionLookupTimeout)
	defer cancel()

	return notAvailableOn(s.nodeVersion(ctx, node))
}

// resourceTypes are node's registered COSI resource definitions, listed once per session
// and node: the reliable way to know whether a resource type exists on its Talos version.
type resourceTypes struct {
	once sync.Once
	defs []*meta.ResourceDefinition
	err  error
}

func (s *session) resourceDefinitions(ctx context.Context, node string) ([]*meta.ResourceDefinition, error) {
	v, _ := s.definitions.LoadOrStore(node, &resourceTypes{})
	rt := v.(*resourceTypes) //nolint:forcetypeassert

	rt.once.Do(func() {
		list, err := safe.StateListAll[*meta.ResourceDefinition](withNode(ctx, node), s.client.COSI)
		if err != nil {
			rt.err = err

			return
		}

		rt.defs = safe.ToSlice(list, identity)
	})

	if rt.err != nil {
		// Not cached: the next call retries.
		s.definitions.CompareAndDelete(node, v)
	}

	return rt.defs, rt.err
}

// hasResourceType tells whether node registers the resource type (false when unknown).
func (s *session) hasResourceType(ctx context.Context, node, resourceType string) (bool, error) {
	defs, err := s.resourceDefinitions(ctx, node)
	if err != nil {
		return false, err
	}

	for _, rd := range defs {
		if rd.Metadata().ID() == resourceType || rd.TypedSpec().Type == resourceType {
			return true, nil
		}
	}

	return false, nil
}
