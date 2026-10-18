package plugin

import (
	"slices"
	"testing"
)

// builtinLocales are the apps' languages besides English (scripts/check-translations.py).
var builtinLocales = []string{"fr", "es", "uk", "de", "it"}

// Every built-in plugin renders its samples without an error and validates without a warning.
func TestBuiltinsRenderTheirSamples(t *testing.T) {
	for _, c := range Builtins() {
		if len(c.Warnings) > 0 {
			t.Errorf("%s: warnings %+v", c.Plugin.ID, c.Warnings)
		}

		for _, locale := range append([]string{""}, builtinLocales...) {
			tree, errs := Render(c, SamplesInput(c, locale, stubHeuristics{}, renderNow), renderNow)
			if len(errs) > 0 || len(tree.Errors) > 0 {
				t.Errorf("%s %q: %v %+v", c.Plugin.ID, locale, errs, tree.Errors)
			}

			if !tree.Active || len(tree.Cards) == 0 {
				t.Errorf("%s %q: tree %+v", c.Plugin.ID, locale, tree)
			}
		}
	}
}

// A built-in plugin's words are in every language of the apps, and its i18n maps hold only
// words it shows: the YAML is where these translations live now.
func TestBuiltinsTranslated(t *testing.T) {
	for _, c := range Builtins() {
		texts := translatableTexts(c.Plugin)

		for _, locale := range builtinLocales {
			m := c.Plugin.I18n[locale]

			for _, text := range texts {
				if m[text] == "" {
					t.Errorf("%s %s: no translation of %q", c.Plugin.ID, locale, text)
				}
			}

			for key := range m {
				if !slices.Contains(texts, key) {
					t.Errorf("%s %s: %q is translated but never shown", c.Plugin.ID, locale, key)
				}
			}
		}
	}
}

// translatableTexts are the distinct texts the render passes through i18n (see labelRefs).
func translatableTexts(p *Plugin) []string { return uniqueTexts(labelRefs(p)) }
