package ichorgo

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMaskedNamespaceMapsBack(t *testing.T) {
	SetPrivacyMask(true, "alice")
	defer SetPrivacyMask(false, "")

	privacy.learnNamespaces([]string{"alice-apps", "kube-system"})

	fake := strings.Trim(privacy.mask(`"alice-apps"`), `"`)
	if fake == "alice-apps" {
		t.Fatalf("not masked: %s", fake)
	}

	args, err := newPageArgs(fake, "", 0)
	if err != nil || args.namespace != "alice-apps" {
		t.Fatalf("got %+v %v", args, err)
	}

	if args, _ := newPageArgs("kube-system", "", 0); args.namespace != "kube-system" {
		t.Fatalf("plain namespace %q", args.namespace)
	}

	// Forgotten with the mapping, when the mask changes.
	SetPrivacyMask(false, "")

	if privacy.revealNamespace(fake) != fake {
		t.Fatal("kept after the mask was turned off")
	}
}

func TestKubeWorkloadReadsOne(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	w := demoKubeWorkloads()[0]

	out, err := KubeWorkload(cfg, "", "", w.Kind, w.Namespace, w.Name)
	if err != nil || !strings.Contains(out, `"name":"`+w.Name+`"`) {
		t.Fatalf("got %v %s", err, out)
	}

	if _, err := KubeWorkload(cfg, "", "", "Deployment", "../x", "web"); err == nil {
		t.Error("bad namespace accepted")
	}
}

func TestMapPodCountsSidecarsLikeKubectl(t *testing.T) {
	p := mapPod(decodePod(t, `{"metadata":{"name":"web-1","namespace":"shop"},
		"spec":{"initContainers":[{"name":"setup"},{"name":"proxy","restartPolicy":"Always"}],"containers":[{"name":"web","image":"nginx"}]},
		"status":{"phase":"Running",
			"initContainerStatuses":[
				{"name":"setup","restartCount":1,"state":{"terminated":{"exitCode":0}}},
				{"name":"proxy","ready":true,"started":true,"restartCount":2,"state":{"running":{}}}],
			"containerStatuses":[{"name":"web","ready":true,"restartCount":0,"state":{"running":{}}}]}}`))

	// READY 2/2, RESTARTS 3, Running: what the Table rows say.
	if p.Status != "Running" || p.Ready != 2 || p.Containers != 2 || p.Restarts != 3 || !p.Healthy {
		t.Fatalf("got %+v", p)
	}
}

func TestCronJobsPageReadsJobsOfItsNamespaces(t *testing.T) {
	api, k := newPagingKubeAPI(t, func(_ http.ResponseWriter, path string, _ url.Values) string {
		switch path {
		case "/apis/batch/v1/cronjobs":
			return `{"metadata":{},"items":[{"metadata":{"name":"backup","namespace":"ops"},"spec":{"schedule":"0 3 * * *"}},
				{"metadata":{"name":"report","namespace":"shop"},"spec":{"schedule":"0 4 * * *"}}]}`
		case "/apis/batch/v1/namespaces/ops/jobs":
			return `{"metadata":{},"items":[{"metadata":{"name":"backup-1","namespace":"ops","ownerReferences":[{"kind":"CronJob","name":"backup"}]}}]}`
		}

		return `{"metadata":{},"items":[]}`
	})

	page, err := listCronJobsPage(context.Background(), k, "", pageQuery{}, time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	if len(page.CronJobs) != 2 || len(page.CronJobs[0].Runs) != 1 || !page.Complete {
		t.Fatalf("got %+v", page)
	}

	var paths []string
	for _, r := range api.requests() {
		paths = append(paths, r.path)
	}

	// Not every namespace's Jobs: those of the page's namespaces.
	if strings.Join(paths, ",") != "/apis/batch/v1/cronjobs,/apis/batch/v1/namespaces/ops/jobs,/apis/batch/v1/namespaces/shop/jobs" {
		t.Fatalf("requests %v", paths)
	}
}
