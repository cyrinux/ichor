package name.levis.ichor.data

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.updateAndGet

/**
 * The nodes the live cluster summary samples on every tick of a large cluster: each screen
 * publishes its own set under a source name (the overview its problem nodes, the dense card
 * the problems it lists, the Nodes screen its visible rows), and [nodes] is their union.
 * Names from another cluster are harmless: the core leaves unknown nodes out.
 */
class LiveFocus {
    private val sources = MutableStateFlow<Map<String, Set<String>>>(emptyMap())
    private val _nodes = MutableStateFlow<Set<String>>(emptySet())
    val nodes: StateFlow<Set<String>> = _nodes.asStateFlow()

    /** Replaces [source]'s set with [nodes]. */
    fun publish(source: String, nodes: Set<String>) {
        val next = sources.updateAndGet { it + (source to nodes) }
        _nodes.value = next.values.flatten().toSet()
    }

    private var key: Any? = null

    /**
     * Forgets every source when [key] (the cluster and its cached data) differs from the last
     * one, so coming back to the same cluster keeps what the Nodes screen showed.
     */
    fun scope(key: Any?) {
        if (key == this.key) return
        this.key = key
        sources.value = emptyMap()
        _nodes.value = emptySet()
    }

    companion object {
        const val OVERVIEW = "overview"
        const val DENSE_CARD = "dense-card"
        const val NODES_SCREEN = "nodes-screen"
    }
}
