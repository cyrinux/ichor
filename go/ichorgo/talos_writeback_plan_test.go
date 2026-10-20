package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTalosWriteBackPlan(t *testing.T) {
	files := repoFiles(map[string]string{
		"talos/patches/dns.yaml": "machine:\n  network:\n    nameservers: [192.0.2.1]\n",
		"talos/nodes/cp-1.yaml":  "machine:\n  network:\n    hostname: cp-1\n  install:\n    disk: /dev/sda\n",
	})
	patch := "machine:\n  install:\n    disk: /dev/nvme0n1\n  network:\n    nameservers: [192.0.2.53]\n  time:\n    servers: [ntp.example.org]\n"

	plan, err := planTalosWriteBack(files, "talos", "cp-1", "controlplane", patch)
	if err != nil {
		t.Fatal(err)
	}

	// The nameservers are the shared patch's: left out, with the file named.
	if plan.File != "talos/nodes/cp-1.yaml" || plan.Create || len(plan.Skipped) != 1 || plan.Skipped[0].Why != "set in talos/patches/dns.yaml" {
		t.Fatalf("plan %+v", plan)
	}

	if strings.Contains(plan.Patch, "nameservers") || !strings.Contains(plan.Patch, "nvme0n1") || !strings.Contains(plan.Patch, "ntp.example.org") {
		t.Errorf("patch:\n%s", plan.Patch)
	}

	// A node without a file of its own gets a new one under the Talos directory.
	created, err := planTalosWriteBack(files, "talos", "worker-a.lab.example", "worker", "machine:\n  install:\n    disk: /dev/sdb\n")
	if err != nil || !created.Create || created.File != "talos/nodes/worker-a.yaml" {
		t.Errorf("created %+v %v", created, err)
	}

	// talhelper: a node listed without a patch file of its own is edited by hand.
	talhelper := repoFiles(map[string]string{"talos/talconfig.yaml": "nodes:\n  - hostname: cp-1\n"})
	if _, err := planTalosWriteBack(talhelper, "talos", "cp-1", "controlplane", patch); err != errWriteBackTalhelper {
		t.Errorf("talhelper: %v", err)
	}
}

func TestTalosQuickEdit(t *testing.T) {
	edit, err := json.Marshal(quickEdit{Op: "talos", Patch: "machine:\n  install:\n    disk: /dev/nvme0n1\n", Skipped: []string{"machine.time.servers: set in talos/patches/time.yaml"}})
	if err != nil {
		t.Fatal(err)
	}

	out, err := GitOpsQuickEdit(writeBackNodeFile, string(edit))
	if err != nil {
		t.Fatal(err)
	}

	var res quickEditResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	if !res.Changed || !strings.Contains(res.Content, "disk: /dev/nvme0n1") || len(res.Skipped) != 1 || res.Line != 12 {
		t.Errorf("talos quick edit %+v", res)
	}

	// A new node file: the editor opens it empty.
	created, err := gitOpsQuickEdit("", quickEdit{Op: "talos", Patch: "machine:\n  install:\n    disk: /dev/sdb\n", Hostname: "worker-a", Create: true})
	if err != nil || !created.Changed || !strings.Contains(created.Content, "hostname: worker-a") {
		t.Errorf("created %+v %v", created, err)
	}
}

func TestTalosWriteBackPlanDemo(t *testing.T) {
	node := demoNodes()[0]

	out, err := TalosRepoWriteBackPlan(`{"kind":"demo"}`, demoClusterRepoURL, demoClusterRepoHead, "talos", node.Hostname, node.Role,
		"machine:\n  install:\n    disk: /dev/sda\n")
	if err != nil {
		t.Fatal(err)
	}

	var plan talosWriteBackPlan
	if err := json.Unmarshal([]byte(out), &plan); err != nil || plan.File != "talos/nodes/demo-cp-1.yaml" || plan.Create {
		t.Errorf("demo plan %s %v", out, err)
	}
}
