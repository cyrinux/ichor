package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const applyManifests = `# a pasted snippet
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  resourceVersion: "12"
  uid: abc
spec:
  replicas: 2
status:
  readyReplicas: 1
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
  namespace: other
data:
  level: info
---
apiVersion: v1
kind: Namespace
metadata:
  name: web
---
`

func TestApplyDocuments(t *testing.T) {
	objs, err := applyDocuments("web", applyManifests)
	if err != nil {
		t.Fatal(err)
	}

	if len(objs) != 3 {
		t.Fatalf("%d objects", len(objs))
	}

	meta := objs[0]["metadata"].(map[string]any)
	if _, has := meta["resourceVersion"]; has || objs[0]["status"] != nil || meta["uid"] != nil {
		t.Fatalf("read-only fields kept: %v", objs[0])
	}

	cases := map[string]string{
		"":                                      "nothing to apply",
		"# only a comment\n":                    "nothing to apply",
		"apiVersion: v1\nmetadata: {name: a}\n": "no apiVersion or kind",
		"apiVersion: v1\nkind: ConfigMap\n":     "no metadata.name",
		"apiVersion: v1\nkind: ConfigMap\nmetadata: {name: 'Bad Name'}\n": "invalid name",
		"a: [": "invalid YAML",
		"apiVersion: v1\nkind: Secret\nmetadata: {name: s}\ndata: {k: '<hidden, 12 bytes>'}\n": "hidden Secret values",
	}

	for in, want := range cases {
		_, err := applyDocuments("", in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: got %v, want %q", in, err, want)
		}
	}

	if _, err := applyDocuments("", strings.Repeat("x", applyMaxBytes+1)); !errors.Is(err, errApplyTooBig) {
		t.Fatalf("size: %v", err)
	}

	if _, err := applyDocuments("Bad NS", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: a}\n"); err == nil {
		t.Fatal("bad fallback namespace accepted")
	}
}

// applyAPI is a cluster where the Deployment exists (and drifts), the ConfigMap is new, the
// Namespace exists unchanged; a Deployment "owned" is managed by someone else (conflict).
func applyAPI(t *testing.T) *fakeKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/apps/v1": `{"resources":[{"name":"deployments","kind":"Deployment","namespaced":true}]}`,
		"GET /api/v1":       `{"resources":[{"name":"configmaps","kind":"ConfigMap","namespaced":true},{"name":"namespaces","kind":"Namespace","namespaced":false}]}`,
		"GET /apis/apps/v1/namespaces/web/deployments/web":   `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"web","resourceVersion":"12"},"spec":{"replicas":1}}`,
		"PATCH /apis/apps/v1/namespaces/web/deployments/web": `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"web","namespace":"web","resourceVersion":"13"},"spec":{"replicas":2}}`,
		"PATCH /api/v1/namespaces/other/configmaps/settings": `{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"settings","namespace":"other"},"data":{"level":"info"}}`,
		"GET /api/v1/namespaces/web":                         `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"web"}}`,
		"PATCH /api/v1/namespaces/web":                       `{"apiVersion":"v1","kind":"Namespace","metadata":{"name":"web"}}`,
		"GET /apis/apps/v1/namespaces/web/deployments/owned": `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"owned","namespace":"web"},"spec":{"replicas":1}}`,
	})
	f.answerWith("PATCH /apis/apps/v1/namespaces/web/deployments/owned", 409, `{"kind":"Status","code":409,"reason":"Conflict","message":"Apply failed with 1 conflict: conflict with \"kustomize-controller\": .spec.replicas"}`)

	return f
}

func TestApplyObjectsPreviewAndApply(t *testing.T) {
	f := applyAPI(t)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	objs, err := applyDocuments("web", applyManifests+"apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: owned}\nspec: {replicas: 3}\n")
	if err != nil {
		t.Fatal(err)
	}

	preview := applyObjects(context.Background(), k, objs, "web", true)

	var got []string
	for _, r := range preview.Resources {
		got = append(got, r.Change+" "+r.Kind+"/"+r.Namespace+"/"+r.Name)
	}

	want := []string{"error Deployment/web/owned", "created ConfigMap/other/settings", "changed Deployment/web/web", "unchanged Namespace//web"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("preview:\n%s", strings.Join(got, "\n"))
	}

	for _, r := range preview.Resources {
		switch r.Name {
		case "web":
			if r.Kind == "Deployment" && (!strings.Contains(r.Diff, "-  replicas: 1") || !strings.Contains(r.Diff, "+  replicas: 2")) {
				t.Fatalf("deployment diff:\n%s", r.Diff)
			}
		case "owned":
			if !strings.Contains(r.Error, "another manager owns") || !strings.Contains(r.Error, "kustomize-controller") {
				t.Fatalf("conflict error %q", r.Error)
			}
		}
	}

	if preview.Failed != 1 || preview.Applied != 0 {
		t.Fatalf("counts %+v", preview)
	}

	// Every dry run went as a server-side apply by ichor, without forcing, and with the
	// fallback namespace on the Deployment that named none.
	for _, req := range f.recorded() {
		if req.method != "PATCH" {
			continue
		}

		if req.contentType != "application/apply-patch+yaml" || !strings.Contains(req.query, "dryRun=All") || !strings.Contains(req.query, "fieldManager=ichor") || strings.Contains(req.query, "force") {
			t.Fatalf("dry run request %s %s", req.path, req.query)
		}

		if req.path == "/apis/apps/v1/namespaces/web/deployments/web" {
			var sent map[string]any
			_ = json.Unmarshal([]byte(req.body), &sent)

			if sent["metadata"].(map[string]any)["namespace"] != "web" {
				t.Fatalf("namespace not filled in: %s", req.body)
			}
		}
	}

	applied := applyObjects(context.Background(), k, objs, "web", false)
	if applied.Applied != 3 || applied.Failed != 1 {
		t.Fatalf("apply counts %+v", applied)
	}

	real := 0
	for _, req := range f.recorded() {
		if req.method == "PATCH" && !strings.Contains(req.query, "dryRun") {
			real++
		}
	}

	if real != 4 {
		t.Fatalf("%d real applies", real)
	}
}

func TestApplyObjectNeedsNamespace(t *testing.T) {
	f := applyAPI(t)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	objs, _ := applyDocuments("", "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: loose}\n")

	res := applyObjects(context.Background(), k, objs, "", true)
	if res.Resources[0].Change != diffChangeError || !strings.Contains(res.Resources[0].Error, "namespaced") {
		t.Fatalf("%+v", res.Resources[0])
	}
}

func TestApplyAudit(t *testing.T) {
	objs, _ := applyDocuments("web", applyManifests)

	if got := applyAuditObject(objs); got != "3 objects" {
		t.Fatalf("object %q", got)
	}

	if got := applyAuditParams(objs); got != "Deployment, ConfigMap, Namespace" {
		t.Fatalf("params %q", got)
	}

	one, _ := applyDocuments("", "apiVersion: v1\nkind: Namespace\nmetadata: {name: web}\n")
	if got := applyAuditObject(one); got != "Namespace/web" {
		t.Fatalf("single %q", got)
	}
}
