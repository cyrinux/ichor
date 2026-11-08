package ichorgo

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/gitforge"
)

var updateUpdateBotGolden = flag.Bool("update-updatebot-golden", false, "rewrite testdata/updatebot/*.json")

func botFiles(m map[string]string) map[string][]byte {
	out := make(map[string][]byte, len(m))
	for p, c := range m {
		out[p] = []byte(c)
	}

	return out
}

const (
	botFluxRelease = "apiVersion: helm.toolkit.fluxcd.io/v2\nkind: HelmRelease\nmetadata:\n  name: podinfo\n"
	botArgoApp     = "apiVersion: argoproj.io/v1alpha1\nkind: Application\nmetadata:\n  name: web\n"
	botDeployment  = "apiVersion: apps/v1\nkind: Deployment\nspec:\n  template:\n    spec:\n      containers:\n        - name: web\n          image: ghcr.io/example-org/web:1.4.0\n"
	botTalconfig   = "clusterName: lab\ntalosVersion: v1.11.2\nkubernetesVersion: v1.34.1\n"
	botInstaller   = "machine:\n  install:\n    image: factory.talos.dev/installer/376567988ad370138ad8b2698212367b8edcb69b5fd68c80be1f2ec7d603b4ba:v1.11.2\n"
)

func TestDetectUpdateBot(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []updateBotFile
	}{
		{"none", map[string]string{"kubernetes/app.yaml": botDeployment}, []updateBotFile{}},
		{"root config", map[string]string{"renovate.json": "{}"}, []updateBotFile{{"renovate.json", "renovate"}}},
		{"forge directory", map[string]string{".github/renovate.json5": "{}", ".gitlab/renovate.json": "{}"},
			[]updateBotFile{{".github/renovate.json5", "renovate"}, {".gitlab/renovate.json", "renovate"}}},
		{"workflow", map[string]string{
			".github/workflows/renovate.yaml": "jobs:\n  r:\n    steps:\n      - uses: renovatebot/github-action@v43\n",
			".github/workflows/ci.yml":        "jobs:\n  t:\n    steps:\n      - run: make test\n",
		}, []updateBotFile{{".github/workflows/renovate.yaml", "workflow"}}},
		{"dependabot", map[string]string{".github/dependabot.yml": "version: 2\n"}, []updateBotFile{{".github/dependabot.yml", "dependabot"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectUpdateBot(botFiles(tc.files)); !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// draftOf runs updateBotConfigFor and decodes its draft, failing on invalid JSON.
func draftOf(t *testing.T, files map[string]string, talosDir, kubeDir string, partial bool) (updateBotConfig, map[string]any) {
	t.Helper()

	cfg, err := updateBotConfigFor(botFiles(files), talosDir, kubeDir, gitforge.KindGitHub, partial)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Draft.Content == "" {
		return cfg, nil
	}

	if !json.Valid([]byte(cfg.Draft.Content)) {
		t.Fatalf("draft is not JSON:\n%s", cfg.Draft.Content)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(cfg.Draft.Content), &doc); err != nil {
		t.Fatal(err)
	}

	return cfg, doc
}

func TestUpdateBotDraftKubeDirOnly(t *testing.T) {
	cfg, doc := draftOf(t, map[string]string{
		"kubernetes/apps/podinfo/release.yaml": botFluxRelease,
		"kubernetes/apps/web/deployment.yaml":  botDeployment,
		"docs/example.yaml":                    botDeployment, // outside kubeDir
	}, "", "kubernetes", false)

	if cfg.Draft.Path != "renovate.json" || doc["flux"] == nil || doc["kubernetes"] == nil {
		t.Fatalf("draft %s:\n%s", cfg.Draft.Path, cfg.Draft.Content)
	}

	if _, ok := doc["argocd"]; ok {
		t.Errorf("argocd enabled without an Application:\n%s", cfg.Draft.Content)
	}

	if _, ok := doc["customManagers"]; ok {
		t.Errorf("custom managers without talconfig.yaml:\n%s", cfg.Draft.Content)
	}

	if !strings.Contains(cfg.Draft.Content, `"kubernetes/**/*.y*ml"`) || strings.Contains(cfg.Draft.Content, "docs/") {
		t.Errorf("patterns:\n%s", cfg.Draft.Content)
	}

	if !strings.Contains(strings.Join(cfg.Notes, "\n"), "1 file under kubernetes/ (for example kubernetes/apps/web/deployment.yaml)") {
		t.Errorf("notes %q", cfg.Notes)
	}
}

func TestUpdateBotDraftTalconfig(t *testing.T) {
	cfg, doc := draftOf(t, map[string]string{"talos/talconfig.yaml": botTalconfig}, "talos", "kubernetes", false)

	managers, _ := doc["customManagers"].([]any)
	if len(managers) != 2 || doc["flux"] != nil {
		t.Fatalf("managers %v:\n%s", managers, cfg.Draft.Content)
	}

	for _, want := range []string{`"siderolabs/talos"`, `"kubernetes/kubernetes"`, `"talos/talconfig.yaml"`, `talosVersion:\\s*`} {
		if !strings.Contains(cfg.Draft.Content, want) {
			t.Errorf("draft lacks %s:\n%s", want, cfg.Draft.Content)
		}
	}
}

// Both trees and an installer image: the whole draft, against the golden file.
func TestUpdateBotDraftMatchesGolden(t *testing.T) {
	cfg, _ := draftOf(t, map[string]string{
		"talos/talconfig.yaml":             botTalconfig,
		"talos/patches/install.yaml":       botInstaller,
		"talos/talsecret.sops.yaml":        "sops:\n  version: 3.10.2\n",
		"kubernetes/flux/podinfo.yaml":     botFluxRelease,
		"kubernetes/argocd/web.yaml":       botArgoApp,
		"kubernetes/apps/web/deploy.yaml":  botDeployment,
		"kubernetes/charts/web/Chart.yaml": "apiVersion: v2\nname: web\n",
	}, "talos", "kubernetes", false)

	got, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join("testdata", "updatebot", "talos-and-kubernetes.json")
	if *updateUpdateBotGolden {
		if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(want) != string(got)+"\n" {
		t.Errorf("%s differs (go test ./ichorgo -run UpdateBotDraftMatchesGolden -update-updatebot-golden); got:\n%s", path, got)
	}
}

func TestUpdateBotNothingToUpdate(t *testing.T) {
	cfg, _ := draftOf(t, map[string]string{"README.md": "# docs\n", "kubernetes/notes.yaml": "a: 1\n"}, "talos", "kubernetes", false)

	if cfg.Draft.Content != "" || cfg.Draft.Path != "" || len(cfg.Notes) != 1 || !strings.HasPrefix(cfg.Notes[0], "Nothing for a dependency-update bot") {
		t.Errorf("draft %+v notes %q", cfg.Draft, cfg.Notes)
	}
}

func TestUpdateBotConfiguredLeavesNoDraft(t *testing.T) {
	cfg, _ := draftOf(t, map[string]string{"renovate.json": "{}", "kubernetes/app.yaml": botDeployment}, "", "kubernetes", false)

	if cfg.Draft.Content != "" || len(cfg.Present) != 1 || !strings.Contains(cfg.Notes[0], "renovate.json") {
		t.Errorf("draft %+v present %v notes %q", cfg.Draft, cfg.Present, cfg.Notes)
	}
}

// Too large for a snapshot: only the bot files were read, every manager is on under kubeDir.
func TestUpdateBotPartialEnablesEveryManager(t *testing.T) {
	cfg, doc := draftOf(t, map[string]string{".github/workflows/ci.yml": "jobs: {}\n"}, "", "", true)

	for _, m := range []string{"flux", "argocd", "kubernetes"} {
		if doc[m] == nil {
			t.Errorf("%s off:\n%s", m, cfg.Draft.Content)
		}
	}

	if !strings.Contains(cfg.Draft.Content, `"**/*.y*ml"`) || !strings.Contains(cfg.Notes[0], "too large") {
		t.Errorf("draft:\n%s\nnotes %q", cfg.Draft.Content, cfg.Notes)
	}
}

func TestUpdateBotInstallNote(t *testing.T) {
	for kind, want := range map[string]string{
		gitforge.KindGitHub: "github.com/apps/renovate",
		gitforge.KindGitLab: "renovate/renovate image",
		gitforge.KindGitea:  "no hosted app",
		gitforge.KindDemo:   "on its forge",
	} {
		if got := updateBotInstallNote(kind); !strings.Contains(got, want) {
			t.Errorf("%s: %q lacks %q", kind, got, want)
		}
	}
}

// The demo repository: no bot, Talos versions in talos/talconfig.yaml, images under
// kubernetes/, the SOPS note; answered offline.
func TestUpdateBotConfigDemo(t *testing.T) {
	out, err := UpdateBotConfig(`{"kind":"demo"}`, demoClusterRepoURL, "main", "talos", "kubernetes")
	if err != nil {
		t.Fatal(err)
	}

	var cfg updateBotConfig
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatal(err)
	}

	notes := strings.Join(cfg.Notes, "\n")
	if len(cfg.Present) != 0 || cfg.Draft.Path != "renovate.json" || !json.Valid([]byte(cfg.Draft.Content)) ||
		!strings.Contains(cfg.Draft.Content, "talos/talconfig.yaml") || !strings.Contains(notes, "SOPS") {
		t.Errorf("demo: %s", out)
	}
}
