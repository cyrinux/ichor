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
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	machineryconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"github.com/siderolabs/talos/pkg/machinery/role"
	"go.yaml.in/yaml/v4"
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
	newClusterApplyWait  = newClusterApplyFor
	newClusterGraceWait  = newClusterInstallGrace
	newClusterReadyWait  = newClusterReadyFor

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
func StartNewCluster(address, clusterName, kubernetesVersion, installDisk, hostname, endpoint string, listener NewClusterListener) *NewClusterRun {
	listener = maskedNewClusterListener{listener}

	c := &newCluster{
		address: strings.TrimSpace(address), clusterName: clusterName, kubernetesVersion: kubernetesVersion,
		installDisk: installDisk, hostname: hostname, endpoint: endpoint, listener: listener,
		interval: upgradePollInterval, timeout: newClusterTimeout,
	}

	return c.start(newClusterTimeout)
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
	applied   bool // the config may be on the node: Cancel cannot undo anything any more

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

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { c.listener.OnDone("", msg) })

		var err error

		if privacy.isEnabled() {
			err = errNewClusterPrivacy
		} else {
			err = recordedRun("", "", c.auditAction, func() error { return c.run(ctx) })
		}

		c.listener.OnDone(c.node, errText(err))
	}()

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
	req, err := parseNewClusterRequest(c.address, c.clusterName, c.kubernetesVersion, c.installDisk, c.hostname, c.endpoint)
	if err != nil {
		return err
	}

	c.req = req
	c.node = req.host

	if c.interval == 0 {
		c.interval = upgradePollInterval
	}

	if err := checkMaintenanceAddress(ctx, req.address); err != nil {
		return err
	}

	c.emit(newClusterPhaseGenerating, "this phone is making "+req.clusterName+"'s CAs, tokens and keys")

	if err := c.ensureBundle(); err != nil {
		return err
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

	if err := cl.Bootstrap(client.WithNode(ctx, c.node), &machineapi.BootstrapRequest{}); err != nil {
		return fmt.Errorf("bootstrap etcd on %s: %s", c.node, friendlyError(err))
	}

	c.emit(newClusterPhaseIssuing, "issuing the cluster's talosconfig and kubeconfig")

	if err := c.handOver(ctx, cl); err != nil {
		return err
	}

	return nil
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

// ensureBundle makes the cluster's secrets once per run (see newCluster.bundle).
func (c *newCluster) ensureBundle() error {
	if c.bundle != nil {
		return nil
	}

	bundle, err := secrets.NewBundle(secrets.NewClock(), machineryconfig.TalosVersionCurrent)
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

	installed, err := asksClientCert(ctx, c.dial())
	if err != nil {
		if dialRefused(err) {
			return fmt.Errorf("%s does not answer: boot the node from the Talos ISO (its console must show maintenance mode) and start again", c.req.address)
		}

		return errors.New(friendlyError(err))
	}

	if installed {
		return errInstalledNoMaintenance
	}

	if err := c.ensureBundle(); err != nil {
		return err
	}

	full, err := generatedClusterConfig(c.req, c.bundle)
	if err != nil {
		return err
	}

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

	c.emit(newClusterPhaseApplying, "sending the config to "+c.req.address)

	deadline := time.Now().Add(newClusterApplyWait)

	for {
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

		if !isRetryable(err) || ctx.Err() != nil || time.Now().After(deadline) {
			return fmt.Errorf("the node refused the config: %s", friendlyError(err))
		}

		sleepCtx(ctx, c.interval)
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
func nodeAnswersAsInstalled(ctx context.Context, m *client.Client) bool {
	nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	_, err := safe.StateGetByID[*runtime.MachineStatus](nodeCtx, m.COSI, runtime.MachineStatusID)

	return err == nil
}

// answered: does the node answer any call at all?
func nodeAnswered(ctx context.Context, m *client.Client) bool {
	nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	_, err := m.Version(nodeCtx)

	return err == nil
}

// follow polls the maintenance API until the node answers as the installed, then polls
// until Talos reports itself ready: the maintenance API answers until the install starts,
// then nothing does until the installed Talos serves its API again.
func (c *newCluster) follow(ctx context.Context) error {
	m, err := maintenanceClient(ctx, c.dial())
	if err != nil {
		return err
	}

	defer m.Close() //nolint:errcheck

	// While the node still answers, the install has not started; once it goes down, the
	// installed node must come back within newClusterInstallGrace.
	downSince := time.Time{}
	installed := false

	for !installed {
		if err := ctx.Err(); err != nil {
			return err
		}

		switch {
		case nodeAnswersAsInstalled(ctx, m):
			installed = true
		case nodeAnswered(ctx, m):
			downSince = time.Time{}
		case downSince.IsZero():
			downSince = time.Now()
		case time.Since(downSince) > newClusterInstallGrace:
			return fmt.Errorf("%s did not come back installed within %s: check its console for install errors and that %s is the right disk",
				c.req.address, newClusterGraceWait, c.req.installDisk)
		}

		if !installed {
			sleepCtx(ctx, c.interval)
		}
	}

	c.emit(newClusterPhaseBooting, c.req.hostname+" booted Talos with the cluster's identity; waiting for it to be ready")

	return c.waitReady(ctx)
}

// waitReady polls until the node's Talos reports it is running and ready.
func (c *newCluster) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(newClusterReadyWait)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if c.reportedReady(ctx) {
			c.emit(newClusterPhaseReady, c.req.hostname+" is ready")

			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%s booted Talos but never became ready: check its console", c.req.address)
		}

		sleepCtx(ctx, c.interval)
	}
}

// reportedReady asks the maintenance API (it keeps serving an installed node that takes no
// certificate) for the version and the machine status: ready means Talos says its stage is
// running and its status is ready.
func (c *newCluster) reportedReady(ctx context.Context) bool {
	nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	m, err := maintenanceClient(nodeCtx, c.dial())
	if err != nil {
		return false
	}

	defer m.Close() //nolint:errcheck

	if _, err := m.Version(nodeCtx); err != nil {
		return false
	}

	ms, err := safe.StateGetByID[*runtime.MachineStatus](nodeCtx, m.COSI, runtime.MachineStatusID)

	return err == nil && ms.TypedSpec().Stage == runtime.MachineStageRunning && ms.TypedSpec().Status.Ready
}

// dialCluster opens the cluster with the run's own os:admin certificate: the phone is the
// first client of a cluster it just created.
func (c *newCluster) dialCluster() (*client.Client, error) {
	if c.bundle == nil || c.adminCert == nil {
		return nil, errors.New("the cluster's admin certificate was never issued")
	}

	// The cluster API answers where the maintenance API did (only the fake gives the two
	// different ports).
	cfg, err := talosconfigFor(c.req.clusterName, c.req.host, c.bundle.Certs.OS.Crt, c.adminCert, c.clusterAddress)
	if err != nil {
		return nil, err
	}

	cl, err := client.New(context.Background(), client.WithConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("connect to %s with the cluster's own certificate: %s", c.req.host, friendlyError(err))
	}

	return cl, nil
}

// talosconfigFor is the single-context talosconfig of the new cluster as a client config
// (base64 certificates, as talosctl writes them).
func talosconfigFor(clusterName, host string, caPEM []byte, admin *x509Pair, dialPort ...string) (*clientconfig.Config, error) {
	if len(caPEM) == 0 || admin == nil || len(admin.Crt) == 0 || len(admin.Key) == 0 {
		return nil, errors.New("the generated client configuration is incomplete")
	}

	// dialPort is the fake's port: endpoints and nodes keep it, the host stays bare.
	target := host
	if len(dialPort) > 0 && dialPort[0] != "" {
		if _, port, err := net.SplitHostPort(dialPort[0]); err == nil {
			target = net.JoinHostPort(host, port)
		}
	}

	enc := base64.StdEncoding.EncodeToString

	return &clientconfig.Config{
		Context: clusterName,
		Contexts: map[string]*clientconfig.Context{clusterName: {
			Endpoints: []string{target},
			Nodes:     []string{target},
			CA:        enc(caPEM),
			Crt:       enc(admin.Crt),
			Key:       enc(admin.Key),
		}},
	}, nil
}

// handOver issues the admin certificate once and gives the app the talosconfig and
// kubeconfig.
func (c *newCluster) handOver(ctx context.Context, cl *client.Client) error {
	cfg, err := talosconfigFor(c.req.clusterName, c.req.host, c.bundle.Certs.OS.Crt, c.adminCert, c.clusterAddress)
	if err != nil {
		return err
	}

	yaml, err := cfg.Bytes()
	if err != nil {
		return fmt.Errorf("encode talosconfig: %w", err)
	}

	kubeconfig, err := cl.Kubeconfig(client.WithNode(ctx, c.node))
	if err != nil {
		return fmt.Errorf("the control plane did not give a kubeconfig: %s", friendlyError(err))
	}

	c.listener.OnTalosconfig(string(yaml))
	c.listener.OnKubeconfig(string(kubeconfig))
	c.emit(newClusterPhaseDone, c.req.hostname+" is the control plane of "+c.req.clusterName+": store the talosconfig and the kubeconfig")

	return nil
}

// generatedClusterConfig is the node's control-plane config: generated with the bundle
// given (nil: a throwaway one, to measure the size only), then validated the way a joined
// config is. The generated config already carries the hostname (WithHostname) and the
// install disk (WithInstallDisk).
func generatedClusterConfig(req newClusterRequest, bundle *secrets.Bundle) (string, error) {
	opts := []generate.Option{
		generate.WithEndpointList([]string{req.host}),
		generate.WithInstallDisk(req.installDisk),
		generate.WithAdditionalSubjectAltNames([]string{req.host}),
	}
	if bundle != nil {
		opts = append(opts, generate.WithSecretsBundle(bundle))
	}

	in, err := generate.NewInput(req.clusterName, req.endpoint, req.kubernetesVersion, opts...)
	if err != nil {
		return "", fmt.Errorf("prepare the config: %w", err)
	}

	cfg, err := in.Config(machine.TypeControlPlane)
	if err != nil {
		return "", fmt.Errorf("generate the config: %w", err)
	}

	// The node's hostname: the generated config leaves it to the BIOS unless a
	// HostnameConfig document says otherwise; add it the way talosctl writes one.
	raw, err := cfg.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return "", fmt.Errorf("encode the config: %w", err)
	}

	text, err := patchClusterConfig(raw, req)
	if err != nil {
		return "", err
	}

	provider, err := configloader.NewFromBytes([]byte(text))
	if err != nil {
		return "", fmt.Errorf("the generated config is not valid: %w", err)
	}

	if _, err := provider.ValidateAsClient(joinRuntime{}); err != nil {
		return "", fmt.Errorf("the generated config is not valid: %w", err)
	}

	return provider.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
}

// patchClusterConfig puts the node's hostname in the generated config, under its
// HostnameConfig document (the generated config has none unless the input asked).
func patchClusterConfig(raw string, req newClusterRequest) (string, error) {
	docs, err := parseConfigDocs(raw)
	if err != nil {
		return "", fmt.Errorf("the generated config cannot be read: %w", err)
	}

	hostname := (*yaml.Node)(nil)

	for _, doc := range docs {
		if configDocKind(doc) == "HostnameConfig" {
			hostname = doc
		}
	}

	if hostname == nil {
		hostname = &yaml.Node{Kind: yaml.MappingNode}
		docs = append(docs, hostname)
	}

	setYAMLScalar(hostname, []string{"apiVersion"}, "v1alpha1")
	setYAMLScalar(hostname, []string{"kind"}, "HostnameConfig")
	setYAMLScalar(hostname, []string{"hostname"}, req.hostname)
	deleteYAMLKey(hostname, []string{"auto"})

	return encodeConfigDocs(docs)
}

// newClusterNeedsWipe: is the address an installed Talos whose API answers without a
// client certificate (a fresh install without a cluster, which the run can reset)?
func newClusterNeedsWipe(ctx context.Context, address string) (bool, error) {
	installed, err := asksClientCert(ctx, address)
	if err != nil {
		if dialRefused(err) {
			// Nothing there (yet): a node still booting the ISO is not a wipe.
			return false, nil
		}

		return false, errors.New(friendlyError(err))
	}

	if installed {
		return false, errInstalledNoMaintenance
	}

	dialCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	m, err := maintenanceClient(dialCtx, address)
	if err != nil {
		return false, nil
	}

	defer m.Close() //nolint:errcheck

	return nodeAnswersAsInstalled(dialCtx, m), nil
}

// parseNewClusterRequest validates what a bootstrap was asked for.
func parseNewClusterRequest(address, clusterName, kubernetesVersion, installDisk, hostname, endpoint string) (newClusterRequest, error) {
	req := newClusterRequest{
		address: strings.TrimSpace(address), clusterName: strings.TrimSpace(clusterName),
		kubernetesVersion: strings.TrimPrefix(strings.TrimSpace(kubernetesVersion), "v"),
		installDisk:       strings.TrimSpace(installDisk), hostname: strings.TrimSpace(hostname),
		endpoint: strings.TrimSpace(endpoint),
	}

	if req.address == "" || !validHostPort(req.address) {
		return newClusterRequest{}, fmt.Errorf("the address must be the IP (or host) the node's console shows, host or host:port, not %q", address)
	}

	req.host = reqAddressHost(req.address)

	if req.clusterName == "" {
		return newClusterRequest{}, errors.New("the cluster needs a name")
	}

	if !validHostname(req.hostname) {
		return newClusterRequest{}, fmt.Errorf("%q is not a host name for the node", hostname)
	}

	if !strings.HasPrefix(req.installDisk, "/dev/") {
		return newClusterRequest{}, fmt.Errorf("the install disk is a device path like /dev/sda, not %q", installDisk)
	}

	if !kubeVersionOK(req.kubernetesVersion) {
		return newClusterRequest{}, fmt.Errorf("the Kubernetes version is a version like 1.34.0, not %q", kubernetesVersion)
	}

	if req.endpoint == "" {
		req.endpoint = "https://" + net.JoinHostPort(req.host, strconv.Itoa(newClusterDefaultPort))

		return req, nil
	}

	if err := checkClusterEndpoint(req.endpoint); err != nil {
		return newClusterRequest{}, err
	}

	return req, nil
}

// reqAddressHost is the address without its port; unbracketed, so "https://host:6443"
// carries no IPv6 brackets (endpointHost would leave them).
func reqAddressHost(address string) string {
	if h, _, err := net.SplitHostPort(address); err == nil {
		return strings.Trim(h, "[]")
	}

	return strings.Trim(address, "[]")
}

// checkClusterEndpoint accepts the API server address the user picks: an https URL with a
// host (or host:port) and nothing else. It may name the node itself or a VIP or DNS record
// that points at it.
func checkClusterEndpoint(endpoint string) error {
	rest, ok := strings.CutPrefix(endpoint, "https://")
	if !ok || rest == "" || strings.ContainsAny(rest, "/?#@") {
		return fmt.Errorf("the API endpoint is an https:// URL like https://192.0.2.10:6443, not %q", endpoint)
	}

	host := rest

	if h, port, err := net.SplitHostPort(rest); err == nil {
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("the API endpoint %q has a bad port", endpoint)
		}

		host = strings.Trim(h, "[]")
	}

	if net.ParseIP(host) != nil {
		return nil
	}

	if !validHostname(host) {
		return fmt.Errorf("the API endpoint %q does not name a host", endpoint)
	}

	return nil
}

// kubeVersionOK: 1.x or 1.x.y, each part digits.
func kubeVersionOK(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) != 2 && len(parts) != 3 || parts[0] != "1" {
		return false
	}

	for _, p := range parts {
		if p == "" || len(p) > 3 || strings.ContainsFunc(p, func(r rune) bool { return r < '0' || r > '9' }) {
			return false
		}
	}

	return true
}

// dialRefused tells "nothing is there" (connection refused, no route, DNS failure, a
// timeout reaching nothing) from a node that answered badly.
func dialRefused(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())

	for _, s := range []string{"connection refused", "no route to host", "network is unreachable", "no such host", "i/o timeout"} {
		if strings.Contains(msg, s) {
			return true
		}
	}

	return false
}

// maskedNewClusterListener masks what the app displays; the credentials it stores are not
// masked (bootstraps refuse privacy mode anyway).
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
		listener: maskedNewClusterListener{listener}, interval: joinPollInterval,
	}

	return c.start(timeout)
}

// newClusterAt is StartNewCluster's test twin: the console address the user typed, and the
// endpoint the fake node's API actually answers on (production dials both at the address).
func newClusterAt(address, dialAddress, clusterAddress, clusterName, kubernetesVersion, installDisk, hostname, endpoint string, listener NewClusterListener, timeout time.Duration) *NewClusterRun {
	c := &newCluster{
		address: address, dialAddress: dialAddress, clusterAddress: clusterAddress,
		clusterName: clusterName, kubernetesVersion: kubernetesVersion,
		installDisk: installDisk, hostname: hostname, endpoint: endpoint, listener: listener,
		interval: joinPollInterval,
	}

	return c.start(timeout)
}
