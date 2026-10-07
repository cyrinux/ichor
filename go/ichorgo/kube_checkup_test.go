package ichorgo

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

var checkupNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

// findingKinds lists a section's findings as "kind:severity:name".
func findingKinds(s checkupSection) []string {
	out := []string{}
	for _, f := range s.Findings {
		out = append(out, f.Kind+":"+f.Severity+":"+f.Name)
	}

	return out
}

func sectionByID(t *testing.T, r checkupReport, id string) checkupSection {
	t.Helper()

	for _, s := range r.Sections {
		if s.ID == id {
			return s
		}
	}

	t.Fatalf("no section %q", id)

	return checkupSection{}
}

func TestParseQuantity(t *testing.T) {
	for in, want := range map[string]float64{
		"": 0, "250m": 0.25, "2": 2, "1Gi": 1 << 30, "500Mi": 500 << 20, "1G": 1e9, "1k": 1000, "1e3": 1000, "100n": 1e-7, "junk": 0, "110": 110,
	} {
		if got := parseQuantity(in); got < want*0.999999 || got > want*1.000001 {
			t.Errorf("parseQuantity(%q) = %v, want %v", in, got, want)
		}
	}

	if percentOf(1, 0) != 0 || percentOf(1, 3) != 33.3 {
		t.Fatalf("percentOf: %v %v", percentOf(1, 0), percentOf(1, 3))
	}
}

func TestReadCheckup(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"external-secrets.io","preferredVersion":{"version":"v1"}},{"name":"cilium.io","preferredVersion":{"version":"v2"}}]}`,
		"GET /api/v1/pods": `{"items":[
		  {"metadata":{"name":"web-1","namespace":"shop","creationTimestamp":"2026-10-06T08:00:00Z","ownerReferences":[{"kind":"ReplicaSet","name":"web"}]},
		   "spec":{"nodeName":"n1","containers":[{"name":"web","image":"web:1","resources":{"requests":{"cpu":"3800m","memory":"1Gi"}}}],"volumes":[{"persistentVolumeClaim":{"claimName":"data"}}]},
		   "status":{"phase":"Running","containerStatuses":[{"name":"web","ready":false,"restartCount":9,"state":{"waiting":{"reason":"CrashLoopBackOff","message":"back-off 5m0s"}},"lastState":{"terminated":{"reason":"Error","exitCode":2}}}]}},
		  {"metadata":{"name":"cache-0","namespace":"shop","creationTimestamp":"2026-10-06T08:00:00Z"},
		   "spec":{"nodeName":"n1","containers":[{"name":"cache","image":"cache:1"}]},
		   "status":{"phase":"Running","containerStatuses":[{"name":"cache","ready":true,"restartCount":2,"state":{"running":{}},"lastState":{"terminated":{"reason":"OOMKilled","exitCode":137}}}]}},
		  {"metadata":{"name":"big-1","namespace":"shop","creationTimestamp":"2026-10-06T11:00:00Z"},
		   "spec":{"containers":[{"name":"big","image":"big:1"}],"volumes":[{"persistentVolumeClaim":{"claimName":"scratch"}}]},
		   "status":{"phase":"Pending","conditions":[{"type":"PodScheduled","status":"False","reason":"Unschedulable","message":"0/2 nodes are available"}]}},
		  {"metadata":{"name":"young-1","namespace":"shop","creationTimestamp":"2026-10-06T11:58:00Z"},
		   "spec":{"containers":[{"name":"c","image":"c:1"}]},
		   "status":{"phase":"Pending","conditions":[{"type":"PodScheduled","status":"False","reason":"Unschedulable"}]}},
		  {"metadata":{"name":"gone-1","namespace":"shop","creationTimestamp":"2026-10-06T08:00:00Z","deletionTimestamp":"2026-10-06T10:00:00Z","finalizers":["example.com/hold"]},
		   "spec":{"nodeName":"n2","containers":[{"name":"c","image":"c:1"}]},"status":{"phase":"Running"}}]}`,
		"GET /api/v1/nodes": `{"items":[
		  {"metadata":{"name":"n1","labels":{"node-role.kubernetes.io/worker":"","zone":"a"}},"spec":{},
		   "status":{"allocatable":{"cpu":"4","memory":"8Gi","pods":"110"},"conditions":[{"type":"Ready","status":"True"},{"type":"DiskPressure","status":"True","reason":"KubeletHasDiskPressure"}],"nodeInfo":{"kubeletVersion":"v1.34.1"}}},
		  {"metadata":{"name":"n2"},"spec":{"unschedulable":true,"taints":[{"key":"node.kubernetes.io/unschedulable","effect":"NoSchedule","timeAdded":"2026-10-05T12:00:00Z"}]},
		   "status":{"allocatable":{"cpu":"4","memory":"8Gi","pods":"110"},"conditions":[{"type":"Ready","status":"True"}],"nodeInfo":{"kubeletVersion":"v1.30.0"}}}]}`,
		"GET /api/v1/persistentvolumeclaims": `{"items":[
		  {"metadata":{"name":"data","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},"spec":{"storageClassName":"fast"},"status":{"phase":"Bound","capacity":{"storage":"10Gi"}}},
		  {"metadata":{"name":"scratch","namespace":"shop","creationTimestamp":"2026-10-06T11:00:00Z"},"spec":{"storageClassName":"lazy","resources":{"requests":{"storage":"1Gi"}}},"status":{"phase":"Pending"}},
		  {"metadata":{"name":"unused","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z"},"spec":{"storageClassName":"lazy"},"status":{"phase":"Pending"}},
		  {"metadata":{"name":"old","namespace":"shop","creationTimestamp":"2026-10-01T00:00:00Z","deletionTimestamp":"2026-10-06T09:00:00Z","finalizers":["kubernetes.io/pvc-protection"]},"spec":{},"status":{"phase":"Bound"}}]}`,
		"GET /apis/storage.k8s.io/v1/storageclasses": `{"items":[{"metadata":{"name":"fast"},"volumeBindingMode":"Immediate"},{"metadata":{"name":"lazy"},"volumeBindingMode":"WaitForFirstConsumer"}]}`,
		"GET /api/v1/persistentvolumes": `{"items":[
		  {"metadata":{"name":"pv-1"},"spec":{"claimRef":{"namespace":"shop","name":"gone"},"persistentVolumeReclaimPolicy":"Retain"},"status":{"phase":"Released"}},
		  {"metadata":{"name":"pv-2"},"spec":{},"status":{"phase":"Bound"}}]}`,
		"GET /api/v1/nodes/n1/proxy/stats/summary": `{"pods":[{"podRef":{"name":"web-1","namespace":"shop"},"volume":[
		  {"name":"data","pvcRef":{"name":"data","namespace":"shop"},"usedBytes":10200547328,"capacityBytes":10737418240,"inodes":1000,"inodesUsed":10},
		  {"name":"tmp","usedBytes":1,"capacityBytes":2}]}]}`,
		"GET /apis/batch/v1/jobs": `{"items":[
		  {"metadata":{"name":"sync-1","namespace":"shop","ownerReferences":[{"kind":"CronJob","name":"sync"}]},"status":{"conditions":[{"type":"Failed","status":"True","reason":"BackoffLimitExceeded","lastTransitionTime":"2026-10-06T09:00:00Z"}]}},
		  {"metadata":{"name":"sync-2","namespace":"shop","ownerReferences":[{"kind":"CronJob","name":"sync"}]},"status":{"conditions":[{"type":"Complete","status":"True","lastTransitionTime":"2026-10-06T10:00:00Z"}]}},
		  {"metadata":{"name":"migrate","namespace":"shop"},"status":{"conditions":[{"type":"Failed","status":"True","reason":"DeadlineExceeded","lastTransitionTime":"2026-10-06T11:00:00Z"}]}}]}`,
		"GET /api/v1/events": `{"items":[
		  {"metadata":{"creationTimestamp":"2026-10-06T11:00:00Z"},"involvedObject":{"kind":"Pod","namespace":"shop","name":"web-1"},"type":"Warning","reason":"BackOff","message":"old","count":3,"lastTimestamp":"2026-10-06T11:30:00Z"},
		  {"metadata":{"creationTimestamp":"2026-10-06T11:40:00Z"},"involvedObject":{"kind":"Pod","namespace":"shop","name":"web-1"},"type":"Warning","reason":"BackOff","message":"new","eventTime":"2026-10-06T11:40:00.000000Z","series":{"count":7,"lastObservedTime":"2026-10-06T11:59:00.000000Z"}},
		  {"metadata":{"creationTimestamp":"2026-10-05T11:00:00Z"},"involvedObject":{"kind":"Pod","namespace":"shop","name":"web-1"},"type":"Warning","reason":"Stale","message":"yesterday","lastTimestamp":"2026-10-05T11:30:00Z"}]}`,
		"GET /metrics": "# HELP x\napiserver_requested_deprecated_apis{group=\"flowcontrol.apiserver.k8s.io\",removed_release=\"1.35\",resource=\"flowschemas\",subresource=\"\",version=\"v1beta3\"} 1\n" +
			"apiserver_requested_deprecated_apis{group=\"\",removed_release=\"\",resource=\"componentstatuses\",subresource=\"\",version=\"v1\"} 1\n" +
			"apiserver_requested_deprecated_apis{group=\"example.io\",removed_release=\"1.40\",resource=\"things\",subresource=\"\",version=\"v1beta1\"} 1\n",
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": `{"items":[{"metadata":{"name":"policy"},"webhooks":[
		  {"name":"a.policy","clientConfig":{"service":{"namespace":"policy","name":"hook"}}},
		  {"name":"b.policy","failurePolicy":"Ignore","clientConfig":{"service":{"namespace":"policy","name":"hook"}}},
		  {"name":"c.policy","clientConfig":{"url":"https://example.com"}}]}]}`,
		"GET /apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations": `{"items":[
		  {"metadata":{"name":"inject"},"webhooks":[{"name":"i","failurePolicy":"Ignore","clientConfig":{"service":{"namespace":"policy","name":"hook"}}}]},
		  {"metadata":{"name":"fine"},"webhooks":[{"name":"f","clientConfig":{"service":{"namespace":"ok","name":"hook"}}}]}]}`,
		"GET /apis/discovery.k8s.io/v1/namespaces/policy/endpointslices": `{"items":[{"endpoints":[{"conditions":{"ready":false}}]}]}`,
		"GET /apis/discovery.k8s.io/v1/namespaces/ok/endpointslices":     `{"items":[{"endpoints":[{"conditions":{"ready":true}}]}]}`,
		"GET /api/v1/resourcequotas":                                     `{"items":[{"metadata":{"name":"compute","namespace":"shop"},"status":{"hard":{"requests.cpu":"4","pods":"10"},"used":{"requests.cpu":"3800m","pods":"2"}}}]}`,
		"GET /api/v1/services": `{"items":[
		  {"metadata":{"name":"public","namespace":"shop","creationTimestamp":"2026-10-06T08:00:00Z"},"spec":{"type":"LoadBalancer"},"status":{"loadBalancer":{}}},
		  {"metadata":{"name":"served","namespace":"shop","creationTimestamp":"2026-10-06T08:00:00Z"},"spec":{"type":"LoadBalancer"},"status":{"loadBalancer":{"ingress":[{"ip":"192.0.2.9"}]}}},
		  {"metadata":{"name":"internal","namespace":"shop","creationTimestamp":"2026-10-06T08:00:00Z"},"spec":{"type":"ClusterIP"}}]}`,
		"GET /apis/cilium.io/v2/ciliumloadbalancerippools": `{"items":[{"metadata":{"name":"pool"},"status":{"conditions":[
		  {"type":"cilium.io/IPsAvailable","status":"Unknown","message":"0"},{"type":"cilium.io/IPsTotal","status":"Unknown","message":"4"}]}}]}`,
		"GET /api/v1/namespaces": `{"items":[
		  {"metadata":{"name":"shop"},"status":{"phase":"Active"}},
		  {"metadata":{"name":"old","deletionTimestamp":"2026-10-01T00:00:00Z"},"status":{"phase":"Terminating","conditions":[
		    {"type":"NamespaceContentRemaining","status":"True","message":"Some resources are remaining."},{"type":"NamespaceDeletionContentFailure","status":"False","message":"ignored"}]}}]}`,
		"GET /apis/certificates.k8s.io/v1/certificatesigningrequests": `{"items":[
		  {"metadata":{"name":"csr-1","creationTimestamp":"2026-10-06T09:00:00Z"},"spec":{"signerName":"kubernetes.io/kubelet-serving","username":"system:node:n1"},"status":{}},
		  {"metadata":{"name":"csr-2","creationTimestamp":"2026-10-06T10:00:00Z"},"spec":{"signerName":"kubernetes.io/kubelet-serving","username":"system:node:n1"},"status":{}},
		  {"metadata":{"name":"csr-3","creationTimestamp":"2026-10-06T10:00:00Z"},"spec":{"signerName":"kubernetes.io/kubelet-serving","username":"system:node:n2"},"status":{"conditions":[{"type":"Approved","status":"True"}]}},
		  {"metadata":{"name":"csr-4","creationTimestamp":"2026-10-06T11:59:00Z"},"spec":{"signerName":"kubernetes.io/kubelet-serving","username":"system:node:n2"},"status":{}}]}`,
		"GET /apis/external-secrets.io/v1/externalsecrets": `{"items":[
		  {"metadata":{"name":"db","namespace":"shop"},"spec":{"secretStoreRef":{"name":"vault"}},"status":{"conditions":[{"type":"Ready","status":"False","reason":"SecretSyncedError","message":"denied"}]}},
		  {"metadata":{"name":"ok","namespace":"shop"},"spec":{"secretStoreRef":{"name":"vault"}},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`,
		"GET /apis/external-secrets.io/v1/clustersecretstores": `{"items":[{"metadata":{"name":"vault"},"status":{"conditions":[{"type":"Ready","status":"False","reason":"InvalidProviderConfig"}]}}]}`,
		"GET /api/v1/secrets": `{"items":[
		  {"metadata":{"name":"sh.helm.release.v1.shop.v1","namespace":"shop","labels":{"owner":"helm","name":"shop","status":"superseded","version":"1"}}},
		  {"metadata":{"name":"sh.helm.release.v1.shop.v2","namespace":"shop","labels":{"owner":"helm","name":"shop","status":"failed","version":"2","modifiedAt":"1791280000"}}},
		  {"metadata":{"name":"sh.helm.release.v1.mon.v5","namespace":"mon","labels":{"owner":"helm","name":"mon","status":"pending-upgrade","version":"5","modifiedAt":"1791280000"}}},
		  {"metadata":{"name":"sh.helm.release.v1.fine.v1","namespace":"mon","labels":{"owner":"helm","name":"fine","status":"deployed","version":"1"}}}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := readCheckup(context.Background(), k, checkupNow)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string][]string{
		checkWorkloads:     {"podCrashLoop:critical:web-1", "podUnschedulable:critical:big-1", "podOOMKilled:warning:cache-0", "jobFailed:warning:migrate"},
		checkEvents:        {"event:info:web-1"},
		checkStorage:       {"volumeFull:critical:data", "pvcPending:critical:scratch", "pvReleased:info:pv-1"},
		checkUpgrade:       {"deprecatedAPI:critical:flowschemas.v1beta3.flowcontrol.apiserver.k8s.io", "deprecatedAPI:warning:things.v1beta1.example.io", "deprecatedAPI:info:componentstatuses.v1"},
		checkWebhooks:      {"webhookDown:critical:policy", "webhookSkipped:warning:inject"},
		checkCapacity:      {"nodeRequestsHigh:warning:n1", "quotaNearLimit:warning:compute"},
		checkNodes:         {"nodePressure:warning:n1", "nodeCordoned:warning:n2", "nodeVersionSkew:warning:n2"},
		checkLoadBalancers: {"lbPending:critical:public", "lbPoolExhausted:critical:pool"},
		checkTerminating:   {"namespaceTerminating:warning:old", "podTerminating:warning:gone-1", "pvcTerminating:warning:old"},
		checkCertificates:  {"csrPending:warning:system:node:n1"},
		checkSecrets:       {"secretStoreNotReady:critical:vault", "externalSecretFailed:warning:db"},
		checkHelm:          {"helmFailed:warning:shop", "helmPending:warning:mon"},
	}

	if len(r.Sections) != len(want) {
		t.Fatalf("%d sections", len(r.Sections))
	}

	for i, s := range r.Sections {
		if got := findingKinds(s); !reflect.DeepEqual(got, want[s.ID]) {
			t.Errorf("section %d %s:\n got  %v\n want %v (error %q)", i, s.ID, got, want[s.ID], s.Error)
		}

		if s.Error != "" && s.ID != checkSecrets {
			t.Errorf("section %s: error %q", s.ID, s.Error)
		}
	}

	if r.Status != healthCritical || r.KubeVersion != "v1.34.0" {
		t.Fatalf("status %q version %q", r.Status, r.KubeVersion)
	}

	events := sectionByID(t, r, checkEvents)
	if e := events.Findings[0]; e.Count != 10 || e.Message != "new" || e.Extra != "Pod" || events.Status != healthOK {
		t.Fatalf("events %+v status %s", e, events.Status)
	}

	if csr := sectionByID(t, r, checkCertificates).Findings[0]; csr.Count != 2 || csr.Extra != "kubernetes.io/kubelet-serving" {
		t.Fatalf("csr %+v", csr)
	}

	if hook := sectionByID(t, r, checkWebhooks).Findings[0]; hook.Count != 2 || hook.Extra != "policy/hook" || hook.Reason != "Validating" {
		t.Fatalf("webhook %+v", hook)
	}

	if len(r.Volumes) != 4 || r.Volumes[0].Name != "data" || !r.Volumes[0].Measured || r.Volumes[0].UsedPercent != 95 || r.Volumes[0].Pod != "web-1" {
		t.Fatalf("volumes %+v", r.Volumes)
	}

	n1 := r.Nodes[0]
	if n1.CPUPercent != 95 || n1.Pods != 2 || n1.PodCapacity != 110 || !reflect.DeepEqual(n1.Roles, []string{"worker"}) ||
		!reflect.DeepEqual(n1.Labels, []string{"node-role.kubernetes.io/worker", "zone=a"}) {
		t.Fatalf("n1 %+v", n1)
	}

	if n2 := r.Nodes[1]; !n2.Cordoned || !reflect.DeepEqual(n2.Taints, []string{"node.kubernetes.io/unschedulable:NoSchedule"}) || n2.Pods != 1 {
		t.Fatalf("n2 %+v", n2)
	}

	if len(r.Releases) != 3 || r.Releases[0].Name != "shop" || r.Releases[0].Revision != 2 || r.Releases[2].Status != "deployed" {
		t.Fatalf("releases %+v", r.Releases)
	}
}

func TestReadCheckupEmptyCluster(t *testing.T) {
	empty := `{"items":[]}`
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[]}`, "GET /api/v1/pods": empty, "GET /api/v1/nodes": empty, "GET /api/v1/persistentvolumeclaims": empty,
		"GET /api/v1/persistentvolumes": empty, "GET /apis/batch/v1/jobs": empty, "GET /api/v1/events": empty, "GET /metrics": "",
		"GET /apis/admissionregistration.k8s.io/v1/validatingwebhookconfigurations": empty,
		"GET /apis/admissionregistration.k8s.io/v1/mutatingwebhookconfigurations":   empty,
		"GET /api/v1/resourcequotas": empty, "GET /api/v1/services": empty, "GET /api/v1/namespaces": empty,
		"GET /apis/certificates.k8s.io/v1/certificatesigningrequests": empty, "GET /api/v1/secrets": empty,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := readCheckup(context.Background(), k, checkupNow)
	if err != nil || r.Status != healthOK {
		t.Fatalf("%+v %v", r, err)
	}

	for _, s := range r.Sections {
		want := healthOK
		if s.ID == checkLoadBalancers || s.ID == checkSecrets || s.ID == checkHelm {
			want = checkAbsent
		}

		if s.Status != want || s.Error != "" || s.Findings == nil {
			t.Errorf("section %s: %+v", s.ID, s)
		}
	}

	if r.Nodes == nil || r.Volumes == nil || r.Releases == nil {
		t.Fatal("nil lists")
	}
}

// A section that cannot be read says so and leaves the others standing.
func TestReadCheckupSectionErrors(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{"GET /apis": `{"groups":[]}`})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	r, err := readCheckup(context.Background(), k, checkupNow)
	if err != nil || r.Status != healthOK {
		t.Fatalf("%+v %v", r, err)
	}

	if s := sectionByID(t, r, checkWorkloads); s.Status != checkUnknown || s.Error == "" {
		t.Fatalf("workloads %+v", s)
	}
}

func TestNewSectionCapsAndRanks(t *testing.T) {
	findings := []checkupFinding{}
	for range checkupMaxFindings + 5 {
		findings = append(findings, checkupFinding{Kind: "a", Severity: sevInfo})
	}

	findings = append(findings, checkupFinding{Kind: "b", Severity: sevWarning})

	s := newSection("x", 1, findings)
	if s.Status != healthWarning || s.Truncated != 6 || len(s.Findings) != checkupMaxFindings || s.Findings[0].Kind != "b" {
		t.Fatalf("%+v", s)
	}
}

func TestPodFinding(t *testing.T) {
	old := checkupNow.Add(-time.Hour).UnixMilli()

	for name, c := range map[string]struct {
		pod  checkPod
		kind string
		sev  string
	}{
		"healthy":       {checkPod{kubePod: kubePod{Healthy: true, Status: "Running", Created: old}}, "", ""},
		"image":         {checkPod{kubePod: kubePod{Status: "ImagePullBackOff", Created: old, Images: []string{"x:1"}}, Phase: "Pending"}, findPodImagePull, sevCritical},
		"image, young":  {checkPod{kubePod: kubePod{Status: "ErrImagePull", Created: checkupNow.UnixMilli()}, Phase: "Pending"}, findPodImagePull, sevWarning},
		"init crash":    {checkPod{kubePod: kubePod{Status: "Init:CrashLoopBackOff", Created: old}, Phase: "Pending"}, findPodCrashLoop, sevCritical},
		"creating":      {checkPod{kubePod: kubePod{Status: "ContainerCreating", Created: old}, Phase: "Pending"}, findPodStuckStarting, sevWarning},
		"creating, new": {checkPod{kubePod: kubePod{Status: "ContainerCreating", Created: checkupNow.UnixMilli()}, Phase: "Pending"}, "", ""},
		"not ready":     {checkPod{kubePod: kubePod{Status: "Running", Created: old, Ready: 1, Containers: 2}, Phase: "Running"}, findPodNotReady, sevWarning},
		"job pod":       {checkPod{kubePod: kubePod{Status: "Error", Created: old, Owner: "Job/x"}, Phase: "Failed"}, "", ""},
		"evicted":       {checkPod{kubePod: kubePod{Status: "Evicted", Created: old}, Phase: "Failed"}, findPodFailed, sevInfo},
		"unknown":       {checkPod{kubePod: kubePod{Status: "ContainerStatusUnknown", Created: old}, Phase: "Running"}, findPodFailed, sevWarning},
		"terminating":   {checkPod{kubePod: kubePod{Status: "Terminating", Created: old}, Deleted: checkupNow}, "", ""},
	} {
		f, ok := podFinding(c.pod, checkupNow)
		if ok != (c.kind != "") || (ok && (f.Kind != c.kind || f.Severity != c.sev)) {
			t.Errorf("%s: %+v %v", name, f, ok)
		}
	}
}

func TestDrainFinding(t *testing.T) {
	node := func(name string, cpu, mem float64) checkupNode {
		return checkupNode{Name: name, Ready: true, CPUAllocatable: 4, CPURequests: cpu, MemoryAllocatable: 8, MemoryRequests: mem}
	}

	if _, ok := drainFinding([]checkupNode{node("a", 1, 1), node("b", 1, 1)}); ok {
		t.Fatal("room enough")
	}

	if _, ok := drainFinding([]checkupNode{node("a", 4, 8)}); ok {
		t.Fatal("one node has nowhere to drain to by design")
	}

	f, ok := drainFinding([]checkupNode{node("a", 0.1, 7.5), node("b", 0.1, 7), node("c", 0.1, 2)})
	if !ok || f.Name != "c" || f.Extra != "memory" || f.Count != 3 || f.Severity != sevInfo {
		t.Fatalf("%+v %v", f, ok)
	}

	cordoned := node("c", 0, 0)
	cordoned.Cordoned = true

	if f, ok := drainFinding([]checkupNode{node("a", 3, 2), node("b", 3, 2), cordoned}); !ok || f.Count != 2 || f.Extra != "cpu" {
		t.Fatalf("%+v %v", f, ok)
	}
}

func TestKubeMinor(t *testing.T) {
	for in, want := range map[string][3]int{"v1.34.1": {1, 34, 1}, "1.32": {1, 32, 1}, "v1.30+": {1, 30, 1}, "": {0, 0, 0}, "main": {0, 0, 0}} {
		major, minor, ok := kubeMinor(in)
		if got := [3]int{major, minor, map[bool]int{true: 1}[ok]}; got != want {
			t.Errorf("kubeMinor(%q) = %v", in, got)
		}
	}
}

func TestReadEvents(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /api/v1/namespaces/shop/events": `{"items":[
		  {"metadata":{"creationTimestamp":"2026-10-06T10:00:00Z"},"involvedObject":{"kind":"Deployment","namespace":"shop","name":"web"},"type":"Normal","reason":"ScalingReplicaSet","message":"up","source":{"component":"deployment-controller"}},
		  {"metadata":{"creationTimestamp":"2026-10-06T11:00:00Z"},"involvedObject":{"kind":"Pod","namespace":"shop","name":"web-5d-abc"},"type":"Warning","reason":"BackOff","message":"` + strings.Repeat("x", 500) + `","count":4,"reportingComponent":"kubelet"},
		  {"metadata":{"creationTimestamp":"2026-10-06T11:30:00Z"},"involvedObject":{"kind":"Pod","namespace":"shop","name":"webhook-1"},"type":"Normal","reason":"Pulled","message":"other"}]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := readEvents(context.Background(), k, "shop", "", "web")
	if err != nil || len(got.Events) != 2 {
		t.Fatalf("%+v %v", got, err)
	}

	if e := got.Events[0]; e.Name != "web-5d-abc" || e.Count != 4 || e.Source != "kubelet" || len(e.Message) > eventMessageLimit+4 || e.First == 0 {
		t.Fatalf("%+v", e)
	}

	if _, err := readEvents(context.Background(), k, "shop", "Pod", "web-5d-abc"); err != nil {
		t.Fatal(err)
	}

	reqs := f.recorded()
	if q := reqs[len(reqs)-1].query; !strings.Contains(q, "involvedObject.kind%3DPod%2CinvolvedObject.name%3Dweb-5d-abc") {
		t.Fatalf("query %q", q)
	}
}

func TestKubeEventsValidatesAndDemo(t *testing.T) {
	yaml := demoConfigForTest(t)

	for _, bad := range [][3]string{{"", "", ""}, {"demo", "Pod/x", "a"}, {"demo", "Pod", "a,b=c"}, {"bad ns", "", ""}} {
		if _, err := KubeEvents(yaml, "", "", bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}

	out, err := KubeEvents(yaml, "", "", "demo", "", "worker")
	if err != nil {
		t.Fatal(err)
	}

	var list kubeEventList
	if err := json.Unmarshal([]byte(out), &list); err != nil || len(list.Events) != 4 {
		t.Fatalf("%s %v", out, err)
	}

	out, err = KubeEvents(yaml, "", "", "demo", "Deployment", "worker")
	if err != nil || json.Unmarshal([]byte(out), &list) != nil || len(list.Events) != 1 {
		t.Fatalf("%s %v", out, err)
	}
}

// The demo shows every section, with findings the apps can word.
func TestKubeCheckupDemo(t *testing.T) {
	out, err := KubeCheckup(demoConfigForTest(t), "", "")
	if err != nil {
		t.Fatal(err)
	}

	var r checkupReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatal(err)
	}

	if r.Status != healthCritical || len(r.Sections) != 12 || len(r.Nodes) != 5 || len(r.Volumes) == 0 || len(r.Releases) == 0 {
		t.Fatalf("%+v", r)
	}

	for _, s := range r.Sections {
		if len(s.Findings) == 0 || s.Status == checkAbsent {
			t.Errorf("demo section %s is empty", s.ID)
		}
	}

	if helm := sectionByID(t, r, checkHelm); !reflect.DeepEqual(findingKinds(helm), []string{"helmPending:warning:kube-prometheus-stack", "helmFailed:warning:hello-ichor"}) {
		t.Fatalf("helm %v", findingKinds(helm))
	}
}

func TestRollbackRefusesUnknownNodeAndDemo(t *testing.T) {
	yaml := demoConfigForTest(t)

	if err := Rollback(yaml, "", ""); err == nil {
		t.Fatal("no node accepted")
	}

	if err := Rollback(yaml, "", "192.0.2.10"); err == nil {
		t.Fatal("the demo rolled back")
	}
}
