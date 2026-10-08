package ichorgo

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"sync"
	"time"
)

const (
	groupCilium = "cilium.io"
	// groupCalicoCRD stores Calico's objects with the Kubernetes datastore: always served
	// then, API server or not. groupCalico is the Calico API server (projectcalico.org/v3).
	groupCalicoCRD = "crd.projectcalico.org"
	groupCalico    = "projectcalico.org"
	// runningPods are the pods policies apply to: neither Succeeded nor Failed.
	runningPods = "status.phase!=Succeeded,status.phase!=Failed"
)

// netPolicyReport is every network policy of the cluster, and how isolated each namespace is.
type netPolicyReport struct {
	Cilium     bool             `json:"cilium"` // the Cilium policy CRDs are served
	Calico     bool             `json:"calico"` // Calico's policy API is served
	Policies   []netPolicy      `json:"policies"`
	Namespaces []netPolicyNSRow `json:"namespaces"`
	Error      string           `json:"error,omitempty"` // a kind that could not be read
}

// netPolicyNSRow sums up one namespace: its pods, and how many of them only receive
// (Ingress) or send (Egress) what a policy allows.
type netPolicyNSRow struct {
	Namespace       string `json:"namespace"`
	Pods            int    `json:"pods"`
	IngressIsolated int    `json:"ingressIsolated"`
	EgressIsolated  int    `json:"egressIsolated"`
	Policies        int    `json:"policies"` // namespaced policies, not cluster-wide ones
}

// npPod holds what policy matching needs of a pod.
type npPod struct {
	Metadata struct {
		Name      string            `json:"name"`
		Namespace string            `json:"namespace"`
		Labels    map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		ServiceAccountName string `json:"serviceAccountName"`
		HostNetwork        bool   `json:"hostNetwork"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase"`
	} `json:"status"`
}

type npNamespace struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
}

// KubeNetworkPolicies lists the cluster's network policies through the Kubernetes API
// (os:admin): Kubernetes NetworkPolicies and, with Cilium, CiliumNetworkPolicies and
// CiliumClusterwideNetworkPolicies, with Calico its NetworkPolicies and
// GlobalNetworkPolicies, as {"cilium","calico","policies","namespaces","error"} (see
// netPolicyReport). kubeServer: see KubePods.
func KubeNetworkPolicies(configYAML, contextName, kubeServer string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName = unmaskContext(configYAML, contextName)

	return kubeReadJSON(kubeTarget{configYAML, contextName, kubeServer}, demoNetPolicies, readNetPolicyReport)
}

func readNetPolicyReport(ctx context.Context, k *kubeClient) (netPolicyReport, error) {
	var (
		policies   []netPolicy
		cni        policyCNIs
		policyErr  error
		pods       kubeList[npPod]
		namespaces kubeList[npNamespace]
		errs       = make([]error, 2)
		wg         sync.WaitGroup
	)

	wg.Go(func() { policies, cni, policyErr = readNetPolicies(ctx, k) })
	// Finished pods are left out below anyway: the server drops them before sending.
	wg.Go(func() { errs[0] = getList(ctx, k, "/api/v1/pods?fieldSelector="+url.QueryEscape(runningPods), &pods) })
	wg.Go(func() { errs[1] = getList(ctx, k, "/api/v1/namespaces", &namespaces) })
	wg.Wait()

	if policies == nil && policyErr != nil {
		return netPolicyReport{}, policyErr
	}

	for _, err := range errs {
		if err != nil {
			return netPolicyReport{}, err
		}
	}

	report := netPolicyReport{Cilium: cni.cilium, Calico: cni.calico, Policies: policies}
	if policyErr != nil {
		report.Error = sectionError(policyErr)
	}

	report.Namespaces = attachPolicyPods(report.Policies, pods.Items, namespaces.Items)

	return report, nil
}

// policyCNIs tells which CNI's policy API the cluster serves.
type policyCNIs struct {
	cilium, calico bool
}

// readNetPolicies reads the policies of every kind the cluster serves. A CNI's kind that
// fails is returned with the error; the Kubernetes one failing fails the call.
func readNetPolicies(ctx context.Context, k *kubeClient) ([]netPolicy, policyCNIs, error) {
	groups, err := readAPIGroups(ctx, k)
	if err != nil {
		return nil, policyCNIs{}, err
	}

	var (
		nps            kubeList[npObject]
		cilium, calico []netPolicy
		errs           = make([]error, 3)
		wg             sync.WaitGroup
		cni            policyCNIs
	)

	ciliumVersion, ok := groups[groupCilium]
	cni.cilium = ok
	calicoAPI, ok := calicoPolicyAPI(groups)
	cni.calico = ok

	wg.Go(func() { errs[0] = getList(ctx, k, "/apis/networking.k8s.io/v1/networkpolicies", &nps) })

	if cni.cilium {
		wg.Go(func() { cilium, errs[1] = readCiliumPolicies(ctx, k, "/apis/"+groupCilium+"/"+ciliumVersion) })
	}

	if cni.calico {
		wg.Go(func() { calico, errs[2] = readCalicoPolicies(ctx, k, calicoAPI) })
	}

	wg.Wait()

	if errs[0] != nil {
		return nil, cni, errs[0]
	}

	out := make([]netPolicy, 0, len(nps.Items)+len(cilium)+len(calico))

	for _, o := range nps.Items {
		out = append(out, mapNetworkPolicy(o))
	}

	out = append(out, cilium...)
	out = append(out, calico...)

	slices.SortFunc(out, func(a, b netPolicy) int {
		return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name), cmp.Compare(a.Kind, b.Kind))
	})

	return out, cni, cmp.Or(errs[1], errs[2])
}

// readCiliumPolicies reads the CiliumNetworkPolicies and CiliumClusterwideNetworkPolicies
// api serves; a kind the server does not know is skipped.
func readCiliumPolicies(ctx context.Context, k *kubeClient, api string) ([]netPolicy, error) {
	var (
		cnps, ccnps kubeList[cnpObject]
		errs        = make([]error, 2)
		wg          sync.WaitGroup
	)

	wg.Go(func() { errs[0] = ignoreNotFound(getList(ctx, k, api+"/ciliumnetworkpolicies", &cnps)) })
	wg.Go(func() { errs[1] = ignoreNotFound(getList(ctx, k, api+"/ciliumclusterwidenetworkpolicies", &ccnps)) })
	wg.Wait()

	out := make([]netPolicy, 0, len(cnps.Items)+len(ccnps.Items))

	for _, o := range cnps.Items {
		out = append(out, mapCiliumPolicy(o, false))
	}

	for _, o := range ccnps.Items {
		out = append(out, mapCiliumPolicy(o, true))
	}

	return out, cmp.Or(errs[0], errs[1])
}

// calicoPolicyAPI is where to read Calico's policies: its CRDs when served (the Kubernetes
// datastore), else the Calico API server; "" and false without Calico.
func calicoPolicyAPI(groups map[string]string) (string, bool) {
	if version, ok := groups[groupCalicoCRD]; ok {
		return "/apis/" + groupCalicoCRD + "/" + version, true
	}

	if version, ok := groups[groupCalico]; ok {
		return "/apis/" + groupCalico + "/" + version, true
	}

	return "", false
}

// readCalicoPolicies reads Calico's NetworkPolicies and GlobalNetworkPolicies from api. The
// API server also lists the Kubernetes policies it mirrors, which are read apart.
func readCalicoPolicies(ctx context.Context, k *kubeClient, api string) ([]netPolicy, error) {
	var (
		nps, gnps kubeList[calicoPolicyObject]
		errs      = make([]error, 2)
		wg        sync.WaitGroup
	)

	wg.Go(func() { errs[0] = ignoreNotFound(getList(ctx, k, api+"/networkpolicies", &nps)) })
	wg.Go(func() { errs[1] = ignoreNotFound(getList(ctx, k, api+"/globalnetworkpolicies", &gnps)) })
	wg.Wait()

	out := make([]netPolicy, 0, len(nps.Items)+len(gnps.Items))

	for _, o := range nps.Items {
		if calicoPolicyName(o.Metadata.Name) {
			out = append(out, mapCalicoPolicy(o, false))
		}
	}

	for _, o := range gnps.Items {
		if calicoPolicyName(o.Metadata.Name) {
			out = append(out, mapCalicoPolicy(o, true))
		}
	}

	return out, cmp.Or(errs[0], errs[1])
}

// attachPolicyPods fills each policy's Pods and returns the namespace rows. Pods on the
// host network and finished pods are left out: policies do not apply to them.
func attachPolicyPods(policies []netPolicy, pods []npPod, namespaces []npNamespace) []netPolicyNSRow {
	nsLabels := make(map[string]map[string]string, len(namespaces))
	for _, ns := range namespaces {
		nsLabels[ns.Metadata.Name] = ns.Metadata.Labels
	}

	rows := map[string]*netPolicyNSRow{}
	row := func(ns string) *netPolicyNSRow {
		if r, ok := rows[ns]; ok {
			return r
		}

		r := &netPolicyNSRow{Namespace: ns}
		rows[ns] = r

		return r
	}

	for i := range policies {
		policies[i].Pods = []string{}
		if policies[i].Namespace != "" {
			row(policies[i].Namespace).Policies++
		}
	}

	for _, pod := range pods {
		m := pod.Metadata
		if pod.Spec.HostNetwork || pod.Status.Phase == "Succeeded" || pod.Status.Phase == "Failed" {
			continue
		}

		labels := podLabelSet(m.Labels, m.Namespace, pod.Spec.ServiceAccountName, nsLabels[m.Namespace])
		r := row(m.Namespace)
		r.Pods++

		var ingress, egress bool

		for i := range policies {
			p := &policies[i]

			selected, in, out := p.isolation(m.Namespace, labels)
			if !selected {
				continue
			}

			p.PodCount++
			if len(p.Pods) < netPolicyMaxPods {
				p.Pods = append(p.Pods, m.Namespace+"/"+m.Name)
			}

			ingress, egress = ingress || in, egress || out
		}

		if ingress {
			r.IngressIsolated++
		}

		if egress {
			r.EgressIsolated++
		}
	}

	out := make([]netPolicyNSRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}

	slices.SortFunc(out, func(a, b netPolicyNSRow) int { return cmp.Compare(a.Namespace, b.Namespace) })

	return out
}

// isolatingPolicies are the policies that put the endpoint (namespace, labels) in
// default-deny for direction: the ones to read when traffic to or from it is denied.
func isolatingPolicies(policies []netPolicy, namespace string, labels labelSet, direction string) []policyRef {
	var out []policyRef

	for i := range policies {
		if policies[i].isolates(namespace, labels, direction) {
			out = append(out, policies[i].ref())
		}
	}

	return out
}

// policyRefreshInterval is how often a live flow view rereads the policies.
const policyRefreshInterval = 30 * time.Second
