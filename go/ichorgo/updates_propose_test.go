package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/gitforge"
)

const (
	planWebFile = "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\nspec:\n  template:\n    spec:\n      containers:\n" +
		"        - name: web\n          image: ghcr.io/example-org/web:1.2.0\n        - name: proxy\n          image: nginx:1.27.4\n"
	planAppFile = "apiVersion: helm.toolkit.fluxcd.io/v2\nkind: HelmRelease\nmetadata:\n  name: app\n  namespace: demo\nspec:\n" +
		"  chart:\n    spec:\n      chart: app\n      version: 1.0.0\n"
)

// planTestEnv is a Flux Kustomization apps (repository repoURL, path apps/) declaring the
// Deployment web (two containers) and the HelmRelease app, and a HelmRelease shop whose chart
// renders its image.
func planTestEnv(repoURL string, files map[string][]byte) updateEditEnv {
	apps := gitopsSource{RepoURL: repoURL, Tracked: "main", Path: "apps", Kind: gitopsKindKustomize, Browsable: true}

	return updateEditEnv{
		sources: func(_ context.Context, app updateEditApp) (gitopsSources, error) {
			switch app.Name {
			case "apps":
				return gitopsSources{Sources: []gitopsSource{apps}}, nil
			case "shop":
				return gitopsSources{Sources: []gitopsSource{{RepoURL: "https://charts.example.test", Kind: gitopsKindChart, Chart: "shop"}}}, nil
			}

			return gitopsSources{}, errors.New("not found")
		},
		files: func(_ context.Context, src gitopsSource) (sourceFiles, error) {
			return treeFilesUnder(files, src.Path), nil
		},
		meta: func(_ context.Context, _, _, namespace, name string) (fluxMeta, error) {
			return fluxMeta{Name: name, Namespace: namespace, Labels: map[string]string{fluxKustomizeNameLabel: "apps", fluxKustomizeNsLabel: "flux-system"}}, nil
		},
	}
}

func planItems() []updateProposalItem {
	apps := &updateOwner{Tool: "flux", Kind: "Kustomization", Namespace: "flux-system", Name: "apps"}
	web := func(container string) []updateWorkloadRef {
		return []updateWorkloadRef{{Kind: "Deployment", Namespace: "demo", Name: "web", Containers: []string{container}}}
	}

	return []updateProposalItem{
		{updateEditItem: updateEditItem{
			Repo: "ghcr.io/example-org/web", Workloads: web("web"), Owner: apps, Version: "1.2.1",
			Edit: &updateEdit{Op: "image", Kind: "Deployment", Name: "web", Container: "web", Image: "ghcr.io/example-org/web:1.2.1"},
		}, Tag: "1.2.0", Current: "1.2.0", Patch: "1.2.1"},
		{updateEditItem: updateEditItem{
			Repo: "ghcr.io/example-org/shop", Workloads: []updateWorkloadRef{{Kind: "Deployment", Namespace: "demo", Name: "shop"}},
			Owner: &updateOwner{Tool: "flux", Kind: "HelmRelease", Namespace: "demo", Name: "shop"}, Edit: &updateEdit{Op: "path", Hint: updateRenderedHint},
		}, Tag: "3.0.0", Current: "3.0.0", Patch: "3.0.1"},
		{updateEditItem: updateEditItem{
			Repo: "nginx", Workloads: web("proxy"), Owner: apps, Version: "1.28.0",
			Edit: &updateEdit{Op: "image", Kind: "Deployment", Name: "web", Container: "proxy", Image: "nginx:1.27.5"},
		}, Tag: "1.27.4", Current: "1.27.4", Patch: "1.27.5", Minor: "1.28.0"},
		{updateEditItem: updateEditItem{
			Name: "app", Owner: &updateOwner{Tool: "flux", Kind: "HelmRelease", Namespace: "demo", Name: "app"}, Version: "2.0.0",
			Edit: &updateEdit{Op: "path", Path: "spec.chart.spec.version", Value: "1.0.1"},
		}, Current: "1.0.0", Patch: "1.0.1", Major: "2.0.0", RepoURL: "https://charts.example.test"},
	}
}

func TestUpdateProposalPlan(t *testing.T) {
	fixedBranchSuffix(t)

	files := map[string][]byte{"apps/web/deployment.yaml": []byte(planWebFile), "apps/releases/app.yaml": []byte(planAppFile)}
	contents := "/api/v3/repos/example-org/fleet/contents/"
	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		contents + "apps/web/deployment.yaml": jsonAnswer(`{"type":"file","path":"apps/web/deployment.yaml","sha":"web5ha","size":` +
			itoa(len(planWebFile)) + `,"encoding":"base64","content":"` + b64Content(planWebFile) + `"}`),
		contents + "apps/releases/app.yaml": jsonAnswer(`{"type":"file","path":"apps/releases/app.yaml","sha":"app5ha","size":` +
			itoa(len(planAppFile)) + `,"encoding":"base64","content":"` + b64Content(planAppFile) + `"}`),
	})
	repoURL := "https://127.0.0.1/example-org/fleet.git"

	plan, err := planUpdateProposal(context.Background(), planTestEnv(repoURL, files), forgeUpdateProposalRepo(gitHostJSON("github", srv.URL)), planItems())
	if err != nil {
		t.Fatal(err)
	}

	if plan.BaseRef != "main" || plan.Source == nil || plan.Source.RepoURL != repoURL || len(plan.Files) != 2 {
		t.Fatalf("plan %+v", plan)
	}

	web := plan.Files[0]
	if web.Path != "apps/web/deployment.yaml" || web.FileSha != "web5ha" || web.Original != planWebFile || len(web.Changes) != 2 ||
		!strings.Contains(web.Content, "web:1.2.1\n") || !strings.Contains(web.Content, "nginx:1.28.0\n") {
		t.Errorf("two edits on one file: %+v", web)
	}

	// The second edit's line is on the content the first one edited.
	if c := web.Changes[1]; c.Item != "nginx" || c.Index != 2 || c.Line != 12 || c.Before != "image: nginx:1.27.4" || c.After != "image: nginx:1.28.0" {
		t.Errorf("second change %+v", c)
	}

	if app := plan.Files[1]; app.FileSha != "app5ha" || len(app.Changes) != 1 || app.Changes[0].After != "version: 2.0.0" {
		t.Errorf("chart %+v", app)
	}

	if len(plan.Skipped) != 1 || plan.Skipped[0].Index != 1 || plan.Skipped[0].Reason != updateEditRenderedNote {
		t.Errorf("skipped %+v", plan.Skipped)
	}

	if plan.Title != "Update 3 dependencies" || plan.Message != plan.Title || plan.Branch != "ichor/update-3-dependencies-0001" {
		t.Errorf("title %q branch %q", plan.Title, plan.Branch)
	}

	for _, row := range []string{
		"| `ghcr.io/example-org/web` | `1.2.0` | `1.2.1` | patch | https://ghcr.io/example-org/web |",
		"| `nginx` | `1.27.4` | `1.28.0` | minor | https://hub.docker.com/_/nginx |",
		"| `app` | `1.0.0` | `2.0.0` | major | https://charts.example.test |",
		"- `ghcr.io/example-org/shop`: rendered by a chart",
	} {
		if !strings.Contains(plan.Body, row) {
			t.Errorf("body lacks %q:\n%s", row, plan.Body)
		}
	}
}

func TestUpdateProposalPlanSkipsAndLimits(t *testing.T) {
	items := planItems()

	// No Git host: the item is skipped, and says how to fix it.
	web := map[string][]byte{"apps/web/deployment.yaml": []byte(planWebFile)}

	plan, err := planUpdateProposal(context.Background(), planTestEnv("https://git.example.test/example-org/fleet.git", web), forgeUpdateProposalRepo(""), items[:1])
	if err != nil || len(plan.Files) != 0 || len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "add a Git host") {
		t.Errorf("no host: %+v %v", plan, err)
	}

	// Not in Git.
	plan, err = planUpdateProposal(context.Background(), planTestEnv("https://git.example.test/example-org/fleet.git", nil), forgeUpdateProposalRepo(""), items[:1])
	if err != nil || len(plan.Files) != 0 || len(plan.Skipped) != 1 || !strings.Contains(plan.Skipped[0].Reason, "no file under apps") {
		t.Errorf("no file: %+v %v", plan, err)
	}

	files := map[string][]byte{}
	many := make([]updateProposalItem, 0, updateProposalMaxFiles+1)

	for i := range updateProposalMaxFiles + 1 {
		name := fmt.Sprintf("web%d", i)
		files["apps/"+name+".yaml"] = []byte(strings.Replace(planWebFile, "metadata:\n  name: web\n", "metadata:\n  name: "+name+"\n", 1))
		it := items[0]
		it.Workloads = []updateWorkloadRef{{Kind: "Deployment", Namespace: "demo", Name: name}}
		e := *it.Edit
		e.Name = name
		it.Edit = &e
		many = append(many, it)
	}

	repo := updateProposalRepo{
		base: func(context.Context, gitopsSource) (string, error) { return "main", nil },
		file: func(_ context.Context, _ gitopsSource, _, p string) (gitforge.File, error) {
			return gitforge.File{Path: p, SHA: "5ha", Content: string(files[p]), Size: int64(len(files[p]))}, nil
		},
	}

	if _, err := planUpdateProposal(context.Background(), planTestEnv("https://git.example.test/example-org/fleet.git", files), repo, many); err == nil ||
		!strings.Contains(err.Error(), "20 files at most") {
		t.Errorf("21 files: %v", err)
	}

	if plan, err := planUpdateProposal(context.Background(), planTestEnv("https://git.example.test/example-org/fleet.git", files), repo, many[:1]); err != nil ||
		plan.Title != "Update web to 1.2.1" {
		t.Errorf("one update: %+v %v", plan, err)
	}

	if _, err := UpdateProposalPlan("", "", "", "", "[]"); !errors.Is(err, errUpdateProposalNone) {
		t.Errorf("none selected: %v", err)
	}
}

func TestUpdateProposalPlanDemo(t *testing.T) {
	noKubeDial(t)

	cfg := demoKubeconfigForTest(t)

	out, err := ClusterUpdates(cfg, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var updates struct {
		Images []map[string]any `json:"images"`
		Charts []map[string]any `json:"charts"`
	}
	if err := json.Unmarshal([]byte(out), &updates); err != nil {
		t.Fatal(err)
	}

	var items []map[string]any

	for _, it := range append(updates.Images, updates.Charts...) {
		switch it["repo"] {
		case "ghcr.io/stefanprodan/podinfo", "ghcr.io/example-org/podinfo-exporter", "registry.k8s.io/coredns/coredns":
			items = append(items, it)
		}

		if it["name"] == "ingress-nginx" {
			items = append(items, it)
		}
	}

	in, _ := json.Marshal(items) //nolint:errcheck

	got, err := UpdateProposalPlan(cfg, "", "", "", string(in))
	if err != nil {
		t.Fatal(err)
	}

	var plan updateProposalPlan
	if err := json.Unmarshal([]byte(got), &plan); err != nil {
		t.Fatal(err)
	}

	if len(plan.Files) != 2 || plan.Files[0].Path != "apps/podinfo/deployment.yaml" || len(plan.Files[0].Changes) != 2 ||
		plan.Files[1].Path != "apps/infra/ingress-nginx.yaml" || len(plan.Skipped) != 1 || plan.Title != "Update 3 dependencies" {
		t.Errorf("demo plan %s", got)
	}
}
