package talosmobile

import (
	"context"
	"strings"
	"sync"

	"github.com/cosi-project/runtime/pkg/safe"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
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
	Version         string           `json:"version"`
	Arch            string           `json:"arch"`
	Platform        string           `json:"platform"`
	Role            string           `json:"role"`
	Stage           string           `json:"stage"`
	Ready           bool             `json:"ready"`
	UnmetConditions []unmetCondition `json:"unmetConditions"`
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
}

// ClusterOverview queries every node of the context in parallel and returns a JSON clusterOverview.
// Unreachable nodes are reported per node instead of failing the whole call.
func ClusterOverview(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		name, _, err := resolveContext(configYAML, contextName)
		if err != nil {
			return "", err
		}

		nodes := targetNodes(s.context)
		result := clusterOverview{Context: name, Nodes: make([]nodeOverview, len(nodes))}
		domains := make([]string, len(nodes))

		var wg sync.WaitGroup

		for i, node := range nodes {
			wg.Go(func() {
				p := probeNode(ctx, s.client, node)
				result.Nodes[i], domains[i] = buildNodeOverview(node, p), p.domain
			})
		}

		wg.Wait()

		learnClusterHosts(ctx, s.client, result.Nodes, domains)

		return toJSON(result)
	})
}

func probeNode(ctx context.Context, c *client.Client, node string) nodeProbe {
	ctx, cancel := context.WithTimeout(ctx, nodeTimeout)
	defer cancel()

	nodeCtx := client.WithNode(ctx, node)

	var p nodeProbe

	resp, err := c.Version(nodeCtx)
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

func roleName(t machine.Type, err error) string {
	switch {
	case err != nil || t == machine.TypeUnknown:
		return "unknown"
	case t.IsControlPlane():
		return "controlplane"
	default:
		return "worker"
	}
}
