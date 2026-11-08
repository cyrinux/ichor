package ichorgo

import (
	"encoding/json"
	"net"
	"slices"
	"strings"

	"github.com/cyrinux/ichor/go/kube/netmap"
)

// How a Service or an Ingress is reached from outside the cluster through a provider's own
// network (a tailnet name), or reaches out through it (egress): the network maps draw it as one
// ExternalName box, the provider in its detail, so a new provider adds a matcher here and no
// app string.

// netKindExternalName is the box kind of an external entry, whatever the provider.
const netKindExternalName = "ExternalName"

// Which way an entry carries traffic: from the provider's network in (ingress), or out to it
// (egress).
const (
	externalIngress = "ingress"
	externalEgress  = "egress"
)

// externalEntry is one name a provider serves a Service or an Ingress under (ingress), or the
// target it reaches through it (egress).
type externalEntry struct {
	Provider string // "Tailscale"
	Kind     string // ingress or egress
	// Name is the name on the provider's network, else the object's namespace/name while it
	// has none yet.
	Name   string
	URL    string // what Name opens, "" when it is not a web address
	Health string // the provider's own condition: ok, warning or critical
	Detail string // "Tailscale · 100.64.0.51": the provider first, then the other addresses or the problem
	// Addresses are every name and IP of the entry, Name first: the load-balancer addresses it
	// stands for.
	Addresses []string
}

// externalMatcher reads one provider's entries off a Service or an Ingress (kind, the object's
// JSON); it runs only where the cluster serves group.
type externalMatcher struct {
	provider string
	group    string
	lists    []externalList // the provider's own kinds the matcher reads (readExternalObjects)
	match    func(kind string, raw json.RawMessage, objs externalObjects) []externalEntry
}

// externalMatchers is every provider the maps know.
var externalMatchers = []externalMatcher{
	{provider: tsProvider, group: groupTailscale, match: func(kind string, raw json.RawMessage, _ externalObjects) []externalEntry {
		return tsExternalEntries(kind, raw)
	}},
	{provider: nbProvider, group: groupNetBird, match: nbExternalEntries, lists: []externalList{
		{plural: "networkresources"}, {plural: "networkrouters"}, {plural: "nbresources", version: "v1"},
	}},
	{provider: tgProvider, group: groupTwingate, match: tgExternalEntries, lists: []externalList{{plural: "twingateresources"}}},
	{provider: ngProvider, group: groupNgrok, match: ngExternalEntries, lists: []externalList{{plural: "agentendpoints"}}},
	{provider: cfProvider, group: groupCloudflare, match: cfExternalEntries, lists: []externalList{{plural: "tunnelbindings"}}},
}

// externalEntries are the external entries of the Service or Ingress raw (kind), from the
// matchers whose API group groups (readAPIGroups) serves, from the provider objects objs
// (readExternalObjects).
func externalEntries(kind string, raw json.RawMessage, groups map[string]string, objs externalObjects) []externalEntry {
	var out []externalEntry

	for _, m := range externalMatchers {
		if _, served := groups[m.group]; served {
			out = append(out, m.match(kind, raw, objs)...)
		}
	}

	return out
}

// externalKey indexes entries by object: "Service/ns/name".
func externalKey(kind, namespace, name string) string { return kind + "/" + namespace + "/" + name }

// externalNode is the box of e in layer. A box in front of the Services keeps its health empty
// while the provider says ok, so the map colours it from what it sends traffic to.
func externalNode(e externalEntry, layer int) netmap.Node {
	n := netmap.Node{
		ID: "ext/" + e.Provider + "/" + e.Name, Layer: layer, Kind: netKindExternalName, Name: e.Name,
		Detail: e.Detail, URL: e.URL,
	}
	if e.Health != healthOK {
		n.Health = e.Health
	}

	return n
}

// The Tailscale matcher.

const tsProvider = "Tailscale"

// tsExternalEntries is the Service or Ingress raw's tailnet entry, when the operator serves it.
func tsExternalEntries(kind string, raw json.RawMessage) []externalEntry {
	row, ok := tsExposedOf(kind, raw)
	if !ok {
		return nil
	}

	return []externalEntry{tsEntryOf(row)}
}

// tsExposedOf is the exposed row of the Service or Ingress raw, false when the operator does
// not serve it.
func tsExposedOf(kind string, raw json.RawMessage) (tsExposed, bool) {
	switch kind {
	case tsKindService:
		var s tsServiceObject
		if json.Unmarshal(raw, &s) != nil {
			return tsExposed{}, false
		}

		return tsServiceRow(s)
	case tsKindIngress:
		var ing ingressObject
		if json.Unmarshal(raw, &ing) != nil || ing.Spec.IngressClassName != tsClassTailscale {
			return tsExposed{}, false
		}

		return tsIngressRow(ing), true
	}

	return tsExposed{}, false
}

// tsEntryOf is a row as an entry: its first tailnet name (or target), opened over HTTPS for an
// Ingress or a Service on 443 (HTTP on 80), the other names, or the problem, in the detail.
func tsEntryOf(row tsExposed) externalEntry {
	e := externalEntry{
		Provider: tsProvider, Kind: row.Direction, Name: row.Namespace + "/" + row.Name, Health: row.Health,
		Addresses: slices.Clone(row.TailnetNames),
	}

	if len(row.TailnetNames) > 0 {
		e.Name = row.TailnetNames[0]
	}

	if row.Direction == tsDirectionIngress && len(row.TailnetNames) > 0 && net.ParseIP(e.Name) == nil {
		switch {
		case row.Kind == tsKindIngress || slices.Contains(row.Ports, "443"):
			e.URL = "https://" + e.Name
		case slices.Contains(row.Ports, "80"):
			e.URL = "http://" + e.Name
		}
	}

	parts := []string{tsProvider}
	if row.Health == healthOK {
		parts = append(parts, row.TailnetNames[min(1, len(row.TailnetNames)):]...)
	} else if problem := strings.Trim(row.Reason+": "+row.Message, ": "); problem != "" {
		parts = append(parts, problem)
	}

	e.Detail = strings.Join(parts, " · ")

	return e
}
