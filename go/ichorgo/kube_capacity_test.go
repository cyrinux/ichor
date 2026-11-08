package ichorgo

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

const (
	capacityNodesBody = `{"items":[
		{"metadata":{"name":"w1"},"status":{"allocatable":{"cpu":"4","memory":"8Gi","pods":"10"},"conditions":[{"type":"Ready","status":"True"}]}},
		{"metadata":{"name":"w2"},"status":{"allocatable":{"cpu":"2","memory":"4Gi","pods":"10"},"conditions":[{"type":"Ready","status":"True"}]}}]}`
	capacityPodsBody = `{"items":[
		{"metadata":{"name":"web-5d8f-a","namespace":"web","labels":{"app":"web"},"ownerReferences":[{"kind":"ReplicaSet","name":"web-5d8f","controller":true}]},
		 "spec":{"nodeName":"w1","priorityClassName":"low","containers":[{"name":"app","resources":{"requests":{"cpu":"1","memory":"2Gi"},"limits":{"memory":"2Gi"}}}]},
		 "status":{"phase":"Running","qosClass":"Burstable"}},
		{"metadata":{"name":"web-5d8f-b","namespace":"web","labels":{"app":"web"},"ownerReferences":[{"kind":"ReplicaSet","name":"web-5d8f","controller":true}]},
		 "spec":{"nodeName":"w2","priorityClassName":"low","containers":[{"name":"app","resources":{"requests":{"cpu":"1","memory":"2Gi"},"limits":{"memory":"2Gi"}}}]},
		 "status":{"phase":"Running","qosClass":"Burstable","containerStatuses":[{"name":"app","ready":true,"restartCount":1,
		   "lastState":{"terminated":{"reason":"OOMKilled","exitCode":137}}}]}},
		{"metadata":{"name":"db-0","namespace":"web","ownerReferences":[{"kind":"StatefulSet","name":"db","controller":true}]},
		 "spec":{"nodeName":"w1","priorityClassName":"high","priority":1000,"containers":[{"name":"db","resources":{"requests":{"cpu":"500m","memory":"1Gi","ephemeral-storage":"1Gi"},"limits":{"cpu":"500m","memory":"1Gi","ephemeral-storage":"2Gi"}}}]},
		 "status":{"phase":"Running","qosClass":"Guaranteed"}},
		{"metadata":{"name":"proxy-a","namespace":"kube-system","ownerReferences":[{"kind":"DaemonSet","name":"proxy","controller":true}]},
		 "spec":{"nodeName":"w1","containers":[{"name":"proxy"}]},"status":{"phase":"Running"}},
		{"metadata":{"name":"proxy-b","namespace":"kube-system","ownerReferences":[{"kind":"DaemonSet","name":"proxy","controller":true}]},
		 "spec":{"nodeName":"w2","containers":[{"name":"proxy"}]},"status":{"phase":"Running"}},
		{"metadata":{"name":"lone","namespace":"web"},"spec":{"containers":[{"name":"x"}]},"status":{"phase":"Pending"}},
		{"metadata":{"name":"done","namespace":"web"},"spec":{"nodeName":"w2","containers":[{"name":"x","resources":{"requests":{"cpu":"1"}}}]},"status":{"phase":"Succeeded"}}]}`
	capacitySetsBody = `{"items":[{"metadata":{"name":"web-5d8f","namespace":"web","ownerReferences":[{"kind":"Deployment","name":"web","controller":true}]}}]}`
	capacityPDBsBody = `{"items":[{"metadata":{"name":"web","namespace":"web"},"spec":{"selector":{"matchLabels":{"app":"web"}}},"status":{"disruptionsAllowed":1}}]}`
)

func capacityFake(t *testing.T) *fakeKubeAPI {
	t.Helper()

	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/nodes":                        capacityNodesBody,
		"GET /api/v1/pods":                         capacityPodsBody,
		"GET /apis/apps/v1/replicasets":            capacitySetsBody,
		"GET /apis/policy/v1/poddisruptionbudgets": capacityPDBsBody,
	})
	// A namespaced ServiceAccount may not list the cluster's PriorityClasses.
	f.answerWith("GET /apis/scheduling.k8s.io/v1/priorityclasses", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden"}`)

	return f
}

func TestKubeCapacityReadsAClusterWithoutMetricsNorPriorityClasses(t *testing.T) {
	c, err := readCapacity(context.Background(), openFakeKube(t, capacityFake(t)))
	if err != nil {
		t.Fatal(err)
	}

	// Metrics-server is absent (404) and PriorityClasses refused: still a report.
	if c.MetricsAvailable || c.MetricsForbidden || c.PriorityClassesRead || !c.PDBsRead {
		t.Errorf("flags %+v", c)
	}

	if len(c.Nodes) != 2 || c.Totals.Nodes != 2 || c.Totals.SchedulableNodes != 2 || c.Totals.Pods != 5 {
		t.Fatalf("nodes %+v totals %+v", c.Nodes, c.Totals)
	}

	w1 := c.Nodes[0]
	if w1.Name != "w1" || w1.CPUFree != 2.5 || w1.MemoryFree != 5<<30 || w1.PodsFree != 7 || w1.MemoryLimits != 3<<30 || w1.CPUUsage != 0 {
		t.Errorf("w1 %+v", w1)
	}

	names := []string{}
	for _, w := range c.Workloads {
		names = append(names, w.Kind+"/"+w.Name)
	}

	// Lowest priority first, no class first among equals, then the most memory.
	want := []string{"DaemonSet/proxy", "Pod/lone", "Deployment/web", "StatefulSet/db"}
	if len(names) != len(want) {
		t.Fatalf("workloads %v", names)
	}

	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("workloads %v, want %v", names, want)
		}
	}

	proxy, lone, web, db := c.Workloads[0], c.Workloads[1], c.Workloads[2], c.Workloads[3]

	if proxy.Replicas != 2 || proxy.QoS != qosBestEffort || proxy.Risk != riskEvictFirst || !proxy.MoreFitNA || !proxy.NoPriorityClass {
		t.Errorf("proxy %+v", proxy)
	}

	// Pending, no resources: computed BestEffort.
	if lone.QoS != qosBestEffort || lone.Risk != riskEvictFirst || !lone.MoreFitNA {
		t.Errorf("lone %+v", lone)
	}

	// The ReplicaSet's pods group under its Deployment; one was OOM-killed; the class could not
	// be read and the pods carry no priority: 0.
	if web.Replicas != 2 || web.CPURequest != 1 || web.MemoryRequest != 2<<30 || web.MemoryLimit != 2<<30 || web.CPULimit != 0 ||
		web.QoS != qosBurstable || web.PriorityClassName != "low" || web.NoPriorityClass || web.Priority != 0 ||
		web.PDB != "web" || web.PDBAllowed != 1 || web.OOMKills != 1 || web.Risk != riskOOM {
		t.Errorf("web %+v", web)
	}

	// 1 cpu / 2Gi: two on w1 (2.5 cpu, 5Gi free), one on w2 (1 cpu, 2Gi).
	if web.MoreFit != 3 || web.MoreFitNA {
		t.Errorf("web moreFit %d", web.MoreFit)
	}

	if db.Priority != 1000 || db.QoS != qosGuaranteed || db.Risk != riskOK || db.PDBAllowed != -1 || db.MoreFit != 7 {
		t.Errorf("db %+v", db)
	}

	// Losing w1: web-a goes to w2 and fills it, db-0 stays Pending.
	if n := c.NPlusOne; n.OK || n.Node != "w1" || n.PendingPods != 1 || n.ShortCPU != 0.5 || n.ShortMemory != 1<<30 {
		t.Errorf("nPlusOne %+v", n)
	}

	if len(c.Assumptions) == 0 {
		t.Error("no assumptions")
	}
}

func TestKubeCapacityUsesMetricsWhenServed(t *testing.T) {
	f := capacityFake(t)
	f.answerWith("GET /apis/metrics.k8s.io/v1beta1/nodes", http.StatusOK, `{"items":[{"metadata":{"name":"w1"},"usage":{"cpu":"1500m","memory":"3Gi"}}]}`)
	f.answerWith("GET /apis/metrics.k8s.io/v1beta1/pods", http.StatusOK, `{"items":[
		{"metadata":{"name":"db-0","namespace":"web"},"containers":[{"usage":{"cpu":"100m","memory":"950Mi"}}]}]}`)
	// w1's kubelet answers, w2's does not (404): w2 stays unmeasured.
	f.answerWith("GET /api/v1/nodes/w1/proxy/stats/summary", http.StatusOK, `{
		"node":{"fs":{"availableBytes":32212254720,"capacityBytes":107374182400,"usedBytes":75161927680},
		        "runtime":{"imageFs":{"availableBytes":32212254720,"capacityBytes":107374182400,"usedBytes":75161927680}}},
		"pods":[{"podRef":{"name":"db-0","namespace":"web"},"ephemeral-storage":{"usedBytes":536870912}}]}`)

	c, err := readCapacity(context.Background(), openFakeKube(t, f))
	if err != nil {
		t.Fatal(err)
	}

	if !c.MetricsAvailable || c.Nodes[0].CPUUsage != 1.5 || c.Totals.MemoryUsage != 3<<30 {
		t.Errorf("usage %+v %+v", c.Nodes[0], c.Totals)
	}

	db := c.Workloads[len(c.Workloads)-1]
	// 950Mi of a 1Gi limit: past 90 %. 512Mi of its 2Gi ephemeral limit: fine.
	if db.Name != "db" || db.MemoryUsage != 950<<20 || db.Risk != riskOOM ||
		db.EphemeralRequest != 1<<30 || db.EphemeralLimit != 2<<30 || db.EphemeralUsage != 512<<20 {
		t.Errorf("db %+v", db)
	}

	w1, w2 := c.Nodes[0], c.Nodes[1]
	if !c.DiskMeasured || c.DiskForbidden || !w1.DiskMeasured || w1.DiskCapacity != 100<<30 || w1.DiskAvailable != 30<<30 ||
		w1.ImageFsCapacity != 100<<30 || w1.EphemeralRequests != 1<<30 || w2.DiskMeasured || w2.DiskCapacity != 0 {
		t.Errorf("disk %v %v, w1 %+v, w2 %+v", c.DiskMeasured, c.DiskForbidden, w1, w2)
	}

	if c.Totals.DiskMeasuredNodes != 1 || c.Totals.DiskCapacity != 100<<30 || c.Totals.DiskAvailable != 30<<30 || c.Totals.EphemeralRequests != 1<<30 {
		t.Errorf("totals %+v", c.Totals)
	}
}

func TestKubeCapacityFailsWithoutNodesOrPods(t *testing.T) {
	f := capacityFake(t)
	f.answerWith("GET /api/v1/pods", http.StatusForbidden, `{"kind":"Status","reason":"Forbidden"}`)

	if _, err := readCapacity(context.Background(), openFakeKube(t, f)); err == nil {
		t.Error("no error without pods")
	}
}

func TestKubeCapacityPodQoS(t *testing.T) {
	decode := func(spec string) checkPod {
		t.Helper()

		p, err := decodeCheckPod(json.RawMessage(`{"metadata":{"name":"p","namespace":"n"},"spec":` + spec + `,"status":{"phase":"Pending"}}`))
		if err != nil {
			t.Fatal(err)
		}

		return p
	}

	cases := []struct{ spec, qos string }{
		{`{"containers":[{"name":"a"}]}`, qosBestEffort},
		// A CPU request alone is Burstable, not BestEffort.
		{`{"containers":[{"name":"a","resources":{"requests":{"cpu":"100m"}}}]}`, qosBurstable},
		{`{"containers":[{"name":"a","resources":{"requests":{"cpu":"1","memory":"1Gi"},"limits":{"cpu":"1000m","memory":"1024Mi"}}}]}`, qosGuaranteed},
		// Limits alone: the requests default to them.
		{`{"containers":[{"name":"a","resources":{"limits":{"cpu":"1","memory":"1Gi"}}}]}`, qosGuaranteed},
		{`{"containers":[{"name":"a","resources":{"limits":{"cpu":"1","memory":"1Gi"}}},{"name":"b"}]}`, qosBurstable},
		{`{"containers":[{"name":"a","resources":{"requests":{"memory":"512Mi"},"limits":{"cpu":"1","memory":"1Gi"}}}]}`, qosBurstable},
	}

	for _, c := range cases {
		if got := decode(c.spec).QoS; got != c.qos {
			t.Errorf("%s: %s, want %s", c.spec, got, c.qos)
		}
	}

	if p := decode(`{"containers":[{"name":"a","resources":{"limits":{"memory":"1Gi"}}},{"name":"b"}]}`); p.MemoryLimit != 0 {
		t.Errorf("a container without a limit leaves the pod unbounded: %v", p.MemoryLimit)
	}
}

func TestKubeCapacityResolvesPriorities(t *testing.T) {
	classes := []priorityClassObject{{Value: 100}, {Value: 5, GlobalDefault: true}}
	classes[0].Metadata.Name, classes[1].Metadata.Name = "low", "default"

	set := checkPod{PriorityClassName: "low", Priority: 7, prioritySet: true}
	named := checkPod{PriorityClassName: "low"}
	unknown := checkPod{PriorityClassName: "gone"}
	none := checkPod{}

	got := resolvePriorities([]checkPod{set, named, unknown, none}, classes)
	if got[0].Priority != 7 || got[1].Priority != 100 || got[2].Priority != 5 || got[3].Priority != 5 {
		t.Errorf("priorities %d %d %d %d", got[0].Priority, got[1].Priority, got[2].Priority, got[3].Priority)
	}

	if got := resolvePriorities([]checkPod{none}, nil); got[0].Priority != 0 {
		t.Errorf("no classes: %d", got[0].Priority)
	}
}

func TestKubeCapacityNPlusOneHoldsWithRoom(t *testing.T) {
	node := func(name string, cpuFree, memoryFree float64) capacityNode {
		return capacityNode{checkupNode: checkupNode{Name: name, Ready: true}, CPUFree: cpuFree, MemoryFree: memoryFree, PodsFree: 10}
	}

	nodes := []capacityNode{node("a", 2, 4<<30), node("b", 2, 4<<30)}
	pods := []checkPod{
		{kubePod: kubePod{Name: "x", Node: "a"}, Phase: "Running", CPU: 1, Memory: 2 << 30},
		{kubePod: kubePod{Name: "y", Node: "b"}, Phase: "Running", CPU: 1, Memory: 2 << 30},
		// A static pod stays with its node.
		{kubePod: kubePod{Name: "z", Node: "a"}, Phase: "Running", CPU: 8, Memory: 64 << 30, Static: true},
	}

	if n := nPlusOne(nodes, pods); !n.OK || n.Node != "" {
		t.Errorf("nPlusOne %+v", n)
	}
}

func TestKubeCapacityDemo(t *testing.T) {
	c := demoKubeCapacity()

	if c.NPlusOne.OK || !c.MetricsAvailable {
		t.Errorf("demo %+v", c.NPlusOne)
	}

	risks, critical := map[string]bool{}, false

	for i, w := range c.Workloads {
		risks[w.Risk] = true
		critical = critical || w.PriorityClassName == "system-cluster-critical"

		if i > 0 && c.Workloads[i-1].Priority > w.Priority {
			t.Errorf("not sorted at %s", w.Name)
		}
	}

	for _, r := range []string{riskOOM, riskDiskNear, riskEvictFirst, riskUnbounded, riskOK} {
		if !risks[r] {
			t.Errorf("no %s workload", r)
		}
	}

	if !critical {
		t.Error("no system-cluster-critical workload")
	}

	// Every node says its ephemeral storage and disk; demo-worker-2 is near full, not yet under
	// DiskPressure; report-cache is near its ephemeral limit; demo-worker-1's N+1 is unchanged.
	if !c.DiskMeasured || c.DiskForbidden || c.Totals.DiskMeasuredNodes != 5 || c.Totals.EphemeralAllocatable != 980<<30 {
		t.Errorf("demo disk %v/%v totals %+v", c.DiskMeasured, c.DiskForbidden, c.Totals)
	}

	for _, n := range c.Nodes {
		used := 100 - percentOf(n.DiskAvailable, n.DiskCapacity)
		if !n.DiskMeasured || n.EphemeralAllocatable <= 0 || n.EphemeralFree <= 0 || n.DiskPressure || (n.Name == "demo-worker-2") != (used >= 85) {
			t.Errorf("demo node %s: %+v (%.0f %% used)", n.Name, n, used)
		}
	}

	for _, w := range c.Workloads {
		if w.Name == "report-cache" && (w.Risk != riskDiskNear || percentOf(w.EphemeralUsage, w.EphemeralLimit) < diskNearPercent) {
			t.Errorf("report-cache %+v", w)
		}
	}

	if n := c.NPlusOne; n.Node != "demo-worker-1" || n.PendingPods != 9 || n.ShortCPU != 1.9 {
		t.Errorf("demo nPlusOne %+v", n)
	}

	out, err := KubeCapacity(mustDemoConfig(t), "", "")
	if err != nil {
		t.Fatalf("demo call: %v", err)
	}

	var decoded kubeCapacity
	if err := json.Unmarshal([]byte(out), &decoded); err != nil || len(decoded.Workloads) != len(c.Workloads) || decoded.NPlusOne.OK {
		t.Errorf("demo call answered %v %+v", err, decoded.NPlusOne)
	}
}
