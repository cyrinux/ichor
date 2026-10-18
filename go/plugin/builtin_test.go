package plugin

import (
	"regexp"
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

var (
	quoted = regexp.MustCompile(`"([^"\\]*)"`)
	// templateHole is a {{ … }} of a template: what is left outside them is words to translate.
	templateHole = regexp.MustCompile(`\{\{[^}]*\}\}`)
	letter       = regexp.MustCompile(`\pL`)
)

// translatableTexts are the labels, titles and messages the render passes through i18n, and the
// string literals of the row status texts.
func translatableTexts(p *Plugin) []string {
	var out []string

	add := func(s string) {
		if s != "" && letter.MatchString(templateHole.ReplaceAllString(s, "")) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}

	addItems := func(items []Item) {
		for _, it := range items {
			add(it.Label)
		}
	}

	for _, card := range p.Cards {
		if card.Title != nil {
			add(card.Title.One)
			add(card.Title.Other)
		}

		add(card.Empty)
		addItems(card.Badges)

		if card.Row == nil {
			continue
		}

		if card.Row.Status != nil {
			for _, m := range quoted.FindAllStringSubmatch(card.Row.Status.Text.Src, -1) {
				add(m[1])
			}
		}

		for _, s := range card.Row.Detail {
			add(s.Label)
			addItems(s.Items)

			if s.Type == "text" {
				add(s.Text.Src)
			}

			for _, col := range s.Columns {
				add(col.Label)
			}
		}
	}

	for _, rules := range p.Health {
		for _, r := range rules {
			add(r.Message.Src)
		}
	}

	for _, a := range p.Actions {
		add(a.Label)
		add(a.DisabledReason.Src)

		for _, prm := range a.Params {
			add(prm.Label)
		}
	}

	for _, f := range p.Findings {
		add(f.Title.Src)
		add(f.Detail.Src)
	}

	slices.Sort(out)

	return out
}
