package ichorgo

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

const (
	// planTimeout covers probing every node (an unreachable one takes nodeTimeout) and etcd.
	planTimeout     = 40 * time.Second
	etcdListTimeout = 5 * time.Second
)

type upgradePlan struct {
	Node           string    `json:"node"`
	Hostname       string    `json:"hostname"`
	ControlPlane   bool      `json:"controlPlane"`
	CurrentVersion string    `json:"currentVersion"`
	CurrentImage   string    `json:"currentImage"`
	Schematic      string    `json:"schematic"`
	Etcd           *planEtcd `json:"etcd"`
	Blockers       []string  `json:"blockers"`
	Warnings       []string  `json:"warnings"`
	// Acknowledge are risks the user must confirm to start: StartUpgrade refuses them
	// unless acknowledged (force does not skip them).
	Acknowledge  []string `json:"acknowledge"`
	Forceable    bool     `json:"forceable"` // every blocker is an etcd check that force skips
	etcdBlockers []string // the blockers force bypasses
	target       planPeer
	peers        []planPeer // the other nodes
}

type planEtcd struct {
	Members         int  `json:"members"`
	Healthy         int  `json:"healthy"`
	ThisNodeMember  bool `json:"thisNodeMember"`
	QuorumAfterLoss bool `json:"quorumAfterLoss"` // false: taking this node down loses quorum
}

// planPeer is a node's state as the plan sees it.
type planPeer struct {
	node         string
	hostname     string
	reachable    bool
	err          string
	stage        string
	ready        bool
	version      string
	controlPlane bool
}

// etcdHealth is the etcd membership seen from the cluster, for a control-plane target.
type etcdHealth struct {
	err         string
	members     int // voting members
	healthy     int
	unstarted   int
	thisMember  bool
	thisHealthy bool
}

type planInput struct {
	target       planPeer
	others       []planPeer
	image        string
	imageErr     string
	etcd         *etcdHealth
	defaultImage string
	endpoints    []string   // the talosconfig context's endpoints
	lock         *lockState // nil when not checked
}

// UpgradePlan checks whether node can be upgraded now, read-only (os:reader, os:admin to read
// the installer image from the machine config). See upgradePlan for the JSON. Blockers are
// hard stops (StartUpgrade refuses them); when "forceable" is true they are all etcd
// checks, which force skips like `talosctl upgrade --force`. It also reads the cluster
// upgrade lock (see upgradelock.go) through the Kubernetes API; kubeServer: see KubePods.
func UpgradePlan(configYAML, contextName, kubeServer, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	return withSession(configYAML, contextName, planTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		return toJSON(gatherPlan(ctx, s, node, kubeTarget{configYAML, contextName, kubeServer}))
	})
}

// gatherPlan probes the cluster for the plan; a zero kube skips reading the upgrade lock.
func gatherPlan(ctx context.Context, s *session, node string, kube kubeTarget) upgradePlan {
	nodes := targetNodes(s.context)
	peers := make([]planPeer, len(nodes))

	forEachNode(nodes, func(i int, n string) { peers[i] = peerFromProbe(n, probeNode(ctx, s.client, n)) })

	in := planInput{endpoints: s.context.Endpoints}

	for _, p := range peers {
		if p.node == node {
			in.target = p
		} else {
			in.others = append(in.others, p)
		}
	}

	if !in.target.reachable {
		return computePlan(in)
	}

	in.defaultImage = defaultInstallerRepo + ":" + in.target.version
	in.image, in.imageErr = installerImage(ctx, s.client, node)

	if in.target.controlPlane {
		via := []string{node}
		for _, p := range in.others {
			if p.controlPlane && p.reachable {
				via = append(via, p.node)
			}
		}

		in.etcd = gatherEtcdHealth(ctx, s.client, via, node, in.target.hostname)
	}

	in.lock = readUpgradeLock(ctx, kube, time.Now())

	return computePlan(in)
}

func peerFromProbe(node string, p nodeProbe) planPeer {
	o := buildNodeOverview(node, p)

	return planPeer{
		node:         node,
		hostname:     o.Hostname,
		reachable:    o.Reachable,
		err:          o.Error,
		stage:        o.Stage,
		ready:        o.Ready,
		version:      o.Version,
		controlPlane: o.Role == "controlplane",
	}
}

// installerImage reads machine.install.image from the node's machine config (os:admin).
func installerImage(ctx context.Context, c *client.Client, node string) (string, string) {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	mc, err := safe.StateGetByID[*config.MachineConfig](client.WithNode(ctx, node), c.COSI, config.ActiveID)
	if err != nil {
		return "", friendlyError(err)
	}

	if cfg := mc.Provider(); cfg != nil && cfg.Machine() != nil && cfg.Machine().Install() != nil {
		return cfg.Machine().Install().Image(), ""
	}

	return "", ""
}

// gatherEtcdHealth lists the etcd members through the first control plane of via that
// answers (the target first) and asks each member for its status through its own address.
func gatherEtcdHealth(ctx context.Context, c *client.Client, via []string, node, hostname string) *etcdHealth {
	members, listErr := firstAnswer(via, func(n string) ([]*machineapi.EtcdMember, error) { return etcdMembers(ctx, c, n) })
	if listErr != nil {
		return &etcdHealth{err: friendlyError(listErr)}
	}

	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	h := &etcdHealth{}

	// Which members to ask, and whether each is the node being planned.
	type probe struct {
		addr string
		this bool
	}

	var probes []probe

	for _, m := range members {
		if m.GetIsLearner() {
			continue
		}

		h.members++

		addr := memberAddress(m)
		this := m.GetHostname() == hostname || addr == node

		if this {
			h.thisMember = true
		}

		if m.GetHostname() == "" || addr == "" {
			h.unstarted++

			continue
		}

		probes = append(probes, probe{addr, this})
	}

	var mu sync.Mutex

	forEachNode(probes, func(_ int, p probe) {
		if _, healthy := memberHealthy(ctx, c, p.addr); !healthy {
			return
		}

		mu.Lock()
		defer mu.Unlock()

		h.healthy++
		h.thisHealthy = h.thisHealthy || p.this
	})

	return h
}

func etcdMembers(ctx context.Context, c etcdReader, node string) ([]*machineapi.EtcdMember, error) {
	ctx, cancel := context.WithTimeout(ctx, etcdListTimeout)
	defer cancel()

	resp, err := c.EtcdMemberList(client.WithNode(ctx, node), &machineapi.EtcdMemberListRequest{})
	if err != nil {
		return nil, err
	}

	return first(resp.GetMessages()).GetMembers(), nil
}

func memberAddress(m *machineapi.EtcdMember) string {
	for _, raw := range append(slices.Clone(m.GetClientUrls()), m.GetPeerUrls()...) {
		if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}

	return ""
}

// memberHealthy asks the member at addr for its own etcd status: healthy when it answered
// without reporting errors.
func memberHealthy(ctx context.Context, c etcdReader, addr string) (*machineapi.EtcdMemberStatus, bool) {
	resp, err := c.EtcdStatus(client.WithNode(ctx, addr))
	if err != nil {
		return nil, false
	}

	st := first(resp.GetMessages()).GetMemberStatus()

	return st, st != nil && len(st.GetErrors()) == 0
}

// busyStages are the stages in which a node is in the middle of a lifecycle operation.
var busyStages = []string{
	runtime.MachineStageBooting.String(), runtime.MachineStageInstalling.String(),
	runtime.MachineStageUpgrading.String(), runtime.MachineStageRebooting.String(),
	runtime.MachineStageShuttingDown.String(), runtime.MachineStageResetting.String(),
}

// computePlan applies the upgrade rules to what was gathered. Talos itself refuses (unless
// forced) to upgrade a control plane when etcd has 2 members or any member is unhealthy.
func computePlan(in planInput) upgradePlan {
	t := in.target
	plan := upgradePlan{
		Node:           t.node,
		Hostname:       t.hostname,
		ControlPlane:   t.controlPlane,
		CurrentVersion: t.version,
		Blockers:       []string{},
		Warnings:       []string{},
		Acknowledge:    []string{},
		target:         t,
		peers:          in.others,
	}

	if !t.reachable {
		plan.Blockers = append(plan.Blockers, "node is unreachable: "+strings.TrimPrefix(t.err, "unreachable: "))

		return plan
	}

	plan.CurrentImage = in.image
	if plan.CurrentImage == "" {
		plan.CurrentImage = in.defaultImage

		if in.imageErr != "" {
			plan.Warnings = append(plan.Warnings,
				"could not read the installer image from the machine config ("+in.imageErr+"); assuming "+defaultInstallerRepo)
		}
	}

	plan.Schematic = imageSchematic(plan.CurrentImage)

	switch {
	case t.stage == runtime.MachineStageUpgrading.String() || t.stage == runtime.MachineStageInstalling.String():
		plan.Blockers = append(plan.Blockers, "an upgrade is apparently already in progress on this node ("+t.stage+")")
	case t.stage != runtime.MachineStageRunning.String():
		plan.Blockers = append(plan.Blockers, "node is "+t.stage+", not running")
	case !t.ready:
		plan.Warnings = append(plan.Warnings, "node is not ready")
	}

	plan.Blockers = append(plan.Blockers, peerBlockers(in.others)...)
	plan.Warnings = append(plan.Warnings, peerWarnings(t, in.others)...)

	applyLock(&plan, in.lock, append([]planPeer{t}, in.others...))

	if onlyEndpoint(t, in.endpoints) {
		plan.Acknowledge = append(plan.Acknowledge,
			"this node is the only Talos endpoint of this context: the app loses access while it reboots, and if it does not boot again you need its console to recover")
	}

	if t.controlPlane {
		applyEtcd(&plan, in.etcd)
	}

	plan.Forceable = len(plan.Blockers) > 0 && len(plan.etcdBlockers) == len(plan.Blockers)

	// Without the legacy upgrade API the node neither drains nor checks etcd: see requestUpgrade.
	if t.version != "" && compareMinor(t.version, legacyUpgradeRemoved) >= 0 {
		plan.Warnings = append(plan.Warnings, noDrainWarning+": its pods stop when it reboots")
		plan.Forceable = false
	}

	return plan
}

func peerBlockers(others []planPeer) []string {
	var out []string

	for _, p := range others {
		if p.reachable && slices.Contains(busyStages, p.stage) {
			out = append(out, fmt.Sprintf("%s is %s: wait until it is running again (another upgrade or reboot in progress?)", p.hostname, p.stage))
		}
	}

	return out
}

func peerWarnings(t planPeer, others []planPeer) []string {
	var out []string

	versions := map[string]bool{t.version: true}

	for _, p := range others {
		switch {
		case !p.reachable:
			out = append(out, p.hostname+" is unreachable")
		case p.stage == runtime.MachineStageRunning.String() && !p.ready:
			out = append(out, p.hostname+" is not ready")
		}

		if p.reachable && p.version != "" {
			versions[p.version] = true
		}
	}

	if len(versions) > 1 {
		list := slices.Sorted(maps.Keys(versions))
		out = append(out, "nodes run different Talos versions: "+strings.Join(list, ", ")+" (upgrade one minor version at a time)")
	}

	return out
}

func applyEtcd(plan *upgradePlan, h *etcdHealth) {
	block := func(msg string) {
		plan.Blockers = append(plan.Blockers, msg)
		plan.etcdBlockers = append(plan.etcdBlockers, msg)
	}

	if h == nil || h.err != "" {
		reason := "no answer"
		if h != nil {
			reason = h.err
		}

		block("cannot check etcd health: " + reason)

		return
	}

	quorum := h.members/2 + 1
	remaining := h.healthy
	if h.thisHealthy {
		remaining--
	}

	plan.Etcd = &planEtcd{
		Members:         h.members,
		Healthy:         h.healthy,
		ThisNodeMember:  h.thisMember,
		QuorumAfterLoss: remaining >= quorum,
	}

	switch {
	case h.members == 1:
		plan.Acknowledge = append(plan.Acknowledge,
			"single control plane: etcd and the Kubernetes API are down while it upgrades, and the cluster is down if it does not boot again")
	case !plan.Etcd.QuorumAfterLoss:
		block(fmt.Sprintf("etcd would lose quorum: %d of %d members would stay healthy, %d needed", max(remaining, 0), h.members, quorum))
	}

	if unhealthy := h.members - h.healthy; unhealthy > 0 {
		block(fmt.Sprintf("%d of %d etcd members are unhealthy or not started: Talos only upgrades a control plane when all are healthy", unhealthy, h.members))
	}

	if !h.thisMember {
		plan.Warnings = append(plan.Warnings, "this control-plane node is not an etcd member")
	}
}

// applyLock turns the cluster upgrade lock into a blocker (another run holds it) or a
// warning (it could not be read: the upgrade goes on without a lock).
func applyLock(plan *upgradePlan, lock *lockState, peers []planPeer) {
	switch {
	case lock == nil:
	case lock.err != "":
		plan.Warnings = append(plan.Warnings, "cannot check the cluster upgrade lock ("+lock.err+"): make sure nobody else upgrades this cluster now")
	case lock.held != nil && !lockSettled(lock.held, peers):
		plan.Blockers = append(plan.Blockers, lock.held.describe())
	}
}

// onlyEndpoint tells every endpoint of the context is node itself: rebooting it cuts the
// app off. A hostname endpoint is compared by name only (best effort).
func onlyEndpoint(t planPeer, endpoints []string) bool {
	if len(endpoints) == 0 {
		return false
	}

	for _, e := range endpoints {
		if host := endpointHost(e); host != t.node && (t.hostname == "" || host != t.hostname) {
			return false
		}
	}

	return true
}

// UpgradeVersionCheck returns the risk of upgrading from one Talos version to another, to
// acknowledge before StartUpgrade, or "": Talos supports upgrading one minor version at a
// time, and a downgrade is not an upgrade path it tests.
func UpgradeVersionCheck(fromVersion, toVersion string) (out string) {
	defer maskResult(&out, new(error))

	return versionRisk(fromVersion, toVersion)
}

func versionRisk(fromVersion, toVersion string) string {
	from, okFrom := normalizeTalosVersion(fromVersion)
	to, okTo := normalizeTalosVersion(toVersion)

	if !okFrom || !okTo {
		return ""
	}

	a, b := versionNumbers(from), versionNumbers(to)

	switch {
	case slices.Compare(b[:], a[:]) < 0:
		return fmt.Sprintf("%s is older than %s: downgrading Talos is not a tested path and may not boot", to, from)
	case b[0] != a[0] || b[1] > a[1]+1:
		return fmt.Sprintf("%s skips minor versions from %s: Talos supports upgrading one minor version at a time", to, from)
	}

	return ""
}
