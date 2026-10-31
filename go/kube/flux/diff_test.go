package flux

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube/kubetest"
)

const (
	diffKs     = "/apis/kustomize.toolkit.fluxcd.io/v1/namespaces/flux-system/kustomizations/apps"
	diffGit    = "/apis/source.toolkit.fluxcd.io/v1/namespaces/flux-system/gitrepositories/fleet"
	diffDeploy = "/apis/apps/v1/namespaces/web/deployments/web"
	diffCM     = "/api/v1/namespaces/web/configmaps/settings"
	diffArt    = "gitrepository/flux-system/fleet/abc.tar.gz"
)

// diffAPI is a cluster with a Kustomization "apps" deploying ./app to namespace web: its
// Deployment drifted, a ConfigMap is new, a Service was dropped from Git (pruned), another
// is kept by its annotation, a third is already gone. source-controller only answers
// through the proxy (no Exec).
func diffAPI(t *testing.T) *kubetest.FakeAPI {
	t.Helper()

	tgz := fluxTarball(t, map[string]string{
		"app/kustomization.yaml": "resources: [deploy.yaml, cm.yaml, off.yaml]\n",
		"app/off.yaml":           "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: off\n  annotations:\n    kustomize.toolkit.fluxcd.io/reconcile: disabled\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: skipped\n  labels:\n    kustomize.toolkit.fluxcd.io/ssa: Ignore\n",
		"app/deploy.yaml":        "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  template:\n    spec:\n      containers:\n      - name: web\n        image: web:2\n        env:\n        - name: DOMAIN\n          value: ${DOMAIN}\n",
		"app/cm.yaml":            "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\ndata:\n  level: info\n",
	})

	labels := `"labels":{"kustomize.toolkit.fluxcd.io/name":"apps","kustomize.toolkit.fluxcd.io/namespace":"flux-system"}`

	return kubetest.New(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`,
		"GET " + diffKs: `{"metadata":{"name":"apps","namespace":"flux-system"},
			"spec":{"path":"./app","prune":true,"targetNamespace":"web","sourceRef":{"kind":"GitRepository","name":"fleet"},
				"postBuild":{"substituteFrom":[{"kind":"Secret","name":"vars"},{"kind":"ConfigMap","name":"optional","optional":true}]}},
			"status":{"lastAppliedRevision":"main@sha1:old","inventory":{"entries":[
				{"id":"web_web_apps_Deployment","v":"v1"},{"id":"web_legacy__Service","v":"v1"},
				{"id":"web_kept__Service","v":"v1"},{"id":"web_gone__ConfigMap","v":"v1"}]}}}`,
		"GET " + diffGit: `{"status":{"artifact":{"path":"` + diffArt + `","url":"http://source-controller.flux-system.svc.cluster.local./` + diffArt + `","revision":"main@sha1:new"}}}`,
		"GET /api/v1/namespaces/flux-system/services/source-controller":                     `{"spec":{"selector":{"app":"source-controller"}}}`,
		"GET /api/v1/namespaces/flux-system/pods":                                           `{"items":[{"metadata":{"name":"source-controller-1"},"spec":{"containers":[{"name":"manager","args":["--storage-path=/data"]}]},"status":{"phase":"Running"}}]}`,
		"GET /api/v1/namespaces/flux-system/services/source-controller:80/proxy/" + diffArt: string(tgz),
		"GET /api/v1/namespaces/flux-system/secrets/vars":                                   `{"data":{"DOMAIN":"` + base64.StdEncoding.EncodeToString([]byte("internal.example.net")) + `"}}`,
		"GET /apis/apps/v1": `{"resources":[{"name":"deployments","kind":"Deployment","namespaced":true},{"name":"deployments/scale","kind":"Scale","namespaced":true}]}`,
		"GET /api/v1":       `{"resources":[{"name":"configmaps","kind":"ConfigMap","namespaced":true},{"name":"services","kind":"Service","namespaced":true}]}`,
		"GET " + diffDeploy: `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"web","resourceVersion":"7",` + labels + `},
			"spec":{"template":{"spec":{"containers":[{"name":"web","image":"web:1","env":[{"name":"DOMAIN","value":"internal.example.net"}]}]}}},"status":{"replicas":1}}`,
		"PATCH " + diffDeploy: `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"web","resourceVersion":"8",` + labels + `},
			"spec":{"template":{"spec":{"containers":[{"name":"web","image":"web:2","env":[{"name":"DOMAIN","value":"internal.example.net"}]}]}}}}`,
		"PATCH " + diffCM: `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"web",` + labels + `},"data":{"level":"info"}}`,
		"GET /api/v1/namespaces/web/services/legacy": `{"apiVersion":"v1","kind":"Service","metadata":{"name":"legacy","namespace":"web"},"spec":{"ports":[{"port":80}]}}`,
		"GET /api/v1/namespaces/web/services/kept":   `{"apiVersion":"v1","kind":"Service","metadata":{"name":"kept","namespace":"web","annotations":{"kustomize.toolkit.fluxcd.io/prune":"disabled"}}}`,
	})
}

func TestDiffFluxKustomization(t *testing.T) {
	f := diffAPI(t)

	d, err := Diff(context.Background(), Env{Client: f.KubeClient(t)}, "flux-system", "apps")
	if err != nil {
		t.Fatal(err)
	}

	if d.Revision != "main@sha1:new" || d.Applied != "main@sha1:old" {
		t.Fatalf("revisions %q %q", d.Revision, d.Applied)
	}

	var got []string
	for _, r := range d.Resources {
		got = append(got, r.Change+" "+r.Kind+"/"+r.Name)
	}

	// What needs a look first; the ConfigMap already gone is left out.
	want := "created ConfigMap/settings, changed Deployment/web, deleted Service/legacy, ignored ConfigMap/off, ignored ConfigMap/skipped, ignored Service/kept"
	if strings.Join(got, ", ") != want {
		t.Fatalf("got %v", got)
	}

	if deploy := d.Resources[1]; !strings.Contains(deploy.Diff, "-          image: web:1\n+          image: web:2\n") || strings.Contains(deploy.Diff, "labels") {
		t.Fatalf("deployment diff:\n%s", deploy.Diff)
	}

	out, _ := json.Marshal(d)
	if strings.Contains(string(out), "internal.example.net") {
		t.Fatalf("a value read from a Secret shows: %s", out)
	}

	var applied string

	for _, r := range f.Recorded() {
		if r.Method == "GET" && strings.Contains(r.Path, "/configmaps/optional") {
			continue
		}

		if r.Method != "GET" {
			if r.Method != "PATCH" || !strings.Contains(r.Query, "dryRun=All") || !strings.Contains(r.Query, "fieldManager=kustomize-controller") || r.ContentType != "application/apply-patch+yaml" {
				t.Fatalf("not a dry run: %+v", r)
			}

			if r.Path == diffDeploy {
				applied = r.Body
			}
		}
	}

	// The wanted object carries the substituted Secret value and the owner labels.
	if !strings.Contains(applied, "internal.example.net") || !strings.Contains(applied, OwnerName) || !strings.Contains(applied, `"namespace":"web"`) {
		t.Fatalf("applied %s", applied)
	}
}

func TestDiffFluxKustomizationReportsAStoppedSource(t *testing.T) {
	f := kubetest.New(t, map[string]string{
		"GET /apis":      `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`,
		"GET " + diffKs:  `{"spec":{"path":"./app","sourceRef":{"kind":"GitRepository","name":"fleet"}}}`,
		"GET " + diffGit: `{"status":{}}`,
	})

	if _, err := Diff(context.Background(), Env{Client: f.KubeClient(t)}, "flux-system", "apps"); err == nil || !strings.Contains(err.Error(), "no artifact") {
		t.Fatalf("got %v", err)
	}
}

func TestFetchArtifactByExecFirst(t *testing.T) {
	f := kubetest.New(t, map[string]string{
		"GET /api/v1/namespaces/flux-system/services/source-controller": `{"spec":{"selector":{"app":"source-controller"}}}`,
		"GET /api/v1/namespaces/flux-system/pods":                       `{"items":[{"metadata":{"name":"source-controller-1"},"spec":{"containers":[{"name":"manager","args":["--storage-path=/store"]}]},"status":{"phase":"Running"}}]}`,
	})

	var ran []string

	env := Env{Client: f.KubeClient(t), Exec: func(_ context.Context, _ time.Duration, _ int, namespace, pod, container string, argv []string) ([]byte, []byte, error) {
		ran = append([]string{namespace, pod, container}, argv...)

		return []byte("tarball"), nil, nil
	}}

	got, err := FetchArtifact(context.Background(), env, &Artifact{Path: diffArt, URL: "http://source-controller.flux-system.svc/x"})
	if err != nil || string(got) != "tarball" {
		t.Fatalf("got %q %v", got, err)
	}

	if want := "flux-system source-controller-1 manager cat /store/" + diffArt; strings.Join(ran, " ") != want {
		t.Fatalf("ran %v", ran)
	}
}

func TestFetchArtifactRefusesAPathThatLeaves(t *testing.T) {
	f := kubetest.New(t, nil)

	if _, err := FetchArtifact(context.Background(), Env{Client: f.KubeClient(t)}, &Artifact{Path: "a b;rm", URL: "http://s.ns/x"}); err == nil {
		t.Fatal("a path a shell could read was accepted")
	}
}
