package plugin

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"
)

// Translating a plugin's labels without a hand: the words a draft shows are first looked up
// in a glossary made of the built-in plugins' i18n maps, then a translator (a maintainer, or
// the model the CRD request workflow asks) answers for the rest, and its answer is checked
// before it lands in the YAML. probe plugin-i18n drives it.

// Locales are the apps' languages besides English, in the order the plugin files list them.
var Locales = []string{"fr", "de", "es", "it", "uk"}

// Translations are, per locale, an English label and its translation: the shape of a plugin's
// i18n section and of what a translator returns.
type Translations map[string]map[string]string

// Glossary maps an English label to its translation per locale, learnt from plugins that
// already translate it.
type Glossary map[string]map[string]string

// GlossaryOf learns from the plugins' i18n maps. A label two plugins translate differently
// keeps the most frequent wording, the first alphabetically on a tie.
func GlossaryOf(plugins []*Plugin) Glossary {
	votes := map[string]map[string]map[string]int{}

	for _, p := range plugins {
		for locale, m := range p.I18n {
			for text, translation := range m {
				if votes[text] == nil {
					votes[text] = map[string]map[string]int{}
				}

				if votes[text][locale] == nil {
					votes[text][locale] = map[string]int{}
				}

				votes[text][locale][translation]++
			}
		}
	}

	g := Glossary{}

	for text, locales := range votes {
		g[text] = map[string]string{}

		for locale, counts := range locales {
			g[text][locale] = mostVoted(counts)
		}
	}

	return g
}

func mostVoted(counts map[string]int) string {
	words := slices.Sorted(func(yield func(string) bool) {
		for w := range counts {
			if !yield(w) {
				return
			}
		}
	})

	sort.SliceStable(words, func(i, j int) bool { return counts[words[i]] > counts[words[j]] })

	return words[0]
}

// Labels are the distinct texts the render translates, in file order.
func Labels(p *Plugin) []string {
	var out []string

	for _, r := range labelRefs(p) {
		if !slices.Contains(out, r.Text) {
			out = append(out, r.Text)
		}
	}

	return out
}

// Missing lists, per locale, the labels of p with no translation; a locale with none is absent.
func Missing(p *Plugin, locales []string) map[string][]string {
	out := map[string][]string{}

	for _, locale := range locales {
		for _, text := range Labels(p) {
			if _, ok := p.I18n[locale][text]; !ok {
				out[locale] = append(out[locale], text)
			}
		}
	}

	return out
}

// Fill returns the missing translations of p the glossary knows.
func (g Glossary) Fill(p *Plugin, locales []string) Translations {
	out := Translations{}

	for locale, texts := range Missing(p, locales) {
		for _, text := range texts {
			if translation, ok := g[text][locale]; ok {
				if out[locale] == nil {
					out[locale] = map[string]string{}
				}

				out[locale][text] = translation
			}
		}
	}

	return out
}

var percentHole = regexp.MustCompile(`%[0-9]*[a-z]`)

// Check refuses a translation that cannot be applied to p: an unknown locale, a text p does
// not show, an empty or multi-line wording, or one whose holes ({{ … }}, %d) differ from the
// English. The error names the first offender.
func Check(p *Plugin, locales []string, tr Translations) error {
	labels := Labels(p)

	for _, locale := range slices.Sorted(mapKeys(tr)) {
		if !slices.Contains(locales, locale) {
			return fmt.Errorf("%s: not one of the apps' languages (%s)", locale, strings.Join(locales, ", "))
		}

		for _, text := range slices.Sorted(mapKeys(tr[locale])) {
			if err := checkWording(text, tr[locale][text]); err != nil {
				return fmt.Errorf("%s %q: %w", locale, text, err)
			}

			if !slices.Contains(labels, text) {
				return fmt.Errorf("%s %q: not a label of the plugin", locale, text)
			}
		}
	}

	return nil
}

func checkWording(text, translation string) error {
	switch {
	case strings.TrimSpace(translation) == "":
		return fmt.Errorf("empty translation")
	case strings.ContainsAny(translation, "\n\r"):
		return fmt.Errorf("the translation spans several lines")
	case len(translation) > 4*len(text)+40:
		return fmt.Errorf("the translation is far longer than the English")
	}

	if want, got := sortedHoles(text), sortedHoles(translation); !slices.Equal(want, got) {
		return fmt.Errorf("the holes differ: %v in English, %v in the translation", want, got)
	}

	return nil
}

func sortedHoles(s string) []string {
	holes := append(templateHole.FindAllString(s, -1), percentHole.FindAllString(templateHole.ReplaceAllString(s, ""), -1)...)
	slices.Sort(holes)

	return holes
}

func mapKeys[V any](m map[string]V) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

var (
	topLevelI18n  = regexp.MustCompile(`(?m)^i18n:[ \t]*(#.*)?\r?\n`)
	topLevelKey   = regexp.MustCompile(`(?m)^[^\s#]`)
	samplesKey    = regexp.MustCompile(`(?m)^samples:`)
	leadingNotice = regexp.MustCompile(`(?:^|\n)((?:#[^\n]*\n)*)$`)
)

// MergeI18n adds tr to p's i18n section in src, the plugin file, and returns the new text.
// Labels in file order, locales in Locales' order, every text double-quoted as the built-in
// plugins write them; the rest of the file, comments included, is left as it is. A file
// without an i18n section gets one before `samples:` and the comment above it.
func MergeI18n(src []byte, p *Plugin, tr Translations) ([]byte, error) {
	merged := Translations{}

	for locale, m := range p.I18n {
		merged[locale] = map[string]string{}
		for text, translation := range m {
			merged[locale][text] = translation
		}
	}

	for locale, m := range tr {
		if merged[locale] == nil {
			merged[locale] = map[string]string{}
		}

		for text, translation := range m {
			merged[locale][text] = translation
		}
	}

	block, err := i18nBlock(p, merged)
	if err != nil {
		return nil, err
	}

	if loc := topLevelI18n.FindIndex(src); loc != nil {
		end := len(src)
		if next := topLevelKey.FindIndex(src[loc[1]:]); next != nil {
			end = loc[1] + next[0]
		}

		return slices.Concat(src[:loc[0]], block, src[end:]), nil
	}

	loc := samplesKey.FindIndex(src)
	if loc == nil {
		return nil, fmt.Errorf("no i18n and no samples section to put it before")
	}

	start := loc[0]
	if m := leadingNotice.FindSubmatchIndex(src[:start]); m != nil && m[2] >= 0 {
		start = m[2]
	}

	return slices.Concat(src[:start], block, src[start:]), nil
}

// i18nBlock writes the section: locales in Locales' order (unknown ones after, sorted), the
// plugin's labels in file order, then translated texts it no longer shows, sorted.
func i18nBlock(p *Plugin, merged Translations) ([]byte, error) {
	locales := slices.DeleteFunc(slices.Clone(Locales), func(l string) bool { return merged[l] == nil })
	for _, l := range slices.Sorted(mapKeys(merged)) {
		if !slices.Contains(locales, l) {
			locales = append(locales, l)
		}
	}

	labels := Labels(p)
	section := yMapping()

	for _, locale := range locales {
		texts := slices.Clone(labels)
		texts = slices.DeleteFunc(texts, func(t string) bool { _, ok := merged[locale][t]; return !ok })

		for _, t := range slices.Sorted(mapKeys(merged[locale])) {
			if !slices.Contains(texts, t) {
				texts = append(texts, t)
			}
		}

		m := yMapping()
		for _, t := range texts {
			m.Content = append(m.Content, yPair(t, yText(merged[locale][t]))...)
			m.Content[len(m.Content)-2].Style = yaml.DoubleQuotedStyle
		}

		section.Content = append(section.Content, yPair(locale, m)...)
	}

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(yMapping(yPair("i18n", section))); err != nil {
		return nil, err
	}

	if err := enc.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
