package helm

import (
	"reflect"
	"testing"
)

func TestThreeWayMergePatch(t *testing.T) {
	original := map[string]any{"a": "1", "gone": "x", "m": map[string]any{"k": "v", "old": "o"}}
	modified := map[string]any{"a": "2", "m": map[string]any{"k": "v"}, "new": []any{"x"}}
	live := map[string]any{"a": "1", "gone": "x", "m": map[string]any{"k": "drift", "old": "o", "server": "s"}, "status": map[string]any{}}

	want := map[string]any{"a": "2", "gone": nil, "m": map[string]any{"k": "v", "old": nil}, "new": []any{"x"}}
	if got := threeWayMergePatch(original, modified, live); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}

	if got := threeWayMergePatch(modified, modified, map[string]any{"a": "2", "m": map[string]any{"k": "v", "x": 1}, "new": []any{"x"}}); len(got) != 0 {
		t.Fatalf("no-op patch %v", got)
	}
}

func TestLiveMatchesIgnoresServerDefaults(t *testing.T) {
	want := map[string]any{"spec": map[string]any{"ports": []any{map[string]any{"port": 80.0}}}}
	live := map[string]any{"spec": map[string]any{"ports": []any{map[string]any{"port": 80.0, "nodePort": 31000.0, "protocol": "TCP"}}, "clusterIP": "10.0.0.1"}}

	if p := threeWayMergePatch(want, want, live); len(p) != 0 {
		t.Fatalf("defaults made a patch: %v", p)
	}

	// A field the current manifest set and the target drops, still live: the list is replaced.
	was := map[string]any{"spec": map[string]any{"ports": []any{map[string]any{"port": 80.0, "name": "http"}}}}
	live["spec"].(map[string]any)["ports"] = []any{map[string]any{"port": 80.0, "name": "http", "nodePort": 31000.0}}

	if p := threeWayMergePatch(was, want, live); len(p) == 0 {
		t.Fatal("a dropped field was kept")
	}
}

func TestParseHelmManifestReadsAsHelm(t *testing.T) {
	objs, err := parseManifest("---\n# Source: x\nkind: A\nmetadata: {name: a}\nspec: {enabled: yes, date: 2024-01-01}\n---\n# only a comment\n---\nkind: B\nkind: C\n")
	if err != nil || len(objs) != 2 {
		t.Fatalf("%+v %v", objs, err)
	}

	if spec := objs[0]["spec"].(map[string]any); spec["date"] != "2024-01-01" || spec["enabled"] != true || objs[1]["kind"] != "C" {
		t.Fatalf("got %+v", objs)
	}
}
