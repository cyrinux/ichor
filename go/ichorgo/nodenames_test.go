package ichorgo

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func nodeUp(node, hostname, role string) nodeOverview {
	return nodeOverview{Node: node, Hostname: hostname, Role: role, Reachable: true}
}

func nodeDown(node string) nodeOverview {
	return nodeOverview{Node: node, Hostname: node, Role: roleUnknown}
}

var testDataKey = bytes.Repeat([]byte{7}, dataKeySize)

// withDataDir remembers node names in a temporary directory, returned, with testDataKey.
func withDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	SetDataDir(dir, testDataKey)
	t.Cleanup(func() { SetDataDir("", nil) })

	return dir
}

// rememberedHostname is what a down node 10.0.0.3 is given after w-1 was recorded for it.
func rememberedHostname() string {
	nodes := []nodeOverview{nodeDown("10.0.0.3")}
	rememberNodeNames([]string{"fp"}, "fp", nodes)

	return nodes[0].Hostname
}

func TestNodeNamesFileIsEncrypted(t *testing.T) {
	dir := withDataDir(t)
	rememberNodeNames([]string{"fp"}, "fp", []nodeOverview{nodeUp("10.0.0.3", "w-1", "worker")})

	data, err := os.ReadFile(filepath.Join(dir, nodeNamesFile))
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(data, []byte("w-1")) || bytes.Contains(data, []byte("10.0.0.3")) {
		t.Errorf("file holds names or addresses in clear: %q", data)
	}

	if got := rememberedHostname(); got != "w-1" {
		t.Errorf("hostname = %q, want w-1 read back", got)
	}
}

func TestPlaintextNodeNamesFileIsReadThenEncrypted(t *testing.T) {
	dir := withDataDir(t)
	path := filepath.Join(dir, nodeNamesFile)
	if err := os.WriteFile(path, []byte(`{"fp":{"10.0.0.3":{"hostname":"w-1","role":"worker"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := rememberedHostname(); got != "w-1" {
		t.Errorf("hostname = %q, want w-1 from the older plaintext file", got)
	}

	data, _ := os.ReadFile(path)
	if bytes.Contains(data, []byte("w-1")) {
		t.Error("the plaintext file was not rewritten encrypted")
	}
}

func TestNodeNamesFileFromAnotherKeyIsIgnored(t *testing.T) {
	withDataDir(t)
	rememberNodeNames([]string{"fp"}, "fp", []nodeOverview{nodeUp("10.0.0.3", "w-1", "worker")})

	dir := nodeNames.dir
	SetDataDir(dir, bytes.Repeat([]byte{8}, dataKeySize))

	if got := rememberedHostname(); got != "10.0.0.3" {
		t.Errorf("hostname = %q, want the address: a file sealed with a reset key is as good as none", got)
	}
}

func TestRememberNodeNamesWithoutKey(t *testing.T) {
	SetDataDir(t.TempDir(), nil)
	t.Cleanup(func() { SetDataDir("", nil) })

	rememberNodeNames([]string{"fp"}, "fp", []nodeOverview{nodeUp("10.0.0.3", "w-1", "worker")})

	if got := rememberedHostname(); got != "10.0.0.3" {
		t.Errorf("hostname = %q, want the address: nothing is kept without a key", got)
	}
}

func TestRememberNodeNamesFillsDownNode(t *testing.T) {
	withDataDir(t)

	rememberNodeNames([]string{"fp"}, "fp", []nodeOverview{nodeUp("10.0.0.2", "cp-1", "controlplane"), nodeUp("10.0.0.3", "w-1", "worker")})

	nodes := []nodeOverview{nodeUp("10.0.0.2", "cp-1", "controlplane"), nodeDown("10.0.0.3")}
	rememberNodeNames([]string{"fp"}, "fp", nodes)

	if nodes[1].Hostname != "w-1" || nodes[1].Role != "worker" {
		t.Errorf("down node = %+v, want its last known names", nodes[1])
	}

	if nodes[1].Reachable {
		t.Error("filling names must not mark the node reachable")
	}
}

func TestRememberNodeNamesWithoutDataDir(t *testing.T) {
	SetDataDir("", testDataKey)

	rememberNodeNames([]string{"fp"}, "fp", []nodeOverview{nodeUp("10.0.0.3", "w-1", "worker")})

	nodes := []nodeOverview{nodeDown("10.0.0.3")}
	rememberNodeNames([]string{"fp"}, "fp", nodes)

	if nodes[0].Hostname != "10.0.0.3" {
		t.Errorf("hostname = %q, want the address: nothing is remembered without a data dir", nodes[0].Hostname)
	}
}

func TestRememberNodeNamesUnknownNode(t *testing.T) {
	withDataDir(t)

	nodes := []nodeOverview{nodeDown("10.0.0.9")}
	rememberNodeNames([]string{"fp"}, "fp", nodes)

	if nodes[0].Hostname != "10.0.0.9" || nodes[0].Role != roleUnknown {
		t.Errorf("never seen node = %+v, want it unchanged", nodes[0])
	}
}

func TestMergeKnownNodes(t *testing.T) {
	saved := knownNodes{
		"fp":      {"10.0.0.2": {Hostname: "cp-1", Role: "controlplane"}, "10.0.0.4": {Hostname: "old"}},
		"other":   {"10.1.0.2": {Hostname: "x"}},
		"removed": {"10.2.0.2": {Hostname: "y"}},
	}

	// 10.0.0.2 answered without its hostname; 10.0.0.4 left the talosconfig.
	nodes := []nodeOverview{
		{Node: "10.0.0.2", Hostname: "10.0.0.2", Role: "controlplane", Reachable: true},
		nodeUp("10.0.0.3", "w-1", "worker"),
	}

	got := mergeKnownNodes(saved, []string{"fp", "other"}, "fp", nodes)
	want := knownNodes{
		"fp":    {"10.0.0.2": {Hostname: "cp-1", Role: "controlplane"}, "10.0.0.3": {Hostname: "w-1", Role: "worker"}},
		"other": {"10.1.0.2": {Hostname: "x"}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestMergeKnownNodesRenamedNode(t *testing.T) {
	saved := knownNodes{"fp": {"10.0.0.2": {Hostname: "old", Role: "worker"}}}

	got := mergeKnownNodes(saved, []string{"fp"}, "fp", []nodeOverview{nodeUp("10.0.0.2", "new", "worker")})

	if got["fp"]["10.0.0.2"].Hostname != "new" {
		t.Errorf("hostname = %q, want the one the node says now", got["fp"]["10.0.0.2"].Hostname)
	}
}
