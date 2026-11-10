package ichorgo

// Bootstrapping a whole cluster from the phone, for the case a node at its console offers:
// it is booted from the Talos ISO (maintenance mode, nothing installed) and becomes the
// cluster's first and only control plane; the user joins more nodes afterwards
// (StartNodeJoin). Like `talosctl gen config` does on a laptop, the phone makes the
// cluster's secrets (its CAs, tokens and keys) and the node's control-plane config, sends
// that config over the insecure maintenance API (what `talosctl apply-config --insecure`
// does), follows the node through its install and reboot, bootstraps etcd, then hands the
// app an os:admin talosconfig and the admin kubeconfig. The secrets are made on the phone
// and leave it only inside the config sent to the address the user confirmed.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	machineryconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/role"
)

const (
	// newClusterTimeout bounds the whole run: an install, a reboot and a control plane
	// coming up take a few minutes.
	newClusterTimeout = 20 * time.Minute
	// newClusterInstallGrace: how long the node may stay unreachable between taking the
	// config and answering as the installed system, before the run gives up on it.
	newClusterInstallGrace = 5 * time.Minute
	// newClusterReadyFor bounds the wait for the installed node to report itself ready.
	newClusterReadyFor = 3 * time.Minute
	// newClusterAnswerFor bounds the retries of the first handshake: a node that never
	// answers (a wrong address, an ISO that never boots) should not burn the whole run
	// before the app is told what to check.
	newClusterAnswerFor = 2 * time.Minute
	// newClusterApplyFor bounds the retries of the config apply (a node that just booted
	// the ISO may still be bringing up its maintenance API).
	newClusterApplyFor = 60 * time.Second
	// newClusterAdminTTLHours is the lifetime of the talosconfig the run hands over.
	newClusterAdminTTLHours = 24 * 30
	// newClusterDefaultPort is the API server port of the endpoint built from the node.
	newClusterDefaultPort = 6443
)

// Phases reported through NewClusterListener.OnProgress, in order.
const (
	newClusterPhaseGenerating = "generating"
	newClusterPhaseApplying   = "applying"
	newClusterPhaseInstalling = "installing"
	newClusterPhaseBooting    = "booting"
	newClusterPhaseReady      = "ready"
	newClusterPhaseBootstrap  = "bootstrap"
	newClusterPhaseIssuing    = "issuing"
	newClusterPhaseDone       = "done"
)

var (
	// The three bounded waits as variables: the tests shorten them the way the other runs
	// shorten upgradePollInterval; the constants above are the shipped values.
	newClusterAnswerWait = newClusterAnswerFor
	newClusterApplyWait  = newClusterApplyFor
	newClusterGraceWait  = newClusterInstallGrace
	newClusterReadyWait  = newClusterReadyFor
	// newClusterTestInterval is the poll step the test twins run with (the tests' fast
	// step; production runs use upgradePollInterval). The tests' shortNewClusterWaits
	// shortens it further.
	newClusterTestInterval = 250 * time.Millisecond

	errNewClusterPrivacy = errors.New("turn privacy mode off to bootstrap a cluster: its secrets are generated here and carried to the node in the config")
	// errInstalledNoMaintenance: the address runs a Talos that asks for a client
	// certificate: it belongs to a cluster and cannot be wiped from here.
	errInstalledNoMaintenance = errors.New("that node belongs to a cluster already (it asks for a client certificate): boot it from the Talos ISO to reuse it")
)

// newClusterRequest is what a bootstrap was asked for, validated.
type newClusterRequest struct {
	address           string // host or host:port, as typed (the maintenance address)
	host              string // the address without its port: the node to follow and dial
	clusterName       string
	kubernetesVersion string // x.y[.z], no leading v
	installDisk       string
	installImage      string // the Image Factory installer reference, when chosen
	hostname          string
	endpoint          string // https://host:6443: the API server the cluster and kubeconfig point at
}

// newClusterPlan is what NewClusterPlan returns before anything is sent: no secret in it.
type newClusterPlan struct {
	// Warnings is one line per thing the user must confirm.
	Warnings []string `json:"warnings"`
	// NeedsWipe is true when the address already answers as an installed Talos whose API
	// still takes no client certificate (a fresh install that was never given a cluster
	// identity): the run resets it (erasing its system disk) before applying the config.
	NeedsWipe bool `json:"needsWipe"`
	// Size is the size in bytes of the config the node will be sent.
	Size int `json:"size"`
	// KubernetesVersion, Endpoint, InstallDisk and Hostname echo what was validated.
	KubernetesVersion string `json:"kubernetesVersion"`
	Endpoint          string `json:"endpoint"`
	InstallDisk       string `json:"installDisk"`
	Hostname          string `json:"hostname"`
	// NeedsBootstrap is always true: this is the cluster's first node.
	NeedsBootstrap bool `json:"needsBootstrap"`
}

// NewClusterPlan is what the apps show before StartNewCluster: what will happen and what it
// costs (the cluster's secrets are generated on the phone and carried to the node), and
// whether the address is already an installed Talos node the run would wipe first. Nothing
// is kept from what is generated here: it only measures what the node will receive.
func NewClusterPlan(address, clusterName, kubernetesVersion, installDisk, hostname, endpoint string) (out string, err error) {
	defer maskResult(&out, &err)

	req, err := parseNewClusterRequest(address, clusterName, kubernetesVersion, installDisk, hostname, endpoint)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	if err := checkMaintenanceAddress(ctx, req.address); err != nil {
		return "", err
	}

	wipe, err := newClusterNeedsWipe(ctx, req.address)
	if err != nil {
		return "", err
	}

	// The size of what the node will be sent, from a throwaway bundle (the real secrets
	// are made by the run, never here).
	full, err := generatedClusterConfig(req, nil)
	if err != nil {
		return "", err
	}

	warnings := []string{
		"this phone generates the cluster's CAs, tokens and keys: nothing is downloaded, and no secret of another cluster is involved",
		"they travel to the node inside the config, over TLS but unauthenticated: a node in maintenance mode cannot prove who it is",
		"make sure " + req.address + " is your node, on your own network: check the IP its console shows",
		"the node installs Talos to " + req.installDisk + " and becomes the single control plane of " + req.clusterName + "; that disk is erased",
		"store the talosconfig and the kubeconfig this run hands over: they are the only copy of the cluster's access",
	}
	if wipe {
		warnings = append(warnings, "the address already runs an installed Talos without a cluster: the run resets it first, erasing its system disk")
	}

	return toJSON(newClusterPlan{
		Warnings: warnings, NeedsWipe: wipe, Size: len(full),
		KubernetesVersion: req.kubernetesVersion, Endpoint: req.endpoint,
		InstallDisk: req.installDisk, Hostname: req.hostname, NeedsBootstrap: true,
	})
}

// NewClusterListener follows a bootstrap (implemented in Kotlin/Swift).
type NewClusterListener interface {
	// OnProgress gets {"phase","message","at","address"} (at: unix ms; address: where the
	// node is followed) on every change.
	OnProgress(json string)
	// OnTalosconfig hands over the os:admin talosconfig of the new cluster (the app stores
	// it as a new context); OnKubeconfig the admin kubeconfig. Both are called exactly
	// once, only when the bootstrap succeeded.
	OnTalosconfig(talosconfigYAML string)
	OnKubeconfig(kubeconfigYAML string)
	// OnDone is called exactly once; nodeAddress is where the control plane answers (empty
	// when it never did), errMessage is empty when the cluster bootstrapped.
	OnDone(nodeAddress string, errMessage string)
}

// NewClusterRun is a handle on a running bootstrap.
type NewClusterRun struct {
	cancel context.CancelFunc
}

// Cancel stops the run before its next step. Once the config is on the node the install
// cannot be undone; start the bootstrap again to follow the node from where it is.
func (r *NewClusterRun) Cancel() { r.cancel() }

type newClusterProgress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	At      int64  `json:"at"`
	Address string `json:"address,omitempty"`
}

// StartNewCluster bootstraps a fresh single-node cluster on the node at address: it
// generates the cluster's secrets and the node's control-plane config (cluster name,
// Kubernetes version, install disk, hostname; endpoint defaults to the node's own address
// as https://host:6443), sends them over the insecure maintenance API, follows the node
// through its install and reboot until Talos reports itself ready, bootstraps etcd, then
// hands over an os:admin talosconfig (valid 30 days) and the admin kubeconfig through the
// listener. A node that turns out to be a fresh Talos install answers the maintenance
// reset and is wiped first; a node of another cluster (it asks for a client certificate)
// is refused. Bounded by 20 minutes; Cancel only stops following. Refused in privacy mode
// (the config carries the secrets). Recorded in the activity log as cluster-bootstrap
// (hostname, Kubernetes version, disk, endpoint; never the config).
// startNewClusterLegacy bootstraps from explicit parts (the tests' twin and the older
// call shape): it makes the secrets itself. Production goes through NewClusterPrepare then
// StartNewCluster(draftId, ...).
func startNewClusterLegacy(address, clusterName, kubernetesVersion, installDisk, hostname, endpoint string, listener NewClusterListener) *NewClusterRun {
	listener = maskedNewClusterListener{listener}

	c := &newCluster{
		address: strings.TrimSpace(address), clusterName: clusterName, kubernetesVersion: kubernetesVersion,
		installDisk: installDisk, hostname: hostname, endpoint: endpoint, listener: listener,
		interval: upgradePollInterval, timeout: newClusterTimeout,
	}

	ctx, cancel := context.WithTimeout(context.Background(), newClusterTimeout)
	run := &NewClusterRun{cancel: cancel}

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { listener.OnDone("", msg) })
		defer holdCalls("", "")()

		var err error

		if privacy.isEnabled() {
			err = errNewClusterPrivacy
		} else {
			err = recordedRun("", "", c.auditAction, func() error { return c.run(ctx) })
		}

		listener.OnDone(c.node, errText(err))
	}()

	return run
}

// startRun opens the run's scope-bound goroutine: the run records itself in the activity
// log (recordedRun, so a panic is recorded too) and holds its cluster's scope for the
// duration. The audit and call-scope guards see the wiring here, in the run's own start.
func startRun(c *newCluster, ctx context.Context, cancel context.CancelFunc) {
	go func() {
		defer cancel()
		defer onPanic(func(msg string) { c.listener.OnDone("", msg) })
		defer holdCalls("", "")()

		var err error

		if privacy.isEnabled() {
			err = errNewClusterPrivacy
		} else {
			err = recordedRun("", "", c.auditAction, func() error { return c.run(ctx) })
		}

		c.listener.OnDone(c.node, errText(err))
	}()
}

// newCluster is one run of StartNewCluster.
type newCluster struct {
	address, clusterName, kubernetesVersion, installDisk, hostname, endpoint string
	listener                                                                 NewClusterListener

	req  newClusterRequest
	node string // where the control plane is followed: the address without its port

	// bundle is made once per run and reused across attempts: a second bootstrap of a
	// node that already took an earlier attempt's config carries the same cluster
	// identity, so it follows what it installed rather than clashing with it. The admin
	// certificate is issued once and kept for the same reason: dialCluster needs it too.
	bundle    *secrets.Bundle
	adminCert *x509Pair
	applied bool // the config may be on the node: Cancel cannot undo anything any more

	// draft carries a prepared cluster (NewClusterPrepare): its bundle and the exact
	// control-plane config are reused as-is, and storedYAML is the talosconfig the app
	// stored before this run, used to follow the node. Empty on the legacy path, where the
	// run makes the secrets itself (the tests' StartNewCluster twin).
	draft      *newClusterDraftEntry
	storedYAML string

	// dialAddress and clusterAddress, when set (the tests), are where the fake's
	// maintenance API and installed cluster API answer: the endpoint host stays the
	// address the console shows. Production dials address itself.
	dialAddress    string
	clusterAddress string

	// interval is how often the node is asked; timeout bounds the whole run. The tests
	// replace both (they also replace upgradePollInterval, which the runs share).
	interval time.Duration
	timeout  time.Duration
}

// dial is where the node's API answers: the address as typed, or the test fake's endpoint.
func (c *newCluster) dial() string {
	if c.dialAddress != "" {
		return c.dialAddress
	}

	return c.req.address
}

// x509Pair is a PEM certificate and its key (the run's os:admin client certificate).
type x509Pair struct {
	Crt []byte
	Key []byte
}

func (c *newCluster) start(timeout time.Duration) *NewClusterRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	run := &NewClusterRun{cancel: cancel}

	startRun(c, ctx, cancel)

	return run
}

func (c *newCluster) auditAction() auditAction {
	return auditAction{Action: "cluster-bootstrap", Node: c.node, Params: "hostname=" + strings.TrimSpace(c.hostname) +
		" k8s=" + strings.TrimSpace(c.kubernetesVersion) + " disk=" + strings.TrimSpace(c.installDisk) +
		" endpoint=" + strings.TrimSpace(c.endpoint)}
}

func (c *newCluster) emit(phase, message string) {
	emitJSON(newClusterProgress{Phase: phase, Message: message, At: time.Now().UnixMilli(), Address: c.node}, c.listener.OnProgress)
}

func (c *newCluster) run(ctx context.Context) error {
	req, err := c.request()
	if err != nil {
		return err
	}

	c.req = req
	c.node = req.host

	if c.interval == 0 {
		c.interval = upgradePollInterval
	}

	// The user already confirmed the machine's address in the prepares step (or the
	// address was validated for the draft): keep the local-network rule.
	if err := checkMaintenanceAddress(ctx, req.address); err != nil {
		return err
	}

	if c.draft == nil {
		c.emit(newClusterPhaseGenerating, "this phone is making "+req.clusterName+"'s CAs, tokens and keys")

		if err := c.ensureBundle(); err != nil {
			return err
		}
	}

	if err := c.apply(ctx); err != nil {
		return c.ended(err)
	}

	if err := c.follow(ctx); err != nil {
		return c.ended(err)
	}

	c.emit(newClusterPhaseBootstrap, "bootstrapping etcd on "+c.node)

	cl, err := c.dialCluster()
	if err != nil {
		return err
	}

	defer cl.Close() //nolint:errcheck

	if err := cl.Bootstrap(withRunNode(ctx, c.node), &machineapi.BootstrapRequest{}); err != nil {
		return fmt.Errorf("bootstrap etcd on %s: %s", c.node, friendlyError(err))
	}

	c.emit(newClusterPhaseIssuing, "issuing the cluster's talosconfig and kubeconfig")

	if err := c.handOver(ctx, cl); err != nil {
		return err
	}

	return nil
}

// request is the run's validated request: the draft's own for a prepared run (the address
// comes from the start call), the parts parsed for the legacy twin.
func (c *newCluster) request() (newClusterRequest, error) {
	if c.draft != nil {
		return c.draft.req, nil
	}

	return parseNewClusterRequest(c.address, c.clusterName, c.kubernetesVersion, c.installDisk, c.hostname, c.endpoint)
}

// ended turns the run's context ending into what to say about it.
func (c *newCluster) ended(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		if c.applied {
			return fmt.Errorf("stopped: %s did not come up in time and the install cannot be undone; start the bootstrap again to follow the node from where it is",
				c.req.address)
		}

		return errors.New("stopped before the config was sent: nothing changed on the node")
	}

	return err
}

// newClusterSecretsCache holds the one bundle the test runs share (see ensureBundle):
// package state, only ever touched by the test twins.
var newClusterSecretsCache *secrets.Bundle

// bundleFor makes the run's bundle: its own (a test scripted one), else the tests' shared
// one, else a fresh one. Making a bundle costs real entropy and seconds; the test twins
// share one.
func (c *newCluster) bundleFor() (*secrets.Bundle, error) {
	if c.draft != nil {
		return c.draft.bundle, nil
	}

	if c.bundle != nil {
		return c.bundle, nil
	}

	if newClusterSecretsCache != nil {
		return newClusterSecretsCache, nil
	}

	return secrets.NewBundle(secrets.NewClock(), machineryconfig.TalosVersionCurrent)
}

// ensureBundle makes the cluster's secrets and the run's os:admin certificate once
// (see newCluster.bundle).
func (c *newCluster) ensureBundle() error {
	bundle, err := c.bundleFor()
	if err != nil {
		return fmt.Errorf("generate the cluster's secrets: %w", err)
	}

	admin, err := bundle.GenerateTalosAPIClientCertificateWithTTL(role.MakeSet(role.Admin), newClusterAdminTTLHours*time.Hour)
	if err != nil {
		return fmt.Errorf("issue the admin certificate: %w", err)
	}

	c.bundle, c.adminCert = bundle, &x509Pair{Crt: admin.Crt, Key: admin.Key}

	return nil
}

// apply sends the config to the node, wiping it first when it turns out to answer as an
// installed Talos whose API still takes no client certificate (a fresh install that was
// never given a cluster identity).
func (c *newCluster) apply(ctx context.Context) error {
	c.emit(newClusterPhaseApplying, "reaching "+c.req.address+" over the maintenance API")

	// A refused handshake means the maintenance API is not up yet (an ISO still booting);
	// a node whose API asks for a client certificate belongs to a cluster already.
	upSince := time.Now()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		installed, err := asksClientCert(ctx, c.dial())

		switch {
		case err == nil && !installed:
		case err == nil:
			return errInstalledNoMaintenance
		case isRetryable(err) && time.Since(upSince) < newClusterAnswerWait:
			sleepCtx(ctx, c.interval)
			continue
		case dialRefused(err):
			return fmt.Errorf("%s does not answer: boot the node from the Talos ISO (its console must show maintenance mode) and start again", c.req.address)
		default:
			return errors.New(friendlyError(err))
		}

		break
	}

	// The maintenance client opens once and serves the wipe check and the apply: the
	// fake drops a target with no warning, and each call answers within nodeTimeout.
	m, err := maintenanceClient(ctx, c.dial())
	if err != nil {
		return err
	}

	defer m.Close() //nolint:errcheck

	if nodeAnswersAsInstalled(ctx, m) {
		if err := c.wipe(ctx, m); err != nil {
			return err
		}
	}

	// The config is what takes the most time to have: the secrets and the node's
	// config are made while the node is reached, so the apply goes out at once. A prepared
	// draft already holds the exact config made by NewClusterPrepare: reuse it byte for
	// byte, so what the app previewed is what the node is sent.
	full := ""
	if c.draft != nil {
		full = c.draft.full
	} else {
		if err := c.ensureBundle(); err != nil {
			return err
		}

		var err error

		if full, err = generatedClusterConfig(c.req, c.bundle); err != nil {
			return err
		}
	}

	c.emit(newClusterPhaseApplying, "sending the config to "+c.req.address)

	deadline := time.Now().Add(newClusterApplyWait)

	for {
		// A cancel that landed before the config is even dispatched never touched
		// the node: nothing changed, and the run says so.
		if err := ctx.Err(); err != nil {
			return err
		}

		// From the accepted attempt on, the config may be on the node even when the
		// answer never comes back; the flag goes on before the call, so a lost answer
		// (an Unavailable the retry loop takes for a node still booting) stays honest.
		c.applied = true

		_, err = m.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
			Data: []byte(full), Mode: machineapi.ApplyConfigurationRequest_AUTO,
		})
		if err == nil {
			break
		}

		if isRetryable(err) && ctx.Err() == nil && !time.Now().After(deadline) {
			sleepCtx(ctx, c.interval)

			continue
		}

		if ctx.Err() != nil {
			// The apply was the run's last word: what its end says about the node is
			// the run's, not this one call's transport error. A cancel that arrived
			// while the call was still held never reached the node: honest ends tell
			// that the config went out only from an attempt whose answer came back.
			return ctx.Err()
		}

		return fmt.Errorf("the node refused the config: %s", friendlyError(err))
	}

	c.emit(newClusterPhaseInstalling, "the node installs Talos to "+c.req.installDisk+" and reboots: it does not answer meanwhile")

	return nil
}

// wipe resets an installed-but-certificate-free node: Talos's reset of the system
// partitions with a reboot back into maintenance mode (what `talosctl reset --system`
// does), then waits for it to answer again with no machine config.
func (c *newCluster) wipe(ctx context.Context, m *client.Client) error {
	c.emit(newClusterPhaseApplying, "the node is an installed Talos without a cluster: resetting it first (its system disk is erased)")

	c.applied = true // a reset cannot be undone either

	_, err := m.ResetGenericWithResponse(ctx, &machineapi.ResetRequest{
		Graceful: true, Reboot: true,
		SystemPartitionsToWipe: []*machineapi.ResetPartitionSpec{
			{Label: constants.EFIPartitionLabel, Wipe: true},
			{Label: constants.BIOSGrubPartitionLabel, Wipe: true},
			{Label: constants.MetaPartitionLabel, Wipe: true},
			{Label: constants.StatePartitionLabel, Wipe: true},
		},
	})
	if err != nil {
		if !isRetryable(err) {
			return fmt.Errorf("reset the installed node: %s", friendlyError(err))
		}
		// The answer was lost on the way back while the node rebooted: keep going.
		c.emit(newClusterPhaseApplying, "the reset took the node down: waiting for it to come back in maintenance mode")
	}

	deadline := time.Now().Add(newClusterGraceWait)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		// The fake takes the whole node (its maintenance API included) while it
		// reboots; a real node keeps answering throughout. Either way the wipe is
		// done when the API answers again with no machine status.
		if nodeAnswered(ctx, m) && !nodeAnswersAsInstalled(ctx, m) {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not come back in maintenance mode after the reset: check its console", c.req.address)
		}

		sleepCtx(ctx, c.interval)
	}
}

// answeredAsInstalled: does the node answer a machine status read? Only an installed
// Talos has one (the maintenance mode of the ISO has nothing to report).
type maskedNewClusterListener struct{ NewClusterListener }

func (l maskedNewClusterListener) OnProgress(json string) {
	l.NewClusterListener.OnProgress(privacy.mask(json))
}

func (l maskedNewClusterListener) OnTalosconfig(talosconfigYAML string) {
	// A credential the app stores: never masked.
	l.NewClusterListener.OnTalosconfig(talosconfigYAML)
}

func (l maskedNewClusterListener) OnKubeconfig(kubeconfigYAML string) {
	l.NewClusterListener.OnKubeconfig(kubeconfigYAML)
}

func (l maskedNewClusterListener) OnDone(nodeAddress, errMessage string) {
	// The address goes into the stored talosconfig: it stays real (bootstraps refuse privacy mode).
	l.NewClusterListener.OnDone(nodeAddress, privacy.maskPlain(errMessage))
}

// newClusterForTest runs StartNewCluster's flow with the fake's poll interval and a test
// timeout: the exported entry point's constants (a 20 minute ceiling) are not for tests.
func newClusterForTest(address, clusterName, kubernetesVersion, installDisk, hostname, endpoint string, listener NewClusterListener, timeout time.Duration) *NewClusterRun {
	c := &newCluster{
		address: address, clusterName: clusterName, kubernetesVersion: kubernetesVersion,
		installDisk: installDisk, hostname: hostname, endpoint: endpoint,
		listener: maskedNewClusterListener{listener}, interval: newClusterTestInterval,
	}

	return c.start(timeout)
}

// newClusterAt is StartNewCluster's test twin: the console address the user typed, and the
// endpoint the fake node's API actually answers on (production dials both at the address).
func newClusterAt(address, installTarget, clusterAddress, clusterName, kubernetesVersion, installDisk, hostname, endpoint string, listener NewClusterListener, timeout time.Duration) *NewClusterRun {
	c := &newCluster{
		address: address, dialAddress: installTarget, clusterAddress: clusterAddress,
		clusterName: clusterName, kubernetesVersion: kubernetesVersion,
		installDisk: installDisk, hostname: hostname, endpoint: endpoint, listener: listener,
		interval: newClusterTestInterval,
	}

	return c.start(timeout)
}
