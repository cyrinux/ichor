package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/kube/netmap: how traffic reaches pods, left to right, the caller's entry boxes
// (an Argo CD app's hosts, Gateways and routes) in front of Service → Pod → node.

/** A network map; empty when nothing sends traffic anywhere. */
@Serializable
data class NetMap(
    /** Sorted by layer, worst first within a layer. */
    val nodes: List<NetMapNode> = emptyList(),
    val edges: List<NetMapEdge> = emptyList(),
    /** The deepest broken box, the likely root cause; null when every box is fine. */
    val problem: NetMapProblem? = null,
) {
    /** No Service: nothing sends traffic to the pods. */
    val takesNoTraffic: Boolean get() = nodes.none { it.kind == NetMapNode.SERVICE }

    /** The layers that have boxes, left to right: empty ones get no column. */
    val layers: List<Int> get() = nodes.map { it.layer }.distinct().sorted()

    /** How many boxes each layer has. */
    val layerCounts: Map<Int, Int> get() = nodes.groupingBy { it.layer }.eachCount()

    fun nodesIn(layer: Int): List<NetMapNode> = nodes.filter { it.layer == layer }

    fun node(id: String): NetMapNode? = nodes.firstOrNull { it.id == id }

    /** Whether [layer] has more boxes than a column shows folded: it gets a "+N more" chip. */
    fun folds(layer: Int, limit: Int = COLUMN_LIMIT): Boolean = nodesIn(layer).size > limit

    /**
     * The boxes of [layer] a column shows: past [limit] of them, unless [expanded], the first
     * [limit] (the worst, as the core sorts them) show and the rest hide behind a "+N more" box
     * ([moreId]).
     */
    fun column(layer: Int, expanded: Boolean, limit: Int = COLUMN_LIMIT): NetMapColumn {
        val all = nodesIn(layer)
        if (expanded || all.size <= limit) return NetMapColumn(layer, all)
        return NetMapColumn(layer, all.take(limit), hiddenNodes = all.drop(limit))
    }

    /** The ids of the boxes folded behind a "+N more", none when [expanded]. */
    fun hiddenIds(expanded: Boolean): Set<String> =
        layers.flatMap { column(it, expanded).hiddenNodes }.map { it.id }.toSet()

    /**
     * The hops to draw with the boxes of [hidden] folded: theirs go to their column's "+N more"
     * box instead, once per pair, in the worst health of those it stands for.
     */
    fun edgesHiding(hidden: Set<String>): List<NetMapEdge> {
        if (hidden.isEmpty()) return edges
        return edges
            .map { NetMapEdge(from = proxy(it.from, hidden), to = proxy(it.to, hidden), health = it.health) }
            .groupBy { it.key }
            .map { (_, same) -> same.minBy { it.healthState.ordinal } }
    }

    /**
     * Everything traffic through [id] crosses: what sends traffic to it (upstream, following
     * edges backwards) and where it goes (downstream), never sideways into sibling paths. With
     * boxes [hidden], their "+N more" box and its hops light up for them.
     */
    fun pathThrough(id: String, hidden: Set<String> = emptySet()): NetMapPath {
        if (node(id) == null) return NetMapPath()
        val up = walk(id) { at -> edges.filter { it.to == at }.map { it to it.from } }
        val down = walk(id) { at -> edges.filter { it.from == at }.map { it to it.to } }
        val nodes = up.first + down.first
        val lit = up.second + down.second
        val more = nodes.filter { it in hidden }.map { proxy(it, hidden) }.toSet()
        val proxied = lit.map { proxy(it.from, hidden) to proxy(it.to, hidden) }
        return NetMapPath(nodes = nodes + more, edges = lit.map { it.key }.toSet() + proxied)
    }

    private fun proxy(id: String, hidden: Set<String>) =
        if (id in hidden) node(id)?.let { moreId(it.layer) } ?: id else id

    private fun walk(start: String, next: (String) -> List<Pair<NetMapEdge, String>>): Pair<Set<String>, Set<NetMapEdge>> {
        val seen = mutableSetOf(start)
        val crossed = mutableSetOf<NetMapEdge>()
        val queue = ArrayDeque(listOf(start))
        while (queue.isNotEmpty()) {
            next(queue.removeFirst()).forEach { (edge, node) ->
                crossed += edge
                if (seen.add(node)) queue += node
            }
        }
        return seen to crossed
    }

    /** The node a pod runs on, from its pod → node edge; null for an unscheduled pod. */
    fun nodeOf(pod: NetMapNode): NetMapNode? =
        edges.filter { it.from == pod.id }.firstNotNullOfOrNull { e -> node(e.to)?.takeIf { it.kind == NetMapNode.NODE } }

    /**
     * Joined with Talos: the nodes in [down] (hostnames Talos reports not ready) become
     * critical and NotReady, as do the hops into them, and such a node is the problem unless
     * the core already named a node that is not ready (a node down explains more than a pod).
     */
    fun withDownNodes(down: Set<String>): NetMap {
        val hit = nodes.filter { it.kind == NetMapNode.NODE && it.name in down && it.healthState != ServiceHealth.CRITICAL }
            .map { it.id }.toSet()
        if (hit.isEmpty()) return this
        val marked = nodes.map { if (it.id in hit) it.copy(health = ServiceHealth.CRITICAL.wire, detail = NOT_READY) else it }
        val first = marked.first { it.id in hit }
        val keep = problem?.let { it.kind == NetMapNode.NODE && it.detail != SCHEDULING_DISABLED } ?: false
        return copy(
            nodes = marked,
            edges = edges.map { if (it.to in hit) it.copy(health = ServiceHealth.CRITICAL.wire) else it },
            problem = if (keep) problem else NetMapProblem(kind = NetMapNode.NODE, name = first.name, detail = NOT_READY),
        )
    }

    companion object {
        /** Boxes past this many in a column fold behind a "+N more" chip. */
        const val COLUMN_LIMIT = 8

        /** The id of the "+N more" box the folded boxes of [layer] have their hops drawn to. */
        fun moreId(layer: Int): String = "more/$layer"

        /** A node's detail when it is not ready, as the core words it. */
        const val NOT_READY = "NotReady"

        /** A cordoned node's detail, as the core words it. */
        const val SCHEDULING_DISABLED = "SchedulingDisabled"
    }
}

@Serializable
data class NetMapNode(
    /** "svc/ns/name", "pod/ns/name", "node/name", "host/https://..."... */
    val id: String,
    /** The column, left to right: the caller's own layers first, then Service, Pod and node. */
    val layer: Int = 0,
    /** Service, Pod or Node, or the caller's: Host, LoadBalancer, Gateway, Ingress, HTTPRoute… */
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    /** "ClusterIP 10.96.0.12 · 80→8080", a pod's status, an address. */
    val detail: String = "",
    /** critical, warning, ok or idle. */
    val health: String = "",
    /** What a host opens, "" otherwise. */
    val url: String = "",
    /** The caller's own resource (an Argo CD app's); false for pods and nodes. */
    val managed: Boolean = false,
) {
    val healthState: ServiceHealth get() = ServiceHealth.from(health)

    /** A Gateway or route the caller does not own: drawn outlined, as someone else's. */
    val shared: Boolean get() = !managed && kind in SHARED_KINDS

    companion object {
        const val HOST = "Host"
        const val LOAD_BALANCER = "LoadBalancer"
        const val GATEWAY = "Gateway"
        const val INGRESS = "Ingress"
        const val HTTP_ROUTE = "HTTPRoute"
        const val GRPC_ROUTE = "GRPCRoute"
        const val TLS_ROUTE = "TLSRoute"
        const val INGRESS_ROUTE = "IngressRoute"
        const val INGRESS_ROUTE_TCP = "IngressRouteTCP"
        const val SERVICE = "Service"
        const val POD = "Pod"
        const val NODE = "Node"
        const val EXTERNAL_NAME = "ExternalName"
        const val PROXY_GROUP = "ProxyGroup"
        const val PROXY = "Proxy"

        /** The kinds that route traffic to Services: drawn with the route icon. */
        val ROUTE_KINDS = setOf(INGRESS, HTTP_ROUTE, GRPC_ROUTE, TLS_ROUTE, INGRESS_ROUTE, INGRESS_ROUTE_TCP)
        private val SHARED_KINDS = ROUTE_KINDS + GATEWAY
    }
}

/** One hop; [health] is its target's: traffic stops where it is red. */
@Serializable
data class NetMapEdge(val from: String, val to: String, val health: String = "") {
    val healthState: ServiceHealth get() = ServiceHealth.from(health)

    /** Which hop it is, whatever its health. */
    val key: Pair<String, String> get() = from to to
}

/** The likely root cause, for the caller to word. */
@Serializable
data class NetMapProblem(
    /** Node, Pod or Service, or one of the caller's kinds (Ingress, Gateway, a route). */
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val detail: String = "",
)

/** The boxes a column draws, and those it folds behind "+N more". */
data class NetMapColumn(val layer: Int, val nodes: List<NetMapNode>, val hiddenNodes: List<NetMapNode> = emptyList()) {
    val hidden: Int get() = hiddenNodes.size
}

/** The boxes and hops (by [NetMapEdge.key]) one box's traffic crosses, to highlight. */
data class NetMapPath(val nodes: Set<String> = emptySet(), val edges: Set<Pair<String, String>> = emptySet()) {
    fun contains(id: String) = id in nodes

    fun contains(edge: NetMapEdge) = edge.key in edges
}
