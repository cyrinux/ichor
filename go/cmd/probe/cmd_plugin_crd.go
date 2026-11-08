package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/cyrinux/ichor/go/ichorgo"
	"github.com/cyrinux/ichor/go/plugin"
)

// pluginFromCRD writes a plugin draft for a CRD manifest (plugin.Generate), checked as
// plugin-validate and plugin-lint -crd check one: the YAML to stdout or -out, the lint
// summary as one JSON line to stderr. A CRD a built-in plugin or a native integration
// already reads is refused unless -force.
func pluginFromCRD(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("plugin-from-crd", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "the plugin id (default: from the CRD's group)")
	name := fs.String("name", "", "the product name (default: the id)")
	website := fs.String("website", "", "the project's home page")
	category := fs.String("category", "", "the hub that lists it (default: data-services)")
	out := fs.String("out", "", "write the draft to this file instead of stdout")
	force := fs.Bool("force", false, "write a draft even for a CRD the app already reads")

	file, err := fileAndFlags(fs, args)
	if err != nil {
		return fmt.Errorf("%w\nusage: plugin-from-crd MANIFEST [-id ID] [-name NAME] [-website URL] [-category C] [-out FILE] [-force]", err)
	}

	src, err := os.ReadFile(argPath(file))
	if err != nil {
		return err
	}

	crd, err := plugin.LoadCRD(src)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}

	if by, ok := covered(crd.Spec.Group, crd.Spec.Names.Plural); ok && !*force {
		return fmt.Errorf("%s: already covered by %s (-force writes a draft anyway)", file, by)
	}

	draft, err := plugin.Generate(crd, plugin.GenerateOptions{ID: *id, Name: *name, Website: *website, Category: *category})
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}

	warnings, err := checkDraft(draft, crd)
	if err != nil {
		return err
	}

	if *out != "" {
		if err := os.WriteFile(argPath(*out), draft, 0o644); err != nil {
			return err
		}
	} else if _, err := stdout.Write(draft); err != nil {
		return err
	}

	p, _ := plugin.Load("draft.yaml", draft) //nolint:errcheck // checkDraft loaded it

	return printJSON(stderr, map[string]any{"ok": true, "id": p.ID, "warnings": warnings})
}

// checkDraft holds a draft to what plugin-validate checks; a draft that fails is a generator
// bug, never the CRD's. It answers how many warnings plugin-lint -crd gives it.
func checkDraft(draft []byte, crd *plugin.CRDManifest) (int, error) {
	p, err := plugin.Load("draft.yaml", draft)
	if err != nil {
		return 0, fmt.Errorf("the generated draft does not load (a generator bug): %w", err)
	}

	if problems := plugin.Validate(p); plugin.HasErrors(problems) {
		return 0, fmt.Errorf("the generated draft does not validate (a generator bug): %w", errProblems(problems))
	}

	return len(plugin.Lint(p, crd)), nil
}

// covered names what already reads group/plural: a built-in plugin, else a native integration.
func covered(group, plural string) (string, bool) {
	for _, c := range plugin.Builtins() {
		for _, res := range c.Plugin.Resources {
			if res.Group == group && res.Resource == plural {
				return "built-in plugin " + c.Plugin.ID, true
			}
		}
	}

	raw, err := ichorgo.SupportedIntegrations()
	if err != nil {
		return "", false
	}

	var list struct {
		Items []struct {
			ID     string   `json:"id"`
			Groups []string `json:"groups"`
		} `json:"items"`
	}

	if json.Unmarshal([]byte(raw), &list) != nil {
		return "", false
	}

	for _, it := range list.Items {
		if slices.Contains(it.Groups, group) {
			return "native integration " + it.ID, true
		}
	}

	return "", false
}
