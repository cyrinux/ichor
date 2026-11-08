package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// The Garage cluster layout from the app: which node holds which role (zone, capacity or
// gateway, tags), what is staged for the next version, what applying it would move, the
// version history, and the staging, apply and revert of changes. All through
// `garage json-api` in a Garage pod (the node's own RPC secret), the way Garage's own
// `garage layout` subcommands work: a change is staged, previewed, then applied by naming
// the version it creates, which Garage refuses when another change got in first.

// Staged change kinds, per node.
const (
	garageStagedNone   = "none"
	garageStagedAdd    = "add"    // a node without a role gets one
	garageStagedChange = "change" // a node's role changes
	garageStagedRemove = "remove" // a node leaves the layout
)

// Layout version statuses, as GetClusterLayoutHistory names them.
const (
	garageLayoutCurrent    = "current"
	garageLayoutDraining   = "draining"
	garageLayoutHistorical = "historical"
)

// garageGatewayCapacity marks a gateway role (no capacity) in the app's own JSON.
const garageGatewayCapacity = -1

// garageMinCapacity is the least Garage accepts for a storage role.
const garageMinCapacity = 1024

// garageLayoutReads are the cluster-wide endpoints garageAPI may run again: they change nothing.
var garageLayoutReads = map[string]bool{"GetClusterLayout": true, "GetClusterLayoutHistory": true, "GetClusterStatus": true, "PreviewClusterLayoutChanges": true}

// garageZonePattern bounds a zone or a tag: Garage takes any string, the app keeps them to
// one printable line.
var garageZonePattern = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,64}$`)

// garageLayoutReport is what the app shows: the layout, its staging and its history, merged
// with the cluster status so every node is one entry.
type garageLayoutReport struct {
	Version              int64                 `json:"version"`
	NextVersion          int64                 `json:"nextVersion"` // version + 1 while something is staged, else 0
	PartitionSize        int64                 `json:"partitionSize"`
	ZoneRedundancy       string                `json:"zoneRedundancy"`       // "maximum" or a number
	StagedZoneRedundancy string                `json:"stagedZoneRedundancy"` // "" when not staged
	Preview              *garageLayoutPreview  `json:"preview,omitempty"`    // only while something is staged
	Nodes                []garageLayoutNode    `json:"nodes"`
	Versions             []garageLayoutVersion `json:"versions"`
	MinAck               int64                 `json:"minAck"`
	Trackers             []garageLayoutTracker `json:"trackers"` // only while several versions are live
	Stuck                bool                  `json:"stuck"`    // a draining version waits on a node that is down
	Messages             []string              `json:"messages"` // Garage's own lines after an apply
}

// garageLayoutPreview is PreviewClusterLayoutChanges: what applying the staged changes would do.
type garageLayoutPreview struct {
	OK                bool     `json:"ok"`
	Error             string   `json:"error"`
	Messages          []string `json:"messages"`
	MovedPartitions   int64    `json:"movedPartitions"` // -1 when Garage did not say
	UsableCapacity    int64    `json:"usableCapacity"`
	EffectiveCapacity int64    `json:"effectiveCapacity"`
	TotalCapacity     int64    `json:"totalCapacity"`
	LowUsableCapacity bool     `json:"lowUsableCapacity"`
	LowPartitionSize  bool     `json:"lowPartitionSize"`
}

type garageLayoutRole struct {
	Zone             string   `json:"zone"`
	Capacity         int64    `json:"capacity"` // bytes, -1 for a gateway
	Tags             []string `json:"tags"`
	StoredPartitions int64    `json:"storedPartitions"`
	UsableCapacity   int64    `json:"usableCapacity"`
}

type garageLayoutNode struct {
	ID         string            `json:"id"`
	Hostname   string            `json:"hostname"`
	KubeNode   string            `json:"kubeNode"`
	Up         bool              `json:"up"`
	Known      bool              `json:"known"` // the cluster still knows the node (GetClusterStatus lists it)
	Draining   bool              `json:"draining"`
	DataTotal  int64             `json:"dataTotal"`
	Role       *garageLayoutRole `json:"role"`
	Staged     string            `json:"staged"`
	StagedRole *garageLayoutRole `json:"stagedRole"`
}

type garageLayoutVersion struct {
	Version      int64  `json:"version"`
	Status       string `json:"status"`
	StorageNodes int64  `json:"storageNodes"`
	GatewayNodes int64  `json:"gatewayNodes"`
}

type garageLayoutTracker struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Ack      int64  `json:"ack"`
	Sync     int64  `json:"sync"`
	SyncAck  int64  `json:"syncAck"`
}

// Garage's own answers.

type garageAPIRole struct {
	Zone     string   `json:"zone"`
	Tags     []string `json:"tags"`
	Capacity *int64   `json:"capacity"`
}

type garageAPILayout struct {
	Version       int64 `json:"version"`
	PartitionSize int64 `json:"partitionSize"`
	Roles         []struct {
		ID string `json:"id"`
		garageAPIRole
		StoredPartitions *int64 `json:"storedPartitions"`
		UsableCapacity   *int64 `json:"usableCapacity"`
	} `json:"roles"`
	Parameters        *garageAPIParameters `json:"parameters"`
	StagedParameters  *garageAPIParameters `json:"stagedParameters"`
	StagedRoleChanges []struct {
		ID     string `json:"id"`
		Remove bool   `json:"remove"`
		garageAPIRole
	} `json:"stagedRoleChanges"`
}

type garageAPIParameters struct {
	ZoneRedundancy json.RawMessage `json:"zoneRedundancy"`
}

type garageAPIHistory struct {
	CurrentVersion int64 `json:"currentVersion"`
	MinAck         int64 `json:"minAck"`
	Versions       []struct {
		Version      int64  `json:"version"`
		Status       string `json:"status"`
		StorageNodes int64  `json:"storageNodes"`
		GatewayNodes int64  `json:"gatewayNodes"`
	} `json:"versions"`
	UpdateTrackers map[string]struct {
		Ack     int64 `json:"ack"`
		Sync    int64 `json:"sync"`
		SyncAck int64 `json:"syncAck"`
	} `json:"updateTrackers"`
}

type garageAPIPreview struct {
	Error      string   `json:"error"`
	Message    []string `json:"message"`
	Statistics *struct {
		TotalMovedPartitions *int64 `json:"totalMovedPartitions"`
		UsableCapacity       int64  `json:"usableCapacity"`
		EffectiveCapacity    int64  `json:"effectiveCapacity"`
		TotalCapacity        int64  `json:"totalCapacity"`
		LowUsableCapacity    bool   `json:"lowUsableCapacity"`
		LowPartitionSize     bool   `json:"lowPartitionSize"`
	} `json:"statistics"`
}

// garageAPI runs a cluster-wide admin endpoint through the CLI: the body, when there is one,
// is the request itself (no node envelope, unlike garageCall). A read cut short is asked again.
func garageAPI[T any](ctx context.Context, run execFunc, t garageTarget, endpoint string, body any) (T, error) {
	var out T

	argv := []string{"/garage", "json-api", endpoint}

	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return out, err
		}

		argv = append(argv, string(payload))
	}

	for attempt := 0; ; attempt++ {
		stdout, stderr, err := run(ctx, t.namespace, t.pod, t.container, argv)
		if err != nil {
			return out, fmt.Errorf("%s: %w", endpoint, garageCommandError(err, stderr))
		}

		err = json.Unmarshal(stdout, &out)
		if err == nil {
			return out, nil
		}

		var syntaxErr *json.SyntaxError
		if garageLayoutReads[endpoint] && attempt < garageReadRetries && errors.As(err, &syntaxErr) && ctx.Err() == nil {
			var zero T

			out = zero

			continue
		}

		return out, fmt.Errorf("unexpected %s answer (%v, %d bytes): %q", endpoint, err, len(stdout), clipUTF8(string(stdout), 200))
	}
}

// KubeGarageLayout reads a Garage cluster's layout (os:admin): the roles, the staged changes
// with what applying them would do, and the version history. namespace and pod name a ready
// Garage pod (the instance's "pod"). kubeServer: see KubePods.
func KubeGarageLayout(configYAML, contextName, kubeServer, namespace, pod string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return toJSON(demoGarageLayout(namespace))
	}

	report, err := withGarage(kubeTarget{configYAML, contextName, kubeServer}, namespace, pod, readGarageLayout)
	if err != nil {
		return "", err
	}

	return toJSON(report)
}

// KubeGarageStageRole stages a role for a Garage node (os:admin): zone, capacity in bytes
// (or gateway, then capacity is ignored) and tags (a JSON array of strings). A node without
// a role joins the layout, one with a role changes it. Nothing moves until
// KubeGarageApplyLayout. Answers the layout report, with the preview of the staged changes.
func KubeGarageStageRole(configYAML, contextName, kubeServer, namespace, pod, garageNode, zone string, capacity int64, gateway bool, tagsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))
	garageNode, zone = strings.TrimSpace(garageNode), strings.TrimSpace(zone)

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "garage-layout-stage", Namespace: namespace, Object: "Pod/" + pod,
		Params: fmt.Sprintf("node=%s zone=%s capacity=%d gateway=%t", shortGarageID(garageNode), zone, capacity, gateway)})

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return "", err
	}

	role, err := garageRoleChange(garageNode, zone, capacity, gateway, tagsJSON)
	if err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	report, err := withGarage(kubeTarget{configYAML, contextName, kubeServer}, namespace, pod,
		func(ctx context.Context, run execFunc, t garageTarget) (garageLayoutReport, error) {
			return updateGarageLayout(ctx, run, t, map[string]any{"roles": []any{role}})
		})
	if err != nil {
		return "", kubeMutationError(err)
	}

	return toJSON(report)
}

// KubeGarageStageRemoval stages the removal of a Garage node from the layout (os:admin): its
// data moves to the other nodes once the change is applied. Answers the layout report.
func KubeGarageStageRemoval(configYAML, contextName, kubeServer, namespace, pod, garageNode string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))
	garageNode = strings.TrimSpace(garageNode)

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "garage-layout-remove", Namespace: namespace, Object: "Pod/" + pod, Params: "node=" + shortGarageID(garageNode)})

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return "", err
	}

	if !garageFullNodeIDPattern.MatchString(garageNode) {
		return "", fmt.Errorf("invalid Garage node ID %q", garageNode)
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	report, err := withGarage(kubeTarget{configYAML, contextName, kubeServer}, namespace, pod,
		func(ctx context.Context, run execFunc, t garageTarget) (garageLayoutReport, error) {
			return updateGarageLayout(ctx, run, t, map[string]any{"roles": []any{map[string]any{"id": garageNode, "remove": true}}})
		})
	if err != nil {
		return "", kubeMutationError(err)
	}

	return toJSON(report)
}

// KubeGarageSetZoneRedundancy stages the layout's zone redundancy (os:admin): "maximum", or
// the least number of zones each partition is replicated in. Answers the layout report.
func KubeGarageSetZoneRedundancy(configYAML, contextName, kubeServer, namespace, pod, value string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))
	value = strings.TrimSpace(value)

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "garage-layout-redundancy", Namespace: namespace, Object: "Pod/" + pod, Params: "zoneRedundancy=" + value})

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return "", err
	}

	redundancy, err := garageZoneRedundancyBody(value)
	if err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	report, err := withGarage(kubeTarget{configYAML, contextName, kubeServer}, namespace, pod,
		func(ctx context.Context, run execFunc, t garageTarget) (garageLayoutReport, error) {
			return updateGarageLayout(ctx, run, t, map[string]any{"roles": []any{}, "parameters": map[string]any{"zoneRedundancy": redundancy}})
		})
	if err != nil {
		return "", kubeMutationError(err)
	}

	return toJSON(report)
}

// KubeGarageApplyLayout applies the staged layout changes (os:admin) as the version the
// preview announced (the report's nextVersion): Garage refuses any other, so a change staged
// meanwhile is never applied unseen. Data then moves between nodes. Answers the layout
// report, with Garage's own lines in messages.
func KubeGarageApplyLayout(configYAML, contextName, kubeServer, namespace, pod string, version int64) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "garage-layout-apply", Namespace: namespace, Object: "Pod/" + pod, Params: "version=" + strconv.FormatInt(version, 10)})

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return "", err
	}

	if version <= 0 {
		return "", errors.New("the layout version to apply must be the one the preview announced")
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	report, err := withGarage(kubeTarget{configYAML, contextName, kubeServer}, namespace, pod,
		func(ctx context.Context, run execFunc, t garageTarget) (garageLayoutReport, error) {
			applied, err := garageAPI[struct {
				Message []string `json:"message"`
			}](ctx, run, t, "ApplyClusterLayout", map[string]int64{"version": version})
			if err != nil {
				return garageLayoutReport{}, err
			}

			report, err := readGarageLayout(ctx, run, t)
			report.Messages = append([]string{}, applied.Message...)

			return report, err
		})
	if err != nil {
		return "", kubeMutationError(err)
	}

	return toJSON(report)
}

// KubeGarageRevertLayout drops every staged layout change (os:admin). Answers the layout report.
func KubeGarageRevertLayout(configYAML, contextName, kubeServer, namespace, pod string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)
	namespace, pod = privacy.reveal(strings.TrimSpace(namespace)), privacy.reveal(strings.TrimSpace(pod))

	defer recordAction(&err, configYAML, contextName, auditAction{Server: kubeServer, Action: "garage-layout-revert", Namespace: namespace, Object: "Pod/" + pod})

	if err := validateKubeName("pod", namespace, pod); err != nil {
		return "", err
	}

	if isDemoContext(configYAML, contextName) {
		return "", errDemoUnavailable
	}

	report, err := withGarage(kubeTarget{configYAML, contextName, kubeServer}, namespace, pod,
		func(ctx context.Context, run execFunc, t garageTarget) (garageLayoutReport, error) {
			if _, err := garageAPI[json.RawMessage](ctx, run, t, "RevertClusterLayout", nil); err != nil {
				return garageLayoutReport{}, err
			}

			return readGarageLayout(ctx, run, t)
		})
	if err != nil {
		return "", kubeMutationError(err)
	}

	return toJSON(report)
}

// garageFullNodeIDPattern is a whole node ID: UpdateClusterLayout takes no prefix.
var garageFullNodeIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// garageRoleChange is one UpdateClusterLayout role entry, checked the way Garage's CLI does:
// a zone, a capacity or the gateway flag, tags.
func garageRoleChange(nodeID, zone string, capacity int64, gateway bool, tagsJSON string) (map[string]any, error) {
	if !garageFullNodeIDPattern.MatchString(nodeID) {
		return nil, fmt.Errorf("invalid Garage node ID %q", nodeID)
	}

	if !garageZonePattern.MatchString(zone) {
		return nil, errors.New("a zone is required (one line, 64 characters at most)")
	}

	var tags []string
	if strings.TrimSpace(tagsJSON) != "" {
		if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
			return nil, fmt.Errorf("tags: %w", err)
		}
	}

	clean := []string{}

	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}

		if !garageZonePattern.MatchString(tag) {
			return nil, fmt.Errorf("invalid tag %q", tag)
		}

		clean = append(clean, tag)
	}

	role := map[string]any{"id": nodeID, "zone": zone, "tags": clean, "capacity": nil}

	if !gateway {
		if capacity < garageMinCapacity {
			return nil, fmt.Errorf("a storage node needs a capacity of at least %d bytes, or the gateway role", garageMinCapacity)
		}

		role["capacity"] = capacity
	}

	return role, nil
}

// garageZoneRedundancyBody encodes a zone redundancy as the API takes it: "maximum" or {"atLeast": n}.
func garageZoneRedundancyBody(value string) (any, error) {
	switch strings.ToLower(value) {
	case "maximum", "max", "none":
		return "maximum", nil
	}

	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return nil, errors.New(`zone redundancy must be "maximum" or a number of zones (at least 1)`)
	}

	return map[string]int{"atLeast": n}, nil
}

// garageZoneRedundancy words the API's value: "maximum" or the number.
func garageZoneRedundancy(p *garageAPIParameters) string {
	if p == nil || len(p.ZoneRedundancy) == 0 {
		return ""
	}

	var word string
	if json.Unmarshal(p.ZoneRedundancy, &word) == nil {
		return word
	}

	var atLeast struct {
		AtLeast int64 `json:"atLeast"`
	}
	if json.Unmarshal(p.ZoneRedundancy, &atLeast) == nil {
		return strconv.FormatInt(atLeast.AtLeast, 10)
	}

	return string(p.ZoneRedundancy)
}

// updateGarageLayout stages body (roles and/or parameters) and reads the layout back.
func updateGarageLayout(ctx context.Context, run execFunc, t garageTarget, body map[string]any) (garageLayoutReport, error) {
	if _, err := garageAPI[json.RawMessage](ctx, run, t, "UpdateClusterLayout", body); err != nil {
		return garageLayoutReport{}, err
	}

	return readGarageLayout(ctx, run, t)
}

// readGarageLayout asks the layout, its history and the cluster status together, then the
// preview when something is staged, and merges them into one report.
func readGarageLayout(ctx context.Context, run execFunc, t garageTarget) (garageLayoutReport, error) {
	var (
		layout                           garageAPILayout
		history                          garageAPIHistory
		status                           garageClusterStatus
		layoutErr, historyErr, statusErr error
		wg                               sync.WaitGroup
	)

	wg.Go(func() { layout, layoutErr = garageAPI[garageAPILayout](ctx, run, t, "GetClusterLayout", nil) })
	wg.Go(func() { history, historyErr = garageAPI[garageAPIHistory](ctx, run, t, "GetClusterLayoutHistory", nil) })
	wg.Go(func() { status, statusErr = garageAPI[garageClusterStatus](ctx, run, t, "GetClusterStatus", nil) })
	wg.Wait()

	if layoutErr != nil {
		return garageLayoutReport{}, layoutErr
	}

	// The history and the status refine the picture; the layout alone is still one.
	if historyErr != nil {
		history = garageAPIHistory{CurrentVersion: layout.Version}
	}

	if statusErr != nil {
		status = garageClusterStatus{}
	}

	report := mergeGarageLayout(layout, history, status)

	if report.NextVersion > 0 {
		preview, err := garageAPI[garageAPIPreview](ctx, run, t, "PreviewClusterLayoutChanges", nil)
		report.Preview = garagePreviewOf(preview, err)
	}

	return report, nil
}

func garagePreviewOf(p garageAPIPreview, err error) *garageLayoutPreview {
	out := &garageLayoutPreview{Messages: []string{}, MovedPartitions: -1}

	switch {
	case err != nil:
		out.Error = err.Error()
	case p.Error != "":
		out.Error = p.Error
	default:
		out.OK = true
		out.Messages = append(out.Messages, p.Message...)

		if s := p.Statistics; s != nil {
			out.UsableCapacity, out.EffectiveCapacity, out.TotalCapacity = s.UsableCapacity, s.EffectiveCapacity, s.TotalCapacity
			out.LowUsableCapacity, out.LowPartitionSize = s.LowUsableCapacity, s.LowPartitionSize

			if s.TotalMovedPartitions != nil {
				out.MovedPartitions = *s.TotalMovedPartitions
			}
		}
	}

	return out
}

func garageRoleOf(r garageAPIRole) *garageLayoutRole {
	role := &garageLayoutRole{Zone: r.Zone, Capacity: garageGatewayCapacity, Tags: append([]string{}, r.Tags...)}
	if r.Capacity != nil {
		role.Capacity = *r.Capacity
	}

	return role
}

// mergeGarageLayout joins the three answers node by node: a node is known to the cluster, in
// the layout, staged, or any mix of those.
func mergeGarageLayout(layout garageAPILayout, history garageAPIHistory, status garageClusterStatus) garageLayoutReport {
	report := garageLayoutReport{
		Version: layout.Version, PartitionSize: layout.PartitionSize,
		ZoneRedundancy: garageZoneRedundancy(layout.Parameters), StagedZoneRedundancy: garageZoneRedundancy(layout.StagedParameters),
		Nodes: []garageLayoutNode{}, Versions: []garageLayoutVersion{}, Trackers: []garageLayoutTracker{}, Messages: []string{},
		MinAck: history.MinAck,
	}

	if report.StagedZoneRedundancy == report.ZoneRedundancy {
		report.StagedZoneRedundancy = ""
	}

	nodes := map[string]*garageLayoutNode{}
	order := []string{}

	nodeOf := func(id string) *garageLayoutNode {
		if n, ok := nodes[id]; ok {
			return n
		}

		n := &garageLayoutNode{ID: id, Staged: garageStagedNone}
		nodes[id] = n
		order = append(order, id)

		return n
	}

	for _, s := range status.Nodes {
		n := nodeOf(s.ID)
		n.Hostname, n.Up, n.Known, n.Draining = s.Hostname, s.IsUp, true, s.Draining

		if s.DataPartition != nil {
			n.DataTotal = s.DataPartition.Total
		}
	}

	for _, r := range layout.Roles {
		n := nodeOf(r.ID)
		n.Role = garageRoleOf(r.garageAPIRole)

		if r.StoredPartitions != nil {
			n.Role.StoredPartitions = *r.StoredPartitions
		}

		if r.UsableCapacity != nil {
			n.Role.UsableCapacity = *r.UsableCapacity
		}
	}

	for _, c := range layout.StagedRoleChanges {
		n := nodeOf(c.ID)

		switch {
		case c.Remove:
			n.Staged = garageStagedRemove
		case n.Role == nil:
			n.Staged, n.StagedRole = garageStagedAdd, garageRoleOf(c.garageAPIRole)
		default:
			n.Staged, n.StagedRole = garageStagedChange, garageRoleOf(c.garageAPIRole)
		}
	}

	if len(layout.StagedRoleChanges) > 0 || report.StagedZoneRedundancy != "" {
		report.NextVersion = layout.Version + 1
	}

	hostnames := map[string]string{}

	for _, id := range order {
		n := nodes[id]
		if n.Hostname == "" && n.Role != nil && len(n.Role.Tags) > 0 {
			// Garage forgets a long-gone node's hostname; its layout tags often name the host.
			n.Hostname = strings.Join(n.Role.Tags, ",")
		}

		hostnames[id] = n.Hostname
		report.Nodes = append(report.Nodes, *n)
	}

	// Layout members first (down ones ahead), then the staged, then the rest; by zone and name.
	rank := func(n garageLayoutNode) int {
		switch {
		case n.Role != nil && !n.Up && n.Known:
			return 0
		case n.Role != nil:
			return 1
		case n.Staged != garageStagedNone:
			return 2
		default:
			return 3
		}
	}

	slices.SortStableFunc(report.Nodes, func(a, b garageLayoutNode) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}

		return strings.Compare(garageNodeZone(a)+"/"+a.Hostname+a.ID, garageNodeZone(b)+"/"+b.Hostname+b.ID)
	})

	for _, v := range history.Versions {
		report.Versions = append(report.Versions, garageLayoutVersion{Version: v.Version, Status: strings.ToLower(v.Status), StorageNodes: v.StorageNodes, GatewayNodes: v.GatewayNodes})
	}

	slices.SortFunc(report.Versions, func(a, b garageLayoutVersion) int { return int(b.Version - a.Version) })

	draining := slices.ContainsFunc(report.Versions, func(v garageLayoutVersion) bool { return v.Status == garageLayoutDraining })

	for id, tr := range history.UpdateTrackers {
		report.Trackers = append(report.Trackers, garageLayoutTracker{ID: id, Hostname: hostnames[id], Ack: tr.Ack, Sync: tr.Sync, SyncAck: tr.SyncAck})

		behind := tr.Ack < history.CurrentVersion || tr.Sync < history.CurrentVersion
		if n, ok := nodes[id]; draining && behind && ok && !n.Up {
			report.Stuck = true
		}
	}

	slices.SortFunc(report.Trackers, func(a, b garageLayoutTracker) int { return strings.Compare(a.Hostname+a.ID, b.Hostname+b.ID) })

	return report
}

func garageNodeZone(n garageLayoutNode) string {
	switch {
	case n.Role != nil:
		return n.Role.Zone
	case n.StagedRole != nil:
		return n.StagedRole.Zone
	default:
		return ""
	}
}
