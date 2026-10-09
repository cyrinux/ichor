package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// KubeExplain is `kubectl explain` from the API server's OpenAPI v3 documents: the help the
// YAML editor shows for the field at the cursor. /openapi/v3 lists one document per
// group-version, each at a URL carrying a hash of its content; the document holds the
// schemas of the group-version's kinds, CRDs' structural schemas included.

// explainUnavailablePrefix starts every error that means the cluster has no help to give,
// so the apps can tell it from a failed call.
const explainUnavailablePrefix = "schema help unavailable"

var (
	errExplainNoOpenAPI = errors.New(explainUnavailablePrefix + ": the API server does not publish OpenAPI v3 (Kubernetes 1.24 or later does)")
	kubeKindPattern     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,62}$`)
)

const (
	// explainIndexTTL is how long the list of documents is reused: a CRD installed meanwhile
	// shows after it.
	explainIndexTTL = 2 * time.Minute
	// explainDocTTL bounds how long a document stays decoded; its URL's hash already makes
	// a cached one safe.
	explainDocTTL = 30 * time.Minute
	// explainMaxDocs bounds the decoded documents kept: core v1 alone is a few MB.
	explainMaxDocs = 4
	// explainMaxPath bounds the field path accepted.
	explainMaxPath = 512
)

// KubeExplain describes the field at fieldPath ("spec.template.spec.containers", "" for the
// kind itself) of kind in group/version, as JSON explainField: type, description, enum,
// whether required, and the fields it holds. Read-only.
func KubeExplain(configYAML, contextName, kubeServer, group, version, kind, fieldPath string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	group, version, kind = strings.TrimSpace(group), strings.TrimSpace(version), strings.TrimSpace(kind)

	segments, err := checkExplainRequest(group, version, kind, fieldPath)
	if err != nil {
		return "", err
	}

	var doc *explainDoc

	if isDemoContext(configYAML, contextName) {
		doc, err = demoExplainDoc()
	} else {
		target := kubeTarget{configYAML, contextName, kubeServer}
		doc, err = withKube(target, func(ctx context.Context, k *kubeClient) (*explainDoc, error) {
			return explainDocs.get(ctx, k, target.key(), group, version)
		})
	}

	if err != nil {
		return "", explainFetchError(err)
	}

	if doc == nil {
		return "", fmt.Errorf("%s: the API server publishes no schema for %s", explainUnavailablePrefix, groupVersion(group, version))
	}

	field, err := doc.explain(group, version, kind, segments)
	if err != nil {
		return "", err
	}

	return toJSON(field)
}

func checkExplainRequest(group, version, kind, fieldPath string) ([]string, error) {
	switch {
	case !kubeGroupPattern.MatchString(group):
		return nil, fmt.Errorf("invalid API group %q", group)
	case !kubeVersionPattern.MatchString(version):
		return nil, fmt.Errorf("invalid API version %q", version)
	case !kubeKindPattern.MatchString(kind):
		return nil, fmt.Errorf("invalid kind %q", kind)
	case len(fieldPath) > explainMaxPath:
		return nil, errors.New("field path too long")
	}

	return splitFieldPath(fieldPath), nil
}

// explainFetchError names a missing /openapi/v3 as unavailable help.
func explainFetchError(err error) error {
	if isNotFound(err) {
		return errExplainNoOpenAPI
	}

	return err
}

// explainDocs caches the documents of every cluster.
var explainDocs = newExplainCache()

// explainCache keeps each cluster's document list a short while and a few decoded
// documents, keyed by cluster and the document's URL (hash included).
type explainCache struct {
	mu      sync.Mutex
	now     func() time.Time
	indexes map[string]explainIndexEntry
	docs    map[string]explainDocEntry
}

type explainIndexEntry struct {
	paths map[string]string // "apis/apps/v1" → serverRelativeURL
	at    time.Time
}

type explainDocEntry struct {
	doc *explainDoc
	at  time.Time
}

func newExplainCache() *explainCache {
	return &explainCache{now: time.Now, indexes: map[string]explainIndexEntry{}, docs: map[string]explainDocEntry{}}
}

// get returns the decoded document of group/version, nil when the cluster publishes none
// for it. cluster keys the cache (kubeTarget.key()).
func (c *explainCache) get(ctx context.Context, k *kubeClient, cluster, group, version string) (*explainDoc, error) {
	paths, err := c.index(ctx, k, cluster)
	if err != nil {
		return nil, err
	}

	gvPath := "apis/" + group + "/" + version
	if group == "" {
		gvPath = "api/" + version
	}

	rel, ok := paths[gvPath]
	if !ok {
		return nil, nil
	}

	key := cluster + "\x00" + rel

	c.mu.Lock()
	entry, ok := c.docs[key]
	c.mu.Unlock()

	if ok && c.now().Sub(entry.at) < explainDocTTL {
		return entry.doc, nil
	}

	var raw oaDocument
	if err := k.get(ctx, rel, &raw); err != nil {
		return nil, err
	}

	doc := newExplainDoc(raw)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.evictLocked()
	c.docs[key] = explainDocEntry{doc: doc, at: c.now()}

	return doc, nil
}

// index returns the cluster's group-version paths and the URL of each document.
func (c *explainCache) index(ctx context.Context, k *kubeClient, cluster string) (map[string]string, error) {
	c.mu.Lock()
	entry, ok := c.indexes[cluster]
	c.mu.Unlock()

	if ok && c.now().Sub(entry.at) < explainIndexTTL {
		return entry.paths, nil
	}

	var index struct {
		Paths map[string]struct {
			ServerRelativeURL string `json:"serverRelativeURL"`
		} `json:"paths"`
	}

	if err := k.get(ctx, "/openapi/v3", &index); err != nil {
		return nil, err
	}

	paths := make(map[string]string, len(index.Paths))

	for gv, p := range index.Paths {
		rel := p.ServerRelativeURL
		if rel == "" {
			rel = "/openapi/v3/" + gv
		}

		if strings.HasPrefix(rel, "/openapi/v3/") {
			paths[gv] = rel
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for key, e := range c.indexes {
		if c.now().Sub(e.at) >= explainIndexTTL {
			delete(c.indexes, key)
		}
	}

	c.indexes[cluster] = explainIndexEntry{paths: paths, at: c.now()}

	return paths, nil
}

// evictLocked drops expired documents, then the oldest ones beyond explainMaxDocs-1, making
// room for one more.
func (c *explainCache) evictLocked() {
	for key, e := range c.docs {
		if c.now().Sub(e.at) >= explainDocTTL {
			delete(c.docs, key)
		}
	}

	for len(c.docs) >= explainMaxDocs {
		oldest := ""

		for key, e := range c.docs {
			if oldest == "" || e.at.Before(c.docs[oldest].at) {
				oldest = key
			}
		}

		delete(c.docs, oldest)
	}
}
