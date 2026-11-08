package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/gitforge"
)

func Test_findManifest(t *testing.T) {
	deploy := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  namespace: shop\nspec: {}\n"

	for _, tc := range []struct {
		name           string
		files          map[string]string
		kind, ns, res  string
		prefix, suffix string
		want           []manifestMatch
	}{
		{
			name:  "plain match",
			files: map[string]string{"apps/web/deploy.yaml": deploy},
			kind:  "Deployment", ns: "shop", res: "web",
			want: []manifestMatch{{Path: "apps/web/deploy.yaml", Line: 4}},
		},
		{
			name:  "other namespace",
			files: map[string]string{"apps/web/deploy.yaml": deploy},
			kind:  "Deployment", ns: "other", res: "web",
			want: []manifestMatch{},
		},
		{
			name:  "flux prefix and suffix",
			files: map[string]string{"apps/web/deploy.yaml": deploy},
			kind:  "Deployment", ns: "shop", res: "prod-web-v2", prefix: "prod-", suffix: "-v2",
			want: []manifestMatch{{Path: "apps/web/deploy.yaml", Line: 4, renamed: true}},
		},
		{
			name: "kustomization prefix, namespace overridden, file skipped",
			files: map[string]string{
				"apps/kustomization.yaml":    "namePrefix: team-\nnamespace: other\nresources: [web]\n",
				"apps/web/kustomization.yml": "kind: Kustomization\nmetadata:\n  name: web\nnamePrefix: a-\n",
				"apps/web/deploy.yaml":       deploy,
			},
			kind: "Deployment", ns: "other", res: "team-a-web",
			want: []manifestMatch{{Path: "apps/web/deploy.yaml", Line: 4, renamed: true}},
		},
		{
			name:  "kustomization file not matched",
			files: map[string]string{"kustomization.yaml": "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nmetadata:\n  name: web\n"},
			kind:  "Kustomization", res: "web",
			want: []manifestMatch{},
		},
		{
			name: "multi-document file and List items",
			files: map[string]string{
				"all.yaml": "kind: Service\nmetadata:\n  name: web\n---\n" + deploy + "---\nkind: List\nitems:\n  - kind: Deployment\n    metadata:\n      name: web\n",
			},
			kind: "Deployment", ns: "shop", res: "web",
			want: []manifestMatch{{Path: "all.yaml", Line: 8, DocumentIndex: 1}, {Path: "all.yaml", Line: 16, DocumentIndex: 2}},
		},
		{
			name:  "json and a template skipped",
			files: map[string]string{"cm.json": `{"kind":"ConfigMap","metadata":{"name":"web"}}`, "t.yaml": "kind: {{ .Kind }\n", "README.md": "kind: ConfigMap"},
			kind:  "ConfigMap", res: "web",
			want: []manifestMatch{{Path: "cm.json", Line: 1}},
		},
		{
			name:  "no match",
			files: map[string]string{"apps/web/deploy.yaml": deploy},
			kind:  "StatefulSet", res: "web",
			want: []manifestMatch{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string][]byte{}
			for p, c := range tc.files {
				files[p] = []byte(c)
			}

			got := findManifest(files, tc.kind, tc.ns, tc.res, tc.prefix, tc.suffix)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGitOpsSourceFindDemo(t *testing.T) {
	noKubeDial(t)

	cfg := demoKubeconfigForTest(t)

	for _, tool := range []string{"flux", "argocd"} {
		kind := "Kustomization"
		if tool == "argocd" {
			kind = "Application"
		}

		out, err := GitOpsSourceFind(cfg, "", "", "", tool, kind, "flux-system", "podinfo", "0", "Deployment", "demo", "podinfo")
		if err != nil || out != `{"matches":[{"path":"apps/podinfo/deployment.yaml","line":4,"documentIndex":0}],"searched":3,"note":""}` {
			t.Errorf("%s: %s %v", tool, out, err)
		}
	}

	if _, err := GitOpsSourceFind(cfg, "", "", "", "flux", "Kustomization", "flux-system", "podinfo", "1", "Deployment", "demo", "podinfo"); err == nil {
		t.Error("a missing source was searched")
	}

	if _, err := GitOpsSourceFind(cfg, "", "", "", "flux", "Kustomization", "flux-system", "podinfo", "x", "Deployment", "demo", "podinfo"); err == nil {
		t.Error("a bad source index was accepted")
	}

	if _, err := GitOpsSourceFind(cfg, "", "", "", "flux", "GitRepository", "flux-system", "podinfo", "0", "Deployment", "demo", "podinfo"); err == nil {
		t.Error("a GitRepository was searched as an app")
	}
}

func TestGitOpsSourceFindHelm(t *testing.T) {
	read := func(context.Context) (sourceFiles, error) {
		t.Fatal("a Helm source was read")
		return sourceFiles{}, nil
	}

	got, err := findInSource(context.Background(), gitopsSource{Kind: gitopsKindHelm, Browsable: true}, manifestQuery{"Deployment", "", "web"}, read)
	if err != nil || got.Note != gitopsFindHelmNote || got.Matches == nil || len(got.Matches) != 0 {
		t.Errorf("%+v %v", got, err)
	}

	// A plain directory holding a Chart.yaml is rendered by Helm too.
	files := treeFilesUnder(map[string][]byte{"charts/web/Chart.yaml": nil, "charts/web/templates/d.yaml": nil}, "charts/web")
	if !files.helm {
		t.Error("a chart directory was searched")
	}
}

func TestTreeFilesUnderBounds(t *testing.T) {
	files := map[string][]byte{"other/x.yaml": nil, "apps/big.yaml": make([]byte, gitopsFindMaxFile+1), "apps/kustomization.yaml": nil, "apps/notes.txt": nil}
	for i := range gitopsFindMaxFiles + 5 {
		files["apps/f"+strings.Repeat("0", 3)+string(rune('a'+i%26))+strings.Repeat("z", i/26)+".yaml"] = nil
	}

	got := treeFilesUnder(files, "apps")
	if len(got.files) != gitopsFindMaxFiles || !got.cut {
		t.Errorf("%d files, cut %v", len(got.files), got.cut)
	}

	for p := range got.files {
		if !strings.HasPrefix(p, "apps/") || p == "apps/big.yaml" || p == "apps/notes.txt" {
			t.Errorf("read %s", p)
		}
	}
}

func TestWalkForgeFiles(t *testing.T) {
	tree := map[string][]gitforge.Entry{
		"apps":     {{Path: "apps/web", Type: "dir"}, {Path: "apps/kustomization.yaml", Type: "file", Size: 10}, {Path: "apps/huge.yaml", Type: "file", Size: gitopsFindMaxFile + 1}},
		"apps/web": {{Path: "apps/web/deploy.yaml", Type: "file", Size: 10}, {Path: "apps/web/logo.png", Type: "file", Size: 10}},
	}

	var read []string

	r := sourceForge{
		tree: func(_ context.Context, dir string) ([]gitforge.Entry, error) {
			if e, ok := tree[dir]; ok {
				return e, nil
			}

			return nil, gitforge.ErrNotFound
		},
		file: func(_ context.Context, p string) (gitforge.File, error) {
			read = append(read, p)

			return gitforge.File{Path: p, Content: "kind: Deployment\nmetadata:\n  name: web\n"}, nil
		},
	}

	got, err := walkForgeFiles(context.Background(), r, "apps")
	if err != nil || len(got.files) != 2 || got.cut || got.helm {
		t.Fatalf("%+v %v", got, err)
	}

	if strings.Join(read, ",") != "apps/kustomization.yaml,apps/web/deploy.yaml" {
		t.Errorf("read %v", read)
	}

	if _, err := walkForgeFiles(context.Background(), r, "nowhere"); err == nil || !strings.Contains(err.Error(), "nowhere") {
		t.Errorf("missing dir: %v", err)
	}

	r.file = func(context.Context, string) (gitforge.File, error) {
		return gitforge.File{}, errors.New("the token for git.example.org was refused")
	}
	if _, err := walkForgeFiles(context.Background(), r, "apps"); err == nil {
		t.Error("a refused token was ignored")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got, err := walkForgeFiles(ctx, r, "apps"); err != nil || !got.cut {
		t.Errorf("out of time: %+v %v", got, err)
	}
}

// A source on another host than the Git host's is refused before any request: the token stays
// with its host.
func TestGitOpsSourceFindChecksTheHost(t *testing.T) {
	src := gitopsSource{RepoURL: "https://git.example.org/team/fleet.git", Tracked: "main", Path: "apps", Kind: gitopsKindDirectory, Browsable: true}

	host, _ := json.Marshal(gitforge.Host{Kind: gitforge.KindGitea, BaseURL: "https://code.example.net", Token: "secret-token"}) //nolint:errcheck
	if _, err := newSourceForge(string(host), src); err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Errorf("other host: %v", err)
	}

	host, _ = json.Marshal(gitforge.Host{Kind: gitforge.KindGitea, BaseURL: "https://code.example.net", Token: "t", Aliases: []string{"git.example.org"}}) //nolint:errcheck
	if _, err := newSourceForge(string(host), src); err != nil {
		t.Errorf("alias: %v", err)
	}
}

func TestArgoKustomizeRenames(t *testing.T) {
	var o argoObject
	if err := json.Unmarshal([]byte(`{"spec":{"source":{"repoURL":"https://git.example.org/a/b","path":"x","kustomize":{"namePrefix":"p-","namespace":"n"}}}}`), &o); err != nil {
		t.Fatal(err)
	}

	if g := argoGitOpsSources(o)[0]; g.namePrefix != "p-" || !g.setsNamespace {
		t.Errorf("%+v", g)
	}
}
