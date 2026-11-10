package ichorgo

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	machineryconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
	"google.golang.org/grpc"
)

const (
	newClusterNode = "10.0.2.11"
	// newClusterPoll is the run's polling step; newClusterStep is how long the fake
	// node stays down between its stages. Both stay well inside the run's context
	// deadlines (the maintenance calls answer in nodeTimeout, the rest in the run's
	// own timeout), so a cancel lands where the test expects instead of the run
	// burning a whole call deadline first.
	newClusterPoll = 10 * time.Millisecond
	newClusterStep = 30 * time.Millisecond
	newClusterK8s  = "1.34.1"
	newClusterDisk = "/dev/sda"
	newClusterName = "home"
	newClusterHost = "cp-1"
)

// newClusterRecorder captures a bootstrap run's listener calls.
type newClusterRecorder struct {
	events      chan newClusterProgress
	talosconfig chan string
	kubeconfig  chan string
	done        chan [2]string
	ended       chan struct{} // closed once OnDone ran: the run's goroutine is past its last read
	endOnce     sync.Once
}

func newNewClusterRecorder() *newClusterRecorder {
	return &newClusterRecorder{
		events:      make(chan newClusterProgress, 512),
		talosconfig: make(chan string, 4),
		kubeconfig:  make(chan string, 4),
		done:        make(chan [2]string, 1),
		ended:       make(chan struct{}),
	}
}

func (r *newClusterRecorder) OnProgress(js string) {
	var p newClusterProgress
	if err := json.Unmarshal([]byte(js), &p); err == nil {
		r.events <- p
	}
}

func (r *newClusterRecorder) OnTalosconfig(yaml string) { r.talosconfig <- yaml }
func (r *newClusterRecorder) OnKubeconfig(yaml string)  { r.kubeconfig <- yaml }

func (r *newClusterRecorder) OnDone(nodeAddress, errMessage string) {
	r.done <- [2]string{nodeAddress, errMessage}

	r.endOnce.Do(func() { close(r.ended) })
}

// stopRunAtCleanup cancels the run and waits for it to end when the test returns (even
// by a Fatal), so the run's goroutine never reads package state a cleanup registered
// before this one (shortNewClusterWaits' restore) is writing back.
func stopRunAtCleanup(t *testing.T, run *NewClusterRun, rec *newClusterRecorder) {
	t.Helper()

	t.Cleanup(func() {
		run.Cancel()

		select {
		case <-rec.ended:
		case <-time.After(30 * time.Second):
			t.Error("the run did not stop after its cancel")
		}
	})
}

// wait returns the run's end and every event before it.
func (r *newClusterRecorder) wait(t *testing.T) (address, errMessage string, events []newClusterProgress) {
	t.Helper()

	select {
	case d := <-r.done:
		close(r.events)

		for e := range r.events {
			events = append(events, e)
		}

		return d[0], d[1], events
	case <-time.After(120 * time.Second):
		t.Fatal("the bootstrap never ended")

		return "", "", nil
	}
}

func newClusterPhasesOf(events []newClusterProgress) []string {
	return slices.Collect(func(yield func(string) bool) {
		for _, e := range events {
			if !yield(e.Phase) {
				return
			}
		}
	})
}

// maintenanceNodePair is a fake metal node in maintenance mode: two ports behind one
// host, the way Sidero's Metal API and apid sit. The maintenance port answers
// unauthenticated (cleartext, the way the insecure client reaches it) and only under the
// "" node target (the maintenance API is dialed naming no node); it has no machine
// status, nothing is installed, and its cluster port answers nothing at all until the
// install brings apid up. The returned addresses are the two ports' host:port.
func maintenanceNodePair(t *testing.T) (*fakeTalos, string, string) {
	t.Helper()

	// The cluster API (apid) serves TLS under the cluster's own CA, and the run dials
	// it with the admin certificate those secrets issue: both come from the bundle the
	// test run shares with the run itself.
	f, maintenance := maintenanceNodeWith(t, sharedNewClusterBundle(t))

	return f, maintenance, serveCAPort
}

// maintenanceNodeWith is maintenanceNodePair for a run that brings its own secrets (a
// prepared draft): apid serves under bundle, the maintenance API answers beside it.
func maintenanceNodeWith(t *testing.T, bundle *secrets.Bundle) (f *fakeTalos, maintenance string) {
	t.Helper()

	f = newFakeTalos()
	f.setVersion("", "v1.14.4")

	f.serveCA(t, bundle)

	maintenance, _ = startTLSServer(t, func(grpcServer *grpc.Server) {
		registerFakeTalos(grpcServer, f)
	})

	return f, maintenance
}

// sharedNewClusterBundle makes the one bundle the package's runs share (the run's
// ensureBundle takes it from newClusterSecretsCache): apid's certificate authority and
// the admin certificate the run dials it with must come from the same secrets.
func sharedNewClusterBundle(t *testing.T) *secrets.Bundle {
	t.Helper()

	if newClusterSecretsCache == nil {
		bundle, err := secrets.NewBundle(secrets.NewClock(), machineryconfig.TalosVersionCurrent)
		if err != nil {
			t.Fatal(err)
		}

		newClusterSecretsCache = bundle
	}

	return newClusterSecretsCache
}

// startNewClusterTest runs StartNewCluster's test twin as the user typed it: the console
// address is the maintenance port (the address they dial), the maintenance API answers
// there, and the installed cluster API answers on the node's own port.
func startNewClusterTest(maintenance, cluster string, listener NewClusterListener, timeout time.Duration) *NewClusterRun {
	return newClusterAtEndpoint(maintenance, maintenance, cluster, listener, timeout)
}

func newClusterAtEndpoint(endpoint, installTarget, clusterAddress string, listener NewClusterListener, timeout time.Duration) *NewClusterRun {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		panic(err)
	}

	return newClusterAt(host, installTarget, clusterAddress, newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "", listener, timeout)
}

// installThenBoot makes f behave like a metal node bootstrapped from maintenance mode:
// the config goes out over the maintenance target (the "" name the maintenance API is
// dialed with), the machine reboots into its install (every port refuses, as the machine
// is down), then its apid answers under the node's own name (installed Talos: a machine
// status, a version) and reports itself ready. applied receives the config bytes the
// node was sent.
func installThenBoot(t *testing.T, f *fakeTalos, node, clusterPort string) {
	t.Helper()

	f.mu.Lock()
	f.applied = make(chan string, 4)
	f.mu.Unlock()

	f.onApply = func(string) {
		// The install reboots the machine: both ports go quiet, then the maintenance
		// API (the ISO's) answers again while Talos installs.
		f.mu.Lock()
		f.nodeStays = func(target string) bool { return target == "" }
		f.mu.Unlock()

		go func() {
			time.Sleep(2 * newClusterStep)

			// apid is up: the installed Talos answers under the node's own name.
			f.mu.Lock()
			f.nodeStays = nil
			f.mu.Unlock()

			f.setVersion(node, "v1.14.4")
			f.setStage(node, runtime.MachineStageInstalling, false)

			time.Sleep(newClusterStep)
			f.setStage(node, runtime.MachineStageRunning, true)
		}()
	}
}

// serveKubeconfigAfterBootstrap makes f answer Kubeconfig once its first Bootstrap came
// in (the kubeconfig the cluster serves only exists once etcd is up), newClusterStep
// later: the run's kubeconfig ask retries until then.
func serveKubeconfigAfterBootstrap(t *testing.T, f *fakeTalos, node string) {
	t.Helper()

	go func() {
		deadline := time.Now().Add(30 * time.Second)

		for {
			f.mu.Lock()
			n := len(f.bootstraps)
			f.mu.Unlock()

			if n > 0 {
				break
			}

			if time.Now().After(deadline) {
				t.Error("the cluster was never bootstrapped")

				return
			}

			time.Sleep(newClusterPoll)
		}

		f.mu.Lock()
		f.kubeconfigs = map[string][]byte{nodeKey(node): []byte("apiVersion: v1\nkind: Config\n")}
		f.mu.Unlock()
	}()
}

// downUntil brings the fake down until the test releases hold: like a node whose
// maintenance API is still coming up (or rebooting), where every call is refused.
func downUntil(f *fakeTalos, hold chan struct{}) {
	f.mu.Lock()
	f.nodeStays = func(string) bool { return false }
	f.mu.Unlock()

	go func() {
		<-hold

		// The machine is back: the fake answers every target again, as it did before.
		f.mu.Lock()
		defer f.mu.Unlock()

		f.nodeStays = nil
	}()
}

func release(hold chan struct{}) {
	select {
	case <-hold:
	default:
		close(hold)
	}
}

// shortNewClusterWaits keeps the run's bounded waits inside the test's timeout: the
// shipped values are minutes, the fake moves in milliseconds.
func shortNewClusterWaits(t *testing.T) {
	t.Helper()

	apply, grace, ready := newClusterApplyWait, newClusterGraceWait, newClusterReadyWait
	interval := newClusterTestInterval

	newClusterApplyWait, newClusterGraceWait, newClusterReadyWait = 8*time.Second, 5*time.Second, 5*time.Second
	newClusterTestInterval = newClusterPoll

	t.Cleanup(func() {
		newClusterApplyWait, newClusterGraceWait, newClusterReadyWait = apply, grace, ready
		newClusterTestInterval = interval
	})
}

func TestStartNewClusterHappyPath(t *testing.T) {
	shortNewClusterWaits(t)

	f, maintenance, cluster := maintenanceNodePair(t)
	installThenBoot(t, f, cluster, cluster)
	serveKubeconfigAfterBootstrap(t, f, cluster)

	rec := newNewClusterRecorder()
	startNewClusterTest(maintenance, cluster, rec, time.Minute)

	node, errMessage, events := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("bootstrap failed: %s (events %+v)", errMessage, events)
	}

	if wantHost, _, _ := net.SplitHostPort(maintenance); node != wantHost {
		t.Errorf("node = %q, want %s", node, wantHost)
	}

	want := []string{newClusterPhaseGenerating, newClusterPhaseApplying, newClusterPhaseApplying,
		newClusterPhaseInstalling, newClusterPhaseBooting, newClusterPhaseReady,
		newClusterPhaseBootstrap, newClusterPhaseIssuing, newClusterPhaseDone}
	if got := newClusterPhasesOf(events); !slices.Equal(got, want) {
		t.Errorf("phases = %v, want %v", got, want)
	}

	// The config reached the maintenance API exactly once, for real.
	f.mu.Lock()
	applies := slices.Clone(f.applies)
	bootstraps := slices.Clone(f.bootstraps)
	kubeconfigs := slices.Clone(f.calls)
	f.mu.Unlock()

	if len(applies) != 1 || applies[0].node != "" || applies[0].dryRun || applies[0].mode != machineapi.ApplyConfigurationRequest_AUTO {
		t.Errorf("applies = %+v, want one real AUTO apply to the maintenance API", applies)
	}

	if len(bootstraps) != 1 {
		t.Errorf("bootstraps = %+v, want one", bootstraps)
	}

	if !slices.Contains(kubeconfigs, "Kubeconfig "+nodeKey(cluster)) {
		t.Errorf("the kubeconfig was never asked of the new control plane: %v", kubeconfigs)
	}

	// The talosconfig handed over is a usable os:admin config of the new cluster: its
	// endpoints carry the console host and the port of the API that answered.
	talosconfig := awaitChannel(t, rec.talosconfig, "no talosconfig was handed over")
	cfg, err := clientconfig.FromBytes([]byte(talosconfig))
	if err != nil {
		t.Fatalf("talosconfig does not parse: %v:\n%s", err, talosconfig)
	}

	ctx := cfg.Contexts[cfg.Context]
	wantHost, _, _ := net.SplitHostPort(maintenance)
	if cfg.Context != newClusterName || len(ctx.Endpoints) != 1 || ctx.Endpoints[0] != cluster {
		t.Errorf("talosconfig context = %q endpoints %v, want %s at %s", cfg.Context, ctx.Endpoints, newClusterName, cluster)
	}

	if _, port, _ := net.SplitHostPort(ctx.Endpoints[0]); port == "" || ctx.Endpoints[0] == wantHost {
		t.Errorf("talosconfig endpoint %q lost the port of the cluster API", ctx.Endpoints[0])
	}

	for _, part := range []string{ctx.CA, ctx.Crt, ctx.Key} {
		if _, err := base64.StdEncoding.DecodeString(part); err != nil || part == "" {
			t.Errorf("talosconfig carries a bad base64 certificate part: %v", err)
		}
	}

	kubeconfig := awaitChannel(t, rec.kubeconfig, "no kubeconfig was handed over")
	if !strings.Contains(kubeconfig, "kind: Config") {
		t.Errorf("kubeconfig = %q", kubeconfig)
	}

	// The config the node was sent carries the hostname and the disk, and progress lines
	// carry no secret of the bundle.
	sent := awaitChannel(t, f.applied, "the fake never stored an applied config")

	if !strings.Contains(sent, newClusterHost) || !strings.Contains(sent, newClusterDisk) {
		t.Errorf("the applied config misses hostname or disk:\n%.200s", sent)
	}

	for _, e := range events {
		if strings.Contains(e.Message, "BEGIN") || strings.Contains(e.Message, "secret") {
			t.Errorf("a progress line carries config or secrets: %q", e.Message)
		}
	}
}

func awaitChannel(t *testing.T, ch chan string, fail string) string {
	t.Helper()

	select {
	case s := <-ch:
		return s
	case <-time.After(10 * time.Second):
		t.Fatal(fail)

		return ""
	}
}

func TestStartNewClusterRefusesInstalledClusterNode(t *testing.T) {
	// An installed Talos whose API asks for a client certificate belongs to a cluster.
	pair, _ := testServerCert(t)

	lis, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS12,
		NextProtos: []string{"h2"},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = lis.Close() })

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

	rec := newNewClusterRecorder()
	address := lis.Addr().String()
	newClusterForTest(address, newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "", rec, time.Minute)

	node, errMessage, _ := rec.wait(t)
	if node != "127.0.0.1" || !strings.Contains(errMessage, "belongs to a cluster") {
		t.Errorf("got %q, %q; want the refusal of a cluster node", node, errMessage)
	}
}

func TestStartNewClusterNothingThere(t *testing.T) {
	shortNewClusterWaits(t)

	answer := newClusterAnswerWait
	newClusterAnswerWait = 200 * time.Millisecond
	t.Cleanup(func() { newClusterAnswerWait = answer })

	rec := newNewClusterRecorder()

	// A port nobody listens on: nothing answered, nothing was changed.
	dead := freeLocalAddr(t)
	startNewClusterTest(dead, dead, rec, time.Minute)

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "does not answer") || !strings.Contains(errMessage, "ISO") {
		t.Errorf("err = %q; want the ISO guidance for a node that never answered", errMessage)
	}
}

func freeLocalAddr(t *testing.T) string {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := lis.Addr().String()
	_ = lis.Close() //nolint:errcheck

	return addr
}

// A node booted from the ISO whose maintenance API is still coming up: the run waits out
// the unreachable stretch (the handshake and the apply retry) and bootstraps it.
func TestStartNewClusterApplyRetriesWhileNodeBoots(t *testing.T) {
	shortNewClusterWaits(t)

	f, maintenance, cluster := maintenanceNodePair(t)
	installThenBoot(t, f, cluster, cluster)
	serveKubeconfigAfterBootstrap(t, f, cluster)

	hold := make(chan struct{})
	downUntil(f, hold)
	t.Cleanup(func() { release(hold) })

	go func() {
		time.Sleep(15 * newClusterPoll)
		release(hold)
	}()

	rec := newNewClusterRecorder()
	startNewClusterTest(maintenance, cluster, rec, time.Minute)

	_, errMessage, _ := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("the retrying apply failed: %s", errMessage)
	}
}

// The node takes the config and never comes back installed: the run says so and says the
// install cannot be undone.
func TestStartNewClusterNeverComesBack(t *testing.T) {
	shortNewClusterWaits(t)

	f, maintenance, cluster := maintenanceNodePair(t)

	// The node comes back installed and stays installing forever (the maintenance API
	// stays up the way the ISO's does while Talos installs).
	f.onApply = func(string) {
		go func() {
			time.Sleep(newClusterStep)
			f.setVersion(cluster, "v1.14.4")
			f.setStage(cluster, runtime.MachineStageInstalling, false)
		}()
	}

	rec := newNewClusterRecorder()
	startNewClusterTest(maintenance, cluster, rec, 10*time.Second)

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "cannot be undone") || !strings.Contains(errMessage, "start the bootstrap again") {
		t.Errorf("err = %q; want a timeout that names the un-undoable install", errMessage)
	}
}

// installedWithoutClusterNode: an installed Talos without a cluster: its API still answers
// unauthenticated and reports a machine status. The run wipes it (the reset takes the
// whole node down as it reboots into maintenance mode) and installs it.
func installedWithoutClusterNode(t *testing.T) (*fakeTalos, string, string) {
	t.Helper()

	f, maintenance, cluster := maintenanceNodePair(t)
	// An installed Talos without a cluster: its maintenance API still answers
	// unauthenticated and a machine status answers the un-named calls, which is how the
	// run sees it and wipes it first.
	f.setStage("", runtime.MachineStageRunning, true)

	f.mu.Lock()
	f.applied = make(chan string, 4)
	f.mu.Unlock()

	f.onApply = func(string) {
		go func() {
			// After the wipe the config installs Talos afresh: the machine reboots
			// (the maintenance API comes back with nothing installed), then the
			// installed Talos answers under the node's own name, installing then
			// running.
			f.mu.Lock()
			f.nodeStays = func(target string) bool { return target == "" }
			f.mu.Unlock()

			time.Sleep(2 * newClusterStep)

			f.mu.Lock()
			f.nodeStays = nil
			f.mu.Unlock()

			f.setVersion(cluster, "v1.14.4")
			f.setStage(cluster, runtime.MachineStageInstalling, false)

			time.Sleep(newClusterStep)
			f.setStage(cluster, runtime.MachineStageRunning, true)
		}()
	}

	return f, maintenance, cluster
}

// wipeTakesNodeDown: the reset reboots the node, so the maintenance target (its API and
// machine state) disappears until the wipe is seen; from there only the installed node
// target answers, until the config's onApply takes over.
func wipeTakesNodeDown(t *testing.T, f *fakeTalos) {
	t.Helper()

	go func() {
		deadline := time.Now().Add(10 * time.Second)

		for {
			f.mu.Lock()
			saw := len(f.resets) > 0
			f.mu.Unlock()

			if saw {
				break
			}

			if time.Now().After(deadline) {
				t.Error("the node never took the reset")

				return
			}

			time.Sleep(newClusterPoll)
		}

		// The reset reboots the machine: every target goes quiet, then the maintenance
		// API answers again with nothing installed (the wipe cleared its state).
		f.mu.Lock()
		f.nodeStays = func(string) bool { return false }
		f.mu.Unlock()

		time.Sleep(newClusterStep)

		f.mu.Lock()
		defer f.mu.Unlock()

		f.nodeStays = nil
		f.resources[""] = nil
	}()
}

// A node that already carries a clusterless Talos install answers the maintenance reset
// and is wiped before the config goes out.
func TestStartNewClusterWipesInstalledClusterlessNode(t *testing.T) {
	shortNewClusterWaits(t)

	f, maintenance, cluster := installedWithoutClusterNode(t)
	wipeTakesNodeDown(t, f)
	serveKubeconfigAfterBootstrap(t, f, cluster)

	rec := newNewClusterRecorder()
	startNewClusterTest(maintenance, cluster, rec, time.Minute)

	node, errMessage, events := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("wipe bootstrap failed: %s (events %+v)", errMessage, events)
	}

	if wantHost, _, _ := net.SplitHostPort(maintenance); node != wantHost {
		t.Errorf("node = %q, want %s", node, wantHost)
	}

	f.mu.Lock()
	resets := slices.Clone(f.resets)
	f.mu.Unlock()

	if len(resets) != 1 || !resets[0].GetReboot() || len(resets[0].GetSystemPartitionsToWipe()) == 0 {
		t.Errorf("resets = %+v, want one rebooting system wipe", resets)
	}

	wiped := slices.ContainsFunc(events, func(e newClusterProgress) bool {
		return strings.Contains(e.Message, "resetting it first")
	})

	if !wiped {
		t.Errorf("no wipe notice in %+v", events)
	}
}

func TestStartNewClusterPrivacyRefused(t *testing.T) {
	withDataDir(t)
	SetPrivacyMask(true, "")
	t.Cleanup(func() { SetPrivacyMask(false, "") })

	rec := newNewClusterRecorder()
	newClusterForTest(newClusterNode, newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "", rec, time.Minute)

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "privacy mode") {
		t.Errorf("err = %q; want the privacy refusal", errMessage)
	}
}

func TestStartNewClusterValidation(t *testing.T) {
	cases := []struct{ name, address, cluster, k8s, disk, hostname, endpoint, want string }{
		{"address", "", newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "", "the address must be"},
		{"address junk", "10.0.2.11/others", newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "", "the address must be"},
		{"name", newClusterNode, " ", newClusterK8s, newClusterDisk, newClusterHost, "", "needs a name"},
		{"hostname", newClusterNode, newClusterName, newClusterK8s, newClusterDisk, "bad host!", "", "not a host name"},
		{"disk", newClusterNode, newClusterName, newClusterK8s, "sda", newClusterHost, "", "device path"},
		{"k8s", newClusterNode, newClusterName, "1.34.1-rc1", newClusterDisk, newClusterHost, "", "Kubernetes version"},
		{"k8s major", newClusterNode, newClusterName, "2.0.0", newClusterDisk, newClusterHost, "", "Kubernetes version"},
		{"endpoint", newClusterNode, newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "http://x", "https:// URL"},
		{"endpoint port", newClusterNode, newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "https://10.0.2.10:abc", "bad port"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newNewClusterRecorder()
			newClusterForTest(tc.address, tc.cluster, tc.k8s, tc.disk, tc.hostname, tc.endpoint, rec, 5*time.Second)

			_, errMessage, _ := rec.wait(t)
			if !strings.Contains(errMessage, tc.want) {
				t.Errorf("err = %q, want it to mention %q", errMessage, tc.want)
			}
		})
	}
}

func TestParseNewClusterRequestDefaults(t *testing.T) {
	req, err := parseNewClusterRequest("10.0.2.11:50000", " home ", "v1.34.1", "/dev/nvme0n1", "cp-1", "")
	if err != nil {
		t.Fatal(err)
	}

	if req.host != "10.0.2.11" || req.endpoint != "https://10.0.2.11:6443" {
		t.Errorf("req = %+v; want host 10.0.2.11 and the node's own endpoint", req)
	}

	if req.kubernetesVersion != "1.34.1" {
		t.Errorf("kubernetesVersion = %q", req.kubernetesVersion)
	}

	// validHostPort refuses a bracketed IPv6 without a port; with one it passes.
	if _, err := parseNewClusterRequest("[fd00::5]:50000", newClusterName, "1.34", newClusterDisk, newClusterHost, ""); err != nil {
		t.Errorf("ipv6 with port: %v", err)
	}

	if err := checkClusterEndpoint("https://cluster.example.com"); err != nil {
		t.Errorf("dns endpoint: %v", err)
	}

	if err := checkClusterEndpoint("https://[fd00::5]:6443"); err != nil {
		t.Errorf("ipv6 endpoint: %v", err)
	}
}

// Cancel before the config goes out: nothing changed on the node. The maintenance API
// holds its answer (the fake records the call, then waits) while the run is still
// pre-apply, so cancelling lands in between.
func TestStartNewClusterCancelBeforeApply(t *testing.T) {
	shortNewClusterWaits(t)

	f, maintenance, cluster := maintenanceNodePair(t)

	// The maintenance API holds its state Get (the call that decides whether the node is
	// an installed Talos to wipe), so the run is still pre-apply when the cancel lands:
	// nothing was written to the node.
	hold := make(chan struct{})
	f.mu.Lock()
	f.getHold = map[string]chan struct{}{"": hold}
	f.mu.Unlock()

	t.Cleanup(func() { release(hold) })

	rec := newNewClusterRecorder()
	run := startNewClusterTest(maintenance, cluster, rec, time.Minute)

	// Let the run reach and park in the held state Get (the held call is recorded only
	// once it answers, so its success is not observable yet), then cancel: nothing has
	// been written to the node.
	time.Sleep(4 * newClusterPoll)

	run.Cancel()

	node, errMessage, _ := rec.wait(t)
	wantHost, _, _ := net.SplitHostPort(maintenance)
	if node != wantHost || !strings.Contains(errMessage, "stopped before the config was sent") {
		t.Errorf("got %q, %q; want a clean stop before the apply", node, errMessage)
	}

	// The held call never went through: nothing was applied.
	f.mu.Lock()
	applies := slices.Clone(f.applies)
	f.mu.Unlock()

	if len(applies) != 0 {
		t.Errorf("applies = %+v, want none: the run was cancelled before sending", applies)
	}
}

// Cancel while the node is being followed after the apply: the config was sent, so the
// run says the install cannot be undone and points at starting it again.
func TestStartNewClusterCancelWhileFollowing(t *testing.T) {
	shortNewClusterWaits(t)

	f, maintenance, cluster := maintenanceNodePair(t)

	f.mu.Lock()
	f.applied = make(chan string, 4)
	f.mu.Unlock()

	// The node takes the config, comes back installed and stays installing: the run
	// follows it, and the cancel lands mid-follow.
	f.onApply = func(string) {
		go func() {
			time.Sleep(newClusterStep)
			f.setVersion(cluster, "v1.14.4")
			f.setStage(cluster, runtime.MachineStageInstalling, false)
		}()
	}

	rec := newNewClusterRecorder()
	run := startNewClusterTest(maintenance, cluster, rec, time.Minute)

	// The apply happened (the channel receives the config bytes when it does); cancel
	// mid-follow, while the node installs.
	select {
	case <-f.applied:
	case <-time.After(10 * time.Second):
		t.Fatal("the node never took the config")
	}

	run.Cancel()

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "cannot be undone") {
		t.Errorf("err = %q; want the un-undoable install message", errMessage)
	}
}

// StartNewCluster records the run in the activity log without the config.
func TestStartNewClusterAudited(t *testing.T) {
	withDataDir(t)
	shortNewClusterWaits(t)

	// The production entry polls at upgradePollInterval (5s): shorten it, as the upgrade
	// tests do, so the run follows the fake's reboot within the test's waits.
	interval := upgradePollInterval
	upgradePollInterval = newClusterPoll

	t.Cleanup(func() { upgradePollInterval = interval })

	spec, err := toJSON(newClusterSpec{
		Name: clusterNameSpec(), Endpoint: "127.0.0.1", KubernetesVersion: newClusterK8s,
		TalosVersion: "v1.14.4", InstallDisk: newClusterDisk,
	})
	if err != nil {
		t.Fatal(err)
	}

	prepared, err := NewClusterPrepare(spec)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}

	var draft newClusterDraft
	if err := fromJSON(prepared, &draft); err != nil {
		t.Fatal(err)
	}

	// The audited run is the production entry: it dials the node for both the maintenance
	// API and apid (the fake's cluster API answers at Talos's own API port, see serveCA).
	// The draft made its own secrets, and the talosconfig it hands the run is signed by
	// them: apid must serve under that bundle, not the tests' shared one.
	newClusterDraftsMu.Lock()
	bundle := newClusterDrafts[draft.DraftID].bundle
	newClusterDraftsMu.Unlock()

	f, endpoint := maintenanceNodeWith(t, bundle)
	installThenBoot(t, f, serveCAPort, serveCAPort)
	serveKubeconfigAfterBootstrap(t, f, serveCAPort)

	rec := newNewClusterRecorder()
	stopRunAtCleanup(t, StartNewCluster(draft.DraftID, endpoint, draft.Talosconfig, rec), rec)

	_, errMessage, _ := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("bootstrap failed: %s", errMessage)
	}

	entries := readAudit(t, "", "cluster-bootstrap")
	if len(entries) != 1 {
		t.Fatalf("audit entries = %+v, want one", entries)
	}

	e := entries[0]
	if e.Outcome != auditOK || e.Node != nodeKey(endpoint) {
		t.Errorf("entry = %+v", e)
	}

	if !strings.Contains(e.Params, "hostname="+clusterNameSpec()) || !strings.Contains(e.Params, "k8s="+newClusterK8s) ||
		!strings.Contains(e.Params, "disk="+newClusterDisk) {
		t.Errorf("params = %q", e.Params)
	}

	if strings.Contains(e.Params, "BEGIN") || strings.Contains(e.Params, "secrets:") {
		t.Errorf("params carry config: %q", e.Params)
	}
}

// clusterNameSpec is the cluster name the audited test uses.
func clusterNameSpec() string { return "home-lab" }

// TestNewClusterDebugDump: temporary diagnostic, not a test of behavior: it runs the
// happy path once and dumps every call the fake saw and when.
func TestNewClusterDebugDump(t *testing.T) {
	shortNewClusterWaits(t)

	f, maintenance, cluster := maintenanceNodePair(t)
	installThenBoot(t, f, cluster, cluster)
	serveKubeconfigAfterBootstrap(t, f, cluster)

	start := time.Now()

	f.mu.Lock()
	f.trace = func(method, node string) {
		t.Logf("%6dms %-28s node=%q", time.Since(start).Milliseconds(), method, node)
	}
	f.mu.Unlock()

	rec := newNewClusterRecorder()
	startNewClusterTest(maintenance, cluster, rec, time.Minute)

	_, errMessage, events := rec.wait(t)
	t.Logf("done after %dms: err=%q", time.Since(start).Milliseconds(), errMessage)

	for _, e := range events {
		t.Logf("event %s: %s", e.Phase, e.Message)
	}
}

// TestNewClusterFakeTalks proves the two-port fake answers the clients the run uses:
// the maintenance client on the maintenance port (the "" target) and the cluster client
// (the run's own admin config) on the cluster port under the node's own name.
func TestNewClusterFakeTalks(t *testing.T) {
	f, maintenance, cluster := maintenanceNodePair(t)
	f.setVersion(cluster, "v1.14.4")
	f.setStage(cluster, runtime.MachineStageRunning, true)

	m, err := maintenanceClient(t.Context(), maintenance)
	if err != nil {
		t.Fatal(err)
	}

	defer m.Close() //nolint:errcheck

	if _, err := m.Version(context.Background()); err != nil {
		t.Errorf("maintenance Version: %v", err)
	}

	c := &newCluster{req: newClusterRequest{address: cluster, host: "127.0.0.1"}, clusterAddress: cluster}
	if err := c.ensureBundle(); err != nil {
		t.Fatal(err)
	}

	cl, err := c.dialCluster()
	if err != nil {
		t.Fatalf("dialCluster: %v", err)
	}

	defer cl.Close() //nolint:errcheck

	if _, err := cl.Version(withRunNode(context.Background(), "127.0.0.1")); err != nil {
		t.Errorf("cluster Version under the node name: %v", err)
	}

	if !c.reportedReady(context.Background(), "127.0.0.1") {
		t.Errorf("the ready node was not reported ready")
	}
}
