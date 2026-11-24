package ichorgo

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// clusterAt is DescribeCluster's answer for the fake's cluster at version and status.
func clusterAt(version, status string) string {
	return `{"cluster":{"name":"prod","version":"` + version + `","platformVersion":"eks.20","status":"` + status + `"}}`
}

func decodePlan(t *testing.T, raw string) eksUpgradePlan {
	t.Helper()

	var p eksUpgradePlan
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}

	return p
}

func eksPlanOf(t *testing.T, stored string) eksUpgradePlan {
	t.Helper()

	raw, err := KubeEKSUpgradePlan(stored, "admin@test", "")
	if err != nil {
		t.Fatal(err)
	}

	return decodePlan(t, raw)
}

func TestKubeEKSUpgradePlan(t *testing.T) {
	t.Run("one minor available, a group behind", func(t *testing.T) {
		fake, stored := scaleSetup(t)

		p := eksPlanOf(t, stored)
		if !p.Installed || p.Cluster != "prod" || p.Region != "eu-west-3" || p.Version != "1.34" || p.Status != "ACTIVE" ||
			p.PlatformVersion != "eks.9" || p.Next != "1.35" || !p.NextKnown || p.SupportEnds != 1823200000000 || p.Updating != nil {
			t.Fatalf("plan %+v", p)
		}

		if !slices.Equal(p.GroupsBehind, []string{"batch"}) || !slices.Equal(p.Groups, []string{"batch", "spot", "system"}) {
			t.Fatalf("groups %v behind %v", p.Groups, p.GroupsBehind)
		}

		if !slices.Equal(p.Blockers, []string{eksBlockerGroupsBehind}) || p.Error != "" {
			t.Fatalf("blockers %v, error %q", p.Blockers, p.Error)
		}

		for _, c := range fake.calls {
			if strings.HasPrefix(c, "clusters/prod/updates") {
				t.Fatalf("an active cluster's updates were listed: %v", fake.calls)
			}
		}
	})

	t.Run("without DescribeClusterVersions", func(t *testing.T) {
		_, stored := scaleSetup(t, "cluster-versions")

		p := eksPlanOf(t, stored)
		if p.Next != "1.35" || p.NextKnown || p.SupportEnds != 0 || p.Error != "" || p.Forbidden != "" {
			t.Fatalf("plan %+v", p)
		}
	})

	t.Run("up to date", func(t *testing.T) {
		fake, stored := scaleSetup(t)
		fake.cluster = clusterAt("1.35", "ACTIVE")

		p := eksPlanOf(t, stored)
		if p.Next != "" || p.NextKnown || p.SupportEnds != 1828200000000 {
			t.Fatalf("plan %+v", p)
		}
	})

	t.Run("update in progress", func(t *testing.T) {
		fake, stored := scaleSetup(t)
		fake.cluster = clusterAt("1.33", "UPDATING")
		fake.clusterUpdateIDs = []string{"c-old", "c-run"}
		fake.clusterUpdates["c-old"] = `{"update":{"id":"c-old","status":"Successful","type":"VersionUpdate","errors":[]}}`
		fake.clusterUpdates["c-run"] = `{"update":{"id":"c-run","status":"InProgress","type":"VersionUpdate","errors":[]}}`

		p := eksPlanOf(t, stored)
		if p.Updating == nil || p.Updating.ID != "c-run" || p.Updating.Status != "InProgress" {
			t.Fatalf("updating %+v", p.Updating)
		}

		if !slices.Equal(p.Blockers, []string{eksBlockerUpdating}) {
			t.Fatalf("blockers %v", p.Blockers)
		}
	})

	t.Run("not active", func(t *testing.T) {
		fake, stored := scaleSetup(t)
		fake.cluster = clusterAt("1.33", "PENDING")

		if p := eksPlanOf(t, stored); !slices.Equal(p.Blockers, []string{"the cluster is pending: only an active cluster can be upgraded"}) {
			t.Fatalf("blockers %v", p.Blockers)
		}
	})

	t.Run("DescribeCluster forbidden", func(t *testing.T) {
		_, stored := scaleSetup(t, "clusters/prod")

		p := eksPlanOf(t, stored)
		if !p.Installed || p.Forbidden != "eks:DescribeCluster" || p.Error != "eks:DescribeCluster is not allowed for this role" || p.Next != "" {
			t.Fatalf("plan %+v", p)
		}
	})
}

func TestKubeEKSUpgradePlanNotEKS(t *testing.T) {
	withDataDir(t)
	withAuthStore(t)

	fake := newFakeEKSNodeGroups(t, "AKIDBASE")
	api := newFakeKubeAPI(t, map[string]string{})

	stored, err := MergeKubeconfig("", "", api.kubeconfigFor(api.URL), "")
	if err != nil {
		t.Fatal(err)
	}

	if p := eksPlanOf(t, stored); p.Installed || p.Blockers == nil || p.GroupsBehind == nil {
		t.Fatalf("plan %+v", p)
	}

	if fake.callCount() != 0 {
		t.Fatalf("%d AWS calls", fake.callCount())
	}

	withAuditClock(t, time.Now())

	if _, err := KubeEKSUpgrade(stored, "admin@test", "", "1.35"); err == nil || err.Error() != errNotEKSCluster.Error() {
		t.Fatalf("got %v", err)
	}
}

func TestKubeEKSUpgrade(t *testing.T) {
	fake, stored := scaleSetup(t)
	fake.cluster = clusterAt("1.33", "ACTIVE")

	raw, err := KubeEKSUpgrade(stored, "admin@test", "", "v1.34")
	if err != nil {
		t.Fatal(err)
	}

	if u := decodeUpdate(t, raw); u.ID != "0f4c2a8e-update" || u.Status != "InProgress" || u.Type != "VersionUpdate" || u.Errors == nil {
		t.Fatalf("update %+v", u)
	}

	if len(fake.posts) != 1 || fake.postPaths[0] != "clusters/prod/updates" {
		t.Fatalf("posts %v", fake.postPaths)
	}

	var body map[string]string
	if err := json.Unmarshal([]byte(fake.posts[0]), &body); err != nil {
		t.Fatal(err)
	}

	if len(body) != 2 || body["version"] != "1.34" || body["clientRequestToken"] == "" {
		t.Fatalf("body %s", fake.posts[0])
	}

	entries := readAudit(t, "", "upgrade-eks-control-plane")
	if len(entries) != 1 || entries[0].Object != "Cluster/prod" || entries[0].Params != "version=1.34" || entries[0].Outcome != auditOK {
		t.Fatalf("audit %+v", entries)
	}
}

func TestKubeEKSUpgradeRefusals(t *testing.T) {
	fake, stored := scaleSetup(t)

	refused := func(version, want string) {
		t.Helper()

		if _, err := KubeEKSUpgrade(stored, "admin@test", "", version); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: %v", version, err)
		}
	}

	refused(" ", "no version named")

	if fake.callCount() != 0 {
		t.Fatalf("%d AWS calls without a version", fake.callCount())
	}

	// 1.34 with the group batch on 1.33.
	refused("1.35", eksBlockerGroupsBehind)

	fake.cluster = clusterAt("1.33", "ACTIVE")
	refused("1.35", "one minor at a time, to 1.34, not 1.35")
	refused("1.33", "to 1.34, not 1.33")

	fake.cluster = clusterAt("1.33", "UPDATING")
	refused("1.34", eksBlockerUpdating)

	fake.cluster = clusterAt("1.35", "ACTIVE")
	refused("1.36", "no newer Kubernetes version is available for this cluster (1.35)")

	if len(fake.posts) != 0 {
		t.Fatalf("%d posts", len(fake.posts))
	}

	if n := len(readAudit(t, "", "upgrade-eks-control-plane")); n != 6 {
		t.Fatalf("%d audit entries, one per attempt", n)
	}
}

func TestKubeEKSUpgradeAWSAnswers(t *testing.T) {
	t.Run("forbidden", func(t *testing.T) {
		fake, stored := scaleSetup(t, "clusters/prod/updates")
		fake.cluster = clusterAt("1.33", "ACTIVE")

		_, err := KubeEKSUpgrade(stored, "admin@test", "", "1.34")
		if err == nil || err.Error() != "eks:UpdateClusterVersion is not allowed for this role" {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("cluster forbidden", func(t *testing.T) {
		fake, stored := scaleSetup(t, "clusters/prod")

		_, err := KubeEKSUpgrade(stored, "admin@test", "", "1.35")
		if err == nil || err.Error() != "eks:DescribeCluster is not allowed for this role" || len(fake.posts) != 0 {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("refused by AWS, never retried", func(t *testing.T) {
		fake, stored := scaleSetup(t)
		fake.cluster = clusterAt("1.33", "ACTIVE")
		fake.postStatus = http.StatusBadRequest

		_, err := KubeEKSUpgrade(stored, "admin@test", "", "1.34")
		if err == nil || !strings.Contains(err.Error(), "currently not in Active State") || len(fake.posts) != 1 {
			t.Fatalf("got %v, %d posts", err, len(fake.posts))
		}
	})

	t.Run("in use", func(t *testing.T) {
		fake, stored := scaleSetup(t)
		fake.cluster = clusterAt("1.33", "ACTIVE")
		fake.postStatus = http.StatusConflict

		_, err := KubeEKSUpgrade(stored, "admin@test", "", "1.34")
		if err == nil || err.Error() != "another update is running on this cluster" {
			t.Fatalf("got %v", err)
		}
	})
}

func TestKubeEKSUpdate(t *testing.T) {
	fake, stored := scaleSetup(t)
	fake.clusterUpdates["c-1"] = `{"update":{"id":"c-1","status":"InProgress","type":"VersionUpdate","errors":[]}}`

	read := func() nodeGroupUpdate {
		t.Helper()

		raw, err := KubeEKSUpdate(stored, "admin@test", "", "c-1")
		if err != nil {
			t.Fatal(err)
		}

		return decodeUpdate(t, raw)
	}

	if u := read(); u.Status != "InProgress" {
		t.Fatalf("in progress %+v", u)
	}

	fake.clusterUpdates["c-1"] = `{"update":{"id":"c-1","status":"Failed","type":"VersionUpdate","errors":[` +
		`{"errorCode":"InsufficientFreeAddresses","errorMessage":"subnets have too few free addresses","resourceIds":["subnet-0a1b"]}]}}`

	if u := read(); u.Status != "Failed" || len(u.Errors) != 1 || u.Errors[0].Code != "InsufficientFreeAddresses" {
		t.Fatalf("failed %+v", u)
	}

	for _, c := range fake.calls {
		if strings.Contains(c, "nodegroupName") {
			t.Fatalf("a cluster update read named a group: %v", fake.calls)
		}
	}

	if _, err := KubeEKSUpdate(stored, "admin@test", "", " "); err == nil || err.Error() != "no update named" {
		t.Fatalf("no id: %v", err)
	}
}

func TestKubeEKSUpdateForbidden(t *testing.T) {
	_, stored := scaleSetup(t, "clusters/prod/updates/c-1")

	_, err := KubeEKSUpdate(stored, "admin@test", "", "c-1")
	if err == nil || !strings.Contains(err.Error(), "eks:DescribeUpdate is not allowed for this role") {
		t.Fatalf("got %v", err)
	}
}

func TestKubeEKSUpgradeDemo(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	demo := demoKubeconfigForTest(t)

	raw, err := KubeEKSUpgradePlan(demo, "", "")
	if err != nil {
		t.Fatal(err)
	}

	p := decodePlan(t, raw)
	if !p.Installed || p.Version != "1.34" || p.Next != "1.35" || !p.NextKnown || p.SupportEnds == 0 ||
		!slices.Equal(p.GroupsBehind, []string{"gpu"}) || !slices.Equal(p.Blockers, []string{eksBlockerGroupsBehind}) {
		t.Fatalf("demo plan %+v", p)
	}

	if _, err := KubeEKSUpgrade(demo, "", "", "1.35"); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Fatalf("demo: %v", err)
	}

	raw, err = KubeEKSUpdate(demo, "", "", "demo-update")
	if err != nil {
		t.Fatal(err)
	}

	if u := decodeUpdate(t, raw); u.ID != "demo-update" || u.Status != "Successful" {
		t.Fatalf("demo update %+v", u)
	}
}

func TestNextKubeMinor(t *testing.T) {
	for in, want := range map[string]string{"1.31": "1.32", "v1.34.2": "1.35", "1.9": "1.10", "": "", "latest": ""} {
		if got := nextKubeMinor(in); got != want {
			t.Errorf("nextKubeMinor(%q) = %q, want %q", in, got, want)
		}
	}
}
