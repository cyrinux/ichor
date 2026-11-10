package ichorgo

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	clientconfig "github.com/siderolabs/talos/pkg/machinery/client/config"
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

const (
	newClusterNode   = "10.0.2.11"
	newClusterPoll   = 10 * time.Millisecond
	newClusterStep   = 60 * time.Millisecond
	newClusterK8s    = "1.34.1"
	newClusterDisk   = "/dev/sda"
	newClusterName   = "home"
	newClusterHost   = "cp-1"
)

// newClusterRecorder captures a bootstrap run's listener calls.
type newClusterRecorder struct {
	events      chan newClusterProgress
	talosconfig chan string
	kubeconfig  chan string
	done        chan [2]string
}

func newNewClusterRecorder() *newClusterRecorder {
	return &newClusterRecorder{
		events:      make(chan newClusterProgress, 512),
		talosconfig: make(chan string, 4),
		kubeconfig:  make(chan string, 4),
		done:        make(chan [2]string, 1),
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
	case <-time.After(90 * time.Second):
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

// maintenanceOnlyNode is a fake node in maintenance mode: its API answers unauthenticated
// (the fake never asks for a client certificate) under the "" node target, it has no
// machine status, and nothing is installed. The returned address is the loopback IP the
// fake listens on (the run dials the maintenance API on the default port, so the fake's
// random port is put back by maintenanceDial): what a console shows.
func maintenanceOnlyNode(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.setVersion("", "v1.14.4")

	endpoint, _ := f.serve(t)

	return f, endpoint
}

// installThenBoot makes f behave like a node bootstrapped from maintenance mode: the real
// config apply takes the node down (the install and reboot), then the installed Talos
// answers under its own address and reports itself ready. The kubeconfig is what the
// installed control plane serves; applied receives the config bytes the node was sent.
func installThenBoot(t *testing.T, f *fakeTalos, node string) {
	t.Helper()

	f.mu.Lock()
	f.kubeconfigs = map[string][]byte{node: []byte("apiVersion: v1\nkind: Config\n")}
	f.applied = make(chan string, 4)
	f.mu.Unlock()

	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()

		go func() {
			time.Sleep(newClusterStep)
			f.setVersion(node, "v1.14.4")
			f.setStage(node, runtime.MachineStageInstalling, false)

			time.Sleep(newClusterStep)
			f.setStage(node, runtime.MachineStageRunning, true)
		}()
	}
}

// startNewClusterTest bootstraps the fake node served at endpoint: the console shows its
// host, the maintenance API answers at maintenanceEndpoint and the installed cluster API
// at clusterEndpoint (the fake's two ports; production dials the address itself).
func startNewClusterTest(endpoint string, listener NewClusterListener, timeout time.Duration) *NewClusterRun {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		panic(err)
	}

	return newClusterAt(host, endpoint, endpoint, newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "", listener, timeout)
}

// shortNewClusterWaits keeps the run's bounded waits inside the test's timeout: the
// shipped values are minutes, the fake moves in milliseconds.
func shortNewClusterWaits(t *testing.T) {
	t.Helper()

	apply, grace, ready := newClusterApplyWait, newClusterGraceWait, newClusterReadyWait

	newClusterApplyWait, newClusterGraceWait, newClusterReadyWait = 4*time.Second, 2*time.Second, 2*time.Second

	t.Cleanup(func() {
		newClusterApplyWait, newClusterGraceWait, newClusterReadyWait = apply, grace, ready
	})
}

func TestStartNewClusterHappyPath(t *testing.T) {
	f, address := maintenanceOnlyNode(t)
	installThenBoot(t, f, address)

	// installThenBoot lets the fake record the config bytes the node was sent.

	rec := newNewClusterRecorder()
	startNewClusterTest(address, rec, time.Minute)

	node, errMessage, events := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("bootstrap failed: %s (events %+v)", errMessage, events)
	}

	if wantHost, _, _ := net.SplitHostPort(address); node != wantHost {
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

	if !slices.Contains(kubeconfigs, "Kubeconfig "+address) {
		t.Errorf("the kubeconfig was never asked of the new control plane: %v", kubeconfigs)
	}

	// The talosconfig handed over is a usable os:admin config of the new cluster.
	talosconfig := awaitChannel(t, rec.talosconfig, "no talosconfig was handed over")
	cfg, err := clientconfig.FromBytes([]byte(talosconfig))
	if err != nil {
		t.Fatalf("talosconfig does not parse: %v:\n%s", err, talosconfig)
	}

	ctx := cfg.Contexts[cfg.Context]
	wantHost, _, _ := net.SplitHostPort(address)
	if cfg.Context != newClusterName || len(ctx.Endpoints) != 1 || ctx.Endpoints[0] != wantHost {
		t.Errorf("talosconfig context = %q endpoints %v, want %s at %s", cfg.Context, ctx.Endpoints, newClusterName, wantHost)
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
	rec := newNewClusterRecorder()

	// A port nobody listens on: nothing answered, nothing was changed.
	startNewClusterTest(freeLocalAddr(t), rec, time.Minute)

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



// A node booted from the ISO whose maintenance API is still coming up: the apply retries.
func TestStartNewClusterApplyRetriesWhileNodeBoots(t *testing.T) {
	f, address := maintenanceOnlyNode(t)
	installThenBoot(t, f, address)

	shortNewClusterWaits(t)

	// The node is briefly unreachable: every call without a node target (its maintenance
	// API) waits, then the hold is released.
	hold := make(chan struct{})

	f.mu.Lock()
	f.versionHold = map[string]chan struct{}{"": hold}
	f.mu.Unlock()

	go func() {
		time.Sleep(3 * newClusterPoll)
		f.mu.Lock()
		delete(f.versionHold, "")
		f.mu.Unlock()
		close(hold)
	}()

	rec := newNewClusterRecorder()
	startNewClusterTest(address, rec, time.Minute)

	_, errMessage, _ := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("the retrying apply failed: %s", errMessage)
	}
}

// The node takes the config and never comes back installed: the run says so and says the
// install cannot be undone.
func TestStartNewClusterNeverComesBack(t *testing.T) {
	shortNewClusterWaits(t)

	f, address := maintenanceOnlyNode(t)

	// The node answers as installed and serves a kubeconfig, but never becomes ready.
	f.mu.Lock()
	f.kubeconfigs = map[string][]byte{address: []byte("apiVersion: v1\nkind: Config\n")}
	f.mu.Unlock()

	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()

		go func() {
			time.Sleep(newClusterStep)
			f.setVersion(address, "v1.14.4")
			f.setStage(address, runtime.MachineStageInstalling, false)
		}()
	}

	rec := newNewClusterRecorder()
	startNewClusterTest(address, rec, 20*time.Second)

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "cannot be undone") || !strings.Contains(errMessage, "start the bootstrap again") {
		t.Errorf("err = %q; want a timeout that names the un-undoable install", errMessage)
	}
}

// installedWithoutClusterNode: an installed Talos without a cluster: its API still answers
// unauthenticated and reports a machine status. apply wipes it, then the install follows.
func installedWithoutClusterNode(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f, endpoint := maintenanceOnlyNode(t)

	// The maintenance and the installed API answer under the same host the run dials;
	// both report a machine status (installed), so the run wipes first.
	f.setStage("", runtime.MachineStageRunning, true)
	f.setStage(endpoint, runtime.MachineStageRunning, true)
	f.setVersion(endpoint, "v1.14.4")

	f.mu.Lock()
	f.kubeconfigs = map[string][]byte{endpoint: []byte("apiVersion: v1\nkind: Config\n")}
	f.mu.Unlock()

	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()

		go func() {
			time.Sleep(newClusterStep)
			f.setStage(endpoint, runtime.MachineStageRunning, true)
		}()
	}

	return f, endpoint
}

// dropMachineStatus removes ""'s machine status once the reset was seen: the node is back
// in maintenance mode.
func dropMachineStatus(t *testing.T, f *fakeTalos, dropFrom string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		f.mu.Lock()
		saw := len(f.resets) > 0
		f.mu.Unlock()

		if saw {
			f.mu.Lock()
			defer f.mu.Unlock()

			for _, node := range []string{"", dropFrom} {
				f.resources[node] = slices.DeleteFunc(f.resources[node], func(r resource.Resource) bool {
					return r.Metadata().Type() == "runtime.MachineStatus"
				})
			}

			return
		}

		time.Sleep(newClusterPoll)
	}

	t.Error("the node never took the reset")
}

// Cancel before the config goes out: nothing changed on the node.
func TestStartNewClusterCancelBeforeApply(t *testing.T) {
	shortNewClusterWaits(t)

	f, address := maintenanceOnlyNode(t)

	// The maintenance API never answers (Version holds), so the run is still pre-apply.
	hold := make(chan struct{})

	f.mu.Lock()
	f.versionHold = map[string]chan struct{}{"": hold}
	f.mu.Unlock()

	t.Cleanup(func() {
		select {
		case <-hold:
		default:
			close(hold)
		}
	})

	rec := newNewClusterRecorder()
	run := startNewClusterTest(address, rec, time.Minute)

	// Wait until the run reached the maintenance API, then cancel: its first call is
	// waiting in the held Get.
	deadline := time.Now().Add(10 * time.Second)

	for {
		if len(f.called("Get ")) > 0 || time.Now().After(deadline) {
			break
		}

		time.Sleep(newClusterPoll)
	}

	run.Cancel()

	node, errMessage, _ := rec.wait(t)
	wantHost, _, _ := net.SplitHostPort(address)
	if node != wantHost || !strings.Contains(errMessage, "stopped before the config was sent") {
		t.Errorf("got %q, %q; want a clean stop before the apply", node, errMessage)
	}

	f.mu.Lock()
	applies := slices.Clone(f.applies)
	f.mu.Unlock()

	if len(applies) != 0 {
		t.Errorf("applies = %+v, want none: the run was cancelled before sending", applies)
	}
}

// A node that already carries a clusterless Talos install answers the maintenance reset
// and is wiped before the config goes out.
func TestStartNewClusterWipesInstalledClusterlessNode(t *testing.T) {
	shortNewClusterWaits(t)

	f, address := installedWithoutClusterNode(t)

	// The reset takes the node down and it returns in maintenance mode.
	go dropMachineStatus(t, f, address)

	rec := newNewClusterRecorder()
	startNewClusterTest(address, rec, time.Minute)

	node, errMessage, events := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("wipe bootstrap failed: %s (events %+v)", errMessage, events)
	}

	if wantHost, _, _ := net.SplitHostPort(address); node != wantHost {
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

// StartNewCluster records the run in the activity log without the config.
func TestStartNewClusterAudited(t *testing.T) {
	withDataDir(t)
	shortNewClusterWaits(t)

	f, address := maintenanceOnlyNode(t)
	installThenBoot(t, f, address)

	rec := newNewClusterRecorder()
	StartNewCluster(newClusterNode, newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "", rec)

	_, errMessage, _ := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("bootstrap failed: %s", errMessage)
	}

	entries := readAudit(t, "", "cluster-bootstrap")
	if len(entries) != 1 {
		t.Fatalf("audit entries = %+v, want one", entries)
	}

	e := entries[0]
	if e.Outcome != auditOK || e.Node != address {
		t.Errorf("entry = %+v", e)
	}

	if !strings.Contains(e.Params, "hostname=cp-1") || !strings.Contains(e.Params, "k8s=1.34.1") ||
		!strings.Contains(e.Params, "disk=/dev/sda") || !strings.Contains(e.Params, "endpoint=https://"+address+":6443") {
		t.Errorf("params = %q", e.Params)
	}

	if strings.Contains(e.Params, "BEGIN") || strings.Contains(e.Params, "secrets:") {
		t.Errorf("params carry config: %q", e.Params)
	}
}

