package ichorgo

import (
	"context"
	"encoding/json"
	"fmt"
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
		Name, Instance, Phase, Health string
		Reasons                       []string
	}

	var got []summary
	for _, c := range out.Clusters {
		got = append(got, summary{c.Name, c.Instance, c.Phase, c.Health, c.Reasons})
	}

	want := []summary{
		// Instance attachment happens in attachGarageOperator (see the end-to-end tests).
		{"edge", "", "Running", healthCritical, []string{garageOpReasonAdminUnreachable}},
		{"garage", "", "Degraded", healthWarning, []string{
			garageOpReasonQuorumAtRisk, garageOpReasonDegraded, garageOpReasonStorageReplicas,
			garageOpReasonLayoutStaged, garageOpReasonTombstones, garageOpReasonNodeDisconnected,
			garageOpReasonNodeNotInLayout,
		}},
		{"archive", "", "Ready", healthOK, []string{}},
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
		gotB = append(gotB, summary{b.Name, b.Instance, b.Phase, b.Health, b.Reasons})
	}

	wantB := []summary{
		{"backups", "", "Ready", healthWarning, []string{garageOpReasonLifecycle}},
		{"site", "", "Ready", healthWarning, []string{garageOpReasonWebsiteExposure}},
		{"stuck", "", "Ready", healthWarning, []string{garageOpReasonLookupStuck}},
		{"files", "", "Ready", healthOK, []string{}},
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
		gotK = append(gotK, summary{k.Name, k.Instance, k.Phase, k.Health, k.Reasons})
	}

	wantK := []summary{
		{"broken", "", "Failed", healthCritical, []string{garageOpReasonKeyFailed}},
		{"app-writer", "", "Ready", healthOK, []string{}},
		{"backup-agent", "", "Ready", healthOK, []string{}},
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
		gotN = append(gotN, summary{n.Name, n.Instance, n.Phase, n.Health, n.Reasons})
	}

	wantN := []summary{
		{"garage-2xv9p", "", "Degraded", healthWarning, []string{garageOpReasonNodeDisconnected}},
		{"gw-0", "", "Running", healthWarning, []string{garageOpReasonNodeNotInLayout}},
		{"garage-4kq7z", "", "Ready", healthOK, []string{}},
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
	// The operator's CRDs are served without a hint: the garage section opens anyway, and
	// its CRs attach to the instances the CLI reads.
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

	// The unmatched edge handle stays at the section root; the others attach.
	if g.Clusters[0].Instance != "" || g.Clusters[1].Instance != "garage/garage" || g.Clusters[2].Instance != "garage-archive/garage-archive" {
		t.Errorf("cluster instances: %q %q %q", g.Clusters[0].Instance, g.Clusters[1].Instance, g.Clusters[2].Instance)
	}

	if g.Buckets[0].Instance != "garage/garage" || g.Keys[0].Instance != "garage/garage" || g.Nodes[0].Instance != "garage/garage" {
		t.Errorf("item instances: %q %q %q", g.Buckets[0].Instance, g.Keys[0].Instance, g.Nodes[0].Instance)
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
	if err := garageOpList(context.Background(), k, "/apis/garage.rajsingh.info/v1beta1/garagebuckets", &buckets, &truncated); err != nil {
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

	if g.Clusters[0].Instance == "" || g.Clusters[1].Instance == "" {
		t.Errorf("demo cluster instances: %q %q", g.Clusters[0].Instance, g.Clusters[1].Instance)
	}
}
