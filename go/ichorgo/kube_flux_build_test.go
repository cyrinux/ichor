package ichorgo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"strings"
	"testing"

	"sigs.k8s.io/kustomize/kyaml/filesys"
)

// fluxTarball is a gzipped tarball of files (name -> content).
func fluxTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}

		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func fluxSourceFS(t *testing.T, files map[string]string) filesys.FileSystem {
	t.Helper()

	fsys := filesys.MakeFsInMemory()
	if err := untarFluxArtifact(fluxTarball(t, files), fsys, fluxSourceRoot); err != nil {
		t.Fatal(err)
	}

	return fsys
}

func builtByName(t *testing.T, built []fluxBuilt, kind, name string) fluxBuilt {
	t.Helper()

	for _, b := range built {
		meta, _ := b.obj["metadata"].(map[string]any)
		if b.obj["kind"] == kind && meta["name"] == name {
			return b
		}
	}

	t.Fatalf("no %s %s in %d objects", kind, name, len(built))

	return fluxBuilt{}
}

func spec(t *testing.T, js string) fluxKustomizeSpec {
	t.Helper()

	var s fluxKustomizeSpec
	if err := json.Unmarshal([]byte(js), &s); err != nil {
		t.Fatal(err)
	}

	return s
}

const (
	deployYAML = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  labels: {app: web}
spec:
  selector: {matchLabels: {app: web}}
  template:
    metadata: {labels: {app: web}}
    spec:
      containers:
      - name: web
        image: ghcr.io/example/web:1.0.0
        env:
        - {name: DOMAIN, value: "${DOMAIN}"}
        - {name: LEVEL, value: "${LEVEL:=info}"}
`
	cmYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: raw
  annotations: {kustomize.toolkit.fluxcd.io/substitute: disabled}
data: {template: "${KEEP_ME}"}
`
)

func TestBuildFluxKustomizationGeneratesKustomization(t *testing.T) {
	fsys := fluxSourceFS(t, map[string]string{
		"apps/web/deploy.yaml":            deployYAML,
		"apps/web/cm.yaml":                cmYAML,
		"apps/web/README.md":              "# readme",
		"apps/web/.hidden/x.yaml":         "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: hidden}\n",
		"apps/web/sub/kustomization.yaml": "resources: [svc.yaml]\n",
		"apps/web/sub/svc.yaml":           "apiVersion: v1\nkind: Service\nmetadata: {name: web}\nspec: {ports: [{port: 80}]}\n",
	})

	built, err := buildFluxKustomization(fsys, spec(t, `{"path":"./apps/web","targetNamespace":"web"}`), "apps", "flux-system", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Like kustomize-controller: a dot directory is not skipped, README.md is not YAML.
	if len(built) != 4 {
		t.Fatalf("got %d objects", len(built))
	}

	svc := builtByName(t, built, "Service", "web")
	meta := svc.obj["metadata"].(map[string]any)
	labels := meta["labels"].(map[string]any)

	if meta["namespace"] != "web" || labels[fluxOwnerName] != "apps" || labels[fluxOwnerNamespace] != "flux-system" {
		t.Fatalf("service metadata %v", meta)
	}

	// Without postBuild nothing is substituted.
	d := builtByName(t, built, "Deployment", "web")
	if b, _ := json.Marshal(d.obj); !strings.Contains(string(b), "${DOMAIN}") {
		t.Fatalf("substituted without postBuild: %s", b)
	}
}

func TestBuildFluxKustomizationSettingsAndPostBuild(t *testing.T) {
	fsys := fluxSourceFS(t, map[string]string{
		"app/kustomization.yaml":       "resources: [deploy.yaml, cm.yaml]\n",
		"app/deploy.yaml":              deployYAML,
		"app/cm.yaml":                  cmYAML,
		"app/extra/kustomization.yaml": "apiVersion: kustomize.config.k8s.io/v1alpha1\nkind: Component\nlabels: [{pairs: {tier: front}}]\n",
	})

	s := spec(t, `{
		"path": "app", "targetNamespace": "prod", "namePrefix": "p-",
		"commonMetadata": {"labels": {"team": "a"}, "annotations": {"owner": "ops"}},
		"images": [{"name": "ghcr.io/example/web", "newTag": "2.0.0"}],
		"patches": [{"target": {"kind": "Deployment"}, "patch": "- op: add\n  path: /spec/replicas\n  value: 3"}],
		"components": ["extra"],
		"postBuild": {"substitute": {"DOMAIN": "example.org"}}
	}`)

	built, err := buildFluxKustomization(fsys, s, "apps", "flux-system", map[string]string{"DOMAIN": "example.org"})
	if err != nil {
		t.Fatal(err)
	}

	d := builtByName(t, built, "Deployment", "p-web")
	b, _ := json.Marshal(d.obj)

	for _, want := range []string{`"namespace":"prod"`, `web:2.0.0"`, `"replicas":3`, `"value":"example.org"`, `"value":"info"`, `"team":"a"`, `"owner":"ops"`, `"tier":"front"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}

	cm := builtByName(t, built, "ConfigMap", "p-raw")
	if cm.obj["data"].(map[string]any)["template"] != "${KEEP_ME}" {
		t.Fatalf("substituted despite the opt-out: %v", cm.obj["data"])
	}
}

func TestBuildFluxKustomizationEncryptedAndErrors(t *testing.T) {
	fsys := fluxSourceFS(t, map[string]string{
		"s/secret.yaml": "apiVersion: v1\nkind: Secret\nmetadata:\n  name: creds\ndata:\n  password: ENC[AES256_GCM,data:x]\nsops:\n  version: 3.9.0\n",
	})

	built, err := buildFluxKustomization(fsys, spec(t, `{"path":"s"}`), "apps", "flux-system", nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(built) != 1 || !built[0].encrypted || built[0].obj["data"] != nil || built[0].obj["sops"] != nil {
		t.Fatalf("got %+v", built)
	}

	if _, err := buildFluxKustomization(fsys, spec(t, `{"path":"missing"}`), "apps", "flux-system", nil); err == nil || !strings.Contains(err.Error(), "not in the source") {
		t.Fatalf("missing path: %v", err)
	}
}

func TestUntarFluxArtifactKeepsPathsInside(t *testing.T) {
	fsys := filesys.MakeFsInMemory()

	err := untarFluxArtifact(fluxTarball(t, map[string]string{"../../etc/evil.yaml": "x", "ok/a.yaml": "y"}), fsys, fluxSourceRoot)
	if err != nil {
		t.Fatal(err)
	}

	if !fsys.Exists(fluxSourceRoot+"/etc/evil.yaml") || !fsys.Exists(fluxSourceRoot+"/ok/a.yaml") || fsys.Exists("/etc/evil.yaml") {
		t.Fatal("a path escaped the source root")
	}

	if err := untarFluxArtifact([]byte("not a tarball"), fsys, fluxSourceRoot); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestParseFluxArtifactURL(t *testing.T) {
	srv, err := parseFluxArtifactURL("http://source-controller.flux-system.svc.cluster.local./gitrepository/flux-system/podinfo/abc.tar.gz")
	if err != nil || srv != (fluxArtifactServer{namespace: "flux-system", service: "source-controller", port: "80"}) {
		t.Fatalf("got %+v %v", srv, err)
	}

	srv, err = parseFluxArtifactURL("http://source-controller-shard1.gitops.svc:9090/x")
	if err != nil || srv != (fluxArtifactServer{namespace: "gitops", service: "source-controller-shard1", port: "9090"}) {
		t.Fatalf("got %+v %v", srv, err)
	}

	if _, err := parseFluxArtifactURL("http://localhost/x"); err == nil {
		t.Fatal("a host without a namespace was accepted")
	}
}

func TestFluxStoragePath(t *testing.T) {
	for args, want := range map[string]string{
		"": "/data",
		"--log-level=info --storage-path=/var/x/": "/var/x",
		"--storage-path /store":                   "/store",
	} {
		if got := fluxStoragePath(strings.Fields(args)); got != want {
			t.Errorf("%q: got %q, want %q", args, got, want)
		}
	}
}

func TestBuildFluxKustomizationEmptyPath(t *testing.T) {
	fsys := fluxSourceFS(t, map[string]string{"empty/README.md": "nothing to apply"})

	built, err := buildFluxKustomization(fsys, spec(t, `{"path":"empty"}`), "apps", "flux-system", nil)
	if err != nil || len(built) != 0 {
		t.Fatalf("got %d objects, %v", len(built), err)
	}
}

func TestBuildFluxKustomizationMergesImagesLikeFlux(t *testing.T) {
	fsys := fluxSourceFS(t, map[string]string{
		"app/kustomization.yaml": "resources: [deploy.yaml]\nimages:\n- name: ghcr.io/example/web\n  newName: registry.example.com/web\n  newTag: 1.5.0\n",
		"app/deploy.yaml":        deployYAML,
	})

	built, err := buildFluxKustomization(fsys, spec(t, `{"path":"app","images":[{"name":"ghcr.io/example/web","newTag":"2.0.0"}]}`), "apps", "flux-system", nil)
	if err != nil {
		t.Fatal(err)
	}

	b, _ := json.Marshal(builtByName(t, built, "Deployment", "web").obj)
	if !strings.Contains(string(b), `"image":"registry.example.com/web:2.0.0"`) {
		t.Fatalf("got %s", b)
	}
}

func TestBuildFluxKustomizationCommonMetadataOnObjectsOnly(t *testing.T) {
	fsys := fluxSourceFS(t, map[string]string{"app/deploy.yaml": deployYAML})

	built, err := buildFluxKustomization(fsys, spec(t, `{"path":"app","commonMetadata":{"labels":{"team":"${TEAM}"},"annotations":{"owner":"ops"}}}`), "apps", "flux-system", map[string]string{"TEAM": "x"})
	if err != nil {
		t.Fatal(err)
	}

	d := builtByName(t, built, "Deployment", "web").obj
	meta := d["metadata"].(map[string]any)
	tmpl := d["spec"].(map[string]any)["template"].(map[string]any)["metadata"].(map[string]any)

	// Set after the substitutions, on the object's own metadata.
	if meta["labels"].(map[string]any)["team"] != "${TEAM}" || meta["annotations"].(map[string]any)["owner"] != "ops" {
		t.Fatalf("metadata %v", meta)
	}

	if tmpl["annotations"] != nil || tmpl["labels"].(map[string]any)["team"] != nil {
		t.Fatalf("pod template changed: %v", tmpl)
	}
}

func TestBuildFluxKustomizationComponentsAndBadFiles(t *testing.T) {
	fsys := fluxSourceFS(t, map[string]string{"app/deploy.yaml": deployYAML})

	if _, err := buildFluxKustomization(fsys, spec(t, `{"path":"app","components":["https://example.com/c"]}`), "a", "b", nil); err == nil || !strings.Contains(err.Error(), "local and relative") {
		t.Fatalf("remote component: %v", err)
	}

	fsys = fluxSourceFS(t, map[string]string{"app/deploy.yaml": deployYAML})
	if _, err := buildFluxKustomization(fsys, spec(t, `{"path":"app","components":["missing"],"ignoreMissingComponents":true}`), "a", "b", nil); err != nil {
		t.Fatalf("missing component ignored: %v", err)
	}

	fsys = fluxSourceFS(t, map[string]string{"app/deploy.yaml": deployYAML, "app/notes.yaml": "just: text\n"})
	if _, err := buildFluxKustomization(fsys, spec(t, `{"path":"app"}`), "a", "b", nil); err == nil || !strings.Contains(err.Error(), "notes.yaml") {
		t.Fatalf("not a manifest: %v", err)
	}
}

func TestBuildFluxKustomizationSubstituteStrategy(t *testing.T) {
	level := func(js string) any {
		fsys := fluxSourceFS(t, map[string]string{"app/deploy.yaml": deployYAML})

		built, err := buildFluxKustomization(fsys, spec(t, js), "a", "b", map[string]string{})
		if err != nil {
			t.Fatal(err)
		}

		c := builtByName(t, built, "Deployment", "web").obj["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0]

		return c.(map[string]any)["env"].([]any)[1].(map[string]any)["value"]
	}

	// No variable: nothing substituted, unless the strategy says always.
	if v := level(`{"path":"app","postBuild":{}}`); v != "${LEVEL:=info}" {
		t.Fatalf("WithVariables: %v", v)
	}

	if v := level(`{"path":"app","postBuild":{"substituteStrategy":"Always"}}`); v != "info" {
		t.Fatalf("Always: %v", v)
	}
}
