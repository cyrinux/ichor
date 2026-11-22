package ichorgo

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// scaleSetup is an EKS cluster signed in with access keys, its node groups behind the fake.
func scaleSetup(t *testing.T, forbid ...string) (*fakeEKSNodeGroups, string) {
	t.Helper()

	withDataDir(t)
	withAuthStore(t)
	withAuditClock(t, time.Now())

	fake := newFakeEKSNodeGroups(t, "AKIDBASE", forbid...)
	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	return fake, storedEKSCluster(t, api)
}

func decodeUpdate(t *testing.T, raw string) nodeGroupUpdate {
	t.Helper()

	var u nodeGroupUpdate
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatal(err)
	}

	return u
}

func TestKubeNodeGroupScale(t *testing.T) {
	fake, stored := scaleSetup(t)

	raw, err := KubeNodeGroupScale(stored, "admin@test", "", "system", 5, 2, 8)
	if err != nil {
		t.Fatal(err)
	}

	if u := decodeUpdate(t, raw); u.ID != "0f4c2a8e-update" || u.Status != "InProgress" || u.Errors == nil {
		t.Fatalf("update %+v", u)
	}

	if len(fake.posts) != 1 {
		t.Fatalf("%d posts", len(fake.posts))
	}

	var body struct {
		ScalingConfig map[string]int `json:"scalingConfig"`
		Token         string         `json:"clientRequestToken"`
	}

	if err := json.Unmarshal([]byte(fake.posts[0]), &body); err != nil {
		t.Fatal(err)
	}

	if c := body.ScalingConfig; c["desiredSize"] != 5 || c["minSize"] != 2 || c["maxSize"] != 8 || body.Token == "" {
		t.Fatalf("body %s", fake.posts[0])
	}

	entries := readAudit(t, "", "scale-nodegroup")
	if len(entries) != 1 || entries[0].Object != "NodeGroup/system" || entries[0].Params != "desired=5,min=2,max=8" ||
		entries[0].Outcome != auditOK || strings.Contains(entries[0].Params, "AKID") {
		t.Fatalf("audit %+v", entries)
	}
}

// A degraded group (at its maximum, instances that do not launch) may change its size.
func TestKubeNodeGroupScaleDegraded(t *testing.T) {
	fake, stored := scaleSetup(t)

	if _, err := KubeNodeGroupScale(stored, "admin@test", "", "batch", 2, 0, 4); err != nil || len(fake.posts) != 1 {
		t.Fatalf("got %v, %d posts", err, len(fake.posts))
	}
}

func TestKubeNodeGroupScaleRefusals(t *testing.T) {
	fake, stored := scaleSetup(t)

	for _, c := range []struct {
		desired, minSize, maxSize int
		want                      string
	}{
		{1, -1, 3, "minimum size must be 0 or more"},
		{0, 0, 0, "maximum size must be at least 1"},
		{3, 4, 3, "must not be above the maximum"},
		{1, 2, 4, "desired size (1) must be between"},
		{5, 2, 4, "desired size (5) must be between"},
	} {
		_, err := KubeNodeGroupScale(stored, "admin@test", "", "system", c.desired, c.minSize, c.maxSize)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v", c, err)
		}
	}

	if fake.callCount() != 0 {
		t.Fatalf("%d AWS calls for impossible sizes", fake.callCount())
	}

	_, err := KubeNodeGroupScale(stored, "admin@test", "", "spot", 1, 0, 3)
	if err == nil || err.Error() != "the group is updating: wait for it to finish" || len(fake.posts) != 0 {
		t.Fatalf("updating group: %v, %d posts", err, len(fake.posts))
	}

	if n := len(readAudit(t, "", "scale-nodegroup")); n != 6 {
		t.Fatalf("%d audit entries, one per attempt", n)
	}
}

func TestKubeNodeGroupScaleAWSAnswers(t *testing.T) {
	t.Run("forbidden", func(t *testing.T) {
		_, stored := scaleSetup(t, "clusters/prod/node-groups/system/update-config")

		_, err := KubeNodeGroupScale(stored, "admin@test", "", "system", 3, 2, 6)
		if err == nil || err.Error() != "eks:UpdateNodegroupConfig is not allowed for this role" {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("describe forbidden", func(t *testing.T) {
		fake, stored := scaleSetup(t, "clusters/prod/node-groups/system")

		_, err := KubeNodeGroupScale(stored, "admin@test", "", "system", 3, 2, 6)
		if err == nil || err.Error() != "eks:DescribeNodegroup is not allowed for this role" || len(fake.posts) != 0 {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("in use", func(t *testing.T) {
		fake, stored := scaleSetup(t)
		fake.postStatus = http.StatusConflict

		_, err := KubeNodeGroupScale(stored, "admin@test", "", "system", 3, 2, 6)
		if err == nil || err.Error() != "another update is running on this group" {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		fake, stored := scaleSetup(t)
		fake.postStatus = http.StatusBadRequest

		_, err := KubeNodeGroupScale(stored, "admin@test", "", "system", 3, 2, 6)
		if err == nil || !strings.Contains(err.Error(), "currently not in Active State") || strings.Contains(err.Error(), "may have been applied") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestKubeNodeGroupScaleNotEKS(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	api := newFakeKubeAPI(t, map[string]string{})

	stored, err := MergeKubeconfig("", "", api.kubeconfigFor(api.URL), "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := KubeNodeGroupScale(stored, "admin@test", "", "system", 3, 2, 6); err == nil || err.Error() != errNotEKSNodeGroups.Error() {
		t.Fatalf("got %v", err)
	}
}

func TestKubeNodeGroupScaleDemo(t *testing.T) {
	withDataDir(t)
	withAuditClock(t, time.Now())

	demo := demoKubeconfigForTest(t)

	if _, err := KubeNodeGroupScale(demo, "", "", "system", 3, 2, 6); err == nil || err.Error() != errDemoUnavailable.Error() {
		t.Fatalf("demo: %v", err)
	}

	raw, err := KubeNodeGroupUpdate(demo, "", "", "system", "demo-update")
	if err != nil {
		t.Fatal(err)
	}

	if u := decodeUpdate(t, raw); u.ID != "demo-update" || u.Status != "Successful" {
		t.Fatalf("demo update %+v", u)
	}
}

func TestKubeNodeGroupUpdate(t *testing.T) {
	fake, stored := scaleSetup(t)

	fake.updates["u-1"] = `{"update":{"id":"u-1","status":"InProgress","type":"ConfigUpdate","errors":[]}}`

	raw, err := KubeNodeGroupUpdate(stored, "admin@test", "", "system", "u-1")
	if err != nil {
		t.Fatal(err)
	}

	if u := decodeUpdate(t, raw); u.Status != "InProgress" {
		t.Fatalf("in progress %+v", u)
	}

	fake.updates["u-1"] = `{"update":{"id":"u-1","status":"Successful","type":"ConfigUpdate","errors":[]}}`

	if u := decodeUpdate(t, mustUpdate(t, stored, "u-1")); u.Status != "Successful" {
		t.Fatalf("successful %+v", u)
	}

	fake.updates["u-2"] = `{"update":{"id":"u-2","status":"Failed","type":"ConfigUpdate","errors":[` +
		`{"errorCode":"AsgInstanceLaunchFailures","errorMessage":"Could not launch On-Demand Instances","resourceIds":["eks-system-asg"]}]}}`

	u := decodeUpdate(t, mustUpdate(t, stored, "u-2"))
	if u.Status != "Failed" || len(u.Errors) != 1 || u.Errors[0].Code != "AsgInstanceLaunchFailures" || u.Errors[0].ResourceIDs[0] != "eks-system-asg" {
		t.Fatalf("failed %+v", u)
	}
}

func TestKubeNodeGroupUpdateForbidden(t *testing.T) {
	_, stored := scaleSetup(t, "clusters/prod/updates/u-1")

	_, err := KubeNodeGroupUpdate(stored, "admin@test", "", "system", "u-1")
	if err == nil || !strings.Contains(err.Error(), "eks:DescribeUpdate is not allowed for this role") {
		t.Fatalf("got %v", err)
	}
}

func mustUpdate(t *testing.T, stored, id string) string {
	t.Helper()

	raw, err := KubeNodeGroupUpdate(stored, "admin@test", "", "system", id)
	if err != nil {
		t.Fatal(err)
	}

	return raw
}
