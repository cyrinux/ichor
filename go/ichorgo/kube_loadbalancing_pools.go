package ichorgo

import (
	"cmp"
	"encoding/binary"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// maxPoolSize caps a pool's size: an IPv6 /64 is not counted address by address.
const maxPoolSize = 1 << 30

// addrRange is the addresses of a pool line: a CIDR or "first-last".
type addrRange struct{ from, to netip.Addr }

// parseAddrRange reads "192.0.2.0/28" or "192.0.2.10-192.0.2.20"; false when it cannot.
func parseAddrRange(s string) (addrRange, bool) {
	s = strings.TrimSpace(s)

	if first, last, ok := strings.Cut(s, "-"); ok {
		from, err1 := netip.ParseAddr(strings.TrimSpace(first))
		to, err2 := netip.ParseAddr(strings.TrimSpace(last))

		if err1 != nil || err2 != nil || from.Is4() != to.Is4() || to.Less(from) {
			return addrRange{}, false
		}

		return addrRange{from, to}, true
	}

	p, err := netip.ParsePrefix(s)
	if err != nil {
		a, err := netip.ParseAddr(s)

		return addrRange{a, a}, err == nil
	}

	p = p.Masked()

	return addrRange{p.Addr(), lastAddr(p)}, true
}

// lastAddr is the last address of p (its host bits set).
func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().As16()
	offset := 0

	if p.Addr().Is4() {
		offset = 96
	}

	for bit := offset + p.Bits(); bit < 128; bit++ {
		b[bit/8] |= 1 << (7 - bit%8)
	}

	a := netip.AddrFrom16(b)
	if p.Addr().Is4() {
		return a.Unmap()
	}

	return a
}

// size counts the addresses of r, capped at maxPoolSize.
func (r addrRange) size() int {
	from, to := r.from.As16(), r.to.As16()

	if binary.BigEndian.Uint64(from[:8]) != binary.BigEndian.Uint64(to[:8]) {
		return maxPoolSize
	}

	diff := binary.BigEndian.Uint64(to[8:]) - binary.BigEndian.Uint64(from[8:])
	if diff >= maxPoolSize {
		return maxPoolSize
	}

	return int(diff) + 1
}

func (r addrRange) contains(a netip.Addr) bool {
	a = a.Unmap()

	return a.Is4() == r.from.Is4() && !a.Less(r.from) && !r.to.Less(a)
}

// poolRanges parses the address lines of a pool, skipping those it cannot read.
func poolRanges(lines []string) []addrRange {
	out := []addrRange{}

	for _, l := range lines {
		if r, ok := parseAddrRange(l); ok {
			out = append(out, r)
		}
	}

	return out
}

func rangesSize(ranges []addrRange) int {
	// Capped before adding: int is 32 bits on android/arm, where two capped ranges would overflow.
	total := 0
	for _, r := range ranges {
		total = min(total, maxPoolSize-r.size()) + r.size()
	}

	return total
}

func rangesContain(ranges []addrRange, address string) bool {
	a, err := netip.ParseAddr(address)

	return err == nil && slices.ContainsFunc(ranges, func(r addrRange) bool { return r.contains(a) })
}

// ciliumPoolObject is a CiliumLoadBalancerIPPool: its blocks (cidrs before v2), the Services
// it serves, and the conditions where Cilium counts its addresses.
type ciliumPoolObject struct {
	Metadata checkMeta `json:"metadata"`
	Spec     struct {
		Blocks []struct {
			CIDR  string `json:"cidr"`
			Start string `json:"start"`
			Stop  string `json:"stop"`
		} `json:"blocks"`
		CIDRs []struct {
			CIDR string `json:"cidr"`
		} `json:"cidrs"`
		ServiceSelector *labelSelector `json:"serviceSelector"`
		Disabled        bool           `json:"disabled"`
	} `json:"spec"`
	Status struct {
		Conditions []kubeCondition `json:"conditions"`
	} `json:"status"`
}

func (p ciliumPoolObject) addresses() []string {
	out := []string{}

	for _, b := range p.Spec.Blocks {
		switch {
		case b.CIDR != "":
			out = append(out, b.CIDR)
		case b.Start != "":
			out = append(out, b.Start+"-"+cmp.Or(b.Stop, b.Start))
		}
	}

	for _, c := range p.Spec.CIDRs {
		out = append(out, c.CIDR)
	}

	return out
}

// counts is what Cilium's conditions say of the pool: false when they say nothing.
func (p ciliumPoolObject) counts() (total, available int, ok bool) {
	conds := kubeConditions(p.Status.Conditions)

	available, err1 := strconv.Atoi(strings.TrimSpace(conds.get("cilium.io/IPsAvailable").Message))
	total, err2 := strconv.Atoi(strings.TrimSpace(conds.get("cilium.io/IPsTotal").Message))

	return total, available, err1 == nil && err2 == nil
}

// serves tells whether the pool may give svc an address: enabled, and its selector (if any)
// matches the Service's labels or the name and namespace Cilium adds to them.
func (p ciliumPoolObject) serves(svc lbServiceObject) bool {
	if p.Spec.Disabled {
		return false
	}

	return p.Spec.ServiceSelector == nil || p.Spec.ServiceSelector.matches(ciliumServiceLabels(svc))
}

// lbPoolRows lists MetalLB's and Cilium's pools with what is left of them. waiting: some
// Service waits for an address, which makes an exhausted pool critical.
func lbPoolRows(in lbInputs, waiting bool) []lbPool {
	out := []lbPool{}

	if in.metallb != nil {
		for _, p := range in.metallb.pools {
			out = append(out, metallbPoolRow(in, p, waiting))
		}
	}

	for _, p := range in.cilium {
		out = append(out, ciliumPoolRow(in, p, waiting))
	}

	return out
}

func metallbPoolRow(in lbInputs, p metallbPool, waiting bool) lbPool {
	ranges := poolRanges(p.Spec.Addresses)
	row := lbPool{
		Name: p.Metadata.Name, Controller: lbControllerMetalLB, Addresses: nonNil(p.Spec.Addresses), Total: rangesSize(ranges),
		AutoAssign: p.Spec.AutoAssign == nil || *p.Spec.AutoAssign, Namespaces: nonNil(p.Spec.ServiceAllocation.Namespaces),
		Priority: p.Spec.ServiceAllocation.Priority, AdvertisedBy: in.metallb.advertisers(p),
	}

	row.Services, row.Assigned = servicesInPool(in.services, p.Metadata.Name, ranges)
	row.Available = max(row.Total-row.Assigned, 0)

	if s := p.Status; s != nil && s.AssignedIPv4+s.AssignedIPv6+s.AvailableIPv4+s.AvailableIPv6 > 0 {
		row.Assigned, row.Available, row.CountsKnown = s.AssignedIPv4+s.AssignedIPv6, s.AvailableIPv4+s.AvailableIPv6, true
	}

	row.Level = poolLevel(row.Available == 0, waiting, len(row.AdvertisedBy) == 0)

	return row
}

func ciliumPoolRow(in lbInputs, p ciliumPoolObject, waiting bool) lbPool {
	ranges := poolRanges(p.addresses())
	row := lbPool{
		Name: p.Metadata.Name, Controller: lbControllerCilium, Addresses: p.addresses(), Total: rangesSize(ranges),
		AutoAssign: !p.Spec.Disabled, Namespaces: []string{}, AdvertisedBy: in.ciliumL2.advertisers(p, in.services),
	}

	row.Services, row.Assigned = servicesInPool(in.services, "", ranges)
	row.Available = max(row.Total-row.Assigned, 0)

	if total, available, ok := p.counts(); ok {
		row.Total, row.Available, row.Assigned, row.CountsKnown = total, available, max(total-available, 0), true
	}

	row.Level = poolLevel(row.Available == 0, waiting, in.ciliumL2.unannounced(p, in.services))

	return row
}

// poolLevel: an exhausted pool is critical while a Service waits, else a warning, as is a
// pool nothing advertises.
func poolLevel(exhausted, waiting, unadvertised bool) string {
	switch {
	case exhausted && waiting:
		return serviceCritical
	case exhausted, unadvertised:
		return serviceWarning
	default:
		return serviceOK
	}
}

// servicesInPool lists the Services with an address from the pool (MetalLB's annotation, or
// an address in its ranges) and counts the addresses they hold there.
func servicesInPool(services []lbServiceObject, pool string, ranges []addrRange) (names []string, addresses int) {
	names = []string{}

	for _, s := range services {
		in := 0

		for _, ing := range s.Status.LoadBalancer.Ingress {
			if rangesContain(ranges, ing.IP) {
				in++
			}
		}

		if in > 0 || pool != "" && s.Metadata.Annotations[metallbAllocatedFrom] == pool {
			names = append(names, s.Metadata.Namespace+"/"+s.Metadata.Name)
			addresses += in
		}
	}

	return names, addresses
}
