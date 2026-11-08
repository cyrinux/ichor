package ichorgo

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Writing a patch into a node's file of the repository, line by line: a changed scalar is
// rewritten in place (its quoting kept), a changed list or mapping replaces its block, a value
// the file does not hold yet is added under its nearest parent with the file's indentation, and
// a value a shared patch sets instead is skipped with that file named. Every other line, its
// comments included, stays as it was.

var (
	errWriteBackNoFile    = errors.New("the repository has no file for this node: create one")
	errWriteBackTemplated = errors.New("the file holds ${...} substitutions the app does not resolve: edit it by hand")
)

type talosWriteBackOptions struct {
	// Hostname is written into a file created for the node.
	Hostname string `json:"hostname"`
	// Create: an empty file becomes a new node file holding the patch.
	Create bool `json:"create"`
	// SharedFiles are the node's shared patches (path → content): a value only they set is
	// skipped rather than overridden in the node's file.
	SharedFiles map[string]string `json:"sharedFiles"`
}

type talosWriteBack struct {
	Content string          `json:"content"`
	Changed bool            `json:"changed"`
	Line    int             `json:"line"`
	Skipped []talosRepoSkip `json:"skipped"`
	Created bool            `json:"created"`
}

// talosLeaf is one value of a patch: the document it belongs to, its path, its value (a
// scalar or a list; a mapping only when empty).
type talosLeaf struct {
	doc   string
	ident map[string]any // apiVersion, kind, name of a multi-doc document
	path  []string
	value any
}

// TalosRepoWriteBack writes patchYAML (TalosConfigPatchFrom's patch) into fileContent, the
// node's file of the repository: {"content","changed","line","skipped":[{"path","why"}],
// "created"}. optionsJSON: {"hostname","create","sharedFiles":{path: content}}. A value a
// shared file sets and the node's file does not is skipped; an empty file with create becomes
// a new node file; a patch setting a secret is refused. Local.
func TalosRepoWriteBack(fileContent, patchYAML, optionsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	var opts talosWriteBackOptions
	if strings.TrimSpace(optionsJSON) != "" {
		if err := json.Unmarshal([]byte(optionsJSON), &opts); err != nil {
			return "", fmt.Errorf("read the options: %w", err)
		}
	}

	res, err := talosRepoWriteBack(fileContent, patchYAML, opts)
	if err != nil {
		return "", err
	}

	return toJSON(res)
}

func talosRepoWriteBack(content, patchYAML string, opts talosWriteBackOptions) (talosWriteBack, error) {
	leaves, err := talosPatchLeaves(patchYAML)
	if err != nil {
		return talosWriteBack{}, err
	}

	for _, l := range leaves {
		if p := strings.Join(l.path, "."); l.doc == talosV1Alpha1 && talosSecretPath(p) || containsRedacted(l.value) {
			return talosWriteBack{}, fmt.Errorf("the change sets %s: secrets are never written to the repository", p)
		}
	}

	if strings.TrimSpace(content) == "" {
		if !opts.Create {
			return talosWriteBack{}, errWriteBackNoFile
		}

		return newNodeFile(leaves, opts.Hostname)
	}

	if strings.Contains(content, "${") {
		return talosWriteBack{}, errWriteBackTemplated
	}

	res := talosWriteBack{Content: content, Skipped: []talosRepoSkip{}}

	var written []talosLeaf

	for _, l := range leaves {
		docs, err := fileDocs(res.Content)
		if err != nil {
			return talosWriteBack{}, fmt.Errorf("read the node's file: %w", err)
		}

		i := slices.IndexFunc(docs, func(d fileDoc) bool { return d.key == l.doc })
		if i < 0 {
			res.Content = appendDocument(res.Content, l)
			written = append(written, l)

			continue
		}

		if !holdsPath(docs[i].node, l.path) {
			if shared := sharedOwner(opts.SharedFiles, l); shared != "" {
				res.Skipped = append(res.Skipped, talosRepoSkip{Path: strings.Join(l.path, "."), Why: "set in " + shared})

				continue
			}
		}

		next, err := writeLeaf(res.Content, docs[i].node, l)
		if err != nil {
			res.Skipped = append(res.Skipped, talosRepoSkip{Path: strings.Join(l.path, "."), Why: err.Error()})

			continue
		}

		res.Content = next
		written = append(written, l)
	}

	if err := verifyWriteBack(res.Content, written); err != nil {
		return talosWriteBack{}, err
	}

	res.Changed = res.Content != content
	res.Line = firstChangedLine(content, res.Content)

	return res, nil
}

// talosPatchLeaves are the values of a patch, in its order.
func talosPatchLeaves(patchYAML string) ([]talosLeaf, error) {
	docs, err := yamlDocuments([]byte(patchYAML))
	if err != nil {
		return nil, fmt.Errorf("read the patch: %w", err)
	}

	var leaves []talosLeaf

	for _, d := range docs {
		m, ok := d.(map[string]any)
		if !ok {
			continue
		}

		key := talosDocKey(m)
		ident := map[string]any{}

		if key != talosV1Alpha1 {
			for _, k := range []string{"apiVersion", "kind", "name"} {
				if v, ok := m[k]; ok {
					ident[k] = v
				}
			}
		}

		var walk func(path []string, v any)
		walk = func(path []string, v any) {
			if mm, ok := v.(map[string]any); ok && len(mm) > 0 {
				for _, k := range keysSorted(mm) {
					if len(path) == 0 && ident[k] != nil {
						continue
					}

					walk(append(slices.Clone(path), k), mm[k])
				}

				return
			}

			leaves = append(leaves, talosLeaf{doc: key, ident: ident, path: path, value: v})
		}

		walk(nil, m)
	}

	if len(leaves) == 0 {
		return nil, errors.New("the patch changes nothing")
	}

	return leaves, nil
}

// fileDoc is one document of a file, with its key and parsed tree (lines are the file's).
type fileDoc struct {
	key  string
	node *yaml.Node
}

func fileDocs(content string) ([]fileDoc, error) {
	dec := yaml.NewDecoder(strings.NewReader(content))

	var docs []fileDoc

	for {
		var n yaml.Node

		err := dec.Decode(&n)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return docs, nil
			}

			return nil, err
		}

		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}

		m, _ := v.(map[string]any)
		if len(n.Content) == 0 || m == nil {
			continue
		}

		docs = append(docs, fileDoc{key: talosDocKey(m), node: n.Content[0]})
	}
}

// holdsPath tells whether the mapping root has a value at path.
func holdsPath(root *yaml.Node, path []string) bool {
	n := root
	for _, k := range path {
		if n = mapValue(n, k); n == nil {
			return false
		}
	}

	return true
}

// sharedOwner is the shared file that sets l's value, "" when none does.
func sharedOwner(shared map[string]string, l talosLeaf) string {
	for _, p := range keysSorted(shared) {
		docs, err := yamlDocuments([]byte(shared[p]))
		if err != nil {
			continue
		}

		for _, d := range docs {
			m, ok := d.(map[string]any)
			if ok && talosDocKey(m) == l.doc && yamlPath(m, l.path...) != nil {
				return p
			}
		}
	}

	return ""
}

// verifyWriteBack checks that content now holds each written value.
func verifyWriteBack(content string, written []talosLeaf) error {
	docs, err := talosDocsByKey(content)
	if err != nil {
		return fmt.Errorf("the written file is not YAML (%w): edit it by hand", err)
	}

	for _, l := range written {
		got := yamlPath(docs[l.doc], l.path...)
		if !reflect.DeepEqual(normalizeYAMLValue(got), normalizeYAMLValue(l.value)) {
			return fmt.Errorf("%s could not be written: edit the file by hand", strings.Join(l.path, "."))
		}
	}

	return nil
}

// normalizeYAMLValue makes values decoded from two texts comparable (an empty map and nil).
func normalizeYAMLValue(v any) any {
	if m, ok := v.(map[string]any); ok && len(m) == 0 {
		return nil
	}

	return v
}

func firstChangedLine(before, after string) int {
	a, b := strings.Split(before, "\n"), strings.Split(after, "\n")
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			return i + 1
		}
	}

	if len(a) == len(b) {
		return 0
	}

	return min(len(a), len(b)) + 1
}
