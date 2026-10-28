package ichorgo

import (
	"encoding/json"
	"testing"
)

var testScreens = []screenEntry{
	{ID: "etcd", Title: "etcd", Subtitle: "Members, alarms, snapshots", Keywords: []string{"members", "alarm", "snapshot"}, Section: "Cluster"},
	{ID: "upgrade-cluster", Title: "Upgrade cluster", Section: "Operate"},
	{ID: "upgrade-kubernetes", Title: "Upgrade Kubernetes", Section: "Operate"},
	{ID: "settings/alerts", Title: "Settings → Alerts", Section: "Settings"},
	{ID: "node/node-a/hardware", Title: "node-a → Hardware", Subtitle: "Disks, CPU, memory", Section: "node-a"},
	{ID: "alerts", Title: "Alerts", Keywords: []string{"alertmanager"}, Section: "Cluster"},
}

func TestSearchScreens(t *testing.T) {
	tests := []struct {
		name, query string
		limit       int
		want        []string
		total       int
	}{
		{name: "empty query matches nothing", query: "  ", want: []string{}},
		{name: "exact title first", query: "etcd", want: []string{"etcd"}, total: 1},
		{name: "prefix of a title word", query: "upg", want: []string{"upgrade-cluster", "upgrade-kubernetes"}, total: 2},
		{name: "later word of the title", query: "kube", want: []string{"upgrade-kubernetes"}, total: 1},
		{name: "every word must match", query: "alert set", want: []string{"settings/alerts"}, total: 1},
		{name: "title before keyword", query: "alert", want: []string{"alerts", "settings/alerts"}, total: 2},
		{name: "keyword hit", query: "snapshot", want: []string{"etcd"}, total: 1},
		{name: "node screen", query: "hard", want: []string{"node/node-a/hardware"}, total: 1},
		{name: "limit keeps the total", query: "upg", limit: 1, want: []string{"upgrade-cluster"}, total: 2},
		{name: "no match", query: "zzz", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, err := json.Marshal(map[string]any{"screens": testScreens})
			if err != nil {
				t.Fatal(err)
			}

			raw, err := SearchScreens(string(in), tt.query, tt.limit)
			if err != nil {
				t.Fatal(err)
			}

			var out screenMatches
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				t.Fatal(err)
			}

			got := []string{}
			for _, r := range out.Results {
				got = append(got, r.ID)
			}

			if len(got) != len(tt.want) || out.Total != tt.total {
				t.Fatalf("got %v (total %d), want %v (total %d)", got, out.Total, tt.want, tt.total)
			}

			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestSearchScreensFuzzyIsBelowAPrefix(t *testing.T) {
	out := searchScreens(testScreens, "hrdw", 0)
	if len(out.Results) != 1 || out.Results[0].Score >= searchPrefix {
		t.Fatalf("fuzzy hit should score below a prefix: %+v", out.Results)
	}
}

func TestSearchScreensRejectsBadJSON(t *testing.T) {
	if _, err := SearchScreens("{", "etcd", 0); err == nil {
		t.Fatal("want an error for invalid JSON")
	}
}
