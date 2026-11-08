package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cyrinux/ichor/go/gitforge"
)

func TestTextDiff(t *testing.T) {
	big := strings.Repeat("x", gitforge.MaxFile+1)

	cases := []struct {
		name, before, after, file string
		change                    string
		diff                      string
		wantErr                   string
	}{
		{name: "equal", before: "a: 1\n", after: "a: 1\n", file: "apps/web/deployment.yaml", change: "unchanged"},
		{name: "both empty", file: "new.yaml", change: "unchanged"},
		{name: "crlf only", before: "a: 1\r\nb: 2\r\n", after: "a: 1\nb: 2\n", file: "f.yaml", change: "unchanged"},
		{
			name: "one line changed", before: "replicas: 1\nimage: web:1.0\n", after: "replicas: 2\nimage: web:1.0\n",
			file: "apps/web/deployment.yaml", change: "changed",
			diff: "--- a/apps/web/deployment.yaml\n+++ b/apps/web/deployment.yaml\n@@ -1,2 +1,2 @@\n-replicas: 1\n+replicas: 2\n image: web:1.0\n",
		},
		{
			name: "crlf before, change after", before: "a: 1\r\n", after: "a: 2\n", file: "f.yaml", change: "changed",
			diff: "--- a/f.yaml\n+++ b/f.yaml\n@@ -1,1 +1,1 @@\n-a: 1\n+a: 2\n",
		},
		{name: "no name", before: "a", after: "b", file: " ", wantErr: "no file name"},
		{name: "before too big", before: big, after: "a", file: "f.yaml", wantErr: "over 1 MiB"},
		{name: "after too big", before: "a", after: big, file: "f.yaml", wantErr: "over 1 MiB"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := TextDiff(tc.before, tc.after, tc.file)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			var got gitOpsDiff
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}

			if got.Kind != "file" || got.Name != tc.file || got.Warnings == nil || len(got.Resources) != 1 {
				t.Fatalf("diff = %s", out)
			}

			r := got.Resources[0]
			if r.Kind != "file" || r.Name != tc.file || r.Change != tc.change || r.Diff != tc.diff || r.Truncated {
				t.Errorf("resource = %+v, want change %q diff %q", r, tc.change, tc.diff)
			}
		})
	}
}

// The apps decode every field of the diff shape (iOS has no optional one).
func TestTextDiffHasEveryField(t *testing.T) {
	out, err := TextDiff("a\n", "b\n", "f.yaml")
	if err != nil {
		t.Fatal(err)
	}

	for _, field := range []string{`"kind":"file"`, `"namespace":""`, `"revision":""`, `"applied":""`, `"warnings":[]`,
		`"group":""`, `"version":""`, `"change":"changed"`, `"truncated":false`, `"error":""`} {
		if !strings.Contains(out, field) {
			t.Errorf("%s lacks %s", out, field)
		}
	}
}

func TestTextDiffTruncatesALargeDiff(t *testing.T) {
	before := strings.Repeat("old line of a file\n", 5000)
	after := strings.Repeat("new line of a file\n", 5000)

	d := textDiff(before, after, "f.yaml")
	r := d.Resources[0]

	if !r.Truncated || len(r.Diff) > diffMaxBytes || !strings.HasSuffix(r.Diff, "\n") {
		t.Errorf("truncated = %v, %d bytes", r.Truncated, len(r.Diff))
	}
}

func TestGitOpsProposalDefaults(t *testing.T) {
	prev := gitforge.BranchSuffix
	gitforge.BranchSuffix = func() string { return "a1b2" }

	t.Cleanup(func() { gitforge.BranchSuffix = prev })

	cases := []struct {
		name                                      string
		tool, kind, namespace, app, path, cluster string
		want                                      gitOpsProposal
		wantErr                                   string
	}{
		{
			name: "argo", tool: "argocd", kind: "Application", namespace: "argocd", app: "podinfo",
			path: "apps/podinfo/deployment.yaml", cluster: "lab",
			want: gitOpsProposal{
				Title: "Update apps/podinfo/deployment.yaml", Message: "Update apps/podinfo/deployment.yaml",
				Branch: "ichor/podinfo-deployment-a1b2",
				Body:   "Proposed from Ichor for Argo CD Application argocd/podinfo on lab.",
			},
		},
		{
			name: "flux, leading slash, upper case", tool: "flux", kind: "Kustomization", namespace: "flux-system", app: "Apps",
			path: "/clusters/home/kustomization.yaml", cluster: "home",
			want: gitOpsProposal{
				Title: "Update clusters/home/kustomization.yaml", Message: "Update clusters/home/kustomization.yaml",
				Branch: "ichor/apps-kustomization-a1b2",
				Body:   "Proposed from Ichor for Flux Kustomization flux-system/Apps on home.",
			},
		},
		{
			name: "no extension, no cluster", tool: "flux", kind: "HelmRelease", namespace: "web", app: "shop",
			path: "charts/shop/Chart.lock.d/values", cluster: " ",
			want: gitOpsProposal{
				Title: "Update charts/shop/Chart.lock.d/values", Message: "Update charts/shop/Chart.lock.d/values",
				Branch: "ichor/shop-values-a1b2",
				Body:   "Proposed from Ichor for Flux HelmRelease web/shop.",
			},
		},
		{
			name: "long name is cut", tool: "argocd", kind: "Application", namespace: "argocd",
			app: strings.Repeat("very-long-name-", 5), path: "a.yaml", cluster: "lab",
			want: gitOpsProposal{
				Title: "Update a.yaml", Message: "Update a.yaml",
				Branch: "ichor/very-long-name-very-long-name-very-long-a1b2",
				Body:   "Proposed from Ichor for Argo CD Application argocd/" + strings.Repeat("very-long-name-", 5) + " on lab.",
			},
		},
		{name: "unknown tool", tool: "helm", path: "a.yaml", wantErr: "unknown GitOps tool"},
		{name: "no path", tool: "flux", path: " / ", wantErr: "no file name"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := GitOpsProposalDefaults(tc.tool, tc.kind, tc.namespace, tc.app, tc.path, tc.cluster)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			var got gitOpsProposal
			if err := json.Unmarshal([]byte(out), &got); err != nil {
				t.Fatal(err)
			}

			if got != tc.want {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}
