package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

const checkDeployment = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 1
  template:
    spec:
      containers:
        - name: web
          image: ghcr.io/example-org/web:1.0.0
`

func checkFiles() map[string][]byte {
	return repoFiles(map[string]string{
		"base/kustomization.yaml":          "resources:\n  - deployment.yaml\n  - secret.yaml\n",
		"base/deployment.yaml":             checkDeployment,
		"base/secret.yaml":                 "apiVersion: v1\nkind: Secret\nmetadata:\n  name: web\nstringData:\n  password: hunter2-long-value\n",
		"overlays/prod/kustomization.yaml": "namespace: prod\nresources:\n  - ../../base\npatches:\n  - path: replicas.yaml\n",
		"overlays/prod/replicas.yaml":      "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  replicas: 3\n",
		"plain/config.yaml":                "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\ndata:\n  level: info\n",
		"charts/web/Chart.yaml":            "apiVersion: v2\nname: web\nversion: 0.1.0\n",
		"charts/web/values.yaml":           "replicas: 1\n",
		"remote/kustomization.yaml":        "resources:\n  - https://github.com/example-org/shared//base?ref=main\n",
		"README.md":                        "# repo\n",
	})
}

func changes(c gitOpsEditCheck) []string {
	var out []string
	for _, r := range c.Diff.Resources {
		out = append(out, r.Change+" "+r.Kind+" "+r.Namespace+"/"+r.Name)
	}

	return out
}

func TestEditCheckKustomize(t *testing.T) {
	files := checkFiles()
	edited := strings.Replace(checkDeployment, "web:1.0.0", "web:1.1.0", 1)

	c := checkEdit(files, "base/deployment.yaml", edited, nil)
	if c.Kind != editCheckKustomize || c.Dir != "base" || !c.OK || c.Unchanged != 1 {
		t.Fatalf("check %+v", c)
	}

	if got := changes(c); len(got) != 1 || got[0] != "changed Deployment /web" || !strings.Contains(c.Diff.Resources[0].Diff, "+        - image: ghcr.io/example-org/web:1.1.0") {
		t.Fatalf("changes %v %s", got, c.Diff.Resources[0].Diff)
	}

	// The overlay's patch: its namespace, and the Deployment it patches.
	c = checkEdit(files, "overlays/prod/replicas.yaml", strings.Replace(string(files["overlays/prod/replicas.yaml"]), "3", "5", 1), nil)
	if got := changes(c); c.Dir != "overlays/prod" || len(got) != 1 || got[0] != "changed Deployment prod/web" {
		t.Fatalf("overlay %+v", c)
	}

	// A patch whose target is gone breaks the build.
	c = checkEdit(files, "overlays/prod/replicas.yaml", strings.Replace(string(files["overlays/prod/replicas.yaml"]), "name: web", "name: gone", 1), nil)
	if c.OK || len(c.Errors) != 1 || c.Errors[0].Message == "" || strings.Contains(c.Errors[0].Message, fluxSourceRoot) {
		t.Fatalf("broken patch %+v", c)
	}

	// Removing a file the kustomization lists breaks it too.
	if c = checkEdit(files, "base/secret.yaml", "", nil); c.OK || len(c.Errors) != 1 {
		t.Fatalf("removed %+v", c)
	}
}

func TestEditCheckSecretsMasked(t *testing.T) {
	files := checkFiles()
	edited := strings.Replace(string(files["base/secret.yaml"]), "hunter2-long-value", "swordfish-long-value", 1)

	c := checkEdit(files, "base/secret.yaml", edited, nil)
	if got := changes(c); len(got) != 1 || got[0] != "changed Secret /web" {
		t.Fatalf("changes %v", got)
	}

	if d := c.Diff.Resources[0].Diff; strings.Contains(d, "hunter2") || strings.Contains(d, "swordfish") || !strings.Contains(d, diffSecretMask) {
		t.Errorf("secret shown: %s", d)
	}
}

func TestEditCheckKinds(t *testing.T) {
	files := checkFiles()

	c := checkEdit(files, "base/deployment.yaml", "kind: Deployment\nmetadata:\n  name: [web\n", nil)
	if c.OK || c.Kind != editCheckNone || len(c.Errors) != 1 || c.Errors[0].Path != "base/deployment.yaml" || c.Errors[0].Line == 0 {
		t.Fatalf("bad YAML %+v", c)
	}

	c = checkEdit(files, "plain/config.yaml", strings.Replace(string(files["plain/config.yaml"]), "info", "debug", 1)+"---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: more\n", nil)
	if got := changes(c); c.Kind != editCheckPlain || !c.OK || len(got) != 2 || got[0] != "created ConfigMap /more" || got[1] != "changed ConfigMap /settings" {
		t.Fatalf("plain %+v %v", c, got)
	}

	if c = checkEdit(files, "charts/web/values.yaml", "replicas: 2\n", nil); c.Kind != editCheckHelm || !c.OK || !strings.Contains(c.Note, "Helm") {
		t.Fatalf("chart %+v", c)
	}

	if c = checkEdit(files, "README.md", "# repo, edited\n", nil); c.Kind != editCheckNone || !c.OK {
		t.Fatalf("readme %+v", c)
	}

	c = checkEdit(files, "remote/kustomization.yaml", "resources:\n  - https://github.com/example-org/shared//base?ref=v2\n", nil)
	if c.OK || len(c.Errors) != 1 || !strings.Contains(c.Errors[0].Message, "not fetched on the phone") {
		t.Fatalf("remote %+v", c)
	}
}

func TestEditCheckFluxSpec(t *testing.T) {
	files := checkFiles()
	spec := &fluxKustomizeSpec{Path: "./base", TargetNamespace: "staging"}
	edited := strings.Replace(checkDeployment, "replicas: 1", "replicas: 2", 1)

	c := checkEdit(files, "base/deployment.yaml", edited, spec)
	if got := changes(c); c.Dir != "base" || len(got) != 1 || got[0] != "changed Deployment staging/web" {
		t.Fatalf("flux %+v", c)
	}

	if strings.Contains(c.Diff.Resources[0].Diff, fluxOwnerName) {
		t.Error("owner labels in the diff")
	}
}

func TestEditCheckDemo(t *testing.T) {
	content := strings.Replace(demoGitForgeFiles["kubernetes/apps/web/deployment.yaml"], "web:1.4.0", "web:1.5.0", 1)

	out, err := GitOpsEditCheck(`{"kind":"demo"}`, demoClusterRepoURL, demoClusterRepoHead, "kubernetes/apps/web/deployment.yaml", content, "")
	if err != nil {
		t.Fatal(err)
	}

	var c gitOpsEditCheck
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		t.Fatal(err)
	}

	if got := changes(c); !c.OK || c.Kind != editCheckKustomize || len(got) != 1 || got[0] != "changed Deployment web/web" {
		t.Fatalf("demo %+v", c)
	}
}
