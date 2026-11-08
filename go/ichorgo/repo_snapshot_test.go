package ichorgo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/gitforge"
)

const snapTestSHA = "0123456789abcdef0123456789abcdef01234567"

// testArchive is a forge-style tar.gz: every file under top/, plus a pax header and a directory.
func testArchive(t *testing.T, top string, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}

	must(tw.WriteHeader(&tar.Header{Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header", PAXRecords: map[string]string{"comment": snapTestSHA}}))
	must(tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: top + "/", Mode: 0o755}))

	for name, content := range files {
		must(tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: top + "/" + name, Mode: 0o644, Size: int64(len(content))}))
		_, err := tw.Write([]byte(content))
		must(err)
	}

	must(tw.Close())
	must(gz.Close())

	return buf.Bytes()
}

var snapTestFiles = map[string]string{
	"README.md":                           "# cluster-config\nKubeSpan is on.\n",
	"talos/patches/kubespan.yaml":         "machine:\n  network:\n    kubespan:\n      enabled: true\n",
	"talos/talsecret.sops.yaml":           "kubespanKey: ENC[AES256_GCM,data:a3ViZXNwYW4=]\nother: ENC[AES256_GCM,data:kubespan]\n",
	"kubernetes/apps/web/deployment.yaml": "image: ghcr.io/example-org/web:1.4.0\n",
	"logo.png":                            "\x89PNG\x00\x00kubespan",
}

// giteaCommits answers Gitea's commit list for ref main with snapTestSHA.
func giteaCommits(t *testing.T) func(w http.ResponseWriter, r *http.Request) {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sha") != "main" || r.URL.Query().Get("limit") != "1" {
			t.Errorf("gitea commits %q", r.URL.RawQuery)
		}

		fmt.Fprint(w, `[{"sha":"`+snapTestSHA+`"}]`)
	}
}

// resetSnapshots empties the in-memory snapshots, for a test that counts downloads.
func resetSnapshots(t *testing.T) {
	t.Helper()

	repoSnapshots.Lock()
	repoSnapshots.list = nil
	repoSnapshots.Unlock()
	t.Cleanup(func() {
		repoSnapshots.Lock()
		repoSnapshots.list = nil
		repoSnapshots.Unlock()
	})
}

func searchHits(t *testing.T, out string) repoSearchResult {
	t.Helper()

	var res repoSearchResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	return res
}

func TestGitForgeArchivePerForge(t *testing.T) {
	archive := testArchive(t, "example-org-cluster-config-0123456", snapTestFiles)

	// GitHub's signed URL is on another host: it must never see the token.
	codeload := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("PRIVATE-TOKEN") != "" {
			t.Error("the token went to the redirect's host")
		}

		w.Write(archive) //nolint:errcheck
	}))
	t.Cleanup(codeload.Close)

	serve := func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) } //nolint:errcheck

	for _, tc := range []struct {
		kind   string
		routes map[string]func(w http.ResponseWriter, r *http.Request)
	}{
		{gitforge.KindGitHub, map[string]func(w http.ResponseWriter, r *http.Request){
			"/api/v3/repos/example-org/cluster-config/commits/main": jsonAnswer(`{"sha":"` + snapTestSHA + `","files":[]}`),
			"/api/v3/repos/example-org/cluster-config/tarball/" + snapTestSHA: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, codeload.URL+"/example-org/cluster-config/legacy.tar.gz/"+snapTestSHA+"?token=signed", http.StatusFound)
			},
		}},
		{gitforge.KindGitLab, map[string]func(w http.ResponseWriter, r *http.Request){
			"/api/v4/projects/example-org%2Fcluster-config/repository/commits/main": jsonAnswer(`{"id":"` + snapTestSHA + `"}`),
			"/api/v4/projects/example-org%2Fcluster-config/repository/archive.tar.gz": func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("sha") != snapTestSHA {
					t.Errorf("gitlab archive of %q", r.URL.RawQuery)
				}

				serve(w, r)
			},
		}},
		{gitforge.KindGitea, map[string]func(w http.ResponseWriter, r *http.Request){
			"/api/v1/repos/example-org/cluster-config/commits":                            giteaCommits(t),
			"/api/v1/repos/example-org/cluster-config/archive/" + snapTestSHA + ".tar.gz": serve,
		}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			resetSnapshots(t)

			srv := gitServer(t, tc.routes)
			host, repo := gitHostJSON(tc.kind, srv.URL), "https://127.0.0.1/example-org/cluster-config"

			out, err := GitRepoSearch(host, repo, "main", "KUBESPAN", 0)
			if err != nil {
				t.Fatal(err)
			}

			res := searchHits(t, out)
			want := []repoSearchHit{
				{Path: "README.md", Line: 2, Text: "KubeSpan is on."},
				{Path: "talos/patches/kubespan.yaml", Line: 3, Text: "kubespan:"},
				{Path: "talos/talsecret.sops.yaml", Line: 1, Text: "kubespanKey: ENC[AES256_GCM,data:a3ViZXNwYW4=]"},
			}

			if res.SHA != snapTestSHA || res.Searched != 4 || res.Truncated || fmt.Sprint(res.Hits) != fmt.Sprint(want) {
				t.Errorf("got %+v, want hits %+v", res, want)
			}
		})
	}
}

func TestRepoSnapshotCache(t *testing.T) {
	resetSnapshots(t)
	dir := withDataDir(t)

	var downloads atomic.Int32

	archive := testArchive(t, "cluster-config-"+snapTestSHA, snapTestFiles)
	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v1/repos/example-org/cluster-config/commits": giteaCommits(t),
		"/api/v1/repos/example-org/cluster-config/archive/" + snapTestSHA + ".tar.gz": func(w http.ResponseWriter, _ *http.Request) {
			downloads.Add(1)
			w.Write(archive) //nolint:errcheck
		},
	})
	host, repo := gitHostJSON(gitforge.KindGitea, srv.URL), "https://127.0.0.1/example-org/cluster-config"

	status := func() repoSnapshotStatus {
		t.Helper()

		out, err := GitRepoSnapshotPrepare(host, repo, "main")
		if err != nil {
			t.Fatal(err)
		}

		var s repoSnapshotStatus
		if err := json.Unmarshal([]byte(out), &s); err != nil {
			t.Fatal(err)
		}

		return s
	}

	if s := status(); s.Cached || s.SHA != snapTestSHA || s.Files != 5 || s.Bytes != int64(len(archive)) {
		t.Errorf("first %+v", s)
	}

	if s := status(); !s.Cached {
		t.Errorf("second %+v", s)
	}

	// Out of memory, still on disk: no download, and what is on disk is not the archive in clear.
	repoSnapshots.Lock()
	repoSnapshots.list = nil
	repoSnapshots.Unlock()

	if s := status(); !s.Cached || s.Files != 5 {
		t.Errorf("from disk %+v", s)
	}

	if n := downloads.Load(); n != 1 {
		t.Errorf("%d downloads, want 1", n)
	}

	kept, _ := filepath.Glob(filepath.Join(dir, gitSnapshotDir, "*"+gitSnapshotExt)) //nolint:errcheck
	if len(kept) != 1 {
		t.Fatalf("kept %v", kept)
	}

	if b, _ := os.ReadFile(kept[0]); bytes.HasPrefix(b, []byte{0x1f, 0x8b}) || bytes.Contains(b, []byte("KubeSpan")) { //nolint:errcheck
		t.Error("the archive is kept unencrypted")
	}
}

func TestRepoSnapshotWithoutDataDirStaysInMemory(t *testing.T) {
	resetSnapshots(t)
	SetDataDir("", nil)

	archive := testArchive(t, "top", map[string]string{"a.yaml": "kind: A\n"})
	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v1/repos/example-org/cluster-config/archive/" + snapTestSHA + ".tar.gz": func(w http.ResponseWriter, _ *http.Request) {
			w.Write(archive) //nolint:errcheck
		},
	})

	// A commit is not resolved: no branch call.
	if _, err := GitRepoSnapshotPrepare(gitHostJSON(gitforge.KindGitea, srv.URL), "https://127.0.0.1/example-org/cluster-config", snapTestSHA); err != nil {
		t.Fatal(err)
	}
}

func TestGitForgeArchiveTooLarge(t *testing.T) {
	resetSnapshots(t)

	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v1/repos/example-org/cluster-config/archive/" + snapTestSHA + ".tar.gz": func(w http.ResponseWriter, _ *http.Request) {
			w.Write(make([]byte, gitforge.MaxArchive+1)) //nolint:errcheck
		},
	})

	_, err := GitRepoSearch(gitHostJSON(gitforge.KindGitea, srv.URL), "https://127.0.0.1/example-org/cluster-config", snapTestSHA, "x", 0)
	if err == nil || err.Error() != "the repository archive is over 48 MiB; browse it file by file" {
		t.Errorf("too large: %v", err)
	}
}

func TestGitForgeArchiveRedirectRefusals(t *testing.T) {
	resetSnapshots(t)

	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v1/repos/example-org/cluster-config/archive/" + snapTestSHA + ".tar.gz": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://downloads.example.org/archive.tar.gz", http.StatusFound)
		},
	})

	_, err := GitRepoSnapshotPrepare(gitHostJSON(gitforge.KindGitea, srv.URL), "https://127.0.0.1/example-org/cluster-config", snapTestSHA)
	if err == nil || !strings.Contains(err.Error(), "plain http") {
		t.Errorf("plain http redirect: %v", err)
	}

	if strings.Contains(fmt.Sprint(err), gitTestToken) {
		t.Error("the token is in the error")
	}
}

func TestSnapshotCommitResolvesEveryRef(t *testing.T) {
	resetSnapshots(t)

	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v3/repos/example-org/cluster-config/commits/v1.2.0": jsonAnswer(`{"sha":"` + strings.ToUpper(snapTestSHA) + `"}`),
		"/api/v3/repos/example-org/cluster-config/commits/HEAD":   jsonAnswer(`{"sha":"not-a-sha"}`),
	})
	h, err := gitforge.ParseHost(gitHostJSON(gitforge.KindGitHub, srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	f := gitforge.New(h)
	repo := gitforge.RepoRef{Host: "127.0.0.1", Owner: "example-org", Name: "cluster-config"}

	if sha, err := snapshotCommit(t.Context(), f, repo, "v1.2.0"); err != nil || sha != snapTestSHA {
		t.Errorf("tag: %q %v", sha, err)
	}

	if _, err := snapshotCommit(t.Context(), f, repo, "HEAD"); err == nil {
		t.Error("a ref without a commit was cached")
	}

	if _, err := snapshotCommit(t.Context(), f, repo, "gone"); err == nil || err.Error() != "gone is not in example-org/cluster-config" {
		t.Errorf("unknown ref: %v", err)
	}
}

func TestSnapshotMemoryIsBoundedInBytes(t *testing.T) {
	resetSnapshots(t)

	big := func() *repoSnapshot { return &repoSnapshot{text: gitSnapshotMemoryBytes/2 + 1} }
	storeRepoSnapshot("a", big())
	storeRepoSnapshot("b", big())

	if cachedRepoSnapshot("a") != nil || cachedRepoSnapshot("b") == nil {
		t.Error("two snapshots over the byte bound are both kept")
	}

	storeRepoSnapshot("c", &repoSnapshot{text: gitSnapshotMemoryBytes * 2})
	if cachedRepoSnapshot("c") == nil {
		t.Error("the newest snapshot is not kept")
	}
}

func TestConcurrentSnapshotLoadsDownloadOnce(t *testing.T) {
	resetSnapshots(t)

	var downloads atomic.Int32

	archive := testArchive(t, "top", map[string]string{"a.yaml": "kind: A\n"})
	srv := gitServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v1/repos/example-org/cluster-config/archive/" + snapTestSHA + ".tar.gz": func(w http.ResponseWriter, _ *http.Request) {
			downloads.Add(1)
			time.Sleep(50 * time.Millisecond)
			w.Write(archive) //nolint:errcheck
		},
	})
	host := gitHostJSON(gitforge.KindGitea, srv.URL)

	done := make(chan error, 4)
	for range 4 {
		go func() {
			_, err := GitRepoSearch(host, "https://127.0.0.1/example-org/cluster-config", snapTestSHA, "kind", 0)
			done <- err
		}()
	}

	for range 4 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}

	if n := downloads.Load(); n != 1 {
		t.Errorf("%d downloads, want 1", n)
	}

	snapshotLoads.Lock()
	left := len(snapshotLoads.keys)
	snapshotLoads.Unlock()

	if left != 0 {
		t.Errorf("%d load locks left", left)
	}
}

func TestUnpackRepoArchiveStripsTheTopAndStaysInside(t *testing.T) {
	archive := testArchive(t, "owner-repo-abc", map[string]string{
		"a/b.yaml": "kind: B\n", "../../escape.txt": "out", "big.txt": strings.Repeat("x", gitSnapshotMaxText+1),
	})

	snap, err := unpackRepoArchive(archive, snapTestSHA)
	if err != nil {
		t.Fatal(err)
	}

	if string(snap.files["a/b.yaml"]) != "kind: B\n" || snap.sizes["big.txt"] != gitSnapshotMaxText+1 || snap.files["big.txt"] != nil {
		t.Errorf("files %v sizes %v", keysOf(snap.files), snap.sizes)
	}

	for p := range snap.sizes {
		if strings.Contains(p, "..") || strings.HasPrefix(p, "owner-repo-abc") {
			t.Errorf("kept %q", p)
		}
	}

	if _, err := unpackRepoArchive([]byte("not a tarball"), snapTestSHA); err == nil {
		t.Error("garbage unpacked")
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}

func TestSearchRepoFilesBounds(t *testing.T) {
	files := map[string][]byte{
		"a.yaml": []byte("image: one\nimage: two\nimage: three\n"),
		"b.yaml": []byte(strings.Repeat("é", gitSearchLineMax+10) + " image\n"),
	}

	res := searchRepoFiles(files, "sha", "IMAGE", 2)
	if len(res.Hits) != 2 || !res.Truncated || res.Hits[1].Text != "image: two" {
		t.Errorf("truncated %+v", res)
	}

	res = searchRepoFiles(files, "sha", "image", 0)
	if len(res.Hits) != 4 || res.Truncated || !strings.HasSuffix(res.Hits[3].Text, "…") || len([]rune(res.Hits[3].Text)) != gitSearchLineMax+1 {
		t.Errorf("clipped %+v", res.Hits)
	}

	if _, err := GitRepoSearch(`{"kind":"demo"}`, "", "main", "  ", 0); err != errGitSearchEmpty {
		t.Errorf("empty query: %v", err)
	}
}

func TestGitRepoSearchDemo(t *testing.T) {
	out, err := GitRepoSearch(`{"kind":"demo"}`, demoClusterRepoURL, demoClusterRepoHead, "demo-worker-1", 0)
	if err != nil {
		t.Fatal(err)
	}

	res := searchHits(t, out)
	if res.SHA != demoClusterRepoHead || len(res.Hits) < 2 || res.Hits[0].Path != "talos/nodes/demo-worker-1.yaml" && res.Hits[0].Path != "talos/talconfig.yaml" {
		t.Errorf("demo %+v", res)
	}

	if out, err := GitRepoSnapshotPrepare(`{"kind":"demo"}`, "", "main"); err != nil || !strings.Contains(out, `"cached":true`) {
		t.Errorf("demo prepare %s %v", out, err)
	}
}

func TestPruneSnapshotFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	for i := range 7 {
		p := filepath.Join(dir, fmt.Sprintf("k%d%s", i, gitSnapshotExt))
		if err := os.WriteFile(p, make([]byte, 10), 0o600); err != nil {
			t.Fatal(err)
		}

		at := now.Add(time.Duration(i) * time.Minute)
		os.Chtimes(p, at, at) //nolint:errcheck
	}

	pruneSnapshotFiles(dir, 5, 1<<20)

	left, _ := filepath.Glob(filepath.Join(dir, "*")) //nolint:errcheck
	if len(left) != 5 {
		t.Fatalf("left %v", left)
	}

	if _, err := os.Stat(filepath.Join(dir, "k0"+gitSnapshotExt)); err == nil {
		t.Error("the oldest is kept")
	}

	pruneSnapshotFiles(dir, 5, 25)

	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 2 { //nolint:errcheck
		t.Errorf("by size, left %v", left)
	}
}

func TestFluxSourceSearchDemo(t *testing.T) {
	out, err := FluxSourceSearch(demoKubeconfigForTest(t), "", "", "GitRepository", "flux-system", "flux-system", "podinfo", 0)
	if err != nil {
		t.Fatal(err)
	}

	if res := searchHits(t, out); res.SHA != demoGitOpsCommit || len(res.Hits) == 0 {
		t.Errorf("flux demo %+v", res)
	}
}
