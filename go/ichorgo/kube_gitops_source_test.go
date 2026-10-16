package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const (
	gitopsSHA     = "0123456789abcdef0123456789abcdef01234567"
	gitopsArgoApp = "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/"
	gitopsFluxSrc = "/apis/source.toolkit.fluxcd.io/v1/namespaces/flux-system/"
	gitopsGroups  = `{"groups":[{"name":"argoproj.io","preferredVersion":{"version":"v1alpha1"}},
		{"name":"kustomize.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}},
		{"name":"helm.toolkit.fluxcd.io","preferredVersion":{"version":"v2"}},
		{"name":"source.toolkit.fluxcd.io","preferredVersion":{"version":"v1"}}]}`
)

func gitopsClient(t *testing.T, answers map[string]string) (*kubeClient, *fakeKubeAPI) {
	t.Helper()

	answers["GET /apis"] = gitopsGroups
	f := newFakeKubeAPI(t, answers)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return k, f
}

func sourceLine(g gitopsSource) string {
	return strings.Join([]string{g.Kind, g.RepoURL, g.WebURL, g.Tracked, g.Revision, g.Path, g.Chart, strings.Join(g.ValueFiles, ",")}, "|")
}

func TestGitOpsSourceArgo(t *testing.T) {
	k, _ := gitopsClient(t, map[string]string{
		"GET " + gitopsArgoApp + "podinfo": `{"spec":{"source":{"repoURL":"git@git.example.org:example-org/platform.git",
			"path":"./apps/podinfo/","targetRevision":"main","kustomize":{"namePrefix":"x-"}}},
			"status":{"sync":{"revision":"` + gitopsSHA + `"}}}`,
		"GET " + gitopsArgoApp + "shop": `{"spec":{"sources":[
			{"repoURL":"https://charts.example.org","chart":"shop","targetRevision":"1.2.*","helm":{"valueFiles":["$values/shop/values-prod.yaml"]}},
			{"repoURL":"https://git.example.org/example-org/platform.git","targetRevision":"v2","ref":"values"},
			{"repoURL":"https://git.example.org/example-org/platform.git","path":"charts/extra","helm":{"valueFiles":["values.yaml"]}}]},
			"status":{"sync":{"revisions":["1.2.3","` + gitopsSHA + `","` + gitopsSHA + `"]}}}`,
	})

	single, err := readArgoSources(context.Background(), k, "argocd", "podinfo")
	if err != nil {
		t.Fatal(err)
	}

	want := "kustomize|git@git.example.org:example-org/platform.git|https://git.example.org/example-org/platform|main|" + gitopsSHA + "|apps/podinfo||"
	if len(single.Sources) != 1 || sourceLine(single.Sources[0]) != want || !single.Sources[0].Browsable || single.Sources[0].FluxArtifact {
		t.Errorf("single %+v", single.Sources)
	}

	multi, err := readArgoSources(context.Background(), k, "argocd", "shop")
	if err != nil {
		t.Fatal(err)
	}

	got := []string{}
	for _, g := range multi.Sources {
		got = append(got, sourceLine(g))
	}

	wantMulti := []string{
		"chart|https://charts.example.org||1.2.*|||shop|$values/shop/values-prod.yaml",
		"values|https://git.example.org/example-org/platform.git|https://git.example.org/example-org/platform|v2|" + gitopsSHA + "|||",
		"helm|https://git.example.org/example-org/platform.git|https://git.example.org/example-org/platform|HEAD|" + gitopsSHA + "|charts/extra||charts/extra/values.yaml",
	}
	if strings.Join(got, "\n") != strings.Join(wantMulti, "\n") {
		t.Errorf("multi\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(wantMulti, "\n"))
	}

	if multi.Sources[0].Browsable || !multi.Sources[1].Browsable {
		t.Errorf("browsable %v %v", multi.Sources[0].Browsable, multi.Sources[1].Browsable)
	}
}

func TestGitOpsSourceFlux(t *testing.T) {
	k, _ := gitopsClient(t, map[string]string{
		"GET /apis/kustomize.toolkit.fluxcd.io/v1/namespaces/flux-system/kustomizations/podinfo": `{"metadata":{"name":"podinfo","namespace":"flux-system"},
			"spec":{"path":"./apps/podinfo","sourceRef":{"kind":"GitRepository","name":"fleet"}}}`,
		"GET /apis/helm.toolkit.fluxcd.io/v2/namespaces/web/helmreleases/shop": `{"metadata":{"name":"shop","namespace":"web"},
			"spec":{"chart":{"spec":{"chart":"./charts/shop","valuesFiles":["charts/shop/values-prod.yaml"],"sourceRef":{"kind":"GitRepository","name":"fleet","namespace":"flux-system"}}}}}`,
		"GET /apis/helm.toolkit.fluxcd.io/v2/namespaces/web/helmreleases/redis": `{"metadata":{"name":"redis","namespace":"web"},
			"spec":{"chart":{"spec":{"chart":"redis","version":"20.x","sourceRef":{"kind":"HelmRepository","name":"bitnami","namespace":"flux-system"}}}}}`,
		"GET " + gitopsFluxSrc + "gitrepositories/fleet": `{"spec":{"url":"ssh://git@git.example.org/example-org/fleet.git","ref":{"branch":"main"}},
			"status":{"artifact":{"revision":"main@sha1:` + gitopsSHA + `"}}}`,
		"GET /apis/helm.toolkit.fluxcd.io/v2/namespaces/web/helmreleases/cache": `{"metadata":{"name":"cache","namespace":"web"},
			"spec":{"chartRef":{"kind":"OCIRepository","name":"valkey","namespace":"flux-system"}}}`,
		"GET /apis/helm.toolkit.fluxcd.io/v2/namespaces/web/helmreleases/queue": `{"metadata":{"name":"queue","namespace":"web"},
			"spec":{"chartRef":{"kind":"HelmChart","name":"web-queue","namespace":"flux-system"}}}`,
		"GET " + gitopsFluxSrc + "ocirepositories/valkey": `{"spec":{"url":"oci://registry.example.org/charts/valkey","ref":{"tag":"2.1.0","digest":"sha256:feed"}},
			"status":{"artifact":{"revision":"2.1.0@sha256:feed"}}}`,
		"GET " + gitopsFluxSrc + "helmcharts/web-queue": `{"spec":{"chart":"queue","version":"3.x","sourceRef":{"kind":"HelmRepository","name":"bitnami"}},
			"status":{"artifact":{"revision":"3.2.0"}}}`,
		"GET " + gitopsFluxSrc + "helmrepositories/bitnami": `{"spec":{"url":"oci://registry.example.org/charts","type":"oci"},"status":{"artifact":{"revision":"sha256:abc"}}}`,
	})

	cases := []struct{ kind, ns, name, want string }{
		{"Kustomization", "flux-system", "podinfo", "kustomize|ssh://git@git.example.org/example-org/fleet.git|https://git.example.org/example-org/fleet|main|" + gitopsSHA + "|apps/podinfo||"},
		{"HelmRelease", "web", "shop", "helm|ssh://git@git.example.org/example-org/fleet.git|https://git.example.org/example-org/fleet|main|" + gitopsSHA + "|charts/shop||charts/shop/values-prod.yaml"},
		{"HelmRelease", "web", "redis", "chart|oci://registry.example.org/charts||20.x|||redis|"},
		{"HelmRelease", "web", "cache", "chart|oci://registry.example.org/charts/valkey||sha256:feed||||"},
		{"HelmRelease", "web", "queue", "chart|||3.x|||queue|"},
	}

	for _, c := range cases {
		res, err := readFluxSources(context.Background(), k, c.kind, c.ns, c.name)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}

		g := res.Sources[0]
		if len(res.Sources) != 1 || sourceLine(g) != c.want {
			t.Errorf("%s: %s\nwant %s", c.name, sourceLine(g), c.want)
		}

		// Only Git sources are browsable on a Git host; an OCI chart is still read from its artifact.
		fromGit := c.name == "podinfo" || c.name == "shop"
		artifact := fromGit || c.name == "cache"
		if g.Browsable != fromGit || g.FluxArtifact != artifact || (g.FluxSource != nil) != artifact {
			t.Errorf("%s: browsable %v artifact %v source %+v", c.name, g.Browsable, g.FluxArtifact, g.FluxSource)
		}

		if fromGit && *g.FluxSource != (fluxRef{Kind: "GitRepository", Namespace: "flux-system", Name: "fleet"}) {
			t.Errorf("%s: source %+v", c.name, g.FluxSource)
		}
	}
}

func TestFluxCommitOf(t *testing.T) {
	for in, want := range map[string]string{
		"main@sha1:" + gitopsSHA:               gitopsSHA,
		"v1.2.3@sha1:" + gitopsSHA:             gitopsSHA,
		"sha1:" + gitopsSHA:                    gitopsSHA,
		"main/" + gitopsSHA:                    gitopsSHA,
		"latest@sha256:abcdef":                 "",
		"latest/" + gitopsSHA + gitopsSHA[:24]: "",
		"1.2.3":                                "",
	} {
		if got := fluxCommitOf(in); got != want {
			t.Errorf("%s: %q, want %q", in, got, want)
		}
	}
}

// A Flux source's files come from its artifact (here through the source-controller proxy),
// unpacked once for every directory walked.
func TestFluxSourceTreeFromArtifact(t *testing.T) {
	const art = "ocirepository/flux-system/fleet/abc.tar.gz"

	tgz := fluxTarball(t, map[string]string{
		"apps/podinfo/kustomization.yaml": "resources: [deploy.yaml]\n",
		"apps/podinfo/deploy.yaml":        "kind: Deployment\n",
		"README.md":                       "fleet\n",
	})

	k, f := gitopsClient(t, map[string]string{
		"GET " + gitopsFluxSrc + "ocirepositories/fleet":                                `{"status":{"artifact":{"path":"` + art + `","url":"http://source-controller.flux-system.svc.cluster.local./` + art + `","revision":"main@sha1:` + gitopsSHA + `"}}}`,
		"GET /api/v1/namespaces/flux-system/services/source-controller":                 `{"spec":{"selector":{"app":"source-controller"}}}`,
		"GET /api/v1/namespaces/flux-system/pods":                                       `{"items":[{"metadata":{"name":"source-controller-1"},"spec":{"containers":[{"name":"manager","args":["--storage-path=/data"]}]},"status":{"phase":"Running"}}]}`,
		"GET /api/v1/namespaces/flux-system/services/source-controller:80/proxy/" + art: string(tgz),
	})

	cluster := "test-" + t.Name()

	root, err := readFluxTree(context.Background(), k, cluster, "OCIRepository", "flux-system", "fleet")
	if err != nil {
		t.Fatal(err)
	}

	entries := listTreeDir(root.sizes(), "")
	sortGitForgeEntries(entries)

	out, _ := json.Marshal(entries) //nolint:errcheck
	if string(out) != `[{"path":"apps","type":"dir","size":0},{"path":"README.md","type":"file","size":6}]` {
		t.Errorf("root %s", out)
	}

	again, err := readFluxTree(context.Background(), k, cluster, "OCIRepository", "flux-system", "fleet")
	if err != nil || again != root {
		t.Errorf("the tree was not reused: %v", err)
	}

	fetches := 0
	for _, r := range f.recorded() {
		if strings.Contains(r.path, "/proxy/") {
			fetches++
		}
	}

	if fetches != 1 {
		t.Errorf("artifact fetched %d times, want 1", fetches)
	}

	if got := string(root.files["apps/podinfo/deploy.yaml"]); got != "kind: Deployment\n" {
		t.Errorf("file %q", got)
	}

	if _, err := readFluxTree(context.Background(), k, cluster, "GitRepository", "flux-system", "fleet"); err == nil || err.Error() != "no Flux GitRepository flux-system/fleet" {
		t.Errorf("missing source: %v", err)
	}
}

func TestArgoKustomizeNullIsNotDeclared(t *testing.T) {
	var o argoObject
	if err := json.Unmarshal([]byte(`{"spec":{"source":{"repoURL":"https://git.example.org/a/b","path":"x","kustomize":null}}}`), &o); err != nil {
		t.Fatal(err)
	}

	if k := argoGitOpsSources(o)[0].Kind; k != gitopsKindDirectory {
		t.Errorf("kind %s", k)
	}
}

func TestFluxSourceTreeRefusesOtherKinds(t *testing.T) {
	if _, err := FluxSourceTree(demoKubeconfigForTest(t), "", "", "HelmRepository", "flux-system", "bitnami", ""); err == nil {
		t.Error("a HelmRepository was read as a tree")
	}
}

func TestStoreFluxTreeBoundsTheCache(t *testing.T) {
	big := func() *fluxTree { return &fluxTree{files: map[string][]byte{}, size: fluxTreeCacheMax / 2} }

	storeFluxTree(t.Name()+"a", big())
	storeFluxTree(t.Name()+"b", big())
	storeFluxTree(t.Name()+"c", big())

	fluxTrees.Lock()
	_, a := fluxTrees.m[t.Name()+"a"]
	_, c := fluxTrees.m[t.Name()+"c"]
	fluxTrees.Unlock()

	if a || !c {
		t.Errorf("oldest kept %v, newest kept %v", a, c)
	}
}

func TestGitOpsSourceDemo(t *testing.T) {
	noKubeDial(t)

	cfg := demoKubeconfigForTest(t)

	out, err := GitOpsSource(cfg, "", "", "flux", "Kustomization", "flux-system", "podinfo")
	if err != nil || !strings.Contains(out, `"fluxArtifact":true`) || !strings.Contains(out, `"path":"apps/podinfo"`) {
		t.Errorf("source %s %v", out, err)
	}

	out, err = FluxSourceTree(cfg, "", "", "GitRepository", "flux-system", "flux-system", "apps/podinfo")
	if err != nil || strings.Count(out, `"type":"file"`) != 3 {
		t.Errorf("tree %s %v", out, err)
	}

	out, err = FluxSourceFile(cfg, "", "", "GitRepository", "flux-system", "flux-system", "apps/podinfo/hpa.yaml")
	if err != nil || !strings.Contains(out, "HorizontalPodAutoscaler") || !strings.Contains(out, `"sha":"`+demoGitOpsCommit+`"`) {
		t.Errorf("file %s %v", out, err)
	}

	if _, err := FluxSourceFile(cfg, "", "", "GitRepository", "flux-system", "flux-system", "../etc/passwd"); err == nil {
		t.Error("a path out of the source was read")
	}

	if _, err := GitOpsSource(cfg, "", "", "flux", "GitRepository", "flux-system", "x"); err == nil {
		t.Error("a GitRepository was mapped as an app")
	}
}
