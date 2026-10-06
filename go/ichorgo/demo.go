package ichorgo

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"strings"
	"time"
)

// This reserved endpoint is intercepted locally. It is never dialled, even for
// operations the demo does not implement. No credentials for a real cluster exist.
const demoEndpoint = "demo.ichor.invalid"

var errDemoUnavailable = errors.New("This action is unavailable in the demo cluster")

// DemoConfig creates an ordinary, mergeable talosconfig for the built-in demo.
// The public demo identity is deterministic so adding it again updates the same
// cluster; it is only used to pass the normal config validation and role checks.
func DemoConfig() (out string, err error) {
	defer maskErr(&err)
	seed := sha256.Sum256([]byte("Ichor built-in demo identity v1"))
	key := ed25519.NewKeyFromSeed(seed[:])
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Ichor demo", Organization: []string{"os:admin"}},
		NotBefore: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:  time.Date(2120, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA:      true, BasicConstraintsValid: true,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, key.Public(), key)
	if err != nil {
		return "", err
	}
	priv, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", err
	}
	encode := func(kind string, bytes []byte) string {
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: bytes}))
	}
	crt := encode("CERTIFICATE", der)
	return fmt.Sprintf("context: Demo cluster\ncontexts:\n  Demo cluster:\n    endpoints: [%s]\n    nodes: [192.0.2.10, 192.0.2.11, 192.0.2.12, 192.0.2.20, 192.0.2.21]\n    ca: %s\n    crt: %s\n    key: %s\n", demoEndpoint, crt, crt, encode("PRIVATE KEY", priv)), nil
}

func isDemoContext(yaml, name string) bool {
	if !strings.Contains(yaml, demoEndpoint) {
		return false
	}
	_, c, err := resolveContext(yaml, name)
	return err == nil && slices.Contains(c.Endpoints, demoEndpoint)
}

// demoTalosVersion is the Talos version the demo nodes say they run.
const demoTalosVersion = "v1.14.0"

func demoNodes() []nodeOverview {
	nodes := make([]nodeOverview, 5)
	for i := range nodes {
		role, host, address, cores, memory := "controlplane", fmt.Sprintf("demo-cp-%d", i+1), fmt.Sprintf("192.0.2.%d", 10+i), 4, uint64(8<<30)
		if i >= 3 {
			role, host, address, cores, memory = "worker", fmt.Sprintf("demo-worker-%d", i-2), fmt.Sprintf("192.0.2.%d", 17+i), 8, 16<<30
		}
		nodes[i] = nodeOverview{Node: address, Hostname: host, Reachable: true, Version: demoTalosVersion, Arch: "amd64", Platform: "metal", Role: role, Stage: "running", Ready: true, CPUCount: cores, MemTotal: memory, MemAvailable: memory * 3 / 5, UnmetConditions: []unmetCondition{}}
		if i >= 3 { // the workers sit behind a NAT the discovery service sees
			nodes[i].PublicIPs = []string{fmt.Sprintf("203.0.113.%d", 40+i)}
		}
	}
	return nodes
}

var demoBoot = time.Now().Add(-72 * time.Hour)

func demoStats(n nodeOverview) nodeStats {
	seconds := time.Since(demoBoot).Seconds()
	// The integral keeps cumulative counters monotonic while producing a gentle
	// changing CPU rate between polls (rather than multiplying a changing rate).
	busy := float64(n.CPUCount) * (seconds*0.24 + 0.06*30*(1-math.Cos(seconds/30)))
	return nodeStats{At: time.Now().UnixMilli(), CPUBusy: busy, CPUTotal: seconds * float64(n.CPUCount), CPUCount: n.CPUCount,
		CPUWait: seconds * float64(n.CPUCount) * 0.04, CPUSteal: seconds * float64(n.CPUCount) * 0.01, BootTime: uint64(demoBoot.Unix()),
		NetworkDevices: []networkCounters{{Name: "eth0", Rx: uint64(seconds * 180000), Tx: uint64(seconds * 75000), Errors: uint64(seconds / 60), Drops: uint64(seconds / 30)}},
		DiskDevices:    []diskCounters{{Name: "nvme0n1", Read: uint64(seconds * 90000), Write: uint64(seconds * 45000), Operations: uint64(seconds * 25), TimeMs: uint64(seconds * 50), BusyMs: uint64(seconds * 120)}}, Errors: map[string]string{},
		MemTotal: n.MemTotal, MemAvailable: uint64(float64(n.MemTotal) * (0.6 + 0.04*math.Sin(seconds/45))), Load1: 0.8,
		NetRx: uint64(seconds * 180000), NetTx: uint64(seconds * 75000), DiskRead: uint64(seconds * 90000), DiskWrite: uint64(seconds * 45000)}
}

func demoRead(operation, yaml, name, node string, args ...string) (string, error) {
	contextName, _, err := resolveContext(yaml, name)
	if err != nil {
		return "", err
	}
	nodes := demoNodes()
	n := nodes[0]
	if node != "" {
		index := slices.IndexFunc(nodes, func(n nodeOverview) bool { return n.Node == node })
		if index < 0 {
			return "", errors.New("node is not part of the demo cluster")
		}
		n = nodes[index]
	}
	now := time.Now().UnixMilli()
	sample := demoStats(n)
	switch operation {
	case "ClusterOverview":
		return toJSON(clusterOverview{Context: contextName, Nodes: nodes})
	case "NodeStats":
		return toJSON(sample)
	case "ClusterStats":
		result := clusterStats{At: now, Nodes: []nodeCounters{}}
		for _, n := range nodes {
			s := demoStats(n)
			result.Nodes = append(result.Nodes, nodeCounters{Node: n.Node, CPUBusy: s.CPUBusy, CPUTotal: s.CPUTotal, CPUCount: s.CPUCount, MemTotal: s.MemTotal, MemAvailable: s.MemAvailable})
		}
		return toJSON(result)
	case "ClusterInventory":
		return inventoryJSON(demoInventory(now))
	case "NodeServices":
		services := []serviceInfo{}
		ids := []string{"apid", "containerd", "kubelet", "machined", "trustd"}
		if n.Role == "controlplane" {
			ids = append(ids, "etcd")
		}
		for _, id := range ids {
			services = append(services, serviceInfo{ID: id, State: "Running", Health: "healthy", Message: "Health check successful", LastEvent: "Service started", LastChange: demoBoot.Unix()})
		}
		return toJSON(services)
	case "NodeResources":
		return toJSON(nodeResources{MemTotal: sample.MemTotal, MemAvailable: sample.MemAvailable, Load1: 0.8, Load5: 0.7, Load15: 0.6, BootTime: uint64(demoBoot.Unix()), CPUCount: n.CPUCount, CPUModel: "Intel Xeon (demo)", Mounts: []mountUsage{{Filesystem: "/dev/nvme0n1p6", MountedOn: "/var", Size: 100 << 30, Available: 72 << 30}}})
	case "NodeFeatures":
		features := computeFeatures(n.Version)
		for _, id := range []string{"packetCapture", "upgrade", "etcdSnapshot", "etcdMemberActions", "supportBundle", "issueConfig", "debugShell", "serviceControl"} {
			features.Features[id] = featureState{Reason: errDemoUnavailable.Error()}
		}
		return toJSON(features)
	case "NodeTime":
		return toJSON(nodeTime{Node: n.Node, Server: "time.demo.invalid", LocalTime: now, RemoteTime: now + 2, OffsetMs: 2})
	case "ClusterTime":
		result := clusterTime{Context: contextName, Nodes: []nodeTime{}}
		for _, n := range nodes {
			result.Nodes = append(result.Nodes, nodeTime{Node: n.Node, Server: "time.demo.invalid", LocalTime: now, RemoteTime: now + 2, OffsetMs: 2})
		}
		return toJSON(result)
	case "EtcdStatus":
		result := etcdOverview{LeaderID: "a1", Members: []etcdMember{}, Statuses: []etcdNodeStatus{}, Alarms: []etcdAlarm{}}
		for i, n := range nodes[:3] {
			id := fmt.Sprintf("a%d", i+1)
			result.Members = append(result.Members, etcdMember{ID: id, Hostname: n.Hostname, PeerURLs: []string{"https://" + n.Node + ":2380"}, ClientURLs: []string{"https://" + n.Node + ":2379"}})
			// The third member trails the leader, so the demo shows a lagging follower.
			index := uint64(125600)
			if i == 2 {
				index -= 2400
			}
			result.Statuses = append(result.Statuses, etcdNodeStatus{Node: n.Node, MemberID: id, IsLeader: i == 0, DbSize: 64 << 20, DbSizeInUse: 42 << 20, RaftIndex: index, RaftAppliedIndex: index, RaftTerm: 4, Version: "3.6.0", Errors: []string{}})
		}
		return toJSON(result)
	case "ServiceLogs", "KernelLogs", "ContainerLogs":
		service := "kernel"
		if len(args) > 0 && args[0] != "" {
			service = args[0]
		}
		lines := []string{}
		for i := 0; i < 20; i++ {
			lines = append(lines, demoLogLine(service, time.Now().Add(time.Duration(i-20)*time.Minute)))
		}
		tail := newTailLines(defaultLogLines)
		if len(args) > 1 {
			var limit int
			if _, err := fmt.Sscanf(args[1], "%d", &limit); err == nil {
				tail = newTailLines(clampTail(limit))
			}
		}
		for _, line := range lines {
			tail.write([]byte(line + "\n"))
		}
		return toJSON(tail.result())
	case "NodeNetwork":
		return toJSON(nodeNetwork{
			Links:     []linkInfo{{Name: "eth0", Type: "ether", State: "up", HardwareAddr: "02:00:00:00:00:10", MTU: 1500, SpeedMbit: 1000}},
			Addresses: []addressInfo{{Address: n.Node + "/24", Link: "eth0", Family: "inet4", Scope: "global"}},
			Routes:    []routeInfo{{Destination: "default", Gateway: "192.0.2.1", Link: "eth0", Table: "main", Family: "inet4"}},
			Resolvers: []string{"192.0.2.1"}, TimeServers: []string{"time.demo.invalid"}, Errors: map[string]string{},
		})
	case "NodeConnections":
		return toJSON([]connectionInfo{
			{Protocol: "tcp", LocalIP: "0.0.0.0", LocalPort: 50000, State: "LISTEN", Listening: true, Pid: 102, ProcessName: "apid"},
			{Protocol: "tcp", LocalIP: "0.0.0.0", LocalPort: 10250, State: "LISTEN", Listening: true, Pid: 230, ProcessName: "kubelet"},
		})
	case "NodeProcesses":
		return toJSON(processList{At: now, Processes: []processInfo{
			{Pid: 1, State: "S", Threads: 12, CPUTime: time.Since(demoBoot).Seconds() * 0.02, RSS: 32 << 20, VMS: 64 << 20, Command: "machined", Args: "/sbin/machined"},
			{Pid: 230, Ppid: 1, State: "S", Threads: 24, CPUTime: time.Since(demoBoot).Seconds() * 0.15, RSS: 128 << 20, VMS: 256 << 20, Command: "kubelet", Args: "/usr/bin/kubelet --config=/etc/kubernetes/kubelet.yaml"},
		}})
	case "NodeCgroups":
		return toJSON(demoCgroups(n, now))
	case "NodeContainers":
		return toJSON(containerList{At: now, Containers: []containerInfo{
			{ID: "demo-coredns", PodNamespace: "kube-system", Pod: "coredns-demo", Name: "coredns", Image: "registry.k8s.io/coredns/coredns:v1.12.0", Status: "CONTAINER_RUNNING", Pid: 300, Memory: 24 << 20, CPUNanos: uint64(time.Since(demoBoot).Seconds() * 2e7)},
			{ID: "demo-web", PodNamespace: "demo", Pod: "hello-ichor", Name: "web", Image: "nginx:1.27", Status: "CONTAINER_RUNNING", Pid: 320, Memory: 16 << 20, CPUNanos: uint64(time.Since(demoBoot).Seconds() * 1e7)},
		}})
	case "NodeImages":
		return toJSON([]imageInfo{
			{Name: "registry.k8s.io/coredns/coredns:v1.12.0", Digest: "sha256:abc123demo", Size: 20 << 20, Created: demoBoot.UnixMilli()},
			{Name: "nginx:1.27", Digest: "sha256:def456demo", Size: 64 << 20, Created: demoBoot.UnixMilli()},
		})
	case "NodeMounts":
		return toJSON(mountList{Mounts: []mountInfo{{Filesystem: "/dev/nvme0n1p6", MountedOn: "/var", Size: 100 << 30, Available: 72 << 30, Used: 28 << 30, UsedPercent: 28}}})
	case "NodeVolumes":
		return toJSON(volumeList{Supported: true, Volumes: []volumeInfo{
			{ID: "EPHEMERAL", Phase: "ready", Type: "partition", Location: "/dev/nvme0n1p6", Size: 100 << 30, Filesystem: "xfs", MountedOn: "/var"},
			{ID: "STATE", Phase: "ready", Type: "partition", Location: "/dev/nvme0n1p5", Size: 100 << 20, Filesystem: "xfs", MountedOn: "/system/state"},
		}})
	case "NodeHardware":
		return toJSON(nodeHardware{
			System:     &systemInfo{Manufacturer: "Ichor", Product: "Demo server", Serial: "DEMO-001"},
			Processors: []processorInfo{{Socket: "CPU0", Manufacturer: "Intel", Model: "Xeon (demo)", Cores: uint32(n.CPUCount), Threads: uint32(n.CPUCount), MaxSpeedMHz: 3200}},
			Memory:     []memoryModule{{Slot: "DIMM0", SizeMiB: uint32(n.MemTotal >> 20), Type: "DDR4", SpeedMTs: 3200}},
			Disks:      []diskInfo{{Name: "nvme0n1", DevPath: "/dev/nvme0n1", Model: "Demo NVMe", Size: 120 << 30, Type: "nvme", SystemDisk: true}},
			Extensions: []extensionInfo{}, Security: &securityInfo{SecureBoot: true, BootedWithUKI: true}, Errors: map[string]string{},
		})
	case "NodeDiskHealth":
		return `{"supported":false,"reason":"Disk health data is unavailable in the demo cluster","disks":[]}`, nil
	case "KubeSpanStatus":
		return toJSON(demoKubeSpan())
	case "ClusterTopology":
		if privacy.isEnabled() {
			return toJSON(hideLocations(demoTopology()))
		}
		return toJSON(demoTopology())
	case "NodeMachineConfig":
		return fmt.Sprintf("# Sample configuration for the Ichor demo\nversion: v1alpha1\nmachine:\n  type: %s\n  network:\n    hostname: %s\ncluster:\n  clusterName: ichor-demo\n  controlPlane:\n    endpoint: https://192.0.2.10:6443\n", n.Role, n.Hostname), nil
	case "NodeDiskUsage":
		root := "/var/log"
		if len(args) > 0 {
			root = args[0]
		}
		return toJSON(map[string]any{"entries": []map[string]any{{"path": root + "/services", "size": 32 << 20, "isDir": true}, {"path": root + "/system.log", "size": 8 << 20, "isDir": false}}, "truncated": false})
	case "ResourceTypes":
		return toJSON([]resourceTypeInfo{{Type: "HostnameStatuses.net.talos.dev", Aliases: []string{"hostname"}, Namespace: "network"}})
	case "ResourceList":
		return toJSON(resourceItems{Type: "HostnameStatuses.net.talos.dev", Namespace: "network", Items: []resourceItem{{ID: "hostname", Namespace: "network", Version: "1", Phase: "running", Updated: now}}})
	case "ResourceGet":
		return toJSON(map[string]string{"yaml": fmt.Sprintf("metadata:\n  namespace: network\n  type: HostnameStatuses.net.talos.dev\n  id: hostname\n  version: 1\nspec:\n  hostname: %s\n  domainname: demo.invalid\n", n.Hostname)})
	}
	return "", errDemoUnavailable
}

func demoLogLine(service string, at time.Time) string {
	return fmt.Sprintf("%s info [%s] Demo: health check successful, node ready", at.UTC().Format(time.RFC3339), service)
}
