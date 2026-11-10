package ichorgo

// The config side of a bootstrap: the machine config as the run generates it from the
// fresh secrets (and patches it for the node the console shows), and the validation of
// what the user typed. The reaching and following half lives in newcluster_follow.go.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/encoder"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"go.yaml.in/yaml/v4"
)

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
