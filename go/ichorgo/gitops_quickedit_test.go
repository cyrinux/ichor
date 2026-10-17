package ichorgo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readQuickEditFixture(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", "quickedit", name))
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// oneLineDiff checks that after differs from before by exactly one line: line (1-based)
// replaced by want, or want inserted at line when the file grew by one line.
func oneLineDiff(t *testing.T, before, after string, line int, want string) {
	t.Helper()

	b, a := strings.Split(before, "\n"), strings.Split(after, "\n")

	switch len(a) - len(b) {
	case 0:
		var diff []int

		for i := range b {
			if b[i] != a[i] {
				diff = append(diff, i+1)
			}
		}

		if len(diff) != 1 || diff[0] != line || a[line-1] != want {
			t.Fatalf("changed lines %v, want only line %d = %q; got:\n%s", diff, line, want, after)
		}
	case 1:
		got := append(append([]string{}, a[:line-1]...), a[line:]...)
		if a[line-1] != want || strings.Join(got, "\n") != before {
			t.Fatalf("want %q inserted at line %d; got:\n%s", want, line, after)
		}
	default:
		t.Fatalf("the file went from %d to %d lines:\n%s", len(b), len(a), after)
	}
}

func TestGitOpsQuickEdit(t *testing.T) {
	cases := []struct {
		name, fixture, edit string
		line                int
		want                string // the new line
		err                 string
	}{
		{"double-quoted image keeps its quotes and comment", "deployment.yaml",
			`{"op":"image","documentIndex":0,"container":"app","image":"ghcr.io/example-org/app:1.4.0"}`,
			24, `          image: "ghcr.io/example-org/app:1.4.0" # bumped by hand`, ""},
		{"a bare tag keeps the repository", "deployment.yaml",
			`{"op":"image","container":"sidecar","image":"1.9.9"}`,
			34, `          image: ghcr.io/example-org/proxy:1.9.9`, ""},
		{"single-quoted init container", "deployment.yaml",
			`{"op":"image","container":"migrate","image":"ghcr.io/example-org/app-migrate:1.3.0"}`,
			20, `          image: 'ghcr.io/example-org/app-migrate:1.3.0'`, ""},
		{"replicas", "deployment.yaml", `{"op":"replicas","replicas":3}`, 9, `  replicas: 3`, ""},
		{"missing container", "deployment.yaml", `{"op":"image","container":"web","image":"1.0.0"}`,
			0, "", "container web is not in Deployment app"},
		{"several containers need a name", "deployment.yaml", `{"op":"image","image":"1.0.0"}`,
			0, "", "say which container of Deployment app: app, sidecar, migrate"},
		{"a block scalar is left to the editor", "deployment.yaml",
			`{"op":"path","path":"spec.template.spec.containers.0.env.0.value","value":"hi"}`, 0, "", "block scalar"},
		{"multi-document image", "multi.yaml", `{"op":"image","documentIndex":1,"image":"ghcr.io/example-org/worker:3.2.0"}`,
			23, `          image: ghcr.io/example-org/worker:3.2.0`, ""},
		{"replicas added under spec", "multi.yaml", `{"op":"replicas","documentIndex":1,"replicas":4}`,
			15, `  replicas: 4`, ""},
		{"a Service has no replicas", "multi.yaml", `{"op":"replicas","documentIndex":2,"replicas":4}`,
			0, "", "Service worker has no replicas"},
		{"a ConfigMap has no containers", "multi.yaml", `{"op":"image","documentIndex":0,"image":"x:1"}`,
			0, "", "ConfigMap app-config has no containers"},
		{"document out of range", "multi.yaml", `{"op":"replicas","documentIndex":3,"replicas":1}`,
			0, "", "document 3 is not in the file (it has 3)"},
		{"kind and name match the index", "multi.yaml",
			`{"op":"image","documentIndex":1,"kind":"StatefulSet","name":"worker","image":"ghcr.io/example-org/worker:3.2.0"}`,
			23, `          image: ghcr.io/example-org/worker:3.2.0`, ""},
		{"the index moved: found by kind and name", "multi.yaml",
			`{"op":"image","documentIndex":0,"kind":"StatefulSet","name":"worker","image":"ghcr.io/example-org/worker:3.2.0"}`,
			23, `          image: ghcr.io/example-org/worker:3.2.0`, ""},
		{"an index past the end: found by kind and name", "multi.yaml",
			`{"op":"replicas","documentIndex":7,"kind":"StatefulSet","name":"worker","replicas":4}`,
			15, `  replicas: 4`, ""},
		{"the object is gone", "multi.yaml",
			`{"op":"replicas","documentIndex":1,"kind":"Deployment","name":"app","replicas":4}`,
			0, "", "Deployment app is not in this file any more: reload"},
		{"the name changed", "multi.yaml",
			`{"op":"replicas","documentIndex":1,"kind":"StatefulSet","name":"queue","replicas":4}`,
			0, "", "StatefulSet queue is not in this file any more: reload"},
		{"CronJob container under jobTemplate", "cronjob.yaml", `{"op":"image","container":"report","image":"ghcr.io/example-org/report:0.8.2"}`,
			14, `              image: ghcr.io/example-org/report:0.8.2`, ""},
		{"kustomize newTag keeps its quotes", "kustomization.yaml",
			`{"op":"image","kustomizeImage":"ghcr.io/example-org/app","image":"1.4.0"}`,
			9, `    newTag: "1.4.0"`, ""},
		{"kustomize newTag added after the name", "kustomization.yaml",
			`{"op":"image","kustomizeImage":"ghcr.io/example-org/proxy","image":"ghcr.io/example-org/proxy-fips:2.1.0"}`,
			11, `    newTag: 2.1.0`, ""},
		{"kustomize digest is left to the editor", "kustomization.yaml",
			`{"op":"image","kustomizeImage":"ghcr.io/example-org/pinned","image":"1.0.0"}`, 0, "", "pinned by digest"},
		{"kustomize image not listed", "kustomization.yaml",
			`{"op":"image","kustomizeImage":"ghcr.io/example-org/other","image":"1.0.0"}`, 0, "", "is not in the kustomization's images"},
		{"kustomize replicas count", "kustomization.yaml", `{"op":"replicas","kustomizeReplica":"app","replicas":5}`,
			16, `    count: 5 # peak hours`, ""},
		{"HelmRelease values path", "helmrelease.yaml", `{"op":"path","path":"spec.values.image.tag","value":"1.4.0"}`,
			15, `      tag: 1.4.0 # the chart's default is older`, ""},
		{"a tag that reads as a number is quoted", "helmrelease.yaml", `{"op":"path","path":"spec.values.image.tag","value":"1.10"}`,
			15, `      tag: "1.10" # the chart's default is older`, ""},
		{"a number value", "helmrelease.yaml", `{"op":"path","path":"spec.values.replicaCount","value":3}`,
			16, `    replicaCount: 3`, ""},
		{"a missing path", "helmrelease.yaml", `{"op":"path","path":"spec.values.image.digest","value":"x"}`,
			0, "", "spec.values.image.digest is not in HelmRelease app: digest is missing"},
		{"negative replicas", "deployment.yaml", `{"op":"replicas","replicas":-1}`, 0, "", "replicas must be between 0"},
		{"unknown op", "deployment.yaml", `{"op":"env"}`, 0, "", `unknown quick edit "env"`},
		{"unknown field", "deployment.yaml", `{"op":"replicas","replica":3}`, 0, "", "unknown field"},
		{"an image with a space", "deployment.yaml", `{"op":"image","container":"app","image":"a b"}`, 0, "", "is not an image reference"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := readQuickEditFixture(t, c.fixture)

			out, err := GitOpsQuickEdit(before, c.edit)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			var res quickEditResult
			if err := json.Unmarshal([]byte(out), &res); err != nil {
				t.Fatal(err)
			}

			if !res.Changed || res.Line != c.line {
				t.Errorf("changed=%v line=%d, want true %d", res.Changed, res.Line, c.line)
			}

			oneLineDiff(t, before, res.Content, c.line, c.want)
		})
	}
}

func TestGitOpsQuickEditResultJSON(t *testing.T) {
	out, err := GitOpsQuickEdit("kind: Deployment\nspec:\n  replicas: 1\n", `{"op":"replicas","documentIndex":0,"replicas":3}`)
	if err != nil || out != `{"content":"kind: Deployment\nspec:\n  replicas: 3\n","changed":true,"line":3}` {
		t.Fatalf("%s %v", out, err)
	}
}

func TestGitOpsQuickEditUnchanged(t *testing.T) {
	before := readQuickEditFixture(t, "deployment.yaml")

	out, err := GitOpsQuickEdit(before, `{"op":"replicas","replicas":2}`)
	if err != nil {
		t.Fatal(err)
	}

	var res quickEditResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if res.Changed || res.Line != 9 || res.Content != before {
		t.Fatalf("changed=%v line=%d", res.Changed, res.Line)
	}
}

func TestGitOpsQuickEditKustomizeNewNameAndTag(t *testing.T) {
	before := readQuickEditFixture(t, "kustomization.yaml")

	res, err := gitOpsQuickEdit(before, quickEdit{Op: "image", KustomizeImage: "ghcr.io/example-org/app", Image: "ghcr.io/example-org/app-next:2.0.0"})
	if err != nil {
		t.Fatal(err)
	}

	want := strings.Replace(before, "    newTag: \"1.4.1\"\n", "    newName: ghcr.io/example-org/app-next\n    newTag: \"2.0.0\"\n", 1)
	if res.Content != want || res.Line != 9 {
		t.Fatalf("line %d:\n%s", res.Line, res.Content)
	}
}

func TestGitOpsQuickEditLineEndings(t *testing.T) {
	crlf := "kind: Deployment\r\nspec:\r\n  selector: {}\r\n  template:\r\n    spec:\r\n      containers:\r\n        - name: a\r\n          image: ghcr.io/example-org/app:1\r\n"

	res, err := gitOpsQuickEdit(crlf, quickEdit{Op: "replicas", Replicas: new(int)})
	if err != nil {
		t.Fatal(err)
	}

	if want := strings.Replace(crlf, "spec:\r\n", "spec:\r\n  replicas: 0\r\n", 1); res.Content != want {
		t.Fatalf("%q", res.Content)
	}

	res, err = gitOpsQuickEdit(crlf, quickEdit{Op: "image", Image: "2"})
	if err != nil || res.Content != strings.Replace(crlf, "app:1\r\n", "app:2\r\n", 1) {
		t.Fatalf("%q %v", res.Content, err)
	}

	noEOL := "images:\n  - name: ghcr.io/example-org/app"

	res, err = gitOpsQuickEdit(noEOL, quickEdit{Op: "image", KustomizeImage: "ghcr.io/example-org/app", Image: "1.0.0"})
	if err != nil || res.Content != noEOL+"\n    newTag: 1.0.0" || res.Line != 3 {
		t.Fatalf("%q %d %v", res.Content, res.Line, err)
	}
}

func TestGitOpsQuickEditNonASCIIColumns(t *testing.T) {
	in := "kind: Deployment\nmetadata: {name: café, annotations: {é: \"ü\"}}\nspec: {replicas: 1, template: {spec: {containers: [{name: a, image: \"x:1\"}]}}}\n"

	res, err := gitOpsQuickEdit(in, quickEdit{Op: "image", Image: "x:2"})
	if err != nil || res.Content != strings.Replace(in, `"x:1"`, `"x:2"`, 1) {
		t.Fatalf("%q %v", res.Content, err)
	}

	res, err = gitOpsQuickEdit(in, quickEdit{Op: "replicas", Replicas: new(5)})
	if err != nil || res.Content != strings.Replace(in, "replicas: 1", "replicas: 5", 1) {
		t.Fatalf("%q %v", res.Content, err)
	}
}

func TestGitOpsQuickEditNotYAML(t *testing.T) {
	if _, err := GitOpsQuickEdit("a: [b\n", `{"op":"replicas","replicas":1}`); err == nil || !strings.Contains(err.Error(), "not plain YAML") {
		t.Fatalf("err = %v", err)
	}
}

func TestGitOpsQuickEditTargets(t *testing.T) {
	out, err := GitOpsQuickEditTargets(readQuickEditFixture(t, "cronjob.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	if want := `{"documents":[{"index":0,"kind":"CronJob","name":"report","scalable":false,"containers":[{"name":"report","image":"ghcr.io/example-org/report:0.9.0","line":14}]}],"kustomizeImages":[],"kustomizeReplicas":[]}`; out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}

	out, err = GitOpsQuickEditTargets(readQuickEditFixture(t, "kustomization.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	want := `{"documents":[],"kustomizeImages":[` +
		`{"documentIndex":0,"name":"ghcr.io/example-org/app","newTag":"1.4.1","line":8},` +
		`{"documentIndex":0,"name":"ghcr.io/example-org/proxy","newName":"ghcr.io/example-org/proxy-fips","line":10},` +
		`{"documentIndex":0,"name":"ghcr.io/example-org/pinned","digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","line":12}],` +
		`"kustomizeReplicas":[{"documentIndex":0,"name":"app","count":2,"line":15}]}`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

func TestGitOpsQuickEditTargetsWorkloads(t *testing.T) {
	var got quickEditTargets

	out, err := GitOpsQuickEditTargets(readQuickEditFixture(t, "deployment.yaml"))
	if err != nil || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("%s %v", out, err)
	}

	d := got.Documents[0]
	if len(got.Documents) != 1 || d.Kind != "Deployment" || d.Name != "app" || d.Replicas == nil || *d.Replicas != 2 || !d.Scalable ||
		len(d.Containers) != 3 || d.Containers[2] != (quickEditContainer{Name: "migrate", Image: "ghcr.io/example-org/app-migrate:1.4.0", Init: true, Line: 20}) {
		t.Fatalf("%s", out)
	}

	got = quickEditTargets{}

	out, err = GitOpsQuickEditTargets(readQuickEditFixture(t, "multi.yaml"))
	if err != nil || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("%s %v", out, err)
	}

	if len(got.Documents) != 1 || got.Documents[0].Index != 1 || got.Documents[0].Replicas != nil || !got.Documents[0].Scalable {
		t.Fatalf("%s", out)
	}
}

// A kustomize images entry in the same file wins over the manifest's image.
func TestGitOpsQuickEditTargetsOverride(t *testing.T) {
	in := "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nimages:\n  - name: ghcr.io/example-org/app\n    newTag: \"2\"\n---\n" +
		"kind: Deployment\nmetadata: {name: app}\nspec:\n  template:\n    spec:\n      containers:\n        - {name: app, image: \"ghcr.io/example-org/app:1\"}\n"

	var got quickEditTargets

	out, err := GitOpsQuickEditTargets(in)
	if err != nil || json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("%s %v", out, err)
	}

	if len(got.Documents) != 1 || got.Documents[0].Containers[0].OverriddenBy != "ghcr.io/example-org/app" || got.Documents[0].Containers[0].Line != 13 {
		t.Fatalf("%s", out)
	}
}

func TestGitOpsQuickEditFlowStyleImage(t *testing.T) {
	in := "kind: Pod\nmetadata: {name: p}\nspec:\n  containers: [{name: a, image: ghcr.io/example-org/app:1, args: [\"x\", y]}] # one\n"

	res, err := gitOpsQuickEdit(in, quickEdit{Op: "image", Image: "ghcr.io/example-org/app:1.1"})
	if err != nil || res.Content != strings.Replace(in, "app:1,", "app:1.1,", 1) {
		t.Fatalf("%q %v", res.Content, err)
	}
}

func TestGitOpsQuickEditScalarForms(t *testing.T) {
	cases := []struct{ in, edit, want, err string }{
		{"a: 'it''s'\nb: 1\n", `{"op":"path","path":"a","value":"it's new"}`, "a: 'it''s new'\nb: 1\n", ""},
		{"a: \"x\\\"y\" # c\n", `{"op":"path","path":"a","value":"z\"w"}`, "a: \"z\\\"w\" # c\n", ""},
		{"a: !!str 1.0\nb: 1\n", `{"op":"path","path":"a","value":"2.0"}`, "a: !!str \"2.0\"\nb: 1\n", ""},
		{"a: &tag !!str 1.0\nb: *tag\n", `{"op":"path","path":"a","value":"2.0"}`, "", "has an anchor (&tag)"},
		{"a: &v 1\n", `{"op":"path","path":"a","value":2}`, "", "has an anchor (&v)"},
		// YAML 1.1 (Helm) reads these as booleans or null: quoted as strings.
		{"a: x\n", `{"op":"path","path":"a","value":"yes"}`, "a: \"yes\"\n", ""},
		{"a: x\n", `{"op":"path","path":"a","value":"on"}`, "a: \"on\"\n", ""},
		{"a: x\n", `{"op":"path","path":"a","value":"Y"}`, "a: \"Y\"\n", ""},
		{"a: x\n", `{"op":"path","path":"a","value":"OFF"}`, "a: \"OFF\"\n", ""},
		{"a: x\n", `{"op":"path","path":"a","value":"~"}`, "a: \"~\"\n", ""},
		{"a: x\n", `{"op":"path","path":"a","value":"True"}`, "a: \"True\"\n", ""},
		{"a: x\n", `{"op":"path","path":"a","value":"yesterday"}`, "a: yesterday\n", ""},
		{"a: x\n", `{"op":"path","path":"a","value":true}`, "a: true\n", ""},
		{"a: x\n", `{"op":"path","path":"a","value":"y"} `, "a: \"y\"\n", ""},
		{"a: x\n", `{"op":"path","path":"a","value":"y"} {"op":"path"}`, "", "data after the JSON object"},
		{"a: x\n", `{"op":"path","path":"a","value":"y"}}`, "", "data after the JSON object"},
		{"a: x\n", `{"op":"path","path":"a","value":"y"}x`, "", "data after the JSON object"},
		{"a: true\n", `{"op":"path","path":"a","value":false}`, "a: false\n", ""},
		{"a: 1\n", `{"op":"path","path":"a","value":1.5}`, "a: 1.5\n", ""},
		{"a: x\n", `{"op":"path","path":"a","value":null}`, "", "say the new value"},
		{"a: x\n", `{"op":"path","path":"a","value":{"b":1}}`, "", "a string, a number or true/false"},
		{"a: x\n", `{"op":"path","path":"a","value":"two\nlines"}`, "", "one line"},
		{"a: x\n", `{"op":"path","path":" ","value":"y"}`, "", "say the path"},
		{"a: {b: x}\n", `{"op":"path","path":"a","value":"y"}`, "", "not a plain value"},
		{"a: plain\n  continued\n", `{"op":"path","path":"a","value":"y"}`, "", "spans several lines"},
		{"apiVersion: kustomize.toolkit.fluxcd.io/v1\nkind: Kustomization\nspec:\n  images:\n    - name: ghcr.io/example-org/app\n      newTag: v1\n",
			`{"op":"image","kustomizeImage":"ghcr.io/example-org/app","image":"v2"}`,
			"apiVersion: kustomize.toolkit.fluxcd.io/v1\nkind: Kustomization\nspec:\n  images:\n    - name: ghcr.io/example-org/app\n      newTag: v2\n", ""},
		{"images:\n  - name: ghcr.io/example-org/app\n", `{"op":"image","kustomizeImage":"ghcr.io/example-org/app","image":"ghcr.io/example-org/app@sha256:00"}`, "", "digest"},
		{"images:\n  - name: ghcr.io/example-org/app\n", `{"op":"image","kustomizeImage":"ghcr.io/example-org/app","image":"ghcr.io/example-org/app:"}`, "", "say the tag"},
		{"images:\n  - {name: ghcr.io/example-org/app}\n", `{"op":"image","kustomizeImage":"ghcr.io/example-org/app","image":"1"}`, "", "cannot be added"},
		{"kind: Deployment\nspec: {}\n", `{"op":"replicas","replicas":1}`, "", "cannot be added"},
		{"- a\n", `{"op":"replicas","replicas":1}`, "", "is not a manifest"},
		{"kind: Pod\nspec:\n  containers:\n    - name: a\n", `{"op":"image","image":"x:1"}`, "", "container a in Pod has no image"},
		{"a: 1\n", `{"op":"replicas","documentIndex":-1,"replicas":1}`, "", "documentIndex is negative"},
		{"kind: Deployment\nmetadata: {name: a}\nspec: {replicas: 1}\n---\nkind: Deployment\nmetadata: {name: a}\nspec: {replicas: 2}\n---\na: 1\n",
			`{"op":"replicas","documentIndex":2,"kind":"Deployment","name":"a","replicas":3}`, "", "Deployment a is in this file more than once"},
		// Kustomize entries carry no kind or name.
		{"images:\n  - name: ghcr.io/example-org/app\n    newTag: v1\n", `{"op":"image","documentIndex":0,"kind":"","name":"","kustomizeImage":"ghcr.io/example-org/app","image":"v2"}`,
			"images:\n  - name: ghcr.io/example-org/app\n    newTag: v2\n", ""},
	}

	for _, c := range cases {
		out, err := GitOpsQuickEdit(c.in, c.edit)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("%s: err = %v, want %q", c.edit, err, c.err)
			}

			continue
		}

		var res quickEditResult
		if err != nil || json.Unmarshal([]byte(out), &res) != nil || res.Content != c.want {
			t.Errorf("%s: %q %v, want %q", c.edit, res.Content, err, c.want)
		}
	}
}

func TestImageRepo(t *testing.T) {
	for in, want := range map[string]string{
		"ghcr.io/example-org/app:1.2":               "ghcr.io/example-org/app",
		"registry.example.org:5000/app":             "registry.example.org:5000/app",
		"registry.example.org:5000/app:1@sha256:00": "registry.example.org:5000/app",
		"app": "app",
	} {
		if got := imageRepo(in); got != want {
			t.Errorf("imageRepo(%q) = %q, want %q", in, got, want)
		}
	}
}
