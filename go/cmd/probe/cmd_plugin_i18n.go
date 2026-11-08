package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cyrinux/ichor/go/plugin"
)

// plugin-i18n FILE [-fill JSON] [-no-glossary] [-dry-run]: translates a plugin's labels from the
// built-in plugins' glossary and from a translator's JSON answer ({locale: {English: text}}),
// checked before it is written; the file is rewritten with its i18n section completed. The JSON
// line on stdout lists what remains untranslated: what the CRD request workflow asks a model for.
var pluginI18nCommands = []command{{name: "plugin-i18n", args: "FILE [-fill JSON] [-no-glossary] [-dry-run]", noConfig: true, run: func(e env) (string, error) {
	return noOutput, pluginI18n(flag.Args()[1:], os.Stdout, os.Stderr)
}}}

// i18nReport is plugin-i18n's line: the translations written and the labels still English.
type i18nReport struct {
	OK      bool                `json:"ok"`
	ID      string              `json:"id"`
	Filled  int                 `json:"filled"`
	Missing map[string][]string `json:"missing"`
}

func pluginI18n(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("plugin-i18n", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fill := fs.String("fill", "", "a JSON file of translations to apply, {locale: {English: translation}}")
	noGlossary := fs.Bool("no-glossary", false, "do not take translations from the built-in plugins")
	dryRun := fs.Bool("dry-run", false, "report without rewriting the file")

	file, err := fileAndFlags(fs, args)
	if err != nil {
		return fmt.Errorf("%w\nusage: plugin-i18n FILE [-fill JSON] [-no-glossary] [-dry-run]", err)
	}

	src, err := os.ReadFile(argPath(file))
	if err != nil {
		return err
	}

	p, _, problems := checkPluginFile(file)
	if plugin.HasErrors(problems) {
		printProblems(stderr, file, problems)

		return errProblems(problems)
	}

	translations := plugin.Translations{}
	if !*noGlossary {
		translations = plugin.GlossaryOf(builtinPlugins()).Fill(p, plugin.Locales)
	}

	if *fill != "" {
		answer, err := readTranslations(argPath(*fill))
		if err != nil {
			return fmt.Errorf("%s: %w", *fill, err)
		}

		if err := plugin.Check(p, plugin.Locales, answer); err != nil {
			return fmt.Errorf("%s: %w", *fill, err)
		}

		translations = overlay(translations, answer)
	}

	out, err := plugin.MergeI18n(src, p, translations)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}

	merged, err := plugin.Load(file, out)
	if err != nil {
		return fmt.Errorf("%s: the merged file does not load: %w", file, err)
	}

	if _, err := plugin.Compile(merged); err != nil {
		return fmt.Errorf("%s: the merged file does not compile: %w", file, err)
	}

	if !*dryRun && len(translations) > 0 {
		if err := os.WriteFile(argPath(file), out, 0o600); err != nil {
			return err
		}
	}

	return printJSON(stdout, i18nReport{OK: true, ID: p.ID, Filled: countTranslations(translations), Missing: plugin.Missing(merged, plugin.Locales)})
}

func builtinPlugins() []*plugin.Plugin {
	var out []*plugin.Plugin
	for _, c := range plugin.Builtins() {
		out = append(out, c.Plugin)
	}

	return out
}

func readTranslations(path string) (plugin.Translations, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var tr plugin.Translations
	if err := json.Unmarshal(src, &tr); err != nil {
		return nil, errors.New("not a JSON object of {locale: {English: translation}}")
	}

	return tr, nil
}

// overlay lays the answer over the glossary's translations, the answer winning.
func overlay(base, over plugin.Translations) plugin.Translations {
	out := plugin.Translations{}

	for _, src := range []plugin.Translations{base, over} {
		for locale, m := range src {
			if out[locale] == nil {
				out[locale] = map[string]string{}
			}

			for text, translation := range m {
				out[locale][text] = translation
			}
		}
	}

	return out
}

func countTranslations(tr plugin.Translations) int {
	n := 0
	for _, m := range tr {
		n += len(m)
	}

	return n
}
