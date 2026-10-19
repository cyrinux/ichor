package ichorgo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const writeBackBase = `version: v1alpha1
machine:
  type: controlplane
  token: '******'
  network:
    hostname: cp-1
    nameservers:
      - 192.0.2.1
  time:
    servers:
      - time.example.org
  kubelet:
    extraArgs:
      max-pods: "110"
cluster:
  clusterName: lab
---
apiVersion: v1alpha1
kind: HostnameConfig
hostname: cp-1
auto: "off"
`

func changePaths(p talosConfigPatch) []string {
	var out []string
	for _, c := range p.Changes {
		out = append(out, c.Kind+":"+c.Path)
	}

	return out
}

func TestTalosConfigPatchFrom(t *testing.T) {
	draft := strings.NewReplacer(
		"max-pods: \"110\"", "max-pods: \"150\"", // a scalar
		"      - time.example.org", "      - ntp-1.example.org\n      - ntp-2.example.org", // a list
		"  clusterName: lab", "  clusterName: lab\n  allowSchedulingOnControlPlanes: true", // a new key
		"auto: \"off\"", "auto: stable", // the other document
	).Replace(writeBackBase)
	draft = strings.Replace(draft, "    nameservers:\n      - 192.0.2.1\n", "", 1) // a removed key

	p, err := talosConfigPatchFrom(writeBackBase, draft)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"v1alpha1:cluster.allowSchedulingOnControlPlanes", "v1alpha1:machine.kubelet.extraArgs.max-pods",
		"v1alpha1:machine.network.nameservers", "v1alpha1:machine.time.servers", "HostnameConfig:auto",
	}
	if got := changePaths(p); !slices.Equal(got, want) {
		t.Fatalf("changes %v, want %v", got, want)
	}

	if p.TouchesSecrets || len(p.Notes) != 1 || !strings.Contains(p.Notes[0], "machine.network.nameservers") {
		t.Errorf("secrets %v notes %v", p.TouchesSecrets, p.Notes)
	}

	// Only the changed values, the list whole; the other document named by its identity.
	for _, s := range []string{"max-pods: \"150\"", "- ntp-1.example.org", "- ntp-2.example.org", "allowSchedulingOnControlPlanes: true", "kind: HostnameConfig", "auto: stable"} {
		if !strings.Contains(p.Patch, s) {
			t.Errorf("patch lacks %q:\n%s", s, p.Patch)
		}
	}

	for _, s := range []string{"clusterName", "hostname: cp-1\n", "token", "nameservers", "type: controlplane"} {
		if strings.Contains(p.Patch, s) {
			t.Errorf("patch holds %q:\n%s", s, p.Patch)
		}
	}

	if _, err := talosConfigPatchFrom(writeBackBase, writeBackBase); err != errTalosPatchSame {
		t.Errorf("same: %v", err)
	}

	secret, err := talosConfigPatchFrom(writeBackBase, strings.Replace(writeBackBase, "  clusterName: lab", "  clusterName: lab\n  secret: '******'", 1))
	if err != nil || !secret.TouchesSecrets {
		t.Errorf("secret %+v %v", secret, err)
	}
}

const writeBackNodeFile = `# cp-1: the first control plane node.
machine:
  network:
    hostname: cp-1 # short name
    interfaces:
      - interface: eth0
        dhcp: true
  kubelet:
    extraArgs:
      max-pods: "110" # raised for the CI runners
  install:
    disk: /dev/sda
`

func writeBack(t *testing.T, content, patch string, opts talosWriteBackOptions) talosWriteBack {
	t.Helper()

	res, err := talosRepoWriteBack(content, patch, opts)
	if err != nil {
		t.Fatal(err)
	}

	return res
}

func TestTalosRepoWriteBackInPlace(t *testing.T) {
	res := writeBack(t, writeBackNodeFile, "machine:\n  kubelet:\n    extraArgs:\n      max-pods: \"150\"\n", talosWriteBackOptions{})

	want := strings.Replace(writeBackNodeFile, `max-pods: "110" # raised`, `max-pods: "150" # raised`, 1)
	if res.Content != want || !res.Changed || res.Line != 10 || len(res.Skipped) != 0 {
		t.Fatalf("in place %+v\n%s", res, res.Content)
	}
}

func TestTalosRepoWriteBackAddsAndReplaces(t *testing.T) {
	patch := `machine:
  time:
    servers:
      - ntp-1.example.org
  network:
    interfaces:
      - interface: eth0
        dhcp: false
        addresses: [192.0.2.11/24]
  install:
    wipe: true
`
	res := writeBack(t, writeBackNodeFile, patch, talosWriteBackOptions{})

	for _, s := range []string{
		"# cp-1: the first control plane node.", "hostname: cp-1 # short name", `max-pods: "110" # raised for the CI runners`,
		"  time:\n    servers:\n      - ntp-1.example.org\n",
		"    disk: /dev/sda\n    wipe: true\n",
		"        dhcp: false\n",
	} {
		if !strings.Contains(res.Content, s) {
			t.Errorf("lacks %q:\n%s", s, res.Content)
		}
	}

	if strings.Contains(res.Content, "dhcp: true") {
		t.Errorf("the list was not replaced:\n%s", res.Content)
	}
}

func TestTalosRepoWriteBackSkipsSharedValues(t *testing.T) {
	shared := map[string]string{"talos/patches/time.yaml": "machine:\n  time:\n    servers:\n      - time.example.org\n"}
	patch := "machine:\n  time:\n    servers:\n      - ntp-1.example.org\n  install:\n    disk: /dev/nvme0n1\n"

	res := writeBack(t, writeBackNodeFile, patch, talosWriteBackOptions{SharedFiles: shared})

	if len(res.Skipped) != 1 || res.Skipped[0].Path != "machine.time.servers" || res.Skipped[0].Why != "set in talos/patches/time.yaml" {
		t.Errorf("skipped %+v", res.Skipped)
	}

	if !strings.Contains(res.Content, "disk: /dev/nvme0n1") || strings.Contains(res.Content, "ntp-1") {
		t.Errorf("content:\n%s", res.Content)
	}
}

func TestTalosRepoWriteBackMultiDocAndNewFile(t *testing.T) {
	patch := "apiVersion: v1alpha1\nkind: HostnameConfig\nauto: stable\n"

	res := writeBack(t, writeBackNodeFile, patch, talosWriteBackOptions{})
	if !strings.HasSuffix(res.Content, "---\napiVersion: v1alpha1\nkind: HostnameConfig\nauto: stable\n") {
		t.Errorf("appended document:\n%s", res.Content)
	}

	created := writeBack(t, "", "machine:\n  install:\n    disk: /dev/sdb\n", talosWriteBackOptions{Create: true, Hostname: "worker-a"})
	docs, err := talosDocsByKey(created.Content)
	if err != nil || !created.Created || yamlPath(docs[talosV1Alpha1], "machine", "network", "hostname") != "worker-a" ||
		yamlPath(docs[talosV1Alpha1], "machine", "install", "disk") != "/dev/sdb" {
		t.Errorf("created %+v %v", created, err)
	}

	if _, err := talosRepoWriteBack("", "machine:\n  install:\n    disk: /dev/sdb\n", talosWriteBackOptions{}); err != errWriteBackNoFile {
		t.Errorf("no file: %v", err)
	}
}

func TestTalosRepoWriteBackRefuses(t *testing.T) {
	if _, err := talosRepoWriteBack(writeBackNodeFile, "machine:\n  token: abc.def\n", talosWriteBackOptions{}); err == nil || !strings.Contains(err.Error(), "machine.token") {
		t.Errorf("secret: %v", err)
	}

	if _, err := talosRepoWriteBack("machine:\n  install:\n    disk: ${DISK}\n", "machine:\n  install:\n    disk: /dev/sda\n", talosWriteBackOptions{}); err != errWriteBackTemplated {
		t.Errorf("templated: %v", err)
	}

	// A value under a flow mapping cannot be added in place: skipped with why.
	res := writeBack(t, "machine:\n  network: {}\n", "machine:\n  network:\n    hostname: cp-1\n", talosWriteBackOptions{})
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Why, "by hand") || res.Changed {
		t.Errorf("flow %+v", res)
	}
}

func TestTalosWriteBackExported(t *testing.T) {
	base := "machine:\n  install:\n    disk: /dev/sda\n"
	out, err := TalosConfigPatchFrom(base, "machine:\n  install:\n    disk: /dev/sdb\n")
	if err != nil {
		t.Fatal(err)
	}

	var p talosConfigPatch
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}

	out, err = TalosRepoWriteBack(writeBackNodeFile, p.Patch, `{"hostname":"cp-1"}`)
	if err != nil {
		t.Fatal(err)
	}

	var res talosWriteBack
	if err := json.Unmarshal([]byte(out), &res); err != nil || !res.Changed || !strings.Contains(res.Content, "disk: /dev/sdb") {
		t.Errorf("exported %s %v", out, err)
	}
}
