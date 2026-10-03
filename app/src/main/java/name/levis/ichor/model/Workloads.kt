package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/talosmobile/kube_workloads.go.

@Serializable
data class KubeWorkloadList(val workloads: List<KubeWorkload> = emptyList())

@Serializable
data class KubeWorkload(
    /** Deployment, StatefulSet or DaemonSet. */
    val kind: String,
    val namespace: String,
    val name: String,
    val desired: Int = 0,
    val ready: Int = 0,
    val updated: Int = 0,
    val available: Int = 0,
    /** ready, progressing, degraded, paused or scaledDown. */
    val state: String = "",
    /** Unix millis of the last rollout restart, 0 when never. */
    val restartedAt: Long = 0,
    /** Unix millis. */
    val created: Long = 0,
    val images: List<String> = emptyList(),
) {
    val key: String get() = "$kind/$namespace/$name"

    val workloadState: WorkloadState get() = WorkloadState.from(state)

    /** kubectl refuses to restart a paused Deployment, and so does the Go core. */
    val canRestart: Boolean get() = workloadState != WorkloadState.PAUSED
}

enum class WorkloadState(val wire: String) {
    READY("ready"),
    PROGRESSING("progressing"),
    DEGRADED("degraded"),
    PAUSED("paused"),
    SCALED_DOWN("scaledDown"),
    UNKNOWN(""),
    ;

    companion object {
        fun from(wire: String): WorkloadState = entries.firstOrNull { it.wire == wire } ?: UNKNOWN
    }
}

/** Namespaces that have workloads, sorted. */
val List<KubeWorkload>.namespaces: List<String>
    get() = map { it.namespace }.distinct().sorted()

/**
 * Workloads of [namespace] (all when null) whose name, kind or image contains [query]
 * (case-insensitive), the ones that need attention (degraded, progressing) first.
 */
fun List<KubeWorkload>.filtered(namespace: String?, query: String): List<KubeWorkload> {
    val q = query.trim()
    return filter { w ->
        (namespace == null || w.namespace == namespace) &&
            (q.isEmpty() || w.name.contains(q, ignoreCase = true) || w.kind.contains(q, ignoreCase = true) ||
                w.images.any { it.contains(q, ignoreCase = true) })
    }.sortedWith(compareBy<KubeWorkload> { it.attentionRank }.thenBy { it.namespace }.thenBy { it.name }.thenBy { it.kind })
}

private val KubeWorkload.attentionRank: Int
    get() = when (workloadState) {
        WorkloadState.DEGRADED -> 0
        WorkloadState.PROGRESSING -> 1
        else -> 2
    }

/**
 * The workloads that run [pods] (an app's, from the inventory), through each pod's owner in
 * [kubePods]: a StatefulSet or DaemonSet directly, a Deployment through its ReplicaSet, named
 * `<deployment>-<pod-template-hash>`. Pods without such an owner (static, Job, bare) are left
 * out. In this list's order.
 */
fun List<KubeWorkload>.ownersOf(pods: List<InventoryPod>, kubePods: List<KubePod>): List<KubeWorkload> {
    val owners = kubePods.associate { it.key to it.owner }
    val wanted = pods.mapNotNull { pod -> owners["${pod.namespace}/${pod.pod}"]?.let { ownerKey(pod.namespace, it) } }.toSet()
    return filter { it.key in wanted }
}

/** The [KubeWorkload.key] an owner reference ("ReplicaSet/web-5d8f") stands for; null for other kinds. */
private fun ownerKey(namespace: String, owner: String): String? {
    val kind = owner.substringBefore('/')
    val name = owner.substringAfter('/', "")
    if (name.isEmpty()) return null
    return when (kind) {
        "StatefulSet", "DaemonSet" -> "$kind/$namespace/$name"
        "ReplicaSet" -> name.substringBeforeLast('-', "").takeIf { it.isNotEmpty() }?.let { "Deployment/$namespace/$it" }
        else -> null
    }
}
