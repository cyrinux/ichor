package ichorgo

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	cosiv1alpha1 "github.com/cosi-project/runtime/api/v1alpha1"
	"github.com/cosi-project/runtime/pkg/resource"
	clusterapi "github.com/siderolabs/talos/pkg/machinery/api/cluster"
	"github.com/siderolabs/talos/pkg/machinery/api/common"
	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	timeapi "github.com/siderolabs/talos/pkg/machinery/api/time"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// fakeTalos is the Talos API of a few nodes behind one endpoint: like apid, it routes each
// call by its "node" metadata. It serves the machine, image, lifecycle and cluster services
// and the COSI state; scripts replace the default answers where a test needs to.
type fakeTalos struct {
	mu        sync.Mutex
	versions  map[string]string              // node -> Talos tag; a node without one is down
	resources map[string][]resource.Resource // node -> its COSI resources
	calls     []string                       // "Method node", in order
	// members are the etcd members (node -> member id); nil: each node is its own only member.
	members map[string]uint64
	leader  uint64
	// etcdErrors are the errors a node's etcd status reports (an unhealthy member).
	etcdErrors map[string][]string

	// Scripts (nil: the default answer).
	upgrade          func(node string, req *machineapi.UpgradeRequest) error
	pull             func(stream grpc.ServerStreamingServer[machineapi.ImageServicePullResponse]) error
	lifecycleUpgrade func(stream grpc.ServerStreamingServer[machineapi.LifecycleServiceUpgradeResponse]) error
	health           func(node string, stream grpc.ServerStreamingServer[clusterapi.HealthCheckProgress]) error
	healthInfos      []*clusterapi.ClusterInfo // the ClusterInfo of each HealthCheck, in order
	snapshot         func(stream grpc.ServerStreamingServer[common.Data]) error
	rebootErr        error
	resets           []*machineapi.ResetRequest   // the Reset requests received, in order
	restarts         []*machineapi.RestartRequest // the container Restart requests, in order
	disarmErr        error
	// alarms are the active etcd alarms; a disarm clears them.
	alarms    []*machineapi.EtcdMemberAlarm
	defragErr map[string]error // node -> its defragmentation's error
	failLogs  string           // the service whose logs cannot be read
	// mounts are a node's mounted filesystems (none: /dev/sda6 on /var, 40 % used).
	mounts map[string][]*machineapi.MountStat
	// systemImages are the images of the system containerd namespace (the CRI one has pause).
	systemImages []*machineapi.ImageServiceListResponse
	// imagePull answers MachineService.ImagePull (nil: Unimplemented, like Talos without the
	// deprecated API, so ImageService.Pull serves the pull).
	imagePull func(node string, req *machineapi.ImagePullRequest) error
	// debugRun answers a debug container run of spec on node (nil: Unimplemented).
	debugRun func(node string, spec *machineapi.DebugContainerRunRequestSpec) (output string, exitCode int32)
	// files are a node's file tree (absolute path -> content), served by List and Read; a
	// node without one lists a fixed directory.
	files map[string]map[string]string
	// cpuFreq is a node's CPUFreqStats answer (a node without an entry: Unimplemented).
	cpuFreq map[string][]*machineapi.CPUFreqStats
	// applies are the ApplyConfiguration requests received, in order; a real (not dry run)
	// one replaces the node's machine config, then onApply runs. applied receives the
	// config bytes of each real apply (tests that care about what the node was sent).
	applies []fakeConfigApply
	applied chan string
	onApply func(node string)
	// etcdDown are the nodes whose etcd does not answer (EtcdStatus: Unavailable) until a
	// recovery bootstraps it.
	etcdDown map[string]bool
	// recovered is what EtcdRecover received, per node; bootstraps the Bootstrap requests.
	recovered  map[string][]byte
	bootstraps []*machineapi.BootstrapRequest
	// pcaps are the pcap streams a node's PacketCapture sends; pcapHold keeps a node's stream
	// open after it (until the capture is stopped), like a live capture.
	pcaps    map[string][]byte
	pcapHold map[string]bool
	// clockOffsets are a node's clock offsets to its NTP server, in ms, for TimeService.Time
	// (a node without one: Unimplemented).
	clockOffsets map[string]int64
	// versionHold holds a node's Version answer until the channel is closed (or the call
	// gives up): a slow node.
	versionHold map[string]chan struct{}
	// kubeconfigs are a node's Kubeconfig answers (a node without one: Unavailable), each
	// sent as one common.Data chunk, gzip+tar wrapped the way Talos sends it.
	kubeconfigs map[string][]byte
	// events answers MachineService.Events for the nodes it targets (nil: Unimplemented).
	events func(nodes []string, req *machineapi.EventsRequest, stream grpc.ServerStreamingServer[machineapi.Event]) error
	// nodeStays answers whether a target with a version still answers at all: a run that
	// reboots the whole machine takes its maintenance API with it, so the test can close
	// that target while the installed one (its own node target) comes up.
	nodeStays func(node string) bool
	// getHold and applyHold hold a node target's state Get / every ApplyConfiguration
	// answer before the fake records anything: a run cancelled while held leaves the
	// fake untouched.
	getHold   map[string]chan struct{}
	applyHold chan struct{}
	// trace (when set) hears every recorded call, for tests that wait on the fake's state.
	trace func(method, node string)
}

type fakeConfigApply struct {
	node   string
	dryRun bool
	mode   machineapi.ApplyConfigurationRequest_Mode
}

func newFakeTalos() *fakeTalos {
	return &fakeTalos{versions: map[string]string{}, resources: map[string][]resource.Resource{}}
}

// addNode adds a running, ready node of the given type with its hostname.
func (f *fakeTalos) addNode(t *testing.T, node, version string, typ machine.Type) {
	t.Helper()

	mt := config.NewMachineType()
	mt.SetMachineType(typ)

	hs := network.NewHostnameStatus(network.NamespaceName, network.HostnameID)
	hs.TypedSpec().Hostname = "host-" + strings.ReplaceAll(node, ".", "-")

	f.mu.Lock()
	f.versions[node] = version
	f.mu.Unlock()

	f.put(node, mt, hs)
	f.setStage(node, runtime.MachineStageRunning, true)
}

// put adds or replaces resources of node.
func (f *fakeTalos) put(node string, rs ...resource.Resource) {
	node = nodeKey(node)

	f.mu.Lock()
	defer f.mu.Unlock()

	for _, r := range rs {
		list := slices.DeleteFunc(f.resources[node], func(old resource.Resource) bool {
			return old.Metadata().Type() == r.Metadata().Type() && old.Metadata().ID() == r.Metadata().ID()
		})
		f.resources[node] = append(list, r)
	}
}

func (f *fakeTalos) setStage(node string, stage runtime.MachineStage, ready bool) {
	node = nodeKey(node)

	ms := runtime.NewMachineStatus()
	ms.TypedSpec().Stage = stage
	ms.TypedSpec().Status.Ready = ready
	f.put(node, ms)
}

func (f *fakeTalos) setVersion(node, version string) {
	node = nodeKey(node)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.versions[node] = version
}

// putMachineConfig gives node a generated machine config.
func (f *fakeTalos) putMachineConfig(t *testing.T, node string) {
	t.Helper()

	raw, err := generatedConfig()
	if err != nil {
		t.Fatal(err)
	}

	provider, err := configloader.NewFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}

	f.put(node, config.NewMachineConfigWithID(provider, config.ActiveID))
}

// enter records the call and tells the node it targets, or why it does not answer.
func (f *fakeTalos) enter(ctx context.Context, method string) (string, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	node := ""
	if v := md.Get("node"); len(v) > 0 {
		node = v[0]
	}

	node = nodeKey(node)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, method+" "+node)

	if f.trace != nil {
		f.trace(method, node)
	}

	if _, up := f.versions[node]; !up {
		return node, status.Errorf(codes.Unavailable, "connection to %s refused", node)
	}

	if f.nodeStays != nil && !f.nodeStays(node) {
		return node, status.Errorf(codes.Unavailable, "connection to %s refused", node)
	}

	return node, nil
}

func (f *fakeTalos) called(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []string

	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}

	return out
}

// waitVersion blocks while the node ctx targets is held (see versionHold).
// waitHold waits for a test's release of a held answer, if any.
func (f *fakeTalos) waitHold(ctx context.Context, hold chan struct{}) {
	if hold == nil {
		return
	}

	select {
	case <-hold:
	case <-ctx.Done():
	}
}

// holdFor returns the held-answer channel of a call's node target, read from metadata.
func (f *fakeTalos) holdFor(ctx context.Context, holds map[string]chan struct{}) chan struct{} {
	if holds == nil {
		return nil
	}

	md, _ := metadata.FromIncomingContext(ctx)

	node := ""
	if v := md.Get("node"); len(v) > 0 {
		node = v[0]
	}

	node = nodeKey(node)

	f.mu.Lock()
	defer f.mu.Unlock()

	if hold, ok := holds[node]; ok || node != "" {
		return hold
	}

	// A call naming no node (the maintenance way) still meets a held answer when the
	// test holds the fake's only target: the hold is per fake, not per dial path.
	for _, hold := range holds {
		return hold
	}

	return nil
}

func (f *fakeTalos) waitVersion(ctx context.Context) {
	// The hold is keyed by node, but a call that names none (the maintenance way) still
	// meets a held answer when the test holds the fake's target (see holdFor).
	hold := f.holdFor(ctx, f.versionHold)
	if hold == nil {
		return
	}

	select {
	case <-hold:
	case <-ctx.Done():
	}
}

// start serves f over TLS on loopback and returns a talosconfig for nodes through it.
func (f *fakeTalos) start(t *testing.T, nodes ...string) string {
	t.Helper()

	endpoint, caB64 := f.serve(t)

	var list strings.Builder
	for _, n := range nodes {
		fmt.Fprintf(&list, "            - %s\n", n)
	}

	return fmt.Sprintf("context: fake\ncontexts:\n    fake:\n        endpoints:\n            - %s\n        ca: %s\n        nodes:\n%s",
		endpoint, caB64, list.String())
}

// registerFakeTalos registers every service f serves on a gRPC server.
func registerFakeTalos(s *grpc.Server, f *fakeTalos) {
	machineapi.RegisterMachineServiceServer(s, fakeTalosMachine{f: f})
	machineapi.RegisterImageServiceServer(s, fakeTalosImage{f: f})
	machineapi.RegisterLifecycleServiceServer(s, fakeTalosLifecycle{f: f})
	machineapi.RegisterDebugServiceServer(s, fakeTalosDebug{f: f})
	clusterapi.RegisterClusterServiceServer(s, fakeTalosCluster{f: f})
	cosiv1alpha1.RegisterStateServer(s, fakeTalosState{f: f})
	timeapi.RegisterTimeServiceServer(s, fakeTalosTime{f: f})
}

// serveCAPort is the loopback port the fake's cluster API (apid) listens on: production
// dials the node at Talos's own API port (50000), so the fake serves the installed
// cluster exactly there and the run reaches it without a test-only port.
const serveCAPort = "127.0.0.1:50000"

// serveCA serves f as the installed cluster's apid would: TLS on the node's own address
// with a certificate that node's secrets issued (its CA is the cluster's OS CA, and the
// run dials it with the admin certificate of the same secrets). apid's certificate
// carries the endpoint as an IP SAN: a real cluster's certificate carries its LAN IPs.
func (f *fakeTalos) serveCA(t *testing.T, bundle *secrets.Bundle) (endpoint, caPEM string) {
	t.Helper()

	crt, key, ca := clusterAPICert(t, bundle)

	leaf, err := tls.X509KeyPair(crt, key)
	if err != nil {
		t.Fatal(err)
	}

	lis, err := net.Listen("tcp", serveCAPort)
	if err != nil {
		t.Fatal(err)
	}

	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{leaf}, MinVersion: tls.VersionTLS12,
	})))
	registerFakeTalos(server, f)

	go server.Serve(lis) //nolint:errcheck

	t.Cleanup(server.Stop)

	return lis.Addr().String(), string(ca)
}

// clusterAPICert issues the leaf certificate apid serves: a certificate signed by the
// cluster's OS CA (so the run, which dials with the same CA, verifies it) and carrying
// 127.0.0.1 as an IP SAN (the node's own address, as a real cluster carries its LAN IPs).
func clusterAPICert(t *testing.T, bundle *secrets.Bundle) (crtPEM, keyPEM, caPEM []byte) {
	t.Helper()

	caBlock, _ := pem.Decode(bundle.Certs.OS.Crt)
	if caBlock == nil {
		t.Fatal("the OS CA certificate is not PEM")
	}

	ca, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}

	caKey, err := parseCAKey(bundle.Certs.OS.Key)
	if err != nil {
		t.Fatal(err)
	}

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "talos"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
		bundle.Certs.OS.Crt
}

// parseCAKey decodes the OS CA's private key, whose PEM block type depends on the key
// algorithm Talos generated it with (v1.x uses PKCS8).
func parseCAKey(keyPEM []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("no PEM block in the OS CA key")
	}

	var key any

	var err error

	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	}

	if err != nil {
		return nil, err
	}

	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("the OS CA key of type %T cannot sign", key)
	}

	return signer, nil
}

// nodeKey names a node the way the fake keys its state: by host, without any port. The
// run addresses a node by its bare address ("127.0.0.1"), while tests often speak of the
// endpoint the fake listens on ("127.0.0.1:50001"): both name the same machine.
func nodeKey(node string) string {
	if host, _, err := net.SplitHostPort(node); err == nil {
		return host
	}

	return node
}

// serve serves f over TLS on loopback, without client authentication (like a node in
// maintenance mode), and returns its endpoint and CA.
func (f *fakeTalos) serve(t *testing.T) (endpoint, caB64 string) {
	t.Helper()

	return startTLSServer(t, func(s *grpc.Server) { registerFakeTalos(s, f) })
}
