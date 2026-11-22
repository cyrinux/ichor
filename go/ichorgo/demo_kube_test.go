package ichorgo

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func demoKubeconfigForTest(t *testing.T) string {
	t.Helper()

	yaml, err := DemoKubeconfig()
	if err != nil {
		t.Fatal(err)
	}

	return yaml
}

// noKubeDial fails the test if a Kubernetes client is opened: the demo is never dialled.
func noKubeDial(t *testing.T) {
	t.Helper()

	saved := kubeClients
	kubeClients = newKubeClientCache(func(target kubeTarget) (*kubeClient, error) {
		t.Errorf("the demo opened a Kubernetes client for %q", target.context)

		return nil, errors.New("dialled")
	})

	t.Cleanup(func() { kubeClients = saved })
}

func TestDemoKubeconfigImportsAsKubeDemo(t *testing.T) {
	yaml := demoKubeconfigForTest(t)
	if yaml != demoKubeconfigForTest(t) {
		t.Fatal("the kube demo must be stable across imports")
	}

	if !IsKubeconfig(yaml) {
		t.Fatal("the kube demo must be sniffed as a kubeconfig")
	}

	out, err := ParseKubeconfig(yaml)
	if err != nil {
		t.Fatal(err)
	}

	var summary kubeconfigSummary
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}

	if len(summary.Contexts) != 1 {
		t.Fatalf("contexts: %+v", summary.Contexts)
	}

	c := summary.Contexts[0]
	if c.Kind != kindKube || !c.Demo || c.Problem != "" || c.SignIn != "" || c.Auth != authToken || c.Name != demoKubeContext {
		t.Fatalf("kube demo summary: %+v", c)
	}

	// Stored next to a Talos demo and a real cluster; adding it again replaces it.
	talos := demoConfigForTest(t)
	stored, err := MergeKubeconfig("", talos, yaml, "")
	if err != nil {
		t.Fatal(err)
	}

	conflicts, err := KubeImportConflicts(stored, talos, yaml)
	if err != nil {
		t.Fatal(err)
	}

	var found []struct {
		Index  int     `json:"index"`
		SameAs *string `json:"sameAs"`
	}
	if err := json.Unmarshal([]byte(conflicts), &found); err != nil {
		t.Fatal(err)
	}

	if len(found) != 1 || found[0].SameAs == nil {
		t.Fatalf("adding the kube demo again must offer to replace it: %s", conflicts)
	}

	again, err := MergeKubeconfig(stored, talos, yaml, `[{"index":0,"replace":true}]`)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := loadKubeconfigDoc(again)
	if err != nil {
		t.Fatal(err)
	}

	if len(doc.Contexts) != 1 {
		t.Fatalf("the kube demo was duplicated: %d contexts", len(doc.Contexts))
	}

	if !isDemoContext(again, "") || !isDemoContext(again, demoKubeContext) || isDemoContext(again, "missing") {
		t.Fatal("the stored kube demo must be recognised by its context")
	}

	if isTalosDemoContext(again, demoKubeContext) || !isTalosDemoContext(talos, "") || isKubeDemoContext(talos, "") {
		t.Fatal("the two demos must be told apart")
	}
}

func TestDemoKubeNodesAreEKSFlavoured(t *testing.T) {
	noKubeDial(t)

	yaml := demoKubeconfigForTest(t)

	out, err := KubeNodes(yaml, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var nodes kubeNodesOverview
	if err := json.Unmarshal([]byte(out), &nodes); err != nil {
		t.Fatal(err)
	}

	if len(nodes.Nodes) != len(demoNodes()) {
		t.Fatalf("nodes: %+v", nodes.Nodes)
	}

	kinds, spot := map[string]bool{}, false

	for _, n := range nodes.Nodes {
		if len(n.Roles) != 0 || n.Pool == "" || n.InstanceType == "" || n.Capacity == "" || !n.Ready {
			t.Fatalf("an EKS node has a pool, a machine type and a capacity, no role: %+v", n)
		}

		kinds[n.PoolKind] = true
		spot = spot || n.Capacity == capacitySpot
	}

	if !kinds["eks"] || !kinds["karpenter"] || !spot {
		t.Fatalf("expected a managed node group, a Karpenter pool and a spot node: %+v", nodes.Nodes)
	}

	out, err = KubeWhoAmI(yaml, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var who kubeWhoAmI
	if err := json.Unmarshal([]byte(out), &who); err != nil || who.User == "" || who.Unknown {
		t.Fatalf("who am I: %s %v", out, err)
	}
}

func TestDemoKubeReadsAnswerFromTheDemoInventory(t *testing.T) {
	noKubeDial(t)

	yaml := demoKubeconfigForTest(t)

	reads := map[string]func() (string, error){
		"workloads":     func() (string, error) { return KubeWorkloads(yaml, "", "") },
		"pods":          func() (string, error) { return KubePods(yaml, "", "") },
		"node pods":     func() (string, error) { return KubeNodePodsPage(yaml, "", "", "demo-worker-1", "", "", 50, false) },
		"events":        func() (string, error) { return KubeEvents(yaml, "", "", "demo", "", "") },
		"namespaces":    func() (string, error) { return KubeNamespaces(yaml, "", "") },
		"argo cd":       func() (string, error) { return KubeArgoCD(yaml, "", "") },
		"flux":          func() (string, error) { return KubeFlux(yaml, "", "") },
		"data services": func() (string, error) { return KubeDataServices(yaml, "", "", "") },
		"action access": func() (string, error) { return KubeActionAccess(yaml, "", "", "") },
		"cron jobs":     func() (string, error) { return KubeCronJobs(yaml, "", "") },
		"helm":          func() (string, error) { return KubeHelmReleases(yaml, "", "", "") },
		"api health":    func() (string, error) { return KubeAPIHealth(yaml, "", "") },
		"checkup":       func() (string, error) { return KubeCheckup(yaml, "", "", "") },
		"drain plan":    func() (string, error) { return KubeDrainPlan(yaml, "", "", "demo-worker-1") },
	}

	for name, read := range reads {
		out, err := read()
		if err != nil || out == "" {
			t.Errorf("%s: %q %v", name, out, err)
		}
	}

	if info, err := KubeSignInInfo(yaml, demoKubeContext); err != nil || info != "" {
		t.Fatalf("the kube demo has static credentials, no sign-in: %q %v", info, err)
	}
}

func TestDemoKubeRefusesMutationsAndTalos(t *testing.T) {
	noKubeDial(t)

	yaml := demoKubeconfigForTest(t)

	mutations := map[string]func() error{
		"restart": func() error { return KubeRolloutRestart(yaml, "", "", "Deployment", "demo", "hello-ichor") },
		"delete":  func() error { return KubeDeletePod(yaml, "", "", "demo", "hello-ichor") },
		"cordon":  func() error { return KubeNodeCordon(yaml, "", "", "demo-worker-1", true) },
		"meta":    func() error { return KubeNodeMetaApply(yaml, "", "", "demo-worker-1", `{"taints":[]}`, false) },
		"argo":    func() error { return KubeArgoAction(yaml, "", "", "argocd", "demo", "sync", "") },
		"flux":    func() error { return KubeFluxAction(yaml, "", "", "Kustomization", "flux-system", "apps", "reconcile") },
	}

	for name, mutate := range mutations {
		if err := mutate(); !errors.Is(err, errDemoUnavailable) {
			t.Errorf("%s: want errDemoUnavailable, got %v", name, err)
		}
	}

	talos := map[string]func() (string, error){
		"overview":  func() (string, error) { return ClusterOverview(yaml, "") },
		"etcd":      func() (string, error) { return EtcdStatus(yaml, "") },
		"node name": func() (string, error) { return KubeNodeName(yaml, "", "192.0.2.20") },
		"discover":  func() (string, error) { return DiscoverNodes(yaml, "") },
		"storage":   func() (string, error) { return ClusterStorageHealth(yaml, "") },
	}

	for name, read := range talos {
		if _, err := read(); err == nil || err.Error() != errTalosUnavailable.Error() {
			t.Errorf("%s: want errTalosUnavailable, got %v", name, err)
		}
	}
}

func TestTalosDemoUnchangedByKubeDemo(t *testing.T) {
	noKubeDial(t)

	yaml := demoConfigForTest(t)

	out, err := KubeNodes(yaml, "", "")
	if err != nil {
		t.Fatal(err)
	}

	var nodes kubeNodesOverview
	if err := json.Unmarshal([]byte(out), &nodes); err != nil {
		t.Fatal(err)
	}

	if len(nodes.Nodes) == 0 || len(nodes.Nodes[0].Roles) == 0 || nodes.Nodes[0].Pool != "" {
		t.Fatalf("the Talos demo keeps its control planes and no cloud pools: %+v", nodes.Nodes)
	}

	if _, err := ClusterOverview(yaml, ""); err != nil {
		t.Fatalf("the Talos demo still answers Talos reads: %v", err)
	}

	if name, err := KubeNodeName(yaml, "", "192.0.2.20"); err != nil || name == "" {
		t.Fatalf("Talos demo node name: %q %v", name, err)
	}
}

// TestDemoPodNamesAreListed: every pod the other demo data links to (a PostgreSQL instance, a
// Garage instance, an app of the Apps sheet) is a pod of demoPods in the same namespace, so the
// link opens its logs instead of an empty screen.
func TestDemoPodNamesAreListed(t *testing.T) {
	listed := map[string]kubePod{}
	for _, p := range demoPods() {
		listed[p.Namespace+"/"+p.Name] = p
	}

	services := demoDataServices(time.Now())

	type link struct{ from, namespace, pod string }

	var links []link

	for _, c := range services.CNPG.Clusters {
		for _, p := range c.InstancePods {
			links = append(links, link{"cnpg " + c.Name, c.Namespace, p.Name})
		}
	}

	for _, g := range services.Garage.Instances {
		links = append(links, link{"garage " + g.Name, g.Namespace, g.Pod})
	}

	// The Apps sheet rows the demo covers; the other inventory rows stay inventory-only.
	inventory := map[string]bool{}
	for _, w := range demoWorkloads {
		inventory[w.namespace+"/"+w.pod] = true
	}

	for _, key := range []string{"demo/hello-ichor-7d9c5-fghij", "home/home-assistant-0"} {
		if !inventory[key] {
			t.Errorf("the Apps sheet no longer lists %s: update the covered rows", key)
		}

		ns, name, _ := strings.Cut(key, "/")
		links = append(links, link{"apps", ns, name})
	}

	if len(links) < 15 {
		t.Fatalf("only %d linked pods: the demo data lost its CloudNativePG or Garage pods", len(links))
	}

	for _, l := range links {
		if _, ok := listed[l.namespace+"/"+l.pod]; !ok {
			t.Errorf("%s links to %s/%s, which demoPods does not list", l.from, l.namespace, l.pod)
		}
	}

	// A CloudNativePG instance pod agrees with the PostgreSQL list on where it runs.
	for _, c := range services.CNPG.Clusters {
		for _, p := range c.InstancePods {
			got := listed[c.Namespace+"/"+p.Name]
			if got.Node != p.Node || got.Status != p.Phase {
				t.Errorf("%s: demoPods has %s on %q, the PostgreSQL list %s on %q", p.Name, got.Status, got.Node, p.Phase, p.Node)
			}
		}
	}
}
