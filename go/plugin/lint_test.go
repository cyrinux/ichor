package plugin

import (
	"strings"
	"testing"
)

// gadgetPlugin reads gadgetCRD: it shows the size, has a Ready rule no sample triggers, a
// resource no card shows, no empty text, and French for some labels only.
const gadgetPlugin = `schemaVersion: ichor.dev/plugin/v1alpha1
id: demo-gadgets
name: Demo Gadgets
detect:
  - group: demo.example.com
resources:
  gadget:
    group: demo.example.com
    version: v1
    resource: gadgets
    kind: Gadget
    scope: Namespaced
  config:
    group: ""
    version: v1
    resource: configmaps
    kind: ConfigMap
    scope: Namespaced
fields:
  gadget:
    size: $.spec.size | default(0)
health:
  gadget:
    - when: fields.size > 100
      health: warning
      message: "too big"
    - when: "!conditions.Ready"
      health: critical
      message: "not ready"
cards:
  summary:
    title: '"Gadgets"'
  list:
    from: gadget
    row:
      title: $.metadata.name
      status: { text: 'fields.size > 100 ? "Big" : "Small"' }
      detail:
        - type: keyValues
          items:
            - { label: "Size", value: fields.size }
samples:
  gadget:
    - { metadata: { name: g1, namespace: demo-lab }, spec: { size: 500 }, status: { conditions: [{ type: Ready, status: "True" }] } }
  config:
    - { metadata: { name: c1, namespace: demo-lab } }
i18n:
  fr: { "too big": "trop grand", "Big": "Grand", "Small": "Petit", "Size": "Taille" }
  de: { "too big": "zu groß", "Big": "Groß", "Small": "Klein", "Size": "Größe", "not ready": "nicht bereit" }
`

func lintOf(t *testing.T, src, crdSrc string) []Problem {
	t.Helper()

	p, err := Load("gadgets.yaml", []byte(src))
	if err != nil {
		t.Fatal(err)
	}

	var crd *CRDManifest
	if crdSrc != "" {
		if crd, err = LoadCRD([]byte(crdSrc)); err != nil {
			t.Fatal(err)
		}
	}

	return Lint(p, crd)
}

func lintMessages(problems []Problem) string {
	lines := make([]string, len(problems))
	for i, p := range problems {
		lines[i] = p.Severity + " " + p.String()
	}

	return strings.Join(lines, "\n")
}

func TestLint(t *testing.T) {
	got := lintMessages(lintOf(t, gadgetPlugin, gadgetCRD))

	for _, want := range []string{
		`warning 13: resources.config: resource config is read but no card shows it`,
		`warning 7: resources.gadget: printer column "Ready" (.status.conditions[?(@.type=="Ready")].status) is not used`,
		`warning 7: resources.gadget: printer column "Age" (.metadata.creationTimestamp) is not used`,
		`warning 7: resources.gadget: condition Degraded (in the CRD's schema) has no health rule`,
		`warning 7: resources.gadget: the CRD has a scale subresource but no action scales gadget`,
		`warning 27: health.gadget[1]: no sample triggers this rule (health critical)`,
		`warning 29: health.gadget[1].message: "not ready" has no translation in fr`,
		`warning 33: cards.list: card list has no empty text`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}

	for _, unwanted := range []string{`"Size"`, `condition Ready`, `health.gadget[0]`, `"Big"`, "error"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("unexpected %q in\n%s", unwanted, got)
		}
	}
}

func TestLintNoI18nIsOneWarning(t *testing.T) {
	src := gadgetPlugin[:strings.Index(gadgetPlugin, "i18n:")]

	got := lintMessages(lintOf(t, src, ""))
	if strings.Count(got, "translation") != 0 || strings.Count(got, "no i18n: the 5 labels stay English") != 1 {
		t.Errorf("got\n%s", got)
	}

	if strings.Contains(got, "printer column") {
		t.Errorf("printer columns without a CRD:\n%s", got)
	}
}

func TestLintComparesTheCRD(t *testing.T) {
	cases := map[string]string{
		"kind: Gadget":                       "kind: Gizmo",
		"scope: Namespaced\n  config":        "scope: Cluster\n  config",
		"version: v1\n    resource: gadgets": "version: v3\n    resource: gadgets",
		"resource: gadgets":                  "resource: gizmos",
	}
	wants := map[string]string{
		"kind: Gizmo":    "resources.gadget.kind: kind Gizmo: the CRD says Gadget",
		"scope: Cluster": "resources.gadget.scope: scope Cluster: the CRD says Namespaced",
		"version: v3":    "resources.gadget.version: the CRD has no version v3",
		"gizmos":         "resources: no resource reads the CRD's Gadget (group demo.example.com, resource gadgets)",
	}

	for old, replacement := range cases {
		got := lintMessages(lintOf(t, strings.Replace(gadgetPlugin, old, replacement, 1), gadgetCRD))

		for key, want := range wants {
			if strings.Contains(replacement, key) && !strings.Contains(got, want) {
				t.Errorf("%s: missing %q in\n%s", replacement, want, got)
			}
		}
	}
}

func TestLintPreferredVersionReadsTheStorageVersion(t *testing.T) {
	src := strings.Replace(gadgetPlugin, "version: v1\n    resource: gadgets", "version: preferred\n    resource: gadgets", 1)
	if got := lintMessages(lintOf(t, src, gadgetCRD)); !strings.Contains(got, `printer column "Age"`) {
		t.Errorf("got\n%s", got)
	}
}

func TestLintScaleActionAndConditionRules(t *testing.T) {
	src := strings.Replace(gadgetPlugin, "cards:\n", `actions:
  scale:
    label: "Scale"
    on: gadget
    verb: subresource
    subresource: scale
    rbac: { verbs: [patch], group: demo.example.com, resource: gadgets/scale }
    audit: demo-gadgets.scale
cards:
`, 1)
	src = strings.Replace(src, `- when: "!conditions.Ready"`, `- when: '!conditions.Ready || condition("Degraded").status == "True"'`, 1)

	got := lintMessages(lintOf(t, src, gadgetCRD))
	if strings.Contains(got, "scale subresource") || strings.Contains(got, "condition Degraded") {
		t.Errorf("got\n%s", got)
	}
}

func TestLintReportsValidationErrors(t *testing.T) {
	got := lintOf(t, strings.Replace(gadgetPlugin, "fields.size > 100\n", "fields.sise > 100\n", 1), "")
	if !HasErrors(got) || !strings.Contains(lintMessages(got), "unknown fields.sise") {
		t.Errorf("got\n%s", lintMessages(got))
	}
}

func TestLintBuiltinsHaveNoUnusedResourceOrMissingEmpty(t *testing.T) {
	for _, c := range Builtins() {
		for _, p := range Lint(c.Plugin, nil) {
			if strings.Contains(p.Msg, "no card shows it") || strings.Contains(p.Msg, "no empty text") || strings.Contains(p.Msg, "translation") {
				t.Errorf("%s: %s", c.Plugin.ID, p)
			}
		}
	}
}

func TestUsesPath(t *testing.T) {
	cases := []struct {
		src, path string
		want      bool
	}{
		{"$.spec.size|default(0)", ".spec.size", true},
		{"$.spec.sizeLimit", ".spec.size", false},
		{"$.spec.sizeLimit+$.spec.size", ".spec.size", true},
		{"$.spec.size.value", ".spec.size", true},
		{`$.status.conditions[?(@.type=="Ready")].status`, `.status.conditions[?(@.type == "Ready")].status`, true},
	}

	for _, tc := range cases {
		if got := usesPath(tc.src, tc.path); got != tc.want {
			t.Errorf("usesPath(%q, %q) = %v", tc.src, tc.path, got)
		}
	}
}
