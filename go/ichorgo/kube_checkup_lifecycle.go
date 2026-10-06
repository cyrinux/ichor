package ichorgo

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The findings of the loadbalancers section.
const (
	findLBPending       = "lbPending"       // a LoadBalancer Service without an address (Extra: its class)
	findLBPoolExhausted = "lbPoolExhausted" // a Cilium pool with no address left (Value: its size)
	findLBPoolConflict  = "lbPoolConflict"  // a Cilium pool overlapping another (Message)
)

// The findings of the terminating section: deleted, and still there. Since is when the
// deletion was asked, Message what is in the way (finalizers, remaining content).
const (
	findNamespaceTerminating = "namespaceTerminating"
	findPodTerminating       = "podTerminating"
	findPVCTerminating       = "pvcTerminating"
)

// The findings of the certificates section, one per signer and requester. Name: who asks
// (a node's kubelet), Extra: the signer, Count: how many requests, Since: the oldest.
const (
	findCSRPending = "csrPending"
	findCSRDenied  = "csrDenied"
)

const (
	// lbGrace is how long a controller may take to give a Service its address.
	lbGrace  = 2 * time.Minute
	csrGrace = 10 * time.Minute
	// csrDeniedRecent is how long a refused request is worth showing.
	csrDeniedRecent = 24 * time.Hour
)

type lbServiceObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		Type              string `json:"type"`
		LoadBalancerClass string `json:"loadBalancerClass"`
	} `json:"spec"`
	Status struct {
		LoadBalancer struct {
			Ingress []struct {
				IP       string `json:"ip"`
				Hostname string `json:"hostname"`
			} `json:"ingress"`
		} `json:"loadBalancer"`
	} `json:"status"`
}

type conditionedObject struct {
	Metadata checkMeta `json:"metadata"`
	Status   struct {
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}

// checkupLoadBalancers finds the Services of type LoadBalancer nothing gave an address to,
// and with Cilium the pools that ran out. Absent when the cluster has neither.
func checkupLoadBalancers(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	services, err := listObjects[lbServiceObject](ctx, k, "/api/v1/services?fieldSelector=spec.type%3DLoadBalancer")
	if err != nil {
		// A server that cannot select on the type lists them all.
		services, err = listObjects[lbServiceObject](ctx, k, "/api/v1/services")
	}

	services = slices.DeleteFunc(services, func(s lbServiceObject) bool { return s.Spec.Type != "LoadBalancer" })

	var (
		pools   []conditionedObject
		poolErr error
	)

	if _, ok := in.groups[groupCilium]; ok {
		pools, poolErr = readCiliumPools(ctx, k)
	}

	if err == nil && poolErr == nil && len(services) == 0 && len(pools) == 0 {
		return absentSection(checkLoadBalancers)
	}

	findings := lbFindings(services, in.now)
	findings = append(findings, poolFindings(pools, len(findings) > 0)...)

	return newSection(checkLoadBalancers, len(services)+len(pools), findings, err, poolErr)
}

// readCiliumPools reads Cilium's load balancer IP pools, whichever version serves them.
func readCiliumPools(ctx context.Context, k *kubeClient) ([]conditionedObject, error) {
	pools, err := listObjects[conditionedObject](ctx, k, "/apis/cilium.io/v2/ciliumloadbalancerippools")
	if isNotFound(err) {
		pools, err = listObjects[conditionedObject](ctx, k, "/apis/cilium.io/v2alpha1/ciliumloadbalancerippools")
	}

	return pools, ignoreNotFound(err)
}

func lbFindings(services []lbServiceObject, now time.Time) []checkupFinding {
	findings := []checkupFinding{}

	for _, s := range services {
		if len(s.Status.LoadBalancer.Ingress) > 0 || s.Metadata.DeletionTimestamp != nil || !olderThan(s.Metadata.CreationTimestamp, now, lbGrace) {
			continue
		}

		findings = append(findings, checkupFinding{
			Kind: findLBPending, Severity: sevCritical, Namespace: s.Metadata.Namespace, Name: s.Metadata.Name,
			Extra: s.Spec.LoadBalancerClass, Since: milli(s.Metadata.CreationTimestamp),
		})
	}

	return findings
}

// poolFindings reads the conditions Cilium keeps on a pool: cilium.io/IPsAvailable and
// cilium.io/IPsTotal carry their number as the message. An empty pool is critical once a
// Service waits for an address.
func poolFindings(pools []conditionedObject, waiting bool) []checkupFinding {
	findings := []checkupFinding{}

	for _, p := range pools {
		conds := kubeConditions(p.Status.Conditions)

		if c := conds.get("cilium.io/PoolConflict"); c.Status == "True" {
			findings = append(findings, checkupFinding{Kind: findLBPoolConflict, Severity: sevWarning, Name: p.Metadata.Name, Message: c.Message})
		}

		available, err := strconv.Atoi(strings.TrimSpace(conds.get("cilium.io/IPsAvailable").Message))
		if err != nil || available > 0 {
			continue
		}

		total, _ := strconv.Atoi(strings.TrimSpace(conds.get("cilium.io/IPsTotal").Message)) //nolint:errcheck // 0 when unknown

		f := checkupFinding{Kind: findLBPoolExhausted, Severity: sevWarning, Name: p.Metadata.Name, Value: float64(total)}
		if waiting {
			f.Severity = sevCritical
		}

		findings = append(findings, f)
	}

	return findings
}

// checkupTerminating finds what was deleted long ago and is still there.
func checkupTerminating(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	namespaces, err := listObjects[conditionedObject](ctx, k, "/api/v1/namespaces")
	findings := []checkupFinding{}

	for _, ns := range namespaces {
		if !olderThan(ns.Metadata.deleted(), in.now, terminatingGrace) {
			continue
		}

		var why []string

		for _, c := range ns.Status.Conditions {
			// NamespaceContentRemaining, NamespaceFinalizersRemaining, NamespaceDeletion*Failure.
			if c.Status == "True" && (strings.HasSuffix(c.Type, "Remaining") || strings.HasSuffix(c.Type, "Failure")) {
				why = append(why, c.Message)
			}
		}

		findings = append(findings, checkupFinding{
			Kind: findNamespaceTerminating, Severity: sevWarning, Name: ns.Metadata.Name,
			Message: strings.Join(why, " "), Since: milli(ns.Metadata.deleted()),
		})
	}

	for _, p := range in.pods {
		if olderThan(p.Deleted, in.now, terminatingGrace) {
			findings = append(findings, checkupFinding{
				Kind: findPodTerminating, Severity: sevWarning, Namespace: p.Namespace, Name: p.Name, Node: p.Node,
				Message: strings.Join(p.Finalizers, ", "), Since: milli(p.Deleted),
			})
		}
	}

	for _, pvc := range in.pvcs {
		if olderThan(pvc.Metadata.deleted(), in.now, terminatingGrace) {
			findings = append(findings, checkupFinding{
				Kind: findPVCTerminating, Severity: sevWarning, Namespace: pvc.Metadata.Namespace, Name: pvc.Metadata.Name,
				Message: strings.Join(pvc.Metadata.Finalizers, ", "), Since: milli(pvc.Metadata.deleted()),
			})
		}
	}

	return newSection(checkTerminating, len(namespaces)+len(in.pods)+len(in.pvcs), findings, err, in.podsErr, in.pvcsErr)
}

type csrObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		SignerName string `json:"signerName"`
		Username   string `json:"username"`
	} `json:"spec"`
	Status struct {
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}

// checkupCSRs finds the certificate requests nobody answered: a kubelet asks for its
// serving certificate again every few minutes, so they pile up, one finding per requester.
func checkupCSRs(ctx context.Context, k *kubeClient, in checkupInput) checkupSection {
	csrs, err := listObjects[csrObject](ctx, k, "/apis/certificates.k8s.io/v1/certificatesigningrequests")

	return newSection(checkCertificates, len(csrs), csrFindings(csrs, in.now), err)
}

func csrFindings(csrs []csrObject, now time.Time) []checkupFinding {
	groups := map[string]*checkupFinding{}
	order := []string{}

	add := func(kind, severity string, csr csrObject, message string) {
		key := promKey(kind, csr.Spec.SignerName, csr.Spec.Username)

		f := groups[key]
		if f == nil {
			f = &checkupFinding{Kind: kind, Severity: severity, Name: csr.Spec.Username, Extra: csr.Spec.SignerName, Message: message}
			groups[key] = f
			order = append(order, key)
		}

		f.Count++

		if created := milli(csr.Metadata.CreationTimestamp); f.Since == 0 || created < f.Since {
			f.Since = created
		}
	}

	for _, csr := range csrs {
		conds := kubeConditions(csr.Status.Conditions)

		switch denied := conds.get("Denied"); {
		case denied.Status == "True":
			if !olderThan(csr.Metadata.CreationTimestamp, now, csrDeniedRecent) {
				add(findCSRDenied, sevInfo, csr, denied.Message)
			}
		case conds.is("Approved") || conds.is("Failed"):
		case olderThan(csr.Metadata.CreationTimestamp, now, csrGrace):
			add(findCSRPending, sevWarning, csr, "")
		}
	}

	findings := make([]checkupFinding, 0, len(order))
	for _, key := range order {
		findings = append(findings, *groups[key])
	}

	return findings
}
