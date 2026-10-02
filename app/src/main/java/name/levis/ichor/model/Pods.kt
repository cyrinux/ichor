package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/talosmobile/kube_pods.go.

@Serializable
data class KubePodList(val pods: List<KubePod> = emptyList())

@Serializable
data class KubePod(
    val namespace: String,
    val name: String,
    /** What `kubectl get pods` shows: Running, Pending, CrashLoopBackOff, Init:Error... */
    val status: String = "",
    /** Running with every container ready, or completed. */
    val healthy: Boolean = false,
    val ready: Int = 0,
    val containers: Int = 0,
    val restarts: Int = 0,
    val node: String = "",
    /** "ReplicaSet/web-5d8f", empty when none. */
    val owner: String = "",
    /** Unix millis. */
    val created: Long = 0,
    val images: List<String> = emptyList(),
) {
    val key: String get() = "$namespace/$name"

    /** Pending or terminating: not broken, not done either. */
    val transitional: Boolean get() = status == "Pending" || status == "Terminating" || status.startsWith("Init:") && status.contains('/')
}

/** Namespaces that have pods, sorted. */
val List<KubePod>.podNamespaces: List<String>
    @JvmName("podNamespaces") get() = map { it.namespace }.distinct().sorted()

/**
 * Pods of [namespace] (all when null) whose name, status, node, owner or image contains
 * [query] (case-insensitive), unhealthy ones first, then by namespace and name.
 */
fun List<KubePod>.filteredPods(namespace: String?, query: String): List<KubePod> {
    val q = query.trim()
    return filter { p ->
        (namespace == null || p.namespace == namespace) &&
            (q.isEmpty() || listOf(p.name, p.status, p.node, p.owner).any { it.contains(q, ignoreCase = true) } ||
                p.images.any { it.contains(q, ignoreCase = true) })
    }.sortedWith(compareBy<KubePod> { if (it.healthy) 1 else 0 }.thenBy { it.namespace }.thenBy { it.name })
}
