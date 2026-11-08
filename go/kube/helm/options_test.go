package helm

import (
	"reflect"
	"testing"
)

func TestHelmReplaceBodyKeepsImmutableFields(t *testing.T) {
	svc := object{Kind: "Service", Body: map[string]any{
		"metadata": map[string]any{"name": "site"},
		"spec":     map[string]any{"ports": []any{map[string]any{"port": 80.0}}},
	}}
	live := map[string]any{
		"metadata": map[string]any{"name": "site", "resourceVersion": "42"},
		"spec": map[string]any{"clusterIP": "10.96.0.10", "clusterIPs": []any{"10.96.0.10"}, "ipFamilies": []any{"IPv4"},
			"ipFamilyPolicy": "SingleStack", "healthCheckNodePort": 31000.0, "sessionAffinity": "None"},
	}

	body := replaceBody(svc, live)
	spec := body["spec"].(map[string]any)

	if body["metadata"].(map[string]any)["resourceVersion"] != "42" || spec["clusterIP"] != "10.96.0.10" || spec["ipFamilyPolicy"] != "SingleStack" ||
		spec["healthCheckNodePort"] != 31000.0 || spec["clusterIPs"] == nil || spec["ipFamilies"] == nil || spec["sessionAffinity"] != nil {
		t.Fatalf("body %v", body)
	}

	// The rendering itself is left as it is.
	if _, ok := svc.Body["spec"].(map[string]any)["clusterIP"]; ok || svc.Body["metadata"].(map[string]any)["resourceVersion"] != nil {
		t.Fatalf("rendering changed: %v", svc.Body)
	}

	pvc := object{Kind: "PersistentVolumeClaim", Body: map[string]any{"spec": map[string]any{"storageClassName": "fast"}}}
	body = replaceBody(pvc, map[string]any{"spec": map[string]any{"volumeName": "pv-1", "storageClassName": "slow", "resources": map[string]any{}}})

	if spec := body["spec"].(map[string]any); spec["volumeName"] != "pv-1" || spec["storageClassName"] != "fast" || spec["resources"] != nil {
		t.Fatalf("pvc %v", body)
	}

	cm := object{Kind: "ConfigMap", Body: map[string]any{"data": map[string]any{"a": "1"}}}
	if body := replaceBody(cm, map[string]any{"metadata": map[string]any{}, "spec": map[string]any{"clusterIP": "x"}}); body["spec"] != nil {
		t.Fatalf("configmap %v", body)
	}
}

func TestHelmRollbackOptions(t *testing.T) {
	for in, want := range map[string]Options{
		"":                                 {},
		"  ":                               {},
		`{}`:                               {},
		`{"force":true,"historyMax":10}`:   {Force: true, HistoryMax: 10},
		`{"historyMax":0,"other":"value"}`: {},
	} {
		if got, err := ParseRollbackOptions(in); err != nil || got != want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}

	for _, in := range []string{`{"historyMax":-1}`, `not json`, `{"force":"yes"}`} {
		if _, err := ParseRollbackOptions(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}

	if got := (Options{Force: true, HistoryMax: 2}).AuditParams(3); got != "revision=3 force=true historyMax=2" {
		t.Errorf("audit %q", got)
	}

	refs := []SecretRef{{Revision: 5}, {Revision: 4}, {Revision: 2}}
	for keep, want := range map[int][]int{0: {}, 1: {2, 4, 5}, 2: {2, 4}, 3: {2}, 4: {}, 10: {}} {
		if got := revisions(pruneRefs(refs, keep)); !reflect.DeepEqual(got, want) {
			t.Errorf("keep %d: %v, want %v", keep, got, want)
		}
	}
}
