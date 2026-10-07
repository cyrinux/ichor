package ichorgo

import (
	"encoding/json"
	"testing"
)

func TestNodePoolAndCapacity(t *testing.T) {
	cases := []struct {
		name                 string
		labels               map[string]string
		pool, kind, capacity string
	}{
		{"karpenter spot", map[string]string{karpenterPoolLabel: "general", karpenterCapacityLabel: "spot"}, "general", "karpenter", "spot"},
		{"karpenter reserved", map[string]string{karpenterPoolLabel: "gpu", karpenterCapacityLabel: "reserved"}, "gpu", "karpenter", "reserved"},
		{"karpenter v1alpha5", map[string]string{karpenterProvisionerLabel: "default", karpenterCapacityLabel: "on-demand"}, "default", "karpenter", "on-demand"},
		// EKS Auto Mode: Karpenter's labels next to EKS' own; the NodePool is the answer.
		{"eks auto mode", map[string]string{karpenterPoolLabel: "system", karpenterCapacityLabel: "on-demand", "eks.amazonaws.com/compute-type": "auto"}, "system", "karpenter", "on-demand"},
		{"eks managed spot", map[string]string{eksNodeGroupLabel: "workers", eksCapacityLabel: "SPOT"}, "workers", "eks", "spot"},
		{"eks managed on-demand", map[string]string{eksNodeGroupLabel: "workers", eksCapacityLabel: "ON_DEMAND"}, "workers", "eks", "on-demand"},
		{"eks capacity block", map[string]string{eksNodeGroupLabel: "gpu", eksCapacityLabel: "CAPACITY_BLOCK"}, "gpu", "eks", "reserved"},
		{"eks self-managed", map[string]string{eksNodeGroupLabel: "", "alpha.eksctl.io/nodegroup-name": "ng"}, "", "", ""},
		{"gke spot", map[string]string{gkePoolLabel: "pool-1", gkeSpotLabel: "true"}, "pool-1", "gke", "spot"},
		{"gke preemptible", map[string]string{gkePoolLabel: "pool-1", gkePreemptibleLabel: "true"}, "pool-1", "gke", "spot"},
		{"gke on-demand", map[string]string{gkePoolLabel: "default-pool"}, "default-pool", "gke", "on-demand"},
		// A compute class names the intent; its auto-provisioned pool has a generated name.
		{"gke compute class spot", map[string]string{gkeComputeClassLabel: "batch", gkePoolLabel: "nap-batch-1a2b3c", gkeSpotLabel: "true"}, "batch", "gke-class", "spot"},
		{"gke compute class on-demand", map[string]string{gkeComputeClassLabel: "web", gkePoolLabel: "nap-web-4d5e6f"}, "web", "gke-class", "on-demand"},
		{"aks spot", map[string]string{aksPoolLabel: "spotpool", aksPriorityLabel: "spot"}, "spotpool", "aks", "spot"},
		{"aks regular", map[string]string{aksPoolLabel: "nodepool1"}, "nodepool1", "aks", "on-demand"},
		{"bare metal", map[string]string{"kubernetes.io/hostname": "n1"}, "", "", ""},
		{"no labels", nil, "", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool, kind := nodePool(c.labels)
			if pool != c.pool || kind != c.kind {
				t.Fatalf("pool %q kind %q, want %q %q", pool, kind, c.pool, c.kind)
			}

			if got := nodeCapacity(c.labels, kind); got != c.capacity {
				t.Fatalf("capacity %q, want %q", got, c.capacity)
			}
		})
	}
}

func TestMapKubeNodeReadsPoolLabels(t *testing.T) {
	var obj kubeNodeObject
	if err := json.Unmarshal([]byte(`{"metadata":{"name":"ip-10-0-1-5","labels":{
		"node-role.kubernetes.io/worker":"","karpenter.sh/nodepool":"general","karpenter.sh/capacity-type":"spot",
		"node.kubernetes.io/instance-type":"m6i.large"}},"status":{"conditions":[{"type":"Ready","status":"True"}]}}`), &obj); err != nil {
		t.Fatal(err)
	}

	n := mapKubeNode(obj)
	if n.Pool != "general" || n.PoolKind != "karpenter" || n.Capacity != "spot" || n.InstanceType != "m6i.large" {
		t.Fatalf("node %+v", n)
	}

	// The legacy instance-type label still counts; nothing else is invented.
	obj.Metadata.Labels = map[string]string{legacyInstanceTypeLabel: "n2-standard-4"}

	n = mapKubeNode(obj)
	if n.InstanceType != "n2-standard-4" || n.Pool != "" || n.PoolKind != "" || n.Capacity != "" {
		t.Fatalf("node %+v", n)
	}

	out, _ := json.Marshal(n)
	for _, absent := range []string{`"pool"`, `"poolKind"`, `"capacity"`} {
		if json.Valid(out) && containsJSONKey(out, absent) {
			t.Fatalf("%s should be omitted: %s", absent, out)
		}
	}
}

func containsJSONKey(doc []byte, quotedKey string) bool {
	return len(doc) > 0 && string(doc) != "" && indexOf(string(doc), quotedKey+":") >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}

	return -1
}
