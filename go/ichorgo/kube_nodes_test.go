package ichorgo

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

// One Node as the API server serves it, read through every accessor the consumers share.
func TestKubeNodeObjectAccessors(t *testing.T) {
	var obj kubeNodeObject
	if err := json.Unmarshal([]byte(`{
	  "metadata":{"name":"cp-1","labels":{"node-role.kubernetes.io/master":"","kubernetes.io/os":"linux"}},
	  "spec":{"unschedulable":true,"taints":[
	    {"key":"node-role.kubernetes.io/master","effect":"NoSchedule"},
	    {"key":"node.kubernetes.io/unschedulable","effect":"NoSchedule","timeAdded":"2026-10-08T10:00:00Z"}]},
	  "status":{"addresses":[{"type":"Hostname","address":"cp-1"},{"type":"InternalIP","address":"192.0.2.10"},{"type":"ExternalIP","address":"203.0.113.9"}],
	    "conditions":[{"type":"DiskPressure","status":"True","reason":"KubeletHasDiskPressure"},
	      {"type":"Ready","status":"True","lastHeartbeatTime":"2026-10-08T10:05:00Z"}]}}`), &obj); err != nil {
		t.Fatal(err)
	}

	if got := obj.roles(); !slices.Equal(got, []string{"control-plane"}) {
		t.Errorf("roles = %q", got)
	}

	if got := obj.rawRoles(); !slices.Equal(got, []string{"master"}) {
		t.Errorf("rawRoles = %q", got)
	}

	if !obj.controlPlane() || !obj.ready() || obj.internalIP() != "192.0.2.10" || obj.address("ExternalIP") != "203.0.113.9" || obj.address("Nope") != "" {
		t.Errorf("identity: %+v", obj)
	}

	if got := obj.pressure(); !slices.Equal(got, []string{"DiskPressure"}) {
		t.Errorf("pressure = %q", got)
	}

	if got := obj.taintStrings(); !slices.Equal(got, []string{"node-role.kubernetes.io/master:NoSchedule", "node.kubernetes.io/unschedulable:NoSchedule"}) {
		t.Errorf("taints = %q", got)
	}

	if since := obj.cordonedSince(); since == nil || !since.Equal(time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("cordonedSince = %v", since)
	}

	heartbeat := time.Date(2026, 10, 8, 10, 5, 0, 0, time.UTC)
	if !obj.readySince(heartbeat.Add(-time.Minute)) || obj.readySince(heartbeat.Add(time.Minute)) {
		t.Error("readySince ignores the heartbeat")
	}

	if got := obj.conditions().get("DiskPressure").Reason; got != "KubeletHasDiskPressure" {
		t.Errorf("conditions = %q", got)
	}

	var bare kubeNodeObject
	if bare.ready() || bare.cordonedSince() != nil || bare.pressure() == nil || bare.taintStrings() == nil || len(bare.roles()) != 0 {
		t.Errorf("empty node: %+v", bare)
	}
}
