package ichorgo

import (
	"bytes"
	"maps"
	"path"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Finding the manifest that declares a resource among a source's files: kind, metadata.name
// and metadata.namespace (when both sides set one) of every document, List items included.
// The line is the source file's, where metadata.name is: "defined in", not the changed line.

// manifestMatch is one document declaring the resource: the file, the line of its
// metadata.name (1-based) and the document's index in the file (0-based).
type manifestMatch struct {
	Path          string `json:"path"`
	Line          int    `json:"line"`
	DocumentIndex int    `json:"documentIndex"`

	// renamed: the name matched only once kustomize's namePrefix/nameSuffix were removed.
	renamed bool
}

// kustomizeRename is what a kustomization.yaml does to the names and namespace of the
// resources of its directory and below.
type kustomizeRename struct {
	prefix, suffix string
	namespace      bool
}

// findManifest finds the documents of files (path → content) declaring kind namespace/name.
// prefix and suffix are the outermost rename (a Flux Kustomization's namePrefix/nameSuffix);
// each kustomization file adds its own for its directory and below. They are stripped from
// name before matching. kustomization files are read for their renames, never matched. An
// empty namespace matches any.
func findManifest(files map[string][]byte, kind, namespace, name string, prefix, suffix string) []manifestMatch {
	renames := map[string]kustomizeRename{}

	for p, b := range files {
		if isKustomizationFile(p) {
			renames[path.Dir(p)] = readKustomizeRename(b)
		}
	}

	out := []manifestMatch{}

	for _, p := range slices.Sorted(maps.Keys(files)) {
		if isKustomizationFile(p) || !isManifestFile(p) {
			continue
		}

		prefixes, suffixes, nsSet := renamesFor(p, renames, prefix, suffix)

		ns := namespace
		if nsSet {
			ns = "" // the kustomization overrides whatever the file says
		}

		out = append(out, matchDocuments(p, files[p], kind, ns, name, stripRenames(name, prefixes, suffixes))...)
	}

	return out
}

// renamesFor lists the prefixes and suffixes applied to the resources of p, outermost first,
// and whether a kustomization sets their namespace.
func renamesFor(p string, renames map[string]kustomizeRename, prefix, suffix string) ([]string, []string, bool) {
	prefixes, suffixes, nsSet := []string{prefix}, []string{suffix}, false

	dir := path.Dir(p)

	var chain []string
	for d := dir; ; d = path.Dir(d) {
		chain = append(chain, d)
		if d == "." || d == "/" {
			break
		}
	}

	slices.Reverse(chain)

	for _, d := range chain {
		r, ok := renames[d]
		if !ok {
			continue
		}

		prefixes, suffixes = append(prefixes, r.prefix), append(suffixes, r.suffix)
		nsSet = nsSet || r.namespace
	}

	return prefixes, suffixes, nsSet
}

// stripRenames removes the prefixes and suffixes from name, outermost first, each only when
// name carries it.
func stripRenames(name string, prefixes, suffixes []string) string {
	for _, p := range prefixes {
		if p != "" && len(name) > len(p) {
			name = strings.TrimPrefix(name, p)
		}
	}

	for _, s := range suffixes {
		if s != "" && len(name) > len(s) {
			name = strings.TrimSuffix(name, s)
		}
	}

	return name
}

func readKustomizeRename(b []byte) kustomizeRename {
	var k struct {
		NamePrefix string `yaml:"namePrefix"`
		NameSuffix string `yaml:"nameSuffix"`
		Namespace  string `yaml:"namespace"`
	}

	if yaml.Unmarshal(b, &k) != nil {
		return kustomizeRename{}
	}

	return kustomizeRename{prefix: k.NamePrefix, suffix: k.NameSuffix, namespace: k.Namespace != ""}
}

// matchDocuments matches each document of one file; a document that does not parse ends the
// file (a template, not a manifest).
func matchDocuments(p string, b []byte, kind, namespace, name, stripped string) []manifestMatch {
	var out []manifestMatch

	dec := yaml.NewDecoder(bytes.NewReader(b))

	for i := 0; ; i++ {
		var doc yaml.Node
		if dec.Decode(&doc) != nil {
			break // io.EOF, or the rest is not YAML
		}

		root := &doc
		if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
			root = root.Content[0]
		}

		for _, obj := range manifestObjects(root) {
			if line, renamed, ok := matchObject(obj, kind, namespace, name, stripped); ok {
				out = append(out, manifestMatch{Path: p, Line: line, DocumentIndex: i, renamed: renamed})
			}
		}
	}

	return out
}

// manifestObjects is the document itself, or the items of a List (v1 List, DeploymentList…).
// A kustomize document (Kustomization, Component) declares no resource.
func manifestObjects(root *yaml.Node) []*yaml.Node {
	if root.Kind != yaml.MappingNode {
		return nil
	}

	if strings.HasPrefix(scalarAt(root, "apiVersion"), "kustomize.config.k8s.io/") {
		return nil
	}

	if strings.HasSuffix(scalarAt(root, "kind"), "List") {
		if items := mapValue(root, "items"); items != nil && items.Kind == yaml.SequenceNode {
			return items.Content
		}
	}

	return []*yaml.Node{root}
}

// matchObject tells whether obj is kind namespace/name, or kind namespace/stripped (renamed),
// and the line of its metadata.name.
func matchObject(obj *yaml.Node, kind, namespace, name, stripped string) (int, bool, bool) {
	if obj.Kind != yaml.MappingNode || scalarAt(obj, "kind") != kind {
		return 0, false, false
	}

	meta := mapValue(obj, "metadata")
	if meta == nil || meta.Kind != yaml.MappingNode {
		return 0, false, false
	}

	n := mapValue(meta, "name")
	if n == nil || n.Kind != yaml.ScalarNode {
		return 0, false, false
	}

	if ns := scalarAt(meta, "namespace"); namespace != "" && ns != "" && ns != namespace {
		return 0, false, false
	}

	switch n.Value {
	case name:
		return n.Line, false, true
	case stripped:
		return n.Line, true, true
	}

	return 0, false, false
}

// mapValue is the value of key in the mapping m, nil when absent.
func mapValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}

	return nil
}

func scalarAt(m *yaml.Node, key string) string {
	if v := mapValue(m, key); v != nil && v.Kind == yaml.ScalarNode {
		return v.Value
	}

	return ""
}

// isKustomizationFile: the file kustomize reads in a directory.
func isKustomizationFile(p string) bool {
	switch path.Base(p) {
	case "kustomization.yaml", "kustomization.yml", "Kustomization":
		return true
	}

	return false
}

// isManifestFile: a file that may hold manifests.
func isManifestFile(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".yaml", ".yml", ".json":
		return true
	}

	return false
}
