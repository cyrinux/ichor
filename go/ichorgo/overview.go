package ichorgo

import (
	"context"
	"net/netip"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/protobuf/types/known/emptypb"
)

type clusterOverview struct {
	Context string         `json:"context"`
	Nodes   []nodeOverview `json:"nodes"`
}

type nodeOverview struct {
	Node            string           `json:"node"`
	Hostname        string           `json:"hostname"`
	Reachable       bool             `json:"reachable"`
	Error           string           `json:"error,omitempty"`
	ErrorKind       string           `json:"errorKind,omitempty"` // why an unreachable node failed; see errorKind
	Version         string           `json:"version"`
	Arch            string           `json:"arch"`
	Platform        string           `json:"platform"`
	Role            string           `json:"role"`
	Stage           string           `json:"stage"`
	Ready           bool             `json:"ready"`
	UnmetConditions []unmetCondition `json:"unmetConditions"`
	// Capacity for the cluster summary; 0 when the node did not say.
	CPUCount     int    `json:"cpuCount"`
	MemTotal     uint64 `json:"memTotal"`     // bytes
	MemAvailable uint64 `json:"memAvailable"` // bytes
	// Internet-facing addresses, IPv4 first; empty when the node has none or did not say.
	PublicIPs []string `json:"publicIPs,omitempty"`
	// Cordoned: the Kubernetes node is unschedulable, as the node's own k8s.NodeStatus says
	// (no Kubernetes API needed). CordonKnown is false when that status is not there (an older
	// Talos, a node not yet a Kubernetes member): never read it as schedulable then.
	Cordoned    bool `json:"cordoned"`
	CordonKnown bool `json:"cordonKnown"`
}

type unmetCondition struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// nodeProbe holds the raw answers collected from one node.
type nodeProbe struct {
	version     *machineapi.Version
	versionErr  error
	status      *runtime.MachineStatusSpec
	statusErr   error
	machineType machine.Type
	typeErr     error
	hostname    string
	domain      string
	hostnameErr error
	memory      *machineapi.Memory   // nil when unknown
	cpu         *machineapi.CPUsInfo // nil when unknown
	publicIPs   []string
	// KubeSpan peers that are up, by label: the addresses this node reaches them at.
	peerEndpoints map[string][]netip.Addr
	// kube is the node's Kubernetes node as Talos sees it, nil when unknown.
	kube *kubeNodeState
}

// ClusterOverview queries every node of the context in parallel and returns a JSON clusterOverview.
// Unreachable nodes are reported per node instead of failing the whole call.
func ClusterOverview(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	start := time.Now()
	contextName = unmaskContext(configYAML, contextName)

	return clusterOverviewJSON(context.Background(), start, configYAML, contextName, overviewOptions{})
}

// overviewOptions are what a streamed overview changes; the zero value is ClusterOverview.
type overviewOptions struct {
	// onNode, when set, gets each node as soon as it is known (from the probing goroutines,
	// in completion order; the demo's five 100 ms apart).
	onNode func(nodeOverview)
	// versionTimeout, when set, bounds each node's Version (its reachability probe) within
	// nodeTimeout, so a node that does not answer is reported down early. ClusterOverview
	// keeps the full nodeTimeout: the monitor must not call a slow node down.
	versionTimeout time.Duration
}

// clusterOverviewJSON is ClusterOverview (and StartClusterOverview) for an unmasked context.
// Cancelling parent ends the run with its error and skips what the complete overview
// teaches (remembered names, masked hosts).
func clusterOverviewJSON(parent context.Context, start time.Time, configYAML, contextName string, opts overviewOptions) (string, error) {
	onNode := opts.onNode

	defer trace("overview.total", start)

	demo := isDemoContext(configYAML, contextName)

	trace("overview.resolve", start)

	if demo {
		if traceEnabled() {
			traceCounts("overview.all_nodes", start, len(demoNodes()), 0)
		}

		if err := streamDemoNodes(parent, onNode); err != nil {
			return "", err
		}

		return demoRead("ClusterOverview", configYAML, contextName, "")
	}

	return withSessionFrom(parent, configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		trace("overview.session", start)

		name, cfg, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		nodes := targetNodes(s.context)
		result := clusterOverview{Context: name, Nodes: make([]nodeOverview, len(nodes))}
		domains := make([]string, len(nodes))
		peerEndpoints := make([]map[string][]netip.Addr, len(nodes))

		tracedFanout(start, nodes, func(i int, node string) bool {
			p := probeNodeWithin(ctx, s.client, node, opts.versionTimeout)
			result.Nodes[i], domains[i], peerEndpoints[i] = buildNodeOverview(node, p), p.domain, p.peerEndpoints

			s.rememberVersion(node, result.Nodes[i].Version)

			if onNode != nil {
				learnNodeHost(result.Nodes[i], p.domain)
				onNode(result.Nodes[i])
			}

			return result.Nodes[i].Reachable
		})

		if err := parent.Err(); err != nil {
			return "", err
		}

		result.Nodes = withPeerSeenPublicIPs(result.Nodes, peerEndpoints)

		rememberNodeNames(contextFingerprints(configYAML), contextFingerprint(name, cfg), result.Nodes)
		learnClusterHosts(ctx, s.client, result.Nodes, domains)

		return toJSON(result)
	})
}

// probeNode reads one node under nodeTimeout.
func probeNode(ctx context.Context, c *client.Client, node string) nodeProbe {
	return probeNodeWithin(ctx, c, node, 0)
}

// probeNodeWithin is probeNode with its first call (Version, which tells the node is
// reachable) bounded by versionTimeout when set, the rest under what is left of nodeTimeout.
func probeNodeWithin(ctx context.Context, c *client.Client, node string, versionTimeout time.Duration) nodeProbe {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)

	var p nodeProbe

	versionCtx, cancelVersion := nodeCtx, context.CancelFunc(func() {})
	if versionTimeout > 0 {
		versionCtx, cancelVersion = context.WithTimeout(nodeCtx, versionTimeout)
	}

	resp, err := c.Version(versionCtx)

	cancelVersion()

	if err != nil {
		p.versionErr = err

		return p
	}

	if len(resp.GetMessages()) > 0 {
		p.version = resp.GetMessages()[0]
	}

	if ms, err := safe.StateGetByID[*runtime.MachineStatus](nodeCtx, c.COSI, runtime.MachineStatusID); err != nil {
		p.statusErr = err
	} else {
		p.status = ms.TypedSpec()
	}

	if mt, err := safe.StateGetByID[*config.MachineType](nodeCtx, c.COSI, config.MachineTypeID); err != nil {
		p.typeErr = err
	} else {
		p.machineType = mt.MachineType()
	}

	if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, c.COSI, network.HostnameID); err != nil {
		p.hostnameErr = err
	} else {
		p.hostname, p.domain = hs.TypedSpec().Hostname, hs.TypedSpec().Domainname
	}

	// Best effort: a node that cannot say its size is still reported, without it.
	if mem, err := c.Memory(nodeCtx); err == nil {
		p.memory = first(mem.GetMessages())
	}

	if cpu, err := c.MachineClient.CPUInfo(nodeCtx, &emptypb.Empty{}); err == nil {
		p.cpu = first(cpu.GetMessages())
	}

	p.publicIPs = probePublicIPs(nodeCtx, c)
	p.peerEndpoints = probePeerEndpoints(nodeCtx, c)
	p.kube = fetchKubeNodeState(nodeCtx, c)

	return p
}

func buildNodeOverview(node string, p nodeProbe) nodeOverview {
	out := nodeOverview{
		Node:            node,
		Hostname:        node,
		Role:            roleName(p.machineType, p.typeErr),
		Stage:           runtime.MachineStageUnknown.String(),
		UnmetConditions: []unmetCondition{},
	}

	if p.versionErr != nil {
		out.Error = friendlyError(p.versionErr)
		out.ErrorKind = errorKind(p.versionErr)

		return out
	}

	out.Reachable = true

	if v := p.version; v != nil {
		out.Version = v.GetVersion().GetTag()
		out.Arch = v.GetVersion().GetArch()
		out.Platform = v.GetPlatform().GetName()
	}

	if p.hostnameErr == nil && p.hostname != "" {
		out.Hostname = p.hostname
	}

	const kib = 1024

	out.CPUCount = len(p.cpu.GetCpuInfo())
	out.MemTotal = p.memory.GetMeminfo().GetMemtotal() * kib
	out.MemAvailable = p.memory.GetMeminfo().GetMemavailable() * kib
	out.PublicIPs = p.publicIPs

	if p.kube != nil {
		out.Cordoned, out.CordonKnown = p.kube.Unschedulable, true
	}

	var errs []string

	if p.statusErr != nil {
		errs = append(errs, "machine status: "+friendlyError(p.statusErr))
	} else if p.status != nil {
		out.Stage = p.status.Stage.String()
		out.Ready = p.status.Status.Ready

		for _, c := range p.status.Status.UnmetConditions {
			out.UnmetConditions = append(out.UnmetConditions, unmetCondition{Name: c.Name, Reason: c.Reason})
		}
	}

	if p.typeErr != nil {
		errs = append(errs, "machine type: "+friendlyError(p.typeErr))
	}

	out.Error = strings.Join(errs, "; ")

	return out
}

const roleUnknown = "unknown"

func roleName(t machine.Type, err error) string {
	switch {
	case err != nil || t == machine.TypeUnknown:
		return roleUnknown
	case t.IsControlPlane():
		return "controlplane"
	default:
		return "worker"
	}
}
