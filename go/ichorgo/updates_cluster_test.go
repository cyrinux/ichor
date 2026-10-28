package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/internal/upstream"
)

// updatesRegistry serves the tags and the chart index the cluster below runs.
func updatesRegistry(t *testing.T) (*httptest.Server, *upstream.Client) {
	t.Helper()

	tags := map[string]string{
		"podinfo": `["6.7.0","6.7.1","6.8.0","7.0.0"]`,
		"web":     `["1.2.0","1.2.1"]`,
		"api":     `["2.0.0","2.1.0"]`,
		"auto":    `["1.0.0","1.1.0"]`,
		"job":     `["1.0.0"]`,
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, ok := strings.CutPrefix(r.URL.Path, "/v2/example-org/"); ok {
			if list, ok := tags[strings.TrimSuffix(name, "/tags/list")]; ok {
				_, _ = w.Write([]byte(`{"tags":` + list + `}`)) //nolint:errcheck

				return
			}
		}

		if r.URL.Path == "/charts/index.yaml" {
			_, _ = w.Write([]byte("entries:\n" + //nolint:errcheck
				"  podinfo:\n    - version: 6.7.0\n    - version: 6.8.0\n    - version: 7.0.0\n" +
				"  app:\n    - version: 1.0.0\n    - version: 1.0.1\n" +
				"  shop:\n    - version: 1.0.0\n    - version: 2.0.0\n"))

			return
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	host := strings.TrimPrefix(srv.URL, "https://")

	return srv, upstream.New(upstream.Options{HTTP: srv.Client(), HubURL: srv.URL, DockerRegistry: host})
}

func updatesCluster(t *testing.T, reg string, charts string) *kubeClient {
	t.Helper()

	deploy := func(ns, name, image, labels, annotations, extra string) string {
		return `{"metadata":{"name":"` + name + `","namespace":"` + ns + `","labels":{` + labels + `},"annotations":{` + annotations + `}},
			"spec":{"template":{"spec":{"containers":[{"name":"` + name + `","image":"` + reg + `/example-org/` + image + `"}]` + extra + `}}}}`
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}},
			{"name":"helm.toolkit.fluxcd.io","preferredVersion":{"version":"v2"}},
			{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},
			{"name":"image.toolkit.fluxcd.io","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET /apis/apps/v1/deployments": `{"items":[` +
			deploy("demo", "podinfo", "podinfo:6.7.0", `"helm.toolkit.fluxcd.io/name":"podinfo","helm.toolkit.fluxcd.io/namespace":"demo"`, ``, ``) + `,` +
			deploy("demo", "web", "web:1.2.0", `"kustomize.toolkit.fluxcd.io/name":"apps","kustomize.toolkit.fluxcd.io/namespace":"flux-system"`, ``,
				`,"initContainers":[{"name":"migrate","image":"`+reg+`/example-org/web:1.2.0"}],"imagePullSecrets":[{"name":"regcred"}]`) + `,` +
			deploy("shop", "api", "api:2.0.0", ``, ``, ``) + `,` +
			deploy("auto", "auto", "auto:1.0.0", ``, ``, ``) + `]}`,
		"GET /apis/apps/v1/daemonsets": `{"items":[]}`,
		"GET /apis/batch/v1/cronjobs": `{"items":[{"metadata":{"name":"nightly","namespace":"demo"},
			"spec":{"jobTemplate":{"spec":{"template":{"spec":{"containers":[{"name":"job","image":"` + reg + `/example-org/job:1.0.0"}]}}}}}}]}`,
		"GET /apis/helm.toolkit.fluxcd.io/v2/helmreleases": `{"items":[
			{"metadata":{"name":"podinfo","namespace":"demo"},"spec":{"chart":{"spec":{"chart":"podinfo","version":">=6.0.0",
				"sourceRef":{"kind":"HelmRepository","name":"charts","namespace":"flux-system"}}}},"status":{"history":[{"chartVersion":"6.7.0"}]}},
			{"metadata":{"name":"app","namespace":"demo"},"spec":{"chart":{"spec":{"chart":"app","version":"1.0.0",
				"sourceRef":{"kind":"HelmRepository","name":"charts","namespace":"flux-system"}}}},"status":{"history":[{"chartVersion":"1.0.0"}]}}]}`,
		"GET /apis/source.toolkit.fluxcd.io/v1/helmrepositories": `{"items":[{"metadata":{"name":"charts","namespace":"flux-system"},"spec":{"url":"` + charts + `"}}]}`,
		"GET /apis/source.toolkit.fluxcd.io/v1/ocirepositories":  `{"items":[]}`,
		"GET /apis/argoproj.io/v1alpha1/applications": `{"items":[{"metadata":{"name":"shop","namespace":"argocd",
			"annotations":{"argocd-image-updater.argoproj.io/image-list":"api=` + reg + `/example-org/api:~2"}},
			"spec":{"source":{"repoURL":"` + charts + `","chart":"shop","targetRevision":"1.0.0"}},
			"status":{"resources":[{"kind":"Deployment","namespace":"shop","name":"api"}]}}]}`,
		"GET /apis/image.toolkit.fluxcd.io/v1beta2/imagerepositories": `{"items":[{"metadata":{"name":"auto","namespace":"flux-system"},
			"spec":{"image":"` + reg + `/example-org/auto"}}]}`,
		"GET /apis/image.toolkit.fluxcd.io/v1beta2/imagepolicies": `{"items":[{"metadata":{"name":"auto","namespace":"flux-system"},
			"spec":{"imageRepositoryRef":{"name":"auto"}}}]}`,
		"GET /apis/image.toolkit.fluxcd.io/v1beta2/imageupdateautomations": `{"items":[{"metadata":{"name":"apps","namespace":"flux-system"}}]}`,
	})

	f.answerWith("GET /apis/apps/v1/statefulsets", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","code":403}`)
	f.answerWith("GET /api/v1/secrets", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","code":403}`)
	f.answerWith("GET /api/v1/namespaces/demo/secrets/regcred", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden","code":403}`)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return k
}

func TestClusterUpdates(t *testing.T) {
	srv, client := updatesRegistry(t)
	reg := strings.TrimPrefix(srv.URL, "https://")
	k := updatesCluster(t, reg, srv.URL+"/charts")

	r, err := collectUpdates(context.Background(), k, updateOptions{})
	if err != nil {
		t.Fatal(err)
	}

	lookupUpdates(context.Background(), client, &r)
	finishUpdates(&r)

	images := map[string]*updateImage{}
	for _, img := range r.Images {
		images[img.Repo[strings.LastIndex(img.Repo, "/")+1:]] = img
	}

	if len(r.Images) != 5 || r.Images[0].Repo != reg+"/example-org/podinfo" {
		t.Fatalf("images, major first: %+v", r.Images)
	}

	podinfo := images["podinfo"]
	if podinfo.Patch != "6.7.1" || podinfo.Minor != "6.8.0" || podinfo.Major != "7.0.0" || podinfo.Owner.Kind != "HelmRelease" ||
		podinfo.Edit == nil || podinfo.Edit.Op != "path" || podinfo.Edit.Hint != updateRenderedHint {
		t.Errorf("podinfo, rendered by its chart: %+v %+v %+v", podinfo.Candidate, podinfo.Owner, podinfo.Edit)
	}

	web := images["web"]
	if web.Credentials != "not readable" || web.Owner.Kind != "Kustomization" || len(web.Workloads) != 1 ||
		strings.Join(web.Workloads[0].Containers, ",") != "web,migrate" {
		t.Errorf("web: %+v %+v %+v", web, web.Owner, web.Workloads)
	}

	if e := web.Edit; e == nil || e.Op != "image" || e.Kind != "Deployment" || e.Name != "web" || e.Container != "web" || e.Image != reg+"/example-org/web:1.2.1" {
		t.Errorf("web edit: %+v", e)
	}

	if api := images["api"]; api.AutomatedBy != "argocd-image-updater" || api.Edit != nil || api.Owner.Tool != "argocd" {
		t.Errorf("api: %+v %+v", api, api.Owner)
	}

	if auto := images["auto"]; auto.AutomatedBy != "flux-image-automation" || auto.Edit != nil || auto.Minor != "1.1.0" {
		t.Errorf("auto: %+v", auto)
	}

	if job := images["job"]; job.Workloads[0].Kind != "CronJob" || updateRank(job.Candidate) != 3 || job.Edit != nil || job.Owner != nil {
		t.Errorf("job: %+v", job)
	}

	charts := map[string]*updateChart{}
	for _, ch := range r.Charts {
		charts[ch.Name] = ch
	}

	if p := charts["podinfo"]; p == nil || p.AutomatedBy != "flux-semver" || p.Constraint != ">=6.0.0" || p.Version != "6.7.0" || updateRank(p.Candidate) != 3 || p.Edit != nil {
		t.Errorf("podinfo chart, inside its range: %+v", p)
	}

	if a := charts["app"]; a == nil || a.Patch != "1.0.1" || a.Edit == nil || a.Edit.Path != "spec.chart.spec.version" || a.Edit.Value != "1.0.1" {
		t.Errorf("pinned chart: %+v %+v", a, a.Edit)
	}

	if s := charts["shop"]; s == nil || s.Major != "2.0.0" || s.Owner.Tool != "argocd" || s.Edit == nil || s.Edit.Path != "spec.source.targetRevision" || s.Edit.Value != "2.0.0" {
		t.Errorf("Argo CD chart: %+v", s)
	}

	skipped := map[string]string{}
	for _, s := range r.Skipped {
		skipped[s.What] = s.Reason
	}

	if skipped["StatefulSets"] != "not allowed to list them" || skipped["Helm releases"] != "not allowed to list them" {
		t.Errorf("skipped: %+v", r.Skipped)
	}

	if r.Lookups.Queried != 8 || r.Lookups.Failed != 0 {
		t.Errorf("lookups: %+v", r.Lookups)
	}
}

func TestClusterUpdatesNamespaces(t *testing.T) {
	srv, _ := updatesRegistry(t)
	k := updatesCluster(t, strings.TrimPrefix(srv.URL, "https://"), srv.URL+"/charts")

	r, err := collectUpdates(context.Background(), k, updateOptions{Namespaces: []string{"shop"}})
	if err != nil {
		t.Fatal(err)
	}

	if len(r.Images) != 1 || r.Images[0].Workloads[0].Name != "api" || len(r.Charts) != 0 {
		t.Errorf("only shop's workloads, and charts owned from shop's namespace: %+v %+v", r.Images, r.Charts)
	}
}

func TestClusterUpdatesDemo(t *testing.T) {
	saved := updatesClient
	updatesClient = upstream.New(upstream.Options{HubURL: "https://127.0.0.1:1", DockerRegistry: "127.0.0.1:1"})

	t.Cleanup(func() { updatesClient = saved })

	out, err := ClusterUpdates(demoKubeconfigForTest(t), "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Images []struct {
			Repo, AutomatedBy, Reason string
			Edit                      *updateEdit
		} `json:"images"`
		Charts []struct{ Name, AutomatedBy string } `json:"charts"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	if len(got.Images) < 5 || got.Images[0].Repo != "ghcr.io/stefanprodan/podinfo" || len(got.Charts) != 2 || got.Charts[1].AutomatedBy != "flux-semver" {
		t.Errorf("demo: %s", out)
	}
}

func TestSatisfiesRange(t *testing.T) {
	cases := []struct {
		constraint, version string
		want                bool
	}{
		{"*", "9.9.9", true},
		{">=6.0.0", "7.0.0", true},
		{">=6.0.0", "5.9.9", false},
		{">=1.2 <2", "1.9.0", true},
		{">=1.2 <2", "2.0.0", false},
		{">=1.2, <2", "1.1.0", false},
		{"~1.2", "1.2.9", true},
		{"~1.2", "1.3.0", false},
		{"~1.2.3", "1.2.2", false},
		{"~1", "1.9.0", true},
		{"^1.2.3", "1.9.0", true},
		{"^1.2.3", "2.0.0", false},
		{"^0.3.1", "0.3.9", true},
		{"^0.3.1", "0.4.0", false},
		{"1.x", "1.5.0", true},
		{"1.2.*", "1.3.0", false},
		{"=1.2.3", "1.2.3", true},
		{"1.2", "1.2.7", true},
		{"<1.0 || >=3", "3.1.0", true},
		{"v1.x", "v1.4.0", true},
		{"1.2.x", "nope", false},
		{"what?", "1.0.0", false},
	}

	for _, c := range cases {
		if got := satisfiesRange(c.constraint, c.version); got != c.want {
			t.Errorf("%q in %q: %v, want %v", c.version, c.constraint, got, c.want)
		}
	}

	for v, want := range map[string]bool{">=6.0.0": true, "~1.2": true, "1.x": true, "*": true, "": false, "1.2.3": false, "v2.0.0": false} {
		if isVersionRange(v) != want {
			t.Errorf("isVersionRange(%q) != %v", v, want)
		}
	}
}

func TestUpdateBotPresent(t *testing.T) {
	for files, want := range map[string]string{
		"README.md,renovate.json":                 "renovate",
		"apps/x.yaml,.github/dependabot.yml":      "dependabot",
		".github/renovate.json5":                  "renovate",
		"apps/renovate.json,.github/workflows/ci": "",
	} {
		bot, ok := updateBotPresent(strings.Split(files, ","))
		if bot != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", files, bot, ok, want)
		}
	}
}
