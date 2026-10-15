package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func garageOperatorFixtures(t *testing.T) ([]garageOpClusterObject, []garageOpBucketObject, []garageOpKeyObject, []garageOpNodeObject, []garageOpDeploymentObject) {
	t.Helper()

	return loadFixture[kubeList[garageOpClusterObject]](t, "garageoperator/clusters.json").Items,
		loadFixture[kubeList[garageOpBucketObject]](t, "garageoperator/buckets.json").Items,
		loadFixture[kubeList[garageOpKeyObject]](t, "garageoperator/keys.json").Items,
		loadFixture[kubeList[garageOpNodeObject]](t, "garageoperator/nodes.json").Items,
		loadFixture[kubeList[garageOpDeploymentObject]](t, "garageoperator/deployments.json").Items
}

func TestGarageOpClusterVersion(t *testing.T) {
	for _, tc := range []struct{ preferred, want string }{
		{"v1beta2", "v1beta2"},
		{"v1beta1", "v1beta1"},
		{"v1beta3", "v1beta1"}, // an unknown preferred version: v1beta1 stays served
	} {
		if got := garageOpClusterVersion(tc.preferred); got != tc.want {
			t.Errorf("garageOpClusterVersion(%q) = %q, want %q", tc.preferred, got, tc.want)
		}
	}
}

func TestMapGarageOperator(t *testing.T) {
	clusters, buckets, keys, nodes, deployments := garageOperatorFixtures(t)
	out := mapGarageOperator(clusters, buckets, keys, nodes, deployments, fixtureNow)

	type summary struct {
		Name, Phase, Health string
		Reasons             []string
	}

	var got []summary
	for _, c := range out.Clusters {
		got = append(got, summary{c.Name, c.Phase, c.Health, c.Reasons})
	}

	want := []summary{
		{"edge", "Running", healthCritical, []string{garageOpReasonAdminUnreachable}},
		{"garage", "Degraded", healthWarning, []string{
			garageOpReasonQuorumAtRisk, garageOpReasonDegraded, garageOpReasonStorageReplicas,
			garageOpReasonLayoutStaged, garageOpReasonTombstones, garageOpReasonNodeDisconnected,
			garageOpReasonNodeNotInLayout,
		}},
		{"archive", "Ready", healthOK, []string{}},
	}

	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("clusters:\ngot  %v\nwant %v", got, want)
	}

	c := out.Clusters[1]
	if c.StorageReady != 2 || c.StorageReplicas != 3 || c.GarageVersion != "v2.3.0" || c.LayoutVersion != 12 || c.StagedLayout != 13 {
		t.Errorf("cluster fields: %+v", c)
	}

	if len(c.Tombstones) != 1 || c.Tombstones[0] != "gn-gw-0" {
		t.Errorf("tombstones: %v", c.Tombstones)
	}

	// The kept conditions: the False ones with their reason, and QuorumAtRisk when True.
	var condTypes []string
	for _, cond := range c.Conditions {
		condTypes = append(condTypes, cond.Type)
	}

	if fmt.Sprint(condTypes) != fmt.Sprint([]string{"NodesConnected", "QuorumAtRisk"}) {
		t.Errorf("cluster conditions: %v", condTypes)
	}

	gotB := []summary{}
	for _, b := range out.Buckets {
		gotB = append(gotB, summary{b.Name, b.Phase, b.Health, b.Reasons})
	}

	wantB := []summary{
		{"backups", "Ready", healthWarning, []string{garageOpReasonLifecycle}},
		{"site", "Ready", healthWarning, []string{garageOpReasonWebsiteExposure}},
		{"stuck", "Ready", healthWarning, []string{garageOpReasonLookupStuck}},
		{"files", "Ready", healthOK, []string{}},
	}

	if fmt.Sprint(gotB) != fmt.Sprint(wantB) {
		t.Errorf("buckets:\ngot  %v\nwant %v", gotB, wantB)
	}

	backups := out.Buckets[0]
	if backups.MaxSize != "10Gi" || backups.Quota == nil || backups.Quota.SizePercent != 28 || backups.Keys != 2 {
		t.Errorf("bucket fields: %+v", backups)
	}

	gotK := []summary{}
	for _, k := range out.Keys {
		gotK = append(gotK, summary{k.Name, k.Phase, k.Health, k.Reasons})
	}

	wantK := []summary{
		{"broken", "Failed", healthCritical, []string{garageOpReasonKeyFailed}},
		{"app-writer", "Ready", healthOK, []string{}},
		{"backup-agent", "Ready", healthOK, []string{}},
	}

	if fmt.Sprint(gotK) != fmt.Sprint(wantK) {
		t.Errorf("keys:\ngot  %v\nwant %v", gotK, wantK)
	}

	agent := out.Keys[2]
	if agent.AllBuckets == nil || !agent.AllBuckets.Owner || !agent.ClusterWide {
		t.Errorf("key allBuckets: %+v", agent)
	}

	writer := out.Keys[1]
	if len(writer.Buckets) != 1 || writer.Buckets[0].Bucket != "files" || !writer.Buckets[0].Write {
		t.Errorf("key bucketPermissions: %+v", writer)
	}

	gotN := []summary{}
	for _, n := range out.Nodes {
		gotN = append(gotN, summary{n.Name, n.Phase, n.Health, n.Reasons})
	}

	wantN := []summary{
		{"garage-2xv9p", "Degraded", healthWarning, []string{garageOpReasonNodeDisconnected}},
		{"gw-0", "Running", healthWarning, []string{garageOpReasonNodeNotInLayout}},
		{"garage-4kq7z", "Ready", healthOK, []string{}},
	}

	if fmt.Sprint(gotN) != fmt.Sprint(wantN) {
		t.Errorf("nodes:\ngot  %v\nwant %v", gotN, wantN)
	}

	down := out.Nodes[0]
	if down.LastSeen != 2*24*3600 || down.Zone != "zone-3" || down.Capacity != "500Gi" {
		t.Errorf("node fields: %+v", down)
	}

	if out.Operator == nil || out.Operator.Namespace != "garage-operator-system" || out.Operator.Ready != 1 || out.Operator.Desired != 1 || out.Operator.Health != healthOK {
		t.Errorf("operator: %+v", out.Operator)
	}
}

func TestGarageOpKeyPermCap(t *testing.T) {
	obj := garageOpKeyObject{}
	for i := range 25 {
		obj.Spec.BucketPermissions = append(obj.Spec.BucketPermissions, struct {
			BucketRef *struct {
				Name string `json:"name"`
			} `json:"bucketRef"`
			BucketID    string `json:"bucketId"`
			GlobalAlias string `json:"globalAlias"`
			Read        bool   `json:"read"`
			Write       bool   `json:"write"`
			Owner       bool   `json:"owner"`
		}{BucketRef: &struct {
			Name string `json:"name"`
		}{Name: fmt.Sprintf("bucket-%02d", i)}, Read: true})
	}

	k := mapGarageOpKey(obj)
	if len(k.Buckets) != garageOpKeyPermCap || k.BucketsTotal != 25 {
		t.Errorf("capped permissions: %d listed of %d", len(k.Buckets), k.BucketsTotal)
	}
}

func TestGarageOperatorDown(t *testing.T) {
	_, _, _, _, deployments := garageOperatorFixtures(t)
	deployments[0].Status.ReadyReplicas = 0

	op := mapGarageOperatorDeployment(deployments)
	if op == nil || op.Health != healthCritical || fmt.Sprint(op.Reasons) != fmt.Sprint([]string{garageOpReasonOperatorDown}) {
		t.Fatalf("operator down: %+v", op)
	}

	if op := mapGarageOperatorDeployment(nil); op != nil {
		t.Errorf("no deployment: %+v", op)
	}
}

func TestReadGarageOperatorEndToEnd(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"garage.rajsingh.info","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET /apis/garage.rajsingh.info/v1beta2/garageclusters": string(readFixture(t, "garageoperator/clusters.json")),
		"GET /apis/garage.rajsingh.info/v1beta1/garagebuckets":  string(readFixture(t, "garageoperator/buckets.json")),
		"GET /apis/garage.rajsingh.info/v1beta1/garagekeys":     string(readFixture(t, "garageoperator/keys.json")),
		"GET /apis/garage.rajsingh.info/v1beta1/garagenodes":    string(readFixture(t, "garageoperator/nodes.json")),
		"GET /apis/apps/v1/deployments":                         string(readFixture(t, "garageoperator/deployments.json")),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	out := readGarageOperator(context.Background(), k, "v1beta2", fixtureNow)
	if out.Error != "" || out.Version != "v1beta2" || out.Truncated {
		t.Fatalf("layer: error %q version %q truncated %v", out.Error, out.Version, out.Truncated)
	}

	if len(out.Clusters) != 3 || len(out.Buckets) != 4 || len(out.Keys) != 3 || len(out.Nodes) != 3 {
		t.Fatalf("counts: %d clusters, %d buckets, %d keys, %d nodes", len(out.Clusters), len(out.Buckets), len(out.Keys), len(out.Nodes))
	}

	// The cluster kind was asked at the preferred version, the others at v1beta1.
	for _, r := range f.recorded() {
		if strings.HasPrefix(r.path, "/apis/garage.rajsingh.info/v1beta1/garageclusters") {
			t.Errorf("garageclusters asked at v1beta1: %s", r.path)
		}
	}
}

func TestReadGarageOperatorGroupAbsent(t *testing.T) {
	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[]}`,
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	groups, err := readAPIGroups(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}

	if _, served := groups[groupGarageOperator]; served {
		t.Fatal("the group should not be served")
	}

	layer := readGarageOperator(context.Background(), k, "", fixtureNow)
	_ = layer // not reached in readDataServices; the section stays nil instead
}

func TestReadDataServicesGarageOperator(t *testing.T) {
	// The operator's CRDs are served without a hint: the garage section opens anyway, with
	// its CRs beside the instances the CLI reads.
	pods := []dsPod{
		fakePod("garage", "garage-a", "node-1", true, map[string]string{"app.kubernetes.io/name": "garage"}, "garage", "dxflrs/amd64_garage:v2.3.0"),
		fakePod("garage-archive", "garage-archive-0", "node-2", true, map[string]string{"app.kubernetes.io/name": "garage-archive"}, "garage", "dxflrs/amd64_garage:v2.3.0"),
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[{"name":"garage.rajsingh.info","preferredVersion":{"version":"v1beta2"}}]}`,
		"GET /apis/garage.rajsingh.info/v1beta2/garageclusters": string(readFixture(t, "garageoperator/clusters.json")),
		"GET /apis/garage.rajsingh.info/v1beta1/garagebuckets":  string(readFixture(t, "garageoperator/buckets.json")),
		"GET /apis/garage.rajsingh.info/v1beta1/garagekeys":     string(readFixture(t, "garageoperator/keys.json")),
		"GET /apis/garage.rajsingh.info/v1beta1/garagenodes":    string(readFixture(t, "garageoperator/nodes.json")),
		"GET /apis/apps/v1/deployments":                         string(readFixture(t, "garageoperator/deployments.json")),
		"GET /api/v1/pods":                                      podListJSON(t, pods...),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err := readDataServices(context.Background(), k, fixtureRunner(t, "garage/v2.3.0/single-node"), parseHints(""), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}

	g := res.Garage
	if g == nil || g.Version != "v1beta2" || len(g.Instances) != 2 {
		t.Fatalf("garage section: %+v", g)
	}

	if len(g.Clusters) != 3 || len(g.Buckets) != 4 || len(g.Keys) != 3 || len(g.Nodes) != 3 || g.Operator == nil {
		t.Fatalf("operator layer: %d clusters, %d buckets, %d keys, %d nodes, operator %+v",
			len(g.Clusters), len(g.Buckets), len(g.Keys), len(g.Nodes), g.Operator)
	}

	// The CRs keep the layer's worst-first order next to the instances.
	if g.Clusters[0].Name != "edge" || g.Buckets[0].Cluster != "garage" || g.Keys[0].Name != "broken" {
		t.Errorf("layer order: cluster %q, bucket of %q, key %q", g.Clusters[0].Name, g.Buckets[0].Cluster, g.Keys[0].Name)
	}

	// Hints without garage and without the operator: no section at all.
	f2 := newFakeKubeAPI(t, map[string]string{
		"GET /apis": `{"groups":[]}`,
	})

	k2, err := openKubeClient(context.Background(), f2.kubeconfigFor(f2.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	res, err = readDataServices(context.Background(), k2, nil, parseHints("longhorn"), fixtureNow)
	if err != nil || res.Garage != nil {
		t.Fatalf("no operator, no hint: %+v %v", res.Garage, err)
	}
}

func TestGarageOpListCap(t *testing.T) {
	items := []map[string]any{}
	for i := range garageOpCap + 1 {
		items = append(items, map[string]any{
			"metadata": map[string]any{"name": fmt.Sprintf("bucket-%03d", i), "namespace": "garage"},
			"spec":     map[string]any{"clusterRef": map[string]any{"name": "garage"}},
			"status":   map[string]any{"phase": "Ready"},
		})
	}

	raw, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeKubeAPI(t, map[string]string{
		"GET /apis/garage.rajsingh.info/v1beta1/garagebuckets": string(raw),
	})

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	var buckets []garageOpBucketObject
	truncated := false
	if err := garageOpList(context.Background(), k, "/apis/garage.rajsingh.info/v1beta1/garagebuckets", &buckets, &truncated, nil); err != nil {
		t.Fatal(err)
	}

	if len(buckets) != garageOpCap || !truncated {
		t.Errorf("cap: %d buckets, truncated %v", len(buckets), truncated)
	}
}

func TestKubeGarageOperatorDemo(t *testing.T) {
	cfg, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := KubeGarageOperator(cfg, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var layer garageOperatorLayer
	if err := json.Unmarshal([]byte(out), &layer); err != nil {
		t.Fatal(err)
	}

	if layer.Version != garageOpClusterV2 || len(layer.Clusters) != 2 || len(layer.Buckets) != 2 || len(layer.Keys) != 2 || len(layer.Nodes) != 1 || layer.Operator == nil {
		t.Fatalf("demo layer: %+v", layer)
	}

	// The demo layer merges into the demo garage section with the instances attached.
	out, err = KubeDataServices(cfg, "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	var res dataServices
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatal(err)
	}

	g := res.Garage
	if g == nil || g.Version != garageOpClusterV2 || len(g.Instances) != 2 || len(g.Clusters) != 2 {
		t.Fatalf("demo garage section: %+v", g)
	}

	if g.Operator == nil || len(g.Buckets) != 2 || len(g.Keys) != 2 || len(g.Nodes) != 1 {
		t.Errorf("demo garage section layer: %+v", g)
	}
}

func TestMapGarageOpKeyBucketWithoutRef(t *testing.T) {
	// A permission may name its bucket by id or global alias instead of bucketRef: the
	// reader must not dereference the missing ref.
	for _, tc := range []struct{ name, perm, want string }{
		{"bucketRef", `{"bucketRef":{"name":"files"},"read":true}`, "files"},
		{"bucketId only", `{"bucketId":"a3f8c1","read":true}`, "a3f8c1"},
		{"globalAlias only", `{"globalAlias":"shared-assets","read":true}`, "shared-assets"},
		{"empty bucketRef", `{"bucketRef":{},"bucketId":"4f1d","read":true}`, "4f1d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var obj garageOpKeyObject
			raw := `{"metadata":{"name":"reader","namespace":"garage"},"spec":{"clusterRef":{"name":"garage"},"bucketPermissions":[` + tc.perm + `]}}`
			if err := json.Unmarshal([]byte(raw), &obj); err != nil {
				t.Fatal(err)
			}

			k := mapGarageOpKey(obj)
			if len(k.Buckets) != 1 || k.Buckets[0].Bucket != tc.want || !k.Buckets[0].Read {
				t.Errorf("permissions: %+v, want bucket %q", k.Buckets, tc.want)
			}
		})
	}
}

func TestMapGarageOpClusterNodesByClusterRefNamespace(t *testing.T) {
	// Node CRs belong to the cluster their clusterRef names, in clusterRef.namespace when
	// set, else in their own namespace.
	decode := func(raw string, out any) {
		t.Helper()

		if err := json.Unmarshal([]byte(raw), out); err != nil {
			t.Fatal(err)
		}
	}

	var cluster garageOpClusterObject
	decode(`{"metadata":{"name":"garage","namespace":"garage"},"status":{"phase":"Ready"}}`, &cluster)

	node := func(ns, refNs string) garageOpNodeObject {
		var n garageOpNodeObject
		decode(`{"metadata":{"name":"node-a","namespace":"`+ns+`"},"spec":{"clusterRef":{"name":"garage","namespace":"`+refNs+`"}},"status":{"connected":false,"inLayout":true}}`, &n)

		return n
	}

	for _, tc := range []struct {
		name     string
		node     garageOpNodeObject
		attaches bool
	}{
		{"same namespace", node("garage", ""), true},
		{"other namespace, ref to the cluster's", node("storage", "garage"), true},
		{"cluster's namespace, ref elsewhere", node("garage", "elsewhere"), false},
		{"other namespace, no ref namespace", node("storage", ""), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := mapGarageOperator([]garageOpClusterObject{cluster}, nil, nil, []garageOpNodeObject{tc.node}, nil, fixtureNow)

			got := slices.Contains(out.Clusters[0].Reasons, garageOpReasonNodeDisconnected)
			if got != tc.attaches {
				t.Errorf("disconnected node counted for the cluster: %v, want %v (reasons %v)", got, tc.attaches, out.Clusters[0].Reasons)
			}
		})
	}
}

// garageOpFakeAPI serves the operator's four kinds and the Deployments, each kind empty
// unless overridden.
func garageOpFakeAPI(t *testing.T, overrides map[string]string) *kubeClient {
	t.Helper()

	empty := `{"items":[]}`
	routes := map[string]string{
		"GET /apis/garage.rajsingh.info/v1beta2/garageclusters": empty,
		"GET /apis/garage.rajsingh.info/v1beta1/garagebuckets":  empty,
		"GET /apis/garage.rajsingh.info/v1beta1/garagekeys":     empty,
		"GET /apis/garage.rajsingh.info/v1beta1/garagenodes":    empty,
		"GET /apis/apps/v1/deployments":                         empty,
	}
	for route, body := range overrides {
		routes[route] = body
	}

	f := newFakeKubeAPI(t, routes)

	k, err := openKubeClient(context.Background(), f.kubeconfigFor(f.URL), nil, "")
	if err != nil {
		t.Fatal(err)
	}

	return k
}

func garageOpItems(t *testing.T, n int, item func(i int) map[string]any) string {
	t.Helper()

	items := make([]map[string]any, 0, n)
	for i := range n {
		items = append(items, item(i))
	}

	raw, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

func TestReadGarageOperatorManyDeployments(t *testing.T) {
	// More Deployments than garageOpCap with the operator last: its health is still read,
	// and Deployments the section does not show do not truncate it.
	deployment := func(ns, name, image string, ready int) map[string]any {
		return map[string]any{
			"metadata": map[string]any{"name": name, "namespace": ns},
			"spec":     map[string]any{"template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"image": image}}}}},
			"status":   map[string]any{"replicas": 1, "readyReplicas": ready},
		}
	}

	raw := garageOpItems(t, garageOpCap+1, func(i int) map[string]any {
		if i == garageOpCap {
			return deployment("garage-operator-system", "garage-operator", "ghcr.io/rajsinghtech/garage-operator:v0.6.2", 0)
		}

		return deployment("demo", fmt.Sprintf("app-%03d", i), "ghcr.io/example/other:v1.0.0", 1)
	})

	k := garageOpFakeAPI(t, map[string]string{"GET /apis/apps/v1/deployments": raw})

	out := readGarageOperator(context.Background(), k, "v1beta2", fixtureNow)
	if out.Error != "" || out.Truncated {
		t.Fatalf("layer: error %q truncated %v", out.Error, out.Truncated)
	}

	if out.Operator == nil || out.Operator.Name != "garage-operator" || out.Operator.Health != healthCritical {
		t.Errorf("operator: %+v", out.Operator)
	}
}

func TestReadGarageOperatorTruncatedByOneKind(t *testing.T) {
	// One kind over the cap truncates the layer; the kinds are listed in parallel, each
	// with its own flag (run with -race).
	raw := garageOpItems(t, garageOpCap+1, func(i int) map[string]any {
		return map[string]any{
			"metadata": map[string]any{"name": fmt.Sprintf("key-%03d", i), "namespace": "garage"},
			"spec":     map[string]any{"clusterRef": map[string]any{"name": "garage"}},
		}
	})

	k := garageOpFakeAPI(t, map[string]string{"GET /apis/garage.rajsingh.info/v1beta1/garagekeys": raw})

	out := readGarageOperator(context.Background(), k, "v1beta2", fixtureNow)
	if !out.Truncated || len(out.Keys) != garageOpCap {
		t.Errorf("truncated %v, %d keys", out.Truncated, len(out.Keys))
	}
}
