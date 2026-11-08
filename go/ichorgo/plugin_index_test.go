package ichorgo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyrinux/ichor/go/plugin"
)

// pluginIndexServer serves an index at /index.json, its plugin file at /plugin.yaml and counts
// the index downloads. Each test starts with an empty index cache.
type pluginIndexServer struct {
	*httptest.Server

	indexHits int
	index     string
	file      string
}

func newPluginIndexServer(t *testing.T) *pluginIndexServer {
	t.Helper()

	s := &pluginIndexServer{file: kubeGreenCopy(t)}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			s.indexHits++
			_, _ = w.Write([]byte(s.index))
		case "/plugin.yaml":
			_, _ = w.Write([]byte(s.file))
		case "/big.json":
			_, _ = w.Write([]byte(strings.Repeat(" ", plugin.MaxIndexSize+1)))
		case "/plain":
			http.Redirect(w, r, "http://example.com/index.json", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)

	pluginFetchClient = s.Client
	t.Cleanup(func() { pluginFetchClient = defaultPluginFetchClient })

	forgetPluginIndex()
	t.Cleanup(forgetPluginIndex)

	s.index = s.indexOf(sha256Hex([]byte(s.file)))

	return s
}

// indexOf is an index of two entries: the plugin file with this digest, and a built-in's id.
func (s *pluginIndexServer) indexOf(digest string) string {
	return fmt.Sprintf(`{"schemaVersion":%q,"generatedAt":"2026-01-02T03:04:05Z","plugins":[
		{"id":"kube-green-copy","name":"Sleep","description":"Sleep schedules","version":"v1","url":%q,"sha256":%q,
		 "resources":["sleepinfos.kube-green.com"],"icon":"kube-green","website":"https://example.com","maintainer":"demo-maintainer"},
		{"id":"kube-green","name":"kube-green","url":%q,"sha256":%q}]}`,
		plugin.IndexSchemaVersion, s.URL+"/plugin.yaml", digest, s.URL+"/plugin.yaml", digest)
}

func decodeIndexAnswer(t *testing.T, out string) pluginIndexAnswer {
	t.Helper()

	var got pluginIndexAnswer
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v in %s", err, out)
	}

	return got
}

func TestPluginIndexFetchFlagsInstalledAndBuiltin(t *testing.T) {
	withPluginStore(t)

	s := newPluginIndexServer(t)

	out, err := PluginIndexFetch(s.URL + "/index.json")
	if err != nil {
		t.Fatal(err)
	}

	got := decodeIndexAnswer(t, out)
	if got.URL != s.URL+"/index.json" || len(got.Plugins) != 2 {
		t.Fatalf("answer: %+v", got)
	}

	first, second := got.Plugins[0], got.Plugins[1]
	if first.Installed || first.Builtin || !second.Builtin || second.Installed {
		t.Fatalf("flags before the import: %+v %+v", first, second)
	}

	if first.Maintainer != "demo-maintainer" || first.Website != "https://example.com" || first.Resources[0] != "sleepinfos.kube-green.com" {
		t.Fatalf("entry: %+v", first)
	}

	if _, err := PluginUserImport(s.file); err != nil {
		t.Fatal(err)
	}

	out, err = PluginIndexFetch(s.URL + "/index.json")
	if err != nil {
		t.Fatal(err)
	}

	if !decodeIndexAnswer(t, out).Plugins[0].Installed {
		t.Fatalf("not installed after the import: %s", out)
	}
}

func TestPluginIndexFetchRefuses(t *testing.T) {
	withPluginStore(t)

	s := newPluginIndexServer(t)
	digest := sha256Hex([]byte(s.file))

	cases := []struct {
		name, url, index, want string
	}{
		{"plain http", "http://example.com/index.json", "", "https"},
		{"a file", "file:///etc/passwd", "", "https"},
		{"redirect to http", s.URL + "/plain", "", "https"},
		{"over the cap", s.URL + "/big.json", "", "256 KiB"},
		{"not found", s.URL + "/missing.json", "", "404"},
		{"not JSON", s.URL + "/index.json", "<html>", "not a plugin index"},
		{"another format", s.URL + "/index.json", `{"schemaVersion":"ichor.dev/plugin-index/v9","plugins":[]}`, "format"},
		{"http entry", s.URL + "/index.json", indexWithEntry("demo", "http://example.com/a.yaml", digest), "https"},
		{"no digest", s.URL + "/index.json", indexWithEntry("demo", "https://example.com/a.yaml", ""), "sha256"},
		{"bad digest", s.URL + "/index.json", indexWithEntry("demo", "https://example.com/a.yaml", "abc"), "sha256"},
		{"bad id", s.URL + "/index.json", indexWithEntry("../evil", "https://example.com/a.yaml", digest), "id"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forgetPluginIndex()

			s.index = tc.index
			if _, err := PluginIndexFetch(tc.url); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v, want %q", err, tc.want)
			}
		})
	}
}

func indexWithEntry(id, url, digest string) string {
	return fmt.Sprintf(`{"schemaVersion":%q,"plugins":[{"id":%q,"name":"x","url":%q,"sha256":%q}]}`, plugin.IndexSchemaVersion, id, url, digest)
}

// What the apps would act on is cleaned: a website that is not https is no link, and an icon is
// a bundled icon's name, never a path or an address.
func TestPluginIndexFetchDropsUnsafeLinks(t *testing.T) {
	withPluginStore(t)

	s := newPluginIndexServer(t)
	s.index = fmt.Sprintf(`{"schemaVersion":%q,"plugins":[{"id":"demo","name":"x","url":"https://example.com/a.yaml","sha256":%q,
		"website":"javascript:alert(1)","icon":"https://example.com/track.png"}]}`, plugin.IndexSchemaVersion, strings.Repeat("a", 64))

	out, err := PluginIndexFetch(s.URL + "/index.json")
	if err != nil {
		t.Fatal(err)
	}

	got := decodeIndexAnswer(t, out).Plugins[0]
	if got.Website != "" || got.Icon != "" {
		t.Fatalf("unsafe links kept: %+v", got)
	}
}

func TestPluginIndexFetchCachesForAnHour(t *testing.T) {
	withPluginStore(t)

	s := newPluginIndexServer(t)

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	pluginNow = func() time.Time { return now }

	t.Cleanup(func() { pluginNow = time.Now })

	fetch := func() {
		t.Helper()

		if _, err := PluginIndexFetch(s.URL + "/index.json"); err != nil {
			t.Fatal(err)
		}
	}

	fetch()
	now = now.Add(59 * time.Minute)
	fetch()

	if s.indexHits != 1 {
		t.Fatalf("%d downloads within the hour", s.indexHits)
	}

	now = now.Add(2 * time.Minute)
	fetch()

	if s.indexHits != 2 {
		t.Fatalf("%d downloads after the hour", s.indexHits)
	}
}

// A failure is not kept: the next try downloads again.
func TestPluginIndexFetchDoesNotCacheAFailure(t *testing.T) {
	withPluginStore(t)

	s := newPluginIndexServer(t)
	good := s.index
	s.index = "<html>"

	if _, err := PluginIndexFetch(s.URL + "/index.json"); err == nil {
		t.Fatal("no error")
	}

	s.index = good

	if _, err := PluginIndexFetch(s.URL + "/index.json"); err != nil {
		t.Fatal(err)
	}
}

// The file comes back only when it has the published digest, and nothing is stored: the
// consent sheet still decides (PluginUserCheck, then PluginUserImport).
func TestPluginIndexDownloadVerifiesAndStoresNothing(t *testing.T) {
	withPluginStore(t)

	s := newPluginIndexServer(t)

	out, err := PluginIndexDownload(s.URL+"/plugin.yaml", " "+strings.ToUpper(sha256Hex([]byte(s.file)))+" ")
	if err != nil || out != s.file {
		t.Fatalf("download: %v (%d bytes)", err, len(out))
	}

	list, err := PluginUserList()
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(list, "kube-green-copy") {
		t.Fatalf("stored before the consent: %s", list)
	}
}

func TestPluginIndexDownloadRefuses(t *testing.T) {
	withPluginStore(t)

	s := newPluginIndexServer(t)
	digest := sha256Hex([]byte(s.file))

	cases := []struct{ name, url, digest, want string }{
		{"changed file", s.URL + "/plugin.yaml", strings.Repeat("0", 64), "the file changed since the index was published"},
		{"no digest", s.URL + "/plugin.yaml", "", "sha256"},
		{"short digest", s.URL + "/plugin.yaml", digest[:10], "sha256"},
		{"plain http", "http://example.com/plugin.yaml", digest, "https"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if out, err := PluginIndexDownload(tc.url, tc.digest); err == nil || !strings.Contains(err.Error(), tc.want) || out != "" {
				t.Fatalf("out %d bytes, err %v, want %q", len(out), err, tc.want)
			}
		})
	}
}

// After a digest mismatch the cached index is suspect: the next read downloads it again.
func TestPluginIndexMismatchForgetsTheCachedIndex(t *testing.T) {
	withPluginStore(t)

	s := newPluginIndexServer(t)

	if _, err := PluginIndexFetch(s.URL + "/index.json"); err != nil {
		t.Fatal(err)
	}

	if _, err := PluginIndexDownload(s.URL+"/plugin.yaml", strings.Repeat("0", 64)); err == nil {
		t.Fatal("no error")
	}

	if _, err := PluginIndexFetch(s.URL + "/index.json"); err != nil {
		t.Fatal(err)
	}

	if s.indexHits != 2 {
		t.Fatalf("%d downloads", s.indexHits)
	}
}
