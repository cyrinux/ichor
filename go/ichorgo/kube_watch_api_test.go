package ichorgo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type watchRecord struct{ eventType, json string }

// watchRecorder is a KubeWatchListener and a KubeLiveListener for tests.
type watchRecorder struct {
	events chan watchRecord
	done   chan string
}

func newWatchRecorder() *watchRecorder {
	return &watchRecorder{events: make(chan watchRecord, 16), done: make(chan string, 1)}
}

func (r *watchRecorder) OnEvent(eventType, json string) { r.events <- watchRecord{eventType, json} }
func (r *watchRecorder) OnUpdate(json string)           { r.events <- watchRecord{"UPDATE", json} }
func (r *watchRecorder) OnDone(errMessage string)       { r.done <- errMessage }

func (r *watchRecorder) next(t *testing.T) watchRecord {
	t.Helper()

	select {
	case ev := <-r.events:
		return ev
	case msg := <-r.done:
		t.Fatalf("ended early: %q", msg)
	case <-time.After(10 * time.Second):
		t.Fatal("no event")
	}

	return watchRecord{}
}

func (r *watchRecorder) ended(t *testing.T) string {
	t.Helper()

	select {
	case msg := <-r.done:
		return msg
	case <-time.After(10 * time.Second):
		t.Fatal("never ended")
	}

	return ""
}

func TestStartKubeWorkloadPodsWatch(t *testing.T) {
	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		switch {
		case r.URL.Path == "/apis/apps/v1/namespaces/shop/deployments/web":
			_, _ = io.WriteString(w, `{"metadata":{"name":"web","namespace":"shop"},"spec":{"selector":{"matchLabels":{"app":"web"}}}}`)
		case r.URL.Path != "/api/v1/namespaces/shop/pods" || q.Get("labelSelector") != "app=web" || q.Get("fieldSelector") != "status.phase=Running":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"kind":"Status","message":"unexpected `+r.URL.String()+`"}`)
		case q.Get("watch") == "":
			_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"5"},"items":[`+jsonPod("shop", "web-1", "nginx")+`]}`)
		default:
			streamEvents(w, `{"type":"ADDED","object":`+podWithVersion("web-2", "6")+`}`)
			holdOpen(w, r)
		}
	})

	rec := newWatchRecorder()
	run := StartKubeWorkloadPodsWatch(kubeStoreFor(t, f), "admin@test", "", "Deployment", "shop", "web", "Running", rec)

	first := rec.next(t)

	var page kubePodPage
	if err := json.Unmarshal([]byte(first.json), &page); first.eventType != watchSync || err != nil || len(page.Pods) != 1 || page.Pods[0].Name != "web-1" || !page.Complete {
		t.Fatalf("first event %+v (%v)", first, err)
	}

	second := rec.next(t)

	var pod kubePod
	if err := json.Unmarshal([]byte(second.json), &pod); second.eventType != watchAdded || err != nil || pod.Name != "web-2" || pod.Status != "Running" {
		t.Fatalf("second event %+v (%v)", second, err)
	}

	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("cancelled with %q", msg)
	}
}

func TestStartKubeNodePodsWatch(t *testing.T) {
	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		switch {
		case r.URL.Path != "/api/v1/pods" || q.Get("fieldSelector") != "spec.nodeName=worker-1,status.phase=Running":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"kind":"Status","message":"unexpected `+r.URL.String()+`"}`)
		case q.Get("watch") == "":
			_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"5"},"items":[`+jsonPod("shop", "web-1", "nginx")+`]}`)
		case q.Get("resourceVersion") != "5":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"kind":"Status","message":"watch from `+q.Get("resourceVersion")+`"}`)
		default:
			streamEvents(w, `{"type":"DELETED","object":`+podWithVersion("web-1", "6")+`}`)
			holdOpen(w, r)
		}
	})

	rec := newWatchRecorder()
	run := StartKubeNodePodsWatch(kubeStoreFor(t, f), "admin@test", "", "worker-1", "Running", rec)

	var page kubePodPage
	if first := rec.next(t); first.eventType != watchSync || json.Unmarshal([]byte(first.json), &page) != nil || len(page.Pods) != 1 || page.Pods[0].Name != "web-1" {
		t.Fatalf("first event %+v", first)
	}

	var pod kubePod
	if second := rec.next(t); second.eventType != watchDeleted || json.Unmarshal([]byte(second.json), &pod) != nil || pod.Name != "web-1" {
		t.Fatalf("second event %+v", second)
	}

	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("cancelled with %q", msg)
	}
}

func TestStartKubeNodePodsWatchRefusalsAndDemo(t *testing.T) {
	for _, tc := range []struct{ node, phase, want string }{
		{"../etc", "", "invalid Kubernetes node name"},
		{"", "", "invalid Kubernetes node name"},
		{"worker-1", "Sleeping", "phase"},
	} {
		rec := newWatchRecorder()
		StartKubeNodePodsWatch("", "", "", tc.node, tc.phase, rec)

		if msg := rec.ended(t); !strings.Contains(msg, tc.want) {
			t.Errorf("%q %q ended with %q, want %q", tc.node, tc.phase, msg, tc.want)
		}
	}

	rec := newWatchRecorder()
	run := StartKubeNodePodsWatch(demoKubeconfigForTest(t), "", "", "demo-worker-1", "", rec)

	var page kubePodPage
	if ev := rec.next(t); ev.eventType != watchSync || json.Unmarshal([]byte(ev.json), &page) != nil || len(page.Pods) == 0 {
		t.Fatalf("demo pods %+v", ev)
	}

	for _, p := range page.Pods {
		if p.Node != "demo-worker-1" {
			t.Errorf("demo pod %s on %s", p.Name, p.Node)
		}
	}

	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("demo ended with %q", msg)
	}
}

func TestStartKubeWatchValidatesAndDemo(t *testing.T) {
	rec := newWatchRecorder()
	StartKubeWatch("", "", "", "", "v1", "Pods!", "", "", "", rec)

	if msg := rec.ended(t); !strings.Contains(msg, "invalid resource") {
		t.Errorf("bad resource ended with %q", msg)
	}

	rec = newWatchRecorder()
	StartKubeWorkloadPodsWatch("", "", "", "CronJob", "shop", "web", "", rec)

	if msg := rec.ended(t); msg == "" {
		t.Error("a CronJob's pods were watched")
	}

	rec = newWatchRecorder()
	run := StartKubeWatch(demoKubeconfigForTest(t), "", "", "", "v1", "pods", "demo", "", "", rec)

	var page kubePodPage
	if ev := rec.next(t); ev.eventType != watchSync || json.Unmarshal([]byte(ev.json), &page) != nil || len(page.Pods) == 0 {
		t.Errorf("demo pods %+v", ev)
	}

	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("demo ended with %q", msg)
	}

	rec = newWatchRecorder()
	live := StartKubeObjectWatch(demoKubeconfigForTest(t), "", "", "apps", "v1", "deployments", "demo", "hello-ichor", rec)

	var summary kubeObjectSummary
	if ev := rec.next(t); ev.eventType != "UPDATE" || json.Unmarshal([]byte(ev.json), &summary) != nil || summary.Kind != "Deployment" {
		t.Errorf("demo summary %+v", ev)
	}

	live.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("demo summary ended with %q", msg)
	}
}

// The rollout is read once the lists are watched, then again when a pod changes.
func TestStartKubeRolloutWatchReadsAgainOnChange(t *testing.T) {
	var ready atomic.Bool

	release := make(chan struct{})

	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		switch {
		case r.URL.Path == "/apis/apps/v1/namespaces/shop/statefulsets/db":
			_, _ = io.WriteString(w, `{"metadata":{"name":"db","namespace":"shop","generation":2},
				"spec":{"replicas":1,"selector":{"matchLabels":{"app":"db"}}},
				"status":{"observedGeneration":2,"replicas":1,"readyReplicas":0,"updatedReplicas":1,"updateRevision":"db-2"}}`)
		case r.URL.Path == "/apis/apps/v1/namespaces/shop/statefulsets" && q.Get("watch") == "":
			_, _ = io.WriteString(w, `{"kind":"Table","metadata":{"resourceVersion":"1"},"columnDefinitions":[{"name":"Name"}],"rows":[{"cells":["db"],"object":{"metadata":{"name":"db","namespace":"shop"}}}]}`)
		case r.URL.Path == "/apis/apps/v1/namespaces/shop/statefulsets":
			holdOpen(w, r)
		case r.URL.Path != "/api/v1/namespaces/shop/pods" || q.Get("labelSelector") != "app=db":
			w.WriteHeader(http.StatusBadRequest)
		case q.Get("watch") == "":
			_, _ = io.WriteString(w, `{"metadata":{"resourceVersion":"7"},"items":[`+fakeRolloutPod("db-0", "StatefulSet", "db", "controller-revision-hash", "db-2", "2026-10-04T10:00:00Z", ready.Load())+`]}`)
		default:
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}

			ready.Store(true)
			streamEvents(w, `{"type":"MODIFIED","object":`+fakeRolloutPod("db-0", "StatefulSet", "db", "controller-revision-hash", "db-2", "2026-10-04T10:00:00Z", true)+`}`)
			holdOpen(w, r)
		}
	})

	rec := newWatchRecorder()
	run := StartKubeRolloutWatch(kubeStoreFor(t, f), "admin@test", "", "StatefulSet", "shop", "db", rec)

	var first kubeRolloutStatus
	if ev := rec.next(t); json.Unmarshal([]byte(ev.json), &first) != nil || len(first.Pods) != 1 || first.Pods[0].Healthy {
		t.Fatalf("first status %+v", ev)
	}

	close(release)

	var second kubeRolloutStatus
	if ev := rec.next(t); json.Unmarshal([]byte(ev.json), &second) != nil || len(second.Pods) != 1 || !second.Pods[0].Healthy || !second.Pods[0].Updated {
		t.Fatalf("second status %+v", ev)
	}

	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("cancelled with %q", msg)
	}
}

// The object's own watch being refused ends the view; its events' being refused does not.
func TestStartKubeObjectWatchEventsOptional(t *testing.T) {
	f := newWatchKubeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/namespaces/shop/events"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"Forbidden","message":"events is forbidden"}`)
		case r.URL.Path == "/api/v1/namespaces/shop/configmaps/settings":
			_, _ = io.WriteString(w, `{"kind":"ConfigMap","apiVersion":"v1","metadata":{"name":"settings","namespace":"shop"},"data":{"a":"1"}}`)
		case r.URL.Path == "/api/v1/namespaces/shop/configmaps" && q.Get("watch") == "":
			_, _ = io.WriteString(w, `{"kind":"Table","metadata":{"resourceVersion":"1"},"columnDefinitions":[{"name":"Name"}],"rows":[{"cells":["settings"],"object":{"metadata":{"name":"settings","namespace":"shop"}}}]}`)
		case r.URL.Path == "/api/v1/namespaces/shop/configmaps":
			holdOpen(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"kind":"Status","reason":"NotFound","message":"`+r.URL.Path+` not found"}`)
		}
	})

	rec := newWatchRecorder()
	run := StartKubeObjectWatch(kubeStoreFor(t, f), "admin@test", "", "", "v1", "configmaps", "shop", "settings", rec)

	var summary kubeObjectSummary
	if ev := rec.next(t); json.Unmarshal([]byte(ev.json), &summary) != nil || summary.Kind != "ConfigMap" || !strings.Contains(summary.EventsError, "forbidden") {
		t.Fatalf("summary %+v", ev)
	}

	run.Cancel()

	if msg := rec.ended(t); msg != "" {
		t.Errorf("cancelled with %q", msg)
	}

	rec = newWatchRecorder()
	StartKubeObjectWatch(kubeStoreFor(t, f), "admin@test", "", "", "v1", "secrets", "shop", "missing", rec)

	if msg := rec.ended(t); !strings.Contains(msg, "not found") {
		t.Errorf("a missing object ended with %q", msg)
	}
}
