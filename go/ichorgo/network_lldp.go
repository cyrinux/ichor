package ichorgo

import (
	"context"
	"sort"
	"strings"
	"unicode"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/siderolabs/talos/pkg/machinery/resources/network"
)

// lldpNeighborStatusType is Talos 1.15's LLDPNeighborStatus resource (pkg/machinery/resources/
// network/lldp_neighbor_status.go, tag v1.15.0-beta.1): one per link that heard LLDP, keyed by
// the link name, in the network namespace. This client's machinery (1.14) has no Go type for
// it, so it is read untyped and its YAML spec decoded by key:
//
//	neighbors[{chassisID, systemName, systemDescription, portID, portDescription,
//	           managementAddresses[], vlans[{id, name}]}]
const lldpNeighborStatusType = "LLDPNeighborStatuses.net.talos.dev"

// What a switch announces is untrusted text from the wire: each value is clipped and the
// lists are capped so a chatty or hostile neighbour cannot flood the screen.
const (
	lldpTextMax      = 128
	lldpNeighborsMax = 8 // per link
	lldpAddressesMax = 8
	lldpVLANsMax     = 32
)

// lldpRole names announced switches switch-N in screenshot mode.
const lldpRole = "switch"

type lldpNeighbor struct {
	Link                string     `json:"link"`
	ChassisID           string     `json:"chassisId"`
	SystemName          string     `json:"systemName"`
	SystemDescription   string     `json:"systemDescription"`
	PortID              string     `json:"portId"`
	PortDescription     string     `json:"portDescription"`
	ManagementAddresses []string   `json:"managementAddresses"`
	VLANs               []lldpVLAN `json:"vlans"`
}

type lldpVLAN struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// readNeighbors fills out's LLDP section, like `talosctl get lldpneighborstatus`: only when
// the node knows the resource type (Talos 1.15+); a failure goes to errors["neighbors"].
func readNeighbors(ctx context.Context, s *session, node string, out *nodeNetwork) {
	known, err := s.hasResourceType(ctx, node, lldpNeighborStatusType)
	if err != nil {
		out.Errors["neighbors"] = s.friendly(node, err)

		return
	}

	if !known {
		return
	}

	out.NeighborsSupported = true

	list, err := s.client.COSI.List(ctx, resource.NewMetadata(network.NamespaceName, lldpNeighborStatusType, "", resource.VersionUndefined))
	if err != nil {
		out.Errors["neighbors"] = s.friendly(node, err)

		return
	}

	specs := make(map[string]map[string]any, len(list.Items))

	for _, r := range list.Items {
		spec, err := resourceSpecMap(r)
		if err != nil {
			out.Errors["neighbors"] = "LLDP neighbours of " + r.Metadata().ID() + ": " + err.Error()

			return
		}

		specs[r.Metadata().ID()] = spec
	}

	out.Neighbors = mapLLDPNeighbors(specs)
	learnSwitchNames(out.Neighbors)
}

// mapLLDPNeighbors maps the LLDPNeighborStatus specs (link name -> spec), by link then switch.
func mapLLDPNeighbors(specs map[string]map[string]any) []lldpNeighbor {
	out := []lldpNeighbor{}

	for link, spec := range specs {
		raw, _ := spec["neighbors"].([]any) //nolint:errcheck

		for i, item := range raw {
			if i == lldpNeighborsMax {
				break
			}

			if n, ok := item.(map[string]any); ok {
				out = append(out, mapLLDPNeighbor(link, n))
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Link != out[j].Link {
			return out[i].Link < out[j].Link
		}

		return out[i].SystemName < out[j].SystemName
	})

	return out
}

func mapLLDPNeighbor(link string, spec map[string]any) lldpNeighbor {
	out := lldpNeighbor{
		Link:                link,
		ChassisID:           lldpText(spec, "chassisID"),
		SystemName:          lldpText(spec, "systemName"),
		SystemDescription:   lldpText(spec, "systemDescription"),
		PortID:              lldpText(spec, "portID"),
		PortDescription:     lldpText(spec, "portDescription"),
		ManagementAddresses: []string{},
		VLANs:               []lldpVLAN{},
	}

	addrs, _ := spec["managementAddresses"].([]any) //nolint:errcheck

	for _, a := range addrs {
		if s, ok := a.(string); ok && len(out.ManagementAddresses) < lldpAddressesMax {
			if s = cleanLLDPText(s); s != "" {
				out.ManagementAddresses = append(out.ManagementAddresses, s)
			}
		}
	}

	vlans, _ := spec["vlans"].([]any) //nolint:errcheck

	for _, v := range vlans {
		m, ok := v.(map[string]any)
		if !ok || len(out.VLANs) == lldpVLANsMax {
			continue
		}

		if id := specInt(m, "id"); id != nil {
			out.VLANs = append(out.VLANs, lldpVLAN{ID: *id, Name: lldpText(m, "name")})
		}
	}

	sort.Slice(out.VLANs, func(i, j int) bool { return out.VLANs[i].ID < out.VLANs[j].ID })

	return out
}

func lldpText(spec map[string]any, key string) string {
	return cleanLLDPText(specString(spec, key))
}

// cleanLLDPText drops control characters, collapses whitespace and clips to lldpTextMax runes.
func cleanLLDPText(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")

	if r := []rune(s); len(r) > lldpTextMax {
		s = string(r[:lldpTextMax-1]) + "…"
	}

	return s
}

// learnSwitchNames teaches the screenshot mask the announced switch names (switch-N).
func learnSwitchNames(neighbors []lldpNeighbor) {
	for _, n := range neighbors {
		privacy.learnHost(n.SystemName, lldpRole)
	}
}

// demoNeighbors are the demo node's two uplinks (documentation addresses, invented names).
func demoNeighbors() []lldpNeighbor {
	return []lldpNeighbor{
		{
			Link: "eth0", ChassisID: "02:00:5e:00:53:01", SystemName: "sw-rack-a",
			SystemDescription: "Demo switch OS 4.2", PortID: "Ethernet12", PortDescription: "node uplink",
			ManagementAddresses: []string{"192.0.2.250"}, VLANs: []lldpVLAN{{ID: 30, Name: "servers"}},
		},
		{
			Link: "eth1", ChassisID: "02:00:5e:00:53:02", SystemName: "sw-rack-b",
			SystemDescription: "Demo switch OS 4.2", PortID: "Ethernet12", PortDescription: "storage uplink",
			ManagementAddresses: []string{"192.0.2.251"}, VLANs: []lldpVLAN{{ID: 40, Name: "storage"}, {ID: 41}},
		},
	}
}
