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
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
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
// machine status, and nothing is installed. The returned address is its host:port; the
// run's console address is that host, so everything a user would see matches.
func maintenanceOnlyNode(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.setVersion("", "v1.14.4")

	endpoint, _ := f.serve(t)

	return f, endpoint
}

// newClusterAtEndpoint runs StartNewCluster's test twin against a fake served at
// endpoint: the console shows the endpoint's host, the maintenance API (and, until an
// install takes the whole node down, the installed API) answers at endpoint.
func newClusterAtEndpoint(endpoint, installTarget, clusterAddress string, listener NewClusterListener, timeout time.Duration) *NewClusterRun {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		panic(err)
	}

	return newClusterAt(host, installTarget, clusterAddress, newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "", listener, timeout)
}

func startNewClusterTest(endpoint string, listener NewClusterListener, timeout time.Duration) *NewClusterRun {
	return newClusterAtEndpoint(endpoint, endpoint, endpoint, listener, timeout)
}

// installThenBoot makes f behave like a node bootstrapped from maintenance mode: the
// real config apply takes the node down (the install and reboot: the whole fake, its
// maintenance API included, like a machine that reboots), then the installed Talos
// answers under its node target (the fake's endpoint, which the caller dials) and
// reports itself ready. The kubeconfig is what the installed control plane serves;
// applied receives the config bytes the node was sent. The node target carries a
// machine status without a kubeconfig at first: a control plane whose API server is
// still coming up, so the kubeconfig ask retries until Talos is ready and serving.
func installThenBoot(t *testing.T, f *fakeTalos, node string) {
	t.Helper()

	f.mu.Lock()
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

			time.Sleep(newClusterStep)
			f.mu.Lock()
			f.kubeconfigs = map[string][]byte{node: []byte("apiVersion: v1\nkind: Config\n")}
			f.mu.Unlock()
		}()
	}
}

// downUntil brings the fake down until the test releases hold: like a node whose
// maintenance API is still coming up (or rebooting), where every call is refused.
func downUntil(f *fakeTalos, hold chan struct{}) {
	f.mu.Lock()
	saved, up := f.versions[""]
	delete(f.versions, "")
	f.mu.Unlock()

	go func() {
		<-hold

		if !up {
			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		f.versions[""] = saved
	}()
}

// holdVersion keeps a node's Version answer waiting (the call records first, then holds)
// until the test closes hold: a node that answers slowly.
func holdVersion(f *fakeTalos, node string, hold chan struct{}) {
	f.mu.Lock()
	if f.versionHold == nil {
		f.versionHold = map[string]chan struct{}{}
	}

	f.versionHold[node] = hold
	f.mu.Unlock()
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

	newClusterApplyWait, newClusterGraceWait, newClusterReadyWait = 8*time.Second, 5*time.Second, 5*time.Second

	t.Cleanup(func() {
		newClusterApplyWait, newClusterGraceWait, newClusterReadyWait = apply, grace, ready
	})
}

func TestStartNewClusterHappyPath(t *testing.T) {
	shortNewClusterWaits(t)

	f, endpoint := maintenanceOnlyNode(t)
	installThenBoot(t, f, endpoint)

	rec := newNewClusterRecorder()
	startNewClusterTest(endpoint, rec, time.Minute)

	node, errMessage, events := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("bootstrap failed: %s (events %+v)", errMessage, events)
	}

	if wantHost, _, _ := net.SplitHostPort(endpoint); node != wantHost {
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

	if !slices.Contains(kubeconfigs, "Kubeconfig "+endpoint) {
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
	wantHost, _, _ := net.SplitHostPort(endpoint)
	if cfg.Context != newClusterName || len(ctx.Endpoints) != 1 || ctx.Endpoints[0] != endpoint {
		t.Errorf("talosconfig context = %q endpoints %v, want %s at %s", cfg.Context, ctx.Endpoints, newClusterName, endpoint)
	}

	if _, port, _ := net.SplitHostPort(ctx.Endpoints[0]); port == "" || ctx.Endpoints[0] == wantHost {
		t.Errorf("talosconfig endpoint %q lost the port of the API that answered", ctx.Endpoints[0])
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

// A node booted from the ISO whose maintenance API is still coming up: the run waits out
// the unreachable stretch (the handshake and the apply retry) and bootstraps it.
func TestStartNewClusterApplyRetriesWhileNodeBoots(t *testing.T) {
	shortNewClusterWaits(t)

	f, endpoint := maintenanceOnlyNode(t)
	installThenBoot(t, f, endpoint)

	hold := make(chan struct{})
	downUntil(f, hold)
	t.Cleanup(func() { release(hold) })

	go func() {
		time.Sleep(15 * newClusterPoll)
		release(hold)
	}()

	rec := newNewClusterRecorder()
	startNewClusterTest(endpoint, rec, time.Minute)

	_, errMessage, _ := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("the retrying apply failed: %s", errMessage)
	}
}

// The node takes the config and never comes back installed: the run says so and says the
// install cannot be undone.
func TestStartNewClusterNeverComesBack(t *testing.T) {
	shortNewClusterWaits(t)

	f, endpoint := maintenanceOnlyNode(t)

	// The node comes back after the install and stays installing forever.
	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()

		go func() {
			time.Sleep(newClusterStep)
			f.setVersion(endpoint, "v1.14.4")
			f.setStage(endpoint, runtime.MachineStageInstalling, false)
		}()
	}

	rec := newNewClusterRecorder()
	startNewClusterTest(endpoint, rec, 20*time.Second)

	_, errMessage, _ := rec.wait(t)
	if !strings.Contains(errMessage, "cannot be undone") || !strings.Contains(errMessage, "start the bootstrap again") {
		t.Errorf("err = %q; want a timeout that names the un-undoable install", errMessage)
	}
}

// installedWithoutClusterNode: an installed Talos without a cluster: its API still answers
// unauthenticated and reports a machine status. The run wipes it (the reset takes the
// whole node down as it reboots into maintenance mode) and installs it.
func installedWithoutClusterNode(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f, endpoint := maintenanceOnlyNode(t)
	f.setStage("", runtime.MachineStageRunning, true)

	f.mu.Lock()
	f.applied = make(chan string, 4)
	f.mu.Unlock()

	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()

		go func() {
			time.Sleep(newClusterStep)
			f.setVersion(endpoint, "v1.14.4")
			f.setStage(endpoint, runtime.MachineStageInstalling, false)

			time.Sleep(newClusterStep)
			f.setStage(endpoint, runtime.MachineStageRunning, true)

			time.Sleep(newClusterStep)
			f.mu.Lock()
			f.kubeconfigs = map[string][]byte{endpoint: []byte("apiVersion: v1\nkind: Config\n")}
			f.mu.Unlock()
		}()
	}

	return f, endpoint
}

// wipeTakesNodeDown: the reset reboots the node, so the whole fake (its maintenance API
// included, like a machine that goes down) disappears until the wipe is seen; the
// maintenance-only return is the installedWithoutClusterNode onApply's doing.
func wipeTakesNodeDown(t *testing.T, f *fakeTalos) {
	t.Helper()

	down := make(chan struct{})

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

		f.mu.Lock()
		defer f.mu.Unlock()

		delete(f.versions, "")
		delete(f.resources, "")
		close(down)
	}()

	t.Cleanup(func() {
		select {
		case <-down:
		default:
			// The run never reached the wipe: let nothing hang on the hold.
			f.mu.Lock()
			defer f.mu.Unlock()

			delete(f.versions, "")
			delete(f.resources, "")
		}
	})
}

// A node that already carries a clusterless Talos install answers the maintenance reset
// and is wiped before the config goes out.
func TestStartNewClusterWipesInstalledClusterlessNode(t *testing.T) {
	shortNewClusterWaits(t)

	f, endpoint := installedWithoutClusterNode(t)
	wipeTakesNodeDown(t, f)

	rec := newNewClusterRecorder()
	startNewClusterTest(endpoint, rec, time.Minute)

	node, errMessage, events := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("wipe bootstrap failed: %s (events %+v)", errMessage, events)
	}

	if wantHost, _, _ := net.SplitHostPort(endpoint); node != wantHost {
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

// Cancel before the config goes out: nothing changed on the node. The maintenance
// Version call holds (it records first, then waits) while the apply is still waiting
// for the node to be reachable, so cancelling lands in between.
func TestStartNewClusterCancelBeforeApply(t *testing.T) {
	shortNewClusterWaits(t)

	f, endpoint := maintenanceOnlyNode(t)

	down := make(chan struct{})
	downUntil(f, down)

	hold := make(chan struct{})
	holdVersion(f, "", hold)

	t.Cleanup(func() {
		release(hold)
		release(down)
	})

	rec := newNewClusterRecorder()
	run := startNewClusterTest(endpoint, rec, time.Minute)

	// Wait until the run reached the maintenance API (the held call records itself),
	// then cancel: the run is between calls, the config never went out.
	deadline := time.Now().Add(10 * time.Second)

	for {
		if len(f.called("Version ")) > 0 || time.Now().After(deadline) {
			break
		}

		time.Sleep(newClusterPoll)
	}

	run.Cancel()

	node, errMessage, _ := rec.wait(t)
	wantHost, _, _ := net.SplitHostPort(endpoint)
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

// Cancel while the node is being followed after the apply: the config was sent, so the
// run says the install cannot be undone and points at starting it again.
func TestStartNewClusterCancelWhileFollowing(t *testing.T) {
	shortNewClusterWaits(t)

	f, endpoint := maintenanceOnlyNode(t)

	f.mu.Lock()
	f.applied = make(chan string)
	f.mu.Unlock()

	f.onApply = func(string) {
		f.mu.Lock()
		delete(f.versions, "")
		f.mu.Unlock()

		f.applied <- "sent" // unblocks once the test waits for the apply
	}

	rec := newNewClusterRecorder()
	run := startNewClusterTest(endpoint, rec, time.Minute)

	// The apply happened (the channel closes when it does); cancel mid-follow.
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

	f, endpoint := maintenanceOnlyNode(t)
	installThenBoot(t, f, endpoint)

	rec := newNewClusterRecorder()
	StartNewCluster(endpoint, newClusterName, newClusterK8s, newClusterDisk, newClusterHost, "", rec)

	_, errMessage, _ := rec.wait(t)
	if errMessage != "" {
		t.Fatalf("bootstrap failed: %s", errMessage)
	}

	entries := readAudit(t, "", "cluster-bootstrap")
	if len(entries) != 1 {
		t.Fatalf("audit entries = %+v, want one", entries)
	}

	e := entries[0]
	if e.Outcome != auditOK || e.Node != endpoint {
		t.Errorf("entry = %+v", e)
	}

	if !strings.Contains(e.Params, "hostname=cp-1") || !strings.Contains(e.Params, "k8s=1.34.1") ||
		!strings.Contains(e.Params, "disk=/dev/sda") || !strings.Contains(e.Params, "endpoint=https://"+endpoint+":6443") {
		t.Errorf("params = %q", e.Params)
	}

	if strings.Contains(e.Params, "BEGIN") || strings.Contains(e.Params, "secrets:") {
		t.Errorf("params carry config: %q", e.Params)
	}
}
