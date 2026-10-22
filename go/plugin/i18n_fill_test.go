package plugin

import (
	"reflect"
	"strings"
	"testing"
)

func loadSource(t *testing.T, src string) *Plugin {
	t.Helper()

	p, err := Load("test.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestGlossaryKeepsTheMostFrequentWording(t *testing.T) {
	one := loadSource(t, gadgetPlugin)
	two := loadSource(t, strings.Replace(gadgetPlugin, `"Size": "Taille"`, `"Size": "Dimension"`, 1))
	three := loadSource(t, strings.Replace(gadgetPlugin, `"Size": "Taille"`, `"Size": "Dimension"`, 1))

	g := GlossaryOf([]*Plugin{one, two, three})
	if got := g["Size"]["fr"]; got != "Dimension" {
		t.Errorf("fr Size = %q", got)
	}

	if got := g["not ready"]["de"]; got != "nicht bereit" {
		t.Errorf("de not ready = %q", got)
	}

	// A tie keeps the first alphabetically, whatever the map order.
	g = GlossaryOf([]*Plugin{one, two})
	if got := g["Size"]["fr"]; got != "Dimension" {
		t.Errorf("tie: fr Size = %q", got)
	}
}

func TestMissingAndFill(t *testing.T) {
	p := loadSource(t, gadgetPlugin)

	missing := Missing(p, []string{"fr", "de", "es"})
	want := map[string][]string{
		"fr": {"not ready"},
		"es": {"too big", "not ready", "Big", "Small", "Size"},
	}

	if !reflect.DeepEqual(missing, want) {
		t.Errorf("missing %v", missing)
	}

	// Another plugin that knows "Size" and "not ready" in Spanish, and "not ready" in French.
	other := loadSource(t, strings.NewReplacer(
		"i18n:\n", "i18n:\n  es: { \"Size\": \"Tamaño\", \"not ready\": \"no listo\" }\n",
		`"Size": "Taille" }`, `"Size": "Taille", "not ready": "pas prêt" }`,
	).Replace(gadgetPlugin))

	filled := GlossaryOf([]*Plugin{other}).Fill(p, []string{"fr", "de", "es"})
	wantFilled := Translations{
		"fr": {"not ready": "pas prêt"},
		"es": {"Size": "Tamaño", "not ready": "no listo"},
	}

	if !reflect.DeepEqual(filled, wantFilled) {
		t.Errorf("filled %v", filled)
	}
}

func TestCheckRefusesWhatCannotBeApplied(t *testing.T) {
	p := loadSource(t, strings.Replace(gadgetPlugin, `message: "too big"`, `message: "too big: {{ fields.size }} of %d"`, 1))

	cases := []struct {
		name string
		tr   Translations
		want string
	}{
		{"unknown locale", Translations{"xx": {"Big": "Grand"}}, "xx: not one of the apps' languages"},
		{"not a label", Translations{"fr": {"Huge": "Énorme"}}, `fr "Huge": not a label of the plugin`},
		{"empty", Translations{"fr": {"Big": "  "}}, "empty translation"},
		{"two lines", Translations{"fr": {"Big": "Gr\nand"}}, "spans several lines"},
		{"lost hole", Translations{"fr": {"too big: {{ fields.size }} of %d": "trop grand : %d"}}, "the holes differ"},
		{"changed hole", Translations{"fr": {"too big: {{ fields.size }} of %d": "trop grand : {{ fields.sizes }} sur %d"}}, "the holes differ"},
		{"too long", Translations{"fr": {"Big": strings.Repeat("très ", 30) + "grand"}}, "far longer"},
	}

	for _, c := range cases {
		err := Check(p, Locales, c.tr)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", c.name, err, c.want)
		}
	}

	ok := Translations{"fr": {"too big: {{ fields.size }} of %d": "trop grand : %d sur {{ fields.size }}", "Big": "Grand"}, "uk": {"Size": "Розмір"}}
	if err := Check(p, Locales, ok); err != nil {
		t.Errorf("valid translations refused: %v", err)
	}
}

func TestMergeI18nReplacesTheSection(t *testing.T) {
	p := loadSource(t, gadgetPlugin)
	tr := Translations{"fr": {"not ready": "pas prêt"}, "es": {"Big": "Grande", "Size": "Tamaño"}}

	out, err := MergeI18n([]byte(gadgetPlugin), p, tr)
	if err != nil {
		t.Fatal(err)
	}

	// Everything before the i18n section is untouched.
	head := gadgetPlugin[:strings.Index(gadgetPlugin, "i18n:")]
	if !strings.HasPrefix(string(out), head) {
		t.Fatalf("the head changed:\n%s", out)
	}

	wantTail := `i18n:
  fr:
    "too big": "trop grand"
    "not ready": "pas prêt"
    "Big": "Grand"
    "Small": "Petit"
    "Size": "Taille"
  de:
    "too big": "zu groß"
    "not ready": "nicht bereit"
    "Big": "Groß"
    "Small": "Klein"
    "Size": "Größe"
  es:
    "Big": "Grande"
    "Size": "Tamaño"
`
	if tail := string(out[len(head):]); tail != wantTail {
		t.Errorf("tail:\n%s\nwant:\n%s", tail, wantTail)
	}

	merged := loadSource(t, string(out))
	if merged.I18n["es"]["Size"] != "Tamaño" || merged.I18n["fr"]["too big"] != "trop grand" {
		t.Errorf("reloaded i18n %v", merged.I18n)
	}
}

func TestMergeI18nInsertsBeforeTheSamplesAndTheirComment(t *testing.T) {
	src := strings.Replace(gadgetPlugin[:strings.Index(gadgetPlugin, "i18n:")], "samples:\n", "# Invented names.\n# One per rule.\nsamples:\n", 1)
	p := loadSource(t, src)

	out, err := MergeI18n([]byte(src), p, Translations{"uk": {"Big": "Великий"}})
	if err != nil {
		t.Fatal(err)
	}

	want := "            - { label: \"Size\", value: fields.size }\ni18n:\n  uk:\n    \"Big\": \"Великий\"\n# Invented names.\n# One per rule.\nsamples:\n"
	if !strings.Contains(string(out), want) {
		t.Errorf("got:\n%s", out)
	}

	if _, err := MergeI18n([]byte("schemaVersion: x\n"), p, nil); err == nil {
		t.Error("a file without samples should be refused")
	}
}

func TestLabelsAreInFileOrder(t *testing.T) {
	p := loadSource(t, gadgetPlugin)

	want := []string{"too big", "not ready", "Big", "Small", "Size"}
	if got := Labels(p); !reflect.DeepEqual(got, want) {
		t.Errorf("labels %v", got)
	}
}
