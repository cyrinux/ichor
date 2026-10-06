package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_argocd_network.go: how traffic reaches an Argo CD app,
// host → Gateway → Ingress/HTTPRoute → Service → Pod → node, left to right.

/** The network graph of one Argo CD app; empty for an app with no Service. */
@Serializable
data class ArgoNetwork(
    /** Sorted by layer, worst first within a layer. */
    val nodes: List<ArgoNetNode> = emptyList(),
    val edges: List<ArgoNetEdge> = emptyList(),
    /** The deepest broken box, the likely root cause; null when every box is fine. */
    val problem: ArgoNetProblem? = null,
) {
    /** No Service: nothing sends traffic to the app. */
    val takesNoTraffic: Boolean get() = nodes.none { it.kind == ArgoNetNode.SERVICE }

    /** The layers that have boxes, left to right: empty ones get no column. */
    val layers: List<Int> get() = nodes.map { it.layer }.filter { it in ArgoNetNode.LAYERS }.distinct().sorted()

    /** How many boxes each layer has. */
    val layerCounts: Map<Int, Int> get() = nodes.groupingBy { it.layer }.eachCount()

    fun nodesIn(layer: Int): List<ArgoNetNode> = nodes.filter { it.layer == layer }

    fun node(id: String): ArgoNetNode? = nodes.firstOrNull { it.id == id }

    /**
     * The boxes of [layer] a column shows. Only pods fold: past [limit] of them, unless
     * [expanded], the first [limit] (the worst, as the core sorts them) show and the rest hide
     * behind a "+N more" box ([MORE_PODS]).
     */
    fun column(layer: Int, expanded: Boolean, limit: Int = COLUMN_LIMIT): ArgoNetColumn {
        val all = nodesIn(layer)
        if (layer != ArgoNetNode.LAYER_POD || expanded || all.size <= limit) return ArgoNetColumn(layer, all)
        return ArgoNetColumn(layer, all.take(limit), hiddenNodes = all.drop(limit))
    }

    /** The ids of the pods folded behind "+N more", none when [expanded]. */
    fun hiddenIds(expanded: Boolean): Set<String> =
        column(ArgoNetNode.LAYER_POD, expanded).hiddenNodes.map { it.id }.toSet()

    /**
     * The hops to draw with the boxes of [hidden] folded: theirs go to [MORE_PODS] instead,
     * once per pair, in the worst health of those it stands for.
     */
    fun edgesHiding(hidden: Set<String>): List<ArgoNetEdge> {
        if (hidden.isEmpty()) return edges
        return edges
            .map { ArgoNetEdge(from = proxy(it.from, hidden), to = proxy(it.to, hidden), health = it.health) }
            .groupBy { it.key }
            .map { (_, same) -> same.minBy { it.healthState.ordinal } }
    }

    /**
     * Everything traffic through [id] crosses: what sends traffic to it (upstream, following
     * edges backwards) and where it goes (downstream), never sideways into sibling paths. With
     * boxes [hidden], the "+N more" box and its hops light up for them.
     */
    fun pathThrough(id: String, hidden: Set<String> = emptySet()): ArgoNetPath {
        if (node(id) == null) return ArgoNetPath()
        val up = walk(id) { at -> edges.filter { it.to == at }.map { it to it.from } }
        val down = walk(id) { at -> edges.filter { it.from == at }.map { it to it.to } }
        val nodes = up.first + down.first
        val lit = up.second + down.second
        val more = if (nodes.any { it in hidden }) setOf(MORE_PODS) else emptySet()
        val proxied = lit.map { proxy(it.from, hidden) to proxy(it.to, hidden) }
        return ArgoNetPath(nodes = nodes + more, edges = lit.map { it.key }.toSet() + proxied)
    }

    private fun proxy(id: String, hidden: Set<String>) = if (id in hidden) MORE_PODS else id

    private fun walk(start: String, next: (String) -> List<Pair<ArgoNetEdge, String>>): Pair<Set<String>, Set<ArgoNetEdge>> {
        val seen = mutableSetOf(start)
        val crossed = mutableSetOf<ArgoNetEdge>()
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
    fun nodeOf(pod: ArgoNetNode): ArgoNetNode? =
        edges.filter { it.from == pod.id }.firstNotNullOfOrNull { e -> node(e.to)?.takeIf { it.kind == ArgoNetNode.NODE } }

    /**
     * Joined with Talos: the nodes in [down] (hostnames Talos reports not ready) become
     * critical and NotReady, as do the hops into them, and such a node is the problem unless
     * the core already named a node that is not ready (a node down explains more than a pod).
     */
    fun withDownNodes(down: Set<String>): ArgoNetwork {
        val hit = nodes.filter { it.kind == ArgoNetNode.NODE && it.name in down && it.healthState != ServiceHealth.CRITICAL }
            .map { it.id }.toSet()
        if (hit.isEmpty()) return this
        val marked = nodes.map { if (it.id in hit) it.copy(health = ServiceHealth.CRITICAL.wire, detail = NOT_READY) else it }
        val first = marked.first { it.id in hit }
        val keep = problem?.let { it.kind == ArgoNetNode.NODE && it.detail != SCHEDULING_DISABLED } ?: false
        return copy(
            nodes = marked,
            edges = edges.map { if (it.to in hit) it.copy(health = ServiceHealth.CRITICAL.wire) else it },
            problem = if (keep) problem else ArgoNetProblem(kind = ArgoNetNode.NODE, name = first.name, detail = NOT_READY),
        )
    }

    companion object {
        /** Pods past this many fold behind a "+N more" chip. */
        const val COLUMN_LIMIT = 8

        /** The id of the "+N more" box the folded pods' hops are drawn to. */
        const val MORE_PODS = "more/pods"

        /** A node's detail when it is not ready, as the core words it. */
        const val NOT_READY = "NotReady"

        /** A cordoned node's detail, as the core words it. */
        const val SCHEDULING_DISABLED = "SchedulingDisabled"
    }
}

@Serializable
data class ArgoNetNode(
    /** "svc/ns/name", "pod/ns/name", "node/name", "host/https://..."... */
    val id: String,
    /** 0 host or load balancer, 1 Gateway, 2 Ingress or HTTPRoute, 3 Service, 4 Pod, 5 node. */
    val layer: Int = 0,
    /** Host, LoadBalancer, Gateway, Ingress, HTTPRoute, Service, Pod or Node. */
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    /** "ClusterIP 10.96.0.12 · 80→8080", a pod's status, an address. */
    val detail: String = "",
    /** critical, warning, ok or idle. */
    val health: String = "",
    /** What a host opens, "" otherwise. */
    val url: String = "",
    /** The app's own resource; false for a shared Gateway or route, pods and nodes. */
    val managed: Boolean = false,
) {
    val healthState: ServiceHealth get() = ServiceHealth.from(health)

    /** A Gateway or route the app does not own: drawn outlined, as someone else's. */
    val shared: Boolean get() = !managed && kind in SHARED_KINDS

    companion object {
        const val HOST = "Host"
        const val LOAD_BALANCER = "LoadBalancer"
        const val GATEWAY = "Gateway"
        const val INGRESS = "Ingress"
        const val HTTP_ROUTE = "HTTPRoute"
        const val SERVICE = "Service"
        const val POD = "Pod"
        const val NODE = "Node"

        const val LAYER_HOST = 0
        const val LAYER_GATEWAY = 1
        const val LAYER_ROUTE = 2
        const val LAYER_SERVICE = 3
        const val LAYER_POD = 4
        const val LAYER_NODE = 5
        val LAYERS = LAYER_HOST..LAYER_NODE

        private val SHARED_KINDS = setOf(GATEWAY, INGRESS, HTTP_ROUTE)
    }
}

/** One hop; [health] is its target's: traffic stops where it is red. */
@Serializable
data class ArgoNetEdge(val from: String, val to: String, val health: String = "") {
    val healthState: ServiceHealth get() = ServiceHealth.from(health)

    /** Which hop it is, whatever its health. */
    val key: Pair<String, String> get() = from to to
}

/** The likely root cause, for the app to word by [wording]. */
@Serializable
data class ArgoNetProblem(
    /** Node, Pod, Service, Ingress or Gateway (HTTPRoute possible). */
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val detail: String = "",
) {
    val wording: ArgoNetWording
        get() = when (kind) {
            // Go words a node that is only cordoned SchedulingDisabled.
            ArgoNetNode.NODE -> if (detail == ArgoNetwork.SCHEDULING_DISABLED) ArgoNetWording.NODE_CORDONED else ArgoNetWording.NODE_NOT_READY
            ArgoNetNode.POD -> ArgoNetWording.POD_STATUS
            ArgoNetNode.SERVICE -> ArgoNetWording.NO_READY_PODS
            else -> ArgoNetWording.NO_HEALTHY_BACKEND
        }
}

/** How the problem banner words a [ArgoNetProblem]. */
enum class ArgoNetWording { NODE_NOT_READY, NODE_CORDONED, POD_STATUS, NO_READY_PODS, NO_HEALTHY_BACKEND }

/** The boxes a column draws, and the pods it folds behind "+N more". */
data class ArgoNetColumn(val layer: Int, val nodes: List<ArgoNetNode>, val hiddenNodes: List<ArgoNetNode> = emptyList()) {
    val hidden: Int get() = hiddenNodes.size
}

/** The boxes and hops (by [ArgoNetEdge.key]) one box's traffic crosses, to highlight. */
data class ArgoNetPath(val nodes: Set<String> = emptySet(), val edges: Set<Pair<String, String>> = emptySet()) {
    fun contains(id: String) = id in nodes

    fun contains(edge: ArgoNetEdge) = edge.key in edges
}
