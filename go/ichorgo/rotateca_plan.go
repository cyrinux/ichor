package ichorgo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/secrets"
)

// Rotating the Talos API CA from the phone, like `talosctl rotate-ca --kubernetes=false`:
// this file is the plan (what the rotation touches, what stands in its way), rotateca_run.go
// the run. The Kubernetes CA is left alone: kubeconfig identities keep working, and
// `talosctl rotate-ca --talos=false` covers it from a computer.

const (
	// caRotateMinVersion: machine.acceptedCAs, which lets a node trust two CAs while the
	// rotation goes on, arrived with Talos 1.7.
	caRotateMinVersion = "1.7"
	// caFingerprintLength is how much of a CA's SHA-256 (hex) the plan shows.
	caFingerprintLength = 16
)

var (
	errOmniRotateCA = errors.New("this cluster is managed by Omni, which holds its CA: rotate it from Omni")
	errCANoNodes    = errors.New("no node to rotate: the context names none and cluster discovery lists none")
)

type caRotationCA struct {
	Subject     string `json:"subject"`
	NotBefore   int64  `json:"notBefore"` // unix seconds
	NotAfter    int64  `json:"notAfter"`
	Fingerprint string `json:"fingerprint"`
}

// caRotationNode is one node the rotation patches.
type caRotationNode struct {
	Node      string `json:"node"`
	Hostname  string `json:"hostname"`
	Role      string `json:"role"` // controlplane, worker, or unknown when it did not answer
	Version   string `json:"version"`
	Reachable bool   `json:"reachable"`
	// AcceptedCAs counts the CAs the node accepts besides its issuing one (a rotation left
	// unfinished, or an admin's own); they are kept as they are.
	AcceptedCAs int `json:"acceptedCAs"`
	// Problem says why this node stands in the way ("" when it does not).
	Problem string `json:"problem,omitempty"`
}

type caRotationPlan struct {
	Context   string           `json:"context"`
	Endpoints []string         `json:"endpoints"`
	CA        *caRotationCA    `json:"ca,omitempty"`
	Nodes     []caRotationNode `json:"nodes"`
	// Discovered: the nodes are every member cluster discovery lists, not only the context's.
	Discovered bool     `json:"discovered"`
	Blockers   []string `json:"blockers"`
	Warnings   []string `json:"warnings"`
	Ready      bool     `json:"ready"`
}

// caRotationInput is what the cluster answered, before the plan is computed from it.
type caRotationInput struct {
	context    string
	endpoints  []string
	nodes      []caRotationNode
	discovered bool
	ca         *caRotationCA
	caPEM      []byte
	caErr      string
}

// TalosCARotationPlan tells what rotating the context's Talos API CA would do:
// {"context","endpoints","ca":{"subject","notBefore","notAfter","fingerprint"},
// "nodes":[{"node","hostname","role","version","reachable","acceptedCAs","problem"}],
// "discovered","blockers","warnings","ready"} (os:admin: the CA and each node's config are
// read). The nodes are every member cluster discovery lists, else the context's own. A node
// that does not answer, or runs Talos before 1.7, blocks the rotation: left out, it would
// stop trusting this phone. Refused for an Omni cluster, whose CA Omni holds.
func TalosCARotationPlan(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isTalosDemoContext(configYAML, contextName) {
		return demoCARotationPlan(configYAML, contextName)
	}

	name, cfgCtx, err := resolveContext(configYAML, contextName)
	if err != nil {
		return "", err
	}

	if isOmni(cfgCtx) {
		return "", errOmniRotateCA
	}

	return withSession(configYAML, contextName, planTimeout, func(ctx context.Context, s *session) (string, error) {
		in := gatherCARotation(ctx, s, name)

		return toJSON(computeCARotationPlan(in))
	})
}

// gatherCARotation reads the nodes, their versions and configs, and the current CA.
func gatherCARotation(ctx context.Context, s *session, name string) caRotationInput {
	in := caRotationInput{context: name, endpoints: slices.Clone(s.context.Endpoints)}
	in.nodes, in.discovered = caRotationTargets(ctx, s)

	forEachNode(in.nodes, func(i int, n caRotationNode) {
		in.nodes[i] = describeCARotationNode(ctx, s, n)
	})

	// Every control plane's issuing CA: they differ after a rotation stopped half-way.
	var cps []caRotationNode

	for _, n := range in.nodes {
		if n.Reachable && n.Role == "controlplane" {
			cps = append(cps, n)
		}
	}

	issuing := make([]issuingCA, len(cps))

	forEachNode(cps, func(i int, n caRotationNode) {
		ca, crt, problem := readIssuingCA(ctx, s, n.Node)
		issuing[i] = issuingCA{node: n, ca: ca, pem: crt, problem: problem}
	})

	in.ca, in.caPEM, in.caErr = agreeOnIssuingCA(issuing)

	return in
}

// issuingCA is the CA one control plane issues certificates with.
type issuingCA struct {
	node    caRotationNode
	ca      *caRotationCA
	pem     []byte
	problem string
}

// agreeOnIssuingCA is the CA every control plane issues with, or why there is none: no
// control plane answered, one could not be read, or they issue with different CAs (a
// rotation stopped while making the new CA the issuing one, which the phone cannot finish:
// its next check would fail on the nodes still serving the other CA).
func agreeOnIssuingCA(issuing []issuingCA) (*caRotationCA, []byte, string) {
	if len(issuing) == 0 {
		return nil, nil, errNoControlPlane.Error()
	}

	first := issuing[0]
	if first.problem != "" {
		return nil, nil, first.problem
	}

	for _, other := range issuing[1:] {
		if other.problem != "" {
			return nil, nil, other.problem
		}

		if !bytes.Equal(other.pem, first.pem) {
			return nil, nil, fmt.Sprintf("the control planes issue with different CAs (%s: %s, %s: %s): a rotation stopped half-way. "+
				"Finish it from a computer with talosctl rotate-ca, or put the same machine.ca back on every control plane",
				caNodeLabel(first.node), first.ca.Fingerprint, caNodeLabel(other.node), other.ca.Fingerprint)
		}
	}

	return first.ca, first.pem, ""
}

// caRotationTargets lists the nodes to patch: cluster discovery's members at the address the
// context already uses when it does, else the context's targets alone.
func caRotationTargets(ctx context.Context, s *session) ([]caRotationNode, bool) {
	targets := targetNodes(s.context)

	members, err := listMembers(ctx, s.client, targets)
	if err != nil || len(members) == 0 {
		nodes := make([]caRotationNode, 0, len(targets))
		for _, t := range targets {
			nodes = append(nodes, caRotationNode{Node: t})
		}

		return nodes, false
	}

	nodes := make([]caRotationNode, 0, len(members))
	hostnames := map[string]bool{}

	for _, m := range members {
		if len(m.addresses) == 0 {
			continue
		}

		node := m.addresses[0].String()

		for _, a := range m.addresses {
			if slices.Contains(targets, a.String()) {
				node = a.String()

				break
			}
		}

		hostnames[m.hostname] = true
		nodes = append(nodes, caRotationNode{Node: node, Hostname: m.hostname, Role: m.role})
	}

	// A target discovery does not list (discovery disabled on it) is rotated too; one whose
	// hostname is a member's (a DNS name, a VIP) is that member.
	known := func(t string) bool {
		return slices.ContainsFunc(nodes, func(n caRotationNode) bool { return n.Node == t })
	}

	for i, host := range targetHostnames(ctx, s.client, targets) {
		if t := targets[i]; !known(t) && (host == "" || !hostnames[host]) {
			nodes = append(nodes, caRotationNode{Node: t, Hostname: host})
		}
	}

	return nodes, true
}

// describeCARotationNode adds what the node itself says: version, role, hostname, the CAs
// its config accepts.
func describeCARotationNode(ctx context.Context, s *session, n caRotationNode) caRotationNode {
	n.Version = s.nodeVersion(ctx, n.Node)
	n.Reachable = n.Version != ""

	if !n.Reachable {
		n.Problem = "does not answer: every node must be reachable, or it would stop trusting this phone"

		if n.Role == "" {
			n.Role = roleUnknown
		}

		return n
	}

	nodeCtx, cancel := context.WithTimeout(client.WithNode(ctx, n.Node), nodeTimeout)
	defer cancel()

	if n.Role == "" || n.Role == roleUnknown {
		mt, err := safe.StateGetByID[*config.MachineType](nodeCtx, s.client.COSI, config.MachineTypeID)
		n.Role = roleName(mt.MachineType(), err)
	}

	if n.Hostname == "" {
		if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, s.client.COSI, network.HostnameID); err == nil {
			n.Hostname = hs.TypedSpec().Hostname
		}
	}

	if n.Hostname == "" {
		n.Hostname = n.Node
	}

	if compareMinor(n.Version, caRotateMinVersion) < 0 {
		n.Problem = fmt.Sprintf("runs Talos %s: the rotation needs %s or newer on every node", n.Version, caRotateMinVersion)

		return n
	}

	mc, err := safe.StateGetByID[*config.MachineConfig](nodeCtx, s.client.COSI, config.ActiveID)
	if err != nil {
		n.Problem = "its config cannot be read: " + friendlyError(err)

		return n
	}

	n.AcceptedCAs = len(mc.Provider().Machine().Security().AcceptedCAs())

	return n
}

// readIssuingCA reads the CA the control plane node issues certificates with.
func readIssuingCA(ctx context.Context, s *session, node string) (*caRotationCA, []byte, string) {
	nodeCtx, cancel := context.WithTimeout(client.WithNode(ctx, node), nodeTimeout)
	defer cancel()

	root, err := safe.StateGetByID[*secrets.OSRoot](nodeCtx, s.client.COSI, secrets.OSRootID)
	if err != nil {
		return nil, nil, "the cluster's CA cannot be read: " + friendlyError(err)
	}

	issuing := root.TypedSpec().IssuingCA
	if issuing == nil || len(issuing.Crt) == 0 {
		return nil, nil, "the cluster's CA cannot be read: the node reports none"
	}

	ca, err := describeCA(issuing.Crt)
	if err != nil {
		return nil, nil, "the cluster's CA cannot be read: " + err.Error()
	}

	return ca, issuing.Crt, ""
}

// describeCA reads a PEM certificate's subject, validity and fingerprint.
func describeCA(certPEM []byte) (*caRotationCA, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("not a PEM certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid certificate: %w", err)
	}

	sum := sha256.Sum256(block.Bytes)

	return &caRotationCA{
		Subject:     cert.Subject.String(),
		NotBefore:   cert.NotBefore.Unix(),
		NotAfter:    cert.NotAfter.Unix(),
		Fingerprint: hex.EncodeToString(sum[:])[:caFingerprintLength],
	}, nil
}

// computeCARotationPlan turns what the cluster said into blockers and warnings.
func computeCARotationPlan(in caRotationInput) caRotationPlan {
	plan := caRotationPlan{
		Context: in.context, Endpoints: in.endpoints, CA: in.ca, Nodes: in.nodes, Discovered: in.discovered,
		Blockers: []string{}, Warnings: []string{},
	}

	if plan.Nodes == nil {
		plan.Nodes = []caRotationNode{}
	}

	if len(in.nodes) == 0 {
		plan.Blockers = append(plan.Blockers, errCANoNodes.Error())
	}

	if in.caErr != "" {
		plan.Blockers = append(plan.Blockers, in.caErr)
	}

	extra := 0

	for _, n := range in.nodes {
		if n.Problem != "" {
			plan.Blockers = append(plan.Blockers, caNodeLabel(n)+" "+n.Problem)
		}

		extra += n.AcceptedCAs
	}

	if len(in.nodes) > 0 && !in.discovered {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("Cluster discovery did not answer: only the %d node(s) this talosconfig names are rotated. A node missing from the list keeps the old CA and stops trusting this phone.", len(in.nodes)))
	}

	plan.Warnings = append(plan.Warnings,
		"Every other talosconfig for this cluster (other devices, talosctl on your computer) stops working once the old CA is dropped: issue new ones from this phone afterwards.",
		"Machine configs generated later from the old secrets bundle (talosctl gen config) carry the old CA: generate them from a node's config instead, as Add a node does.",
	)

	if extra > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d CA(s) accepted besides the issuing one stay as they are.", extra))
	}

	plan.Ready = len(plan.Blockers) == 0

	return plan
}

// caNodeLabel names a node for a message: its hostname and address.
func caNodeLabel(n caRotationNode) string {
	if n.Hostname == "" || n.Hostname == n.Node {
		return n.Node
	}

	return n.Hostname + " (" + n.Node + ")"
}

// joinBlockers is the blockers as one error.
func joinBlockers(blockers []string) error {
	return errors.New("the rotation cannot start: " + strings.Join(blockers, "; "))
}
