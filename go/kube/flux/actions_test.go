package flux

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/kube/kubetest"
)

func TestPatchFor(t *testing.T) {
	const stamp = "2026-10-05T10:00:00Z"

	var active, suspended target
	suspended.Spec.Suspend = true

	cases := []struct {
		kind, action string
		obj          target
		want         string
		err          error
	}{
		{"Kustomization", ActionReconcile, active, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"` + stamp + `"}}}`, nil},
		{"GitRepository", ActionReconcile, active, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"` + stamp + `"}}}`, nil},
		{"Kustomization", ActionReconcile, suspended, "", errSuspended},
		{"Kustomization", ActionSuspend, active, `{"spec":{"suspend":true}}`, nil},
		{"HelmRelease", ActionResume, suspended, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"` + stamp + `"}},"spec":{"suspend":null}}`, nil},
		{"HelmRelease", ActionForce, active, `{"metadata":{"annotations":{"reconcile.fluxcd.io/forceAt":"` + stamp + `","reconcile.fluxcd.io/requestedAt":"` + stamp + `"}}}`, nil},
		{"HelmRelease", ActionReset, active, `{"metadata":{"annotations":{"reconcile.fluxcd.io/requestedAt":"` + stamp + `","reconcile.fluxcd.io/resetAt":"` + stamp + `"}}}`, nil},
		{"Kustomization", ActionForce, active, "", errHelmOnly},
	}

	for _, c := range cases {
		p, err := patchFor(c.kind, c.obj, c.action, stamp)
		if !errors.Is(err, c.err) {
			t.Errorf("%s %s: err %v", c.kind, c.action, err)

			continue
		}

		if c.err != nil {
			continue
		}

		if got, _ := json.Marshal(p); string(got) != c.want {
			t.Errorf("%s %s: %s", c.kind, c.action, got)
		}
	}
}

func TestSourceOf(t *testing.T) {
	var hr target
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"cilium","namespace":"kube-system"},"spec":{"chart":{"spec":{"sourceRef":{"kind":"HelmRepository","name":"cilium","namespace":"flux-system"}}}}}`), &hr); err != nil {
		t.Fatal(err)
	}

	if got, err := sourceOf("HelmRelease", hr); err != nil || got != (Ref{"HelmChart", "flux-system", "kube-system-cilium"}) {
		t.Fatalf("got %+v %v", got, err)
	}

	if _, err := sourceOf("GitRepository", hr); !errors.Is(err, ErrNoSource) {
		t.Fatalf("got %v", err)
	}
}

func TestActionWithSourcePatchesBoth(t *testing.T) {
	const (
		ks  = "/apis/kustomize.toolkit.fluxcd.io/v1/namespaces/flux-system/kustomizations/apps"
		git = "/apis/source.toolkit.fluxcd.io/v1/namespaces/flux-system/gitrepositories/flux-system"
	)

	f := kubetest.New(t, map[string]string{
		"GET /apis":    `{"groups":[{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`,
		"GET " + ks:    `{"metadata":{"name":"apps","namespace":"flux-system","resourceVersion":"101"},"spec":{"sourceRef":{"kind":"GitRepository","name":"flux-system"}}}`,
		"GET " + git:   `{"metadata":{"name":"flux-system","namespace":"flux-system","resourceVersion":"300"},"spec":{}}`,
		"PATCH " + ks:  `{}`,
		"PATCH " + git: `{}`,
	})

	if err := Action(context.Background(), f.KubeClient(t), "Kustomization", "flux-system", "apps", ActionReconcileWithSource, time.Now()); err != nil {
		t.Fatal(err)
	}

	var patches []string

	for _, r := range f.Recorded() {
		if r.Method == "PATCH" {
			if r.ContentType != "application/merge-patch+json" || !strings.Contains(r.Body, "requestedAt") {
				t.Fatalf("patch %+v", r)
			}

			patches = append(patches, r.Path)
		}
	}

	if !same(patches, []string{git, ks}) {
		t.Fatalf("source first, then the Kustomization: %v", patches)
	}
}

func TestSuspendPatchesTheHelmRelease(t *testing.T) {
	const hr = "/apis/helm.toolkit.fluxcd.io/v2/namespaces/web/helmreleases/shop"

	f := kubetest.New(t, map[string]string{
		"GET /apis":   `{"groups":[{"name":"helm.toolkit.fluxcd.io","preferredVersion":{"version":"v2"}}]}`,
		"GET " + hr:   `{"metadata":{"name":"shop","namespace":"web","resourceVersion":"7"},"spec":{}}`,
		"PATCH " + hr: `{}`,
	})

	if err := Suspend(context.Background(), f.KubeClient(t), "web", "shop", time.Now()); err != nil {
		t.Fatal(err)
	}

	got := f.Recorded()
	last := got[len(got)-1]

	if last.Method != "PATCH" || last.Body != `{"metadata":{"resourceVersion":"7"},"spec":{"suspend":true}}` {
		t.Fatalf("patch %+v", last)
	}
}

func TestActionWithoutFluxInstalled(t *testing.T) {
	f := kubetest.New(t, map[string]string{"GET /apis": `{"groups":[]}`})

	err := Action(context.Background(), f.KubeClient(t), "Kustomization", "flux-system", "apps", ActionReconcile, time.Now())
	if !errors.Is(err, errNotInstalled) {
		t.Fatalf("got %v", err)
	}
}
