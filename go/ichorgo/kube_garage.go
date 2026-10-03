package ichorgo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Garage cluster states, as GetClusterHealth names them.
const (
	garageHealthy     = "healthy"
	garageDegraded    = "degraded"
	garageUnavailable = "unavailable"
	garageUnknown     = "unknown"
)

// Where an instance's numbers come from.
const (
	garageSourceCLI    = "cli-json" // garage json-api in a pod: the full picture
	garageSourceHealth = "health"   // the admin port's /health through the API server: status only
)

// garageCommands are the only commands the app runs in a Garage container: read-only
// admin API calls through the CLI, which uses the node's own RPC secret (no admin token).
// The image is a static binary on scratch: no shell, absolute path.
var garageCommands = struct{ health, status, stats []string }{
	health: []string{"/garage", "json-api", "GetClusterHealth"},
	status: []string{"/garage", "json-api", "GetClusterStatus"},
	// Node-scoped endpoints take a {node, body} envelope; "*" asks every node.
	stats: []string{"/garage", "json-api", "GetNodeStatistics", `{"node":"*","body":null}`},
}

type garageStatus struct {
	Error     string           `json:"error"`
	Instances []garageInstance `json:"instances"`
}

type garageInstance struct {
	Namespace        string       `json:"namespace"`
	Name             string       `json:"name"` // the pods' app.kubernetes.io/name (or controller)
	Pod              string       `json:"pod"`  // where the CLI ran
	Pods             int          `json:"pods"`
	PodsReady        int          `json:"podsReady"`
	Version          string       `json:"version"`
	Status           string       `json:"status"` // healthy|degraded|unavailable|unknown
	Message          string       `json:"message"`
	ConnectedNodes   int          `json:"connectedNodes"`
	KnownNodes       int          `json:"knownNodes"`
	StorageNodes     int          `json:"storageNodes"`
	StorageNodesUp   int          `json:"storageNodesUp"`
	Partitions       int          `json:"partitions"`
	PartitionsQuorum int          `json:"partitionsQuorum"`
	PartitionsAllOk  int          `json:"partitionsAllOk"`
	ResyncQueue      int64        `json:"resyncQueue"`    // -1 when unknown
	ResyncErrors     int64        `json:"resyncErrors"`   // -1 when unknown
	TableSyncQueue   int64        `json:"tableSyncQueue"` // -1 when unknown
	LayoutVersion    int64        `json:"layoutVersion"`
	Nodes            []garageNode `json:"nodes"`
	Source           string       `json:"source"`
}

type garageNode struct {
	ID             string   `json:"id"`
	Hostname       string   `json:"hostname"`
	Zone           string   `json:"zone"`
	Tags           []string `json:"tags"`
	KubeNode       string   `json:"kubeNode"` // Kubernetes node of the pod with that hostname, "" when none
	Storage        bool     `json:"storage"`  // holds a role in the current layout
	Up             bool     `json:"up"`
	LastSeenSecs   int64    `json:"lastSeenSecs"` // -1 when up or never seen
	Draining       bool     `json:"draining"`
	DataAvail      int64    `json:"dataAvail"`
	DataTotal      int64    `json:"dataTotal"`
	ResyncQueue    int64    `json:"resyncQueue"`    // -1 when its statistics failed
	ResyncErrors   int64    `json:"resyncErrors"`   // -1 when its statistics failed
	TableSyncQueue int64    `json:"tableSyncQueue"` // -1 when its statistics failed
	StatsError     string   `json:"statsError"`
}

// garageGroup is the pods of one Garage cluster and the container Garage runs in.
type garageGroup struct {
	namespace, name, container string
	pods                       []dsPod
}

// findGarageGroups groups the pods running a Garage image by namespace and app name: a
// cluster can run as a DaemonSet or a StatefulSet, and several can share a cluster.
func findGarageGroups(pods []dsPod) []garageGroup {
	catalog := loadAppCatalog()

	var groups []garageGroup

	index := map[string]int{}

	for _, p := range pods {
		container := ""

		for _, c := range p.Spec.Containers {
			if id := catalog.identify(parseImageRef(c.Image)); id.app != nil && id.app.ID == "garage" {
				container = c.Name

				break
			}
		}

		if container == "" {
			continue
		}

		name := p.Metadata.Labels["app.kubernetes.io/name"]
		if name == "" && len(p.Metadata.OwnerReferences) > 0 {
			name = p.Metadata.OwnerReferences[0].Name
		}

		if name == "" {
			name = "garage"
		}

		key := p.Metadata.Namespace + "/" + name
		if i, ok := index[key]; ok {
			groups[i].pods = append(groups[i].pods, p)

			continue
		}

		index[key] = len(groups)
		groups = append(groups, garageGroup{namespace: p.Metadata.Namespace, name: name, container: container, pods: []dsPod{p}})
	}

	return groups
}

// readGarage reads every Garage cluster found among pods; nil when there is none. A pod
// listing that failed is only reported when Garage was hinted (expected to be there).
func readGarage(ctx context.Context, k *kubeClient, run execFunc, pods []dsPod, podsErr error, hinted bool) *garageStatus {
	if podsErr != nil {
		if !hinted {
			return nil
		}

		return &garageStatus{Error: sectionError(podsErr), Instances: []garageInstance{}}
	}

	groups := findGarageGroups(pods)
	if len(groups) == 0 {
		return nil
	}

	out := &garageStatus{Instances: make([]garageInstance, len(groups))}

	var wg sync.WaitGroup

	for i, g := range groups {
		wg.Go(func() {
			out.Instances[i] = readGarageInstance(ctx, run, g, func(ctx context.Context) (string, string) {
				return garageProxyHealth(ctx, k, g)
			})
		})
	}

	wg.Wait()

	return out
}

// healthFallback asks the admin port's /health: status and message.
type healthFallback func(ctx context.Context) (status, message string)

func readGarageInstance(ctx context.Context, run execFunc, g garageGroup, fallback healthFallback) garageInstance {
	inst := garageInstance{
		Namespace: g.namespace, Name: g.name, Pods: len(g.pods), Status: garageUnknown,
		ResyncQueue: -1, ResyncErrors: -1, TableSyncQueue: -1, Nodes: []garageNode{},
	}

	for _, p := range g.pods {
		if p.containerReady(g.container) {
			inst.PodsReady++

			if inst.Pod == "" {
				inst.Pod = p.Metadata.Name
			}
		}
	}

	if inst.Pod == "" {
		inst.Status, inst.Message = garageUnavailable, "no ready Garage pod"

		return inst
	}

	var (
		outs [3][]byte
		errs [3]error
		wg   sync.WaitGroup
	)

	for i, argv := range [][]string{garageCommands.health, garageCommands.status, garageCommands.stats} {
		wg.Go(func() {
			var stderr []byte

			outs[i], stderr, errs[i] = run(ctx, g.namespace, inst.Pod, g.container, argv)
			errs[i] = garageCommandError(errs[i], stderr)
		})
	}

	wg.Wait()

	if errs[0] != nil {
		// Without the CLI, the admin port still tells healthy from not.
		inst.Status, inst.Message = fallback(ctx)
		inst.Message = strings.TrimSpace(inst.Message + " (garage json-api: " + errs[0].Error() + ")")
		inst.Source = garageSourceHealth

		return inst
	}

	inst.Source = garageSourceCLI
	if err := applyGarageHealth(&inst, outs[0]); err != nil {
		inst.Message = err.Error()

		return inst
	}

	if errs[1] == nil {
		applyGarageStatus(&inst, outs[1], g.pods)
	}

	if errs[2] == nil {
		applyGarageStats(&inst, outs[2])
	}

	inst.Status, inst.Message = garageVerdict(inst)

	return inst
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// garageCommandError keeps the CLI's own "Error: ..." line: its stderr is mostly logs.
func garageCommandError(err error, stderr []byte) error {
	var execErr *kubeExecError
	if !errors.As(err, &execErr) {
		return err
	}

	for line := range strings.SplitSeq(ansiEscape.ReplaceAllString(string(stderr), ""), "\n") {
		if msg, ok := strings.CutPrefix(strings.TrimSpace(line), "Error: "); ok {
			return errors.New(msg)
		}
	}

	return err
}

func applyGarageHealth(inst *garageInstance, data []byte) error {
	var h struct {
		Status           string `json:"status"`
		KnownNodes       int    `json:"knownNodes"`
		ConnectedNodes   int    `json:"connectedNodes"`
		StorageNodes     int    `json:"storageNodes"`
		StorageNodesUp   *int   `json:"storageNodesUp"`
		StorageNodesOk   *int   `json:"storageNodesOk"` // before v2.3
		Partitions       int    `json:"partitions"`
		PartitionsQuorum int    `json:"partitionsQuorum"`
		PartitionsAllOk  int    `json:"partitionsAllOk"`
	}

	if err := json.Unmarshal(data, &h); err != nil || h.Status == "" {
		return fmt.Errorf("unexpected GetClusterHealth answer: %q", truncate(string(data), 200))
	}

	inst.Status = h.Status
	inst.KnownNodes, inst.ConnectedNodes, inst.StorageNodes = h.KnownNodes, h.ConnectedNodes, h.StorageNodes
	inst.Partitions, inst.PartitionsQuorum, inst.PartitionsAllOk = h.Partitions, h.PartitionsQuorum, h.PartitionsAllOk

	switch {
	case h.StorageNodesUp != nil:
		inst.StorageNodesUp = *h.StorageNodesUp
	case h.StorageNodesOk != nil:
		inst.StorageNodesUp = *h.StorageNodesOk
	}

	return nil
}

func applyGarageStatus(inst *garageInstance, data []byte, pods []dsPod) {
	var st struct {
		LayoutVersion int64 `json:"layoutVersion"`
		Nodes         []struct {
			ID              string `json:"id"`
			Hostname        string `json:"hostname"`
			GarageVersion   string `json:"garageVersion"`
			IsUp            bool   `json:"isUp"`
			LastSeenSecsAgo *int64 `json:"lastSeenSecsAgo"`
			Draining        bool   `json:"draining"`
			DataPartition   *struct {
				Available int64 `json:"available"`
				Total     int64 `json:"total"`
			} `json:"dataPartition"`
			Role *struct {
				Zone string   `json:"zone"`
				Tags []string `json:"tags"`
			} `json:"role"`
		} `json:"nodes"`
	}

	if json.Unmarshal(data, &st) != nil {
		return
	}

	nodeOfPod := map[string]string{}
	for _, p := range pods {
		nodeOfPod[p.Metadata.Name] = p.Spec.NodeName
	}

	inst.LayoutVersion = st.LayoutVersion

	for _, n := range st.Nodes {
		node := garageNode{
			ID: n.ID, Hostname: n.Hostname, KubeNode: nodeOfPod[n.Hostname], Up: n.IsUp, Draining: n.Draining,
			LastSeenSecs: -1, Tags: []string{}, ResyncQueue: -1, ResyncErrors: -1, TableSyncQueue: -1,
		}

		if n.LastSeenSecsAgo != nil && !n.IsUp {
			node.LastSeenSecs = *n.LastSeenSecsAgo
		}

		if n.DataPartition != nil {
			node.DataAvail, node.DataTotal = n.DataPartition.Available, n.DataPartition.Total
		}

		if n.Role != nil {
			node.Storage = true
			node.Zone = n.Role.Zone
			node.Tags = append(node.Tags, n.Role.Tags...)
		}

		if inst.Version == "" && n.IsUp {
			inst.Version = n.GarageVersion
		}

		inst.Nodes = append(inst.Nodes, node)
	}

	// Down nodes first, then by zone and hostname.
	slices.SortFunc(inst.Nodes, func(a, b garageNode) int {
		if a.Up != b.Up {
			if a.Up {
				return 1
			}

			return -1
		}

		return strings.Compare(a.Zone+"/"+a.Hostname, b.Zone+"/"+b.Hostname)
	})
}

func applyGarageStats(inst *garageInstance, data []byte) {
	var stats struct {
		Success map[string]struct {
			BlockManagerStats struct {
				ResyncErrors   int64 `json:"resyncErrors"`
				ResyncQueueLen int64 `json:"resyncQueueLen"`
			} `json:"blockManagerStats"`
			TableStats []struct {
				InsertQueueLen int64 `json:"insertQueueLen"`
				MerkleQueueLen int64 `json:"merkleQueueLen"`
				GcQueueLen     int64 `json:"gcQueueLen"`
			} `json:"tableStats"`
		} `json:"success"`
		Error map[string]string `json:"error"`
	}

	if json.Unmarshal(data, &stats) != nil || (len(stats.Success) == 0 && len(stats.Error) == 0) {
		return
	}

	inst.ResyncQueue, inst.ResyncErrors, inst.TableSyncQueue = 0, 0, 0

	byID := map[string]int{}
	for i, n := range inst.Nodes {
		byID[n.ID] = i
	}

	for id, s := range stats.Success {
		var tables int64
		for _, t := range s.TableStats {
			tables += t.InsertQueueLen + t.MerkleQueueLen + t.GcQueueLen
		}

		b := s.BlockManagerStats
		inst.ResyncQueue += b.ResyncQueueLen
		inst.ResyncErrors += b.ResyncErrors
		inst.TableSyncQueue += tables

		if i, ok := byID[id]; ok {
			inst.Nodes[i].ResyncQueue, inst.Nodes[i].ResyncErrors, inst.Nodes[i].TableSyncQueue = b.ResyncQueueLen, b.ResyncErrors, tables
		}
	}

	for id, msg := range stats.Error {
		if i, ok := byID[id]; ok {
			inst.Nodes[i].StatsError = msg
		}
	}
}

// garageVerdict refines the reported status with what the details show, and words the
// causes first: a node down explains the partitions and the resync errors after it.
func garageVerdict(inst garageInstance) (string, string) {
	status := inst.Status
	if status != garageHealthy && status != garageDegraded && status != garageUnavailable {
		status = garageUnknown
	}

	var problems []string

	var down []garageNode

	// Only layout members count: a known node without a role (decommissioned, not yet
	// forgotten) being away does not hurt the cluster.
	for _, n := range inst.Nodes {
		if !n.Up && n.Storage {
			down = append(down, n)
		}
	}

	switch {
	case len(down) == 1:
		problems = append(problems, "1 node down"+garageNodeDetail(down[0]))
	case len(down) > 1:
		problems = append(problems, fmt.Sprintf("%d nodes down", len(down)))
	case inst.StorageNodesUp < inst.StorageNodes:
		problems = append(problems, fmt.Sprintf("%d/%d storage nodes up", inst.StorageNodesUp, inst.StorageNodes))
	}

	if inst.Partitions > 0 && inst.PartitionsQuorum < inst.Partitions {
		problems = append(problems, fmt.Sprintf("%d/%d partitions without write quorum", inst.Partitions-inst.PartitionsQuorum, inst.Partitions))
		status = garageUnavailable
	}

	if inst.Partitions > 0 && inst.PartitionsAllOk < inst.Partitions {
		problems = append(problems, fmt.Sprintf("%d/%d partitions not fully replicated", inst.Partitions-inst.PartitionsAllOk, inst.Partitions))
	}

	if inst.ResyncErrors > 0 {
		problems = append(problems, fmt.Sprintf("%d blocks failing to resync", inst.ResyncErrors))
	}

	if len(problems) > 0 && status == garageHealthy {
		status = garageDegraded
	}

	return status, strings.Join(problems, "; ")
}

func garageNodeDetail(n garageNode) string {
	var parts []string
	if n.Zone != "" {
		parts = append(parts, "zone "+n.Zone)
	}

	switch {
	case n.KubeNode != "":
		parts = append(parts, "on "+n.KubeNode)
	case len(n.Tags) > 0:
		// Garage forgets a long-gone node's hostname; its layout tags often name the host.
		parts = append(parts, "tags "+strings.Join(n.Tags, ","))
	}

	if n.LastSeenSecs >= 0 {
		parts = append(parts, "last seen "+compactDuration(n.LastSeenSecs)+" ago")
	}

	if len(parts) == 0 {
		return ""
	}

	return " (" + strings.Join(parts, ", ") + ")"
}

func compactDuration(secs int64) string {
	switch {
	case secs >= 86400:
		return strconv.FormatInt(secs/86400, 10) + "d"
	case secs >= 3600:
		return strconv.FormatInt(secs/3600, 10) + "h"
	case secs >= 60:
		return strconv.FormatInt(secs/60, 10) + "m"
	default:
		return strconv.FormatInt(secs, 10) + "s"
	}
}

// garageProxyHealth asks /health on the admin port through the API server's service proxy.
func garageProxyHealth(ctx context.Context, k *kubeClient, g garageGroup) (string, string) {
	var services lhList[struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Spec struct {
			Selector map[string]string `json:"selector"`
			Ports    []struct {
				Name string `json:"name"`
				Port int    `json:"port"`
			} `json:"ports"`
		} `json:"spec"`
	}]

	if err := k.get(ctx, "/api/v1/namespaces/"+url.PathEscape(g.namespace)+"/services", &services); err != nil {
		return garageUnknown, kubeError(err).Error()
	}

	labels := g.pods[0].Metadata.Labels

	for _, s := range services.Items {
		if len(s.Spec.Selector) == 0 || !selectorMatches(s.Spec.Selector, labels) {
			continue
		}

		for _, p := range s.Spec.Ports {
			if p.Name != "admin" && p.Port != 3903 {
				continue
			}

			path := fmt.Sprintf("/api/v1/namespaces/%s/services/%s:%d/proxy/health", url.PathEscape(g.namespace), url.PathEscape(s.Metadata.Name), p.Port)

			status, ctype, body, err := k.getRaw(ctx, path)
			if err != nil {
				return garageUnknown, kubeError(err).Error()
			}

			return garageHealthFromProxy(status, ctype, body)
		}
	}

	return garageUnknown, "no Service exposes the Garage admin port"
}

// garageHealthFromProxy reads /health: 200 when the node can serve requests, 503 without
// quorum. A JSON Status is the API server's own answer (no ready pod behind the Service).
func garageHealthFromProxy(status int, ctype string, body []byte) (string, string) {
	text := truncate(strings.TrimSpace(string(body)), 300)

	if strings.HasPrefix(ctype, "application/json") && bytes.Contains(body, []byte(`"kind":"Status"`)) {
		return garageUnavailable, "no ready Garage pod behind the admin Service"
	}

	lower := strings.ToLower(text)

	switch {
	case status == http.StatusOK && (strings.Contains(lower, "degraded") || strings.Contains(lower, "unavailable")):
		return garageDegraded, text
	case status == http.StatusOK:
		return garageHealthy, text
	case status == http.StatusServiceUnavailable:
		return garageUnavailable, text
	default:
		return garageUnknown, fmt.Sprintf("HTTP %d: %s", status, text)
	}
}

func selectorMatches(selector, labels map[string]string) bool {
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}

	return true
}
