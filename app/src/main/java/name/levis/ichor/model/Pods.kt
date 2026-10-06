package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_pods.go.

/** One page of pods (KubePodsPage), in the API server's order. */
@Serializable
data class KubePodPage(
    val pods: List<KubePod> = emptyList(),
    @SerialName("continue") val continueToken: String = "",
    val remaining: Long = -1,
    val complete: Boolean = true,
) {
    /** [detailed]: asked as full objects rather than a Table. */
    fun toPage(detailed: Boolean) = KubePage(pods, continueToken, remaining, complete, detailed)
}

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
    /** The pod's containers, to pick one for its logs; empty from older cores. */
    val containerNames: List<String> = emptyList(),
    /** Why a restarted container last stopped ("OOMKilled (exit 137)"), "" when none did. */
    val lastTermination: String = "",
) {
    val key: String get() = "$namespace/$name"

    /** Pending or terminating: not broken, not done either. */
    val transitional: Boolean get() = status == "Pending" || status == "Terminating" || status.startsWith("Init:") && status.contains('/')
}

/** Namespaces that have pods, sorted. */
val List<KubePod>.podNamespaces: List<String>
    @JvmName("podNamespaces") get() = map { it.namespace }.distinct().sorted()

/**
 * Pods of [namespace] (all when null) whose name, status, node, owner or, when
 * [searchImages], image contains [query] (case-insensitive). [sorted]: unhealthy ones
 * first, then by namespace and name; else in the order loaded (a list still incomplete).
 */
fun List<KubePod>.filteredPods(namespace: String?, query: String, sorted: Boolean = true, searchImages: Boolean = true): List<KubePod> {
    val q = query.trim()
    val matching = filter { p ->
        (namespace == null || p.namespace == namespace) &&
            (q.isEmpty() || listOf(p.name, p.status, p.node, p.owner).any { it.contains(q, ignoreCase = true) } ||
                searchImages && p.images.any { it.contains(q, ignoreCase = true) })
    }
    return if (sorted) matching.sortedWith(compareBy<KubePod> { if (it.healthy) 1 else 0 }.thenBy { it.namespace }.thenBy { it.name }) else matching
}
