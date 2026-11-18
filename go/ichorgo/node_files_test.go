package ichorgo

import (
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
)

// filesNode is a node with a small tree: a pod log, the machine config, a binary file and a
// symlink-free /etc.
func filesNode(t *testing.T, f *fakeTalos, node string, tree map[string]string) {
	t.Helper()

	f.addNode(t, node, "v1.14.0", machine.TypeWorker)

	f.mu.Lock()
	if f.files == nil {
		f.files = map[string]map[string]string{}
	}
	f.files[node] = tree
	f.mu.Unlock()
}

var testNodeTree = map[string]string{
	"/var/log/pods/shop_web-1_uid/web/0.log": "line one\nline two\n",
	"/var/log/containers.log":                "c\n",
	"/var/log/lastlog":                       "\x00\x01\x02bin",
	"/etc/resolv.conf":                       "nameserver 10.96.0.10\n",
	"/system/state/config.yaml":              "not: [a machine config\ntoken: dGhpcyBpcyBhIHZlcnkgbG9uZyBzZWNyZXQgdG9rZW4gdmFsdWUgMTIzNDU2Nzg5MA==\n",
}

func TestNodeFilesListsOneLevelDirectoriesFirst(t *testing.T) {
	const node = "192.0.2.91"

	f := newFakeTalos()
	filesNode(t, f, node, testNodeTree)
	cfg := f.start(t, node)

	out, err := NodeFiles(cfg, "fake", node, "")
	got := decodeJSON[nodeDirListing](t, out, err)

	if got.Dir != "/var/log" || got.Truncated || !slices.Equal(got.Roots, nodeFileRoots) {
		t.Fatalf("listing %+v", got)
	}

	want := []nodeFileEntry{
		{Name: "pods", Path: "/var/log/pods", Type: "dir", Modified: "2026-01-02T03:04:05Z"},
		{Name: "containers.log", Path: "/var/log/containers.log", Type: "file", Size: 2, Modified: "2026-01-02T03:04:05Z"},
		{Name: "lastlog", Path: "/var/log/lastlog", Type: "file", Size: 6, Modified: "2026-01-02T03:04:05Z"},
	}
	if !slices.Equal(got.Entries, want) {
		t.Errorf("entries = %+v", got.Entries)
	}

	// Descends into a pod's log directory.
	out, err = NodeFiles(cfg, "fake", node, "/var/log/pods/shop_web-1_uid/web/")
	got = decodeJSON[nodeDirListing](t, out, err)

	if got.Dir != "/var/log/pods/shop_web-1_uid/web" || len(got.Entries) != 1 || got.Entries[0].Path != "/var/log/pods/shop_web-1_uid/web/0.log" {
		t.Errorf("pod dir = %+v", got)
	}

	if !strings.Contains(out, `"target":""`) {
		t.Errorf("every key is written: %s", out)
	}
}

func TestNodeFileReadTextBinaryAndRedaction(t *testing.T) {
	const node = "192.0.2.92"

	f := newFakeTalos()
	filesNode(t, f, node, testNodeTree)
	cfg := f.start(t, node)

	out, err := NodeFileRead(cfg, "fake", node, "/var/log/pods/shop_web-1_uid/web/0.log")
	text := decodeJSON[nodeFileText](t, out, err)

	if text.Text != "line one\nline two\n" || text.Binary || text.Truncated || text.Redacted || text.Size != 18 {
		t.Errorf("log = %+v", text)
	}

	out, err = NodeFileRead(cfg, "fake", node, "/var/log/lastlog")
	bin := decodeJSON[nodeFileText](t, out, err)

	if !bin.Binary || bin.Text != "" || bin.Size != 6 {
		t.Errorf("binary = %+v", bin)
	}

	// A config that does not parse still loses its credential-looking strings.
	out, err = NodeFileRead(cfg, "fake", node, machineConfigPath)
	conf := decodeJSON[nodeFileText](t, out, err)

	if !conf.Redacted || strings.Contains(conf.Text, "dGhpcyBpcyBh") || !strings.Contains(conf.Text, "<redacted>") {
		t.Errorf("config = %+v", conf)
	}
}

func TestNodeFilesRefusals(t *testing.T) {
	const node = "192.0.2.93"

	f := newFakeTalos()
	filesNode(t, f, node, testNodeTree)
	f.mu.Lock()
	f.denied = map[string]bool{"/etc": true, "/etc/resolv.conf": true}
	f.mu.Unlock()

	cfg := f.start(t, node)

	if _, err := NodeFiles(cfg, "fake", node, "/etc"); err == nil || !strings.Contains(err.Error(), "role too limited") {
		t.Errorf("a refused directory: %v", err)
	}

	if _, err := NodeFileRead(cfg, "fake", node, "/etc/resolv.conf"); err == nil {
		t.Error("a refused file reads")
	}

	if _, err := NodeFiles(cfg, "fake", node, "/nope"); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("missing dir: %v", err)
	}

	for _, p := range []string{"var/log", "/var/log/../../etc", ""} {
		if _, err := NodeFileRead(cfg, "fake", node, p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}

	// The bad paths are refused before any call to the node: only resolv.conf was read.
	if calls := f.called("Read"); len(calls) != 1 {
		t.Errorf("reads = %v", calls)
	}
}

func TestReadNodeFileCapsAndKeepsUTF8(t *testing.T) {
	big := strings.Repeat("é", nodeFileMaxBytes) // 2 bytes each: the cap falls inside a rune

	got, err := readNodeFile("/var/log/big.log", strings.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}

	if !got.Truncated || got.Binary || got.Size != nodeFileMaxBytes || !strings.HasPrefix(got.Text, "éé") {
		t.Fatalf("big = truncated %v binary %v size %d", got.Truncated, got.Binary, got.Size)
	}

	if !utf8.ValidString(got.Text) {
		t.Error("the cut left invalid UTF-8")
	}

	// A /proc file: size 0 in the listing, read until EOF.
	got, _ = readNodeFile("/proc/net/route", strings.NewReader("Iface\tDestination\n"))
	if got.Text != "Iface\tDestination\n" || got.Truncated {
		t.Errorf("proc = %+v", got)
	}

	// Latin-1 text is not UTF-8: binary.
	if got, _ := readNodeFile("/etc/legacy", strings.NewReader("caf\xe9 au lait")); !got.Binary {
		t.Errorf("latin-1 = %+v", got)
	}
}

func TestBuildNodeDirListingCapsAndSkipsTheRoot(t *testing.T) {
	infos := []*machineapi.FileInfo{
		{Name: "/var/log", RelativeName: ".", IsDir: true},
		{Name: "/var/log/a/deeper.log", RelativeName: "a/deeper.log"},
		{Name: "/var/log/broken", Error: "lstat: permission denied"},
		{Name: "/var/log/current", Mode: uint32(fs.ModeSymlink), Link: "/var/log/a"},
	}
	for i := range nodeFilesMaxEntries + 5 {
		infos = append(infos, &machineapi.FileInfo{Name: "/var/log/f" + strconv.Itoa(i), Size: int64(i)})
	}

	listing := buildNodeDirListing("/var/log", infos)
	if !listing.Truncated || len(listing.Entries) != nodeFilesMaxEntries {
		t.Errorf("cap: truncated %v, %d entries", listing.Truncated, len(listing.Entries))
	}

	link := listing.Entries[slices.IndexFunc(listing.Entries, func(e nodeFileEntry) bool { return e.Name == "current" })]
	if link.Type != "link" || link.Target != "/var/log/a" || link.Modified != "" {
		t.Errorf("link = %+v", link)
	}

	for _, e := range listing.Entries {
		if e.Name == "broken" || e.Name == "deeper.log" || e.Path == "/var/log" {
			t.Errorf("kept %+v", e)
		}
	}
}

func TestNodeFilesDemo(t *testing.T) {
	cfg := demoConfigForTest(t)
	node := demoNodes()[0].Node

	out, err := NodeFiles(cfg, "", node, "/var/log")
	got := decodeJSON[nodeDirListing](t, out, err)

	if len(got.Entries) < 3 || got.Entries[0].Type != "dir" {
		t.Fatalf("demo /var/log = %+v", got.Entries)
	}

	out, err = NodeFiles(cfg, "", node, demoPodLogDir+"/web")
	pod := decodeJSON[nodeDirListing](t, out, err)

	if len(pod.Entries) != 1 || pod.Entries[0].Name != "0.log" {
		t.Fatalf("demo pod dir = %+v", pod)
	}

	out, err = NodeFileRead(cfg, "", node, pod.Entries[0].Path)
	if log := decodeJSON[nodeFileText](t, out, err); !strings.Contains(log.Text, "listening on :8080") || log.Binary {
		t.Errorf("demo log = %+v", log)
	}

	out, err = NodeFileRead(cfg, "", node, "/var/log/lastlog")
	if bin := decodeJSON[nodeFileText](t, out, err); !bin.Binary {
		t.Errorf("demo binary = %+v", bin)
	}

	out, err = NodeFileRead(cfg, "", node, machineConfigPath)
	if conf := decodeJSON[nodeFileText](t, out, err); !conf.Redacted || !strings.Contains(conf.Text, "******") {
		t.Errorf("demo config = %+v", conf)
	}

	for _, root := range nodeFileRoots {
		if _, err := NodeFiles(cfg, "", node, root); err != nil {
			t.Errorf("demo root %s: %v", root, err)
		}
	}

	if _, err := NodeFiles(cfg, "", node, "/nope"); err == nil {
		t.Error("demo lists a missing directory")
	}

	if _, err := NodeFiles(cfg, "", "192.0.2.250", "/var/log"); err == nil {
		t.Error("demo lists a node outside the demo")
	}
}
