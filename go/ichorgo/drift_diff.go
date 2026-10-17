package ichorgo

import (
	"sort"
	"strings"
)

// listDriftKeys are the settings stored by sortedSetting as a ", "-joined list.
var listDriftKeys = map[string]bool{"dns": true, "ntp": true, "mtu": true, "extensions": true}

func newDriftChange(n, ref driftNode, key, before, after string) driftChange {
	change := driftChange{
		Node: n.Node, Reference: ref.Node, NodeName: n.Hostname, ReferenceName: ref.Hostname,
		Key: key, Before: before, After: after,
		Added: []string{}, Removed: []string{}, Changed: []driftPair{},
	}
	if listDriftKeys[key] {
		change.Added, change.Removed, change.Changed = diffSetting(before, after)
	}
	return change
}

// diffSetting itemises two sortedSetting values: name=value items are paired by name
// (a different value is a change), other items (DNS, NTP servers) are added or removed.
func diffSetting(before, after string) (added, removed []string, changed []driftPair) {
	added, removed, changed = []string{}, []string{}, []driftPair{}
	old, oldNamed := settingItems(before)
	cur, curNamed := settingItems(after)
	for item := range cur {
		if old[item] {
			continue
		}
		if name, value, ok := strings.Cut(item, "="); ok {
			if previous, paired := oldNamed[name]; paired {
				changed = append(changed, driftPair{Name: name, Before: previous, After: value})
				continue
			}
		}
		added = append(added, item)
	}
	for item := range old {
		if cur[item] {
			continue
		}
		if name, _, ok := strings.Cut(item, "="); ok {
			if _, paired := curNamed[name]; paired {
				continue
			}
		}
		removed = append(removed, item)
	}
	sort.Strings(added)
	sort.Strings(removed)
	sort.Slice(changed, func(i, j int) bool { return changed[i].Name < changed[j].Name })
	return added, removed, changed
}

// settingItems splits a joined setting into its items and the value of each named item.
func settingItems(value string) (map[string]bool, map[string]string) {
	items, named := map[string]bool{}, map[string]string{}
	if value == "" {
		return items, named
	}
	for _, item := range strings.Split(value, ", ") {
		items[item] = true
		if name, v, ok := strings.Cut(item, "="); ok {
			named[name] = v
		}
	}
	return items, named
}

// Demo drift: every node carries the reference extensions and interfaces, and the
// second worker differs from the first (an extension bump, one added, one removed,
// a new schematic, a renamed interface and Secure Boot off). Hashes are fake.
const (
	demoSchematic        = "0123abcd0123abcd0123abcd0123abcd0123abcd0123abcd0123abcd0123abcd"
	demoDriftedSchematic = "4567cdef4567cdef4567cdef4567cdef4567cdef4567cdef4567cdef4567cdef"
)

func demoDriftNodes(nodes []driftNode) []driftNode {
	out := make([]driftNode, len(nodes))
	for i, n := range nodes {
		values := make(map[string]string, len(n.Values)+3)
		for k, v := range n.Values {
			values[k] = v
		}
		values["extensions"] = sortedSetting([]string{"iscsi-tools=v0.2.0", "qemu-guest-agent=10.0.2", "schematic=" + demoSchematic, "util-linux-tools=2.41.1"})
		values["mtu"] = sortedSetting([]string{"ens3=1500", "eth1=9000"})
		if n.Hostname == "demo-worker-2" {
			values["extensions"] = sortedSetting([]string{"btrfs=v1.14.1", "iscsi-tools=v0.2.0", "schematic=" + demoDriftedSchematic, "util-linux-tools=2.41.2"})
			values["mtu"] = sortedSetting([]string{"enp1s0=1500", "eth1=9000"})
			values["secureBoot"] = "false"
		}
		n.Values = values
		out[i] = n
	}
	return out
}
