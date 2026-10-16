package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
)

const (
	argoRolloutsGroups = `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`
	argoRolloutsList   = "/apis/argoproj.io/v1alpha1/rollouts"
	argoRolloutShop    = "/apis/argoproj.io/v1alpha1/namespaces/web/rollouts/shop"
)

func argoRolloutsFixture(t *testing.T) string {
	t.Helper()

	raw, err := os.ReadFile("testdata/argorollouts/rollouts.json")
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

func argoRolloutsClient(t *testing.T, answers map[string]string) (*kubeClient, *fakeKubeAPI) {
	t.Helper()

	f := newFakeKubeAPI(t, answers)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return k, f
}

func TestMapArgoRollouts(t *testing.T) {
	k, _ := argoRolloutsClient(t, map[string]string{"GET /apis": argoRolloutsGroups, "GET " + argoRolloutsList: argoRolloutsFixture(t)})

	s, err := readArgoRollouts(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	by := map[string]argoRollout{}
	for _, r := range s.Rollouts {
		by[r.Name] = r
	}

	if !s.Installed || len(s.Rollouts) != 4 || s.Rollouts[0].Name != "checkout" {
		t.Fatalf("rollouts %+v", s)
	}

	shop := by["shop"]
	if shop.Strategy != argoRolloutCanary || !shop.Paused || shop.Step != 1 || len(shop.Steps) != 5 || shop.Weight != 20 ||
		shop.PauseReasons[0] != "CanaryPauseStep" || shop.PausedSince == 0 || shop.Level != healthWarning || shop.UpdatedAt == 0 {
		t.Errorf("shop %+v", shop)
	}

	wantSteps := []argoRolloutStep{{"setWeight", "20%"}, {"pause", "∞"}, {"setWeight", "50%"}, {"analysis", "success-rate"}, {"pause", "1h"}}
	if !equalJSON(t, shop.Steps, wantSteps) {
		t.Errorf("steps %+v", shop.Steps)
	}

	api := by["api"]
	if api.Strategy != argoRolloutBlueGreen || api.AutoPromotion || !api.Paused || api.Analysis == nil || api.Analysis.Name != "api-8b2d4f6a0-pre" || api.Step != -1 {
		t.Errorf("api %+v", api)
	}

	checkout := by["checkout"]
	if !checkout.Aborted || checkout.Level != healthCritical || checkout.Analysis == nil || checkout.Analysis.Status != "Failed" || checkout.Weight != 10 {
		t.Errorf("checkout %+v", checkout)
	}

	web := by["web"]
	if web.Level != healthOK || web.WorkloadRef != "Deployment/web" || web.Weight != 0 || web.Steps[1].Label != "30s" || len(web.Images) != 0 {
		t.Errorf("web %+v", web)
	}
}

// Argo CD alone serves argoproj.io without Rollouts: not installed, no error.
func TestKubeArgoRolloutsNotInstalled(t *testing.T) {
	k, _ := argoRolloutsClient(t, map[string]string{"GET /apis": argoRolloutsGroups})

	s, err := readArgoRollouts(context.Background(), k)
	if err != nil || s.Installed || len(s.Rollouts) != 0 {
		t.Fatalf("%+v %v", s, err)
	}

	k, _ = argoRolloutsClient(t, map[string]string{"GET /apis": `{"groups":[]}`})
	if s, err := readArgoRollouts(context.Background(), k); err != nil || s.Installed {
		t.Fatalf("no group: %+v %v", s, err)
	}
}

func rolloutObject(t *testing.T, raw string) argoRolloutObject {
	t.Helper()

	var o argoRolloutObject
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		t.Fatal(err)
	}

	return o
}

func TestArgoRolloutPatches(t *testing.T) {
	var list kubeList[argoRolloutObject]
	if err := json.Unmarshal([]byte(argoRolloutsFixture(t)), &list); err != nil {
		t.Fatal(err)
	}

	shop, api, checkout, web := list.Items[0], list.Items[1], list.Items[2], list.Items[3]
	midStep := rolloutObject(t, `{"spec":{"strategy":{"canary":{"steps":[{"setWeight":10},{"analysis":{}},{"setWeight":50}]}}},
		"status":{"currentStepIndex":2,"stableRS":"a","currentPodHash":"b"}}`)
	pausedSpec := rolloutObject(t, `{"spec":{"paused":true,"strategy":{"canary":{"steps":[]}}},"status":{"stableRS":"a","currentPodHash":"a","pauseConditions":[{"reason":"PausedBySpec"}],"controllerPause":true}}`)

	cases := []struct {
		name   string
		obj    argoRolloutObject
		action string
		want   string // the patches, "status:" or "object:" then the body
		err    bool
	}{
		{"pause", shop, argoRolloutPause, `object:{"spec":{"paused":true}}`, false},
		{"promote a paused canary", shop, argoRolloutPromote, `status:{"status":{"pauseConditions":null}}`, false},
		{"promote mid-step", midStep, argoRolloutPromote, `status:{"status":{"currentStepIndex":3,"pauseConditions":null}}`, false},
		{"promote paused by spec", pausedSpec, argoRolloutPromote, `object:{"spec":{"paused":false}} status:{"status":{"controllerPause":false,"pauseConditions":null}}`, false},
		{"promote a blue-green", api, argoRolloutPromote, `status:{"status":{"pauseConditions":null}}`, false},
		{"promote fully", shop, argoRolloutPromoteFull, `status:{"status":{"promoteFull":true}}`, false},
		{"abort", shop, argoRolloutAbort, `status:{"status":{"abort":true}}`, false},
		{"retry", checkout, argoRolloutRetry, `status:{"status":{"abort":false}}`, false},
		{"nothing to promote fully", web, argoRolloutPromoteFull, "", true},
		{"nothing to promote", web, argoRolloutPromote, `status:{"status":{"currentStepIndex":2,"pauseConditions":null}}`, false},
	}

	for _, c := range cases {
		patches, err := argoRolloutPatches(c.obj, c.action)
		if c.err {
			var apiErr *kubeAPIError
			if !errors.As(err, &apiErr) || apiErr.Code != http.StatusConflict {
				t.Errorf("%s: %v, want a 409", c.name, err)
			}

			continue
		}

		var got []string

		for _, p := range patches {
			body, _ := json.Marshal(p.body) //nolint:errcheck
			where := "object:"
			if p.status {
				where = "status:"
			}

			got = append(got, where+string(body))
		}

		if err != nil || strings.Join(got, " ") != c.want {
			t.Errorf("%s: %v %q\nwant %q", c.name, err, strings.Join(got, " "), c.want)
		}
	}

	// A settled blue-green has nothing to abort.
	settled := rolloutObject(t, `{"spec":{"strategy":{"blueGreen":{}}},"status":{"stableRS":"a","currentPodHash":"a"}}`)
	if _, err := argoRolloutPatches(settled, argoRolloutAbort); err == nil {
		t.Error("a settled blue-green was aborted")
	}
}

// Status patches go to the status subresource; an old controller without it takes them on
// the object.
func TestArgoRolloutActionStatusSubresource(t *testing.T) {
	obj := `{"metadata":{"name":"shop","namespace":"web","resourceVersion":"11"},"spec":{"strategy":{"canary":{"steps":[]}}},"status":{"stableRS":"a","currentPodHash":"b"}}`
	k, f := argoRolloutsClient(t, map[string]string{
		"GET /apis":                            argoRolloutsGroups,
		"GET " + argoRolloutShop:               obj,
		"PATCH " + argoRolloutShop + "/status": `{}`,
		"PATCH " + argoRolloutShop:             `{}`,
	})

	if err := argoRolloutAction(context.Background(), k, "web", "shop", argoRolloutAbort); err != nil {
		t.Fatal(err)
	}

	f.answerWith("PATCH "+argoRolloutShop+"/status", http.StatusNotFound, `{"kind":"Status","reason":"NotFound"}`)

	if err := argoRolloutAction(context.Background(), k, "web", "shop", argoRolloutRetry); err != nil {
		t.Fatal(err)
	}

	var patches []string

	for _, r := range f.recorded() {
		if r.method == http.MethodPatch {
			if r.contentType != "application/merge-patch+json" {
				t.Fatalf("patch %+v", r)
			}

			patches = append(patches, strings.TrimPrefix(r.path, argoRolloutShop)+" "+r.body)
		}
	}

	want := []string{`/status {"status":{"abort":true}}`, `/status {"status":{"abort":false}}`, ` {"status":{"abort":false}}`}
	if !equalJSON(t, patches, want) {
		t.Fatalf("patches %q", patches)
	}
}

func TestKubeArgoRolloutsDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeArgoRollouts(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var s argoRolloutsStatus
	if err := json.Unmarshal([]byte(out), &s); err != nil || !s.Installed || len(s.Rollouts) != 4 {
		t.Fatalf("demo %s %v", out, err)
	}

	var paused, aborted, blueGreen, healthy bool
	for _, r := range s.Rollouts {
		paused = paused || r.Paused && r.Strategy == argoRolloutCanary
		aborted = aborted || r.Aborted
		blueGreen = blueGreen || r.Strategy == argoRolloutBlueGreen
		healthy = healthy || r.Level == healthOK
	}

	if !paused || !aborted || !blueGreen || !healthy {
		t.Fatalf("demo states %+v", s.Rollouts)
	}

	if err := KubeArgoRolloutAction(cfg, "", "", "demo", "shop", argoRolloutPromote); !errors.Is(err, errDemoUnavailable) {
		t.Fatalf("got %v", err)
	}

	if err := KubeArgoRolloutAction(cfg, "", "", "demo", "shop", "undo"); err == nil {
		t.Fatal("an unknown action ran")
	}
}
