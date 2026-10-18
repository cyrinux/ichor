package ichorgo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func decodeObject(t *testing.T, s string) map[string]any {
	t.Helper()

	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}

	return m
}

func TestDiffLiveChanges(t *testing.T) {
	wanted := decodeObject(t, `{"apiVersion":"apps/v1","kind":"Deployment",
		"metadata":{"name":"app","namespace":"web","labels":{"tier":"web"},"resourceVersion":"1"},
		"spec":{"replicas":2,"template":{"spec":{"containers":[
			{"name":"app","image":"ghcr.io/org/app:1.2","env":[{"name":"LOG","value":"info"}],"ports":[{"containerPort":8080}]},
			{"name":"sidecar","image":"proxy:1"}]}}},
		"status":{"replicas":2}}`)
	live := decodeObject(t, `{"apiVersion":"apps/v1","kind":"Deployment",
		"metadata":{"name":"app","namespace":"web","labels":{"tier":"web","extra":"x"},"resourceVersion":"99"},
		"spec":{"replicas":5,"template":{"spec":{"containers":[
			{"name":"sidecar","image":"proxy:1"},
			{"name":"app","image":"ghcr.io/org/app:1.3","env":[{"name":"LOG","value":"debug"}],"ports":[{"containerPort":8080}]}]}}},
		"status":{"replicas":5}}`)

	got := diffLiveChanges(live, wanted, newKubeDiffMasker())

	labels := make([]string, len(got))
	for i, c := range got {
		labels[i] = c.Label + "=" + c.Wanted + ">" + c.Live + ":" + c.Type
	}

	want := []string{
		"spec.replicas=2>5:int",
		"spec.template.spec.containers[app].env[LOG].value=info>debug:str",
		"spec.template.spec.containers[app].image=ghcr.io/org/app:1.2>ghcr.io/org/app:1.3:str",
	}
	if !slices.Equal(labels, want) {
		t.Fatalf("changes %v, want %v", labels, want)
	}

	if !slices.Equal(got[2].Path, []string{"spec", "template", "spec", "containers", "name=app", "image"}) {
		t.Errorf("path %v", got[2].Path)
	}
}

// Nothing of a Secret, nor a value holding a substituted Secret value, is ever offered.
func TestDiffLiveChangesNeverSecrets(t *testing.T) {
	secret := `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"s"},"data":{"k":"%s"}}`
	if c := diffLiveChanges(decodeObject(t, strings.Replace(secret, "%s", "bGl2ZQ==", 1)), decodeObject(t, strings.Replace(secret, "%s", "Z2l0", 1)), nil); c != nil {
		t.Errorf("secret changes %v", c)
	}

	masker := newKubeDiffMasker()
	masker.add("s3cr3t-password")

	cm := `{"kind":"ConfigMap","metadata":{"name":"c"},"data":{"url":"%s","mode":"%s"}}`
	live := decodeObject(t, strings.NewReplacer("%s", "x").Replace(cm))
	live["data"] = map[string]any{"url": "postgres://u:s3cr3t-password@db", "mode": "b"}
	wanted := decodeObject(t, cm)
	wanted["data"] = map[string]any{"url": "postgres://u:other@db", "mode": "a"}

	got := diffLiveChanges(live, wanted, masker)
	if len(got) != 1 || got[0].Label != "data.mode" {
		t.Errorf("changes %+v", got)
	}
}

const keepManifest = `# the app
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
spec:
  replicas: 2 # scaled by HPA? no
  template:
    spec:
      containers:
        - name: sidecar
          image: proxy:1
        - name: app
          image: "ghcr.io/org/app:1.2"
          env:
            - {name: LOG, value: info}
          ports:
            - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: app
`

func keepEdit(changes ...diffLiveChange) string {
	b, _ := json.Marshal(map[string]any{"op": "keep", "documentIndex": 0, "kind": "Deployment", "name": "app", "changes": changes}) //nolint:errcheck

	return string(b)
}

func TestGitOpsQuickEditKeep(t *testing.T) {
	edit := keepEdit(
		diffLiveChange{Path: []string{"spec", "replicas"}, Label: "spec.replicas", Wanted: "2", Live: "5", Type: "int"},
		diffLiveChange{Path: []string{"spec", "template", "spec", "containers", "name=app", "image"}, Label: "image", Wanted: "ghcr.io/org/app:1.2", Live: "ghcr.io/org/app:1.3", Type: "str"},
		diffLiveChange{Path: []string{"spec", "template", "spec", "containers", "name=app", "env", "name=LOG", "value"}, Label: "env", Wanted: "info", Live: "debug", Type: "str"},
		// Set by an overlay: the file's value is not the wanted one.
		diffLiveChange{Path: []string{"spec", "template", "spec", "containers", "name=sidecar", "image"}, Label: "sidecar image", Wanted: "proxy:2", Live: "proxy:3", Type: "str"},
		// Defaulted by the API server: not in the file.
		diffLiveChange{Path: []string{"spec", "revisionHistoryLimit"}, Label: "spec.revisionHistoryLimit", Wanted: "10", Live: "3", Type: "int"},
	)

	out, err := GitOpsQuickEdit(keepManifest, edit)
	if err != nil {
		t.Fatal(err)
	}

	var res quickEditResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	want := strings.NewReplacer(
		"replicas: 2 # scaled", "replicas: 5 # scaled",
		`image: "ghcr.io/org/app:1.2"`, `image: "ghcr.io/org/app:1.3"`,
		"{name: LOG, value: info}", "{name: LOG, value: debug}",
	).Replace(keepManifest)

	if !res.Changed || res.Content != want || res.Line != 7 {
		t.Fatalf("changed %v line %d:\n%s", res.Changed, res.Line, res.Content)
	}

	if len(res.Skipped) != 2 || !strings.Contains(res.Skipped[0], "sidecar image: the file has proxy:1, set to proxy:2 elsewhere") ||
		!strings.Contains(res.Skipped[1], "not set in this file") {
		t.Errorf("skipped %q", res.Skipped)
	}
}

func TestGitOpsQuickEditKeepNothingInFile(t *testing.T) {
	_, err := GitOpsQuickEdit(keepManifest, keepEdit(
		diffLiveChange{Path: []string{"spec", "paused"}, Label: "spec.paused", Wanted: "false", Live: "true", Type: "bool"},
	))
	if err == nil || !strings.Contains(err.Error(), "none of the live values are set in this file") {
		t.Fatalf("err %v", err)
	}

	_, err = GitOpsQuickEdit(keepManifest, keepEdit(diffLiveChange{Path: []string{"spec", "replicas"}, Wanted: "2", Live: "5", Type: "map"}))
	if err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Fatalf("err %v", err)
	}

	_, err = GitOpsQuickEdit(keepManifest, `{"op":"keep","documentIndex":0}`)
	if err == nil || !strings.Contains(err.Error(), "say the live values") {
		t.Fatalf("err %v", err)
	}
}

// A wanted number matches the file's however it is written; a string stays a string.
func TestGitOpsQuickEditKeepTypes(t *testing.T) {
	file := "kind: ConfigMap\nmetadata:\n  name: c\ndata:\n  port: \"8080\"\n  on: 'yes'\nspec:\n  ratio: 1.0\n"
	b, _ := json.Marshal(map[string]any{"op": "keep", "documentIndex": 0, "changes": []diffLiveChange{ //nolint:errcheck
		{Path: []string{"data", "port"}, Label: "port", Wanted: "8080", Live: "9090", Type: "str"},
		{Path: []string{"spec", "ratio"}, Label: "ratio", Wanted: "1", Live: "0.5", Type: "float"},
	}})

	out, err := GitOpsQuickEdit(file, string(b))
	if err != nil {
		t.Fatal(err)
	}

	var res quickEditResult
	_ = json.Unmarshal([]byte(out), &res) //nolint:errcheck

	if want := strings.NewReplacer(`port: "8080"`, `port: "9090"`, "ratio: 1.0", "ratio: 0.5").Replace(file); res.Content != want {
		t.Fatalf("got:\n%s", res.Content)
	}
}
