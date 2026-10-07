import Foundation

// The cluster map's "last known state". Same rules as Android (TopologyLastKnown.kt).

/// `current` with what `previous` (the map shown before, fetched at `previousAt`) knew of the
/// nodes that no longer answer: an unreachable target comes back from the core named by its
/// address, with no role nor zone, alone on its own network. Each one that answered before (or
/// was itself filled in) keeps its hostname, role and zone, gets `lastSeen`, and goes back to
/// the site it was on. Its error stays the new one; its id stays the address, as the links and
/// sites refer to it. Nodes that answer are left as they are.
public func mergeLastKnown(current: ClusterTopology, previous: ClusterTopology?, previousAt: Date) -> ClusterTopology {
    guard let previous else { return current }
    let previousMillis = Int64((previousAt.timeIntervalSince1970 * 1000).rounded())
    return current.nodes.filter { $0.error != nil && !$0.node.isEmpty }.reduce(current) { map, node in
        guard let before = previous.nodes.first(where: { node.node == $0.node || $0.addresses.contains(node.node) }) else { return map }
        let beforeSite = previous.sites.first { $0.nodes.contains(before.id) }
        return map.lastKnown(node.id, before: before, beforeSite: beforeSite, beforeAt: previousMillis)
    }
}

private extension ClusterTopology {
    func lastKnown(_ id: String, before: TopologyNode, beforeSite: TopologySite?, beforeAt: Int64) -> ClusterTopology {
        guard let seen = before.lastSeen ?? (before.queried && before.error == nil ? beforeAt : nil) else { return self }
        // Another node of this map goes by that name: two chips must not say the same.
        guard !before.hostname.isEmpty,
              !nodes.contains(where: { $0.id != id && $0.hostname.caseInsensitiveCompare(before.hostname) == .orderedSame })
        else { return self }
        let filled = nodes.map { n -> TopologyNode in
            guard n.id == id else { return n }
            return TopologyNode(
                id: n.id, node: n.node, hostname: before.hostname, role: n.role.isEmpty ? before.role : n.role,
                addresses: n.addresses, zone: n.zone.isEmpty ? before.zone : n.zone,
                region: n.region.isEmpty ? before.region : n.region, country: n.country.isEmpty ? before.country : n.country,
                site: n.site, kubespan: n.kubespan, queried: n.queried, error: n.error, lastSeen: seen
            )
        }
        return ClusterTopology(nodes: filled, links: links, sites: sites).backTo(id, beforeSite: beforeSite)
    }

    /// The node `id` back on the site it was on: the site of this map with the same zone or
    /// LAN, or, when it is alone on its own network here, that site named as it was.
    func backTo(_ id: String, beforeSite: TopologySite?) -> ClusterTopology {
        guard let beforeSite, beforeSite.kind != "node", !beforeSite.label.isEmpty,
              let current = sites.first(where: { $0.nodes.contains(id) }),
              current.kind != beforeSite.kind || current.label != beforeSite.label
        else { return self }
        guard let same = sites.first(where: { $0.kind == beforeSite.kind && $0.label == beforeSite.label }) else {
            guard current.kind == "node" else { return self }
            let named = TopologySite(id: current.id, label: beforeSite.label, kind: beforeSite.kind,
                                     country: current.country.isEmpty ? beforeSite.country : current.country, nodes: current.nodes)
            return ClusterTopology(nodes: nodes, links: links, sites: sites.map { $0.id == current.id ? named : $0 })
        }
        let moved = sites.compactMap { s -> TopologySite? in
            switch s.id {
            case same.id:
                return TopologySite(id: s.id, label: s.label, kind: s.kind, country: s.country, nodes: s.nodes + [id])
            case current.id:
                let rest = s.nodes.filter { $0 != id }
                return rest.isEmpty ? nil : TopologySite(id: s.id, label: s.label, kind: s.kind, country: s.country, nodes: rest)
            default:
                return s
            }
        }
        let placed = nodes.map { n -> TopologyNode in
            guard n.id == id else { return n }
            return TopologyNode(id: n.id, node: n.node, hostname: n.hostname, role: n.role, addresses: n.addresses,
                                zone: n.zone, region: n.region, country: n.country, site: same.id,
                                kubespan: n.kubespan, queried: n.queried, error: n.error, lastSeen: n.lastSeen)
        }
        return ClusterTopology(nodes: placed, links: links, sites: moved)
    }
}

extension TopologyNode {
    /// When an unreachable node last answered, nil when unknown.
    public var lastSeenDate: Date? {
        lastSeen.map { Date(epochMillis: $0) }
    }
}
