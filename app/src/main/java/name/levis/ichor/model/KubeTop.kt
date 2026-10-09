package name.levis.ichor.model

import kotlinx.serialization.Serializable
import java.util.Locale

/**
 * `kubectl top` from metrics-server (Go `KubeTopNodes`/`KubeTopPods`). CPU in cores, memory in
 * bytes. [available] false: no metrics-server; [forbidden]: the credentials may not read it.
 */
@Serializable
data class KubeTopNodes(
    val available: Boolean = false,
    val forbidden: Boolean = false,
    val nodes: List<KubeTopNode> = emptyList(),
) {
    val byName: Map<String, KubeTopNode> get() = nodes.associateBy { it.name }
}

@Serializable
data class KubeTopNode(
    val name: String = "",
    val cpu: Double = 0.0,
    val memory: Double = 0.0,
    val cpuAllocatable: Double = 0.0,
    val memoryAllocatable: Double = 0.0,
    val cpuPercent: Double = 0.0,
    val memoryPercent: Double = 0.0,
)

@Serializable
data class KubeTopPods(
    val available: Boolean = false,
    val forbidden: Boolean = false,
    val pods: List<KubeTopPod> = emptyList(),
) {
    /** By [KubeTopPod.key], the key pod rows use. */
    val byKey: Map<String, KubeTopPod> get() = pods.associateBy { it.key }
}

/** A pod's usage with its requests and limits; a limit of 0 means some container has none. */
@Serializable
data class KubeTopPod(
    val namespace: String = "",
    val name: String = "",
    val node: String = "",
    val cpu: Double = 0.0,
    val memory: Double = 0.0,
    val cpuRequest: Double = 0.0,
    val cpuLimit: Double = 0.0,
    val memoryRequest: Double = 0.0,
    val memoryLimit: Double = 0.0,
) {
    val key: String get() = "$namespace/$name"

    /** CPU use as a share of the limit, else of the request; null when neither is set. */
    fun cpuFraction(): Float? = fractionOf(cpu, cpuLimit.takeIf { it > 0 } ?: cpuRequest)

    fun memoryFraction(): Float? = fractionOf(memory, memoryLimit.takeIf { it > 0 } ?: memoryRequest)
}

private fun fractionOf(used: Double, of: Double): Float? = if (of > 0) (used / of).coerceIn(0.0, 1.0).toFloat() else null

/** CPU as kubectl prints it: millicores under one core ("125m"), cores above ("1.5"). */
fun formatCpu(cores: Double): String = when {
    cores < 0.9995 -> "${Math.round(cores * 1000)}m"
    cores % 1.0 == 0.0 -> cores.toLong().toString()
    else -> String.format(Locale.ROOT, "%.1f", cores).removeSuffix(".0")
}

/** How a usage list is ordered. */
enum class TopSort { NAME, CPU, MEMORY }

/**
 * Busiest first for CPU and MEMORY, rows without metrics last; [TopSort.NAME] keeps the order
 * given. [usage] is a row's metrics (a pod row's, looked up by key).
 */
fun <T> List<T>.sortedByUsage(sort: TopSort, usage: (T) -> KubeTopPod?): List<T> = when (sort) {
    TopSort.NAME -> this
    TopSort.CPU -> sortedByDescending { usage(it)?.cpu ?: -1.0 }
    TopSort.MEMORY -> sortedByDescending { usage(it)?.memory ?: -1.0 }
}
