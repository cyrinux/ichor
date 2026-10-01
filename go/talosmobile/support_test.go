package talosmobile

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
)

func fileStep(name, file, content string) bundleStep {
	return bundleStep{name, func(context.Context) ([]bundleFile, error) {
		return []bundleFile{{file, []byte(content)}}, nil
	}}
}

func readZip(t *testing.T, path string) map[string]string {
	t.Helper()

	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}

	defer zr.Close() //nolint:errcheck

	out := map[string]string{}

	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}

		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}

		_ = rc.Close() //nolint:errcheck

		out[f.Name] = string(b)
	}

	return out
}

func TestWriteBundle(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "support.zip")

	sections := []bundleSection{
		{node: "10.0.0.2", dir: "10.0.0.2", reachable: func(context.Context) error { return nil }, steps: []bundleStep{
			fileStep("kernel log", "dmesg.log", "boot\n"),
			{"service logs", func(context.Context) ([]bundleFile, error) {
				// A partial failure keeps what was collected.
				return []bundleFile{{"service-logs/kubelet.log", []byte("k\n")}}, errors.New("etcd: permission denied")
			}},
		}},
		{node: "10.0.0.3", dir: "10.0.0.3", reachable: func(context.Context) error { return errors.New("unreachable: no answer") }, steps: []bundleStep{
			{"kernel log", func(context.Context) ([]bundleFile, error) {
				t.Error("a step ran on an unreachable node")

				return nil, nil
			}},
		}},
		{dir: "cluster", steps: []bundleStep{fileStep("etcd", "etcd.json", "{}\n")}},
	}

	var progress []supportProgress

	size, err := writeBundle(context.Background(), dest, sections, func(p supportProgress) { progress = append(progress, p) }, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dest)
	if err != nil || info.Size() != size || size == 0 {
		t.Fatalf("size = %d, stat = %v, %v", size, info, err)
	}

	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Error("the .part file was left behind")
	}

	files := readZip(t, dest)

	want := map[string]string{
		"10.0.0.2/dmesg.log":                "boot\n",
		"10.0.0.2/service-logs/kubelet.log": "k\n",
		"10.0.0.2/service_logs.error.txt":   "etcd: permission denied\n",
		"10.0.0.3/unreachable.txt":          "unreachable: no answer\n",
		"cluster/etcd.json":                 "{}\n",
	}

	for name, content := range want {
		if files[name] != content {
			t.Errorf("%s = %q, want %q", name, files[name], content)
		}
	}

	if len(files) != len(want)+1 {
		t.Errorf("files = %v", files)
	}

	summary := files["summary.txt"]
	for _, part := range []string{"node: 10.0.0.2", "node: 10.0.0.3", "10.0.0.2/service logs: etcd: permission denied", "10.0.0.3: skipped", "2023-11-14T22:13:20Z"} {
		if !strings.Contains(summary, part) {
			t.Errorf("summary lacks %q:\n%s", part, summary)
		}
	}

	wantProgress := []supportProgress{
		{"10.0.0.2", "kernel log", 0, 2}, {"10.0.0.2", "service logs", 1, 2}, {"10.0.0.2", "done", 2, 2},
		{"10.0.0.3", "kernel log", 0, 1}, {"10.0.0.3", "done", 1, 1},
		{"", "etcd", 0, 1}, {"", "done", 1, 1},
	}

	if !slices.Equal(progress, wantProgress) {
		t.Errorf("progress = %+v", progress)
	}

	b, _ := json.Marshal(progress[0]) //nolint:errcheck
	if string(b) != `{"node":"10.0.0.2","step":"kernel log","done":0,"total":2}` {
		t.Errorf("progress JSON = %s", b)
	}
}

func TestWriteBundleCancelLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "support.zip")

	ctx, cancel := context.WithCancel(context.Background())

	sections := []bundleSection{{node: "n", dir: "n", steps: []bundleStep{
		{"first", func(context.Context) ([]bundleFile, error) {
			cancel()

			return []bundleFile{{"a.txt", []byte("a")}}, nil
		}},
		{"second", func(context.Context) ([]bundleFile, error) {
			t.Error("a step ran after the cancel")

			return nil, nil
		}},
	}}}

	size, err := writeBundle(ctx, dest, sections, func(supportProgress) {}, time.Now())
	if err == nil || err.Error() != "cancelled" || size != 0 {
		t.Errorf("size = %d, err = %v", size, err)
	}

	left, _ := os.ReadDir(dir) //nolint:errcheck
	if len(left) != 0 {
		t.Errorf("files left: %v", left)
	}

	// An unwritable destination fails cleanly too.
	if _, err := writeBundle(context.Background(), filepath.Join(dir, "missing", "s.zip"), nil, func(supportProgress) {}, time.Now()); err == nil {
		t.Error("no error for an unwritable path")
	}
}

func TestSupportTargets(t *testing.T) {
	nodes := []string{"10.0.0.2", "10.0.0.3"}

	if got, err := supportTargets(nodes, ""); err != nil || !slices.Equal(got, nodes) {
		t.Errorf("all: %v, %v", got, err)
	}

	if got, err := supportTargets(nodes, " 10.0.0.3 , ,10.0.0.3"); err != nil || !slices.Equal(got, []string{"10.0.0.3"}) {
		t.Errorf("one: %v, %v", got, err)
	}

	if _, err := supportTargets(nodes, "10.0.0.9"); err == nil {
		t.Error("a node outside the context was accepted")
	}

	if _, err := supportTargets(nil, ""); err == nil {
		t.Error("no node accepted")
	}
}

func TestBundleNeverTakesSensitiveResources(t *testing.T) {
	if bundleResource(&meta.ResourceDefinitionSpec{Type: "MachineConfigs.config.talos.dev", Sensitivity: meta.Sensitive}) {
		t.Error("a sensitive resource type would be bundled")
	}

	if !bundleResource(&meta.ResourceDefinitionSpec{Type: "LinkStatuses.net.talos.dev"}) {
		t.Error("a plain resource type is skipped")
	}
}

func TestBundleFileName(t *testing.T) {
	for in, want := range map[string]string{
		"10.0.0.2": "10.0.0.2", "fd00::2": "fd00__2", "kube-system/pod:c": "kube-system_pod_c", "..": "unnamed", "": "unnamed",
		"service logs": "service_logs",
	} {
		if got := bundleFileName(in); got != want {
			t.Errorf("bundleFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadLogStream(t *testing.T) {
	chunks := []string{"aaaa", "bbbb", "cccc", "dddd"}
	i := 0

	recv := func() (*common.Data, error) {
		if i == len(chunks) {
			return nil, io.EOF
		}

		i++

		return &common.Data{Bytes: []byte(chunks[i-1])}, nil
	}

	// Only the end is kept once more than twice the limit was read.
	got, err := readLogStream(recv, 4)
	if err != nil || !strings.HasSuffix(string(got), "dddd") || len(got) > 8 {
		t.Errorf("got %q, %v", got, err)
	}

	failing := func() (*common.Data, error) {
		return &common.Data{Metadata: &common.Metadata{Error: "no such service"}}, nil
	}
	if _, err := readLogStream(failing, 4); err == nil || err.Error() != "no such service" {
		t.Errorf("err = %v", err)
	}
}

type recordingSupportListener struct {
	progress []string
	path     string
	err      string
}

func (l *recordingSupportListener) OnProgress(json string) { l.progress = append(l.progress, json) }
func (l *recordingSupportListener) OnDone(path string, _ int64, errMessage string) {
	l.path, l.err = path, errMessage
}

func TestMaskedSupportListener(t *testing.T) {
	SetPrivacyMask(true, "")
	defer SetPrivacyMask(false, "")

	rec := &recordingSupportListener{}
	l := maskedSupportListener{rec}

	l.OnProgress(`{"node":"192.168.77.12","step":"mounts","done":1,"total":9}`)
	l.OnDone("/data/192.168.77.12/support.zip", 10, "node 192.168.77.12: timed out")

	if strings.Contains(rec.progress[0], "192.168.77.12") || strings.Contains(rec.err, "192.168.77.12") {
		t.Errorf("not masked: %v / %q", rec.progress, rec.err)
	}

	// The path is the app's own file: it must come back untouched.
	if rec.path != "/data/192.168.77.12/support.zip" {
		t.Errorf("path = %q", rec.path)
	}
}

func TestScrubMachineConfig(t *testing.T) {
	// As left by Talos's RedactSecrets: its own secrets are already "******".
	in := `version: v1alpha1
machine:
  type: controlplane
  token: '******'
  ca:
    crt: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t
    key: '******'
  env:
    https_proxy: http://user:hunter2@proxy:3128
  files:
    - content: |
        api_key = sk-live-123
      path: /var/etc/app.conf
      permissions: 0o600
      op: create
  registries:
    config:
      registry.example.com:
        auth:
          username: bob
          password: hunter2
        tls:
          clientIdentity:
            crt: Q0VSVA==
            key: S0VZ
  network:
    interfaces:
      - interface: wg0
        wireguard:
          privateKey: aGVsbG8=
          peers:
            - publicKey: cHVibGlj
  systemDiskEncryption:
    state:
      provider: luks2
      keys:
        - static:
            passphrase: correct horse
          slot: 0
  kubelet:
    image: ghcr.io/siderolabs/kubelet:v1.34.0
cluster:
  secret: '******'
  inlineManifests:
    - name: creds
      contents: |
        kind: Secret
        stringData: {pw: hunter2}
  extraManifestHeaders:
    Authorization: Bearer abc123
  apiServer:
    image: registry.k8s.io/kube-apiserver:v1.34.0
---
apiVersion: v1alpha1
kind: ExtensionServiceConfig
name: tailscale
environment:
  - TS_AUTHKEY=tskey-auth-123
configFiles:
  - content: secret-file-body
    mountPath: /etc/x
`

	out, err := scrubMachineConfig(in)
	if err != nil {
		t.Fatal(err)
	}

	for _, leaked := range []string{"hunter2", "sk-live-123", "aGVsbG8=", "correct horse", "abc123", "secret-file-body", "S0VZ", "bob", "tskey-auth-123"} {
		if strings.Contains(out, leaked) {
			t.Errorf("%q leaked:\n%s", leaked, out)
		}
	}

	for _, kept := range []string{
		"type: controlplane", "crt: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t", "path: /var/etc/app.conf", "provider: luks2",
		"publicKey: cHVibGlj", "ghcr.io/siderolabs/kubelet:v1.34.0", "kind: ExtensionServiceConfig", "name: tailscale", "slot: 0",
		"mountPath: /etc/x", "---",
	} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q lost:\n%s", kept, out)
		}
	}

	if _, err := scrubMachineConfig("a: [unclosed"); err == nil {
		t.Error("an unparsable config must not be bundled")
	}

	if _, err := scrubMachineConfig(""); err == nil {
		t.Error("empty config accepted")
	}
}
