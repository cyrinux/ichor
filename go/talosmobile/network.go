package talosmobile

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	"github.com/siderolabs/talos/pkg/machinery/nethelpers"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

type nodeNetwork struct {
	Links       []linkInfo        `json:"links"`
	Addresses   []addressInfo     `json:"addresses"`
	Routes      []routeInfo       `json:"routes"`
	Resolvers   []string          `json:"resolvers"`
	TimeServers []string          `json:"timeServers"`
	Errors      map[string]string `json:"errors"` // section -> error, for sections that failed
}

type linkInfo struct {
	Name         string `json:"name"`
	Type         string `json:"type"` // ether, loopback, ...
	Kind         string `json:"kind"` // bond, vlan, veth, wireguard... (empty for physical)
	State        string `json:"state"`
	HardwareAddr string `json:"hardwareAddr"`
	MTU          uint32 `json:"mtu"`
	SpeedMbit    int    `json:"speedMbit"`
	Virtual      bool   `json:"virtual"` // pod/CNI plumbing (veth, lxc*, cilium_*...)
}

type addressInfo struct {
	Address string `json:"address"` // prefix, e.g. 192.168.1.2/24
	Link    string `json:"link"`
	Family  string `json:"family"` // inet4 | inet6
	Scope   string `json:"scope"`
	Virtual bool   `json:"virtual"` // on a virtual (CNI/pod) link
}

type routeInfo struct {
	Destination string `json:"destination"` // "default" for the default route
	Gateway     string `json:"gateway"`
	Link        string `json:"link"`
	Metric      uint32 `json:"metric"`
	Table       string `json:"table"`
	Family      string `json:"family"`
	Virtual     bool   `json:"virtual"` // via a virtual (CNI/pod) link
}

// virtualLinkPrefixes name the CNI/pod links that clutter a node's link list.
var virtualLinkPrefixes = []string{"lxc", "cilium_", "flannel", "cni", "kube-ipvs", "vxlan", "genev_sys"}

// NodeNetwork returns node's links, addresses, main-table routes, DNS resolvers and time
// servers, like `talosctl get links/addresses/routes/resolvers/timeservers` (os:reader).
// Each section is best-effort; only a total failure is an error.
func NodeNetwork(configYAML, contextName, node string) (out string, err error) {
	defer maskResult(&out, &err)

	contextName, node = unmaskTarget(configYAML, contextName, node)

	return withSession(configYAML, contextName, callTimeout, func(ctx context.Context, s *session) (string, error) {
		if err := validatePowerTarget(s.context, node); err != nil {
			return "", err
		}

		nodeCtx := withNode(ctx, node)
		st := s.client.COSI
		out := nodeNetwork{
			Links: []linkInfo{}, Addresses: []addressInfo{}, Routes: []routeInfo{},
			Resolvers: []string{}, TimeServers: []string{}, Errors: map[string]string{},
		}

		if links, err := safe.StateListAll[*network.LinkStatus](nodeCtx, st); err != nil {
			out.Errors["links"] = s.friendly(node, err)
		} else {
			out.Links = mapLinks(safe.ToSlice(links, identity))
		}

		virtual := virtualLinkNames(out.Links)

		if addrs, err := safe.StateListAll[*network.AddressStatus](nodeCtx, st); err != nil {
			out.Errors["addresses"] = s.friendly(node, err)
		} else {
			out.Addresses = mapAddresses(safe.ToSlice(addrs, identity), virtual)
		}

		if routes, err := safe.StateListAll[*network.RouteStatus](nodeCtx, st); err != nil {
			out.Errors["routes"] = s.friendly(node, err)
		} else {
			out.Routes = mapRoutes(safe.ToSlice(routes, identity), virtual)
		}

		if res, err := safe.StateListAll[*network.ResolverStatus](nodeCtx, st); err != nil {
			out.Errors["resolvers"] = s.friendly(node, err)
		} else {
			resolvers := safe.ToSlice(res, identity)
			out.Resolvers = mapResolvers(resolvers)
			learnSearchDomains(resolvers)
		}

		if ts, err := safe.StateListAll[*network.TimeServerStatus](nodeCtx, st); err != nil {
			out.Errors["timeServers"] = s.friendly(node, err)
		} else {
			out.TimeServers = mapTimeServers(safe.ToSlice(ts, identity))
		}

		if len(out.Errors) == 5 {
			return "", fmt.Errorf("node %s: %s", node, out.Errors["links"])
		}

		return toJSON(out)
	})
}

func identity[T any](v T) T { return v }

func mapLinks(in []*network.LinkStatus) []linkInfo {
	out := make([]linkInfo, 0, len(in))

	for _, l := range in {
		spec := l.TypedSpec()
		name := l.Metadata().ID()

		out = append(out, linkInfo{
			Name:         name,
			Type:         spec.Type.String(),
			Kind:         spec.Kind,
			State:        spec.OperationalState.String(),
			HardwareAddr: spec.HardwareAddr.String(),
			MTU:          spec.MTU,
			SpeedMbit:    linkSpeed(spec.SpeedMegabits),
			Virtual:      isVirtualLink(name, spec.Kind),
		})
	}

	// Physical/interesting links first, then the CNI clutter; by name within each group.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Virtual != out[j].Virtual {
			return !out[i].Virtual
		}

		return out[i].Name < out[j].Name
	})

	return out
}

// linkSpeed drops ethtool's "unknown" values (-1, or 0xFFFFFFFF read as unsigned).
func linkSpeed(mbit int) int {
	if mbit <= 0 || int64(mbit) >= math.MaxUint32 {
		return 0
	}

	return mbit
}

// virtualLinkNames returns the virtual links by name, to flag their addresses and routes.
func virtualLinkNames(links []linkInfo) map[string]bool {
	out := map[string]bool{}

	for _, l := range links {
		if l.Virtual {
			out[l.Name] = true
		}
	}

	return out
}

func isVirtualLink(name, kind string) bool {
	if kind == "veth" {
		return true
	}

	for _, p := range virtualLinkPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}

	return false
}

func mapAddresses(in []*network.AddressStatus, virtual map[string]bool) []addressInfo {
	out := make([]addressInfo, 0, len(in))

	for _, a := range in {
		spec := a.TypedSpec()

		out = append(out, addressInfo{
			Address: spec.Address.String(),
			Link:    spec.LinkName,
			Family:  spec.Family.String(),
			Scope:   spec.Scope.String(),
			Virtual: virtual[spec.LinkName] || isVirtualLink(spec.LinkName, ""),
		})
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Virtual != out[j].Virtual {
			return !out[i].Virtual
		}

		if out[i].Link != out[j].Link {
			return out[i].Link < out[j].Link
		}

		if out[i].Family != out[j].Family {
			return out[i].Family < out[j].Family
		}

		return out[i].Address < out[j].Address
	})

	return out
}

// mapRoutes keeps the main routing table (what `ip route` shows): default routes first,
// then routes over physical links, then the (often numerous) per-pod CNI routes.
func mapRoutes(in []*network.RouteStatus, virtual map[string]bool) []routeInfo {
	out := make([]routeInfo, 0, len(in))

	for _, r := range in {
		spec := r.TypedSpec()
		if spec.Table != nethelpers.TableMain {
			continue
		}

		info := routeInfo{
			Destination: "default",
			Link:        spec.OutLinkName,
			Metric:      spec.Priority,
			Table:       spec.Table.String(),
			Family:      spec.Family.String(),
		}

		if spec.Destination.IsValid() && spec.Destination.Bits() > 0 {
			info.Destination = spec.Destination.String()
		}

		if spec.Gateway.IsValid() {
			info.Gateway = spec.Gateway.String()
		}

		// Multipath (ECMP) routes carry their gateways in NextHops instead.
		if info.Gateway == "" && len(spec.NextHops) > 0 {
			hops := make([]string, 0, len(spec.NextHops))
			for _, h := range spec.NextHops {
				hops = append(hops, h.Gateway.String())
			}

			info.Gateway = strings.Join(hops, ", ")

			if info.Link == "" {
				info.Link = spec.NextHops[0].OutLinkName
			}
		}

		info.Virtual = virtual[info.Link] || isVirtualLink(info.Link, "")

		out = append(out, info)
	}

	sort.SliceStable(out, func(i, j int) bool {
		di, dj := out[i].Destination == "default", out[j].Destination == "default"
		if di != dj {
			return di
		}

		if out[i].Virtual != out[j].Virtual {
			return !out[i].Virtual
		}

		if out[i].Family != out[j].Family {
			return out[i].Family < out[j].Family
		}

		if out[i].Destination != out[j].Destination {
			return out[i].Destination < out[j].Destination
		}

		return out[i].Metric < out[j].Metric
	})

	return out
}

func mapResolvers(in []*network.ResolverStatus) []string {
	out := []string{}

	for _, r := range in {
		spec := r.TypedSpec()

		if len(spec.NameServers) > 0 {
			for _, ns := range spec.NameServers {
				out = appendUnique(out, ns.Addr.String())
			}

			continue
		}

		for _, a := range spec.DNSServers {
			out = appendUnique(out, a.String())
		}
	}

	return out
}

func mapTimeServers(in []*network.TimeServerStatus) []string {
	out := []string{}

	for _, ts := range in {
		for _, s := range ts.TypedSpec().NTPServers {
			out = appendUnique(out, s)
		}
	}

	return out
}

func appendUnique(list []string, v string) []string {
	if slices.Contains(list, v) {
		return list
	}

	return append(list, v)
}
