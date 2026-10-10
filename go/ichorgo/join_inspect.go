package ichorgo

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// maintenanceInspection is what a node in maintenance mode (booted from the ISO, not
// installed yet) says about itself: enough to pick its install disk.
type maintenanceInspection struct {
	Address  string      `json:"address"`
	Version  string      `json:"version"`
	Arch     string      `json:"arch"`
	Platform string      `json:"platform"`
	System   *systemInfo `json:"system"` // null when unavailable
	Disks    []diskInfo  `json:"disks"`
	// Links are the physical links, with their addresses.
	Links     []linkInfo    `json:"links"`
	Addresses []addressInfo `json:"addresses"`
	// Maintenance is false for a node that is already installed: it asks for a client
	// certificate, and nothing else is read.
	Maintenance bool              `json:"maintenance"`
	Errors      map[string]string `json:"errors"` // section -> error, for sections that failed
}

// demoMaintenanceAddress is the address the demo answers with a sample inspection.
const demoMaintenanceAddress = "demo"

var (
	errMaintenanceAddress = errors.New("type the node's IP address or host name, as its console shows it")
	errMaintenancePublic  = errors.New("only a node on your local network or VPN can be added: this address is public")
)

// MaintenanceNodeInspect reads a node in maintenance mode at address (IP or host name, port
// 50000 unless given): version, system, disks, links and addresses, like
// `talosctl get disks --insecure`. The maintenance API has no client authentication, so no
// talosconfig is involved; an installed node, which asks for a client certificate, is
// reported with maintenance false. Only private, VPN and link-local addresses are dialled.
func MaintenanceNodeInspect(address string, timeoutSec int) (out string, err error) {
	defer maskResult(&out, &err)

	address = strings.TrimSpace(address)
	if address == demoMaintenanceAddress {
		return toJSON(demoMaintenanceInspection())
	}

	timeout := time.Duration(timeoutSec) * time.Second
	if timeoutSec <= 0 {
		timeout = callTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := checkMaintenanceAddress(ctx, address); err != nil {
		return "", err
	}

	installed, err := asksClientCert(ctx, address)
	if err != nil {
		return "", errors.New(friendlyError(err))
	}

	if installed {
		return toJSON(maintenanceInspection{
			Address: address, Disks: []diskInfo{}, Links: []linkInfo{}, Addresses: []addressInfo{}, Errors: map[string]string{},
		})
	}

	c, err := maintenanceClient(ctx, address)
	if err != nil {
		return "", err
	}

	defer c.Close() //nolint:errcheck

	result, err := inspectMaintenanceNode(ctx, c)
	if err != nil {
		return "", err
	}

	result.Address = address

	return toJSON(result)
}

// maintenanceClient is the client of the unauthenticated maintenance API: TLS without
// verification and without a client certificate, as `talosctl --insecure`. It is only ever
// used here; every other path goes through openSession.
func maintenanceClient(ctx context.Context, address string) (*client.Client, error) {
	c, err := client.New(ctx,
		// The maintenance API has a throwaway self-signed certificate: nothing to verify.
		client.WithTLSConfig(baseTLS(nil, true)),
		client.WithEndpoints(address),
	)
	if err != nil {
		return nil, fmt.Errorf("create Talos client: %w", err)
	}

	return c, nil
}

func inspectMaintenanceNode(ctx context.Context, c *client.Client) (maintenanceInspection, error) {
	resp, err := c.Version(ctx)
	if err != nil {
		return maintenanceInspection{}, errors.New(friendlyError(err))
	}

	out := maintenanceInspection{
		Maintenance: true, Disks: []diskInfo{}, Links: []linkInfo{}, Addresses: []addressInfo{}, Errors: map[string]string{},
	}

	if msg := first(resp.GetMessages()); msg != nil {
		out.Version = msg.GetVersion().GetTag()
		out.Arch = msg.GetVersion().GetArch()
		out.Platform = msg.GetPlatform().GetName()
	}

	st := c.COSI

	if si, err := safe.StateGetByID[*hardware.SystemInformation](ctx, st, hardware.SystemInformationID); err != nil {
		out.Errors["system"] = friendlyError(err)
	} else {
		out.System = mapSystemInfo(si.TypedSpec())
	}

	if disks, err := safe.StateListAll[*block.Disk](ctx, st); err == nil {
		out.Disks = mapBlockDisks(safe.ToSlice(disks, identity))
	} else if apiDisks, apiErr := c.Disks(ctx); apiErr == nil {
		// Older Talos versions serve the Disks API, not the block.Disk resource.
		out.Disks = mapAPIDisks(first(apiDisks.GetMessages()).GetDisks())
	} else {
		out.Errors["disks"] = friendlyError(err)
	}

	if links, err := safe.StateListAll[*network.LinkStatus](ctx, st); err != nil {
		out.Errors["links"] = friendlyError(err)
	} else {
		out.Links = physicalLinks(mapLinks(safe.ToSlice(links, identity)))
	}

	if addrs, err := safe.StateListAll[*network.AddressStatus](ctx, st); err != nil {
		out.Errors["addresses"] = friendlyError(err)
	} else {
		out.Addresses = addressesOn(mapAddresses(safe.ToSlice(addrs, identity), nil), out.Links)
	}

	return out, nil
}

// physicalLinks keeps the links an install can use: no loopback, no virtual plumbing.
func physicalLinks(in []linkInfo) []linkInfo {
	out := make([]linkInfo, 0, len(in))

	for _, l := range in {
		if !l.Virtual && l.Type != "loopback" {
			out = append(out, l)
		}
	}

	return out
}

// addressesOn keeps the addresses of links (loopback and virtual ones dropped).
func addressesOn(in []addressInfo, links []linkInfo) []addressInfo {
	out := make([]addressInfo, 0, len(in))

	for _, a := range in {
		if slices.ContainsFunc(links, func(l linkInfo) bool { return l.Name == a.Link }) {
			out = append(out, a)
		}
	}

	return out
}

// asksClientCert opens a TLS connection to address and tells whether the node asks for a
// client certificate: an installed node's apid does, the maintenance API never does. The
// handshake itself may fail on an installed node (no certificate sent); that is the answer.
func asksClientCert(ctx context.Context, address string) (bool, error) {
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(address, strconv.Itoa(constants.ApidPort))
	}

	// Only the certificate request is looked at: nothing to verify.
	asked := false
	config := baseTLS(nil, true)
	config.NextProtos = []string{"h2"}
	config.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		asked = true

		return &tls.Certificate{}, nil
	}
	dialer := tls.Dialer{Config: config}

	conn, err := dialer.DialContext(ctx, "tcp", address)
	if asked {
		if conn != nil {
			_ = conn.Close() //nolint:errcheck
		}

		return true, nil
	}

	if err != nil {
		return false, err
	}

	return false, conn.Close()
}

// checkMaintenanceAddress accepts an IP or host name (with an optional port) whose addresses
// are all on a local network or VPN: the insecure client never reaches the internet.
func checkMaintenanceAddress(ctx context.Context, address string) error {
	if strings.ContainsAny(address, "/ @?#") {
		return errMaintenanceAddress
	}

	host := address
	if h, port, err := net.SplitHostPort(address); err == nil {
		if port == "" {
			return errMaintenanceAddress
		}

		host = h
	}

	// An IPv6 address in brackets without a port: "[fd00::5]".
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}

	if host == "" {
		return errMaintenanceAddress
	}

	if ip, err := netip.ParseAddr(host); err == nil {
		return checkMaintenanceIP(ip)
	}

	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("%s cannot be resolved: type its IP address instead", host)
	}

	for _, ip := range ips {
		if err := checkMaintenanceIP(ip); err != nil {
			return err
		}
	}

	return nil
}

func checkMaintenanceIP(ip netip.Addr) error {
	ip = ip.Unmap()
	if ip.IsUnspecified() || ip.IsMulticast() {
		return errMaintenanceAddress
	}

	if isPublicAddr(ip) {
		return errMaintenancePublic
	}

	return nil
}

func demoMaintenanceInspection() maintenanceInspection {
	return maintenanceInspection{
		Address: demoMaintenanceAddress, Version: demoTalosVersion, Arch: "amd64", Platform: "metal", Maintenance: true,
		System: &systemInfo{Manufacturer: "Demo Systems", Product: "Mini PC 4", Serial: "DEMO-0042"},
		Disks: []diskInfo{
			{Name: "nvme0n1", DevPath: "/dev/nvme0n1", Model: "Demo NVMe 1TB", Size: 1_000_204_886_016, Type: "nvme"},
			{Name: "sda", DevPath: "/dev/sda", Model: "Demo SSD 480GB", Size: 480_103_981_056, Type: "ssd"},
		},
		Links: []linkInfo{
			{Name: "enp1s0", Type: "ether", State: "up", HardwareAddr: "02:00:00:00:00:42", MTU: 1500, SpeedMbit: 2500},
			{Name: "enp2s0", Type: "ether", State: "down", HardwareAddr: "02:00:00:00:00:43", MTU: 1500},
		},
		Addresses: []addressInfo{{Address: "192.0.2.42/24", Link: "enp1s0", Family: "inet4", Scope: "global"}},
		Errors:    map[string]string{},
	}
}
