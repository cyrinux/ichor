package ichorgo

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeGKE answers the service account token grant and the cluster list of project
// "shop-dev-123456" with clusters; status, when set, answers the list instead.
type fakeGKE struct {
	mu       sync.Mutex
	lists    []string
	clusters string
	status   int
}

func (f *fakeGKE) listCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.lists...)
}

// newFakeGKE serves Google for a GKE cluster at api signed in with a service account key, and
// returns the stored kubeconfig.
func newFakeGKE(t *testing.T, api *fakeKubeAPI, f *fakeGKE) string {
	t.Helper()

	srv := withCloudServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"access_token":"ya29.pools","expires_in":3599}`)

			return
		}

		f.mu.Lock()
		f.lists = append(f.lists, r.URL.Path)
		f.mu.Unlock()

		if r.Header.Get("Authorization") != "Bearer ya29.pools" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		if r.URL.Path != "/v1/projects/shop-dev-123456/locations/-/clusters" {
			http.NotFound(w, r)

			return
		}

		if f.status != 0 {
			w.WriteHeader(f.status)
			_, _ = io.WriteString(w, `{"error":{"code":403,"message":"Required \"container.clusters.list\" permission(s)","status":"PERMISSION_DENIED"}}`)

			return
		}

		_, _ = io.WriteString(w, f.clusters)
	})

	old := gkeEndpoint
	gkeEndpoint = srv.URL

	t.Cleanup(func() { gkeEndpoint = old })

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	der, _ := x509.MarshalPKCS8PrivateKey(key)
	sa, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "viewer@shop-dev-123456.iam.gserviceaccount.com", "private_key_id": "k1",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": srv.URL + "/token",
	})

	stored, err := MergeKubeconfig("", "", execKubeconfig(api, "gke-gcloud-auth-plugin"), "")
	if err != nil {
		t.Fatal(err)
	}

	secrets, _ := json.Marshal(map[string]string{gcpFieldServiceAccount: string(sa)})
	if err := KubeSetCredentials(stored, "admin@test", string(secrets)); err != nil {
		t.Fatal(err)
	}

	return stored
}

// gkeNodesJSON is a node list: each node "pool/zone".
func gkeNodesJSON(providerProject string, nodes ...string) string {
	items := []string{}

	for i, n := range nodes {
		pool, zone, _ := strings.Cut(n, "/")
		id := ""

		if providerProject != "" {
			id = gkeProviderIDPrefix + providerProject + "/" + zone + "/gke-node-" + string(rune('a'+i))
		}

		items = append(items, `{"metadata":{"name":"gke-node-`+string(rune('a'+i))+`","labels":{"`+gkePoolLabel+`":"`+pool+`"}},"spec":{"providerID":"`+id+`"}}`)
	}

	return `{"items":[` + strings.Join(items, ",") + `]}`
}

// gkeClustersJSON is the project's list: another cluster first, then "web" at endpoint.
const gkeClustersJSON = `{"clusters":[
{"name":"other","location":"us-central1","endpoint":"198.51.100.20","currentMasterVersion":"1.32.1-gke.100",
 "nodePools":[{"name":"secret-pool","status":"RUNNING","version":"1.32.1-gke.100","config":{"machineType":"e2-small"}}]},
{"name":"web","location":"europe-west1","endpoint":"127.0.0.1","currentMasterVersion":"1.34.1-gke.1200000",
 "nodePools":[
  {"name":"zonal","status":"RUNNING","version":"1.34.1-gke.1200000","locations":["europe-west1-b"],
   "config":{"machineType":"e2-standard-4","diskSizeGb":100,"imageType":"COS_CONTAINERD"},
   "autoscaling":{"enabled":true,"minNodeCount":1,"maxNodeCount":2}},
  {"name":"regional","status":"RECONCILING","version":"1.33.5-gke.1000","locations":["europe-west1-b","europe-west1-c","europe-west1-d"],
   "config":{"machineType":"n2-standard-8","diskSizeGb":200,"imageType":"UBUNTU_CONTAINERD","spot":true},
   "autoscaling":{"enabled":true,"minNodeCount":0,"maxNodeCount":2}},
  {"name":"fixed","status":"ERROR","statusMessage":"Insufficient quota to satisfy the request","version":"1.34.1-gke.1200000",
   "config":{"machineType":"c3-standard-4","preemptible":true},
   "conditions":[{"code":"GCE_QUOTA_EXCEEDED","message":"Quota CPUS exceeded in region europe-west1"}]},
  {"name":"totals","status":"RUNNING","version":"1.34.1-gke.1200000","locations":["europe-west1-b","europe-west1-c"],
   "config":{"machineType":"e2-medium"},
   "autoscaling":{"enabled":true,"minNodeCount":1,"maxNodeCount":9,"totalMinNodeCount":1,"totalMaxNodeCount":3}}
 ]}
]}`

func TestKubeNodeGroupsGKE(t *testing.T) {
	withAuthStore(t)

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": gkeNodesJSON("shop-dev-123456",
		"zonal/europe-west1-b", "zonal/europe-west1-b", "regional/europe-west1-c", "fixed/europe-west1-d",
		"totals/europe-west1-b", "totals/europe-west1-c", "totals/europe-west1-c")})
	fake := &fakeGKE{clusters: gkeClustersJSON}
	stored := newFakeGKE(t, api, fake)

	r := nodeGroupsOf(t, stored)

	if r.Provider != "gke" || !r.Installed || r.Region != "europe-west1" || r.Cluster != "web" || r.Error != "" || r.Forbidden != "" ||
		r.ClusterVersion != "1.34.1-gke.1200000" {
		t.Fatalf("report %+v", r)
	}

	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "secret-pool") || strings.Contains(string(raw), "other") {
		t.Fatalf("another cluster of the project leaked: %s", raw)
	}

	names := []string{}
	for _, g := range r.Groups {
		names = append(names, g.Name)
	}

	if strings.Join(names, ",") != "fixed,regional,totals,zonal" {
		t.Fatalf("pools %v (sorted)", names)
	}

	fixed, regional, totals, zonal := r.Groups[0], r.Groups[1], r.Groups[2], r.Groups[3]

	if zonal.Min != 1 || zonal.Max != 2 || zonal.Nodes != 2 || zonal.Desired != 2 || !zonal.AtMax || zonal.Updating ||
		zonal.CapacityType != "ON_DEMAND" || zonal.AMIType != "COS_CONTAINERD" || zonal.DiskSize != 100 ||
		strings.Join(zonal.InstanceTypes, ",") != "e2-standard-4" || zonal.Behind || zonal.ReleaseVersion != "" {
		t.Fatalf("zonal %+v", zonal)
	}

	if regional.Min != 0 || regional.Max != 6 || regional.Nodes != 1 || regional.AtMax || !regional.Updating ||
		regional.CapacityType != "SPOT" || !regional.Behind || len(regional.Issues) != 0 || regional.Issues == nil {
		t.Fatalf("regional %+v (per-zone bounds times three zones)", regional)
	}

	if fixed.Min != 0 || fixed.Max != 0 || fixed.AtMax || fixed.Status != "ERROR" || fixed.CapacityType != "SPOT" ||
		len(fixed.Issues) != 2 || fixed.Issues[0].Message != "Insufficient quota to satisfy the request" ||
		fixed.Issues[1].Code != "GCE_QUOTA_EXCEEDED" || fixed.Issues[1].ResourceIDs == nil {
		t.Fatalf("fixed %+v (autoscaling off, preemptible, in error)", fixed)
	}

	if totals.Min != 1 || totals.Max != 3 || totals.Nodes != 3 || !totals.AtMax {
		t.Fatalf("totals %+v (total bounds win over per-zone ones)", totals)
	}

	if calls := fake.listCalls(); len(calls) != 1 {
		t.Fatalf("Google calls %v", calls)
	}
}

func TestKubeNodeGroupsGKERefusals(t *testing.T) {
	for _, tc := range []struct {
		name, nodes, clusters string
		status                int
		wantError, forbidden  string
		wantCalls             int
	}{
		{
			name: "no providerID", nodes: gkeNodesJSON("", "zonal/europe-west1-b"), clusters: gkeClustersJSON,
			wantError: "no node says which Google Cloud project this cluster is in",
		},
		{
			name: "forbidden", nodes: gkeNodesJSON("shop-dev-123456", "zonal/europe-west1-b"), status: http.StatusForbidden,
			wantError: "container.clusters.list is not allowed for this account", forbidden: "container.clusters.list", wantCalls: 1,
		},
		{
			name: "not in the project", nodes: gkeNodesJSON("shop-dev-123456", "zonal/europe-west1-b"),
			clusters:  strings.ReplaceAll(gkeClustersJSON, `"127.0.0.1"`, `"198.51.100.21"`),
			wantError: "this cluster was not found in project shop-dev-123456", wantCalls: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withAuthStore(t)

			api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": tc.nodes})
			fake := &fakeGKE{clusters: tc.clusters, status: tc.status}

			r := nodeGroupsOf(t, newFakeGKE(t, api, fake))
			if r.Provider != "gke" || !r.Installed || r.Error != tc.wantError || r.Forbidden != tc.forbidden ||
				r.Groups == nil || len(r.Groups) != 0 || r.Cluster != "" || r.Region != "" {
				t.Fatalf("report %+v", r)
			}

			if calls := fake.listCalls(); len(calls) != tc.wantCalls {
				t.Fatalf("Google calls %v", calls)
			}
		})
	}
}

func TestKubeNodeGroupsGKESignInNeeded(t *testing.T) {
	withAuthStore(t)

	api := newFakeKubeAPI(t, map[string]string{"GET /api/v1/nodes": gkeNodesJSON("shop-dev-123456", "zonal/europe-west1-b")})
	base, _ := url.Parse(api.URL)

	k := &kubeClient{base: base, http: api.Client(), tokens: kubeAuth.source("gke-test", gkeMethod{})}

	r, err := readNodeGroups(t.Context(), k)
	if err != nil || r.Error != "sign in again to read the node groups" || !r.Installed || r.Provider != "gke" {
		t.Fatalf("report %+v, %v", r, err)
	}
}

func TestGKEClusterServes(t *testing.T) {
	for _, tc := range []struct {
		name, server string
		cluster      gkeCluster
		want         bool
	}{
		{name: "endpoint", server: "https://203.0.113.7", cluster: gkeCluster{Endpoint: "203.0.113.7"}, want: true},
		{name: "endpoint with port", server: "https://203.0.113.7:443", cluster: gkeCluster{Endpoint: "203.0.113.7"}, want: true},
		{name: "private endpoint", server: "https://10.0.0.2", cluster: privateGKE("10.0.0.2", ""), want: true},
		{name: "public endpoint", server: "https://203.0.113.8", cluster: privateGKE("10.0.0.2", "203.0.113.8"), want: true},
		{name: "dns endpoint", server: "https://gke-0a1b2c3d.europe-west1.gke.goog", cluster: dnsGKE("GKE-0a1b2c3d.europe-west1.gke.goog"), want: true},
		{name: "other address", server: "https://203.0.113.9", cluster: gkeCluster{Endpoint: "203.0.113.7"}},
		{name: "proxy", server: "https://k8s-proxy.example.com", cluster: dnsGKE("gke-0a1b2c3d.europe-west1.gke.goog")},
		{name: "empty fields", server: "https://203.0.113.7", cluster: gkeCluster{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, _ := url.Parse(tc.server)
			if got := gkeClusterServes(tc.cluster, base); got != tc.want {
				t.Fatalf("serves %v, want %v", got, tc.want)
			}
		})
	}

	if gkeClusterServes(gkeCluster{Endpoint: "203.0.113.7"}, nil) {
		t.Fatal("a nil base matched")
	}
}

func privateGKE(private, public string) gkeCluster {
	var c gkeCluster
	c.PrivateClusterConfig.PrivateEndpoint, c.PrivateClusterConfig.PublicEndpoint = private, public

	return c
}

func dnsGKE(endpoint string) gkeCluster {
	var c gkeCluster
	c.ControlPlaneEndpointsConfig.DNSEndpointConfig.Endpoint = endpoint

	return c
}

func TestGKEProjectOf(t *testing.T) {
	nodes := []gkeNode{{}, {}, {}}
	nodes[0].Spec.ProviderID = "aws:///eu-west-3a/i-0abc"
	nodes[1].Spec.ProviderID = "gce://shop-dev-123456/europe-west1-b/gke-node-a"
	nodes[2].Spec.ProviderID = "gce://later-project/europe-west1-b/gke-node-b"

	if got := gkeProjectOf(nodes); got != "shop-dev-123456" {
		t.Fatalf("project %q", got)
	}

	if got := gkeProjectOf(nodes[:1]); got != "" {
		t.Fatalf("project %q from a non-GCE node", got)
	}
}
