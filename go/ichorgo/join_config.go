package ichorgo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"go.yaml.in/yaml/v4"
)

// Step 2 of joining a node from the phone: the new node's machine config, made from an existing
// member's (with its secrets: the new node must trust the same CAs and hold the same tokens).
// The node-specific parts are stripped (hostname, static addresses, install disk) and the
// user's choices put in. See JoinNodeConfigPreview and JoinNodeConfigSealed.

const (
	joinRoleWorker       = "worker"
	joinRoleControlPlane = "controlplane"
)

var errJoinPrivacy = errors.New("turn privacy mode off to generate a node's config: it carries the cluster's secrets")

// joinConfigRequest is what the user chose for the new node.
type joinConfigRequest struct {
	template, role, hostname, installDisk, staticAddress string
	edits                                                []configEdit
}

// joinConfigPreview is what JoinNodeConfigPreview returns: no secret in it.
type joinConfigPreview struct {
	RedactedYAML string   `json:"redactedYaml"`
	Warnings     []string `json:"warnings"`
	// NeedsBootstrap is always false: the node joins a running cluster (the template's).
	NeedsBootstrap bool   `json:"needsBootstrap"`
	Size           int    `json:"size"` // bytes of the full config sent to the node
	Role           string `json:"role"`
	Hostname       string `json:"hostname"`
	Template       string `json:"template"`
}

// joinRuntime validates a config for a node that installs Talos to a disk (metal, a VM).
type joinRuntime struct{}

func (joinRuntime) String() string        { return "metal" }
func (joinRuntime) RequiresInstall() bool { return true }
func (joinRuntime) InContainer() bool     { return false }

// JoinNodeConfigPreview makes the machine config of a new node from templateNode's (os:admin:
// it reads that config with its secrets) and returns it redacted, with what to look at:
// {"redactedYaml","warnings","needsBootstrap","size","role","hostname","template"}. role is
// worker or controlplane; hostname and installDisk (/dev/sda) are required; staticAddress
// (CIDR, "" for DHCP) replaces the template's addresses on its first interface; extraEditsJSON
// is a JSON array of MachineConfigEdit's edits ("" for none). The config is checked by the
// machinery's own validation; nothing is stored and the full config is not returned here.
func JoinNodeConfigPreview(configYAML, contextName, templateNode, role, hostname, installDisk, staticAddress, extraEditsJSON string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, templateNode = unmaskTarget(configYAML, contextName, templateNode)

	req, err := parseJoinRequest(templateNode, role, hostname, installDisk, staticAddress, extraEditsJSON)
	if err != nil {
		return "", err
	}

	full, warnings, err := joinConfigFor(configYAML, contextName, req)
	if err != nil {
		return "", err
	}

	redactedText, err := redactConfig(full)
	if err != nil {
		return "", err
	}

	return toJSON(joinConfigPreview{
		RedactedYAML: redactedText, Warnings: warnings, Size: len(full),
		Role: req.role, Hostname: req.hostname, Template: req.template,
	})
}

// JoinNodeConfigSealed is JoinNodeConfigPreview's config in full, secrets included, for
// applying it to the new node (os:admin). Refused in privacy mode; each generation is logged
// in the activity log (the template and the hostname, never the config).
func JoinNodeConfigSealed(configYAML, contextName, templateNode, role, hostname, installDisk, staticAddress, extraEditsJSON string) (out string, err error) {
	// Only the errors are masked: the config goes to a node, and privacy mode refuses it first.
	defer maskErr(&err)

	if privacy.isEnabled() {
		return "", errJoinPrivacy
	}

	contextName, templateNode = unmaskTarget(configYAML, contextName, templateNode)

	defer recordAction(&err, configYAML, contextName,
		auditAction{Action: "join-config-generated", Node: templateNode, Params: "hostname=" + strings.TrimSpace(hostname) + " role=" + role})

	req, err := parseJoinRequest(templateNode, role, hostname, installDisk, staticAddress, extraEditsJSON)
	if err != nil {
		return "", err
	}

	full, _, err := joinConfigFor(configYAML, contextName, req)

	return full, err
}

func parseJoinRequest(template, role, hostname, installDisk, staticAddress, editsJSON string) (joinConfigRequest, error) {
	req := joinConfigRequest{
		template: strings.TrimSpace(template), role: strings.TrimSpace(role), hostname: strings.TrimSpace(hostname),
		installDisk: strings.TrimSpace(installDisk), staticAddress: strings.TrimSpace(staticAddress),
	}

	switch {
	case req.template == "":
		return req, errors.New("pick the node whose config is the template")
	case req.role != joinRoleWorker && req.role != joinRoleControlPlane:
		return req, fmt.Errorf("unknown role %q (worker, controlplane)", role)
	case req.hostname == "" || !validHostname(req.hostname) || strings.Contains(req.hostname, "."):
		return req, fmt.Errorf("%q is not a host name (letters, digits and dashes)", hostname)
	case !strings.HasPrefix(req.installDisk, "/dev/"):
		return req, fmt.Errorf("%q is not a disk (/dev/sda, /dev/nvme0n1…)", installDisk)
	}

	if req.staticAddress != "" {
		if _, err := netip.ParsePrefix(req.staticAddress); err != nil {
			return req, fmt.Errorf("%q is not an address with its prefix (192.0.2.20/24)", staticAddress)
		}
	}

	if strings.TrimSpace(editsJSON) != "" {
		if err := json.Unmarshal([]byte(editsJSON), &req.edits); err != nil {
			return req, fmt.Errorf("invalid edits: %w", err)
		}
	}

	return req, nil
}

// joinConfigFor reads the template's config with its secrets and turns it into the new node's.
func joinConfigFor(configYAML, contextName string, req joinConfigRequest) (string, []string, error) {
	if isTalosDemoContext(configYAML, contextName) {
		text, err := demoJoinTemplate(configYAML, contextName, req.template)
		if err != nil {
			return "", nil, err
		}

		return buildJoinConfig(text, req)
	}

	r, err := withNodeSessionFrom(context.Background(), configYAML, contextName, req.template, configPreviewTimeout, func(ctx context.Context, s *session) (joinResult, error) {
		text, err := machineConfigYAML(ctx, s, req.template, true)
		if err != nil {
			return joinResult{}, err
		}

		full, warnings, err := buildJoinConfig(text, req)

		return joinResult{full, warnings}, err
	})

	return r.full, r.warnings, err
}

type joinResult struct {
	full     string
	warnings []string
}

// buildJoinConfig makes the new node's config from the template's text (secrets included):
// node-specific fields out, the request's in, then the machinery's validation.
func buildJoinConfig(templateText string, req joinConfigRequest) (string, []string, error) {
	docs, err := parseConfigDocs(templateText)
	if err != nil {
		return "", nil, fmt.Errorf("the template's config cannot be read: %w", err)
	}

	warnings := []string{}
	var main *yaml.Node

	// Talos 1.12+ configs name the node in a HostnameConfig document: then only there.
	hostnameDoc := false

	for _, doc := range docs {
		switch configDocKind(doc) {
		case "": // the v1alpha1 document has no kind
			main = doc
		case "HostnameConfig":
			setYAMLScalar(doc, []string{"hostname"}, req.hostname)
			deleteYAMLKey(doc, []string{"auto"})

			hostnameDoc = true
		case "LinkConfig", "BondConfig", "VLANConfig", "BridgeConfig", "StaticHostConfig":
			warnings = append(warnings, fmt.Sprintf("the template has a %s document, kept as it is: check it fits the new node", configDocKind(doc)))
		}
	}

	if main == nil {
		return "", nil, errors.New("the template's config has no v1alpha1 document")
	}

	// A control plane's config holds what a worker must not (the CA keys, etcd, the API
	// server's documents) and a worker's lacks what a control plane needs: the role is the
	// template's. "init" (the bootstrap node of old configs) joins as a control plane.
	templateRole := yamlScalarAt(main, []string{"machine", "type"})
	if templateRole == "init" {
		templateRole = joinRoleControlPlane
	}

	if templateRole != req.role {
		return "", nil, fmt.Errorf("the template is a %s: pick a %s as the template for a %s", templateRole, req.role, req.role)
	}

	setYAMLScalar(main, []string{"machine", "type"}, req.role)
	if hostnameDoc {
		deleteYAMLKey(main, []string{"machine", "network", "hostname"})
	} else {
		setYAMLScalar(main, []string{"machine", "network", "hostname"}, req.hostname)
	}
	setYAMLScalar(main, []string{"machine", "install", "disk"}, req.installDisk)
	deleteYAMLKey(main, []string{"machine", "install", "diskSelector"})

	warnings = append(warnings, joinAddresses(main, req.staticAddress)...)

	if req.role == joinRoleControlPlane {
		warnings = append(warnings, "the new node is a control plane: it joins etcd, and the quorum then counts one more member")
	}

	for _, edit := range req.edits {
		if edit.Doc < 0 || edit.Doc >= len(docs) {
			return "", nil, fmt.Errorf("no document %d in this config", edit.Doc)
		}

		if err := applyConfigEdit(docs[edit.Doc], edit); err != nil {
			return "", nil, fmt.Errorf("%s: %w", strings.Join(edit.Path, "."), err)
		}
	}

	text, err := encodeConfigDocs(docs)
	if err != nil {
		return "", nil, err
	}

	provider, err := configloader.NewFromBytes([]byte(text))
	if err != nil {
		return "", nil, fmt.Errorf("the new config is not valid: %w", err)
	}

	checks, err := provider.ValidateAsClient(joinRuntime{})
	if err != nil {
		return "", nil, fmt.Errorf("the new config is not valid: %w", err)
	}

	warnings = append(warnings, checks...)

	full, err := provider.EncodeString(encoder.WithComments(encoder.CommentsDisabled))

	return full, warnings, err
}

// joinAddresses takes the template's static addresses out of every interface; with address,
// the first interface gets it instead.
func joinAddresses(main *yaml.Node, address string) []string {
	ifaces := yamlNodeAt(main, []string{"machine", "network", "interfaces"})
	if ifaces == nil || ifaces.Kind != yaml.SequenceNode || len(ifaces.Content) == 0 {
		if address != "" {
			return []string{"the template has no network interface to put " + address + " on: add one with an edit, or leave the address to DHCP"}
		}

		return nil
	}

	for _, iface := range ifaces.Content {
		deleteYAMLKey(iface, []string{"addresses"})
		deleteYAMLKey(iface, []string{"routes"})
	}

	if address == "" {
		return []string{"the template's static addresses and routes were removed: the new node takes its address from DHCP"}
	}

	first := ifaces.Content[0]
	seq := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{{Kind: yaml.ScalarNode, Value: address}}}
	setYAMLNode(first, []string{"addresses"}, seq)

	return []string{"the template's routes were removed: add a default route with an edit if " + address + " is not on a network with DHCP"}
}

// redactConfig is text with its secrets hidden, as NodeMachineConfig shows a config.
func redactConfig(text string) (string, error) {
	provider, err := configloader.NewFromBytes([]byte(text))
	if err != nil {
		return "", err
	}

	return provider.RedactSecrets(redacted).EncodeString(encoder.WithComments(encoder.CommentsDisabled))
}

// demoJoinTemplate is a full config for a demo node of node's role, generated with throwaway
// secrets: the demo's sample configs are too short to validate, and no cluster's secrets are
// at stake.
func demoJoinTemplate(configYAML, contextName, node string) (string, error) {
	if _, err := demoRead("NodeMachineConfig", configYAML, contextName, node); err != nil {
		return "", err // not a demo node
	}

	role := machine.TypeWorker

	for _, n := range demoNodes() {
		if n.Node == node && n.Role == joinRoleControlPlane {
			role = machine.TypeControlPlane
		}
	}

	in, err := generate.NewInput("ichor-demo", "https://"+demoEndpoint+":6443", strings.TrimPrefix(demoK8sVersion, "v"))
	if err != nil {
		return "", err
	}

	cfg, err := in.Config(role)
	if err != nil {
		return "", err
	}

	return cfg.EncodeString(encoder.WithComments(encoder.CommentsDisabled))
}

// configDocKind is a document's kind ("" for the v1alpha1 machine config).
func configDocKind(doc *yaml.Node) string { return yamlScalarAt(doc, []string{"kind"}) }

func yamlNodeAt(root *yaml.Node, path []string) *yaml.Node {
	n := root
	for _, key := range path {
		if n == nil || n.Kind != yaml.MappingNode {
			return nil
		}

		var next *yaml.Node

		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				next = n.Content[i+1]

				break
			}
		}

		n = next
	}

	return n
}

func yamlScalarAt(root *yaml.Node, path []string) string {
	if n := yamlNodeAt(root, path); n != nil && n.Kind == yaml.ScalarNode {
		return n.Value
	}

	return ""
}

// setYAMLNode puts value at path, creating the maps on the way.
func setYAMLNode(root *yaml.Node, path []string, value *yaml.Node) {
	n := root
	for i, key := range path {
		var next *yaml.Node

		for j := 0; j+1 < len(n.Content); j += 2 {
			if n.Content[j].Value == key {
				next = n.Content[j+1]
				if i == len(path)-1 {
					n.Content[j+1] = value
					return
				}

				break
			}
		}

		if next == nil {
			if i == len(path)-1 {
				next = value
			} else {
				next = &yaml.Node{Kind: yaml.MappingNode}
			}

			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, next)
		}

		n = next
	}
}

func setYAMLScalar(root *yaml.Node, path []string, value string) {
	setYAMLNode(root, path, &yaml.Node{Kind: yaml.ScalarNode, Value: value})
}

// deleteYAMLKey removes the last key of path from its map, when there.
func deleteYAMLKey(root *yaml.Node, path []string) {
	parent := yamlNodeAt(root, path[:len(path)-1])
	if parent == nil || parent.Kind != yaml.MappingNode {
		return
	}

	key := path[len(path)-1]

	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content = append(parent.Content[:i], parent.Content[i+2:]...)

			return
		}
	}
}
