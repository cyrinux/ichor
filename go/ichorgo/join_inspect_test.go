package ichorgo

import (
	"crypto/tls"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// maintenanceNode is a fake node in maintenance mode: the API answers without a node
// target and without a client certificate.
func maintenanceNode(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.setVersion("", "v1.14.2")

	si := hardware.NewSystemInformation(hardware.SystemInformationID)
	si.TypedSpec().Manufacturer, si.TypedSpec().ProductName, si.TypedSpec().SerialNumber = "Sample Systems", "Box 1", "S-0001"

	nvme := block.NewDisk(block.NamespaceName, "nvme0n1")
	nvme.TypedSpec().Size, nvme.TypedSpec().DevPath, nvme.TypedSpec().Model = 512<<30, "/dev/nvme0n1", "Sample NVMe"

	cdrom := block.NewDisk(block.NamespaceName, "sr0")
	cdrom.TypedSpec().Size, cdrom.TypedSpec().CDROM = 100<<20, true

	lo := testLink("lo", "", 0)
	lo.TypedSpec().Type = nethelpers.LinkLoopbck

	addr := func(link, prefix string) *network.AddressStatus {
		a := network.NewAddressStatus(network.NamespaceName, link+"/"+prefix)
		a.TypedSpec().Address, a.TypedSpec().LinkName = netip.MustParsePrefix(prefix), link
		a.TypedSpec().Family, a.TypedSpec().Scope = nethelpers.FamilyInet4, nethelpers.ScopeGlobal

		return a
	}

	f.put("", si, nvme, cdrom, testLink("enp1s0", "", 1000), lo, addr("enp1s0", "192.0.2.42/24"), addr("lo", "127.0.0.1/8"))

	endpoint, _ := f.serve(t)

	return f, endpoint
}

func TestMaintenanceNodeInspect(t *testing.T) {
	_, endpoint := maintenanceNode(t)

	out, err := MaintenanceNodeInspect(endpoint, 5)
	got := decodeJSON[maintenanceInspection](t, out, err)

	if !got.Maintenance || got.Address != endpoint || got.Version != "v1.14.2" || got.Arch != "amd64" || got.Platform != "metal" {
		t.Fatalf("inspection = %+v", got)
	}

	if got.System == nil || got.System.Manufacturer != "Sample Systems" || got.System.Serial != "S-0001" {
		t.Errorf("system = %+v", got.System)
	}

	if len(got.Disks) != 1 || got.Disks[0].DevPath != "/dev/nvme0n1" || got.Disks[0].Size != 512<<30 {
		t.Errorf("disks = %+v, want the NVMe disk only (no CD-ROM)", got.Disks)
	}

	if len(got.Links) != 1 || got.Links[0].Name != "enp1s0" || got.Links[0].SpeedMbit != 1000 {
		t.Errorf("links = %+v, want enp1s0 only (no loopback)", got.Links)
	}

	if len(got.Addresses) != 1 || got.Addresses[0].Address != "192.0.2.42/24" {
		t.Errorf("addresses = %+v, want enp1s0's only", got.Addresses)
	}

	if len(got.Errors) != 0 {
		t.Errorf("errors = %v", got.Errors)
	}
}

// An installed node's apid asks for a client certificate: it is reported, not read.
func TestMaintenanceNodeInspectInstalledNode(t *testing.T) {
	pair, _ := testServerCert(t)

	lis, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2"},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = lis.Close() }) //nolint:errcheck

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}

			go func() {
				_ = conn.(*tls.Conn).Handshake() //nolint:errcheck,forcetypeassert
				_ = conn.Close()                 //nolint:errcheck
			}()
		}
	}()

	out, err := MaintenanceNodeInspect(lis.Addr().String(), 5)
	got := decodeJSON[maintenanceInspection](t, out, err)

	if got.Maintenance || got.Version != "" || len(got.Disks) != 0 {
		t.Fatalf("inspection = %+v, want an installed node with nothing read", got)
	}
}

func TestMaintenanceNodeInspectUnreachable(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	address := lis.Addr().String()
	_ = lis.Close() //nolint:errcheck

	if out, err := MaintenanceNodeInspect(address, 3); err == nil {
		t.Fatalf("closed port: got %s, want an error", out)
	}
}

// A host that accepts the connection and never answers ends at the timeout.
func TestMaintenanceNodeInspectTimeout(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = lis.Close() }) //nolint:errcheck

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}

			t.Cleanup(func() { _ = conn.Close() }) //nolint:errcheck
		}
	}()

	start := time.Now()

	if out, err := MaintenanceNodeInspect(lis.Addr().String(), 1); err == nil {
		t.Fatalf("silent host: got %s, want an error", out)
	}

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %s, want about the 1 s timeout", elapsed)
	}
}

func TestMaintenanceNodeInspectRefusedAddresses(t *testing.T) {
	for _, tc := range []struct{ address, want string }{
		{"", "IP address or host name"},
		{"8.8.8.8", "public"},
		{"[2001:4860::1]:50000", "public"},
		{"192.0.2.10", "public"}, // documentation range, not a LAN
		{"0.0.0.0", "IP address or host name"},
		{"224.0.0.1", "IP address or host name"},
		{"https://10.0.0.5", "IP address or host name"},
		{"10.0.0.5:", "IP address or host name"},
	} {
		out, err := MaintenanceNodeInspect(tc.address, 1)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %s, %v; want an error with %q", tc.address, out, err, tc.want)
		}
	}
}

func TestCheckMaintenanceAddressLocal(t *testing.T) {
	for _, address := range []string{"10.0.0.5", "192.168.1.20:50000", "172.16.0.9", "100.100.1.2", "fd00::5", "[fd00::5]", "[fe80::1]:50000", "127.0.0.1"} {
		if err := checkMaintenanceAddress(t.Context(), address); err != nil {
			t.Errorf("%q: %v", address, err)
		}
	}
}

func TestMaintenanceNodeInspectDemo(t *testing.T) {
	out, err := MaintenanceNodeInspect(" demo ", 1)
	got := decodeJSON[maintenanceInspection](t, out, err)

	if !got.Maintenance || got.Version != demoTalosVersion || len(got.Disks) == 0 || len(got.Links) == 0 || got.System == nil {
		t.Fatalf("demo inspection = %+v", got)
	}
}
