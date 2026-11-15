package ichorgo

// Step 5 of the Talos lifecycle, the running half: the draft NewClusterPrepare made goes to
// the machine in maintenance mode (`talosctl apply-config --insecure`), the run follows it
// with the talosconfig the app already stored, bootstraps etcd once (`talosctl bootstrap`)
// and waits for etcd and the Kubernetes API. The order is the safety contract: store the
// talosconfig first, send second, bootstrap third.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"google.golang.org/grpc/codes"
)

// New cluster phases reported through JoinListener.OnProgress, in order.
const (
	newClusterPhaseSecrets       = "secrets"
	newClusterPhaseApplying      = "applying"
	newClusterPhaseInstalling    = "installing"
	newClusterPhaseBooted        = "booted"
	newClusterPhaseBootstrapping = "bootstrapping"
	newClusterPhaseReady         = "ready"
)

// newClusterBootstrapFor bounds the bootstrap retries while etcd is not ready to take it.
const newClusterBootstrapFor = 3 * time.Minute

var errNewClusterNotStored = errors.New("store this cluster's talosconfig before starting: it is the only copy of the cluster's access")

// StartNewCluster bootstraps the cluster draftID names (NewClusterPrepare) on the machine in
// maintenance mode at address (the IP its console shows; local network or VPN only).
// storedYAML is the draft's talosconfig as the app stored it: the run refuses to send
// anything before it is stored, and follows the machine with it. Phases: secrets →
// applying → installing → booted → bootstrapping → ready; OnDone gets the machine's
// address. The config is sent once; Bootstrap is retried only while etcd is not ready for
// it, never after it succeeded. Bounded by joinTimeout; Cancel after the config was sent
// only stops following. Refused in privacy mode and on the demo; recorded in the activity
// log as new-cluster-bootstrap (name and address, never the config). A run that ends before
// sending keeps the draft for another try.
func StartNewCluster(draftID, address, storedYAML string, listener JoinListener) *JoinRun {
	listener = maskedJoinListener{listener}

	c := &newClusterRun{
		draftID: strings.TrimSpace(draftID), address: strings.TrimSpace(address), stored: storedYAML,
		listener: listener, interval: upgradePollInterval,
	}

	return c.start(joinTimeout)
}

// newClusterRun is one run of StartNewCluster.
type newClusterRun struct {
	draftID, address, stored string
	listener                 JoinListener
	interval                 time.Duration

	draft   *newClusterDraftEntry
	node    string // the machine's host, where it is followed
	applied bool   // the config was sent: nothing can be undone any more
}

func (c *newClusterRun) start(timeout time.Duration) *JoinRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	go func() {
		defer cancel()
		defer onPanic(func(msg string) { c.listener.OnDone("", msg) })

		err := recordedRun("", "", c.auditAction, func() error { return c.run(ctx, timeout) })
		if err != nil {
			if c.draft != nil && !c.applied {
				returnNewClusterDraft(c.draftID, c.draft)
			}

			c.listener.OnDone("", errText(err))

			return
		}

		c.listener.OnDone(c.node, "")
	}()

	return &JoinRun{cancel: cancel}
}

func (c *newClusterRun) auditAction() auditAction {
	params := ""
	if c.draft != nil {
		params = "name=" + c.draft.req.name + " talos=" + c.draft.req.talosVersion + " k8s=" + c.draft.req.kubernetesVersion
	}

	return auditAction{Action: "new-cluster-bootstrap", Node: hostOnly(c.address), Params: params}
}

func (c *newClusterRun) emit(phase, message string) {
	emitJSON(joinProgress{Phase: phase, Message: message, At: time.Now().UnixMilli(), Address: c.node}, c.listener.OnProgress)
}

func (c *newClusterRun) run(ctx context.Context, timeout time.Duration) error {
	if c.address == demoMaintenanceAddress {
		return errDemoUnavailable
	}

	if privacy.isEnabled() {
		return errJoinPrivacy
	}

	draft, err := takeNewClusterDraft(c.draftID)
	if err != nil {
		return err
	}

	c.draft = draft
	c.node = hostOnly(c.address)

	if strings.TrimSpace(c.stored) != strings.TrimSpace(draft.talos) {
		return errNewClusterNotStored
	}

	if err := checkMaintenanceAddress(ctx, c.address); err != nil {
		return err
	}

	c.emit(newClusterPhaseSecrets, "the talosconfig of "+draft.req.name+" is stored on this phone")

	if err := c.apply(ctx); err != nil {
		return c.ended(ctx, err, timeout)
	}

	cl, err := newClusterClient(ctx, c.stored)
	if err != nil {
		return err
	}

	defer cl.Close() //nolint:errcheck

	return c.ended(ctx, c.follow(ctx, cl), timeout)
}

// ended turns the run's context ending into what to say about it.
func (c *newClusterRun) ended(ctx context.Context, err error, timeout time.Duration) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s was not ready within %s: check its console for install errors and that %s is the right disk; the cluster entry is kept",
			c.node, timeout, c.draft.req.installDisk)
	case ctx.Err() != nil && c.applied:
		return errJoinStopped
	case ctx.Err() != nil:
		return errors.New("stopped before the config was sent: nothing changed on the machine")
	}

	return err
}

// apply sends the control-plane config once, to the address the person confirmed.
func (c *newClusterRun) apply(ctx context.Context) error {
	installed, err := asksClientCert(ctx, c.address)
	if err != nil {
		return errors.New(friendlyError(err))
	}

	if installed {
		return errJoinInstalled
	}

	m, err := maintenanceClient(ctx, c.address)
	if err != nil {
		return err
	}

	defer m.Close() //nolint:errcheck

	c.emit(newClusterPhaseApplying, "sending the control-plane config to "+c.address)

	// From here the config may be on the machine, even when the answer never comes back.
	c.applied = true

	if _, err := m.ApplyConfiguration(ctx, &machineapi.ApplyConfigurationRequest{
		Data: []byte(c.draft.full), Mode: machineapi.ApplyConfigurationRequest_AUTO,
	}); err != nil {
		return fmt.Errorf("the machine refused the config: %s", friendlyError(err))
	}

	c.emit(newClusterPhaseInstalling, "installing Talos to "+c.draft.req.installDisk+"; the machine reboots and does not answer meanwhile")

	return nil
}

// newClusterClient opens the new cluster with the stored talosconfig (one context).
func newClusterClient(ctx context.Context, talosconfig string) (*client.Client, error) {
	cfg, err := clientconfig.FromString(talosconfig)
	if err != nil {
		return nil, fmt.Errorf("read the stored talosconfig: %s", friendlyError(err))
	}

	cl, err := client.New(ctx, client.WithConfig(cfg))
	if err != nil {
		return nil, fmt.Errorf("open the new cluster: %s", friendlyError(err))
	}

	return cl, nil
}

// follow waits for the installed machine to answer with the cluster's identity, bootstraps
// etcd, then waits for etcd and the Kubernetes API.
func (c *newClusterRun) follow(ctx context.Context, cl *client.Client) error {
	if err := c.poll(ctx, func(nctx context.Context) (bool, error) {
		_, err := cl.Version(nctx)

		return err == nil, nil
	}); err != nil {
		return err
	}

	c.emit(newClusterPhaseBooted, c.node+" booted with the cluster's identity")
	c.emit(newClusterPhaseBootstrapping, "bootstrapping etcd on "+c.node)

	if err := c.bootstrap(ctx, cl); err != nil {
		return err
	}

	if err := c.poll(ctx, func(nctx context.Context) (bool, error) {
		return c.etcdHasMember(nctx, cl), nil
	}); err != nil {
		return err
	}

	if err := c.poll(ctx, func(nctx context.Context) (bool, error) {
		_, err := cl.Kubeconfig(nctx)

		return err == nil, nil
	}); err != nil {
		return err
	}

	c.emit(newClusterPhaseReady, c.draft.req.name+" is up: etcd has its member and the Kubernetes API answers")

	return nil
}

// poll asks done every interval, the call bounded by nodeTimeout and aimed at the machine,
// until it says yes, fails, or the run ends.
func (c *newClusterRun) poll(ctx context.Context, done func(context.Context) (bool, error)) error {
	for {
		nctx, cancel := context.WithTimeout(client.WithNode(ctx, c.node), nodeTimeout)
		ok, err := done(nctx)

		cancel()

		switch {
		case err != nil:
			return err
		case ok:
			return nil
		}

		sleepCtx(ctx, c.interval)

		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// bootstrap starts etcd once. Talos refuses it while etcd is not ready to take it (retried
// for newClusterBootstrapFor) and after it already ran (a success only when etcd has a
// member: an answer lost on the way back must not fail the run).
func (c *newClusterRun) bootstrap(ctx context.Context, cl *client.Client) error {
	deadline := time.Now().Add(newClusterBootstrapFor)

	return c.poll(ctx, func(nctx context.Context) (bool, error) {
		err := cl.Bootstrap(nctx, &machineapi.BootstrapRequest{})

		switch {
		case err == nil:
			return true, nil
		case client.StatusCode(err) == codes.AlreadyExists && c.etcdHasMember(nctx, cl):
			return true, nil
		case bootstrapNotReady(err) && time.Now().Before(deadline):
			return false, nil
		}

		return false, fmt.Errorf("bootstrap etcd on %s: %s", c.node, friendlyError(err))
	})
}

// bootstrapNotReady: etcd (or apid) is still coming up.
func bootstrapNotReady(err error) bool {
	return isRetryable(err) || client.StatusCode(err) == codes.FailedPrecondition
}

func (c *newClusterRun) etcdHasMember(nctx context.Context, cl *client.Client) bool {
	resp, err := cl.EtcdMemberList(nctx, &machineapi.EtcdMemberListRequest{})
	if err != nil {
		return false
	}

	return len(first(resp.GetMessages()).GetMembers()) > 0
}
