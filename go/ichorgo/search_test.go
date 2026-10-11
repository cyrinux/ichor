package ichorgo

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	searchClusterA = "aaaaaaaaaaaaaaaa"
	searchClusterB = "bbbbbbbbbbbbbbbb"
)

type searchOut struct {
	Results []struct {
		Cluster, Context, Kind, Namespace, Name, Detail string
		At                                              int64
		Score                                           float64
		Target                                          *shareTarget
	}
	Total int
}

func testSearchEntry(t *testing.T, cluster, key string, at int64, blob any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(blob)
	if err != nil {
		t.Fatal(err)
	}

	return map[string]any{"cluster": cluster, "context": "ctx-" + cluster[:1], "key": key, "at": at, "json": string(raw)}
}

func runSearch(t *testing.T, entries []map[string]any, query string, limit int) searchOut {
	t.Helper()

	in, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		t.Fatal(err)
	}

	out, err := SearchSnapshots(string(in), query, limit)
	if err != nil {
		t.Fatal(err)
	}

	var got searchOut
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}

	return got
}

func searchFixture(t *testing.T) []map[string]any {
	t.Helper()

	return []map[string]any{
		testSearchEntry(t, searchClusterA, "overview", 100, clusterOverview{Nodes: []nodeOverview{
			{Node: "192.0.2.10", Hostname: "cp-1", Reachable: true, Role: "controlplane", Version: "v1.14.0"},
		}}),
		testSearchEntry(t, searchClusterA, "pods|monitoring", 100, kubePodList{Pods: []kubePod{
			{Namespace: "monitoring", Name: "grafana-7d9f-abcde", Status: "Running", Healthy: true, Node: "cp-1", Owner: "ReplicaSet/grafana-7d9f", Images: []string{"docker.io/grafana/grafana:11.0.0"}},
			{Namespace: "monitoring", Name: "prometheus-0", Status: "CrashLoopBackOff", Node: "cp-1", Images: []string{"quay.io/prometheus/prometheus@sha256:abc"}},
		}}),
		testSearchEntry(t, searchClusterB, "workloads", 200, kubeWorkloadList{Workloads: []kubeWorkload{
			{Kind: "Deployment", Namespace: "monitoring", Name: "grafana", Desired: 3, Ready: 2},
			{Kind: "Deployment", Namespace: "shop", Name: "api", Desired: 2, Ready: 2},
			{Kind: "StatefulSet", Namespace: "shop", Name: "api-db", Desired: 1, Ready: 1},
			{Kind: "Deployment", Namespace: "shop", Name: "web-api", Desired: 1, Ready: 1},
			{Kind: "Deployment", Namespace: "shop", Name: "rapid", Desired: 1, Ready: 1},
		}}),
		testSearchEntry(t, searchClusterB, "cronjobs", 200, kubeCronJobList{CronJobs: []kubeCronJob{
			{Namespace: "shop", Name: "nightly-backup", Schedule: "0 3 * * *", State: cronRunFailed},
		}}),
		testSearchEntry(t, searchClusterB, "argocd", 200, argoStatus{Apps: []argoApp{
			{Namespace: "argocd", Name: "shop", Project: "apps", Level: "ok", Health: "Healthy"},
		}}),
		testSearchEntry(t, searchClusterB, "flux", 200, fluxStatus{Apps: []fluxApp{
			{Kind: "HelmRelease", Namespace: "flux-system", Name: "ingress-nginx", Level: "warning"},
		}}),
	}
}

func TestSearchSnapshotsRanking(t *testing.T) {
	entries := searchFixture(t)

	for _, tc := range []struct {
		query string
		want  []string // kind/name in order, a prefix of the results
	}{
		// exact 1.0, prefix 0.8 (api-db), boundary 0.7 (web-api), substring 0.6 (rapid)
		{"api", []string{"workload/api", "workload/api-db", "workload/web-api", "workload/rapid"}},
		// unhealthy first on a tie: the Deployment (2/3 ready) before the healthy pod, both prefixes
		{"grafana", []string{"workload/grafana", "pod/grafana-7d9f-abcde"}},
		// fuzzy subsequence
		{"grfn", []string{"workload/grafana", "pod/grafana-7d9f-abcde"}},
		// secondary field: the namespace, then the image's last segment without a tag
		{"shop", []string{"argo-app/shop"}},
		{"prometheus", []string{"pod/prometheus-0"}},
		// every word must match a field
		{"monitoring graf", []string{"workload/grafana", "pod/grafana-7d9f-abcde"}},
		{"cp-1", []string{"node/cp-1"}},
		{"backup", []string{"cronjob/nightly-backup"}},
		{"nginx", []string{"flux-app/ingress-nginx"}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got := runSearch(t, entries, tc.query, 0)
			if len(got.Results) < len(tc.want) {
				t.Fatalf("got %d results, want at least %d: %+v", len(got.Results), len(tc.want), got.Results)
			}

			for i, w := range tc.want {
				if r := got.Results[i]; r.Kind+"/"+r.Name != w {
					t.Errorf("result %d = %s/%s (%.2f), want %s", i, r.Kind, r.Name, r.Score, w)
				}
			}
		})
	}
}

func TestSearchSnapshotsScoresAndTargets(t *testing.T) {
	got := runSearch(t, searchFixture(t), "api", 0)

	wantScores := map[string]float64{"api": 1.0, "api-db": 0.8, "web-api": 0.7, "rapid": 0.6}
	for _, r := range got.Results {
		if want, ok := wantScores[r.Name]; ok && r.Score != want {
			t.Errorf("%s score = %v, want %v", r.Name, r.Score, want)
		}
	}

	// Every result with a target round-trips through a share link and names its cluster.
	all := runSearch(t, searchFixture(t), "a", 0)
	if all.Total == 0 {
		t.Fatal("no results")
	}

	for _, r := range all.Results {
		if r.Target == nil {
			t.Errorf("%s/%s has no target", r.Kind, r.Name)

			continue
		}

		if r.Target.Cluster != r.Cluster {
			t.Errorf("%s target cluster = %s, want %s", r.Name, r.Target.Cluster, r.Cluster)
		}

		raw, _ := json.Marshal(r.Target)
		link, err := BuildShareLink(string(raw))
		if err != nil {
			t.Fatalf("%s/%s: %v", r.Kind, r.Name, err)
		}

		back, err := parseShareLink(link)
		if err != nil || back != *r.Target {
			t.Errorf("%s/%s round trip = %+v, %v; want %+v", r.Kind, r.Name, back, err, *r.Target)
		}
	}
}

func TestSearchSnapshotsDetailAndCluster(t *testing.T) {
	got := runSearch(t, searchFixture(t), "grafana", 0)

	first := got.Results[0]
	if first.Cluster != searchClusterB || first.Context != "ctx-b" || first.Namespace != "monitoring" || first.At != 200 {
		t.Errorf("first = %+v", first)
	}

	if first.Detail != "Deployment · 2/3 ready" {
		t.Errorf("detail = %q", first.Detail)
	}

	if got.Results[1].Cluster != searchClusterA || got.Results[1].Detail != "Pod · Running · cp-1" {
		t.Errorf("second = %+v", got.Results[1])
	}
}

func TestSearchSnapshotsKeysAndBadBlobs(t *testing.T) {
	entries := []map[string]any{
		// Android writes pods|<namespace>, iOS <key>/<scope>: both are the pods kind.
		testSearchEntry(t, searchClusterA, "pods|kube-system", 1, kubePodList{Pods: []kubePod{{Namespace: "kube-system", Name: "coredns-1", Healthy: true}}}),
		testSearchEntry(t, searchClusterA, "pods/node-a", 1, kubePodList{Pods: []kubePod{{Namespace: "kube-system", Name: "coredns-2", Healthy: true}}}),
		// Unknown keys and a blob that does not parse are skipped, not errors.
		testSearchEntry(t, searchClusterA, "services/node-a", 1, map[string]any{"services": []map[string]string{{"id": "coredns"}}}),
		{"cluster": searchClusterA, "context": "ctx", "key": "workloads", "at": 1, "json": "{not json"},
	}

	got := runSearch(t, entries, "coredns", 0)
	if got.Total != 2 || len(got.Results) != 2 {
		t.Fatalf("got %+v", got)
	}

	if _, err := SearchSnapshots("{bad", "x", 0); err == nil {
		t.Error("invalid entries accepted")
	}
}

func TestSearchSnapshotsLimitAndEmpty(t *testing.T) {
	got := runSearch(t, searchFixture(t), "a", 2)
	if len(got.Results) != 2 || got.Total <= 2 {
		t.Errorf("limit: %d results, total %d", len(got.Results), got.Total)
	}

	for _, q := range []string{"", "   "} {
		if got := runSearch(t, searchFixture(t), q, 0); len(got.Results) != 0 || got.Total != 0 {
			t.Errorf("query %q: %+v", q, got)
		}
	}

	if got := runSearch(t, searchFixture(t), "zzzz", 0); got.Total != 0 {
		t.Errorf("no match: %+v", got)
	}
}

func TestSearchSnapshotsInvalidTargetIsDropped(t *testing.T) {
	entries := []map[string]any{
		// A name the share link refuses (upper case): the result stays, without a target.
		testSearchEntry(t, searchClusterA, "pods", 1, kubePodList{Pods: []kubePod{{Namespace: "default", Name: "Odd_Name"}}}),
		// A cluster id that is not one: no target either.
		{"cluster": "not-an-id", "context": "ctx", "key": "workloads", "at": 1, "json": `{"workloads":[{"kind":"Deployment","namespace":"default","name":"odd"}]}`},
	}

	got := runSearch(t, entries, "odd", 0)
	if got.Total != 2 {
		t.Fatalf("got %+v", got)
	}

	for _, r := range got.Results {
		if r.Target != nil {
			t.Errorf("%s has a target: %+v", r.Name, r.Target)
		}
	}
}

func TestDemoSearchEntries(t *testing.T) {
	demo := DemoSearchEntries()

	for _, q := range []string{"demo", demoPods()[0].Name} {
		got := runSearch(t, nil, q, 0)
		if got.Total != 0 {
			t.Fatal("empty entries matched")
		}

		out, err := SearchSnapshots(demo, q, 0)
		if err != nil {
			t.Fatal(err)
		}

		var res searchOut
		if err := json.Unmarshal([]byte(out), &res); err != nil {
			t.Fatal(err)
		}

		if res.Total == 0 || res.Results[0].Target == nil {
			t.Fatalf("%q: %s", q, out)
		}

		if !strings.Contains(out, `"context":"Demo cluster"`) {
			t.Errorf("%q: demo context missing", q)
		}
	}
}
