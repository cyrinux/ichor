package ichorgo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const testMachineConfig = `version: v1alpha1
debug: false
machine:
    type: worker
    token: '******'
    certSANs:
        - 192.0.2.1
    sysctls:
        net.core.somaxconn: "65535"
    install:
        disk: /dev/sda
---
apiVersion: v1alpha1
kind: HostnameConfig
auto: stable
`

func child(n *configNode, path ...string) *configNode {
	for _, key := range path {
		i := slices.IndexFunc(n.Children, func(c *configNode) bool { return c.Key == key })
		if i < 0 {
			return nil
		}

		n = n.Children[i]
	}

	return n
}

func TestDescribeWithSchema(t *testing.T) {
	tree := describeConfig(testMachineConfig, mustTestSchema(t))

	if !tree.Schema || tree.Error != nil || len(tree.Documents) != 2 {
		t.Fatalf("%+v", tree)
	}

	main, hostname := tree.Documents[0], tree.Documents[1]
	if main.Title != "v1alpha1" || hostname.Title != "HostnameConfig" || hostname.Index != 1 {
		t.Fatalf("titles: %q %q", main.Title, hostname.Title)
	}

	typ := child(main.Node, "machine", "type")
	if typ.Type != configTypeString || typ.Value != "worker" || !slices.Equal(typ.Enum, []string{"controlplane", "worker"}) ||
		typ.Description != "Role of the node." || !slices.Equal(typ.Path, []string{"machine", "type"}) {
		t.Fatalf("machine.type: %+v", typ)
	}

	if token := child(main.Node, "machine", "token"); !token.Redacted {
		t.Fatalf("machine.token must be redacted: %+v", token)
	}

	if debug := child(main.Node, "debug"); debug.Type != configTypeBoolean || debug.Value != "false" {
		t.Fatalf("debug: %+v", debug)
	}

	sans := child(main.Node, "machine", "certSANs")
	if sans.Type != configTypeArray || sans.ItemType != configTypeString || len(sans.Children) != 1 || !slices.Equal(sans.Children[0].Path, []string{"machine", "certSANs", "0"}) {
		t.Fatalf("certSANs: %+v", sans)
	}

	// A map takes any key; its quoted number stays a string.
	sysctls := child(main.Node, "machine", "sysctls")
	if sysctls.FreeKeyType != configTypeString || sysctls.Children[0].Type != configTypeString {
		t.Fatalf("sysctls: %+v", sysctls)
	}

	install := child(main.Node, "machine", "install")
	if install.Description != "How Talos is installed." {
		t.Fatalf("the field's own documentation, next to its $ref, is shown: %+v", install)
	}

	if install.FreeKeyType != "" || len(install.Addable) != 1 || install.Addable[0].Key != "wipe" || install.Addable[0].Type != configTypeBoolean {
		t.Fatalf("install: %+v", install)
	}

	if a := hostname.Node.Addable; len(a) != 1 || a[0].Key != "hostname" {
		t.Fatalf("HostnameConfig addable: %+v", a)
	}

	if auto := child(hostname.Node, "auto"); !slices.Equal(auto.Enum, []string{"stable", "off"}) {
		t.Fatalf("auto: %+v", auto)
	}
}

func TestDescribeWithoutSchema(t *testing.T) {
	tree := describeConfig(testMachineConfig, nil)

	if tree.Schema || len(tree.Documents) != 2 {
		t.Fatalf("%+v", tree)
	}

	machine := child(tree.Documents[0].Node, "machine")
	if machine.Type != configTypeObject || machine.FreeKeyType != configTypeAny || len(machine.Addable) != 0 {
		t.Fatalf("machine: %+v", machine)
	}

	if sans := child(machine, "certSANs"); sans.ItemType != configTypeAny {
		t.Fatalf("certSANs: %+v", sans)
	}
}

func TestDescribeSyntaxError(t *testing.T) {
	out, err := MachineConfigDescribe("machine:\n  type: worker\n bad: [", "")
	if err != nil {
		t.Fatal(err)
	}

	var tree configTree
	if err := json.Unmarshal([]byte(out), &tree); err != nil {
		t.Fatal(err)
	}

	if tree.Error == nil || tree.Error.Line == 0 || tree.Error.Message == "" || len(tree.Documents) != 0 {
		t.Fatalf("%s", out)
	}

	if tree := describeConfig("- a\n- b\n", nil); tree.Error == nil {
		t.Fatal("a list is not a config document")
	}
}

func edit(t *testing.T, draft, editJSON string) string {
	t.Helper()

	out, err := MachineConfigEdit(draft, editJSON)
	if err != nil {
		t.Fatalf("%s: %v", editJSON, err)
	}

	return out
}

func TestEditSetAddRemove(t *testing.T) {
	out := edit(t, testMachineConfig, `{"doc":0,"path":["machine","type"],"op":"set","type":"string","value":"controlplane"}`)
	out = edit(t, out, `{"doc":0,"path":["debug"],"op":"set","type":"boolean","value":"true"}`)
	out = edit(t, out, `{"doc":0,"path":["machine","install"],"op":"add","key":"wipe","type":"boolean","value":"true"}`)
	out = edit(t, out, `{"doc":0,"path":["machine","certSANs"],"op":"add","type":"string","value":"192.0.2.2"}`)
	out = edit(t, out, `{"doc":0,"path":["machine","sysctls"],"op":"add","key":"vm.max_map_count","type":"string","value":"262144"}`)
	out = edit(t, out, `{"doc":0,"path":["machine"],"op":"add","key":"nodeLabels","type":"object"}`)
	out = edit(t, out, `{"doc":0,"path":["machine","nodeLabels"],"op":"add","key":"zone","type":"any","value":"a"}`)
	out = edit(t, out, `{"doc":0,"path":["machine","certSANs","0"],"op":"remove"}`)
	out = edit(t, out, `{"doc":1,"path":["auto"],"op":"set","type":"string","value":"off"}`)

	tree := describeConfig(out, mustTestSchema(t))
	if tree.Error != nil || len(tree.Documents) != 2 {
		t.Fatalf("%+v\n%s", tree.Error, out)
	}

	main := tree.Documents[0].Node

	for path, want := range map[string]string{
		"machine.type":                     "controlplane",
		"debug":                            "true",
		"machine.install.wipe":             "true",
		"machine.install.disk":             "/dev/sda",
		"machine.certSANs.0":               "192.0.2.2",
		"machine.sysctls.vm.max_map_count": "262144",
		"machine.nodeLabels.zone":          "a",
		"machine.token":                    redacted,
	} {
		keys := strings.Split(path, ".")
		if path == "machine.sysctls.vm.max_map_count" {
			keys = []string{"machine", "sysctls", "vm.max_map_count"}
		}

		if n := child(main, keys...); n == nil || n.Value != want {
			t.Errorf("%s: %+v, want %q", path, n, want)
		}
	}

	if n := child(main, "machine", "sysctls", "vm.max_map_count"); n.Type != configTypeString {
		t.Errorf("a string that looks like a number must stay a string:\n%s", out)
	}

	if len(child(main, "machine", "certSANs").Children) != 1 {
		t.Errorf("one SAN was removed:\n%s", out)
	}

	if child(tree.Documents[1].Node, "auto").Value != "off" {
		t.Errorf("second document:\n%s", out)
	}
}

func TestEditAddToEmptyValue(t *testing.T) {
	out := edit(t, "machine:\n    nodeLabels:\n    certSANs:\n", `{"doc":0,"path":["machine","nodeLabels"],"op":"add","key":"a","type":"string","value":"b"}`)
	out = edit(t, out, `{"doc":0,"path":["machine","certSANs"],"op":"add","type":"string","value":"x"}`)

	root := describeConfig(out, nil).Documents[0].Node
	if child(root, "machine", "nodeLabels", "a").Value != "b" || child(root, "machine", "certSANs", "0").Value != "x" {
		t.Fatal(out)
	}
}

func TestEditRefusals(t *testing.T) {
	for name, e := range map[string]string{
		"secret":            `{"doc":0,"path":["machine","token"],"op":"set","type":"string","value":"new"}`,
		"mask as value":     `{"doc":0,"path":["machine","type"],"op":"set","type":"string","value":"******"}`,
		"object":            `{"doc":0,"path":["machine"],"op":"set","type":"string","value":"x"}`,
		"missing field":     `{"doc":0,"path":["machine","nope"],"op":"set","type":"string","value":"x"}`,
		"missing document":  `{"doc":7,"path":["debug"],"op":"set","type":"boolean","value":"true"}`,
		"bad boolean":       `{"doc":0,"path":["debug"],"op":"set","type":"boolean","value":"yes"}`,
		"bad integer":       `{"doc":0,"path":["debug"],"op":"set","type":"integer","value":"1.5"}`,
		"existing key":      `{"doc":0,"path":["machine"],"op":"add","key":"type","type":"string","value":"x"}`,
		"no key":            `{"doc":0,"path":["machine"],"op":"add","type":"string","value":"x"}`,
		"add to scalar":     `{"doc":0,"path":["machine","type"],"op":"add","key":"a","type":"string","value":"x"}`,
		"remove document":   `{"doc":0,"path":[],"op":"remove"}`,
		"list out of range": `{"doc":0,"path":["machine","certSANs","3"],"op":"remove"}`,
		"unknown op":        `{"doc":0,"path":["debug"],"op":"toggle"}`,
		"any with a list":   `{"doc":0,"path":["debug"],"op":"set","type":"any","value":"[1, 2]"}`,
		"not JSON":          `nope`,
	} {
		if out, err := MachineConfigEdit(testMachineConfig, e); err == nil {
			t.Errorf("%s must be refused, got:\n%s", name, out)
		}
	}

	if _, err := MachineConfigEdit("a: [", `{"doc":0,"path":["a"],"op":"remove"}`); err == nil {
		t.Error("an invalid draft must be refused")
	}
}
