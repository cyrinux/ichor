package ichorgo

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// sanitizePodJSON is a pod object with the given phase/reason and one container state.
func sanitizePodJSON(name, phase, reason, state, owner string) string {
	owners := ""
	if owner != "" {
		kind, n, _ := strings.Cut(owner, "/")
		owners = `,"ownerReferences":[{"kind":"` + kind + `","name":"` + n + `"}]`
	}

	statuses := ""
	if state != "" {
		statuses = `,"containerStatuses":[{"name":"app","ready":false,"state":` + state + `}]`
	}

	return `{"metadata":{"name":"` + name + `","namespace":"web","creationTimestamp":"2026-10-01T10:00:00Z"` + owners + `},` +
		`"spec":{"containers":[{"name":"app","image":"nginx"}]},` +
		`"status":{"phase":"` + phase + `","reason":"` + reason + `"` + statuses + `}}`
}

func TestKubeSanitizePreviewSelectsK9sStatuses(t *testing.T) {
	sanitizeNow = func() time.Time { return time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { sanitizeNow = time.Now })

	cases := []struct {
		name, phase, reason, state, owner, want string
	}{
		{"done", "Succeeded", "", `{"terminated":{"reason":"Completed","exitCode":0}}`, "Job/nightly", "Completed"},
		{"waiting", "Pending", "", "", "", "Pending"},
		{"crashing", "Running", "", `{"waiting":{"reason":"CrashLoopBackOff"}}`, "ReplicaSet/api-5d8f", "CrashLoopBackOff"},
		{"failed", "Failed", "", `{"terminated":{"reason":"Error","exitCode":1}}`, "", "Error"},
		{"no-image", "Pending", "", `{"waiting":{"reason":"ImagePullBackOff"}}`, "", "ImagePullBackOff"},
		{"lost", "Failed", "", `{"terminated":{"reason":"ContainerStatusUnknown","exitCode":137}}`, "", "ContainerStatusUnknown"},
		{"evicted", "Failed", "Evicted", "", "ReplicaSet/api-5d8f", "Evicted"},
		{"oom", "Running", "", `{"terminated":{"reason":"OOMKilled","exitCode":137}}`, "", "OOMKilled"},
		{"healthy", "Running", "", `{"running":{}}`, "ReplicaSet/api-5d8f", ""},
		{"init-error", "Pending", "", "", "", ""}, // Init:Error, replaced below: k9s matches whole statuses
	}

	items := make([]string, 0, len(cases))
	for _, c := range cases {
		items = append(items, sanitizePodJSON(c.name, c.phase, c.reason, c.state, c.owner))
	}

	items[len(items)-1] = `{"metadata":{"name":"init-error","namespace":"web"},"spec":{"initContainers":[{"name":"init"}],"containers":[{"name":"app"}]},` +
		`"status":{"phase":"Pending","initContainerStatuses":[{"name":"init","state":{"terminated":{"reason":"Error","exitCode":1}}}]}}`
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/web/pods": `{"kind":"PodList","metadata":{"continue":"next"},"items":[` + strings.Join(items, ",") + `]}`,
	})

	out, err := KubeSanitizePreview(kubeStoreFor(t, f), "admin@test", "", "web")
	if err != nil {
		t.Fatal(err)
	}

	var got sanitizePreview
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	byName := map[string]sanitizePod{}
	for _, p := range got.Pods {
		byName[p.Name] = p
	}

	for _, c := range cases {
		p, ok := byName[c.name]
		if (c.want != "") != ok || p.Status != c.want {
			t.Errorf("%s: got %+v (kept %v), want status %q", c.name, p, ok, c.want)
		}
	}

	if p := byName["crashing"]; p.OwnerKind != "ReplicaSet" || p.OwnerName != "api-5d8f" || p.AgeSec != 3600 || p.Namespace != "web" {
		t.Errorf("crashing = %+v", p)
	}

	if !got.Truncated {
		t.Error("a page with a continue token is not truncated")
	}
}

func TestKubeSanitizeDeletesAndCarriesOn(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	f := newFakeKubeAPI(t, map[string]string{
		"DELETE /api/v1/namespaces/web/pods/done":     `{"kind":"Pod"}`,
		"DELETE /api/v1/namespaces/web/pods/crashing": `{"kind":"Pod"}`,
		// web/gone answers 404: no answer is set.
	})

	out, err := KubeSanitize(kubeStoreFor(t, f), "admin@test", "", "web", "web/done\n\nweb/gone\nweb/crashing\nweb/done\n")
	if err != nil {
		t.Fatal(err)
	}

	var res sanitizeResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	if res.Deleted != 2 || len(res.Failed) != 1 || res.Failed[0].Name != "web/gone" || !strings.Contains(res.Failed[0].Error, "no longer exists") {
		t.Errorf("result %+v", res)
	}

	dels := deleteRequests(f)
	if len(dels) != 3 || dels[0].body != "" {
		t.Errorf("deletes %+v (want 3, default grace period)", dels)
	}

	audit := readAudit(t, "", "")
	if len(audit) != 1 || audit[0].Action != "sanitize-pods" || audit[0].Object != "Pod×3" || audit[0].Namespace != "web" ||
		audit[0].Params != "pods=3" || audit[0].Outcome != auditPartial || audit[0].Error != "1 of 3 pods not deleted" {
		t.Errorf("audit %+v", audit)
	}
}

func TestKubeSanitizeAuditOutcome(t *testing.T) {
	cases := map[string]struct {
		names, outcome, auditErr string
		wantErr                  bool
	}{
		"one pod gone":  {names: "web/done\nweb/gone", outcome: auditPartial, auditErr: "1 of 2 pods not deleted"},
		"all deleted":   {names: "web/done\nweb/crashing", outcome: auditOK},
		"refused parse": {names: "web/done\ndone", outcome: auditFailed, wantErr: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withDataDir(t)
			withAuditClock(t, time.Now())

			f := newFakeKubeAPI(t, map[string]string{
				"DELETE /api/v1/namespaces/web/pods/done":     `{"kind":"Pod"}`,
				"DELETE /api/v1/namespaces/web/pods/crashing": `{"kind":"Pod"}`,
			})

			out, err := KubeSanitize(kubeStoreFor(t, f), "admin@test", "", "web", tc.names)
			if (err != nil) != tc.wantErr || (!tc.wantErr && out == "") {
				t.Fatalf("out %q, err %v", out, err)
			}

			audit := readAudit(t, "", "sanitize-pods")
			if len(audit) != 1 || audit[0].Outcome != tc.outcome || (tc.auditErr != "" && audit[0].Error != tc.auditErr) ||
				(tc.outcome == auditOK && audit[0].Error != "") {
				t.Errorf("audit %+v", audit)
			}
		})
	}
}

func TestKubeSanitizeRefusesBadNames(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	f := newFakeKubeAPI(t, map[string]string{"DELETE /api/v1/namespaces/web/pods/done": `{}`})
	stored := kubeStoreFor(t, f)

	cases := map[string]struct{ namespace, names string }{
		"no slash":         {"web", "web/done\ndone"},
		"empty name":       {"web", "web/"},
		"bad name":         {"web", "web/Not_A_Pod"},
		"other namespace":  {"web", "web/done\nkube-system/coredns-1"},
		"nothing":          {"web", "\n \n"},
		"bad scope":        {"Bad NS", "web/done"},
		"name with a path": {"", "web/done/../x"},
	}

	for name, c := range cases {
		if _, err := KubeSanitize(stored, "admin@test", "", c.namespace, c.names); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	if dels := deleteRequests(f); len(dels) != 0 {
		t.Errorf("a refused sweep deleted %+v", dels)
	}

	// Every namespace: any namespace goes.
	if _, err := KubeSanitize(stored, "admin@test", "", "", "web/done"); err != nil {
		t.Errorf("all namespaces: %v", err)
	}
}

func TestKubeSanitizeDemo(t *testing.T) {
	noKubeDial(t)

	yaml := demoKubeconfigForTest(t)

	out, err := KubeSanitizePreview(yaml, "", "", "demo")
	if err != nil {
		t.Fatal(err)
	}

	var got sanitizePreview
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got.Pods) == 0 {
		t.Fatalf("demo preview %s: %v", out, err)
	}

	for _, p := range got.Pods {
		if !sanitizeStatuses[p.Status] || p.Namespace != "demo" {
			t.Errorf("demo row %+v", p)
		}
	}

	if out, _ := KubeSanitizePreview(yaml, "", "", "kube-system"); !strings.Contains(out, `"pods":[]`) {
		t.Errorf("kube-system demo preview %s", out)
	}

	if _, err := KubeSanitize(yaml, "", "", "demo", "demo/worker-6f4b8-uvwxy"); !errors.Is(err, errDemoUnavailable) {
		t.Errorf("demo sanitize = %v", err)
	}
}

func TestSanitizePodsAccessNeeds(t *testing.T) {
	var got []string
	for _, n := range kubeActionChecks["sanitizePods"] {
		if !n.namespaced {
			t.Errorf("%+v is not namespaced", n)
		}

		got = append(got, n.attrs.Verb+" "+n.attrs.Resource)
	}

	if strings.Join(got, ",") != "delete pods,list pods" {
		t.Errorf("sanitizePods needs %v", got)
	}
}
