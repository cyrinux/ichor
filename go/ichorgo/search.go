package ichorgo

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Search across clusters (U3): the apps keep the last-known JSON of every cluster on the
// phone (Android OfflineCache, iOS LastKnownStore). SearchSnapshots ranks what those blobs
// name for a query and hands back, per hit, the share target the apps already open. It
// calls no cluster and keeps no state: the apps call it as the user types.

const (
	searchDefaultLimit = 50
	// The ladder of a field match; a secondary field (namespace, owner, node, image, project)
	// scores searchSecondary times the same ladder.
	searchExact     = 1.0
	searchPrefix    = 0.8
	searchBoundary  = 0.7
	searchSubstring = 0.6
	searchFuzzy     = 0.35
	searchSecondary = 0.5
)

// searchEntry is one cached blob: the cluster it came from and the key the app stored it
// under ("pods|kube-system" on Android, "services/node-a" on iOS).
type searchEntry struct {
	Cluster string `json:"cluster"`
	Context string `json:"context"`
	Key     string `json:"key"`
	At      int64  `json:"at"`
	JSON    string `json:"json"`
}

type searchResult struct {
	Cluster   string       `json:"cluster"`
	Context   string       `json:"context"`
	Kind      string       `json:"kind"`
	Namespace string       `json:"namespace,omitempty"`
	Name      string       `json:"name"`
	Detail    string       `json:"detail"`
	At        int64        `json:"at"`
	Score     float64      `json:"score"`
	Target    *shareTarget `json:"target,omitempty"`

	unhealthy bool
}

type searchResults struct {
	Results []searchResult `json:"results"`
	Total   int            `json:"total"`
}

// searchItem is one thing a blob names, before it is matched.
type searchItem struct {
	kind, namespace, name, detail string
	secondary                     []string
	unhealthy                     bool
	target                        shareTarget
}

// SearchSnapshots ranks the nodes, pods, workloads, CronJobs, Argo CD and Flux apps the
// cached blobs of entriesJSON ({"entries":[searchEntry…]}) name for query, best first, at
// most limit (50 when <= 0). Unknown keys and blobs that do not parse are skipped. The blobs
// are results the apps cached, already masked in screenshot mode, so the result is not
// masked again (like BuildShareLink's names).
func SearchSnapshots(entriesJSON, query string, limit int) (out string, err error) {
	defer maskErr(&err)

	var in struct {
		Entries []searchEntry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(entriesJSON), &in); err != nil {
		return "", fmt.Errorf("invalid search entries: %w", err)
	}

	return toJSON(searchSnapshots(in.Entries, query, limit))
}

func searchSnapshots(entries []searchEntry, query string, limit int) searchResults {
	out := searchResults{Results: []searchResult{}}

	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 {
		return out
	}

	if limit <= 0 {
		limit = searchDefaultLimit
	}

	for _, e := range entries {
		for _, item := range searchItems(e) {
			score, ok := searchScore(item, words)
			if !ok {
				continue
			}

			out.Results = append(out.Results, searchResult{
				Cluster: e.Cluster, Context: e.Context, Kind: item.kind, Namespace: item.namespace,
				Name: item.name, Detail: item.detail, At: e.At, Score: score,
				Target: searchTarget(e.Cluster, item.target), unhealthy: item.unhealthy,
			})
		}
	}

	slices.SortStableFunc(out.Results, func(a, b searchResult) int {
		return cmp.Or(
			cmp.Compare(b.Score, a.Score),
			searchBool(b.unhealthy, a.unhealthy),
			cmp.Compare(b.At, a.At),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.Cluster, b.Cluster),
		)
	})

	out.Total = len(out.Results)
	if len(out.Results) > limit {
		out.Results = out.Results[:limit]
	}

	return out
}

// searchBool orders true before false when called as searchBool(b, a).
func searchBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

// searchTarget is the item's share target for cluster, nil when a share link would refuse
// it (a name outside DNS-1123, a cluster that is not an id): the result still shows.
func searchTarget(cluster string, t shareTarget) *shareTarget {
	t.Cluster = cluster

	valid, err := t.validated()
	if err != nil {
		return nil
	}

	return &valid
}

// searchKey is the cache key without its scope: "pods|kube-system" and "pods/node-a" are pods.
func searchKey(key string) string {
	if i := strings.IndexAny(key, "|/"); i >= 0 {
		return key[:i]
	}

	return key
}

// searchItems parses e's blob into the narrow fields search reads. Inventory apps and the
// nodes of kubeconfig clusters are not listed: no share target opens their screens yet.
func searchItems(e searchEntry) []searchItem {
	switch searchKey(e.Key) {
	case "overview":
		return searchParse(e.JSON, func(v clusterOverview) []searchItem { return searchNodes(v.Nodes) })
	case "pods":
		if searchIsList(e.JSON) {
			return searchParse(e.JSON, searchPods)
		}

		return searchParse(e.JSON, func(v searchPodList) []searchItem { return searchPods(v.Pods) })
	case "workloads":
		if searchIsList(e.JSON) {
			return searchParse(e.JSON, searchWorkloads)
		}

		return searchParse(e.JSON, func(v searchWorkloadList) []searchItem { return searchWorkloads(v.Workloads) })
	case "cronjobs":
		if searchIsList(e.JSON) {
			return searchParse(e.JSON, searchCronJobs)
		}

		return searchParse(e.JSON, func(v searchCronJobList) []searchItem { return searchCronJobs(v.CronJobs) })
	case "argocd":
		return searchParse(e.JSON, func(v searchApps) []searchItem { return searchArgoApps(v.Apps) })
	case "flux":
		return searchParse(e.JSON, func(v searchApps) []searchItem { return searchFluxApps(v.Apps) })
	}

	return nil
}

// searchIsList tells a bare JSON array (Android keeps its lists so) from the Go object.
func searchIsList(blob string) bool {
	return strings.HasPrefix(strings.TrimSpace(blob), "[")
}

func searchParse[T any](blob string, items func(T) []searchItem) []searchItem {
	var v T
	if json.Unmarshal([]byte(blob), &v) != nil {
		return nil
	}

	return items(v)
}

// The fields search reads of each list: a pods blob can be large, so the rest is not decoded.
type (
	searchPod struct {
		Namespace string   `json:"namespace"`
		Name      string   `json:"name"`
		Status    string   `json:"status"`
		Healthy   bool     `json:"healthy"`
		Node      string   `json:"node"`
		Owner     string   `json:"owner"`
		Images    []string `json:"images"`
	}
	searchWorkload struct {
		Kind      string   `json:"kind"`
		Namespace string   `json:"namespace"`
		Name      string   `json:"name"`
		Desired   int32    `json:"desired"`
		Ready     int32    `json:"ready"`
		Images    []string `json:"images"`
	}
	searchCronJob struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		Title     string `json:"title"`
		Schedule  string `json:"schedule"`
		Suspended bool   `json:"suspended"`
		State     string `json:"state"`
	}
	// searchApp is an Argo CD application or a Flux Kustomization/HelmRelease.
	searchApp struct {
		Kind      string `json:"kind"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		Project   string `json:"project"`
		Level     string `json:"level"`
		Health    string `json:"health"`
	}

	searchPodList struct {
		Pods []searchPod `json:"pods"`
	}
	searchWorkloadList struct {
		Workloads []searchWorkload `json:"workloads"`
	}
	searchCronJobList struct {
		CronJobs []searchCronJob `json:"cronJobs"`
	}
	searchApps struct {
		Apps []searchApp `json:"apps"`
	}
)

func searchNodes(nodes []nodeOverview) []searchItem {
	out := make([]searchItem, 0, len(nodes))

	for _, n := range nodes {
		name := cmp.Or(n.Hostname, n.Node)
		detail := searchDetail("Node", n.Role, n.Version)

		if !n.Reachable {
			detail = searchDetail("Node", "unreachable")
		}

		out = append(out, searchItem{
			kind: "node", name: name, detail: detail, secondary: []string{n.Node}, unhealthy: !n.Reachable,
			target: shareTarget{Target: "node", Addr: n.Node, Host: n.Hostname},
		})
	}

	return out
}

func searchPods(pods []searchPod) []searchItem {
	out := make([]searchItem, 0, len(pods))

	for _, p := range pods {
		out = append(out, searchItem{
			kind: "pod", namespace: p.Namespace, name: p.Name, detail: searchDetail("Pod", p.Status, p.Node),
			secondary: append([]string{p.Namespace, p.Owner, p.Node}, searchImages(p.Images)...), unhealthy: !p.Healthy,
			target: shareTarget{Target: "pod", Namespace: p.Namespace, Name: p.Name},
		})
	}

	return out
}

func searchWorkloads(workloads []searchWorkload) []searchItem {
	out := make([]searchItem, 0, len(workloads))

	for _, w := range workloads {
		out = append(out, searchItem{
			kind: "workload", namespace: w.Namespace, name: w.Name,
			detail:    searchDetail(w.Kind, fmt.Sprintf("%d/%d ready", w.Ready, w.Desired)),
			secondary: append([]string{w.Namespace}, searchImages(w.Images)...), unhealthy: w.Ready < w.Desired,
			target: shareTarget{Target: "workload", Kind: w.Kind, Namespace: w.Namespace, Name: w.Name},
		})
	}

	return out
}

func searchCronJobs(jobs []searchCronJob) []searchItem {
	out := make([]searchItem, 0, len(jobs))

	for _, j := range jobs {
		state := j.Schedule
		if j.Suspended {
			state = "suspended"
		}

		out = append(out, searchItem{
			kind: "cronjob", namespace: j.Namespace, name: j.Name, detail: searchDetail("CronJob", state, searchCronState(j.State)),
			secondary: []string{j.Namespace, j.Title}, unhealthy: j.State == cronRunFailed,
			target: shareTarget{Target: "cronjob", Namespace: j.Namespace, Name: j.Name},
		})
	}

	return out
}

// searchCronState is the latest run's state when it says something: "last run failed".
func searchCronState(state string) string {
	if state == cronRunFailed {
		return "last run failed"
	}

	return ""
}

func searchArgoApps(apps []searchApp) []searchItem {
	out := make([]searchItem, 0, len(apps))

	for _, a := range apps {
		out = append(out, searchItem{
			kind: "argo-app", namespace: a.Namespace, name: a.Name, detail: searchDetail("Argo CD app", a.Health),
			secondary: []string{a.Namespace, a.Project}, unhealthy: searchLevelBad(a.Level),
			target: shareTarget{Target: "argo-app", Namespace: a.Namespace, Name: a.Name},
		})
	}

	return out
}

func searchFluxApps(apps []searchApp) []searchItem {
	out := make([]searchItem, 0, len(apps))

	for _, a := range apps {
		out = append(out, searchItem{
			kind: "flux-app", namespace: a.Namespace, name: a.Name, detail: searchDetail(a.Kind, a.Level),
			secondary: []string{a.Namespace}, unhealthy: searchLevelBad(a.Level),
			target: shareTarget{Target: "flux-app", Kind: a.Kind, Namespace: a.Namespace, Name: a.Name},
		})
	}

	return out
}

func searchLevelBad(level string) bool { return level == "critical" || level == "warning" }

// searchDetail joins the non-empty parts: "Deployment · 2/3 ready".
func searchDetail(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), " · ")
}

// searchImages are the images' names without registry, path, tag or digest:
// "docker.io/grafana/grafana:11.0.0" is "grafana".
func searchImages(images []string) []string {
	out := make([]string, 0, len(images))

	for _, image := range images {
		name, _, _ := strings.Cut(image, "@")
		name = name[strings.LastIndex(name, "/")+1:]
		name, _, _ = strings.Cut(name, ":")
		out = append(out, name)
	}

	return out
}

// searchScore is the mean of each word's best match, false when a word matches nothing.
func searchScore(item searchItem, words []string) (float64, bool) {
	name := strings.ToLower(item.name)
	total := 0.0

	for _, word := range words {
		best := searchMatch(name, word)

		for _, field := range item.secondary {
			best = max(best, searchSecondary*searchMatch(strings.ToLower(field), word))
		}

		if best == 0 {
			return 0, false
		}

		total += best
	}

	return total / float64(len(words)), true
}

// searchMatch scores word in value (both lower case) on the ladder: exact, prefix, after a
// "-", "." or "/", anywhere, then as an ordered subsequence ("grfn" in "grafana").
func searchMatch(value, word string) float64 {
	switch {
	case value == "":
		return 0
	case value == word:
		return searchExact
	case strings.HasPrefix(value, word):
		return searchPrefix
	}

	found := false

	for i := strings.Index(value, word); i >= 0; {
		found = true

		if strings.ContainsRune("-./", rune(value[i-1])) {
			return searchBoundary
		}

		next := strings.Index(value[i+1:], word)
		if next < 0 {
			break
		}

		i += next + 1
	}

	if found {
		return searchSubstring
	}

	if searchSubsequence(value, word) {
		return searchFuzzy
	}

	return 0
}

func searchSubsequence(value, word string) bool {
	i := 0

	for j := 0; j < len(value) && i < len(word); j++ {
		if value[j] == word[i] {
			i++
		}
	}

	return i == len(word)
}

// DemoSearchEntries is the entries JSON of the built-in Talos demo (its overview, pods,
// workloads, CronJobs, Argo CD and Flux apps), so the apps search the demo without a cache.
func DemoSearchEntries() string {
	cluster, contextName := demoSearchCluster()
	now := time.Now()
	at := now.UnixMilli()

	var entries []searchEntry

	for key, blob := range map[string]any{
		"overview":  clusterOverview{Context: contextName, Nodes: demoNodes()},
		"pods":      kubePodList{Pods: demoPods()},
		"workloads": kubeWorkloadList{Workloads: demoKubeWorkloads()},
		"cronjobs":  kubeCronJobList{CronJobs: demoCronJobs(now)},
		"argocd":    demoArgoCD(now),
		"flux":      demoFlux(now),
	} {
		raw, err := json.Marshal(blob)
		if err != nil {
			continue
		}

		entries = append(entries, searchEntry{Cluster: cluster, Context: contextName, Key: key, At: at, JSON: string(raw)})
	}

	slices.SortFunc(entries, func(a, b searchEntry) int { return cmp.Compare(a.Key, b.Key) })

	out, err := toJSON(struct {
		Entries []searchEntry `json:"entries"`
	}{entries})
	if err != nil {
		return `{"entries":[]}`
	}

	return out
}

// demoSearchCluster is the demo's cluster id and context name: the demo identity is
// deterministic, so the id matches the demo cluster the apps stored.
func demoSearchCluster() (string, string) {
	const fallback = "Demo cluster"

	yaml, err := DemoConfig()
	if err != nil {
		return "", fallback
	}

	name, ctx, err := resolveContext(yaml, "")
	if err != nil {
		return "", fallback
	}

	return clusterID(ctx), name
}
