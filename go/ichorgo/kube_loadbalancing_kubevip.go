package ichorgo

import (
	"context"
	"slices"
	"strings"
	"time"
)

// kube-vip's cloud provider reads its ranges from this ConfigMap: cidr-global,
// range-global, cidr-<namespace>, range-<namespace>.
const (
	kubeVipNamespace = "kube-system"
	kubeVipConfigMap = "kubevip"
	kubeVipGlobal    = "global"
)

// The namespaces kube-vip keeps its leases in by default, and the prefixes of their names.
var (
	kubeVipLeaseNamespaces = []string{"kube-system", "kube-vip"}
	kubeVipLeasePrefixes   = []string{"plndr-", "kubevip-"}
)

// lbKubeVip is what kube-vip announces: its address ranges by namespace and its leases.
type lbKubeVip struct {
	Ranges []lbKubeVipRange `json:"ranges"`
	Leases []lbKubeVipLease `json:"leases"`
}

// lbKubeVipRange: Scope is "global" or a namespace; CIDRs its cidr and range lines.
type lbKubeVipRange struct {
	Scope string   `json:"scope"`
	CIDRs []string `json:"cidrs"`
}

type lbKubeVipLease struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Holder    string `json:"holder"`
	Renewed   int64  `json:"renewed,omitempty"` // unix ms
}

// kubeVipState is kube-vip as found: configMap when its ConfigMap exists.
type kubeVipState struct {
	configMap bool
	ranges    []lbKubeVipRange
	leases    []lbKubeVipLease
}

func (v *kubeVipState) rows() *lbKubeVip {
	if v == nil {
		return nil
	}

	return &lbKubeVip{Ranges: nonNil(v.ranges), Leases: nonNil(v.leases)}
}

// readKubeVipConfig reads kube-vip's ranges; nil when its ConfigMap is absent or hidden.
func readKubeVipConfig(ctx context.Context, k *kubeClient) *kubeVipState {
	var cm struct {
		Data map[string]string `json:"data"`
	}

	if err := k.get(ctx, "/api/v1/namespaces/"+kubeVipNamespace+"/configmaps/"+kubeVipConfigMap, &cm); err != nil {
		return nil
	}

	return &kubeVipState{configMap: true, ranges: kubeVipRanges(cm.Data)}
}

// kubeVipRanges groups the cidr-* and range-* keys by scope, global first.
func kubeVipRanges(data map[string]string) []lbKubeVipRange {
	byScope := map[string][]string{}

	for key, value := range data {
		scope, ok := strings.CutPrefix(key, "cidr-")
		if !ok {
			scope, ok = strings.CutPrefix(key, "range-")
		}

		if !ok || scope == "" {
			continue
		}

		for v := range strings.SplitSeq(value, ",") {
			if v = strings.TrimSpace(v); v != "" {
				byScope[scope] = append(byScope[scope], v)
			}
		}
	}

	out := make([]lbKubeVipRange, 0, len(byScope))
	for scope, cidrs := range byScope {
		slices.Sort(cidrs)
		out = append(out, lbKubeVipRange{Scope: scope, CIDRs: cidrs})
	}

	slices.SortFunc(out, func(a, b lbKubeVipRange) int {
		switch {
		case a.Scope == kubeVipGlobal:
			return -1
		case b.Scope == kubeVipGlobal:
			return 1
		default:
			return strings.Compare(a.Scope, b.Scope)
		}
	})

	return out
}

type leaseObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		HolderIdentity string     `json:"holderIdentity"`
		RenewTime      *time.Time `json:"renewTime"`
	} `json:"spec"`
}

// withKubeVipLeases completes v with kube-vip's leases when kube-vip runs or is configured;
// nil when neither. Lease names are configurable: only those kube-vip's default names or
// pods account for are kept, and none found is not an error.
func withKubeVipLeases(ctx context.Context, k *kubeClient, v *kubeVipState, pods []lbPod) *kubeVipState {
	holders := map[string]bool{}

	for _, p := range pods {
		if p.App == lbControllerKubeVip {
			holders[p.Name] = true
			holders[p.Node] = true
		}
	}

	if v == nil && len(holders) == 0 {
		return nil
	}

	if v == nil {
		v = &kubeVipState{}
	}

	v.leases = []lbKubeVipLease{}

	for _, ns := range kubeVipLeaseNamespaces {
		leases, _ := listObjects[leaseObject](ctx, k, "/apis/coordination.k8s.io/v1/namespaces/"+ns+"/leases") //nolint:errcheck // none found

		for _, l := range leases {
			named := slices.ContainsFunc(kubeVipLeasePrefixes, func(p string) bool { return strings.HasPrefix(l.Metadata.Name, p) })
			if !named && !holders[l.Spec.HolderIdentity] {
				continue
			}

			row := lbKubeVipLease{Name: l.Metadata.Name, Namespace: ns, Holder: l.Spec.HolderIdentity}
			if l.Spec.RenewTime != nil {
				row.Renewed = l.Spec.RenewTime.UnixMilli()
			}

			v.leases = append(v.leases, row)
		}
	}

	return v
}
