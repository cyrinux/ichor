package ichorgo

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

type upgradeBody struct {
	Version *string `json:"version"`
	Force   *bool   `json:"force"`
	Token   string  `json:"clientRequestToken"`
}

func decodeUpgradeBody(t *testing.T, raw string) upgradeBody {
	t.Helper()

	var b upgradeBody
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatal(err)
	}

	return b
}

func TestKubeNodeGroupUpgrade(t *testing.T) {
	t.Run("control plane version", func(t *testing.T) {
		fake, stored := scaleSetup(t)

		raw, err := KubeNodeGroupUpgrade(stored, "admin@test", "", "batch", "1.34", false)
		if err != nil {
			t.Fatal(err)
		}

		if u := decodeUpdate(t, raw); u.ID != "0f4c2a8e-update" || u.Type != "VersionUpdate" || u.Errors == nil {
			t.Fatalf("update %+v", u)
		}

		if len(fake.posts) != 1 || fake.postPaths[0] != "clusters/prod/node-groups/batch/update-version" {
			t.Fatalf("posts %v", fake.postPaths)
		}

		b := decodeUpgradeBody(t, fake.posts[0])
		if b.Version == nil || *b.Version != "1.34" || b.Force == nil || *b.Force || b.Token == "" {
			t.Fatalf("body %s", fake.posts[0])
		}

		entries := readAudit(t, "", "upgrade-nodegroup")
		if len(entries) != 1 || entries[0].Object != "NodeGroup/batch" || entries[0].Params != "version=1.34,force=false" ||
			entries[0].Outcome != auditOK || strings.Contains(entries[0].Params, "AKID") {
			t.Fatalf("audit %+v", entries)
		}
	})

	t.Run("latest node image, forced", func(t *testing.T) {
		fake, stored := scaleSetup(t)

		if _, err := KubeNodeGroupUpgrade(stored, "admin@test", "", "system", "", true); err != nil {
			t.Fatal(err)
		}

		if len(fake.posts) != 1 {
			t.Fatalf("%d posts", len(fake.posts))
		}

		b := decodeUpgradeBody(t, fake.posts[0])
		if b.Version != nil || b.Force == nil || !*b.Force || b.Token == "" {
			t.Fatalf("body %s (no version: the latest image of the group's own)", fake.posts[0])
		}

		for _, c := range fake.calls {
			if strings.HasPrefix(c, "clusters/prod?") {
				t.Fatalf("the cluster was described for a node image upgrade: %v", fake.calls)
			}
		}

		if e := readAudit(t, "", "upgrade-nodegroup"); len(e) != 1 || e[0].Params != "version=latest,force=true" {
			t.Fatalf("audit %+v", e)
		}
	})
}

func TestKubeNodeGroupUpgradeRefusals(t *testing.T) {
	fake, stored := scaleSetup(t)

	if _, err := KubeNodeGroupUpgrade(stored, "admin@test", "", " ", "1.34", false); err == nil || err.Error() != "no node group named" {
		t.Fatalf("no group: %v", err)
	}

	if fake.callCount() != 0 {
		t.Fatalf("%d AWS calls without a group", fake.callCount())
	}

	_, err := KubeNodeGroupUpgrade(stored, "admin@test", "", "batch", "1.35", false)
	if err == nil || !strings.Contains(err.Error(), "control plane's version (1.34), not 1.35") {
		t.Fatalf("other version: %v", err)
	}

	_, err = KubeNodeGroupUpgrade(stored, "admin@test", "", "spot", "", false)
	if err == nil || err.Error() != "the group is updating: wait for it to finish" {
		t.Fatalf("updating group: %v", err)
	}

	if len(fake.posts) != 0 {
		t.Fatalf("%d posts", len(fake.posts))
	}

	if n := len(readAudit(t, "", "upgrade-nodegroup")); n != 3 {
		t.Fatalf("%d audit entries, one per attempt", n)
	}
}

func TestKubeNodeGroupUpgradeAWSAnswers(t *testing.T) {
	t.Run("forbidden", func(t *testing.T) {
		_, stored := scaleSetup(t, "clusters/prod/node-groups/system/update-version")

		_, err := KubeNodeGroupUpgrade(stored, "admin@test", "", "system", "", false)
		if err == nil || err.Error() != "eks:UpdateNodegroupVersion is not allowed for this role" {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("cluster forbidden", func(t *testing.T) {
		fake, stored := scaleSetup(t, "clusters/prod")

		_, err := KubeNodeGroupUpgrade(stored, "admin@test", "", "batch", "1.34", false)
		if err == nil || err.Error() != "eks:DescribeCluster is not allowed for this role" || len(fake.posts) != 0 {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("skew refused by AWS", func(t *testing.T) {
		fake, stored := scaleSetup(t)
		fake.postStatus = http.StatusBadRequest

		_, err := KubeNodeGroupUpgrade(stored, "admin@test", "", "system", "1.34", false)
		if err == nil || !strings.Contains(err.Error(), "currently not in Active State") || strings.Contains(err.Error(), "may have been applied") {
			t.Fatalf("got %v", err)
		}

		if len(fake.posts) != 1 {
			t.Fatalf("%d posts: never retried", len(fake.posts))
		}
	})
}

// A pod the update could not evict comes back as AWS words it: the apps name it.
func TestKubeNodeGroupUpgradePodEvictionFailure(t *testing.T) {
	fake, stored := scaleSetup(t)

	fake.updates["v-1"] = `{"update":{"id":"v-1","status":"Failed","type":"VersionUpdate","errors":[` +
		`{"errorCode":"PodEvictionFailure","errorMessage":"Reached max retries while trying to evict pods from nodes in node group batch","resourceIds":["ip-10-0-1-23.eu-west-3.compute.internal"]}]}}`

	u := decodeUpdate(t, mustUpdate(t, stored, "v-1"))
	if u.Status != "Failed" || len(u.Errors) != 1 || u.Errors[0].Code != "PodEvictionFailure" ||
		!strings.Contains(u.Errors[0].Message, "evict pods") || u.Errors[0].ResourceIDs[0] != "ip-10-0-1-23.eu-west-3.compute.internal" {
		t.Fatalf("update %+v", u)
	}
}

func TestKubeNodeGroupUpgradeNotEKS(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	api := newFakeKubeAPI(t, map[string]string{})

	stored, err := MergeKubeconfig("", "", api.kubeconfigFor(api.URL), "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := KubeNodeGroupUpgrade(stored, "admin@test", "", "system", "", false); err == nil || err.Error() != errNotEKSNodeGroups.Error() {
		t.Fatalf("got %v", err)
	}
}

func TestKubeNodeGroupUpgradeDemo(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	if _, err := KubeNodeGroupUpgrade(demoKubeconfigForTest(t), "", "", "gpu", "1.34", false); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Fatalf("demo: %v", err)
	}
}

// A role that may not describe the cluster still reads its groups, none marked behind.
func TestKubeNodeGroupsClusterForbidden(t *testing.T) {
	withAuthStore(t)
	newFakeEKSNodeGroups(t, "AKIDBASE", "clusters/prod")

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	r := nodeGroupsOf(t, storedEKSCluster(t, api))
	if r.Error != "" || r.ClusterVersion != "" || len(r.Groups) != 3 {
		t.Fatalf("report %+v", r)
	}

	for _, g := range r.Groups {
		if g.Behind {
			t.Fatalf("%s behind an unknown version", g.Name)
		}
	}
}

func TestKubeMinorBelow(t *testing.T) {
	for _, c := range []struct {
		version, of string
		want        bool
	}{
		{"1.33", "1.34", true},
		{"1.34", "1.34", false},
		{"1.35", "1.34", false},
		{"1.9", "1.10", true},
		{"v1.33.5", "1.34", true},
		{"", "1.34", false},
		{"1.33", "", false},
	} {
		if got := kubeMinorBelow(c.version, c.of); got != c.want {
			t.Errorf("kubeMinorBelow(%q, %q) = %v", c.version, c.of, got)
		}
	}
}
