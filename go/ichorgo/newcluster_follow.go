package ichorgo

// The run's second half: reaching the node once the install is under way, following it
// from the maintenance API to the installed Talos, and handing over the cluster's own
// certificate. The config-making half lives in newcluster.go.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc/metadata"
)

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

// dialTargets are the addresses the run knocks on after the config went out: where the
// maintenance API answered, and where the cluster API answers (production: both are the
// address the user typed; only the test fake gives them different ports).
func (c *newCluster) dialTargets() []string {
	targets := []string{c.dial()}
	if c.clusterAddress != "" && c.clusterAddress != c.dial() {
		targets = append(targets, c.clusterAddress)
	}

	return targets
}

// follow waits for the install, then for Talos to report itself ready. While the
// maintenance API still answers (a machine status read naming the node, which only the
// installed system has), the install has not started; once every target goes quiet the
// installed node must come back within newClusterInstallGrace.
func (c *newCluster) follow(ctx context.Context) error {
	targets := c.dialTargets()

	// While the node answers as the maintenance ISO, the install has not started.
	upSince := time.Now()
	installed := false

	for !installed {
		if err := ctx.Err(); err != nil {
			return err
		}

		if c.clusterAnswered(ctx) {
			installed = true

			break
		}

		// The maintenance API answering keeps the install alive (the machine is up,
		// installing); the run gives up only when nothing answered for the grace.
		anyUp := false

		for _, target := range targets {
			if nodeAnsweredTo(ctx, target) {
				anyUp = true

				break
			}
		}

		if anyUp {
			upSince = time.Now()
		} else if time.Since(upSince) > newClusterGraceWait {
			return fmt.Errorf("%s did not come back installed within %s: check its console for install errors and that %s is the right disk",
				c.req.address, newClusterGraceWait, c.req.installDisk)
		}

		sleepCtx(ctx, c.interval)
	}

	c.emit(newClusterPhaseBooting, c.req.hostname+" booted Talos with the cluster's identity; waiting for it to be ready")

	return c.waitReady(ctx)
}

// waitReady polls the cluster API until the node's Talos reports it is running and
// ready (the installed system under its own name reports its stage and status).
func (c *newCluster) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(newClusterReadyWait)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if c.reportedReady(ctx, c.node) {
			c.emit(newClusterPhaseReady, c.req.hostname+" is ready")

			return nil
		}

		// A node that answers its cluster API is coming up: the wait runs from the last
		// answer (the boot itself counts; the unreachable stretch belongs to follow).
		if c.clusterAnswered(ctx) {
			deadline = time.Now().Add(newClusterReadyWait)
		} else if time.Now().After(deadline) {
			return fmt.Errorf("%s booted Talos but never became ready: check its console", c.req.address)
		}

		sleepCtx(ctx, c.interval)
	}
}

// clusterAnswered opens the cluster API with the run's own certificate and asks it a
// Version under its own name: an apid still coming up simply does not answer.
func (c *newCluster) clusterAnswered(ctx context.Context) bool {
	cl, err := c.dialCluster()
	if err != nil {
		return false
	}

	defer cl.Close() //nolint:errcheck

	nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	_, err = cl.Version(withRunNode(nodeCtx, c.node))

	return err == nil
}

// reportedReady opens the cluster API with the run's own os:admin certificate (the way
// every post-install call is made) and asks the node, naming it, for the version and the
// machine status: ready means Talos says its stage is running and its status is ready.
// A node whose apid is not up yet cannot be opened or answered: not ready (yet).
func (c *newCluster) reportedReady(ctx context.Context, node string) bool {
	cl, err := c.dialCluster()
	if err != nil {
		return false
	}

	defer cl.Close() //nolint:errcheck

	// The node's name rides in the metadata of every call (the cluster API routes by
	// it the way apid does).
	nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()
	nodeCtx = withRunNode(nodeCtx, node)

	if _, err := cl.Version(nodeCtx); err != nil {
		return false
	}

	ms, err := safe.StateGetByID[*runtime.MachineStatus](nodeCtx, cl.COSI, runtime.MachineStatusID)

	return err == nil && ms.TypedSpec().Stage == runtime.MachineStageRunning && ms.TypedSpec().Status.Ready
}

// withRunNode puts the node's name in the outgoing metadata: like client.WithNode, on a
// context no dial node carries (an empty metadata is fine to set).
func withRunNode(ctx context.Context, node string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "node", node)
}

// nodeAnsweredTo opens target once and asks it a Version: a target that cannot even be
// opened simply is not answering.
func nodeAnsweredTo(ctx context.Context, address string) bool {
	nodeCtx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	m, err := maintenanceClient(nodeCtx, address)
	if err != nil {
		return false
	}

	defer m.Close() //nolint:errcheck

	return nodeAnswered(nodeCtx, m)
}

// clusterConfig is the client configuration the run follows and bootstraps the cluster
// with: the talosconfig the app stored (a draft run) or the run's own os:admin certificate
// (the legacy path, where the run made the secrets itself).
func (c *newCluster) clusterConfig() (*clientconfig.Config, error) {
	if c.draft != nil && c.storedYAML != "" {
		cfg, err := clientconfig.FromBytes([]byte(c.storedYAML))
		if err != nil {
			return nil, fmt.Errorf("read the stored talosconfig: %s", friendlyError(err))
		}

		// The tests give the fake a different port for the installed cluster API: point
		// the stored config at it, the way the legacy path's talosconfigFor does. In
		// production clusterAddress is empty and the config is used as stored.
		if c.clusterAddress != "" {
			for _, ctx := range cfg.Contexts {
				if port := dialPortOf(c.clusterAddress); port != "" {
					ctx.Endpoints = []string{net.JoinHostPort(c.req.host, port)}
					ctx.Nodes = []string{net.JoinHostPort(c.req.host, port)}
				}
			}
		}

		return cfg, nil
	}

	if c.bundle == nil || c.adminCert == nil {
		return nil, errors.New("the cluster's admin certificate was never issued")
	}

	return talosconfigFor(c.req.clusterName, c.req.host, c.bundle.Certs.OS.Crt, c.adminCert, c.clusterAddress)
}

// dialPortOf is the port of a host:port endpoint, "" when there is none.
func dialPortOf(hostPort string) string {
	if _, port, err := net.SplitHostPort(hostPort); err == nil {
		return port
	}

	return ""
}

// dialCluster opens the cluster with the run's own os:admin certificate: the phone is the
// first client of a cluster it just created.
func (c *newCluster) dialCluster() (*client.Client, error) {
	cfg, err := c.clusterConfig()
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

// kubeconfig asks the control plane for the admin kubeconfig, retrying while etcd and the
// Kubernetes API server come up: the cluster answers it only once etcd has a leader, which
// takes seconds after the bootstrap.
func (c *newCluster) kubeconfig(ctx context.Context, cl *client.Client) ([]byte, error) {
	deadline := time.Now().Add(newClusterReadyWait)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		kubeconfig, err := cl.Kubeconfig(withRunNode(ctx, c.node))
		if err == nil {
			return kubeconfig, nil
		}

		if !isRetryable(err) || time.Now().After(deadline) {
			return nil, fmt.Errorf("the control plane did not give a kubeconfig: %s", friendlyError(err))
		}

		sleepCtx(ctx, c.interval)
	}
}

// handOver issues the admin certificate once and gives the app the talosconfig and
// kubeconfig.
func (c *newCluster) handOver(ctx context.Context, cl *client.Client) error {
	// The app stored the talosconfig before this run: hand back exactly that copy, so what
	// it holds and what it was given never differ. The legacy path (no draft) builds it
	// from the run's own admin certificate instead.
	yaml := []byte(c.storedYAML)
	if c.draft == nil {
		cfg, err := c.clusterConfig()
		if err != nil {
			return err
		}

		if yaml, err = cfg.Bytes(); err != nil {
			return fmt.Errorf("encode talosconfig: %w", err)
		}
	}

	kubeconfig, err := c.kubeconfig(ctx, cl)
	if err != nil {
		return err
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
