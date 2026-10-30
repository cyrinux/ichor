package ichorgo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/crypto/x509"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/role"
	"google.golang.org/grpc/codes"
)

// The run of a Talos API CA rotation (TalosCARotationPlan says whether it can start). Like
// talosctl, it never leaves the cluster without a CA every node trusts: the new CA is first
// accepted next to the old one, then made the issuing one, then the old one is dropped, and
// every node is checked after each step. Unlike talosctl, the phone's own talosconfig is
// replaced before anything changes the nodes' certificates: the app stores a transition
// talosconfig (new client certificate, both CAs trusted) and confirms it did (Proceed) before
// the issuing CA is swapped, so an interrupted run still leaves the phone able to reach the
// cluster, whatever step it stopped at.

const (
	caRotationTimeout = 30 * time.Minute
	// caVerifyFor is how long a node is given to answer with the renewed certificates: apid
	// serves a new one shortly after the config is applied.
	caVerifyFor = 45 * time.Second
	// caPatchFor is how long one node's config read and apply is retried.
	caPatchFor = 20 * time.Second
	caSteps    = 5
)

// caHandoverFor bounds the wait for the app to store the transition talosconfig; a variable
// for the tests.
var caHandoverFor = 10 * time.Minute

// Phases reported through TalosCARotationListener.OnProgress, in order.
const (
	caPhaseCheck    = "check"
	caPhaseAccept   = "accept"
	caPhaseHandover = "handover"
	caPhaseSwap     = "swap"
	caPhaseDrop     = "drop"
	caPhaseDone     = "done"
)

// Node states within a phase.
const (
	caNodePending   = "pending"
	caNodePatching  = "patching"
	caNodeVerifying = "verifying"
	caNodeDone      = "done"
	caNodeFailed    = "failed"
)

var (
	errCAHandoverLate = errors.New("the transition talosconfig was not stored in time: stopped before the cluster's issuing CA changed (the new CA is only accepted; the plan counts it among the extra accepted CAs)")
	errCAStopped      = errors.New("stopped: the cluster keeps the CAs it has at this step, and this phone's talosconfig keeps working")
)

// TalosCARotationListener follows a CA rotation (implemented in Kotlin/Swift).
type TalosCARotationListener interface {
	// OnProgress gets {"phase","step","total","node","hostname","message","at","nodes":
	// [{"node","hostname","role","state"}]} on every change: phase is check, accept, handover,
	// swap, drop or done; step counts them from 1; node is the one being patched or checked.
	OnProgress(json string)
	// OnTalosconfig hands over a talosconfig for this context to store in place of the current
	// one (ReplaceContextCredentials): first the transition one (isFinal false), which the run
	// waits to see stored (Proceed) before it goes on; then the final one, with the new CA
	// alone (isFinal true), right before OnDone.
	OnTalosconfig(talosconfigYAML string, isFinal bool)
	// OnDone is called exactly once; errMessage is empty when the CA was rotated.
	OnDone(errMessage string)
}

// TalosCARotationRun is a handle on a running rotation.
type TalosCARotationRun struct {
	cancel  context.CancelFunc
	proceed chan struct{}
}

// Proceed tells the run the transition talosconfig is stored: the issuing CA can change.
func (r *TalosCARotationRun) Proceed() {
	select {
	case r.proceed <- struct{}{}:
	default:
	}
}

// Cancel stops the run before its next step; the cluster keeps the CAs it has then, and the
// phone's talosconfig (the stored one, transition or not) keeps working.
func (r *TalosCARotationRun) Cancel() { r.cancel() }

type caProgressNode struct {
	Node     string `json:"node"`
	Hostname string `json:"hostname"`
	Role     string `json:"role"`
	State    string `json:"state"`
}

type caRotationProgress struct {
	Phase    string           `json:"phase"`
	Step     int              `json:"step"`
	Total    int              `json:"total"`
	Node     string           `json:"node"`
	Hostname string           `json:"hostname"`
	Message  string           `json:"message"`
	At       int64            `json:"at"`
	Nodes    []caProgressNode `json:"nodes"`
}

// dialTalosconfig opens a client for a talosconfig (the tests point it at the fake).
var dialTalosconfig = func(ctx context.Context, cfg *clientconfig.Config) (*client.Client, error) {
	return client.New(ctx, client.WithConfig(cfg))
}

// StartTalosCARotation rotates the context's Talos API CA (os:admin), in the order of
// `talosctl rotate-ca --kubernetes=false`: the plan is checked again (refused when not ready),
// the new CA is added to every node's accepted CAs (control planes first), every node is
// checked with a certificate of the new CA, the transition talosconfig is handed to the app
// (OnTalosconfig) and the run waits for Proceed, the new CA becomes the issuing one, every
// node is checked with the new CA alone, the old CA is dropped, every node is checked once
// more, and the final talosconfig is handed over. The new client certificate is os:admin,
// valid a year (renew it from Issue talosconfig). Recorded in the activity log as
// talos-ca-rotate (the step reached, never a key). Unavailable in the demo.
func StartTalosCARotation(configYAML, contextName string, listener TalosCARotationListener) *TalosCARotationRun {
	contextName = unmaskContext(configYAML, contextName)

	listener = maskedCARotationListener{listener}

	ctx, cancel := context.WithTimeout(context.Background(), caRotationTimeout)
	run := &TalosCARotationRun{cancel: cancel, proceed: make(chan struct{}, 1)}

	r := &caRotation{config: configYAML, context: contextName, listener: listener, proceed: run.proceed}

	go func() {
		defer cancel()
		defer onPanic(listener.OnDone)
		defer holdCalls(configYAML, contextName)()

		var err error

		if isDemoContext(configYAML, contextName) {
			err = errDemoUnavailable
		} else {
			err = recordedRun(configYAML, contextName, r.auditAction, func() error { return r.run(ctx) })
		}

		listener.OnDone(errText(err))
	}()

	return run
}

// caRotation is one run of StartTalosCARotation.
type caRotation struct {
	config, context string
	listener        TalosCARotationListener
	proceed         chan struct{}

	name   string                // the context's name, as the talosconfig spells it
	source *clientconfig.Context // the context as it was: its endpoints and nodes
	nodes  []caRotationNode
	state  []string // the nodes' states, in nodes order
	phase  string

	currentCA []byte // the issuing CA before the rotation, PEM
	newCA     *x509.PEMEncodedCertificateAndKey
	clientCrt *x509.PEMEncodedCertificateAndKey

	transition *client.Client // new client certificate, both CAs trusted
	final      *client.Client // new client certificate, the new CA alone
}

func (r *caRotation) auditAction() auditAction {
	return auditAction{Action: "talos-ca-rotate", Params: fmt.Sprintf("nodes=%d step=%s", len(r.nodes), r.phase)}
}

func (r *caRotation) run(ctx context.Context) error {
	_, err := withSessionFrom(ctx, r.config, r.context, caRotationTimeout, func(ctx context.Context, s *session) (struct{}, error) {
		return struct{}{}, r.rotate(ctx, s)
	})

	return err
}

func (r *caRotation) rotate(ctx context.Context, s *session) error {
	name, _, err := resolveContext(r.config, r.context)
	if err != nil {
		return err
	}

	r.name, r.source = name, cloneContext(s.context)

	if isOmni(s.context) {
		return errOmniRotateCA
	}

	defer r.closeClients()

	if err := r.check(ctx, s); err != nil {
		return err
	}

	if err := r.generate(ctx, s); err != nil {
		return err
	}

	steps := []struct {
		phase  string
		action func(context.Context, *session) error
	}{
		{caPhaseAccept, r.accept},
		{caPhaseHandover, r.handover},
		{caPhaseSwap, r.swap},
		{caPhaseDrop, r.drop},
	}

	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return errCAStopped
		}

		r.phase = step.phase
		r.resetStates()

		if err := step.action(ctx, s); err != nil {
			if ctx.Err() != nil && !errors.Is(err, errCAHandoverLate) {
				return errCAStopped
			}

			return err
		}
	}

	r.phase = caPhaseDone

	yaml, err := r.talosconfig(r.newCA.Crt)
	if err != nil {
		return err
	}

	r.listener.OnTalosconfig(yaml, true)
	r.emit("", "the Talos API CA is rotated: the old one is no longer trusted by any node")

	return nil
}

// check reads the plan again and refuses to start on a blocker.
func (r *caRotation) check(ctx context.Context, s *session) error {
	r.phase = caPhaseCheck
	r.emit("", "reading the cluster's CA and nodes")

	in := gatherCARotation(ctx, s, r.name)
	plan := computeCARotationPlan(in)

	r.nodes = slices.Clone(plan.Nodes)
	r.resetStates()

	if !plan.Ready {
		return joinBlockers(plan.Blockers)
	}

	r.currentCA = in.caPEM

	// Control planes first: trustd on them issues the workers' certificates.
	slices.SortStableFunc(r.nodes, func(a, b caRotationNode) int {
		return boolCompare(b.Role == "controlplane", a.Role == "controlplane")
	})

	r.emit("", fmt.Sprintf("%d node(s) answer; checking each with the current certificate", len(r.nodes)))

	return r.verify(ctx, s.client, "the current certificate")
}

func boolCompare(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

// generate makes the new CA, the new client certificate and the clients using them.
func (r *caRotation) generate(ctx context.Context, s *session) error {
	now := time.Now()

	ca, err := secrets.NewTalosCA(now)
	if err != nil {
		return fmt.Errorf("generate the new CA: %w", err)
	}

	r.newCA = x509.NewCertificateAndKeyFromCertificateAuthority(ca)

	r.clientCrt, err = secrets.NewAdminCertificateAndKey(now, r.newCA, role.MakeSet(role.Admin), constants.TalosAPIDefaultCertificateValidityDuration)
	if err != nil {
		return fmt.Errorf("generate the new client certificate: %w", err)
	}

	transition := clientconfig.NewConfig(r.name, s.context.Endpoints, r.caBundle(), r.clientCrt)
	final := clientconfig.NewConfig(r.name, s.context.Endpoints, r.newCA.Crt, r.clientCrt)

	// The talosconfigs the app will hold keep the cluster's fingerprint and id.
	fingerprint, id := contextFingerprint(r.name, s.context), clusterID(s.context)
	for _, cfg := range []*clientconfig.Config{transition, final} {
		recordFingerprintLineage(rawContextFingerprint(r.name, cfg.Contexts[r.name]), fingerprint)
		recordFingerprintLineage(rawClusterID(cfg.Contexts[r.name]), id)
	}

	if r.transition, err = dialTalosconfig(ctx, transition); err != nil {
		return fmt.Errorf("prepare the transition client: %w", err)
	}

	if r.final, err = dialTalosconfig(ctx, final); err != nil {
		return fmt.Errorf("prepare the new client: %w", err)
	}

	return nil
}

// caBundle is the current CA followed by the new one: what a client trusts while servers
// move from one to the other.
func (r *caRotation) caBundle() []byte {
	bundle := bytes.Clone(r.currentCA)
	if !bytes.HasSuffix(bundle, []byte("\n")) {
		bundle = append(bundle, '\n')
	}

	return append(bundle, r.newCA.Crt...)
}

// talosconfig is the single-context talosconfig the app stores, with caPEM trusted: the
// context's endpoints and nodes, read before the rotation (no session is opened with the old
// credentials once the nodes stop serving certificates of the old CA).
func (r *caRotation) talosconfig(caPEM []byte) (string, error) {
	return buildTalosconfig(r.name, r.source, caPEM, r.clientCrt.Crt, r.clientCrt.Key)
}

func (r *caRotation) closeClients() {
	for _, c := range []*client.Client{r.transition, r.final} {
		if c != nil {
			_ = c.Close() //nolint:errcheck
		}
	}
}

// accept adds the new CA to every node's accepted CAs, then checks each node answers a
// certificate of the new CA.
func (r *caRotation) accept(ctx context.Context, s *session) error {
	err := r.patchAll(ctx, s.client, "accepting the new CA", func(c *v1alpha1.Config, _ bool) {
		if !hasCA(c.MachineConfig.MachineAcceptedCAs, r.newCA.Crt) {
			c.MachineConfig.MachineAcceptedCAs = append(c.MachineConfig.MachineAcceptedCAs, &x509.PEMEncodedCertificate{Crt: r.newCA.Crt})
		}
	})
	if err != nil {
		return err
	}

	return r.verify(ctx, r.transition, "a certificate of the new CA")
}

// handover gives the app the transition talosconfig and waits until it is stored.
func (r *caRotation) handover(ctx context.Context, _ *session) error {
	yaml, err := r.talosconfig(r.caBundle())
	if err != nil {
		return err
	}

	r.emit("", "storing the transition talosconfig on this phone")
	r.listener.OnTalosconfig(yaml, false)

	timer := time.NewTimer(caHandoverFor)
	defer timer.Stop()

	select {
	case <-r.proceed:
		return nil
	case <-timer.C:
		return errCAHandoverLate
	case <-ctx.Done():
		return errCAStopped
	}
}

// swap makes the new CA the issuing one, the old CA staying accepted, then checks each
// node with the new CA alone.
func (r *caRotation) swap(ctx context.Context, _ *session) error {
	err := r.patchAll(ctx, r.transition, "making the new CA the issuing one", func(c *v1alpha1.Config, controlPlane bool) {
		accepted := slices.DeleteFunc(slices.Clone(c.MachineConfig.MachineAcceptedCAs), func(ca *x509.PEMEncodedCertificate) bool {
			return bytes.Equal(ca.Crt, r.newCA.Crt)
		})
		if !hasCA(accepted, r.currentCA) {
			accepted = append(accepted, &x509.PEMEncodedCertificate{Crt: r.currentCA})
		}

		c.MachineConfig.MachineAcceptedCAs = accepted

		if controlPlane {
			c.MachineConfig.MachineCA = &x509.PEMEncodedCertificateAndKey{Crt: r.newCA.Crt, Key: r.newCA.Key}
		} else {
			c.MachineConfig.MachineCA = &x509.PEMEncodedCertificateAndKey{Crt: r.newCA.Crt}
		}
	})
	if err != nil {
		return err
	}

	return r.verify(ctx, r.final, "the new CA alone")
}

// drop removes the old CA from every node's accepted CAs, then checks each node once more.
func (r *caRotation) drop(ctx context.Context, _ *session) error {
	err := r.patchAll(ctx, r.final, "dropping the old CA", func(c *v1alpha1.Config, _ bool) {
		c.MachineConfig.MachineAcceptedCAs = slices.DeleteFunc(slices.Clone(c.MachineConfig.MachineAcceptedCAs), func(ca *x509.PEMEncodedCertificate) bool {
			return bytes.Equal(ca.Crt, r.currentCA)
		})
	})
	if err != nil {
		return err
	}

	return r.verify(ctx, r.final, "the new CA alone")
}

func hasCA(list []*x509.PEMEncodedCertificate, crt []byte) bool {
	return slices.ContainsFunc(list, func(ca *x509.PEMEncodedCertificate) bool { return bytes.Equal(ca.Crt, crt) })
}

// patchAll applies patch to every node's config in turn, without a reboot, control planes
// first; the first failure stops the step.
func (r *caRotation) patchAll(ctx context.Context, c *client.Client, what string, patch func(c *v1alpha1.Config, controlPlane bool)) error {
	for i, n := range r.nodes {
		r.state[i] = caNodePatching
		r.emit(n.Node, what+" on "+n.Hostname)

		if err := r.patchNode(ctx, c, n, patch); err != nil {
			r.state[i] = caNodeFailed
			r.emit(n.Node, what+" on "+n.Hostname+" failed")

			return fmt.Errorf("%s on %s: %w", what, caNodeLabel(n), err)
		}

		r.state[i] = caNodeDone
	}

	r.emit("", what+": every node applied it")

	return nil
}

// patchNode reads the node's config, patches it and applies it (NO_REBOOT), retrying while
// the node is unavailable: apid may be reloading its certificates.
func (r *caRotation) patchNode(ctx context.Context, c *client.Client, n caRotationNode, patch func(c *v1alpha1.Config, controlPlane bool)) error {
	deadline := time.Now().Add(caPatchFor)

	for {
		err := patchNodeConfigOnce(ctx, c, n.Node, func(cfg *v1alpha1.Config) error {
			patch(cfg, n.Role == "controlplane")

			return nil
		})
		if err == nil {
			return nil
		}

		if !isRetryable(err) || time.Now().After(deadline) {
			return errors.New(friendlyError(err))
		}

		sleepCtx(ctx, upgradePollInterval)

		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func patchNodeConfigOnce(ctx context.Context, c *client.Client, node string, patch func(*v1alpha1.Config) error) error {
	ctx, cancel := context.WithTimeout(client.WithNode(ctx, node), callTimeout)
	defer cancel()

	mc, err := safe.StateGetByID[*config.MachineConfig](ctx, c.COSI, config.ActiveID)
	if err != nil {
		return err
	}

	patched, err := mc.Provider().PatchV1Alpha1(patch)
	if err != nil {
		return fmt.Errorf("patch the config: %w", err)
	}

	data, err := patched.EncodeBytes(encoder.WithComments(encoder.CommentsDisabled))
	if err != nil {
		return fmt.Errorf("encode the config: %w", err)
	}

	_, err = c.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{Data: data, Mode: machineapi.ApplyConfigurationRequest_NO_REBOOT})

	return err
}

// verify checks every node answers c (a client with the given credentials), each within
// caVerifyFor.
func (r *caRotation) verify(ctx context.Context, c *client.Client, with string) error {
	for i, n := range r.nodes {
		r.state[i] = caNodeVerifying
		r.emit(n.Node, "checking "+n.Hostname+" with "+with)

		if err := r.verifyNode(ctx, c, n.Node); err != nil {
			r.state[i] = caNodeFailed
			r.emit(n.Node, n.Hostname+" does not answer with "+with)

			return fmt.Errorf("%s does not answer with %s: %w", caNodeLabel(n), with, err)
		}

		r.state[i] = caNodeDone
	}

	r.emit("", "every node answers with "+with)

	return nil
}

func (r *caRotation) verifyNode(ctx context.Context, c *client.Client, node string) error {
	deadline := time.Now().Add(caVerifyFor)

	for {
		nodeCtx, cancel := context.WithTimeout(client.WithNode(ctx, node), nodeTimeout)
		_, err := c.Version(nodeCtx)
		cancel()

		if err == nil {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if time.Now().After(deadline) {
			return errors.New(friendlyError(err))
		}

		sleepCtx(ctx, upgradePollInterval)

		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

// isRetryable tells a transient failure (the node reloading, the connection dropped) from a
// refusal.
func isRetryable(err error) bool {
	code := client.StatusCode(err)

	return code == codes.Unavailable || code == codes.Canceled || code == codes.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded)
}

func (r *caRotation) resetStates() {
	r.state = make([]string, len(r.nodes))
	for i := range r.state {
		r.state[i] = caNodePending
	}
}

// emit reports the run's state; node is the one being worked on ("" between nodes).
func (r *caRotation) emit(node, message string) {
	p := caRotationProgress{Phase: r.phase, Step: caStep(r.phase), Total: caSteps, Node: node, Message: message, At: time.Now().UnixMilli(), Nodes: make([]caProgressNode, len(r.nodes))}

	for i, n := range r.nodes {
		state := caNodePending
		if i < len(r.state) {
			state = r.state[i]
		}

		p.Nodes[i] = caProgressNode{Node: n.Node, Hostname: n.Hostname, Role: n.Role, State: state}

		if n.Node == node {
			p.Hostname = n.Hostname
		}
	}

	emitJSON(p, r.listener.OnProgress)
}

func caStep(phase string) int {
	switch phase {
	case caPhaseCheck:
		return 1
	case caPhaseAccept:
		return 2
	case caPhaseHandover:
		return 3
	case caPhaseSwap:
		return 4
	case caPhaseDrop, caPhaseDone:
		return 5
	default:
		return 0
	}
}
