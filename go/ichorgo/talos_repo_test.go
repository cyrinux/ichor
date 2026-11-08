package ichorgo

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func repoFiles(files map[string]string) map[string][]byte {
	out := map[string][]byte{}
	for p, c := range files {
		out[p] = []byte(c)
	}

	return out
}

// kinds is each found file as "kind path".
func kinds(found talosRepoFiles) []string {
	var out []string
	for _, f := range found.Files {
		out = append(out, f.Kind+" "+f.Path)
	}

	return out
}

func TestTalosRepoFilesPlainPatches(t *testing.T) {
	files := repoFiles(map[string]string{
		"talos/patches/dns.yaml":      "machine:\n  network:\n    nameservers: [192.0.2.1]\n",
		"talos/nodes/cp-1.yaml":       "machine:\n  network:\n    hostname: cp-1\n",
		"talos/nodes/worker-a.yaml":   "machine:\n  network:\n    hostname: worker-a\n",
		"talos/nodes/cp-2.yaml":       "machine:\n  install:\n    disk: /dev/sda\n",
		"talos/kustomization.yaml":    "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n",
		"talos/secrets.sops.yaml":     "cluster:\n  secret: ENC[AES256_GCM,data:eA==]\n",
		"talos/keys.yaml":             "machine:\n  token: abc.def\n",
		"talos/hostname.yaml":         "apiVersion: v1alpha1\nkind: HostnameConfig\nhostname: worker-b\n",
		"talos/json.yaml":             "- op: add\n  path: /machine/kubelet/extraArgs\n  value: {max-pods: \"150\"}\n",
		"talos/notes.txt":             "machine: not yaml here",
		"kubernetes/apps/deploy.yaml": "machine:\n  network:\n    hostname: cp-1\n",
	})

	found := findTalosRepoFiles(files, "talos/", "cp-1.lab.example", "controlplane")

	want := []string{
		"shared talos/json.yaml", "shared talos/patches/dns.yaml",
		"node talos/nodes/cp-1.yaml",
		"secrets talos/keys.yaml", "secrets talos/secrets.sops.yaml",
	}
	if got := kinds(found); !slices.Equal(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}

	if found.Generator != talosGenTalosctl {
		t.Errorf("generator %q", found.Generator)
	}

	if own := found.Files[2]; !strings.Contains(own.Why, "short name") || !own.Patch {
		t.Errorf("node file %+v", own)
	}

	if secret := found.Files[3]; secret.Patch || secret.Why != "sets machine.token" {
		t.Errorf("secrets file %+v", secret)
	}

	// A node named after its file, its hostname set nowhere.
	byName := findTalosRepoFiles(files, "talos", "cp-2", "controlplane")
	if got := kinds(byName); !slices.Contains(got, "node talos/nodes/cp-2.yaml") || slices.Contains(got, "node talos/nodes/cp-1.yaml") {
		t.Errorf("by name %v", got)
	}

	// A HostnameConfig document names its node too.
	if got := kinds(findTalosRepoFiles(files, "talos", "worker-b", "worker")); !slices.Contains(got, "node talos/hostname.yaml") {
		t.Errorf("HostnameConfig %v", got)
	}

	if none := findTalosRepoFiles(files, "nowhere", "cp-1", "controlplane"); len(none.Files) != 0 || none.Generator != talosGenUnknown || none.Note == "" {
		t.Errorf("empty directory %+v", none)
	}
}

func TestTalosRepoFilesTalhelper(t *testing.T) {
	files := repoFiles(map[string]string{
		"talos/talconfig.yaml": `clusterName: lab
patches:
  - "@./patches/network.yaml"
controlPlane:
  patches:
    - |-
      machine:
        kubelet:
          extraArgs:
            rotate-server-certificates: "true"
worker:
  patches:
    - "@./patches/worker.yaml"
nodes:
  - hostname: cp-1
    controlPlane: true
    patches:
      - "@./nodes/cp-1.yaml"
      - "@./nodes/missing.yaml"
  - hostname: worker-a
    patches:
      - "@./nodes/worker-a.yaml"
`,
		"talos/patches/network.yaml": "machine:\n  network:\n    nameservers: [192.0.2.1]\n",
		"talos/patches/worker.yaml":  "machine:\n  nodeLabels:\n    pool: general\n",
		"talos/nodes/cp-1.yaml":      "machine:\n  install:\n    disk: /dev/sda\n",
		"talos/nodes/worker-a.yaml":  "machine:\n  install:\n    disk: /dev/nvme0n1\n",
		"talos/talsecret.sops.yaml":  "cluster:\n  secret: ENC[x]\nsops:\n  mac: ENC[y]\n",
		"talos/unlisted.yaml":        "machine:\n  sysctls:\n    a: b\n",
	})

	found := findTalosRepoFiles(files, "talos", "cp-1", "")

	want := []string{
		"talconfig talos/talconfig.yaml",
		"shared talos/patches/network.yaml",
		"talconfig talos/talconfig.yaml#L6",
		"node talos/nodes/cp-1.yaml",
		"node talos/nodes/missing.yaml",
		"secrets talos/talsecret.sops.yaml",
	}
	if got := kinds(found); !slices.Equal(got, want) {
		t.Fatalf("files %v, want %v", got, want)
	}

	if found.Generator != talosGenTalhelper || found.Note == "" {
		t.Errorf("generator %q note %q", found.Generator, found.Note)
	}

	if f := found.Files[0]; f.Patch || f.Line != 15 {
		t.Errorf("talconfig entry %+v", f)
	}

	if f := found.Files[4]; f.Patch || !strings.Contains(f.Why, "not in the repository") {
		t.Errorf("missing file %+v", f)
	}

	inline, err := talosPatchContent(files, found.Files[2].Path)
	if err != nil || !strings.Contains(string(inline), "rotate-server-certificates") {
		t.Fatalf("inline patch %q %v", inline, err)
	}

	// A worker gets the worker patches, not the control plane's.
	worker := kinds(findTalosRepoFiles(files, "talos", "worker-a", "worker"))
	if !slices.Contains(worker, "shared talos/patches/worker.yaml") || slices.Contains(worker, "talconfig talos/talconfig.yaml#L6") {
		t.Errorf("worker %v", worker)
	}
}

func TestTalosRepoCompare(t *testing.T) {
	files := repoFiles(map[string]string{
		"labels.yaml": "machine:\n  nodeLabels:\n    ichor.test/pool: general\n",
		"same.yaml":   "machine:\n  type: controlplane\n",
		"broken.yaml": "machine: [unclosed\n",
		"env.yaml":    "machine:\n  install:\n    disk: ${DISK}\n",
		"token.yaml":  "machine:\n  token: abc.def\n",
	})

	patches, used, skipped := loadTalosPatches(files, []string{"broken.yaml", "env.yaml", "token.yaml", "gone.yaml", "same.yaml"})
	if len(skipped) != 4 || !slices.Equal(used, []string{"same.yaml"}) {
		t.Fatalf("used %v skipped %+v", used, skipped)
	}

	for i, why := range []string{"YAML", "${...}", "machine.token", "not in the repository"} {
		if !strings.Contains(skipped[i].Why, why) {
			t.Errorf("skip %d: %q, want %q", i, skipped[i].Why, why)
		}
	}

	node := newFakeConfigNode(t)

	same, err := compareTalosRepo(context.Background(), node, patches)
	if err != nil {
		t.Fatal(err)
	}

	if !same.Same || same.Diff.Resources[0].Change != diffChangeUnchanged || len(node.calls) != 0 {
		t.Errorf("same %+v, calls %d", same, len(node.calls))
	}

	patches, _, _ = loadTalosPatches(files, []string{"labels.yaml"})
	node.reboot = true

	drift, err := compareTalosRepo(context.Background(), node, patches)
	if err != nil {
		t.Fatal(err)
	}

	if drift.Same || !drift.NeedsReboot || !strings.Contains(drift.Diff.Resources[0].Diff, "+        ichor.test/pool: general") {
		t.Errorf("drift %+v", drift)
	}

	// Secrets never leave Go: the diff and the draft are redacted, the dry run had them.
	if strings.Contains(drift.Draft, "token: "+tokenOf(t, node)) || !strings.Contains(drift.Draft, redacted) {
		t.Error("the draft is not redacted")
	}

	if len(node.calls) != 1 || !node.calls[0].dryRun || !strings.Contains(node.calls[0].data, tokenOf(t, node)) {
		t.Errorf("dry run %+v", node.calls)
	}
}

// tokenOf is the fake node's machine token.
func tokenOf(t *testing.T, n *fakeConfigNode) string {
	t.Helper()

	snap, err := n.current(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	docs, err := yamlDocuments([]byte(snap.plain))
	if err != nil {
		t.Fatal(err)
	}

	token, _ := yamlPath(docs[0].(map[string]any), "machine", "token").(string)
	if token == "" {
		t.Fatal("no machine token")
	}

	return token
}

func TestTalosRepoDemo(t *testing.T) {
	demo := demoConfigForTest(t)
	node := demoNodes()[0]

	out, err := TalosRepoFiles(`{"kind":"demo"}`, demoClusterRepoURL, demoClusterRepoHead, "talos", node.Hostname, node.Role)
	if err != nil {
		t.Fatal(err)
	}

	var found talosRepoFiles
	if err := json.Unmarshal([]byte(out), &found); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"talconfig talos/talconfig.yaml", "shared talos/patches/network.yaml",
		"node talos/nodes/demo-cp-1.yaml", "secrets talos/talsecret.sops.yaml",
	}
	if got := kinds(found); !slices.Equal(got, want) {
		t.Fatalf("demo files %v, want %v", got, want)
	}

	out, err = TalosRepoCompare(demo, "", `{"kind":"demo"}`, demoClusterRepoURL, demoClusterRepoHead, node.Node, node.Hostname,
		`["talos/patches/network.yaml","talos/nodes/demo-cp-1.yaml"]`)
	if err != nil {
		t.Fatal(err)
	}

	var cmp talosRepoCompare
	if err := json.Unmarshal([]byte(out), &cmp); err != nil {
		t.Fatal(err)
	}

	// One line of drift: the install disk.
	d := cmp.Diff.Resources[0].Diff
	if cmp.Same || strings.Count(d, "\n-  ") != 1 || !strings.Contains(d, "-        disk: /dev/sda") || !strings.Contains(d, "+        disk: /dev/nvme0n1") {
		t.Errorf("demo compare %+v", cmp)
	}

	if _, err := TalosRepoCompare(demo, "", `{"kind":"demo"}`, demoClusterRepoURL, demoClusterRepoHead, node.Node, node.Hostname, `[]`); err != errTalosRepoNoFiles {
		t.Errorf("no files: %v", err)
	}
}
