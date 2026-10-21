package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// tooLargeArchive holds one file more than a snapshot unpacks.
func tooLargeArchive(t *testing.T) []byte {
	t.Helper()

	files := make(map[string]string, fluxArtifactMaxFiles+1)
	for i := range fluxArtifactMaxFiles + 1 {
		files[fmt.Sprintf("vendor/f%05d.txt", i)] = ""
	}

	return testArchive(t, "example-org-cluster-config-0123456", files)
}

// talosForgeRoutes is a GitHub repository too large for a snapshot, with a Talos directory
// read through the contents API: talos/{patches/{dns,deep/x},nodes/cp-1}.yaml, and a level
// below deep/ that the walk must not ask for.
func talosForgeRoutes(t *testing.T, treeCalls *atomic.Int32) map[string]func(w http.ResponseWriter, r *http.Request) {
	t.Helper()

	archive := tooLargeArchive(t)
	codeload := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) })) //nolint:errcheck
	t.Cleanup(codeload.Close)

	const repo = "/api/v3/repos/example-org/cluster-config"

	tree := func(body string) func(w http.ResponseWriter, r *http.Request) {
		return func(w http.ResponseWriter, r *http.Request) {
			treeCalls.Add(1)

			if r.URL.Query().Get("ref") != snapTestSHA {
				t.Errorf("tree read at %q, want the commit", r.URL.RawQuery)
			}

			jsonAnswer(body)(w, r)
		}
	}
	file := func(path, content string) func(w http.ResponseWriter, r *http.Request) {
		return jsonAnswer(`{"type":"file","path":"` + path + `","sha":"b10b","size":` + fmt.Sprint(len(content)) +
			`,"encoding":"base64","content":"` + b64Content(content) + `"}`)
	}

	return map[string]func(w http.ResponseWriter, r *http.Request){
		repo + "/commits/main": jsonAnswer(`{"sha":"` + snapTestSHA + `"}`),
		repo + "/tarball/" + snapTestSHA: func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, codeload.URL+"/archive.tar.gz", http.StatusFound)
		},
		repo + "/contents/talos": tree(`[
			{"type":"dir","path":"talos/patches","size":0},
			{"type":"dir","path":"talos/nodes","size":0},
			{"type":"file","path":"talos/README.md","size":20}]`),
		repo + "/contents/talos/patches": tree(`[
			{"type":"file","path":"talos/patches/dns.yaml","size":50},
			{"type":"dir","path":"talos/patches/deep","size":0}]`),
		repo + "/contents/talos/patches/deep": tree(`[
			{"type":"file","path":"talos/patches/deep/x.yaml","size":40},
			{"type":"dir","path":"talos/patches/deep/deeper","size":0}]`),
		repo + "/contents/talos/nodes":               tree(`[{"type":"file","path":"talos/nodes/cp-1.yaml","size":40}]`),
		repo + "/contents/talos/patches/dns.yaml":    file("talos/patches/dns.yaml", "machine:\n  network:\n    nameservers: [192.0.2.1]\n"),
		repo + "/contents/talos/patches/deep/x.yaml": file("talos/patches/deep/x.yaml", "machine:\n  kubelet:\n    extraArgs: {max-pods: \"150\"}\n"),
		repo + "/contents/talos/nodes/cp-1.yaml":     file("talos/nodes/cp-1.yaml", "machine:\n  network:\n    hostname: cp-1\n"),
	}
}

func TestTalosRepoFilesTooLargeReadsTheTalosDirectory(t *testing.T) {
	resetSnapshots(t)

	var treeCalls atomic.Int32

	srv := gitServer(t, talosForgeRoutes(t, &treeCalls))
	host, repo := gitHostJSON(gitForgeGitHub, srv.URL), "https://127.0.0.1/example-org/cluster-config"

	out, err := TalosRepoFiles(host, repo, "main", "/talos/", "cp-1", "controlplane")
	if err != nil {
		t.Fatal(err)
	}

	var found talosRepoFiles
	if err := json.Unmarshal([]byte(out), &found); err != nil {
		t.Fatal(err)
	}

	want := []string{"shared talos/patches/deep/x.yaml", "shared talos/patches/dns.yaml", "node talos/nodes/cp-1.yaml"}
	if got := kinds(found); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("files %v, want %v", got, want)
	}

	if found.SHA != snapTestSHA || !strings.HasPrefix(found.Note, noteTalosDirOnly) {
		t.Errorf("sha %q, note %q", found.SHA, found.Note)
	}

	// talos, patches, deep, nodes: deeper is three levels down and never asked for.
	if n := treeCalls.Load(); n != 4 {
		t.Errorf("%d tree calls, want 4", n)
	}
}

func TestTalosRepoFilesTooLargeWithoutTalosDirectory(t *testing.T) {
	resetSnapshots(t)

	var treeCalls atomic.Int32

	srv := gitServer(t, talosForgeRoutes(t, &treeCalls))
	host, repo := gitHostJSON(gitForgeGitHub, srv.URL), "https://127.0.0.1/example-org/cluster-config"

	_, err := TalosRepoFiles(host, repo, "main", " / ", "cp-1", "controlplane")
	if err == nil || !strings.Contains(err.Error(), "set its Talos directory") {
		t.Errorf("blank Talos directory: %v", err)
	}

	if treeCalls.Load() != 0 {
		t.Error("the repository was walked without a Talos directory")
	}
}

func TestTalosDirReaderBoundsTheFiles(t *testing.T) {
	entries := make([]string, talosDirMaxFiles+1)
	routes := map[string]func(w http.ResponseWriter, r *http.Request){}

	for i := range entries {
		p := fmt.Sprintf("talos/n%03d.yaml", i)
		entries[i] = `{"type":"file","path":"` + p + `","size":10}`
		routes["/api/v3/repos/example-org/cluster-config/contents/"+p] = jsonAnswer(`{"type":"file","path":"` + p + `","encoding":"base64","content":"` + b64Content("machine: {}\n") + `"}`)
	}

	routes["/api/v3/repos/example-org/cluster-config/contents/talos"] = jsonAnswer("[" + strings.Join(entries, ",") + "]")

	srv := gitServer(t, routes)
	h, err := parseGitForgeHost(gitHostJSON(gitForgeGitHub, srv.URL))
	if err != nil {
		t.Fatal(err)
	}

	repo := gitRepoRef{host: h.host, owner: "example-org", name: "cluster-config"}

	_, err = talosDirReader("talos")(t.Context(), newGitForge(h), repo, snapTestSHA)
	if err == nil || !strings.Contains(err.Error(), "more than 200 YAML files") {
		t.Errorf("201 files: %v", err)
	}
}

// Compare on a repository too large for a snapshot reads the chosen files only: an inline
// talconfig patch's file once, a missing one left for loadTalosPatches to skip.
func TestTalosPathsReaderReadsTheChosenFiles(t *testing.T) {
	resetSnapshots(t)

	var treeCalls atomic.Int32

	srv := gitServer(t, talosForgeRoutes(t, &treeCalls))
	host, repo := gitHostJSON(gitForgeGitHub, srv.URL), "https://127.0.0.1/example-org/cluster-config"

	paths := []string{"talos/patches/dns.yaml", "talos/nodes/cp-1.yaml", "talos/nodes/cp-1.yaml" + talosInlineMark + "3", "talos/gone.yaml"}

	read, err := readTalosRepo(host, repo, "main", talosPathsReader(paths))
	if err != nil {
		t.Fatal(err)
	}

	if len(read.files) != 2 || read.sha != snapTestSHA || read.note != noteTalosDirOnly || treeCalls.Load() != 0 {
		t.Errorf("read %d files %v at %q, note %q, %d tree calls", len(read.files), keysOf(read.files), read.sha, read.note, treeCalls.Load())
	}

	patches, used, skipped := loadTalosPatches(read.files, paths[:2])
	if len(patches) != 2 || len(used) != 2 || len(skipped) != 0 {
		t.Errorf("patches %d, used %v, skipped %v", len(patches), used, skipped)
	}

	if _, _, skipped := loadTalosPatches(read.files, paths[3:]); len(skipped) != 1 || !strings.Contains(skipped[0].Why, "not in the repository") {
		t.Errorf("missing file: %v", skipped)
	}
}
