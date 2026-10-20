// Command schemagen writes the JSON Schema of the plugin format (go/plugin/schema/
// plugin.schema.json) from the types of go/plugin/schema.go: their json names, their schema
// tags (required, enum=a|b, pattern=…, minItems=…, keys=…) and their doc comments. Run by
// go generate in go/plugin; a test fails when the committed file drifts.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/cyrinux/ichor/go/plugin"
)

func main() {
	out := flag.String("o", "schema/plugin.schema.json", "the file to write")
	dir := flag.String("dir", ".", "the go/plugin directory (its schema.go holds the doc comments)")
	flag.Parse()

	b, err := generate(*dir)
	if err == nil {
		err = os.WriteFile(*out, b, 0o644)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
}

// generate is the schema document, indented, with a final newline.
func generate(dir string) ([]byte, error) {
	docs, err := comments(filepath.Join(dir, "schema.go"))
	if err != nil {
		return nil, err
	}

	g := &gen{docs: docs, defs: map[string]any{}}
	root := g.object(reflect.TypeFor[plugin.Plugin]())
	root["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	root["$id"] = "https://raw.githubusercontent.com/cyrinux/ichor/main/go/plugin/schema/plugin.schema.json"
	root["title"] = "Ichor plugin (" + plugin.SchemaVersion + ")"
	root["$defs"] = g.defs
	root["properties"].(map[string]any)["schemaVersion"].(map[string]any)["const"] = plugin.SchemaVersion

	var b bytes.Buffer

	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // keep params.<name> readable
	enc.SetIndent("", "  ")

	if err := enc.Encode(root); err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// comments are the doc comments of schema.go's types ("Type") and fields ("Type.Field").
func comments(path string) (map[string]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	out := map[string]string{}

	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}

		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}

			out[ts.Name.Name] = text(gd.Doc, ts.Doc)

			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}

			for _, field := range st.Fields.List {
				for _, name := range field.Names {
					out[ts.Name.Name+"."+name.Name] = text(field.Doc, field.Comment)
				}
			}
		}
	}

	return out, nil
}

func text(groups ...*ast.CommentGroup) string {
	for _, g := range groups {
		if t := strings.Join(strings.Fields(g.Text()), " "); t != "" {
			return t
		}
	}

	return ""
}

type gen struct {
	docs map[string]string
	defs map[string]any
}

// The types whose YAML is not their Go shape.
var (
	exprType     = reflect.TypeFor[plugin.Expr]()
	templateType = reflect.TypeFor[plugin.Template]()
	toneType     = reflect.TypeFor[plugin.Tone]()
	titleType    = reflect.TypeFor[plugin.Title]()
	itemType     = reflect.TypeFor[plugin.Item]()
)

// schema is the schema of a field's type; a named struct becomes a $defs entry.
func (g *gen) schema(t reflect.Type) map[string]any {
	switch t {
	case exprType:
		return map[string]any{"type": []string{"string", "number", "boolean", "null"}, "description": "An expression: https://cyrinux.github.io/ichor/plugins/#expressions"}
	case templateType:
		return map[string]any{"type": []string{"string", "null"}, "description": "Text with {{ expression }} holes."}
	case toneType:
		return map[string]any{"type": []string{"string", "null"}, "description": "A tone (" + strings.Join(plugin.Tones, ", ") + ") or an expression that evaluates to one."}
	case titleType:
		return g.def(t, func() map[string]any {
			return map[string]any{"description": g.docs[t.Name()], "oneOf": []any{g.schema(exprType), g.object(t)}}
		})
	case itemType:
		return g.def(t, func() map[string]any {
			return map[string]any{"description": g.docs[t.Name()], "oneOf": []any{
				map[string]any{"type": "string", "description": "An action id (in an actions section)."}, g.object(t),
			}}
		})
	}

	switch t.Kind() {
	case reflect.Pointer:
		return g.schema(t.Elem())
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int:
		return map[string]any{"type": "integer"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": g.schema(t.Elem())}
	case reflect.Map:
		if t.Elem().Kind() == reflect.Interface {
			return map[string]any{"type": "object"}
		}

		return map[string]any{"type": "object", "additionalProperties": g.schema(t.Elem())}
	case reflect.Struct:
		return g.def(t, func() map[string]any { return g.object(t) })
	default: // any
		return map[string]any{}
	}
}

// def registers a named type under $defs once and refers to it.
func (g *gen) def(t reflect.Type, build func() map[string]any) map[string]any {
	if _, ok := g.defs[t.Name()]; !ok {
		g.defs[t.Name()] = map[string]any{} // a placeholder against recursion
		g.defs[t.Name()] = build()
	}

	return map[string]any{"$ref": "#/$defs/" + t.Name()}
}

// object is a struct's object schema, from its json names and schema tags.
func (g *gen) object(t reflect.Type) map[string]any {
	props := map[string]any{}
	required := []string{}

	for f := range t.Fields() {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if !f.IsExported() || name == "-" || name == "" {
			continue
		}

		s := g.schema(f.Type)
		if _, isRef := s["$ref"]; isRef {
			s = map[string]any{"allOf": []any{s}}
		}

		if doc := g.docs[t.Name()+"."+f.Name]; doc != "" {
			s["description"] = doc
		}

		if g.tags(f.Tag.Get("schema"), s) {
			required = append(required, name)
		}

		props[name] = s
	}

	out := map[string]any{"type": "object", "additionalProperties": false, "properties": props}
	if doc := g.docs[t.Name()]; doc != "" {
		out["description"] = doc
	}

	if len(required) > 0 {
		out["required"] = required
	}

	return out
}

// tags applies a schema tag to s and reports whether the field is required.
func (g *gen) tags(tag string, s map[string]any) bool {
	required := false

	for part := range strings.SplitSeq(tag, ",") {
		key, value, _ := strings.Cut(part, "=")

		switch key {
		case "required":
			required = true
		case "enum":
			s["enum"] = strings.Split(value, "|")
		case "pattern":
			s["pattern"] = value
		case "keys":
			s["propertyNames"] = map[string]any{"pattern": value}
		case "minItems", "minProperties", "minimum", "maximum":
			n, _ := strconv.Atoi(value)
			s[key] = n
		}
	}

	return required
}
