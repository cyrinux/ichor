package ichorgo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
)

// Removing a node for good: nothing here touches the machine. The talosconfig context stops
// targeting it, and its Node object goes once its kubelet has stopped. Never automatic: a
// rebooting or cut-off node looks the same as a removed one.

const (
	// departedNodeMaxWait bounds KubeDeleteDepartedNode: a drain rarely takes longer.
	departedNodeMaxWait = 10 * time.Minute
)

// departedNodePoll is how often KubeDeleteDepartedNode looks at the Node again.
var departedNodePoll = 10 * time.Second

// contextNodeRemoval says whether a node can leave a context. Endpoint: the app also connects
// through it, and RemoveContextNodes leaves the endpoints as they are.
type contextNodeRemoval struct {
	Allowed  bool   `json:"allowed"`
	Reason   string `json:"reason,omitempty"`
	Endpoint bool   `json:"endpoint"`
}

// nodeForgetCheck is what NodeForgettable returns.
type nodeForgetCheck struct {
	Forgettable bool `json:"forgettable"`
}

// sameContextNode compares two targets however they are written: bracketed, with a port, in capitals.
func sameContextNode(a, b string) bool {
	return strings.EqualFold(endpointHost(a), endpointHost(b))
}

// removalOf decides for ctx. A context without nodes targets its endpoints, so those count.
func removalOf(ctx *clientconfig.Context, demo bool, nodes []string) contextNodeRemoval {
	targets := targetNodes(ctx)
	out := contextNodeRemoval{Endpoint: slices.ContainsFunc(ctx.Endpoints, func(e string) bool {
		return slices.ContainsFunc(nodes, func(n string) bool { return sameContextNode(e, n) })
	})}

	left := slices.DeleteFunc(slices.Clone(targets), func(t string) bool {
		return slices.ContainsFunc(nodes, func(n string) bool { return sameContextNode(t, n) })
	})

	switch {
	case demo:
		out.Reason = errDemoUnavailable.Error()
	case isOmni(ctx):
		out.Reason = "Omni decides which nodes this cluster has"
	case len(targets)-len(left) != len(nodes):
		out.Reason = "this context does not target that node"
	case len(left) == 0:
		out.Reason = "it is the last node of this context"
	default:
		out.Allowed = true
	}

	return out
}

// splitContextNodes parses a comma-separated list, normalized, dropping blanks and duplicates.
func splitContextNodes(nodes string) []string {
	var out []string

	for _, n := range strings.Split(nodes, ",") {
		n = normalizeEndpoint(n)
		if n != "" && !slices.ContainsFunc(out, func(known string) bool { return sameContextNode(known, n) }) {
			out = append(out, n)
		}
	}

	return out
}

// ContextNodeRemovalPlan says, without any network, whether node can leave contextName's nodes
// and whether it is also an endpoint: see contextNodeRemoval for the JSON.
func ContextNodeRemovalPlan(storedYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(storedYAML, contextName, node)

	stored, err := parseTalosconfig(storedYAML)
	if err != nil {
		return "", fmt.Errorf("stored talosconfig: %w", err)
	}

	target, ok := stored.Contexts[contextName]
	if !ok || target == nil {
		return "", fmt.Errorf("context %q not found in the stored talosconfig", contextName)
	}

	return toJSON(removalOf(target, isTalosDemoContext(storedYAML, contextName), splitContextNodes(node)))
}

// RemoveContextNodes returns storedYAML with nodes (comma-separated) out of contextName's
// nodes. A context without nodes targeted its endpoints: the others become its nodes. The
// endpoints stay. Refused for the last node, a node it does not target, Omni and the demo.
func RemoveContextNodes(storedYAML, contextName, nodes string) (out string, err error) {
	// The result is a talosconfig the app stores: only the error is masked.
	defer maskErr(&err)

	contextName, nodes = unmaskTargets(storedYAML, contextName, nodes)

	defer recordAction(&err, storedYAML, contextName, auditAction{Action: "forget", Node: nodes})

	demo := isTalosDemoContext(storedYAML, contextName)
	list := splitContextNodes(nodes)

	return editContext(storedYAML, contextName, func(c *clientconfig.Context) error {
		removal := removalOf(c, demo, list)
		if !removal.Allowed {
			return errors.New("cannot remove the node: " + removal.Reason)
		}

		c.Nodes = slices.DeleteFunc(targetNodes(c), func(t string) bool {
			return slices.ContainsFunc(list, func(n string) bool { return sameContextNode(t, n) })
		})

		return nil
	})
}

// NodeForgettable says whether node can be offered "Forget node" (os:reader): see
// isForgettable. A failed read of the members, or discovery being off, says no.
func NodeForgettable(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	if isDemoContext(configYAML, contextName) {
		return toJSON(nodeForgetCheck{})
	}

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		targets := targetNodes(s.context)
		if isOmni(s.context) || !slices.ContainsFunc(targets, func(t string) bool { return sameContextNode(t, node) }) {
			return toJSON(nodeForgetCheck{})
		}

		hostnames := targetHostnames(ctx, s.client, targets)

		members, err := listMembers(ctx, s.client, targets)
		if err != nil {
			return toJSON(nodeForgetCheck{})
		}

		return toJSON(nodeForgetCheck{Forgettable: isForgettable(node, targets, hostnames, members)})
	})
}

// isForgettable: node does not answer and the cluster no longer lists it. hostnames is each
// target's, "" when it does not answer. A target that answers but is not listed means the list
// is incomplete (a node that just restarted knows only part of the cluster): nothing is then
// forgettable, as a live node could look gone.
func isForgettable(node string, targets, hostnames []string, members []clusterMember) bool {
	answered := 0

	for i, t := range targets {
		if hostnames[i] == "" {
			continue
		}

		if sameContextNode(t, node) || !slices.ContainsFunc(members, func(m clusterMember) bool {
			return strings.EqualFold(m.hostname, hostnames[i])
		}) {
			return false
		}

		answered++
	}

	return answered > 0 && !slices.ContainsFunc(members, func(m clusterMember) bool { return isMember(m, node) })
}

// isMember: node names m, by hostname or by one of its addresses.
func isMember(m clusterMember, node string) bool {
	host := endpointHost(node)
	if strings.EqualFold(m.hostname, host) {
		return true
	}

	return slices.ContainsFunc(m.addresses, func(a netip.Addr) bool { return strings.EqualFold(a.String(), host) })
}

// KubeDeleteDepartedNode deletes the Node object of a node being reset, once its kubelet has
// stopped: deleted earlier, the kubelet registers it again. The reset call returns while the
// node still drains, so this waits, up to timeoutSeconds, until the Node is no longer Ready.
// The Node is found by name (hostname) or address; none means it is already gone. Refused in
// the demo.
func KubeDeleteDepartedNode(configYAML, contextName, kubeServer, hostname, address string, timeoutSeconds int) (err error) {
	defer maskErr(&err)

	contextName, address = unmaskTarget(configYAML, contextName, address)
	hostname = privacy.reveal(strings.TrimSpace(hostname))

	defer recordAction(&err, configYAML, contextName, auditAction{
		Action: "delete", Node: address, Object: "nodes/" + hostname, Params: "departed",
	})

	wait := min(time.Duration(timeoutSeconds)*time.Second, departedNodeMaxWait)
	deadline := time.Now().Add(wait)
	target := kubeTarget{configYAML, contextName, kubeServer}

	for {
		done := false

		err := kubeMutate(target, func(ctx context.Context, k *kubeClient) error {
			var stepErr error

			done, stepErr = deleteIfDeparted(ctx, k, hostname, address)

			return stepErr
		})

		switch {
		case err == nil && done:
			return nil
		case errors.Is(err, errDemoUnavailable), isForbidden(err):
			return err
		case !time.Now().Before(deadline):
			if err != nil {
				return err
			}

			return fmt.Errorf("the kubelet of %s still answers: delete its Node once the reset has finished", hostname)
		}

		time.Sleep(departedNodePoll)
	}
}

// deleteIfDeparted deletes the node's Node when it is not Ready; true when no Node is left.
func deleteIfDeparted(ctx context.Context, k *kubeClient, hostname, address string) (bool, error) {
	objs, err := listObjects[kubeNodeObject](ctx, k, "/api/v1/nodes")
	if err != nil {
		return false, err
	}

	i := slices.IndexFunc(objs, func(o kubeNodeObject) bool { return namesKubeNode(o, hostname, address) })
	if i < 0 {
		return true, nil
	}

	if objs[i].Status.Conditions.is("Ready") {
		return false, nil
	}

	body, err := deleteOptions("Background", "", -1)
	if err != nil {
		return false, err
	}

	err = k.do(ctx, http.MethodDelete, "/api/v1/nodes/"+url.PathEscape(objs[i].Metadata.Name), "application/json", body, nil)
	if err != nil && kubeCode(err) != http.StatusNotFound {
		return false, err
	}

	return true, nil
}

// namesKubeNode: the Node is called hostname, or has address among its addresses.
func namesKubeNode(o kubeNodeObject, hostname, address string) bool {
	if hostname != "" && strings.EqualFold(o.Metadata.Name, hostname) {
		return true
	}

	host := endpointHost(address)

	return host != "" && slices.ContainsFunc(o.Status.Addresses, func(a struct {
		Type    string `json:"type"`
		Address string `json:"address"`
	}) bool {
		return strings.EqualFold(a.Address, host)
	})
}
