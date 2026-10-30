package ichorgo

import (
	"context"
	"errors"
	"slices"
	"sync"
)

// lbInputs is what the Load balancing screen and the checkup's loadbalancers section read.
// metallb is nil when MetalLB is not installed, kubeVip when kube-vip is not found, pods when
// they could not be listed (nothing is then said about a controller being down), sessions
// when no speaker's metrics could be read.
type lbInputs struct {
	groups     map[string]string
	services   []lbServiceObject
	namespaces map[string]labelSet
	metallb    *metallbState
	cilium     []ciliumPoolObject
	ciliumL2   *ciliumL2State // nil without Cilium
	kubeVip    *kubeVipState
	pods       []lbPod
	podsErr    error
	sessions   bgpSessions
	// readErr is why some controller objects are missing (nil when all were read).
	readErr error
}

// errLBPartial: some of MetalLB's objects could not be read.
var errLBPartial = errors.New("some MetalLB objects could not be read")

// lbPod is a running pod of a load balancer controller, App its catalog id.
type lbPod struct {
	Namespace, Name, Node, App string
	// IPs are the pod's and its node's, when listed here (not from the checkup's pods): a
	// Prometheus series without a pod label names its target by instance address.
	IPs []string
}

// readLBInputs reads the LoadBalancer Services, then the controllers together, each allowed
// to fail. pods: the running pods when the caller has them (nil: list them here).
func readLBInputs(ctx context.Context, k *kubeClient, groups map[string]string, pods []lbPod) (lbInputs, error) {
	services, err := listLBServices(ctx, k)
	if err != nil {
		return lbInputs{}, err
	}

	in := lbInputs{groups: groups, services: services, pods: pods}

	var (
		podsErr, ciliumErr, l2Err error
		wg                        sync.WaitGroup
	)

	if _, ok := groups[groupMetalLB]; ok {
		wg.Go(func() { in.metallb = readMetalLB(ctx, k) })
	}

	if _, ok := groups[groupCilium]; ok {
		wg.Go(func() { in.cilium, ciliumErr = readCiliumPools(ctx, k) })
		wg.Go(func() { in.ciliumL2, l2Err = readCiliumL2(ctx, k) })
	}

	if pods == nil {
		wg.Go(func() { in.pods, podsErr = listLBPods(ctx, k) })
	}

	wg.Go(func() { in.kubeVip = readKubeVipConfig(ctx, k) })
	wg.Wait()

	if podsErr != nil {
		in.pods = nil
	}

	in.kubeVip = withKubeVipLeases(ctx, k, in.kubeVip, in.pods)

	if in.metallb != nil {
		in.sessions = readBGPSessions(ctx, k, in.pods)
		in.namespaces = readNamespaceLabels(ctx, k, in.metallb)
	}

	switch {
	case ciliumErr != nil:
		in.readErr = ciliumErr
	case l2Err != nil:
		in.readErr = l2Err
	case in.metallb != nil && in.metallb.partial:
		in.readErr = errLBPartial
	}

	in.podsErr = podsErr

	return in, nil
}

// listLBServices lists the Services of type LoadBalancer.
func listLBServices(ctx context.Context, k *kubeClient) ([]lbServiceObject, error) {
	services, err := listObjects[lbServiceObject](ctx, k, "/api/v1/services?fieldSelector=spec.type%3DLoadBalancer")
	if err != nil {
		// A server that cannot select on the type lists them all.
		services, err = listObjects[lbServiceObject](ctx, k, "/api/v1/services")
	}

	return slices.DeleteFunc(services, func(s lbServiceObject) bool { return s.Spec.Type != "LoadBalancer" }), err
}

// listLBPods lists the running pods of the load balancer controllers (MetalLB, kube-vip),
// told by their images through the app catalog, as the Integrations screen finds projects.
func listLBPods(ctx context.Context, k *kubeClient) ([]lbPod, error) {
	pods, err := listDSPods(ctx, k, "")
	if err != nil {
		return nil, err
	}

	out := []lbPod{}

	for _, p := range pods {
		if p.Status.Phase != "Running" {
			continue
		}

		images := make([]string, 0, len(p.Spec.Containers))
		for _, c := range p.Spec.Containers {
			images = append(images, c.Image)
		}

		if app := lbAppOf(images); app != "" {
			out = append(out, lbPod{
				Namespace: p.Metadata.Namespace, Name: p.Metadata.Name, Node: p.Spec.NodeName, App: app,
				IPs: nonEmpty([]string{p.Status.PodIP, p.Status.HostIP}),
			})
		}
	}

	return out, nil
}

// lbPodsOf keeps the running controller pods of the checkup's pods.
func lbPodsOf(pods []checkPod) []lbPod {
	out := []lbPod{}

	for _, p := range pods {
		if p.Phase != "Running" {
			continue
		}

		if app := lbAppOf(p.Images); app != "" {
			out = append(out, lbPod{Namespace: p.Namespace, Name: p.Name, Node: p.Node, App: app})
		}
	}

	return out
}

// lbAppOf is the catalog id of the load balancer controller images belong to, "" for others.
func lbAppOf(images []string) string {
	catalog := loadAppCatalog()

	for _, image := range images {
		if id := catalog.identify(parseImageRef(image)); id.app != nil && (id.app.ID == lbControllerMetalLB || id.app.ID == lbControllerKubeVip) {
			return id.app.ID
		}
	}

	return ""
}

// runs tells whether a pod of app runs; true when the pods are unknown (nothing to say).
func (in lbInputs) runs(app string) bool {
	return in.pods == nil || slices.ContainsFunc(in.pods, func(p lbPod) bool { return p.App == app })
}

// controllers lists the controllers found, in the order they take a Service without class.
// A kube-vip without its ConfigMap often holds only the control plane's address (kubeadm):
// it comes last, after Cilium, so it does not take the Services Cilium serves.
func (in lbInputs) controllers() []string {
	out := []string{}

	if in.metallb != nil {
		out = append(out, lbControllerMetalLB)
	}

	if in.kubeVip != nil && in.kubeVip.configMap {
		out = append(out, lbControllerKubeVip)
	}

	if _, ok := in.groups[groupCilium]; ok {
		out = append(out, lbControllerCilium)
	}

	if in.kubeVip != nil && !in.kubeVip.configMap {
		out = append(out, lbControllerKubeVip)
	}

	return out
}

// readNamespaceLabels reads the namespaces' labels when a pool selects namespaces by label.
func readNamespaceLabels(ctx context.Context, k *kubeClient, m *metallbState) map[string]labelSet {
	if !slices.ContainsFunc(m.pools, func(p metallbPool) bool { return len(p.Spec.ServiceAllocation.NamespaceSelectors) > 0 }) {
		return nil
	}

	namespaces, err := listObjects[conditionedObject](ctx, k, "/api/v1/namespaces")
	if err != nil {
		return nil
	}

	out := make(map[string]labelSet, len(namespaces))
	for _, ns := range namespaces {
		out[ns.Metadata.Name] = labelSet(ns.Metadata.Labels)
	}

	return out
}
