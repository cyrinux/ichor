package name.levis.ichor.model

/**
 * This map with what [previous] (fetched at [previousAt], epoch millis) knew of the nodes that
 * no longer answer: an unreachable target comes back from the core named by its address, with
 * no role nor zone, alone on its own network. Each one that answered before (or was itself
 * filled in) keeps its hostname, role and zone, gets [TopologyNode.lastSeen], and goes back to
 * the site it was on. Its error stays the new one; its id stays the address, as the links and
 * sites of this map refer to it. Nodes that answer are left as they are.
 */
fun ClusterTopology.withLastKnown(previous: ClusterTopology?, previousAt: Long): ClusterTopology {
    if (previous == null) return this
    return nodes.filter { it.error != null && it.node.isNotBlank() }.fold(this) { map, node ->
        val before = previous.nodes.firstOrNull { node.node == it.node || node.node in it.addresses }
            ?: return@fold map
        map.lastKnown(node.id, before, previous.sites.firstOrNull { before.id in it.nodes }, previousAt)
    }
}

private fun ClusterTopology.lastKnown(id: String, before: TopologyNode, beforeSite: TopologySite?, beforeAt: Long): ClusterTopology {
    val seen = before.lastSeen ?: beforeAt.takeIf { before.queried && before.error == null } ?: return this
    // Another node of this map goes by that name: two chips must not say the same.
    if (before.hostname.isBlank() || nodes.any { it.id != id && it.hostname.equals(before.hostname, ignoreCase = true) }) return this
    val filled = copy(
        nodes = nodes.map { n ->
            if (n.id != id) n
            else n.copy(
                hostname = before.hostname,
                role = n.role.ifBlank { before.role },
                zone = n.zone.ifBlank { before.zone },
                region = n.region.ifBlank { before.region },
                country = n.country.ifBlank { before.country },
                lastSeen = seen,
            )
        },
    )
    return filled.backTo(id, beforeSite)
}

/**
 * The node [id] back on the site it was on: the site of this map with the same zone or LAN, or,
 * when it is alone on its own network here, that site named as it was.
 */
private fun ClusterTopology.backTo(id: String, beforeSite: TopologySite?): ClusterTopology {
    if (beforeSite == null || beforeSite.kind == "node" || beforeSite.label.isBlank()) return this
    val current = sites.firstOrNull { id in it.nodes } ?: return this
    if (current.kind == beforeSite.kind && current.label == beforeSite.label) return this
    val same = sites.firstOrNull { it.kind == beforeSite.kind && it.label == beforeSite.label }
    if (same == null) {
        if (current.kind != "node") return this
        val named = current.copy(kind = beforeSite.kind, label = beforeSite.label, country = current.country.ifBlank { beforeSite.country })
        return copy(sites = sites.map { if (it.id == current.id) named else it })
    }
    val moved = sites.mapNotNull { s ->
        when (s.id) {
            same.id -> s.copy(nodes = s.nodes + id)
            current.id -> s.copy(nodes = s.nodes - id).takeIf { it.nodes.isNotEmpty() }
            else -> s
        }
    }
    return copy(sites = moved, nodes = nodes.map { if (it.id == id) it.copy(site = same.id) else it })
}
