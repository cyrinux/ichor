package ichorgo

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	machineapi "github.com/siderolabs/talos/pkg/machinery/api/machine"
	configdoc "github.com/siderolabs/talos/pkg/machinery/config/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	k8sdoc "github.com/siderolabs/talos/pkg/machinery/config/types/k8s"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/siderolabs/talos/pkg/machinery/resources/config"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
)

// generatedWorkerConfig is a real worker config, made once.
var generatedWorkerConfig = sync.OnceValues(func() ([]byte, error) {
	in, err := generate.NewInput("test", "https://192.0.2.10:6443", "1.34.0")
	if err != nil {
		return nil, err
	}

	cfg, err := in.Config(machine.TypeWorker)
	if err != nil {
		return nil, err
	}

	return cfg.Bytes()
})

func TestRetagImage(t *testing.T) {
	for image, want := range map[string]string{
		"registry.k8s.io/kube-apiserver:v1.34.0":                 "registry.k8s.io/kube-apiserver:v1.35.0",
		"mirror.corp.example:5000/k8s/kube-scheduler:v1.34.0":    "mirror.corp.example:5000/k8s/kube-scheduler:v1.35.0",
		"ghcr.io/siderolabs/kubelet:v1.34.0@sha256:0123456789ab": "ghcr.io/siderolabs/kubelet:v1.35.0",
	} {
		if got := retagImage(image, "v1.35.0"); got != want {
			t.Errorf("retagImage(%q) = %q, want %q", image, got, want)
		}
	}
}

func k8sTestNode(t *testing.T, node string, raw []byte, talosVersion string) k8sNode {
	t.Helper()

	provider, err := configloader.NewFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}

	return k8sNode{
		node: node, hostname: "host-" + strings.ReplaceAll(node, ".", "-"), talosVersion: talosVersion,
		controlPlane: provider.Machine().Type() == machine.TypeControlPlane, provider: provider,
		effective: k8sDefaults("v1.34.0"),
	}
}

// k8sDefaults are the images Talos defaults to for tag.
func k8sDefaults(tag string) map[string]string {
	return map[string]string{
		k8sAPIServer:         "registry.k8s.io/kube-apiserver:" + tag,
		k8sControllerManager: "registry.k8s.io/kube-controller-manager:" + tag,
		k8sScheduler:         "registry.k8s.io/kube-scheduler:" + tag,
		k8sProxy:             "registry.k8s.io/kube-proxy:" + tag,
		k8sKubelet:           "ghcr.io/siderolabs/kubelet:" + tag,
	}
}

// putK8sDefaults gives node the Talos resources holding its effective images.
func putK8sDefaults(f *fakeTalos, node, tag string, controlPlane bool) {
	images := k8sDefaults(tag)

	kubelet := k8s.NewKubeletSpec(k8s.NamespaceName, k8s.KubeletID)
	kubelet.TypedSpec().Image = images[k8sKubelet]
	f.put(node, kubelet)

	if !controlPlane {
		return
	}

	api := k8s.NewAPIServerConfig(k8s.APIServerConfigID)
	api.TypedSpec().Image = images[k8sAPIServer]
	cm := k8s.NewControllerManagerConfig(k8s.ControllerManagerConfigID)
	cm.TypedSpec().Image = images[k8sControllerManager]
	sched := k8s.NewSchedulerConfig(k8s.SchedulerConfigID)
	sched.TypedSpec().Image = images[k8sScheduler]
	manifests := k8s.NewBootstrapManifestsConfig()
	manifests.TypedSpec().ProxyImage = images[k8sProxy]

	f.put(node, api, cm, sched, manifests)
}

func k8sTestNodes(t *testing.T, talosVersion string) []k8sNode {
	t.Helper()

	cp, err := generatedConfig()
	if err != nil {
		t.Fatal(err)
	}

	worker, err := generatedWorkerConfig()
	if err != nil {
		t.Fatal(err)
	}

	return orderK8sNodes([]k8sNode{
		k8sTestNode(t, "192.0.2.61", worker, talosVersion),
		k8sTestNode(t, "192.0.2.52", cp, talosVersion),
		k8sTestNode(t, "192.0.2.51", cp, talosVersion),
	})
}

func stepKeys(steps []k8sPlanStep) []string {
	var out []string
	for _, s := range steps {
		out = append(out, s.Node+" "+s.Component)
	}

	return out
}

func TestBuildK8sPlan(t *testing.T) {
	plan, err := buildK8sPlan(k8sTestNodes(t, "v1.12.0"), "1.35.0")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"192.0.2.51 apiserver", "192.0.2.51 controller-manager", "192.0.2.51 scheduler", "192.0.2.51 proxy",
		"192.0.2.52 apiserver", "192.0.2.52 controller-manager", "192.0.2.52 scheduler", "192.0.2.52 proxy",
		"192.0.2.51 kubelet", "192.0.2.52 kubelet", "192.0.2.61 kubelet",
	}
	if got := stepKeys(plan.Steps); !slices.Equal(got, want) {
		t.Fatalf("steps = %v\nwant %v", got, want)
	}

	if plan.From != "1.34.0" || plan.To != "1.35.0" || !plan.Supported || plan.SupportedRange != "1.30–1.35" || plan.TalosVersion != "v1.12.0" || len(plan.Blockers) != 0 {
		t.Fatalf("plan = %+v", plan)
	}

	first := plan.Steps[0]
	if first.Kind != k8sKindControlPlane || first.Current != "registry.k8s.io/kube-apiserver:v1.34.0" || first.Image != "registry.k8s.io/kube-apiserver:v1.35.0" || !first.Changed {
		t.Errorf("first step = %+v", first)
	}

	if last := plan.Steps[len(plan.Steps)-1]; last.Kind != k8sKindKubelet || !strings.HasSuffix(last.Image, "/kubelet:v1.35.0") {
		t.Errorf("last step = %+v", last)
	}
}

func TestBuildK8sPlanBlockers(t *testing.T) {
	nodes := k8sTestNodes(t, "v1.12.0")

	for version, want := range map[string]string{
		"1.36.0": "skips a minor version",
		"1.33.5": "Kubernetes does not support going back",
	} {
		plan, err := buildK8sPlan(nodes, version)
		if err != nil {
			t.Fatal(err)
		}

		if !slices.ContainsFunc(plan.Blockers, func(b string) bool { return strings.Contains(b, want) }) {
			t.Errorf("%s: blockers = %v, want %q", version, plan.Blockers, want)
		}
	}

	// Talos 1.11 supports up to 1.34: 1.35 is out of range.
	plan, err := buildK8sPlan(k8sTestNodes(t, "v1.11.3"), "1.35.0")
	if err != nil {
		t.Fatal(err)
	}

	if plan.Supported || !slices.ContainsFunc(plan.Blockers, func(b string) bool { return strings.Contains(b, "supports Kubernetes 1.29 to 1.34") }) {
		t.Errorf("out of range = %+v", plan)
	}

	// A node that cannot be read blocks the run.
	broken := slices.Clone(nodes)
	broken[0].err = errors.New("connection refused")

	if plan, _ := buildK8sPlan(broken, "1.35.0"); len(plan.Blockers) == 0 {
		t.Error("an unreadable node must be a blocker")
	}

	if _, err := buildK8sPlan(nodes, "latest"); err == nil {
		t.Error("a bad version must be refused")
	}

	// Already there: nothing to change.
	same, _ := buildK8sPlan(nodes, "1.34.0")
	if slices.ContainsFunc(same.Steps, func(s k8sPlanStep) bool { return s.Changed }) || len(same.Warnings) == 0 {
		t.Errorf("same version = %+v", same)
	}
}

func TestK8sImageStepsSkipsDisabledProxy(t *testing.T) {
	n := k8sTestNodes(t, "v1.12.0")[0]

	patched, err := n.provider.PatchV1Alpha1(func(c *v1alpha1.Config) error {
		disabled := true
		//lint:ignore SA1019 the field most configs still hold
		c.ClusterConfig.ProxyConfig = &v1alpha1.ProxyConfig{Disabled: &disabled}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	n.provider = patched

	steps, err := k8sImageSteps(n, "v1.35.0")
	if err != nil {
		t.Fatal(err)
	}

	if slices.ContainsFunc(steps, func(s k8sPlanStep) bool { return s.Component == k8sProxy }) {
		t.Errorf("a disabled kube-proxy must not be upgraded: %v", stepKeys(steps))
	}
}

// Generated 1.14 configs hold the images in documents of their own (KubeletConfig, …).
func TestK8sImagesInTheirOwnDocuments(t *testing.T) {
	raw, err := generatedWorkerConfig()
	if err != nil {
		t.Fatal(err)
	}

	provider, err := configloader.NewFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.ContainsFunc(provider.Documents(), func(d configdoc.Document) bool { _, ok := d.(*k8sdoc.KubeletConfigV1Alpha1); return ok }) {
		t.Skip("this machinery generates no KubeletConfig document")
	}

	if images, _ := configK8sImages(provider); images[k8sKubelet] != "ghcr.io/siderolabs/kubelet:v1.34.0" {
		t.Fatalf("images = %v", images)
	}

	patched, err := withK8sImages(provider, map[string]string{k8sKubelet: "mirror.corp.example/kubelet:v1.35.0"})
	if err != nil {
		t.Fatal(err)
	}

	if images, _ := configK8sImages(patched); images[k8sKubelet] != "mirror.corp.example/kubelet:v1.35.0" {
		t.Errorf("images = %v", images)
	}

	//lint:ignore SA1019 the v1alpha1 field must stay untouched
	if m := patched.RawV1Alpha1().MachineConfig; m != nil && m.MachineKubelet != nil && m.MachineKubelet.KubeletImage != "" {
		t.Error("the image belongs in its own document, not in v1alpha1")
	}

	if images, _ := configK8sImages(provider); images[k8sKubelet] != "ghcr.io/siderolabs/kubelet:v1.34.0" {
		t.Error("the config read must not be modified")
	}
}

func TestApplyK8sImagesChangesOnlyImages(t *testing.T) {
	node := newFakeConfigNode(t)
	before := node.base()

	plan, err := buildK8sPlan(k8sTestNodes(t, "v1.12.0"), "1.35.0")
	if err != nil {
		t.Fatal(err)
	}

	works := k8sWorks(plan.Steps)

	// A dry run applies nothing.
	if err := applyK8sImages(context.Background(), node, works[0], true); err != nil {
		t.Fatal(err)
	}

	if len(node.calls) != 1 || !node.calls[0].dryRun || node.base() != before {
		t.Fatalf("dry run calls = %d, changed = %t", len(node.calls), node.base() != before)
	}

	// For real: a dry run first, then the change, without a reboot.
	for _, w := range []k8sWork{works[0], works[2]} { // a control plane's images, then its kubelet
		if err := applyK8sImages(context.Background(), node, w, false); err != nil {
			t.Fatal(err)
		}
	}

	if len(node.calls) != 5 || !node.calls[1].dryRun || node.calls[2].dryRun || node.calls[2].mode != machineapi.ApplyConfigurationRequest_NO_REBOOT {
		t.Fatalf("calls = %+v", node.calls)
	}

	var changed []string

	for _, l := range configDiffLines(before, node.base()) {
		if l.Kind == diffLineAdded || l.Kind == diffLineRemoved {
			changed = append(changed, strings.TrimSpace(l.Text))
		}
	}

	if len(changed) != 10 {
		t.Fatalf("changed lines = %v, want the 5 images, before and after", changed)
	}

	for _, l := range changed {
		if !strings.HasPrefix(l, "image: ") {
			t.Errorf("only image fields may change: %q", l)
		}
	}
}

func TestApplyK8sImagesRefusesAReboot(t *testing.T) {
	node := newFakeConfigNode(t)
	node.reboot = true

	plan, _ := buildK8sPlan(k8sTestNodes(t, "v1.12.0"), "1.35.0")

	err := applyK8sImages(context.Background(), node, k8sWorks(plan.Steps)[0], false)
	if !errors.Is(err, errK8sNeedsReboot) || len(node.calls) != 1 {
		t.Fatalf("err = %v, calls = %d", err, len(node.calls))
	}
}

func TestStaticPodsAt(t *testing.T) {
	pod := func(image string, ready bool) map[string]any {
		status := "False"
		if ready {
			status = "True"
		}

		return map[string]any{
			"conditions":        []any{map[string]any{"type": "Ready", "status": status}},
			"containerStatuses": []any{map[string]any{"image": image}},
		}
	}

	want := map[string]string{k8sStaticPods[k8sAPIServer]: "v1.35.0", k8sStaticPods[k8sScheduler]: "v1.35.0"}
	pods := map[string]map[string]any{
		"kube-system/kube-apiserver-host-a": pod("registry.k8s.io/kube-apiserver:v1.35.0", true),
		"kube-system/kube-scheduler-host-a": pod("registry.k8s.io/kube-scheduler:v1.34.0", true),
	}

	if done, waiting := staticPodsAt(pods, want); done || waiting != "kube-scheduler" {
		t.Errorf("old scheduler: done = %t, waiting = %q", done, waiting)
	}

	pods["kube-system/kube-scheduler-host-a"] = pod("registry.k8s.io/kube-scheduler:v1.35.0", false)
	if done, _ := staticPodsAt(pods, want); done {
		t.Error("a pod not Ready yet must be waited for")
	}

	pods["kube-system/kube-scheduler-host-a"] = pod("registry.k8s.io/kube-scheduler:v1.35.0", true)
	if done, _ := staticPodsAt(pods, want); !done {
		t.Error("every pod runs the new version")
	}
}

// stubK8s records the run's steps.
type stubK8s struct {
	calls    []string
	waitFail map[string]error
	onApply  func(w k8sWork)
}

func (s *stubK8s) apply(_ context.Context, w k8sWork, dryRun bool) error {
	call := "apply " + w.node + " " + w.kind
	if dryRun {
		call += " dry"
	}

	s.calls = append(s.calls, call)

	if s.onApply != nil {
		s.onApply(w)
	}

	return nil
}

func (s *stubK8s) wait(_ context.Context, w k8sWork) error {
	s.calls = append(s.calls, "wait "+w.node+" "+w.kind)

	return s.waitFail[w.node+" "+w.kind]
}

func (s *stubK8s) updateProxy(_ context.Context, image string, dryRun bool) (string, error) {
	s.calls = append(s.calls, "proxy "+image)

	return "", nil
}

func runStubK8s(t *testing.T, steps *stubK8s, dryRun bool, stop chan struct{}) ([]k8sProgress, error) {
	t.Helper()

	plan, err := buildK8sPlan(k8sTestNodes(t, "v1.12.0"), "1.35.0")
	if err != nil {
		t.Fatal(err)
	}

	if stop == nil {
		stop = make(chan struct{})
	}

	var progress []k8sProgress

	err = runK8sUpgrade(context.Background(), steps, plan, k8sControl{dryRun: dryRun, stop: stop, emit: func(p k8sProgress) { progress = append(progress, p) }})

	return progress, err
}

func TestK8sUpgradeControlPlanesBeforeKubelets(t *testing.T) {
	steps := &stubK8s{}

	progress, err := runStubK8s(t, steps, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"apply 192.0.2.51 controlplane", "wait 192.0.2.51 controlplane",
		"apply 192.0.2.52 controlplane", "wait 192.0.2.52 controlplane",
		"apply 192.0.2.51 kubelet", "wait 192.0.2.51 kubelet",
		"apply 192.0.2.52 kubelet", "wait 192.0.2.52 kubelet",
		"apply 192.0.2.61 kubelet", "wait 192.0.2.61 kubelet",
		"proxy registry.k8s.io/kube-proxy:v1.35.0",
	}
	if !slices.Equal(steps.calls, want) {
		t.Fatalf("calls = %v\nwant %v", steps.calls, want)
	}

	last := progress[len(progress)-1]
	if last.Phase != k8sPhaseDone || last.Total != 5 || progress[0].Component != "apiserver,controller-manager,scheduler,proxy" {
		t.Errorf("progress = %+v … %+v", progress[0], last)
	}
}

func TestK8sUpgradeDryRunNeverWaits(t *testing.T) {
	steps := &stubK8s{}

	progress, err := runStubK8s(t, steps, true, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range steps.calls {
		if strings.HasPrefix(c, "wait") || (strings.HasPrefix(c, "apply") && !strings.HasSuffix(c, " dry")) {
			t.Errorf("a dry run only asks for dry runs: %v", steps.calls)
		}
	}

	if !progress[len(progress)-1].DryRun {
		t.Error("the progress must say it is a dry run")
	}
}

func TestK8sUpgradeFailedWaitStops(t *testing.T) {
	steps := &stubK8s{waitFail: map[string]error{"192.0.2.51 controlplane": errors.New("kube-apiserver did not come back")}}

	_, err := runStubK8s(t, steps, false, nil)
	if err == nil || !strings.Contains(err.Error(), "kube-apiserver did not come back") {
		t.Fatalf("err = %v", err)
	}

	if len(steps.calls) != 2 {
		t.Errorf("nothing after the failed wait: %v", steps.calls)
	}
}

func TestK8sUpgradeCancelBetweenSteps(t *testing.T) {
	run := &K8sUpgradeRun{stop: make(chan struct{})}
	steps := &stubK8s{onApply: func(k8sWork) { run.Cancel(); run.Cancel() }} // cancelled while the first node changes

	_, err := runStubK8s(t, steps, false, run.stop)
	if !errors.Is(err, errK8sCancelled) {
		t.Fatalf("err = %v", err)
	}

	// The first node is finished and waited for; the next is not touched.
	if want := []string{"apply 192.0.2.51 controlplane", "wait 192.0.2.51 controlplane"}; !slices.Equal(steps.calls, want) {
		t.Errorf("calls = %v", steps.calls)
	}
}

// k8sFake is two control planes and a worker on Talos 1.12, Kubernetes 1.34.0.
func k8sFake(t *testing.T) (*fakeTalos, string) {
	t.Helper()

	f := newFakeTalos()
	f.addNode(t, "192.0.2.51", "v1.12.0", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.52", "v1.12.0", machine.TypeControlPlane)
	f.addNode(t, "192.0.2.61", "v1.12.0", machine.TypeWorker)
	f.putMachineConfig(t, "192.0.2.51")
	f.putMachineConfig(t, "192.0.2.52")

	raw, err := generatedWorkerConfig()
	if err != nil {
		t.Fatal(err)
	}

	provider, err := configloader.NewFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}

	f.put("192.0.2.61", config.NewMachineConfigWithID(provider, config.ActiveID))
	putK8sDefaults(f, "192.0.2.51", "v1.34.0", true)
	putK8sDefaults(f, "192.0.2.52", "v1.34.0", true)
	putK8sDefaults(f, "192.0.2.61", "v1.34.0", false)

	return f, f.start(t, "192.0.2.51", "192.0.2.52", "192.0.2.61")
}

func TestK8sUpgradePlanFake(t *testing.T) {
	_, cfg := k8sFake(t)

	out, err := K8sUpgradePlan(cfg, "fake", "", "1.35.0")
	plan := decodeJSON[k8sUpgradePlan](t, out, err)

	if len(plan.Steps) != 11 || plan.Steps[0].Hostname != "host-192-0-2-51" || plan.From != "1.34.0" || len(plan.Blockers) != 0 {
		t.Fatalf("plan = %s", out)
	}

	// No Kubernetes API behind the fake: the lock and the deprecated APIs are warnings.
	if !slices.ContainsFunc(plan.Warnings, func(w string) bool { return strings.Contains(w, "deprecated APIs") }) {
		t.Errorf("warnings = %v", plan.Warnings)
	}
}

func TestStartK8sUpgradeDryRunFake(t *testing.T) {
	withDataDir(t)

	f, cfg := k8sFake(t)
	rec := doneRecorder{done: make(chan string, 1)}

	StartK8sUpgrade(cfg, "fake", "", "1.35.0", true, rec)

	if got := <-rec.done; got != "" {
		t.Fatalf("dry run = %q", got)
	}

	f.mu.Lock()
	applies := slices.Clone(f.applies)
	f.mu.Unlock()

	if len(applies) != 5 || slices.ContainsFunc(applies, func(a fakeConfigApply) bool { return !a.dryRun }) {
		t.Fatalf("applies = %+v", applies)
	}

	if entries := readAudit(t, "fake", "k8s-upgrade"); len(entries) != 1 || !strings.Contains(entries[0].Params, "from=1.34.0 to=1.35.0 dry-run=true") {
		t.Errorf("audit = %+v", entries)
	}
}

func TestStartK8sUpgradeRefusesBlockers(t *testing.T) {
	withDataDir(t)

	f, cfg := k8sFake(t)
	rec := doneRecorder{done: make(chan string, 1)}

	StartK8sUpgrade(cfg, "fake", "", "1.36.0", false, rec)

	if got := <-rec.done; !strings.Contains(got, "skips a minor version") {
		t.Fatalf("got %q", got)
	}

	if len(f.applies) != 0 {
		t.Errorf("nothing may be applied: %+v", f.applies)
	}
}

func TestK8sUpgradeDemo(t *testing.T) {
	withDataDir(t)

	demo, err := DemoConfig()
	if err != nil {
		t.Fatal(err)
	}

	out, err := K8sUpgradePlan(demo, "", "", "1.35.0")
	plan := decodeJSON[k8sUpgradePlan](t, out, err)

	if plan.From != "1.34.1" || len(plan.Steps) != 3*4+5 || len(plan.Blockers) != 0 || !plan.Supported {
		t.Fatalf("plan = %s", out)
	}

	rec := doneRecorder{done: make(chan string, 1)}
	StartK8sUpgrade(demo, "", "", "1.35.0", true, rec)

	if got := <-rec.done; got != errDemoUnavailable.Error() {
		t.Fatalf("demo = %q", got)
	}

	if entries := readAudit(t, "", "k8s-upgrade"); len(entries) != 1 {
		t.Errorf("audit = %+v", entries)
	}
}
