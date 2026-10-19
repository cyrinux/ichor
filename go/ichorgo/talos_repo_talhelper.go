package ichorgo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	pathpkg "path"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

// A talhelper repository: talconfig.yaml lists the nodes and the patches of each, the global
// ones first, then the role's (controlPlane, worker), then the node's own, the order
// `talhelper genconfig` folds them in. Its node fields (installDisk, ipAddress...) become
// config too, but only its patches are compared.

const talhelperNote = "Only talconfig.yaml's patches are compared, not its node fields (installDisk, ipAddress…)."

// talhelperFiles is the node's files from the talconfig.yaml at cfgPath, false when it is not
// a talconfig or lists no node named hostname.
func talhelperFiles(files map[string][]byte, paths []string, cfgPath, hostname, role string) (talosRepoFiles, bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(files[cfgPath], &doc); err != nil || len(doc.Content) == 0 {
		return talosRepoFiles{}, false
	}

	root := doc.Content[0]
	entry, name := talconfigNode(mapValue(root, "nodes"), hostname)

	if entry == nil {
		return talosRepoFiles{}, false
	}

	base := pathpkg.Base(cfgPath)
	found := talosRepoFiles{Generator: talosGenTalhelper, Note: talhelperNote}
	found.Files = append(found.Files, talosRepoFile{
		Path: cfgPath, Kind: talosFileTalconfig, Line: mapValue(entry, "hostname").Line,
		Why: base + " lists " + name,
	})

	group := "worker"
	if controlPlane := scalarAt(entry, "controlPlane"); controlPlane == "true" || controlPlane == "" && role == "controlplane" {
		group = "controlPlane"
	}

	found.Files = append(found.Files, talconfigPatches(files, cfgPath, mapValue(root, "patches"), talosFileShared, base+" patches")...)
	found.Files = append(found.Files, talconfigPatches(files, cfgPath, mapValue(mapValue(root, group), "patches"), talosFileShared, base+" "+group+".patches")...)
	found.Files = append(found.Files, talconfigPatches(files, cfgPath, mapValue(entry, "patches"), talosFileNode, base+" nodes["+name+"].patches")...)

	for _, p := range paths {
		if docs, err := yamlDocuments(files[p]); err == nil {
			if why, ok := secretsFile(p, docs); ok {
				found.Files = append(found.Files, talosRepoFile{Path: p, Kind: talosFileSecrets, Why: why})
			}
		}
	}

	return found, true
}

// talconfigNode is the entry of nodes naming hostname (its short name matches too), with the
// name it gives.
func talconfigNode(nodes *yaml.Node, hostname string) (*yaml.Node, string) {
	if nodes == nil || nodes.Kind != yaml.SequenceNode {
		return nil, ""
	}

	for _, n := range nodes.Content {
		if name := scalarAt(n, "hostname"); name != "" {
			if same, _ := sameHost(name, hostname); same {
				return n, name
			}
		}
	}

	return nil, ""
}

// talconfigPatches are the entries of a patches list: "@./file" names a file next to
// talconfig.yaml, anything else is an inline patch, known by its line.
func talconfigPatches(files map[string][]byte, cfgPath string, list *yaml.Node, kind, why string) []talosRepoFile {
	if list == nil || list.Kind != yaml.SequenceNode {
		return nil
	}

	var out []talosRepoFile

	for _, item := range list.Content {
		ref, isFile := strings.CutPrefix(item.Value, "@")
		if item.Kind != yaml.ScalarNode || !isFile {
			out = append(out, talosRepoFile{
				Path: cfgPath + talosInlineMark + strconv.Itoa(item.Line), Kind: talosFileTalconfig,
				Line: item.Line, Why: "inline patch in " + why, Patch: true,
			})

			continue
		}

		p := pathpkg.Clean(pathpkg.Join(pathpkg.Dir(cfgPath), strings.TrimSpace(ref)))
		if _, ok := files[p]; !ok {
			out = append(out, talosRepoFile{Path: p, Kind: kind, Why: why + ", but not in the repository"})

			continue
		}

		out = append(out, talosRepoFile{Path: p, Kind: kind, Why: why, Patch: true})
	}

	return out
}

// talconfigInlinePatch is the text of the inline patch at line of a talconfig.yaml.
func talconfigInlinePatch(content []byte, line int) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("is not YAML: %w", err)
	}

	item := inlinePatchAt(&doc, line)
	if item == nil {
		return nil, errors.New("names no inline patch of talconfig.yaml")
	}

	if item.Kind == yaml.ScalarNode {
		return []byte(item.Value), nil
	}

	return yaml.Marshal(item)
}

// inlinePatchAt is the entry at line of a "patches" list anywhere under n.
func inlinePatchAt(n *yaml.Node, line int) *yaml.Node {
	if n == nil {
		return nil
	}

	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if v := n.Content[i+1]; n.Content[i].Value == "patches" && v.Kind == yaml.SequenceNode {
				for _, item := range v.Content {
					if item.Line == line && !strings.HasPrefix(item.Value, "@") {
						return item
					}
				}
			}
		}
	}

	for _, c := range n.Content {
		if found := inlinePatchAt(c, line); found != nil {
			return found
		}
	}

	return nil
}

// yamlDocuments decodes every document of content.
func yamlDocuments(content []byte) ([]any, error) {
	var docs []any

	dec := yaml.NewDecoder(bytes.NewReader(content))

	for {
		var v any

		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			return docs, nil
		}

		if err != nil {
			return nil, err
		}

		docs = append(docs, v)
	}
}
