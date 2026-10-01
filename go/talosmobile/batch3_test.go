package talosmobile

import (
	"net/netip"
	"slices"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	"github.com/siderolabs/talos/pkg/machinery/api/storage"
	timeapi "github.com/siderolabs/talos/pkg/machinery/api/time"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/block"
	"github.com/siderolabs/talos/pkg/machinery/resources/hardware"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func testLink(name, kind string, speed int64) *network.LinkStatus {
	l := network.NewLinkStatus(network.NamespaceName, name)
	l.TypedSpec().Kind = kind
	l.TypedSpec().Type = nethelpers.LinkEther
	l.TypedSpec().OperationalState = nethelpers.OperStateUp
	l.TypedSpec().MTU = 1500
	l.TypedSpec().SpeedMegabits = int(speed) // wraps to -1 on 32-bit, which linkSpeed also drops

	return l
}

func TestMapLinks(t *testing.T) {
	got := mapLinks([]*network.LinkStatus{
		testLink("lxc1234", "veth", 10000),
		testLink("eth0", "", 1000),
		testLink("cilium_vxlan", "vxlan", -1),
		testLink("bond0", "bond", 4294967295),
		testLink("kubespan", "wireguard", 0),
	})

	var names []string
	for _, l := range got {
		names = append(names, l.Name)
	}

	if want := []string{"bond0", "eth0", "kubespan", "cilium_vxlan", "lxc1234"}; !slices.Equal(names, want) {
		t.Errorf("order = %v, want %v", names, want)
	}

	if got[1].SpeedMbit != 1000 || got[0].SpeedMbit != 0 || got[3].SpeedMbit != 0 {
		t.Errorf("speeds = %d %d %d", got[1].SpeedMbit, got[0].SpeedMbit, got[3].SpeedMbit)
	}

	if got[1].Virtual || got[2].Virtual || !got[3].Virtual || !got[4].Virtual {
		t.Errorf("virtual flags wrong: %+v", got)
	}

	if got[1].Type != "ether" || got[1].State != "up" || got[1].MTU != 1500 {
		t.Errorf("eth0 = %+v", got[1])
	}
}

func testRoute(id, dst, gw, link string, table nethelpers.RoutingTable, prio uint32) *network.RouteStatus {
	r := network.NewRouteStatus(network.NamespaceName, id)
	spec := r.TypedSpec()
	spec.Family = nethelpers.FamilyInet4

	if dst != "" {
		spec.Destination = netip.MustParsePrefix(dst)
	}

	if gw != "" {
		spec.Gateway = netip.MustParseAddr(gw)
	}

	spec.OutLinkName, spec.Table, spec.Priority = link, table, prio

	return r
}

func TestMapRoutes(t *testing.T) {
	ecmp := testRoute("ecmp", "10.9.0.0/16", "", "", nethelpers.TableMain, 5)
	ecmp.TypedSpec().NextHops = []network.RouteNextHop{
		{Gateway: netip.MustParseAddr("10.0.0.1"), OutLinkName: "eth0"},
		{Gateway: netip.MustParseAddr("10.0.0.2"), OutLinkName: "eth1"},
	}

	got := mapRoutes([]*network.RouteStatus{
		testRoute("pod", "10.244.0.11/32", "", "cilium_host", nethelpers.TableMain, 0),
		testRoute("local", "10.0.0.0/24", "", "eth0", nethelpers.TableLocal, 0),
		testRoute("lan", "10.0.0.0/24", "", "eth0", nethelpers.TableMain, 1024),
		testRoute("def", "", "10.0.0.1", "eth0", nethelpers.TableMain, 1024),
		ecmp,
	}, map[string]bool{"cilium_host": true})

	want := []routeInfo{
		{Destination: "default", Gateway: "10.0.0.1", Link: "eth0", Metric: 1024, Table: "main", Family: "inet4"},
		{Destination: "10.0.0.0/24", Link: "eth0", Metric: 1024, Table: "main", Family: "inet4"},
		{Destination: "10.9.0.0/16", Gateway: "10.0.0.1, 10.0.0.2", Link: "eth0", Metric: 5, Table: "main", Family: "inet4"},
		{Destination: "10.244.0.11/32", Link: "cilium_host", Table: "main", Family: "inet4", Virtual: true},
	}

	if !equalJSON(t, got, want) {
		t.Error("unexpected routes")
	}
}

func TestMapAddressesResolversTimeServers(t *testing.T) {
	a1 := network.NewAddressStatus(network.NamespaceName, "eth0/10.0.0.5/24")
	a1.TypedSpec().Address = netip.MustParsePrefix("10.0.0.5/24")
	a1.TypedSpec().LinkName = "eth0"
	a1.TypedSpec().Family = nethelpers.FamilyInet4
	a1.TypedSpec().Scope = nethelpers.ScopeGlobal

	a2 := network.NewAddressStatus(network.NamespaceName, "lxcabc/fe80::1/64")
	a2.TypedSpec().Address = netip.MustParsePrefix("fe80::1/64")
	a2.TypedSpec().LinkName = "lxcabc"
	a2.TypedSpec().Family = nethelpers.FamilyInet6
	a2.TypedSpec().Scope = nethelpers.ScopeLink

	addrs := mapAddresses([]*network.AddressStatus{a2, a1}, nil)
	want := []addressInfo{
		{Address: "10.0.0.5/24", Link: "eth0", Family: "inet4", Scope: "global"},
		{Address: "fe80::1/64", Link: "lxcabc", Family: "inet6", Scope: "link", Virtual: true},
	}

	if !equalJSON(t, addrs, want) {
		t.Error("unexpected addresses")
	}

	res := network.NewResolverStatus(network.NamespaceName, network.ResolverID)
	res.TypedSpec().DNSServers = []netip.Addr{netip.MustParseAddr("9.9.9.9")}
	res.TypedSpec().NameServers = []network.NameServerSpec{
		{Addr: netip.MustParseAddr("1.1.1.1")}, {Addr: netip.MustParseAddr("1.1.1.1")}, {Addr: netip.MustParseAddr("1.0.0.1")},
	}

	if got := mapResolvers([]*network.ResolverStatus{res}); !slices.Equal(got, []string{"1.1.1.1", "1.0.0.1"}) {
		t.Errorf("resolvers = %v", got)
	}

	legacy := network.NewResolverStatus(network.NamespaceName, network.ResolverID)
	legacy.TypedSpec().DNSServers = []netip.Addr{netip.MustParseAddr("9.9.9.9")}

	if got := mapResolvers([]*network.ResolverStatus{legacy}); !slices.Equal(got, []string{"9.9.9.9"}) {
		t.Errorf("legacy resolvers = %v", got)
	}

	ts := network.NewTimeServerStatus(network.NamespaceName, network.TimeServerID)
	ts.TypedSpec().NTPServers = []string{"time.cloudflare.com", "pool.ntp.org"}

	if got := mapTimeServers([]*network.TimeServerStatus{ts}); !slices.Equal(got, []string{"time.cloudflare.com", "pool.ntp.org"}) {
		t.Errorf("time servers = %v", got)
	}
}

func TestMapConnections(t *testing.T) {
	got := mapConnections([]*machineapi.ConnectRecord{
		{L4Proto: "tcp", Localip: "10.0.0.5", Localport: 50000, Remoteip: "10.0.0.9", Remoteport: 41000, State: machineapi.ConnectRecord_ESTABLISHED},
		{L4Proto: "tcp", Localip: "0.0.0.0", Localport: 50000, Remoteip: "0.0.0.0", State: machineapi.ConnectRecord_LISTEN,
			Process: &machineapi.ConnectRecord_Process{Pid: 1, Name: "init"}},
		{L4Proto: "udp", Localip: "127.0.0.53", Localport: 53, Remoteip: "0.0.0.0", State: machineapi.ConnectRecord_CLOSE},
		{L4Proto: "tcp6", Localip: "::", Localport: 6443, Remoteip: "::", State: machineapi.ConnectRecord_LISTEN},
		{L4Proto: "tcp", Localip: "10.0.0.5", Localport: 2379, Remoteip: "10.0.0.6", Remoteport: 1234, State: machineapi.ConnectRecord_TIME_WAIT},
	})

	want := []connectionInfo{
		{Protocol: "udp", LocalIP: "127.0.0.53", LocalPort: 53, RemoteIP: "0.0.0.0", State: "CLOSE", Listening: true},
		{Protocol: "tcp6", LocalIP: "::", LocalPort: 6443, RemoteIP: "::", State: "LISTEN", Listening: true},
		{Protocol: "tcp", LocalIP: "0.0.0.0", LocalPort: 50000, RemoteIP: "0.0.0.0", State: "LISTEN", Listening: true, Pid: 1, ProcessName: "init"},
		{Protocol: "tcp", LocalIP: "10.0.0.5", LocalPort: 2379, RemoteIP: "10.0.0.6", RemotePort: 1234, State: "TIME_WAIT"},
		{Protocol: "tcp", LocalIP: "10.0.0.5", LocalPort: 50000, RemoteIP: "10.0.0.9", RemotePort: 41000, State: "ESTABLISHED"},
	}

	if !equalJSON(t, got, want) {
		t.Error("unexpected connections")
	}
}

func TestMapNodeTime(t *testing.T) {
	local := time.UnixMilli(1_800_000_000_000)
	remote := local.Add(-250 * time.Millisecond)

	got := mapNodeTime("n1", &timeapi.Time{
		Server: "pool.ntp.org", Localtime: timestamppb.New(local), Remotetime: timestamppb.New(remote),
	})
	want := nodeTime{Node: "n1", Server: "pool.ntp.org", LocalTime: local.UnixMilli(), RemoteTime: remote.UnixMilli(), OffsetMs: -250}

	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}

	if got := mapNodeTime("n2", nil); got.Error == "" || got.Node != "n2" {
		t.Errorf("nil time = %+v", got)
	}
}

func TestMapImages(t *testing.T) {
	created := time.UnixMilli(1_790_000_000_000)

	got := mapImages([]*machineapi.ImageListResponse{
		{Name: "registry.k8s.io/pause:3.10", Digest: "sha256:b", Size: 300},
		{Name: "ghcr.io/siderolabs/flannel:v1", Digest: "sha256:a", Size: 1000, CreatedAt: timestamppb.New(created)},
	})
	want := []imageInfo{
		{Name: "ghcr.io/siderolabs/flannel:v1", Digest: "sha256:a", Size: 1000, Created: created.UnixMilli()},
		{Name: "registry.k8s.io/pause:3.10", Digest: "sha256:b", Size: 300},
	}

	if !equalJSON(t, got, want) {
		t.Error("unexpected images")
	}
}

func TestMapHardware(t *testing.T) {
	cpu := hardware.NewProcessorInfo("cpu0")
	*cpu.TypedSpec() = hardware.ProcessorSpec{Socket: "CPU0", ProductName: " Xeon ", CoreCount: 8, ThreadCount: 16, MaxSpeed: 3600}
	emptySocket := hardware.NewProcessorInfo("cpu1")
	emptySocket.TypedSpec().Socket = "CPU1"

	if got := mapProcessors([]*hardware.Processor{emptySocket, cpu}); len(got) != 1 || got[0].Model != "Xeon" || got[0].Threads != 16 {
		t.Errorf("processors = %+v", got)
	}

	dimm := hardware.NewMemoryModuleInfo("m0")
	*dimm.TypedSpec() = hardware.MemoryModuleSpec{Size: 16384, DeviceLocator: "DIMM A1", BankLocator: "BANK 0", Speed: 3200, Manufacturer: "Samsung"}
	emptySlot := hardware.NewMemoryModuleInfo("m1")

	if got := mapMemoryModules([]*hardware.MemoryModule{emptySlot, dimm}); len(got) != 1 || got[0].SizeMiB != 16384 || got[0].Slot != "DIMM A1" {
		t.Errorf("memory = %+v", got)
	}

	disks := mapAPIDisks([]*storage.Disk{
		{DeviceName: "/dev/sda", Model: "SSD", Size: 100, Type: storage.Disk_SSD, SystemDisk: true},
		{DeviceName: "nvme0n1", Size: 200, Type: storage.Disk_NVME},
		{DeviceName: "/dev/loop0", Size: 4096},
		{DeviceName: "/dev/dm-0", Size: 4096},
	})
	wantDisks := []diskInfo{
		{Name: "nvme0n1", DevPath: "/dev/nvme0n1", Size: 200, Type: "nvme"},
		{Name: "sda", DevPath: "/dev/sda", Model: "SSD", Size: 100, Type: "ssd", SystemDisk: true},
	}

	if !equalJSON(t, disks, wantDisks) {
		t.Error("unexpected API disks")
	}

	ext := runtime.NewExtensionStatus(runtime.NamespaceName, "0")
	ext.TypedSpec().Metadata.Name = "iscsi-tools"
	ext.TypedSpec().Metadata.Version = "v0.2.0"

	if got := mapExtensions([]*runtime.ExtensionStatus{ext}); len(got) != 1 || got[0].Name != "iscsi-tools" || got[0].Version != "v0.2.0" {
		t.Errorf("extensions = %+v", got)
	}

	sec := mapSecurity(&runtime.SecurityStateSpec{SecureBoot: true, UKISigningKeyFingerprint: "AB:CD"})
	if !sec.SecureBoot || sec.UKISigningKeyFingerprint != "AB:CD" || sec.SELinuxState == "" {
		t.Errorf("security = %+v", sec)
	}
}

func TestMapBlockDisks(t *testing.T) {
	disk := func(id string, spec block.DiskSpec) *block.Disk {
		d := block.NewDisk(block.NamespaceName, id)
		*d.TypedSpec() = spec

		return d
	}

	got := mapBlockDisks([]*block.Disk{
		disk("sdb", block.DiskSpec{DevPath: "/dev/sdb", Size: 4000, Rotational: true, Transport: "sata"}),
		disk("nvme0n1", block.DiskSpec{DevPath: "/dev/nvme0n1", Size: 2000, Transport: "nvme", Model: "Fast"}),
		disk("sda", block.DiskSpec{Size: 1000, Transport: "sata"}),
		disk("mmcblk0", block.DiskSpec{Size: 500, Transport: "mmc"}),
		disk("sr0", block.DiskSpec{Size: 100, CDROM: true}),
		disk("loop0", block.DiskSpec{Size: 100}),
	})

	var types []string
	for _, d := range got {
		types = append(types, d.Name+"="+d.Type)
	}

	if want := []string{"mmcblk0=sd", "nvme0n1=nvme", "sda=ssd", "sdb=hdd"}; !slices.Equal(types, want) {
		t.Errorf("disks = %v, want %v", types, want)
	}

	if got[2].DevPath != "/dev/sda" {
		t.Errorf("default dev path = %q", got[2].DevPath)
	}
}
