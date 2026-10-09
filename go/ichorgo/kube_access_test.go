package ichorgo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAccessAPI answers SelfSubjectAccessReviews with allowed(attributes), and
// SelfSubjectReviews with user when it is set (404 otherwise, as before Kubernetes 1.28).
func fakeAccessAPI(t *testing.T, allowed func(ssarAttributes) bool, user string) (*kubeClient, *atomic.Int32) {
	t.Helper()

	var reviews atomic.Int32

	f := &fakeKubeAPI{Server: httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		switch r.URL.Path {
		case "/version":
			_, _ = io.WriteString(w, `{"gitVersion":"v1.34.0"}`)
		case ssarPath:
			reviews.Add(1)

			var review ssarRequest
			if err := json.Unmarshal(body, &review); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)

				return
			}

			ok := allowed(review.Spec.ResourceAttributes)
			reason := ""
			if !ok {
				reason = "RBAC: no rule"
			}

			_ = json.NewEncoder(w).Encode(map[string]any{"status": map[string]any{"allowed": ok, "reason": reason}})
		case selfSubjectReviewPath:
			if user == "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"kind":"Status","reason":"NotFound"}`)

				return
			}

			_, _ = io.WriteString(w, `{"status":{"userInfo":{"username":"`+user+`","groups":["dev","system:authenticated"]}}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))}
	t.Cleanup(f.Close)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return k, &reviews
}

func TestActionAccessFollowsRBAC(t *testing.T) {
	// May read and restart, not scale nor delete pods.
	k, _ := fakeAccessAPI(t, func(a ssarAttributes) bool {
		return !(a.Subresource == "scale" || (a.Resource == "pods" && a.Verb == "delete"))
	}, "")

	access := readActionAccess(context.Background(), k, "web")

	if !access.Actions["restartWorkload"].Allowed {
		t.Error("restartWorkload denied")
	}

	scale := access.Actions["scale"]
	if scale.Allowed || scale.Verb != "patch" || scale.Resource != "deployments/scale" || scale.Namespace != "web" || scale.Reason != "RBAC: no rule" {
		t.Errorf("scale = %+v", scale)
	}

	if access.Actions["deletePod"].Allowed {
		t.Error("deletePod allowed")
	}

	for _, name := range []string{"execPod", "suspendCronJob", "triggerCronJob", "helmRollback", "argoSync", "fluxReconcile", "cordonNode", "drainNode"} {
		if _, ok := access.Actions[name]; !ok {
			t.Errorf("%s not checked", name)
		}
	}
}

func TestDrainNeedsCordonAndEvictionEverywhere(t *testing.T) {
	// May cordon, not evict outside its namespace.
	k, _ := fakeAccessAPI(t, func(a ssarAttributes) bool {
		return a.Subresource != "eviction" || a.Namespace == "web"
	}, "")

	access := readActionAccess(context.Background(), k, "web")

	if !access.Actions["cordonNode"].Allowed {
		t.Error("cordonNode denied")
	}

	drain := access.Actions["drainNode"]
	if drain.Allowed || drain.Resource != "pods/eviction" || drain.Namespace != "" {
		t.Errorf("drainNode = %+v; want denied on pods/eviction in every namespace", drain)
	}
}

func TestActionAccessFailedReviewDoesNotBlock(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{}) // every review: 404

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	scale := readActionAccess(context.Background(), k, "web").Actions["scale"]
	if !scale.Allowed || !scale.Unknown {
		t.Errorf("scale = %+v; want allowed and unknown", scale)
	}
}

func TestActionAccessIsCached(t *testing.T) {
	k, reviews := fakeAccessAPI(t, func(ssarAttributes) bool { return true }, "")
	cache := newAccessCache(time.Minute)
	read := func(ns string) {
		_, err := cache.get("cluster\x00"+ns, func() (kubeActionAccess, error) {
			return readActionAccess(context.Background(), k, ns), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	read("web")
	first := reviews.Load()
	read("web")

	if reviews.Load() != first {
		t.Errorf("second read asked again: %d then %d reviews", first, reviews.Load())
	}

	read("db")

	if reviews.Load() == first {
		t.Error("another namespace was served from the cache")
	}
}

func TestAccessCacheExpires(t *testing.T) {
	cache := newAccessCache(time.Minute)
	now := time.Unix(1000, 0)
	cache.now = func() time.Time { return now }
	calls := 0
	read := func() {
		_, _ = cache.get("k", func() (kubeActionAccess, error) {
			calls++

			return kubeActionAccess{}, nil
		})
	}

	read()
	now = now.Add(2 * time.Minute)
	read()

	if calls != 2 {
		t.Errorf("fetched %d times, want 2", calls)
	}
}

func TestKubeCanSingleReview(t *testing.T) {
	k, _ := fakeAccessAPI(t, func(a ssarAttributes) bool {
		return a.Verb == "update" && a.Group == "apps" && a.Resource == "deployments" && a.Name == "api"
	}, "")

	ok, err := canDo(context.Background(), k, ssarAttributes{Verb: "update", Group: "apps", Resource: "deployments", Namespace: "web", Name: "api"})
	if err != nil || !ok.Allowed {
		t.Errorf("update deployments/api = %+v, %v", ok, err)
	}

	no, err := canDo(context.Background(), k, ssarAttributes{Verb: "delete", Group: "apps", Resource: "deployments", Namespace: "web", Name: "api"})
	if err != nil || no.Allowed {
		t.Errorf("delete deployments/api = %+v, %v", no, err)
	}
}

func TestWhoAmI(t *testing.T) {
	k, _ := fakeAccessAPI(t, func(ssarAttributes) bool { return true }, "alice@example.com")

	me := readWhoAmI(context.Background(), k)
	if me.User != "alice@example.com" || strings.Join(me.Groups, ",") != "dev,system:authenticated" {
		t.Errorf("whoami = %+v", me)
	}

	old, _ := fakeAccessAPI(t, func(ssarAttributes) bool { return true }, "")
	if me := readWhoAmI(context.Background(), old); me.User != "" || !me.Unknown {
		t.Errorf("whoami on an old API server = %+v; want unknown", me)
	}
}

func TestActionAccessDemoAllowsAll(t *testing.T) {
	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeActionAccess(demo, "Demo cluster", "", "web")
	if err != nil {
		t.Fatal(err)
	}

	var access kubeActionAccess
	if err := json.Unmarshal([]byte(out), &access); err != nil {
		t.Fatal(err)
	}

	if len(access.Actions) == 0 {
		t.Fatal("no actions")
	}

	for name, a := range access.Actions {
		if !a.Allowed {
			t.Errorf("%s denied in the demo", name)
		}
	}
}
