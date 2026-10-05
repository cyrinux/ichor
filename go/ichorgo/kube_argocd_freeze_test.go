package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// argoTestNow is the fixtures' "now": 14:30:20 UTC, outside the infra project's nightly window.
var argoTestNow = time.Date(2026, 10, 5, 14, 30, 20, 0, time.UTC)

func argoProjectFixture(t *testing.T, name string) argoProjectObject {
	t.Helper()

	for _, p := range readArgoFixture[argoProjectObject](t, "appprojects.json") {
		if p.Metadata.Name == name {
			return p
		}
	}

	t.Fatalf("no project %q", name)

	return argoProjectObject{}
}

// applyArgoPatch is what the API server does with a freeze patch: replace the window list
// and set or remove the annotation.
func applyArgoPatch(t *testing.T, p argoProjectObject, patch map[string]any) argoProjectObject {
	t.Helper()

	data, _ := json.Marshal(patch)

	var parsed struct {
		Metadata struct {
			Annotations map[string]*string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			SyncWindows []json.RawMessage `json:"syncWindows"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	annotations := map[string]string{}
	for k, v := range p.Metadata.Annotations {
		annotations[k] = v
	}

	if v := parsed.Metadata.Annotations[argoFreezesAnnotation]; v != nil {
		annotations[argoFreezesAnnotation] = *v
	} else {
		delete(annotations, argoFreezesAnnotation)
	}

	p.Metadata.Annotations = annotations
	p.Spec.SyncWindows = parsed.Spec.SyncWindows

	return p
}

func TestArgoWindowTimes(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}

		return v
	}
	nightly := argoWindowObject{Kind: "deny", Schedule: "0 22 * * *", Duration: "8h"}
	paris := nightly
	paris.TimeZone = "Europe/Paris"

	for _, c := range []struct {
		name       string
		w          argoWindowObject
		now        string
		active     bool
		start, end string
		err        bool
	}{
		{"before", nightly, "2026-10-05T14:30:00Z", false, "2026-10-05T22:00:00Z", "2026-10-06T06:00:00Z", false},
		{"during", nightly, "2026-10-05T23:00:00Z", true, "2026-10-05T22:00:00Z", "2026-10-06T06:00:00Z", false},
		{"next morning", nightly, "2026-10-06T05:59:00Z", true, "2026-10-05T22:00:00Z", "2026-10-06T06:00:00Z", false},
		{"just ended", nightly, "2026-10-06T06:00:00Z", false, "2026-10-06T22:00:00Z", "2026-10-07T06:00:00Z", false},
		// 22:00 in Paris is 20:00 UTC in summer time.
		{"time zone", paris, "2026-10-05T20:30:00Z", true, "2026-10-05T20:00:00Z", "2026-10-06T04:00:00Z", false},
		{"bad schedule", argoWindowObject{Schedule: "never", Duration: "1h"}, "2026-10-05T14:30:00Z", false, "", "", true},
		{"bad duration", argoWindowObject{Schedule: "* * * * *", Duration: "soon"}, "2026-10-05T14:30:00Z", false, "", "", true},
		{"bad zone", argoWindowObject{Schedule: "* * * * *", Duration: "1h", TimeZone: "Mars/Olympus"}, "2026-10-05T14:30:00Z", false, "", "", true},
	} {
		active, start, end, err := argoWindowTimes(c.w, at(c.now))
		if (err != nil) != c.err {
			t.Fatalf("%s: err %v", c.name, err)
		}

		if c.err {
			continue
		}

		if active != c.active || !start.Equal(at(c.start)) || !end.Equal(at(c.end)) {
			t.Fatalf("%s: got %v %v %v", c.name, active, start.UTC(), end.UTC())
		}
	}
}

func TestArgoWindowMatches(t *testing.T) {
	app := argoApp{Name: "web-api", Destination: argoDest{Server: "https://kubernetes.default.svc", Name: "in-cluster", Namespace: "web"}}

	for _, c := range []struct {
		w    argoWindowObject
		want bool
	}{
		{argoWindowObject{Applications: []string{"*"}}, true},
		{argoWindowObject{Applications: []string{"web-*"}}, true},
		{argoWindowObject{Applications: []string{"web-ap?"}}, true},
		{argoWindowObject{Applications: []string{"{db,web}-api"}}, true},
		{argoWindowObject{Applications: []string{"web"}}, false},
		{argoWindowObject{Namespaces: []string{"web"}}, true},
		{argoWindowObject{Namespaces: []string{"we"}}, false},
		{argoWindowObject{Clusters: []string{"in-cluster"}}, true},
		{argoWindowObject{Clusters: []string{"https://kubernetes.*"}}, true},
		// Any selector by default, every non-empty one with andOperator.
		{argoWindowObject{Applications: []string{"db"}, Namespaces: []string{"web"}}, true},
		{argoWindowObject{Applications: []string{"db"}, Namespaces: []string{"web"}, AndOperator: true}, false},
		{argoWindowObject{Applications: []string{"web-*"}, Namespaces: []string{"web"}, AndOperator: true}, true},
		{argoWindowObject{}, false},
	} {
		if got := argoWindowMatches(c.w, app); got != c.want {
			t.Fatalf("%+v: got %v", c.w, got)
		}
	}
}

func TestMapArgoProjectsWindows(t *testing.T) {
	s := argoFixture(t)

	infra := s.Projects[1]
	if infra.Name != "infra" || infra.ManagedBy != "apps" || infra.ManagedServerSide || len(infra.Windows) != 1 {
		t.Fatalf("project %+v", infra)
	}

	w := infra.Windows[0]
	if w.Active || w.Ichor != nil || w.Apps != 1 || w.Error != "" ||
		w.Start != time.Date(2026, 10, 5, 22, 0, 0, 0, time.UTC).UnixMilli() || w.End != time.Date(2026, 10, 6, 6, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("window %+v", w)
	}

	if a := argoAppByName(t, s, "cilium"); a.Freeze != nil {
		t.Fatalf("frozen outside the window: %+v", a.Freeze)
	}

	// At night the Git window freezes the project's app; manual syncs are blocked too.
	night := mapArgoCD(readArgoFixture[argoObject](t, "applications.json"), nil, readArgoFixture[argoProjectObject](t, "appprojects.json"), argoTestNow.Add(9*time.Hour))

	f := argoAppByName(t, night, "cilium").Freeze
	if f == nil || f.Project != "infra" || f.ManualSync || f.ByIchor || f.Until != time.Date(2026, 10, 6, 6, 0, 0, 0, time.UTC).UnixMilli() || len(f.Windows) != 1 {
		t.Fatalf("freeze %+v", f)
	}

	if a := argoAppByName(t, night, "web"); a.Freeze != nil {
		t.Fatalf("other project frozen: %+v", a.Freeze)
	}
}

func TestMapArgoProjectsManagedServerSide(t *testing.T) {
	var p argoProjectObject
	p.Metadata.Namespace, p.Metadata.Name = "argocd", "team"
	p.Metadata.Labels = map[string]string{argoInstanceLabel: "projects"}

	apps := []argoApp{{Namespace: "argocd", Name: "projects", SyncOptions: []string{"ServerSideApply=true"}}}

	got := mapArgoProjects([]argoProjectObject{p}, apps, argoTestNow)
	if got[0].ManagedBy != "projects" || !got[0].ManagedServerSide || got[0].Windows == nil {
		t.Fatalf("project %+v", got[0])
	}
}

func TestArgoFreezeLifecycle(t *testing.T) {
	p := argoProjectFixture(t, "infra")
	apps := []argoApp{
		{Namespace: "argocd", Name: "apps"},
		{Name: "cilium", Project: "infra", Destination: argoDest{Namespace: "kube-system"}},
		{Name: "dns", Project: "infra", Destination: argoDest{Namespace: "dns"}},
	}

	// Freeze kube-system for 60 minutes at 14:30:20: from 14:30 for 61 minutes.
	patch, err := argoFreezePatch(p, argoFreezeActionFreeze, argoFreezeOptions{Namespaces: []string{"kube-system"}, Minutes: 60, ManualSync: true, Reason: "hotfix"}, argoTestNow)
	if err != nil {
		t.Fatal(err)
	}

	if meta := patch["metadata"].(map[string]any); meta["resourceVersion"] != "700" {
		t.Fatalf("metadata %+v", meta)
	}

	p = applyArgoPatch(t, p, patch)

	if len(p.Spec.SyncWindows) != 2 || !strings.Contains(string(p.Spec.SyncWindows[0]), `"description":"nightly backups"`) {
		t.Fatalf("windows %s", p.Spec.SyncWindows)
	}

	var added argoWindowObject
	_ = json.Unmarshal(p.Spec.SyncWindows[1], &added)

	if added.Kind != "deny" || added.Schedule != "30 14 5 10 *" || added.Duration != "61m" || added.TimeZone != "UTC" || !added.ManualSync || added.Namespaces[0] != "kube-system" {
		t.Fatalf("window %+v", added)
	}

	projects := mapArgoProjects([]argoProjectObject{p}, apps, argoTestNow)
	w := projects[0].Windows[1]
	expires := time.Date(2026, 10, 5, 15, 31, 0, 0, time.UTC).UnixMilli()

	if !w.Active || w.Apps != 1 || w.Ichor == nil || w.Ichor.Reason != "hotfix" || w.Ichor.ExpiresAt != expires || w.End != expires || w.Ichor.Expired {
		t.Fatalf("freeze window %+v %+v", w, w.Ichor)
	}

	if f := apps[1].Freeze; f == nil || !f.ByIchor || !f.ManualSync || f.Until != expires || apps[2].Freeze != nil {
		t.Fatalf("app freezes %+v %+v", apps[1].Freeze, apps[2].Freeze)
	}

	// The same freeze asked again within the minute changes nothing.
	if again, err := argoFreezePatch(p, argoFreezeActionFreeze, argoFreezeOptions{Namespaces: []string{"kube-system"}, Minutes: 60, ManualSync: true}, argoTestNow.Add(10*time.Second)); err != nil || again != nil {
		t.Fatalf("again %v %v", again, err)
	}

	// Extend by an hour: the window and its record follow.
	patch, err = argoFreezePatch(p, argoFreezeActionExtend, argoFreezeOptions{Window: w.ID, Minutes: 60}, argoTestNow.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	p = applyArgoPatch(t, p, patch)
	w = mapArgoProjects([]argoProjectObject{p}, nil, argoTestNow)[0].Windows[1]

	if w.Duration != "121m" || w.Ichor == nil || w.Ichor.ExpiresAt != time.Date(2026, 10, 5, 16, 31, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("extended %+v %+v", w, w.Ichor)
	}

	// Ending it removes the window and the annotation; the Git window stays.
	patch, err = argoFreezePatch(p, argoFreezeActionUnfreeze, argoFreezeOptions{Window: w.ID}, argoTestNow.Add(20*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	p = applyArgoPatch(t, p, patch)

	if len(p.Spec.SyncWindows) != 1 || p.Metadata.Annotations[argoFreezesAnnotation] != "" {
		t.Fatalf("after unfreeze %s %v", p.Spec.SyncWindows, p.Metadata.Annotations)
	}

	if _, err := argoFreezePatch(p, argoFreezeActionUnfreeze, argoFreezeOptions{Window: w.ID}, argoTestNow); !errors.Is(err, errArgoWindowGone) {
		t.Fatalf("gone: %v", err)
	}
}

func TestArgoFreezeGitWindows(t *testing.T) {
	p := argoProjectFixture(t, "infra")
	entries, err := argoWindowEntries(p, argoTestNow)
	if err != nil {
		t.Fatal(err)
	}

	id := entries[0].id

	if _, err := argoFreezePatch(p, argoFreezeActionExtend, argoFreezeOptions{Window: id, Minutes: 60}, argoTestNow); !errors.Is(err, errArgoNotIchor) {
		t.Fatalf("extend: %v", err)
	}

	if _, err := argoFreezePatch(p, argoFreezeActionUnfreeze, argoFreezeOptions{Window: id}, argoTestNow); !errors.Is(err, errArgoFromGit) {
		t.Fatalf("unfreeze: %v", err)
	}

	patch, err := argoFreezePatch(p, argoFreezeActionUnfreeze, argoFreezeOptions{Window: id, FromGit: true}, argoTestNow)
	if err != nil {
		t.Fatal(err)
	}

	if spec := patch["spec"].(map[string]any); spec["syncWindows"] != nil {
		t.Fatalf("spec %+v", spec)
	}
}

func TestArgoFreezeExpired(t *testing.T) {
	p := argoProjectFixture(t, "infra")

	patch, err := argoFreezePatch(p, argoFreezeActionFreeze, argoFreezeOptions{Applications: []string{"cilium"}, Minutes: 30}, argoTestNow)
	if err != nil {
		t.Fatal(err)
	}

	p = applyArgoPatch(t, p, patch)
	later := argoTestNow.Add(2 * time.Hour)

	w := mapArgoProjects([]argoProjectObject{p}, nil, later)[0].Windows[1]
	if w.Active || w.Ichor == nil || !w.Ichor.Expired {
		t.Fatalf("expired window %+v %+v", w, w.Ichor)
	}

	if _, err := argoFreezePatch(p, argoFreezeActionExtend, argoFreezeOptions{Window: w.ID, Minutes: 30}, later); !errors.Is(err, errArgoWindowGone) {
		t.Fatalf("extend expired: %v", err)
	}

	// Clearing drops it; the next change to the project does too.
	for _, action := range []string{argoFreezeActionClearExpired, argoFreezeActionFreeze} {
		patch, err := argoFreezePatch(p, action, argoFreezeOptions{Applications: []string{"*"}, Minutes: 15}, later)
		if err != nil {
			t.Fatal(err)
		}

		after := applyArgoPatch(t, p, patch)
		for _, raw := range after.Spec.SyncWindows {
			if strings.Contains(string(raw), "cilium") {
				t.Fatalf("%s kept the expired freeze: %s", action, after.Spec.SyncWindows)
			}
		}
	}

	if patch, err := argoFreezePatch(argoProjectFixture(t, "infra"), argoFreezeActionClearExpired, argoFreezeOptions{}, later); patch != nil || err != nil {
		t.Fatalf("nothing to clear: %v %v", patch, err)
	}
}

func TestArgoFreezeValidates(t *testing.T) {
	p := argoProjectFixture(t, "default")

	for _, opts := range []argoFreezeOptions{
		{Minutes: 60},
		{Applications: []string{"a"}, Namespaces: []string{"b"}, Minutes: 60},
		{Applications: []string{"Bad_Name"}, Minutes: 60},
		{Applications: []string{"a"}, Minutes: 1},
		{Applications: []string{"a"}, Minutes: 8 * 24 * 60},
		{Applications: []string{"a"}, Minutes: 60, Reason: strings.Repeat("x", 201)},
	} {
		if _, err := argoFreezePatch(p, argoFreezeActionFreeze, opts, argoTestNow); err == nil {
			t.Fatalf("accepted %+v", opts)
		}
	}

	if err := KubeArgoFreeze("", "", "", "argocd", "infra", "delete", ""); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("got %v", err)
	}

	if err := KubeArgoFreeze("", "", "", "argocd", "infra", argoFreezeActionFreeze, "{"); err == nil || !strings.Contains(err.Error(), "bad freeze options") {
		t.Fatalf("got %v", err)
	}
}

func TestArgoFreezeActionPatchesProject(t *testing.T) {
	const path = "/apis/argoproj.io/v1alpha1/namespaces/argocd/appprojects/infra"

	project, _ := json.Marshal(argoProjectFixture(t, "infra"))

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis":     `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}}]}`,
		"GET " + path:   string(project),
		"PATCH " + path: `{}`,
		"GET /apis/argoproj.io/v1alpha1/applications": `{"items":[{"metadata":{"name":"cilium","namespace":"argocd"},"spec":{"project":"infra","destination":{"namespace":"kube-system"}}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	opts := argoFreezeOptions{Applications: []string{"*"}, Minutes: 60}
	if err := argoFreezeAction(context.Background(), k, "argocd", "infra", argoFreezeActionFreeze, opts, argoTestNow); err != nil {
		t.Fatal(err)
	}

	reqs := f.recorded()
	last := reqs[len(reqs)-1]

	if last.method != "PATCH" || last.contentType != "application/merge-patch+json" || !strings.Contains(last.body, `"resourceVersion":"700"`) || !strings.Contains(last.body, argoFreezesAnnotation) {
		t.Fatalf("patch %+v", last)
	}

	// Timed by the API server's clock (its Date header), not the caller's.
	if strings.Contains(last.body, `30 14 5 10 *`) {
		t.Fatalf("used the phone's clock: %s", last.body)
	}

	// A name no app of the project has is refused: Argo CD would freeze nothing.
	opts = argoFreezeOptions{Namespaces: []string{"redacted-apps"}, Minutes: 60}
	if err := argoFreezeAction(context.Background(), k, "argocd", "infra", argoFreezeActionFreeze, opts, argoTestNow); err == nil || !strings.Contains(err.Error(), "no app of project infra") {
		t.Fatalf("got %v", err)
	}
}

func TestKubeArgoFreezeDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeArgoCD(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var s argoStatus
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatal(err)
	}

	// The demo freezes the demo namespace and keeps an expired freeze and a Git window.
	var frozen, expired, git bool
	for _, a := range s.Apps {
		frozen = frozen || a.Name == "demo-worker" && a.Freeze != nil && a.Freeze.ByIchor
	}

	for _, p := range s.Projects {
		for _, w := range p.Windows {
			expired = expired || w.Ichor != nil && w.Ichor.Expired
			git = git || w.Ichor == nil && w.Kind == "deny"
		}
	}

	if !frozen || !expired || !git {
		t.Fatalf("demo: frozen %v expired %v git %v", frozen, expired, git)
	}

	if err := KubeArgoFreeze(cfg, "", "", "argocd", "apps", argoFreezeActionClearExpired, ""); !errors.Is(err, demoUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestArgoGlobMatch(t *testing.T) {
	for _, c := range []struct {
		pattern, s string
		want       bool
	}{
		{"web-[0-9]", "web-1", true},
		{"web-[0-9]", "web-a", false},
		{"[!x]eb", "web", true},
		{"[!w]eb", "web", false},
		{"a,b", "a,b", true}, // a comma outside braces is literal
		{"a,b", "abc", false},
		{"{a,b}x", "bx", true},
		{`web\*`, "web*", true},
		{`web\*`, "web-1", false},
		{"web.api", "webxapi", false},
		{"[unclosed", "[unclosed", true},
	} {
		if got := argoGlobMatch(c.pattern, c.s); got != c.want {
			t.Fatalf("%q ~ %q: got %v", c.pattern, c.s, got)
		}
	}

	// Argo CD matches "*" against an empty destination namespace (cluster-scoped apps).
	if !argoWindowMatches(argoWindowObject{Namespaces: []string{"*"}}, argoApp{Name: "crds"}) {
		t.Fatal("empty namespace not matched by *")
	}
}

func TestArgoFreezeEdges(t *testing.T) {
	p := argoProjectFixture(t, "default")

	patch, err := argoFreezePatch(p, argoFreezeActionFreeze, argoFreezeOptions{Applications: []string{"web"}, Minutes: 30}, argoTestNow)
	if err != nil {
		t.Fatal(err)
	}

	p = applyArgoPatch(t, p, patch)
	window := func(now time.Time) argoWindow { return mapArgoProjects([]argoProjectObject{p}, nil, now)[0].Windows[0] }
	expires := time.Date(2026, 10, 5, 15, 1, 0, 0, time.UTC)

	// Active right after its start minute, over at expiresAt.
	if w := window(time.Date(2026, 10, 5, 14, 30, 1, 0, time.UTC)); !w.Active {
		t.Fatalf("not active after start: %+v", w)
	}

	if w := window(expires); w.Active || !w.Ichor.Expired {
		t.Fatalf("still active at expiresAt: %+v %+v", w, w.Ichor)
	}

	// A forgotten freeze comes back a year later: shown active, and expired so the app clears it.
	if w := window(time.Date(2027, 10, 5, 14, 40, 0, 0, time.UTC)); !w.Active || !w.Ichor.Expired {
		t.Fatalf("a year later: %+v %+v", w, w.Ichor)
	}

	// Two freezes of the same scope started the same minute: extending one into the other is refused.
	patch, err = argoFreezePatch(p, argoFreezeActionFreeze, argoFreezeOptions{Applications: []string{"web"}, Minutes: 60}, argoTestNow)
	if err != nil {
		t.Fatal(err)
	}

	p = applyArgoPatch(t, p, patch)
	if _, err := argoFreezePatch(p, argoFreezeActionExtend, argoFreezeOptions{Window: window(argoTestNow).ID, Minutes: 30}, argoTestNow); !errors.Is(err, errArgoSameFreeze) {
		t.Fatalf("got %v", err)
	}

	// Windows differing only by a field Ichor does not know have different ids.
	a, b := json.RawMessage(`{"kind":"deny","schedule":"0 1 * * *","duration":"1h","description":"a"}`), json.RawMessage(`{"duration":"1h","description":"b","kind":"deny","schedule":"0 1 * * *"}`)
	if argoWindowID(a) == argoWindowID(b) || argoWindowID(a) != argoWindowID(json.RawMessage(`{"schedule":"0 1 * * *","description":"a","kind":"deny","duration":"1h"}`)) {
		t.Fatal("window ids")
	}

	// An unreadable annotation: no write, it would lose the record of Ichor's freezes.
	p.Metadata.Annotations[argoFreezesAnnotation] = "{"
	if _, err := argoFreezePatch(p, argoFreezeActionClearExpired, argoFreezeOptions{}, argoTestNow); !errors.Is(err, errArgoFreezesAnnotation) {
		t.Fatalf("got %v", err)
	}
}
