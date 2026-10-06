package ichorgo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const (
	fluxDiffKs     = "/apis/kustomize.toolkit.fluxcd.io/v1/namespaces/flux-system/kustomizations/apps"
	fluxDiffGit    = "/apis/source.toolkit.fluxcd.io/v1/namespaces/flux-system/gitrepositories/fleet"
	fluxDiffDeploy = "/apis/apps/v1/namespaces/web/deployments/web"
	fluxDiffCM     = "/api/v1/namespaces/web/configmaps/settings"
	fluxDiffArt    = "gitrepository/flux-system/fleet/abc.tar.gz"
)

// fluxDiffAPI is a cluster with a Kustomization "apps" deploying ./app to namespace web: its
// Deployment drifted, a ConfigMap is new, a Service was dropped from Git (pruned), another
// is kept by its annotation, a third is already gone. source-controller only answers
// through the proxy (exec is refused).
func fluxDiffAPI(t *testing.T) *fakeKubeAPI {
	t.Helper()

	tgz := fluxTarball(t, map[string]string{
		"app/kustomization.yaml": "resources: [deploy.yaml, cm.yaml, off.yaml]\n",
		"app/off.yaml":           "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: off\n  annotations:\n    kustomize.toolkit.fluxcd.io/reconcile: disabled\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: skipped\n  labels:\n    kustomize.toolkit.fluxcd.io/ssa: Ignore\n",
		"app/deploy.yaml":        "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  template:\n    spec:\n      containers:\n      - name: web\n        image: web:2\n        env:\n        - name: DOMAIN\n          value: ${DOMAIN}\n",
		"app/cm.yaml":            "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: settings\ndata:\n  level: info\n",
	})

	labels := `"labels":{"kustomize.toolkit.fluxcd.io/name":"apps","kustomize.toolkit.fluxcd.io/namespace":"flux-system"}`

	return newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`,
		"GET " + fluxDiffKs: `{"metadata":{"name":"apps","namespace":"flux-system"},
			"spec":{"path":"./app","prune":true,"targetNamespace":"web","sourceRef":{"kind":"GitRepository","name":"fleet"},
				"postBuild":{"substituteFrom":[{"kind":"Secret","name":"vars"},{"kind":"ConfigMap","name":"optional","optional":true}]}},
			"status":{"lastAppliedRevision":"main@sha1:old","inventory":{"entries":[
				{"id":"web_web_apps_Deployment","v":"v1"},{"id":"web_legacy__Service","v":"v1"},
				{"id":"web_kept__Service","v":"v1"},{"id":"web_gone__ConfigMap","v":"v1"}]}}}`,
		"GET " + fluxDiffGit: `{"status":{"artifact":{"path":"` + fluxDiffArt + `","url":"http://source-controller.flux-system.svc.cluster.local./` + fluxDiffArt + `","revision":"main@sha1:new"}}}`,
		"GET /api/v1/namespaces/flux-system/services/source-controller":                         `{"spec":{"selector":{"app":"source-controller"}}}`,
		"GET /api/v1/namespaces/flux-system/pods":                                               `{"items":[{"metadata":{"name":"source-controller-1"},"spec":{"containers":[{"name":"manager","args":["--storage-path=/data"]}]},"status":{"phase":"Running"}}]}`,
		"GET /api/v1/namespaces/flux-system/services/source-controller:80/proxy/" + fluxDiffArt: string(tgz),
		"GET /api/v1/namespaces/flux-system/secrets/vars":                                       `{"data":{"DOMAIN":"` + base64.StdEncoding.EncodeToString([]byte("internal.example.net")) + `"}}`,
		"GET /apis/apps/v1": `{"resources":[{"name":"deployments","kind":"Deployment","namespaced":true},{"name":"deployments/scale","kind":"Scale","namespaced":true}]}`,
		"GET /api/v1":       `{"resources":[{"name":"configmaps","kind":"ConfigMap","namespaced":true},{"name":"services","kind":"Service","namespaced":true}]}`,
		"GET " + fluxDiffDeploy: `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"web","resourceVersion":"7",` + labels + `},
			"spec":{"template":{"spec":{"containers":[{"name":"web","image":"web:1","env":[{"name":"DOMAIN","value":"internal.example.net"}]}]}}},"status":{"replicas":1}}`,
		"PATCH " + fluxDiffDeploy: `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"web","resourceVersion":"8",` + labels + `},
			"spec":{"template":{"spec":{"containers":[{"name":"web","image":"web:2","env":[{"name":"DOMAIN","value":"internal.example.net"}]}]}}}}`,
		"PATCH " + fluxDiffCM:                        `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"web",` + labels + `},"data":{"level":"info"}}`,
		"GET /api/v1/namespaces/web/services/legacy": `{"apiVersion":"v1","kind":"Service","metadata":{"name":"legacy","namespace":"web"},"spec":{"ports":[{"port":80}]}}`,
		"GET /api/v1/namespaces/web/services/kept":   `{"apiVersion":"v1","kind":"Service","metadata":{"name":"kept","namespace":"web","annotations":{"kustomize.toolkit.fluxcd.io/prune":"disabled"}}}`,
	})
}

func TestDiffFluxKustomization(t *testing.T) {
	f := fluxDiffAPI(t)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	d, err := diffFluxKustomization(context.Background(), k, "flux-system", "apps")
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

	for _, r := range f.recorded() {
		if r.method == "GET" && strings.Contains(r.path, "/configmaps/optional") {
			continue
		}

		if r.method != "GET" {
			if r.method != "PATCH" || !strings.Contains(r.query, "dryRun=All") || !strings.Contains(r.query, "fieldManager=kustomize-controller") || r.contentType != "application/apply-patch+yaml" {
				t.Fatalf("not a dry run: %+v", r)
			}

			if r.path == fluxDiffDeploy {
				applied = r.body
			}
		}
	}

	// The wanted object carries the substituted Secret value and the owner labels.
	if !strings.Contains(applied, "internal.example.net") || !strings.Contains(applied, fluxOwnerName) || !strings.Contains(applied, `"namespace":"web"`) {
		t.Fatalf("applied %s", applied)
	}
}

func TestDiffFluxKustomizationReportsAStoppedSource(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":          `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`,
		"GET " + fluxDiffKs:  `{"spec":{"path":"./app","sourceRef":{"kind":"GitRepository","name":"fleet"}}}`,
		"GET " + fluxDiffGit: `{"status":{}}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := diffFluxKustomization(context.Background(), k, "flux-system", "apps"); err == nil || !strings.Contains(err.Error(), "no artifact") {
		t.Fatalf("got %v", err)
	}
}

func TestKubeFluxDiffValidates(t *testing.T) {
	if _, err := KubeFluxDiff("", "", "", "HelmRelease", "flux-system", "apps"); err == nil || !strings.Contains(err.Error(), "only a Kustomization") {
		t.Fatalf("got %v", err)
	}

	if _, err := KubeFluxDiff("", "", "", "Kustomization", "flux-system", "../x"); err == nil {
		t.Fatal("bad name accepted")
	}
}

func TestDemoFluxDiff(t *testing.T) {
	d := demoFluxDiff("flux-system", "apps")

	seen := map[string]bool{}
	for _, r := range d.Resources {
		seen[r.Change] = true

		if (r.Change == diffChangeChanged || r.Change == diffChangeCreated || r.Change == diffChangeDeleted) && !strings.HasPrefix(r.Diff, "--- live\n+++ wanted\n@@ ") {
			t.Errorf("%s %s: diff %q", r.Kind, r.Name, r.Diff)
		}
	}

	for _, c := range []string{diffChangeCreated, diffChangeChanged, diffChangeDeleted, diffChangeEncrypted, diffChangeUnchanged} {
		if !seen[c] {
			t.Errorf("the demo shows no %s object", c)
		}
	}
}
