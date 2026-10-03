package ichorgo

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Only these selected, non-secret effective settings are retained. This deliberately
// does not persist machine configs, credentials, hardware serials or packet data.
type driftNode struct {
	Node     string            `json:"node"`
	Hostname string            `json:"hostname"`
	Role     string            `json:"role"`
	Values   map[string]string `json:"values"`
	Errors   map[string]string `json:"errors"`
}
type driftSnapshot struct {
	Scope string      `json:"scope"`
	At    int64       `json:"at"`
	Nodes []driftNode `json:"nodes"`
}
type driftChange struct {
	Node      string `json:"node"`
	Reference string `json:"reference"`
	Key       string `json:"key"`
	Before    string `json:"before"`
	After     string `json:"after"`
}

// ClusterDriftSnapshot captures effective DNS, NTP, MTU, extensions, security and
// Talos version. Missing sections stay unknown instead of becoming empty settings.
func ClusterDriftSnapshot(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)
	contextName = unmaskContext(configYAML, contextName)
	name, cfg, err := resolveContext(configYAML, contextName)
	if err != nil {
		return "", err
	}
	var overview clusterOverview
	raw, e := ClusterOverview(configYAML, contextName)
	if err = decodeObservation(raw, e, &overview); err != nil {
		return "", err
	}
	result := driftSnapshot{Scope: contextFingerprint(name, cfg), At: time.Now().UnixMilli(), Nodes: make([]driftNode, len(overview.Nodes))}
	forEachNode(overview.Nodes, func(i int, n nodeOverview) {
		d := driftNode{Node: n.Node, Hostname: n.Hostname, Role: n.Role, Values: map[string]string{}, Errors: map[string]string{}}
		if !n.Reachable {
			d.Errors["node"] = n.Error
			result.Nodes[i] = d
			return
		}
		if n.Version != "" {
			d.Values["version"] = n.Version
		}
		var network nodeNetwork
		raw, e := NodeNetwork(configYAML, contextName, n.Node)
		if e := decodeObservation(raw, e, &network); e != nil {
			d.Errors["network"] = e.Error()
		} else {
			for k, v := range network.Errors {
				d.Errors[k] = v
			}
			if network.Errors["resolvers"] == "" {
				d.Values["dns"] = sortedSetting(network.Resolvers)
			}
			if network.Errors["timeServers"] == "" {
				d.Values["ntp"] = sortedSetting(network.TimeServers)
			}
			if network.Errors["links"] == "" {
				mtus := []string{}
				for _, l := range network.Links {
					if !l.Virtual && l.Type != "loopback" {
						mtus = append(mtus, fmt.Sprintf("%s=%d", l.Name, l.MTU))
					}
				}
				d.Values["mtu"] = sortedSetting(mtus)
			}
		}
		var hw nodeHardware
		raw, e = NodeHardware(configYAML, contextName, n.Node)
		if e := decodeObservation(raw, e, &hw); e != nil {
			d.Errors["hardware"] = e.Error()
		} else {
			for _, k := range []string{"extensions", "security"} {
				if hw.Errors[k] != "" {
					d.Errors[k] = hw.Errors[k]
				}
			}
			if hw.Errors["extensions"] == "" {
				extensions := []string{}
				for _, e := range hw.Extensions {
					extensions = append(extensions, e.Name+"="+e.Version)
				}
				d.Values["extensions"] = sortedSetting(extensions)
			}
			if hw.Security != nil && hw.Errors["security"] == "" {
				d.Values["secureBoot"] = fmt.Sprint(hw.Security.SecureBoot)
				d.Values["uki"] = fmt.Sprint(hw.Security.BootedWithUKI)
			}
		}
		result.Nodes[i] = d
	})
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].Node < result.Nodes[j].Node })
	return toJSON(result)
}

// decodeObservation reads a JSON bridge result without losing its error.
func decodeObservation(raw string, err error, target any) error {
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), target)
}
func sortedSetting(values []string) string {
	copy := append([]string{}, values...)
	sort.Strings(copy)
	return strings.Join(copy, ", ")
}

// CompareDrift compares a saved baseline to the same nodes; an empty baseline compares
// to the first node of each role. Differences are observations, not misconfiguration.
func CompareDrift(baselineJSON, currentJSON string) (out string, err error) {
	defer maskResult(&out, &err)
	var current, baseline driftSnapshot
	if err := json.Unmarshal([]byte(currentJSON), &current); err != nil {
		return "", err
	}
	if baselineJSON != "" {
		if err := json.Unmarshal([]byte(baselineJSON), &baseline); err != nil {
			return "", err
		}
		if baseline.Scope != current.Scope {
			return "", fmt.Errorf("baseline belongs to another cluster")
		}
	}
	return toJSON(compareDrift(baseline, current, baselineJSON != ""))
}
func compareDrift(baseline, current driftSnapshot, saved bool) []driftChange {
	out := []driftChange{}
	refs := map[string]driftNode{}
	source := baseline.Nodes
	if !saved {
		source = append([]driftNode{}, current.Nodes...)
		sort.Slice(source, func(i, j int) bool { return source[i].Node < source[j].Node })
	}
	for _, n := range source {
		key := n.Node
		if !saved {
			key = n.Role
		}
		if len(n.Values) == 0 || key == "" {
			continue
		}
		if _, exists := refs[key]; !exists {
			refs[key] = n
		}
	}
	for _, n := range current.Nodes {
		key := n.Node
		if !saved {
			key = n.Role
		}
		ref, ok := refs[key]
		if !ok {
			continue
		}
		for k, value := range n.Values {
			// Unknown in either sample is not a difference (no false removal alerts).
			old, known := ref.Values[k]
			if !known || old == value {
				continue
			}
			out = append(out, driftChange{Node: n.Node, Reference: ref.Node, Key: k, Before: old, After: value})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Node == out[j].Node {
			return out[i].Key < out[j].Key
		}
		return out[i].Node < out[j].Node
	})
	return out
}

type observedNode struct {
	Status   nodeOverview      `json:"status"`
	Stats    *nodeStats        `json:"stats"`
	Services []serviceInfo     `json:"services"`
	Links    map[string]string `json:"links"`
	Errors   map[string]string `json:"errors"`
}
type clusterObservation struct {
	Scope string         `json:"scope"`
	At    int64          `json:"at"`
	Nodes []observedNode `json:"nodes"`
}

// ClusterObservation is a read-only sample for an opt-in incident recording. No raw
// logs/configs are collected. Network/service changes between polls can be missed.
func ClusterObservation(configYAML, contextName string) (out string, err error) {
	defer maskResult(&out, &err)
	contextName = unmaskContext(configYAML, contextName)
	name, cfg, err := resolveContext(configYAML, contextName)
	if err != nil {
		return "", err
	}
	var overview clusterOverview
	raw, e := ClusterOverview(configYAML, contextName)
	if err = decodeObservation(raw, e, &overview); err != nil {
		return "", err
	}
	result := clusterObservation{Scope: contextFingerprint(name, cfg), At: time.Now().UnixMilli(), Nodes: make([]observedNode, len(overview.Nodes))}
	forEachNode(overview.Nodes, func(i int, n nodeOverview) {
		o := observedNode{Status: n, Services: []serviceInfo{}, Links: map[string]string{}, Errors: map[string]string{}}
		if n.Reachable {
			var stats nodeStats
			raw, e := NodeStats(configYAML, contextName, n.Node)
			if e = decodeObservation(raw, e, &stats); e != nil {
				o.Errors["stats"] = e.Error()
			} else {
				o.Stats = &stats
			}
			raw, e = NodeServices(configYAML, contextName, n.Node)
			if e = decodeObservation(raw, e, &o.Services); e != nil {
				o.Errors["services"] = e.Error()
			}
			var net nodeNetwork
			raw, e = NodeNetwork(configYAML, contextName, n.Node)
			if e = decodeObservation(raw, e, &net); e != nil {
				o.Errors["links"] = e.Error()
			} else if net.Errors["links"] != "" {
				o.Errors["links"] = net.Errors["links"]
			} else {
				for _, l := range net.Links {
					if !l.Virtual && l.Type != "loopback" {
						o.Links[l.Name] = l.State
					}
				}
			}
		}
		result.Nodes[i] = o
	})
	return toJSON(result)
}
