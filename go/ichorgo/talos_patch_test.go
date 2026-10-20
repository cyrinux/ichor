package ichorgo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
)

func TestTalosPatchDetect(t *testing.T) {
	cases := []struct {
		name, content, kind, note string
		talos, tryable, secrets   bool
		hostname                  string
	}{
		{name: "kubernetes", content: "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"},
		{name: "crd v1alpha1 group", content: "apiVersion: example.test/v1alpha1\nkind: Thing\n"},
		{name: "talconfig", content: "clusterName: lab\nnodes:\n  - hostname: cp-1\n"},
		{name: "mixed", content: "machine:\n  type: worker\n---\napiVersion: v1\nkind: Secret\n"},
		{name: "not yaml", content: "machine: [unclosed\n"},
		{name: "patch", content: "machine:\n  kubelet:\n    extraArgs:\n      max-pods: \"250\"\n", talos: true, tryable: true, kind: talosPatchKindPatch},
		{name: "own hostname", content: "machine:\n  network:\n    hostname: cp-1\n", talos: true, tryable: true, kind: talosPatchKindPatch, hostname: "cp-1"},
		{
			name: "documents", content: "apiVersion: v1alpha1\nkind: HostnameConfig\nhostname: cp-1\nauto: off\n",
			talos: true, tryable: true, kind: talosPatchKindDocuments, hostname: "cp-1",
		},
		{name: "json patch", content: "- op: add\n  path: /machine/network/hostname\n  value: cp-1\n", talos: true, tryable: true, kind: talosPatchKindJSON},
		{
			name: "whole config", content: "version: v1alpha1\nmachine:\n  type: worker\n  token: abc.def\n  ca:\n    crt: x\n",
			talos: true, kind: talosPatchKindConfig, secrets: true, note: "whole machine config",
		},
		{name: "secret", content: "cluster:\n  secretboxEncryptionSecret: abc\n", talos: true, kind: talosPatchKindPatch, secrets: true, note: "cluster.secretboxEncryptionSecret"},
		{name: "talenv", content: "machine:\n  install:\n    disk: ${DISK}\n", talos: true, kind: talosPatchKindPatch, note: "${...}"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got talosPatchInfo
			out, err := TalosPatchDetect(c.content)
			if err == nil {
				err = json.Unmarshal([]byte(out), &got)
			}

			if err != nil {
				t.Fatal(err)
			}

			if got.Talos != c.talos || got.Tryable != c.tryable || got.TouchesSecrets != c.secrets || (c.talos && got.Kind != c.kind) {
				t.Fatalf("got %+v", got)
			}

			if !strings.Contains(got.Note, c.note) {
				t.Errorf("note %q, want %q", got.Note, c.note)
			}

			if c.hostname != "" && (len(got.Documents) != 1 || got.Documents[0].Hostname != c.hostname) {
				t.Errorf("documents %+v, want hostname %s", got.Documents, c.hostname)
			}
		})
	}
}

func TestTalosPatchDraft(t *testing.T) {
	demo := demoConfigForTest(t)
	node := demoNodes()[0]

	out, err := TalosPatchDraft(demo, "", node.Node, "machine:\n  nodeLabels:\n    ichor.test/pool: general\n")
	if err != nil {
		t.Fatal(err)
	}

	var res talosPatchDraft
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	// Redaction is compareTalosRepo's (TestTalosRepoCompare): the demo config has no secrets.
	if res.Same || !strings.Contains(res.Draft, "ichor.test/pool: general") {
		t.Errorf("draft %+v", res)
	}

	for content, want := range map[string]string{
		"apiVersion: v1\nkind: ConfigMap\n": "not a Talos patch",
		"machine:\n  token: abc.def\n":      "cannot be tried: sets machine.token",
	} {
		if _, err := TalosPatchDraft(demo, "", node.Node, content); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", content, err, want)
		}
	}
}

// The edits replayed through MachineConfigEdit give the node what configpatcher gives it.
func TestTalosPatchEditsMatchConfigpatcher(t *testing.T) {
	snap, err := newFakeConfigNode(t).current(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	patch := "machine:\n  kubelet:\n    extraArgs:\n      max-pods: \"250\"\n  sysctls:\n    net.core.somaxconn: \"65535\"\n" +
		"  install:\n    wipe: true\ncluster:\n  allowSchedulingOnControlPlanes: true\n"

	res, err := talosPatchEdits(snap.redacted, []byte(patch))
	if err != nil || !res.Fixable || len(res.Edits) != 4 {
		t.Fatalf("edits %+v, %v", res, err)
	}

	edited := snap.redacted
	for _, e := range res.Edits {
		b, _ := json.Marshal(e)
		if edited, err = MachineConfigEdit(edited, string(b)); err != nil {
			t.Fatal(err)
		}
	}

	loaded, err := configpatcher.LoadPatch([]byte(patch))
	if err != nil {
		t.Fatal(err)
	}

	patched, err := configpatcher.Apply(configpatcher.WithBytes([]byte(snap.plain)), []configpatcher.Patch{loaded})
	if err != nil {
		t.Fatal(err)
	}

	want, err := patched.Bytes()
	if err != nil {
		t.Fatal(err)
	}

	gotDocs, _ := yamlDocuments([]byte(edited))
	wantDocs, _ := yamlDocuments(want)

	for _, e := range res.Edits {
		got := yamlPath(gotDocs[e.Doc].(map[string]any), e.Path...)
		exp := yamlPath(wantDocs[0].(map[string]any), e.Path...)

		if got != exp {
			t.Errorf("%s: edits give %v, configpatcher %v", strings.Join(e.Path, "."), got, exp)
		}
	}
}

func TestTalosPatchEditsRefusals(t *testing.T) {
	draft := "version: v1alpha1\nmachine:\n  type: worker\n---\napiVersion: v1alpha1\nkind: HostnameConfig\nauto: stable\n"

	for patch, want := range map[string]string{
		"machine:\n  certSANs:\n    - a.example.test\n":                               "the list machine.certSANs",
		"machine:\n  network:\n    $patch: delete\n":                                  "$patch directive",
		"- op: remove\n  path: /machine/network\n":                                    "JSON patch",
		"apiVersion: v1alpha1\nkind: LinkConfig\nname: eth0\nmtu: 9000\n":             "adds a LinkConfig eth0 document",
		"apiVersion: v1\nkind: ConfigMap\n":                                           "not a Talos patch",
		"machine:\n  network: {}\n":                                                   "sets nothing",
		"apiVersion: v1alpha1\nkind: HostnameConfig\nhostname: cp-1\nauto: \"off\"\n": "",
	} {
		res, err := talosPatchEdits(draft, []byte(patch))
		if err != nil {
			t.Fatal(err)
		}

		if want == "" {
			if !res.Fixable || len(res.Edits) != 2 || res.Edits[0].Doc != 1 {
				t.Errorf("%q: %+v", patch, res)
			}

			continue
		}

		if res.Fixable || !strings.Contains(res.Reason, want) {
			t.Errorf("%q: %+v, want %q", patch, res, want)
		}
	}
}
