package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Limits of a plugin file.
const (
	MaxSize  = 1 << 20 // bytes
	maxDepth = 64      // nesting of maps and lists (the format itself needs about 10)
	maxFlow  = 20_000  // [ and { characters, flow collections or not
)

// source is where a loaded plugin comes from: its file name, the sha256 of its bytes (the
// compile cache key), each YAML path's line, and the key order of each mapping.
type source struct {
	name  string
	hash  string
	lines map[string]int
	order map[string][]string
}

// FileName is the file the plugin was loaded from.
func (p *Plugin) FileName() string { return p.src.name }

// Line is the file line of a YAML path (fields.sleepinfo.asleep, cards.list.row.title,
// health.sleepinfo[0]), or of its nearest ancestor written in the file; 0 when none is.
func (p *Plugin) Line(path string) int { return p.src.line(path) }

// line is the line of path, or of its nearest ancestor that has one.
func (s source) line(path string) int {
	for path != "" {
		if l, ok := s.lines[path]; ok {
			return l
		}

		cut := strings.LastIndexAny(path, ".[")
		if cut < 0 {
			break
		}

		path = path[:cut]
	}

	return 0
}

// keys are the keys of the mapping at path in file order, then any other key of m sorted
// (a Plugin built in Go has no file order).
func keys[V any](s source, path string, m map[string]V) []string {
	out := make([]string, 0, len(m))
	seen := map[string]bool{}

	for _, k := range s.order[path] {
		if _, ok := m[k]; ok && !seen[k] {
			out = append(out, k)
			seen[k] = true
		}
	}

	rest := make([]string, 0, len(m)-len(out))
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}

	return append(out, sortedStrings(rest)...)
}

// LoadError is a file that does not load: its YAML does not parse, a key is unknown, a
// value has the wrong type, or the schema version is not this loader's.
type LoadError struct {
	File     string
	Problems []Problem
}

func (e *LoadError) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = fmt.Sprintf("%s:%d: %s", e.File, p.Line, p.Msg)
	}

	return strings.Join(lines, "\n")
}

// Load reads a plugin file strictly: an unknown key is an error with its line. Expressions
// are parsed here; their errors, and every reference, are reported by Validate.
func Load(name string, src []byte) (*Plugin, error) {
	fail := func(line int, format string, args ...any) error {
		return &LoadError{File: name, Problems: []Problem{{Line: line, Msg: fmt.Sprintf(format, args...), Severity: SeverityError}}}
	}

	if len(src) > MaxSize {
		return nil, fail(0, "the file is larger than 1 MiB")
	}

	// The YAML scanner's memory grows much faster than the file with flow collections
	// ({a: 1} 100,000 times takes hundreds of MB): a plugin has a few hundred at most.
	if n := bytes.Count(src, []byte("{")) + bytes.Count(src, []byte("[")); n > maxFlow {
		return nil, fail(0, "the file has %d [ or { (at most %d)", n, maxFlow)
	}

	var root yaml.Node
	if err := yaml.Load(src, &root); err != nil {
		if strings.Contains(err.Error(), "no documents in stream") {
			return nil, fail(1, "the file is empty")
		}

		return nil, &LoadError{File: name, Problems: yamlProblems(err)}
	}

	if root.Kind == 0 || (root.Kind == yaml.DocumentNode && len(root.Content) == 0) {
		return nil, fail(1, "the file is empty")
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fail(doc.Line, "a plugin is a map of keys (schemaVersion, id, …)")
	}

	if err := checkVersion(doc); err != nil {
		return nil, &LoadError{File: name, Problems: []Problem{*err}}
	}

	if problems := checkTree(doc); len(problems) > 0 {
		return nil, &LoadError{File: name, Problems: problems}
	}

	var p Plugin
	if err := doc.Load(&p, yaml.WithKnownFields()); err != nil {
		return nil, &LoadError{File: name, Problems: yamlProblems(err)}
	}

	sum := sha256.Sum256(src)
	p.src = source{name: name, hash: hex.EncodeToString(sum[:]), lines: map[string]int{}, order: map[string][]string{}}
	index(doc, "", p.src)

	return &p, nil
}

// checkVersion refuses a missing schemaVersion or another major version before the strict
// decode, whose errors would only confuse.
func checkVersion(doc *yaml.Node) *Problem {
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "schemaVersion" {
			continue
		}

		v := doc.Content[i+1]
		if v.Value == SchemaVersion {
			return nil
		}

		return &Problem{Path: "schemaVersion", Line: v.Line, Severity: SeverityError,
			Msg: fmt.Sprintf("schemaVersion %q is not supported: this app reads %s", v.Value, SchemaVersion)}
	}

	return &Problem{Path: "schemaVersion", Line: doc.Line, Severity: SeverityError, Msg: "schemaVersion is missing (" + SchemaVersion + ")"}
}

// checkTree refuses, before decoding, what the format cannot carry to the apps as JSON or
// what would only mislead: nesting deeper than maxDepth, keys that are not text, merge keys,
// empty list items, NaN and infinities. It does not follow aliases (the decoder bounds those).
func checkTree(doc *yaml.Node) []Problem {
	var out []Problem

	add := func(n *yaml.Node, format string, args ...any) {
		out = append(out, Problem{Line: n.Line, Msg: fmt.Sprintf(format, args...), Severity: SeverityError})
	}

	var walk func(n *yaml.Node, depth int)
	walk = func(n *yaml.Node, depth int) {
		if len(out) >= 20 {
			return
		}

		if depth > maxDepth {
			add(n, "nested deeper than %d levels", maxDepth)

			return
		}

		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k := n.Content[i]

				switch {
				case k.Tag == "!!merge" || k.Value == "<<":
					add(k, "merge keys (<<) are not supported: write the keys out")
				case k.Kind != yaml.ScalarNode || k.Tag != "!!str":
					add(k, "a key is text: quote %q", k.Value)
				}

				walk(n.Content[i+1], depth+1)
			}
		case yaml.SequenceNode:
			for _, item := range n.Content {
				if item.Tag == "!!null" {
					add(item, "an empty list item")
				}

				walk(item, depth+1)
			}
		case yaml.ScalarNode:
			if v := strings.ToLower(n.Value); n.Tag == "!!float" && (strings.HasSuffix(v, ".nan") || strings.HasSuffix(v, ".inf")) {
				add(n, "%s is not a number JSON can carry", n.Value)
			}
		}
	}

	walk(doc, 0)

	return out
}

// index records the line of every key and the key order of every mapping under path. It
// does not follow aliases, so an alias bomb costs nothing here.
func index(n *yaml.Node, path string, s source) {
	switch n.Kind {
	case yaml.MappingNode:
		order := make([]string, 0, len(n.Content)/2)

		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			child := k
			if path != "" {
				child = path + "." + k
			}

			order = append(order, k)
			s.lines[child] = n.Content[i].Line
			index(n.Content[i+1], child, s)
		}

		s.order[path] = order
	case yaml.SequenceNode:
		for i, item := range n.Content {
			child := path + "[" + strconv.Itoa(i) + "]"
			s.lines[child] = item.Line
			index(item, child, s)
		}
	}
}

var yamlFieldNotFound = regexp.MustCompile(`field (\S+) not found in type \S+`)

// yamlProblems turns the decoder's errors into problems with their lines.
func yamlProblems(err error) []Problem {
	var many *yaml.LoadErrors
	if errors.As(err, &many) && len(many.Errors) > 0 {
		out := make([]Problem, len(many.Errors))
		for i, e := range many.Errors {
			out[i] = yamlProblem(e.Mark.Line, e.Message)
		}

		return out
	}

	var one *yaml.LoadError
	if errors.As(err, &one) {
		return []Problem{yamlProblem(one.Mark.Line, one.Message)}
	}

	return []Problem{yamlProblem(0, err.Error())}
}

var leadingLine = regexp.MustCompile(`^line (\d+): `)

func yamlProblem(line int, msg string) Problem {
	// An unmarshaler's own error carries its line: "line 12: …".
	if m := leadingLine.FindStringSubmatch(msg); m != nil {
		line, _ = strconv.Atoi(m[1])
		msg = msg[len(m[0]):]
	}

	msg = yamlFieldNotFound.ReplaceAllString(msg, "unknown key $1")

	return Problem{Line: line, Msg: msg, Severity: SeverityError}
}
