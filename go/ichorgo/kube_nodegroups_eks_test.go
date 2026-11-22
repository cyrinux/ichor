package ichorgo

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// fakeEKSNodeGroups answers ListNodegroups and DescribeNodegroup from testdata/nodegroups for
// the cluster "prod" in eu-west-3, signed with key; forbid lists the paths it answers 403.
type fakeEKSNodeGroups struct {
	mu     sync.Mutex
	calls  []string
	forbid map[string]bool
	// posts are the bodies of the update-config calls; postStatus answers them when set
	// (else an update id); updates are the DescribeUpdate answers by update id.
	posts      []string
	postStatus int
	updates    map[string]string
}

func newFakeEKSNodeGroups(t *testing.T, key string, forbid ...string) *fakeEKSNodeGroups {
	t.Helper()

	f := &fakeEKSNodeGroups{forbid: map[string]bool{}, updates: map[string]string{}}
	for _, p := range forbid {
		f.forbid[p] = true
	}

	srv := withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		region, path, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")

		f.mu.Lock()
		f.calls = append(f.calls, path+"?"+r.URL.RawQuery)
		f.mu.Unlock()

		auth := r.Header.Get("Authorization")
		if region != "eu-west-3" || !strings.Contains(auth, "Credential="+key+"/") || !strings.Contains(auth, "/eu-west-3/eks/aws4_request") {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		if f.forbid[path] {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"message":"User is not authorized to perform: eks:Something"}`)

			return
		}

		if f.answerUpdates(w, r, path) {
			return
		}

		var file string

		switch {
		case path == "clusters/prod/node-groups" && r.URL.Query().Get("maxResults") != "100":
			w.WriteHeader(http.StatusBadRequest)

			return
		case path == "clusters/prod/node-groups" && r.URL.Query().Get("nextToken") == "":
			file = "list-page1.json"
		case path == "clusters/prod/node-groups" && r.URL.Query().Get("nextToken") == "page-2":
			file = "list-page2.json"
		case strings.HasPrefix(path, "clusters/prod/node-groups/"):
			file = "describe-" + strings.TrimPrefix(path, "clusters/prod/node-groups/") + ".json"
		default:
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write(readFixture(t, "nodegroups/"+file))
	})

	old := eksEndpoint
	eksEndpoint = func(region string) string { return srv.URL + "/" + region }

	t.Cleanup(func() { eksEndpoint = old })

	return f
}

// answerUpdates answers UpdateNodegroupConfig and DescribeUpdate; false for any other call.
func (f *fakeEKSNodeGroups) answerUpdates(w http.ResponseWriter, r *http.Request, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Method == http.MethodPost && strings.HasSuffix(path, "/update-config") {
		body, _ := io.ReadAll(r.Body)
		f.posts = append(f.posts, string(body))

		if f.postStatus != 0 {
			w.WriteHeader(f.postStatus)
			_, _ = io.WriteString(w, `{"message":"Nodegroup cannot be updated as it is currently not in Active State"}`)

			return true
		}

		_, _ = io.WriteString(w, `{"update":{"id":"0f4c2a8e-update","status":"InProgress","type":"ConfigUpdate","errors":[]}}`)

		return true
	}

	if id, ok := strings.CutPrefix(path, "clusters/prod/updates/"); ok {
		answer, found := f.updates[id]
		if !found || r.URL.Query().Get("nodegroupName") == "" {
			http.NotFound(w, r)

			return true
		}

		_, _ = io.WriteString(w, answer)

		return true
	}

	return false
}

func (f *fakeEKSNodeGroups) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.calls)
}

// storedEKSCluster is an EKS kubeconfig for api signed in with access keys (and the role
// when args names one).
func storedEKSCluster(t *testing.T, api *fakeKubeAPI, args ...string) string {
	t.Helper()

	stored, err := MergeKubeconfig("", "", eksKubeconfig(api, args...), "")
	if err != nil {
		t.Fatal(err)
	}

	if err := KubeSetCredentials(stored, "admin@test", `{"awsAccessKeyId":"AKIDBASE","awsSecretAccessKey":"secret"}`); err != nil {
		t.Fatal(err)
	}

	return stored
}

func nodeGroupsOf(t *testing.T, config string) nodeGroupsReport {
	t.Helper()

	return nodeGroupsIn(t, config, "admin@test")
}

func nodeGroupsIn(t *testing.T, config, contextName string) nodeGroupsReport {
	t.Helper()

	raw, err := KubeNodeGroups(config, contextName, "")
	if err != nil {
		t.Fatal(err)
	}

	var r nodeGroupsReport
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatal(err)
	}

	return r
}

func TestKubeNodeGroupsEKS(t *testing.T) {
	withAuthStore(t)
	newFakeAWS(t)

	fake := newFakeEKSNodeGroups(t, "ASIAROLE")
	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": string(readFixture(t, "nodegroups/nodes.json"))})
	stored := storedEKSCluster(t, api, "--role-arn", "arn:aws:iam::123456789012:role/viewer")

	r := nodeGroupsOf(t, stored)

	if r.Provider != "eks" || !r.Installed || r.Region != "eu-west-3" || r.Cluster != "prod" || r.Error != "" || r.Forbidden != "" {
		t.Fatalf("report %+v", r)
	}

	names := []string{}
	for _, g := range r.Groups {
		names = append(names, g.Name)
	}

	if strings.Join(names, ",") != "batch,spot,system" {
		t.Fatalf("groups %v (both pages, sorted)", names)
	}

	batch, spot, system := r.Groups[0], r.Groups[1], r.Groups[2]

	if !batch.AtMax || batch.Nodes != 1 || batch.Desired != 4 || batch.Max != 4 || batch.Status != "DEGRADED" ||
		len(batch.Issues) != 1 || batch.Issues[0].Code != "AsgInstanceLaunchFailures" || batch.Issues[0].ResourceIDs[0] != "eks-batch-0a1b2c3d-asg" ||
		strings.Join(batch.InstanceTypes, ",") != "c7i.2xlarge,c6i.2xlarge" || batch.Version != "1.33" || batch.DiskSize != 80 {
		t.Fatalf("batch %+v", batch)
	}

	if !spot.Updating || spot.AtMax || spot.Nodes != 0 || spot.CapacityType != "SPOT" || spot.Issues == nil || spot.Created != 1_760_000_000_000 {
		t.Fatalf("spot %+v", spot)
	}

	if system.Nodes != 2 || system.Desired != 3 || system.Min != 2 || system.Max != 6 || system.AtMax ||
		system.ReleaseVersion != "1.34.1-20251016" || system.AMIType != "AL2023_x86_64_STANDARD" || system.Created != 1_752_019_200_000 {
		t.Fatalf("system %+v", system)
	}

	// list page 1, page 2, three describes
	if n := fake.callCount(); n != 5 {
		t.Fatalf("%d AWS calls", n)
	}
}

func TestKubeNodeGroupsKeysWithoutRole(t *testing.T) {
	withAuthStore(t)
	newFakeEKSNodeGroups(t, "AKIDBASE")

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	r := nodeGroupsOf(t, storedEKSCluster(t, api))
	if r.Error != "" || len(r.Groups) != 3 {
		t.Fatalf("report %+v", r)
	}
}

func TestKubeNodeGroupsForbidden(t *testing.T) {
	withAuthStore(t)

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	t.Run("list", func(t *testing.T) {
		newFakeEKSNodeGroups(t, "AKIDBASE", "clusters/prod/node-groups")

		r := nodeGroupsOf(t, storedEKSCluster(t, api))
		if !r.Installed || r.Forbidden != "eks:ListNodegroups" || r.Error != "eks:ListNodegroups is not allowed for this role" || len(r.Groups) != 0 || r.Groups == nil {
			t.Fatalf("report %+v", r)
		}
	})

	t.Run("describe", func(t *testing.T) {
		newFakeEKSNodeGroups(t, "AKIDBASE", "clusters/prod/node-groups/batch")

		r := nodeGroupsOf(t, storedEKSCluster(t, api))
		if r.Forbidden != "eks:DescribeNodegroup" || !strings.Contains(r.Error, "eks:DescribeNodegroup") || len(r.Groups) != 2 {
			t.Fatalf("report %+v (the describable groups are kept)", r)
		}
	})
}

func TestKubeNodeGroupsAWSDown(t *testing.T) {
	withAuthStore(t)
	srv := withCloudServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"message":"Service unavailable"}`)
	})

	old := eksEndpoint
	eksEndpoint = func(region string) string { return srv.URL + "/" + region }

	t.Cleanup(func() { eksEndpoint = old })

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	r := nodeGroupsOf(t, storedEKSCluster(t, api))
	if !r.Installed || r.Forbidden != "" || !strings.Contains(r.Error, "Service unavailable") || len(r.Groups) != 0 {
		t.Fatalf("report %+v", r)
	}
}

func TestKubeNodeGroupsNotEKS(t *testing.T) {
	withAuthStore(t)

	fake := newFakeEKSNodeGroups(t, "AKIDBASE")
	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": `{"items":[]}`})

	r := nodeGroupsOf(t, api.kubeconfigFor(api.URL))
	if r.Installed || r.Provider != "" || r.Groups == nil || len(r.Groups) != 0 {
		t.Fatalf("report %+v", r)
	}

	if n := fake.callCount(); n != 0 {
		t.Fatalf("%d AWS calls for a token cluster", n)
	}
}

func TestKubeNodeGroupsSignInNeeded(t *testing.T) {
	withAuthStore(t)

	fake := newFakeEKSNodeGroups(t, "AKIDBASE")

	k := &kubeClient{tokens: kubeAuth.source("eks-test", &eksMethod{cluster: "prod", region: "eu-west-3"})}

	r, err := readNodeGroups(t.Context(), k)
	if err != nil || r.Error != "sign in again to read the node groups" || !r.Installed {
		t.Fatalf("report %+v, %v", r, err)
	}

	if fake.callCount() != 0 {
		t.Fatalf("calls %d", fake.callCount())
	}
}

func TestDemoNodeGroups(t *testing.T) {
	r := nodeGroupsIn(t, demoKubeconfigForTest(t), demoKubeContext)
	if r.Provider != "eks" || !r.Installed || len(r.Groups) != 2 {
		t.Fatalf("demo %+v", r)
	}

	gpu, system := r.Groups[0], r.Groups[1]
	if gpu.Name != "gpu" || !gpu.AtMax || len(gpu.Issues) != 1 || gpu.Issues[0].Code != "AsgInstanceLaunchFailures" {
		t.Fatalf("gpu %+v", gpu)
	}

	pooled := 0

	for _, n := range demoEKSNodes().Nodes {
		if n.Pool == "system" {
			pooled++
		}
	}

	if system.Name != "system" || system.AtMax || len(system.Issues) != 0 || system.Nodes != pooled || pooled == 0 {
		t.Fatalf("system %+v (demo nodes in the group: %d)", system, pooled)
	}

	if talos := demoNodeGroupsFor(kubeTarget{config: mustDemoConfig(t)})(); talos.Installed || len(talos.Groups) != 0 {
		t.Fatalf("talos demo %+v", talos)
	}
}
