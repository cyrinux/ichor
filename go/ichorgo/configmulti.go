package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// The same machine config change on several nodes: the edits made on one node's draft are
// replayed on each node's own config, previewed per node, then applied node by node.

// Node states of a multi-node apply.
const (
	multiStatePending   = "pending"
	multiStateApplying  = "applying"
	multiStateDone      = "done"
	multiStateSkipped   = "skipped"   // the edits do not fit this node's config
	multiStateUnchanged = "unchanged" // its config already has the change
	multiStateFailed    = "failed"
)

var errMultiTry = errors.New("try mode is for one node at a time: pick auto, staged or reboot")

// multiConfigNodePreview is one node's preview: its own diff, or why the edits do not fit it.
type multiConfigNodePreview struct {
	Node        string           `json:"node"`
	Hostname    string           `json:"hostname"`
	Changed     bool             `json:"changed"`
	Lines       []configDiffLine `json:"lines"`
	NeedsReboot bool             `json:"needsReboot"`
	Error       string           `json:"error,omitempty"`
}

type multiConfigPreview struct {
	Nodes     []multiConfigNodePreview `json:"nodes"`
	AnyReboot bool                     `json:"anyReboot"`
}

// multiConfigNodeState is a node's line in a multi-node apply's progress.
type multiConfigNodeState struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
}

// multiConfigProgress is configApplyProgress for the node at Index, plus every node's state.
type multiConfigProgress struct {
	Phase   string                 `json:"phase"`
	Message string                 `json:"message"`
	At      int64                  `json:"at"`
	Index   int                    `json:"index"`
	Total   int                    `json:"total"`
	Node    string                 `json:"node"`
	Nodes   []multiConfigNodeState `json:"nodes"`
}

// multiConfigCluster is what the multi-node preview and apply need (a fake in the tests).
type multiConfigCluster interface {
	applier(node string) configApplier
	// describe is the node's hostname ("" when unknown) and whether it is a control plane.
	describe(ctx context.Context, node string) (hostname string, controlPlane bool)
	// waitBack waits for node to reboot and run again.
	waitBack(ctx context.Context, node string, emit func(phase, message string)) error
	// quorum checks etcd is healthy, before a control plane reboots.
	quorum(ctx context.Context) error
}

// MachineConfigMultiPreview replays editsJSON, a JSON array of MachineConfigEdit's edits, on
// the config of each node of nodesCSV and tells what applying it would change there:
// {"nodes":[{"node","hostname","changed","lines","needsReboot","error"}],"anyReboot"}, with
// lines as in MachineConfigPreview. An edit that does not fit a node (a path it does not
// have) is that node's error, not the call's. Nothing is changed (os:admin, for the dry run).
func MachineConfigMultiPreview(configYAML, contextName, nodesCSV, editsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, nodesCSV = unmaskTargets(configYAML, contextName, nodesCSV)

	if privacy.isEnabled() {
		return "", errConfigPrivacy
	}

	nodes, edits, err := parseMultiConfig(nodesCSV, editsJSON)
	if err != nil {
		return "", err
	}

	if isTalosDemoContext(configYAML, contextName) {
		return toJSON(previewMultiConfig(context.Background(), demoMultiConfig{configYAML, contextName}, nodes, edits))
	}

	return withSession(configYAML, contextName, configPreviewTimeout, func(ctx context.Context, s *session) (string, error) {
		for _, node := range nodes {
			if err := validatePowerTarget(s.context, node); err != nil {
				return "", err
			}
		}

		return toJSON(previewMultiConfig(ctx, talosMultiConfig{s}, nodes, edits))
	})
}

// StartConfigApplyMulti applies editsJSON (as in MachineConfigMultiPreview) to each node of
// nodesCSV for good, one node after the other: workers first, control planes last, each as
// StartConfigApply does in mode auto, staged or reboot (try is for one node only). In reboot
// mode each node is back before the next starts, and etcd must be healthy before a control
// plane reboots. A node the edits do not fit is skipped, one already changed is left alone;
// the first failure stops the run, and the nodes done before it keep the change.
// OnProgress gets StartConfigApply's phases plus "index", "total", "node" and every node's
// state ("nodes": [{"node","hostname","state","error"}]).
func StartConfigApplyMulti(configYAML, contextName, nodesCSV, editsJSON, mode string, listener ConfigApplyListener) *ConfigApplyRun {
	contextName, nodesCSV = unmaskTargets(configYAML, contextName, nodesCSV)

	listener = maskedConfigApplyListener{listener}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)

		mode = strings.ToLower(strings.TrimSpace(mode))

		err := recordedRun(configYAML, contextName, func() auditAction {
			return auditAction{Action: "config-apply-multi", Node: strings.Join(splitCSV(nodesCSV), ","), Params: "mode=" + mode}
		}, func() error {
			return startConfigApplyMulti(ctx, configYAML, contextName, nodesCSV, editsJSON, mode, func(p multiConfigProgress) {
				emitJSON(p, listener.OnProgress)
			})
		})

		listener.OnDone(errText(err))
	}()

	return &ConfigApplyRun{cancel: cancel}
}

func startConfigApplyMulti(ctx context.Context, configYAML, contextName, nodesCSV, editsJSON, mode string, emit func(multiConfigProgress)) error {
	if privacy.isEnabled() {
		return errConfigPrivacy
	}

	nodes, edits, err := parseMultiConfig(nodesCSV, editsJSON)
	if err != nil {
		return err
	}

	if err := checkMultiMode(mode); err != nil {
		return err
	}

	if isDemoContext(configYAML, contextName) {
		return errDemoUnavailable
	}

	s, release, err := acquireSession(configYAML, contextName)
	if err != nil {
		return err
	}

	defer release()

	for _, node := range nodes {
		if err := validatePowerTarget(s.context, node); err != nil {
			return err
		}
	}

	return runConfigApplyMulti(ctx, talosMultiConfig{s}, nodes, edits, mode, emit)
}

func checkMultiMode(mode string) error {
	if mode == "try" {
		return errMultiTry
	}

	_, err := parseApplyMode(mode)

	return err
}

func parseMultiConfig(nodesCSV, editsJSON string) ([]string, []configEdit, error) {
	nodes := splitCSV(nodesCSV)
	if len(nodes) == 0 {
		return nil, nil, errors.New("no node given")
	}

	var edits []configEdit
	if err := json.Unmarshal([]byte(editsJSON), &edits); err != nil {
		return nil, nil, fmt.Errorf("invalid edits: %w", err)
	}

	if len(edits) == 0 {
		return nil, nil, errors.New("no edit given")
	}

	return nodes, edits, nil
}

// replayEdits applies edits, in order, to baseYAML.
func replayEdits(baseYAML string, edits []configEdit) (string, error) {
	docs, err := parseConfigDocs(baseYAML)
	if err != nil {
		return "", fmt.Errorf("the config is not valid YAML: %w", err)
	}

	for _, edit := range edits {
		if edit.Doc < 0 || edit.Doc >= len(docs) {
			return "", fmt.Errorf("no document %d in this config", edit.Doc)
		}

		if err := applyConfigEdit(docs[edit.Doc], edit); err != nil {
			return "", fmt.Errorf("%s: %w", strings.Join(edit.Path, "."), err)
		}
	}

	return encodeConfigDocs(docs)
}

func previewMultiConfig(ctx context.Context, cl multiConfigCluster, nodes []string, edits []configEdit) multiConfigPreview {
	out := multiConfigPreview{Nodes: make([]multiConfigNodePreview, len(nodes))}

	forEachNode(nodes, func(i int, node string) {
		hostname, _ := cl.describe(ctx, node)
		p := multiConfigNodePreview{Node: node, Hostname: hostname, Lines: []configDiffLine{}}

		preview, err := previewNodeEdits(ctx, cl.applier(node), edits)
		if err != nil {
			p.Error = err.Error()
		} else {
			p.Changed, p.Lines, p.NeedsReboot = preview.Changed, preview.Lines, preview.NeedsReboot
		}

		out.Nodes[i] = p
	})

	for _, n := range out.Nodes {
		out.AnyReboot = out.AnyReboot || n.NeedsReboot
	}

	return out
}

func previewNodeEdits(ctx context.Context, a configApplier, edits []configEdit) (configPreview, error) {
	snap, err := a.current(ctx)
	if err != nil {
		return configPreview{}, err
	}

	draft, err := replayEdits(snap.redacted, edits)
	if err != nil {
		return configPreview{}, err
	}

	return previewConfig(ctx, a, snap.redacted, draft)
}

// multiTarget is a node of the run, described.
type multiTarget struct {
	node, hostname string
	controlPlane   bool
}

// orderMultiTargets puts the workers first and the control planes last, in the given order.
func orderMultiTargets(targets []multiTarget) []multiTarget {
	out := slices.Clone(targets)

	slices.SortStableFunc(out, func(a, b multiTarget) int {
		switch {
		case a.controlPlane == b.controlPlane:
			return 0
		case a.controlPlane:
			return 1
		default:
			return -1
		}
	})

	return out
}

func runConfigApplyMulti(ctx context.Context, cl multiConfigCluster, nodes []string, edits []configEdit, mode string, emit func(multiConfigProgress)) error {
	targets := make([]multiTarget, len(nodes))

	forEachNode(nodes, func(i int, node string) {
		hostname, cp := cl.describe(ctx, node)
		if hostname == "" {
			hostname = node
		}

		targets[i] = multiTarget{node: node, hostname: hostname, controlPlane: cp}
	})

	targets = orderMultiTargets(targets)

	states := make([]multiConfigNodeState, len(targets))
	for i, t := range targets {
		states[i] = multiConfigNodeState{Node: t.node, Hostname: t.hostname, State: multiStatePending}
	}

	report := func(i int, phase, message string) {
		emit(multiConfigProgress{
			Phase: phase, Message: message, At: time.Now().UnixMilli(),
			Index: i, Total: len(targets), Node: targets[i].node, Nodes: slices.Clone(states),
		})
	}

	for i, t := range targets {
		states[i].State = multiStateApplying
		report(i, applyPhaseApplying, "")

		state, err := applyMultiNode(ctx, cl, t, edits, mode, func(phase, message string) { report(i, phase, message) })
		states[i].State = state

		if err != nil {
			states[i].Error = err.Error()
		}

		if state == multiStateFailed {
			report(i, applyPhaseDone, err.Error())

			return fmt.Errorf("%s: %w (%s)", t.hostname, err, multiDoneSummary(states))
		}
	}

	report(len(targets)-1, applyPhaseDone, multiDoneSummary(states))

	return nil
}

// applyMultiNode applies the edits to one node: the state it ends in, and why for a node
// skipped or failed.
func applyMultiNode(ctx context.Context, cl multiConfigCluster, t multiTarget, edits []configEdit, mode string, emit func(phase, message string)) (string, error) {
	nodeCtx, cancel := context.WithTimeout(ctx, configApplyTimeout)
	defer cancel()

	a := cl.applier(t.node)

	snap, err := a.current(nodeCtx)
	if err != nil {
		return multiStateFailed, err
	}

	draft, err := replayEdits(snap.redacted, edits)
	if err != nil {
		return multiStateSkipped, err
	}

	// Never two control planes down at once: the previous one is back, and etcd healthy.
	if t.controlPlane && mode == applyModeReboot {
		if err := cl.quorum(nodeCtx); err != nil {
			return multiStateFailed, fmt.Errorf("not rebooting a control plane: %w", err)
		}
	}

	err = runConfigApply(nodeCtx, a, configApply{
		base: snap.redacted, draft: draft, mode: mode, emit: emit,
		waitBack: func(ctx context.Context) error { return cl.waitBack(ctx, t.node, emit) },
	})

	switch {
	case errors.Is(err, errConfigUnchanged):
		return multiStateUnchanged, nil
	case err != nil:
		return multiStateFailed, err
	}

	return multiStateDone, nil
}

// multiDoneSummary names the nodes that took the change and those skipped.
func multiDoneSummary(states []multiConfigNodeState) string {
	var done, skipped []string

	for _, s := range states {
		switch s.State {
		case multiStateDone:
			done = append(done, s.Hostname)
		case multiStateSkipped:
			skipped = append(skipped, s.Hostname)
		}
	}

	summary := "applied to: none"
	if len(done) > 0 {
		summary = "applied to: " + strings.Join(done, ", ")
	}

	if len(skipped) > 0 {
		summary += "; skipped: " + strings.Join(skipped, ", ")
	}

	return summary
}

// talosMultiConfig is multiConfigCluster on a real cluster.
type talosMultiConfig struct{ s *session }

func (t talosMultiConfig) applier(node string) configApplier { return nodeConfigApplier{t.s, node} }

func (t talosMultiConfig) describe(ctx context.Context, node string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)
	hostname, cp := "", false

	if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, t.s.client.COSI, network.HostnameID); err == nil {
		hostname = hs.TypedSpec().Hostname
	}

	if mt, err := safe.StateGetByID[*config.MachineType](nodeCtx, t.s.client.COSI, config.MachineTypeID); err == nil {
		cp = mt.MachineType().IsControlPlane()
	}

	return hostname, cp
}

func (t talosMultiConfig) waitBack(ctx context.Context, node string, emit func(phase, message string)) error {
	return waitNodeRebooted(ctx, t.s.client, node, emit)
}

func (t talosMultiConfig) quorum(ctx context.Context) error {
	ov, err := gatherEtcdOverview(ctx, t.s)
	if err != nil {
		return fmt.Errorf("etcd: %w", err)
	}

	return etcdGate(ov)
}

// demoMultiConfig previews on the demo nodes' sample configs; nothing can be applied.
type demoMultiConfig struct{ yaml, contextName string }

func (d demoMultiConfig) applier(node string) configApplier {
	text, _ := demoRead("NodeMachineConfig", d.yaml, d.contextName, node)

	return demoConfigApplier{text}
}

func (d demoMultiConfig) describe(_ context.Context, node string) (string, bool) {
	for _, n := range demoNodes() {
		if n.Node == node {
			return n.Hostname, n.Role == "controlplane"
		}
	}

	return "", false
}

func (demoMultiConfig) waitBack(context.Context, string, func(string, string)) error {
	return errDemoUnavailable
}

func (demoMultiConfig) quorum(context.Context) error { return errDemoUnavailable }
