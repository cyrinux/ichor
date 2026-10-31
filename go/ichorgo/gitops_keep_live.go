package ichorgo

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/kube/kubediff"
	"go.yaml.in/yaml/v4"
)

// Keeping a live change in Git: an object changed in the cluster by hand (kubectl scale, set
// image, edit) differs from what its GitOps tool would apply. The diff lists the plain values
// that differ (diffLiveChanges); the "keep" quick edit writes the live ones into the manifest,
// only where the file itself holds the wanted value, so a value set by an overlay, a patch, a
// substitution or a chart is left alone and reported instead of being written in the wrong place.

// keepTargets are the values of the "keep" edit to write: each change whose path the
// document has, as a plain value equal to the wanted one, with the index of its change in
// applied. The others are skipped, with why.
func keepTargets(root *yaml.Node, e quickEdit) (targets []quickEditTarget, applied []int, skipped []string, err error) {
	if len(e.Changes) == 0 {
		return nil, nil, nil, errors.New("say the live values to keep")
	}

	if len(e.Changes) > kubediff.MaxLiveChanges {
		return nil, nil, nil, fmt.Errorf("at most %d values at once", kubediff.MaxLiveChanges)
	}

	for i, c := range e.Changes {
		if !slices.Contains([]string{"str", "int", "float", "bool"}, c.Type) {
			return nil, nil, nil, fmt.Errorf("the value of %s has an unknown type %q", c.Label, c.Type)
		}

		if strings.ContainsAny(c.Live, "\n\r") || len(c.Live) > 1024 {
			return nil, nil, nil, fmt.Errorf("%s: a quick edit writes one line: edit it in the editor", c.Label)
		}

		n := keepNode(root, c.Path)

		switch {
		case n == nil:
			skipped = append(skipped, c.Label+": not set in this file")
		case n.Kind != yaml.ScalarNode:
			skipped = append(skipped, c.Label+": not a plain value in this file")
		case !keepSameValue(n.Value, c.Wanted):
			skipped = append(skipped, c.Label+": the file has "+n.Value+", set to "+c.Wanted+" elsewhere (an overlay, a patch or a substitution)")
		default:
			targets = append(targets, quickEditTarget{node: n, value: c.Live, tag: "!!" + c.Type})
			applied = append(applied, i)
		}
	}

	if len(targets) == 0 {
		return nil, nil, skipped, fmt.Errorf("none of the live values are set in this file: %s", strings.Join(skipped, "; "))
	}

	return targets, applied, skipped, nil
}

// keepNode is the node at path in root: a key per mapping, an index or name=<name> per list.
func keepNode(root *yaml.Node, path []string) *yaml.Node {
	n := root

	for _, seg := range path {
		if n == nil {
			return nil
		}

		if n.Kind == yaml.SequenceNode && strings.HasPrefix(seg, kubediff.NamePrefix) {
			name := strings.TrimPrefix(seg, kubediff.NamePrefix)
			i := slices.IndexFunc(n.Content, func(item *yaml.Node) bool { return scalarAt(item, "name") == name })

			if i < 0 {
				return nil
			}

			n = n.Content[i]

			continue
		}

		n = pathStep(n, seg)
	}

	return n
}

// keepSameValue: the file's text and the wanted value are the same value (8080 and
// "8080", 1.0 and 1, true and True).
func keepSameValue(file, wanted string) bool {
	if file == wanted {
		return true
	}

	if a, err := strconv.ParseFloat(file, 64); err == nil {
		if b, err := strconv.ParseFloat(wanted, 64); err == nil {
			return a == b
		}
	}

	if a, err := strconv.ParseBool(file); err == nil {
		if b, err := strconv.ParseBool(wanted); err == nil {
			return a == b
		}
	}

	return false
}

// verifyKeep parses the rewritten file again: same documents, and each applied change now
// holds its live value.
func verifyKeep(out string, count int, e quickEdit, applied []int) error {
	docs, err := parseQuickEditDocs(out)
	if err != nil || len(docs) != count {
		return errors.New("the rewritten file does not parse the same: edit it in the editor")
	}

	index, err := quickEditDocIndex(docs, e)
	if err != nil {
		return fmt.Errorf("the rewritten file does not parse the same: %w", err)
	}

	for _, i := range applied {
		c := e.Changes[i]
		if n := keepNode(docs[index], c.Path); n == nil || n.Kind != yaml.ScalarNode || n.Value != c.Live {
			return errors.New("the rewritten file does not hold the live value: edit it in the editor")
		}
	}

	return nil
}
