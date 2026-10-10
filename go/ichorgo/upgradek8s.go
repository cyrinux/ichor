package ichorgo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/client"
	"github.com/siderolabs/talos/pkg/machinery/compatibility/talos110"
	"github.com/siderolabs/talos/pkg/machinery/compatibility/talos111"
	"github.com/siderolabs/talos/pkg/machinery/compatibility/talos112"
	"github.com/siderolabs/talos/pkg/machinery/compatibility/talos113"
	"github.com/siderolabs/talos/pkg/machinery/compatibility/talos114"
	"github.com/siderolabs/talos/pkg/machinery/compatibility/talos18"
	"github.com/siderolabs/talos/pkg/machinery/compatibility/talos19"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	"github.com/siderolabs/talos/pkg/machinery/config/machine"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/k8s"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// The Kubernetes upgrade, like `talosctl upgrade-k8s`: the control-plane images of each
// control plane's machine config one node at a time, waiting for its static pods, then the
// kubelet image of every node. talosctl's own package lives in the main Talos module, which
// the app does not import (only pkg/machinery), so the sequence is rebuilt here on the
// config-apply code. See K8sUpgradePlan and StartK8sUpgrade (upgradek8s_run.go).

const (
	k8sKindControlPlane = "controlplane"
	k8sKindKubelet      = "kubelet"

	k8sAPIServer         = "apiserver"
	k8sControllerManager = "controller-manager"
	k8sScheduler         = "scheduler"
	k8sProxy             = "proxy"
	k8sKubelet           = "kubelet"
)

// k8sUpgradePlan is what K8sUpgradePlan returns.
type k8sUpgradePlan struct {
	From string `json:"from"` // the API server's version now, "1.34.0"
	To   string `json:"to"`
	// Supported: every node's Talos supports To; SupportedRange is what they all support.
	Supported      bool               `json:"supported"`
	SupportedRange string             `json:"supportedRange"` // "1.30–1.35", "" when unknown
	TalosVersion   string             `json:"talosVersion"`   // the lowest Talos of the nodes
	Steps          []k8sPlanStep      `json:"steps"`
	DeprecatedAPIs []k8sDeprecatedAPI `json:"deprecatedApis"`
	Blockers       []string           `json:"blockers"`
	Warnings       []string           `json:"warnings"`
}

// k8sPlanStep is one component of one node, in the order the run follows.
type k8sPlanStep struct {
	Kind      string `json:"kind"` // controlplane | kubelet
	Node      string `json:"node"`
	Hostname  string `json:"hostname"`
	Component string `json:"component"` // apiserver | controller-manager | scheduler | proxy | kubelet
	Image     string `json:"image"`
	Current   string `json:"current"`
	Changed   bool   `json:"changed"`
}

// k8sDeprecatedAPI is a deprecated API version still requested, from the API server's
// metrics: severity critical when To (or the release before) removes it.
type k8sDeprecatedAPI struct {
	API       string `json:"api"`
	RemovedIn string `json:"removedIn"`
	Severity  string `json:"severity"`
}

// k8sSupport is the Kubernetes minors each Talos minor supports (the Talos support matrix,
// from machinery's compatibility tables).
var k8sSupport = []struct {
	talos    [2]uint64
	min, max uint64 // Kubernetes 1.min to 1.max
}{
	{talos18.MajorMinor, talos18.MinimumKubernetesVersion.Minor, talos18.MaximumKubernetesVersion.Minor},
	{talos19.MajorMinor, talos19.MinimumKubernetesVersion.Minor, talos19.MaximumKubernetesVersion.Minor},
	{talos110.MajorMinor, talos110.MinimumKubernetesVersion.Minor, talos110.MaximumKubernetesVersion.Minor},
	{talos111.MajorMinor, talos111.MinimumKubernetesVersion.Minor, talos111.MaximumKubernetesVersion.Minor},
	{talos112.MajorMinor, talos112.MinimumKubernetesVersion.Minor, talos112.MaximumKubernetesVersion.Minor},
	{talos113.MajorMinor, talos113.MinimumKubernetesVersion.Minor, talos113.MaximumKubernetesVersion.Minor},
	{talos114.MajorMinor, talos114.MinimumKubernetesVersion.Minor, talos114.MaximumKubernetesVersion.Minor},
}

// k8sSupportRange is the Kubernetes minors a Talos version supports.
func k8sSupportRange(talosVersion string) (lo, hi int, ok bool) {
	major, minor, known := kubeMinor(talosVersion)
	if !known {
		return 0, 0, false
	}

	for _, s := range k8sSupport {
		if s.talos == [2]uint64{uint64(major), uint64(minor)} {
			return int(s.min), int(s.max), true
		}
	}

	return 0, 0, false
}

// k8sNode is what the plan reads of one node.
type k8sNode struct {
	node, hostname string
	talosVersion   string
	controlPlane   bool
	provider       talosconfig.Provider // with its secrets
	// effective are the images the node runs (component -> image) when its config leaves
	// them to Talos's defaults, which depend on the node's Talos version.
	effective map[string]string
	err       error
}

// K8sUpgradePlan says what upgrading Kubernetes to toVersion (1.X.Y) would change,
// read-only (os:admin, the machine configs are sensitive): every component image per node
// in the run's order (control planes first, then the kubelets), the Talos support range,
// the deprecated APIs still requested (needs the Kubernetes API: without it, a warning) and
// the blockers (a jump of more than one minor, a version Talos does not support, the
// upgrade lock held, a node that cannot be read). See k8sUpgradePlan for the JSON.
// kubeServer: see KubePods.
func K8sUpgradePlan(configYAML, contextName, kubeServer, toVersion string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	if isKubeconfig(configYAML) {
		return "", errK8sUpgradeNoTalos
	}

	if isDemoContext(configYAML, contextName) {
		return demoRead("K8sUpgradePlan", configYAML, contextName, "", toVersion)
	}

	return withSession(configYAML, contextName, clusterPlanTimeout(configYAML, contextName), func(ctx context.Context, s *session) (string, error) {
		plan, err := gatherK8sPlan(ctx, s, kubeTarget{configYAML, contextName, kubeServer}, toVersion)
		if err != nil {
			return "", err
		}

		return toJSON(plan)
	})
}

var errK8sUpgradeNoTalos = errors.New("the Kubernetes upgrade changes Talos machine configs: this cluster was added from a kubeconfig and has no Talos API")

func gatherK8sPlan(ctx context.Context, s *session, kube kubeTarget, toVersion string) (k8sUpgradePlan, error) {
	plan, err := buildK8sPlan(readK8sNodes(ctx, s), toVersion)
	if err != nil {
		return plan, err
	}

	if lock := readUpgradeLock(ctx, kube, time.Now()); lock != nil {
		switch {
		case lock.err != "":
			plan.Warnings = append(plan.Warnings, "cannot check the cluster upgrade lock ("+lock.err+"): make sure nobody else upgrades this cluster now")
		case lock.held != nil:
			plan.Blockers = append(plan.Blockers, lock.held.describe())
		}
	}

	if plan.From != "" && kube.config != "" {
		addDeprecatedAPIs(ctx, kube, &plan)
	}

	return plan, nil
}

// addDeprecatedAPIs adds the deprecated APIs still requested, read from the API server's
// metrics; an unreadable API server is a warning.
func addDeprecatedAPIs(ctx context.Context, kube kubeTarget, plan *k8sUpgradePlan) {
	var section checkupSection

	err := kubeDo(ctx, kube, func(ctx context.Context, k *kubeClient) error {
		section = checkupUpgrade(ctx, k, plan.From)

		if section.Error != "" {
			return errors.New(section.Error)
		}

		return nil
	})
	if err != nil {
		plan.Warnings = append(plan.Warnings, "the deprecated APIs still in use could not be checked ("+err.Error()+")")

		return
	}

	for _, f := range section.Findings {
		plan.DeprecatedAPIs = append(plan.DeprecatedAPIs, k8sDeprecatedAPI{API: f.Name, RemovedIn: f.Extra, Severity: f.Severity})
	}

	critical := slices.ContainsFunc(plan.DeprecatedAPIs, func(d k8sDeprecatedAPI) bool { return d.Severity == sevCritical })
	if critical {
		plan.Warnings = append(plan.Warnings, "some clients still request APIs that "+plan.To+" removes: they fail after the upgrade")
	}
}

// readK8sNodes reads each node of the context: its Talos version, hostname and machine
// config, control planes first, each group by hostname.
func readK8sNodes(ctx context.Context, s *session) []k8sNode {
	targets := targetNodes(s.context)
	nodes := make([]k8sNode, len(targets))

	forEachNode(targets, func(i int, node string) {
		nodes[i] = readK8sNode(ctx, s, node)
	})

	return orderK8sNodes(nodes)
}

func readK8sNode(ctx context.Context, s *session, node string) k8sNode {
	n := k8sNode{node: node, hostname: node}

	nodeCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	nodeCtx = client.WithNode(nodeCtx, node)

	if hs, err := safe.StateGetByID[*network.HostnameStatus](nodeCtx, s.client.COSI, network.HostnameID); err == nil && hs.TypedSpec().Hostname != "" {
		n.hostname = hs.TypedSpec().Hostname
	}

	resp, err := s.client.Version(nodeCtx)
	if err != nil {
		n.err = s.friendlyErr(node, err)

		return n
	}

	n.talosVersion = first(resp.GetMessages()).GetVersion().GetTag()

	snap, err := nodeConfigApplier{s, node}.current(ctx)
	if err != nil {
		n.err = err

		return n
	}

	n.provider, n.err = configloader.NewFromBytes(snap.raw)
	if n.err == nil {
		n.controlPlane = n.provider.Machine().Type() == machine.TypeControlPlane
		n.effective = effectiveK8sImages(nodeCtx, s.client, n.controlPlane)
	}

	return n
}

// effectiveK8sImages reads the images Talos derived from the config, defaults included
// (missing ones are left out).
func effectiveK8sImages(nodeCtx context.Context, c *client.Client, controlPlane bool) map[string]string {
	out := map[string]string{}

	if spec, err := safe.StateGetByID[*k8s.KubeletSpec](nodeCtx, c.COSI, k8s.KubeletID); err == nil {
		out[k8sKubelet] = spec.TypedSpec().Image
	}

	if !controlPlane {
		return out
	}

	if r, err := safe.StateGetByID[*k8s.APIServerConfig](nodeCtx, c.COSI, k8s.APIServerConfigID); err == nil {
		out[k8sAPIServer] = r.TypedSpec().Image
	}

	if r, err := safe.StateGetByID[*k8s.ControllerManagerConfig](nodeCtx, c.COSI, k8s.ControllerManagerConfigID); err == nil {
		out[k8sControllerManager] = r.TypedSpec().Image
	}

	if r, err := safe.StateGetByID[*k8s.SchedulerConfig](nodeCtx, c.COSI, k8s.SchedulerConfigID); err == nil {
		out[k8sScheduler] = r.TypedSpec().Image
	}

	if r, err := safe.StateGetByID[*k8s.BootstrapManifestsConfig](nodeCtx, c.COSI, k8s.BootstrapManifestsConfigID); err == nil {
		out[k8sProxy] = r.TypedSpec().ProxyImage
	}

	return out
}

func orderK8sNodes(nodes []k8sNode) []k8sNode {
	out := slices.Clone(nodes)

	rank := func(n k8sNode) int {
		if n.controlPlane {
			return 0
		}

		return 1
	}

	slices.SortStableFunc(out, func(a, b k8sNode) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.hostname, b.hostname), cmp.Compare(a.node, b.node))
	})

	return out
}

// buildK8sPlan is the plan from the nodes read: their steps, then the version checks.
func buildK8sPlan(nodes []k8sNode, toVersion string) (k8sUpgradePlan, error) {
	tag, ok := normalizeTalosVersion(toVersion)
	if !ok {
		return k8sUpgradePlan{}, fmt.Errorf("%q is not a Kubernetes version (1.X.Y)", toVersion)
	}

	plan := k8sUpgradePlan{
		To: strings.TrimPrefix(tag, "v"), Steps: []k8sPlanStep{}, DeprecatedAPIs: []k8sDeprecatedAPI{},
		Blockers: []string{}, Warnings: []string{},
	}

	var controlPlanes, kubelets []k8sPlanStep

	for _, n := range nodes {
		if n.err != nil {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s cannot be read (%v): every node's machine config is needed", n.hostname, n.err))

			continue
		}

		steps, err := k8sImageSteps(n, tag)
		if err != nil {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s: %v", n.hostname, err))

			continue
		}

		for _, st := range steps {
			if st.Kind == k8sKindControlPlane {
				controlPlanes = append(controlPlanes, st)
			} else {
				kubelets = append(kubelets, st)
			}
		}

		if plan.From == "" && n.controlPlane {
			_, current := splitImageRef(steps[0].Current) // the API server comes first
			plan.From = strings.TrimPrefix(current, "v")
		}
	}

	plan.Steps = append(append(plan.Steps, controlPlanes...), kubelets...)

	if len(controlPlanes) == 0 && !slices.ContainsFunc(nodes, func(n k8sNode) bool { return n.err != nil }) {
		plan.Blockers = append(plan.Blockers, "no control plane among this context's nodes: add them to its nodes, the upgrade changes their machine configs")
	}

	checkK8sVersions(&plan, nodes)

	if !slices.ContainsFunc(plan.Steps, func(s k8sPlanStep) bool { return s.Changed }) && len(plan.Steps) > 0 {
		plan.Warnings = append(plan.Warnings, "every component already runs "+plan.To)
	}

	return plan, nil
}

// k8sImageSteps are the components of node n and their images for tag (vX.Y.Z): each
// keeps its registry and repository (a mirror stays a mirror), only the tag changes.
func k8sImageSteps(n k8sNode, tag string) ([]k8sPlanStep, error) {
	images, proxy := configK8sImages(n.provider)

	var (
		steps   []k8sPlanStep
		unknown []string
	)

	add := func(kind, component, defaultRepo string) {
		current := images[component]
		if current == "" {
			current = n.effective[component]
		}

		if current == "" {
			unknown = append(unknown, component)
			current = defaultRepo // for the repository: the version stays unknown
		}

		image := retagImage(current, tag)
		steps = append(steps, k8sPlanStep{
			Kind: kind, Node: n.node, Hostname: n.hostname, Component: component,
			Image: image, Current: current, Changed: image != current,
		})
	}

	if n.controlPlane {
		add(k8sKindControlPlane, k8sAPIServer, constants.KubernetesAPIServerImage)
		add(k8sKindControlPlane, k8sControllerManager, constants.KubernetesControllerManagerImage)
		add(k8sKindControlPlane, k8sScheduler, constants.KubernetesSchedulerImage)

		if proxy {
			add(k8sKindControlPlane, k8sProxy, constants.KubeProxyImage)
		}
	}

	add(k8sKindKubelet, k8sKubelet, constants.KubeletImage)

	if len(unknown) > 0 {
		return nil, fmt.Errorf("the image %s runs cannot be read from its config or its Talos resources", strings.Join(unknown, ", "))
	}

	return steps, nil
}

// retagImage is image with tag instead of its tag (and without its digest).
func retagImage(image, tag string) string {
	repo, _ := splitImageRef(image)

	return repo + ":" + tag
}

// checkK8sVersions adds the version blockers: the jump from the current version and the
// Talos support range of every node.
func checkK8sVersions(plan *k8sUpgradePlan, nodes []k8sNode) {
	toMajor, toMinor, _ := kubeMinor(plan.To)

	fromMajor, fromMinor, known := kubeMinor(plan.From)

	switch {
	case !known:
		plan.Blockers = append(plan.Blockers, "the current Kubernetes version cannot be told from the API server image of a control plane")
	case fromMajor != toMajor || toMinor > fromMinor+1:
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s to %s skips a minor version: upgrade one minor at a time", plan.From, plan.To))
	case toMinor < fromMinor:
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("%s is older than %s: Kubernetes does not support going back a minor version", plan.To, plan.From))
	case compareVersions(plan.To, plan.From) < 0:
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s is older than %s: a patch downgrade", plan.To, plan.From))
	}

	lo, hi, rangeKnown := -1, 1<<30, false
	plan.Supported = true

	for _, n := range nodes {
		if n.talosVersion == "" {
			continue
		}

		if plan.TalosVersion == "" || compareVersions(n.talosVersion, plan.TalosVersion) < 0 {
			plan.TalosVersion = n.talosVersion
		}

		nLo, nHi, ok := k8sSupportRange(n.talosVersion)
		if !ok {
			plan.Supported = false
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("the Kubernetes versions Talos %s supports are not known to this app: check the Talos support matrix", n.talosVersion))

			continue
		}

		lo, hi, rangeKnown = max(lo, nLo), min(hi, nHi), true

		if toMajor != 1 || toMinor < nLo || toMinor > nHi {
			plan.Supported = false
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("Talos %s on %s supports Kubernetes 1.%d to 1.%d, not %s", n.talosVersion, n.hostname, nLo, nHi, plan.To))
		}
	}

	if rangeKnown && lo <= hi {
		plan.SupportedRange = fmt.Sprintf("1.%d–1.%d", lo, hi)
	}

	plan.Blockers = slices.Compact(plan.Blockers)
	plan.Warnings = slices.Compact(plan.Warnings)
}

// demoK8sVersion is the Kubernetes version the demo cluster runs.
const demoK8sVersion = "v1.34.1"

// demoK8sPlan is the plan of the demo nodes, all on demoK8sVersion.
func demoK8sPlan(nodes []nodeOverview, toVersion string) (string, error) {
	k8sNodes := make([]k8sNode, 0, len(nodes))

	for _, n := range nodes {
		text := fmt.Sprintf("version: v1alpha1\nmachine:\n  type: %s\n  kubelet:\n    image: %s:%s\ncluster:\n  clusterName: ichor-demo\n",
			n.Role, constants.KubeletImage, demoK8sVersion)

		provider, err := configloader.NewFromBytes([]byte(text))
		if err != nil {
			return "", err
		}

		k8sNodes = append(k8sNodes, k8sNode{
			node: n.Node, hostname: n.Hostname, talosVersion: n.Version, controlPlane: n.Role == "controlplane",
			provider: withDemoImages(provider),
		})
	}

	plan, err := buildK8sPlan(orderK8sNodes(k8sNodes), toVersion)
	if err != nil {
		return "", err
	}

	return toJSON(plan)
}

// withDemoImages sets the control-plane images of a demo config to demoK8sVersion.
func withDemoImages(provider talosconfig.Provider) talosconfig.Provider {
	patched, err := withK8sImages(provider, map[string]string{
		k8sAPIServer:         constants.KubernetesAPIServerImage + ":" + demoK8sVersion,
		k8sControllerManager: constants.KubernetesControllerManagerImage + ":" + demoK8sVersion,
		k8sScheduler:         constants.KubernetesSchedulerImage + ":" + demoK8sVersion,
		k8sProxy:             constants.KubeProxyImage + ":" + demoK8sVersion,
	})
	if err != nil {
		return provider
	}

	return patched
}
