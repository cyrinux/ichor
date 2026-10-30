package kube

import "testing"

func TestNewResource(t *testing.T) {
	r, err := NewResource(" apps ", "v1", "deployments")
	if err != nil {
		t.Fatal(err)
	}

	if got := r.Path("web"); got != "/apis/apps/v1/namespaces/web/deployments" {
		t.Errorf("namespaced path %q", got)
	}

	core, err := NewResource("", "v1", "nodes")
	if err != nil {
		t.Fatal(err)
	}

	if got := core.Path(""); got != "/api/v1/nodes" {
		t.Errorf("core path %q", got)
	}

	for _, bad := range [][3]string{{"Apps", "v1", "x"}, {"apps", "1", "x"}, {"apps", "v1", "a/b"}} {
		if _, err := NewResource(bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestScopedPath(t *testing.T) {
	if got := ScopedPath("/api/v1", "", "pods"); got != "/api/v1/pods" {
		t.Errorf("all namespaces %q", got)
	}

	if got := ScopedPath("/apis/apps/v1", "a b", "deployments"); got != "/apis/apps/v1/namespaces/a%20b/deployments" {
		t.Errorf("namespaced %q", got)
	}
}
