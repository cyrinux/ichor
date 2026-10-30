package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

// Step 3 of joining a node from the phone: the sealed config (step 2) goes to the node in
// maintenance mode over the insecure maintenance API, as `talosctl apply-config --insecure`
// does; the node installs Talos, reboots with the cluster's identity and joins. The run
// follows it until its Kubernetes node is Ready (and, for a control plane, its etcd member
// healthy), then reports its address for the app to add to the stored talosconfig.

const (
	// joinTimeout bounds the whole run: an install and a reboot take a few minutes.
	joinTimeout = 15 * time.Minute
	// joinRediscoverAfter: a node on DHCP that does not answer at its address this long
	// after the install may have been given another one; it is looked up by its hostname.
	joinRediscoverAfter = 90 * time.Second
)

// Join phases reported through JoinListener.OnProgress, in order.
const (
	joinPhaseApplying    = "applying"
	joinPhaseInstalling  = "installing"
	joinPhaseRebooting   = "rebooting"
	joinPhaseBooted      = "booted"
	joinPhaseJoining     = "joining"
	joinPhaseRegistering = "registering"
	joinPhaseDone        = "done"
)

var (
	errJoinInstalled = errors.New("this node is already installed (it asks for a client certificate): only a node booted from the Talos ISO, in maintenance mode, can be joined")
	errJoinStopped   = errors.New("stopped following: the install cannot be undone and continues on the node; it appears on the overview once it joins")
)

// JoinListener follows a node join (implemented in Kotlin/Swift).
type JoinListener interface {
	// OnProgress gets {"phase","message","at","address"} (at: unix ms; address: where the
	// node is followed) on every change.
	OnProgress(json string)
	// OnDone is called exactly once: nodeAddress is the joined node's address, to add to the
	// context's nodes (AddContextNodes); errMessage is empty on success.
	OnDone(nodeAddress string, errMessage string)
}

// JoinRun is a handle on a running join.
type JoinRun struct {
	cancel context.CancelFunc
}

// Cancel stops following the node: once the config is sent, the install goes on.
func (r *JoinRun) Cancel() { r.cancel() }

type joinProgress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	At      int64  `json:"at"`
	Address string `json:"address,omitempty"`
}

// NodeJoinWarnings is what the apps show before StartNodeJoin, for the user to confirm with
// the new node's hostname: one warning per line.
func NodeJoinWarnings(role string) (out string, err error) {
	defer maskResult(&out, &err)

	role = strings.TrimSpace(role)
	if role != joinRoleWorker && role != joinRoleControlPlane {
		return "", fmt.Errorf("unknown role %q (worker, controlplane)", role)
	}

	warnings := []string{
		"The config carries the cluster's secrets (its CA, tokens and keys).",
		"It is sent over TLS but without authentication to the address you typed: a node in maintenance mode cannot prove who it is.",
		"Make sure the address is your node, on your own network: check the IP its console shows.",
		"The node then installs Talos to the disk you picked and erases it; this cannot be undone from the phone.",
	}

	if role == joinRoleControlPlane {
		warnings = append(warnings, "A control plane joins etcd: the quorum then counts one more member.")
	}

	return strings.Join(warnings, "\n"), nil
}

// StartNodeJoin generates the new node's config from templateNode's (JoinNodeConfigSealed's
// arguments), sends it to the node in maintenance mode at address, and follows the node:
// applying → installing → rebooting → booted → joining (Talos ready, its Kubernetes node
// Ready; for a control plane, its etcd member healthy) → registering → done. OnDone gets the
// node's address: staticAddress's, else address, else where cluster discovery finds the
// hostname after a DHCP change. A hostname a cluster member already has is refused, unless
// replacing: the new node replaces that member (a dead node discovery still lists), and the
// member's addresses are then never followed. Bounded by 15 minutes; Cancel only stops
// following. Needs os:admin on the cluster; refused in privacy mode and on the demo. Recorded
// in the activity log as node-join (hostname, role, address; never the config).
func StartNodeJoin(configYAML, contextName, address, templateNode, role, hostname, installDisk, staticAddress, extraEditsJSON string, replacing bool, listener JoinListener) *JoinRun {
	contextName, templateNode = unmaskTarget(configYAML, contextName, templateNode)

	listener = maskedJoinListener{listener}

	j := &nodeJoin{
		config: configYAML, context: contextName, address: strings.TrimSpace(address),
		listener: listener, interval: upgradePollInterval,
		req: joinConfigRequest{
			template: templateNode, role: role, hostname: hostname, installDisk: installDisk, staticAddress: staticAddress,
		},
		editsJSON: extraEditsJSON,
		replacing: replacing,
	}

	return j.start(joinTimeout)
}

// nodeJoin is one run of StartNodeJoin.
type nodeJoin struct {
	config, context string
	address         string // the maintenance address the user typed
	req             joinConfigRequest
	editsJSON       string
	listener        JoinListener
	interval        time.Duration
	// replacing: the hostname may be a member's, the node this one replaces.
	replacing bool

	applied bool   // the config was sent: Cancel cannot undo anything any more
	node    string // where the installed node is followed
	// replaced are the addresses of the members already named like the new node: never followed.
	replaced []string
}

func (j *nodeJoin) start(timeout time.Duration) *JoinRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { j.listener.OnDone("", msg) })
		defer holdCalls(j.config, j.context)()

		err := recordedRun(j.config, j.context, j.auditAction, func() error { return j.run(ctx, timeout) })
		if err != nil {
			j.listener.OnDone("", errText(err))

			return
		}

		j.emit(joinPhaseDone, j.req.hostname+" joined as "+j.node)
		j.listener.OnDone(j.node, "")
	}()

	return &JoinRun{cancel: cancel}
}

func (j *nodeJoin) auditAction() auditAction {
	node := j.node
	if node == "" {
		node = j.address
	}

	params := "hostname=" + strings.TrimSpace(j.req.hostname) + " role=" + strings.TrimSpace(j.req.role)
	if j.replacing {
		params += " replacing=true"
	}

	return auditAction{Action: "node-join", Node: node, Params: params}
}

func (j *nodeJoin) emit(phase, message string) {
	emitJSON(joinProgress{Phase: phase, Message: message, At: time.Now().UnixMilli(), Address: j.node}, j.listener.OnProgress)
}

func (j *nodeJoin) run(ctx context.Context, timeout time.Duration) error {
	if isTalosDemoContext(j.config, j.context) {
		return errDemoUnavailable
	}

	if privacy.isEnabled() {
		return errJoinPrivacy
	}

	req, err := parseJoinRequest(j.req.template, j.req.role, j.req.hostname, j.req.installDisk, j.req.staticAddress, j.editsJSON)
	if err != nil {
		return err
	}

	j.req = req
	j.node = joinNodeAddress(j.address, req.staticAddress)

	if err := checkMaintenanceAddress(ctx, j.address); err != nil {
		return err
	}

	s, release, err := acquireSession(j.config, j.context)
	if err != nil {
		return err
	}

	defer release()

	if err := j.checkHostname(ctx, s); err != nil {
		return err
	}

	j.emit(joinPhaseApplying, "generating the config from "+req.template)

	full, _, err := joinConfigFor(j.config, j.context, req)
	if err != nil {
		return err
	}

	if err := j.apply(ctx, full); err != nil {
		return j.ended(ctx, err, timeout)
	}

	return j.ended(ctx, j.follow(ctx, s), timeout)
}

// ended turns the run's context ending into what to do about it.
func (j *nodeJoin) ended(ctx context.Context, err error, timeout time.Duration) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s did not join within %s: check its console for install errors, that %s is the right disk, and that it reaches the control plane",
			j.req.hostname, timeout, j.req.installDisk)
	case ctx.Err() != nil && j.applied:
		return errJoinStopped
	case ctx.Err() != nil:
		return errors.New("stopped before the config was sent: nothing changed on the node")
	}

	return err
}

// apply sends the config to the node in maintenance mode, once, to the address the user
// confirmed: never retried, never to another address.
func (j *nodeJoin) apply(ctx context.Context, full string) error {
	installed, err := asksClientCert(ctx, j.address)
	if err != nil {
		return errors.New(friendlyError(err))
	}

	if installed {
		return errJoinInstalled
	}

	c, err := maintenanceClient(ctx, j.address)
	if err != nil {
		return err
	}

	defer c.Close() //nolint:errcheck

	j.emit(joinPhaseApplying, "sending the config to "+j.address)

	// From here the config may be on the node, even when the answer never comes back.
	j.applied = true

	if _, err := c.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data: []byte(full), Mode: machineapi.ApplyConfigurationRequest_AUTO,
	}); err != nil {
		return fmt.Errorf("the node refused the config: %s", friendlyError(err))
	}

	j.emit(joinPhaseInstalling, "installing Talos to "+j.req.installDisk)

	return nil
}

// joinObservation is one poll of the new node.
type joinObservation struct {
	maintenance bool // the maintenance API still answers (before the install starts)
	installed   upgradeObservation
	kubeReady   bool
	etcdHealthy bool
}

// follow polls the node until it joined; the maintenance API answers until the install
// starts, then nothing does until the installed system answers with the cluster's identity.
func (j *nodeJoin) follow(ctx context.Context, s *session) error {
	m, err := maintenanceClient(ctx, j.address)
	if err != nil {
		return err
	}

	defer m.Close() //nolint:errcheck

	t := joinTracker{controlPlane: j.req.role == joinRoleControlPlane, hostname: j.req.hostname, disk: j.req.installDisk}
	var downSince time.Time

	timer := time.NewTimer(j.interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}

		o := j.observe(ctx, s, m, !t.leftMaintenance)
		if ctx.Err() != nil {
			continue
		}

		if !o.maintenance && !o.installed.reachable {
			if downSince.IsZero() {
				downSince = time.Now()
			}

			if j.req.staticAddress == "" && time.Since(downSince) > joinRediscoverAfter {
				j.rediscover(ctx, s)
			}
		} else {
			downSince = time.Time{}
		}

		if step := t.step(o); step.phase != "" {
			j.emit(step.phase, step.message)
		}

		if t.joined {
			j.emit(joinPhaseRegistering, "adding "+j.node+" to the cluster's nodes")

			return nil
		}

		timer.Reset(j.interval)
	}
}

func (j *nodeJoin) observe(ctx context.Context, s *session, m *client.Client, maintenanceToo bool) joinObservation {
	var o joinObservation

	if maintenanceToo {
		mctx, cancel := context.WithTimeout(ctx, nodeTimeout)
		_, err := m.Version(mctx)

		cancel()

		o.maintenance = err == nil
	}

	o.installed = observeNode(ctx, s.client, j.node)
	if !o.installed.reachable || o.installed.stage != runtime.MachineStageRunning.String() || !o.installed.ready {
		return o
	}

	nctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(nctx, j.node)

	if state := fetchKubeNodeState(nodeCtx, s.client); state != nil {
		o.kubeReady = state.Ready
	}

	if j.req.role == joinRoleControlPlane && o.kubeReady {
		o.etcdHealthy = etcdMemberHealthy(nodeCtx, s.client)
	}

	return o
}

// etcdMemberHealthy: the node's etcd answers as a full member (no longer a learner) without
// errors.
func etcdMemberHealthy(nodeCtx context.Context, c *client.Client) bool {
	resp, err := c.EtcdStatus(nodeCtx)
	if err != nil {
		return false
	}

	st := first(resp.GetMessages()).GetMemberStatus()

	return st != nil && st.GetMemberId() != 0 && !st.GetIsLearner() && len(st.GetErrors()) == 0
}

// checkHostname refuses a hostname a cluster member already has: the run would follow that
// member (by its hostname after a DHCP change) and report it joined. When replacing, the
// member is the node being replaced: its addresses are remembered, never to be followed. A
// cluster without discovery cannot tell, and is not refused.
func (j *nodeJoin) checkHostname(ctx context.Context, s *session) error {
	lctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	members, err := listMembers(lctx, s.client, targetNodes(s.context))
	if err != nil {
		return nil //nolint:nilerr // no discovery: nothing to compare with
	}

	for _, m := range members {
		if !strings.EqualFold(m.hostname, j.req.hostname) {
			continue
		}

		if !j.replacing {
			return fmt.Errorf("a cluster member is already named %s: pick another host name, or say the new node replaces it", j.req.hostname)
		}

		for _, a := range m.addresses {
			j.replaced = append(j.replaced, a.String())
		}
	}

	if len(j.replaced) > 0 {
		j.emit(joinPhaseApplying, "replacing the "+j.req.hostname+" discovery still lists ("+strings.Join(j.replaced, ", ")+"): it is never followed")
	}

	return nil
}

// rediscover looks the hostname up in cluster discovery: a node on DHCP may come back from
// the install with another address than the maintenance one. An address the context already
// targets is never taken: it is another node.
func (j *nodeJoin) rediscover(ctx context.Context, s *session) {
	lctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	targets := targetNodes(s.context)

	members, err := listMembers(lctx, s.client, targets)
	if err != nil {
		return
	}

	if addr := rediscovered(members, j.req.hostname, targets, j.replaced); addr != "" && addr != j.node {
		j.node = addr
		j.emit(joinPhaseRebooting, j.req.hostname+" came back at "+addr)
	}
}

// rediscovered is the address of the member named hostname that is the new node: not an
// address the context targets (another node), nor one of the replaced member's; "" if none.
func rediscovered(members []clusterMember, hostname string, targets, replaced []string) string {
	for _, m := range members {
		if !strings.EqualFold(m.hostname, hostname) {
			continue
		}

		addr := preferredAddress(m.addresses)
		other := func(t string) bool { return strings.EqualFold(endpointHost(t), addr) }

		if addr != "" && !slices.ContainsFunc(targets, other) && !slices.Contains(replaced, addr) {
			return addr
		}
	}

	return ""
}

// joinNodeAddress is where the installed node answers: its static address, else the
// maintenance one (without a port: the cluster's apid dials the node's own).
func joinNodeAddress(address, staticAddress string) string {
	if p, err := netip.ParsePrefix(staticAddress); err == nil {
		return p.Addr().String()
	}

	host := address
	if h, _, err := net.SplitHostPort(address); err == nil {
		host = h
	}

	return normalizeEndpoint(host)
}

// joinTracker turns observations into phases.
type joinTracker struct {
	controlPlane    bool
	hostname, disk  string
	phase, message  string
	leftMaintenance bool
	joined          bool
}

func (t *joinTracker) step(o joinObservation) trackStep {
	var phase, msg string

	in := o.installed

	switch {
	case o.maintenance && !t.leftMaintenance:
		phase, msg = joinPhaseInstalling, "the node took the config and prepares the install to "+t.disk
	case !in.reachable:
		t.leftMaintenance = true
		phase, msg = joinPhaseRebooting, "installing to "+t.disk+" and rebooting: the node does not answer meanwhile"
	case in.stage != runtime.MachineStageRunning.String() || !in.ready:
		t.leftMaintenance = true
		phase, msg = joinPhaseBooted, "booted Talos "+in.version+", waiting for it to be ready"
	case !o.kubeReady:
		t.leftMaintenance = true
		phase, msg = joinPhaseJoining, "Talos is ready, waiting for Kubernetes to report "+t.hostname+" Ready"
	case t.controlPlane && !o.etcdHealthy:
		t.leftMaintenance = true
		phase, msg = joinPhaseJoining, t.hostname+" is Ready, waiting for its etcd member to be healthy"
	default:
		t.leftMaintenance, t.joined = true, true
		phase, msg = joinPhaseJoining, t.hostname+" joined the cluster"
	}

	// The same phase with another message (etcd after Kubernetes) is still worth a line.
	if phase == t.phase && msg == t.message {
		return trackStep{}
	}

	t.phase, t.message = phase, msg

	return trackStep{phase: phase, message: msg}
}
