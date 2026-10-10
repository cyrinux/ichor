package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/containers.go.

@Serializable
data class ContainerSample(val at: Long, val containers: List<ContainerInfo> = emptyList())

@Serializable
data class ContainerInfo(
    val id: String,
    /** Containerd namespace: [SYSTEM_CONTAINERS] (Talos' own) or [K8S_CONTAINERS]. */
    val namespace: String = K8S_CONTAINERS,
    val podNamespace: String = "",
    val pod: String = "",
    val name: String = "",
    val image: String = "",
    /** e.g. CONTAINER_RUNNING. */
    val status: String = "",
    val pid: Long = 0,
    val memory: Long = 0,
    /** Cumulative CPU time in nanoseconds. */
    val cpuNanos: Long = 0,
)

const val SYSTEM_CONTAINERS = "system"
const val K8S_CONTAINERS = "k8s.io"

/** A Talos container (apid, trustd, an extension service), not a Kubernetes one. */
val ContainerInfo.system: Boolean get() = namespace == SYSTEM_CONTAINERS

val ContainerInfo.running: Boolean get() = status.equals("CONTAINER_RUNNING", ignoreCase = true) || status.equals("running", ignoreCase = true)

/** "CONTAINER_EXITED" → "exited". */
val ContainerInfo.statusLabel: String get() = status.removePrefix("CONTAINER_").lowercase()

data class ContainerRow(val info: ContainerInfo, val cpuPercent: Double)

/** Containers of one pod, or Talos' own containers when [system], with totals for its header. */
data class PodGroup(val namespace: String, val pod: String, val containers: List<ContainerRow>, val system: Boolean = false) {
    val memory: Long get() = containers.sumOf { it.info.memory }
    val cpuPercent: Double get() = containers.sumOf { it.cpuPercent }
}

enum class ContainerSort { CPU, MEMORY }

/**
 * CPU% per container id between two samples: CPU nanoseconds used over wall nanoseconds
 * elapsed, × 100. 0 without a usable previous sample, for a new id (a restarted container
 * gets a new id), or when the counter went backwards.
 */
fun containerCpuPercents(previous: ContainerSample?, current: ContainerSample): Map<String, Double> {
    val wallNanos = previous?.let { (current.at - it.at) * 1_000_000.0 } ?: 0.0
    val before = previous?.containers?.associateBy { it.id }.orEmpty()
    return current.containers.associate { c ->
        val old = before[c.id]
        val percent = when {
            wallNanos <= 0 || old == null || c.cpuNanos < old.cpuNanos -> 0.0
            else -> (c.cpuNanos - old.cpuNanos) / wallNanos * 100
        }
        c.id to percent
    }
}

fun containerRows(previous: ContainerSample?, current: ContainerSample): List<ContainerRow> {
    val cpu = containerCpuPercents(previous, current)
    return current.containers.map { ContainerRow(it, cpu[it.id] ?: 0.0) }
}

private fun ContainerRow.matches(query: String) = listOf(info.podNamespace, info.pod, info.name, info.image)
    .any { it.contains(query, ignoreCase = true) }

/**
 * Containers matching [filter] (namespace, pod, container name or image; case-insensitive),
 * grouped by pod; Talos' system containers form one group, listed first. Pods and the
 * containers inside them are sorted by [sort], heaviest first.
 */
fun List<ContainerRow>.podGroups(filter: String, sort: ContainerSort): List<PodGroup> {
    val query = filter.trim()
    val matching = if (query.isEmpty()) this else filter { it.matches(query) }
    val containerOrder = when (sort) {
        ContainerSort.CPU -> compareByDescending<ContainerRow> { it.cpuPercent }.thenByDescending { it.info.memory }
        ContainerSort.MEMORY -> compareByDescending<ContainerRow> { it.info.memory }.thenByDescending { it.cpuPercent }
    }.thenBy { it.info.name }
    val podOrder = compareByDescending<PodGroup> { it.system }.then(
        when (sort) {
            ContainerSort.CPU -> compareByDescending<PodGroup> { it.cpuPercent }.thenByDescending { it.memory }
            ContainerSort.MEMORY -> compareByDescending<PodGroup> { it.memory }.thenByDescending { it.cpuPercent }
        },
    ).thenBy { it.namespace }.thenBy { it.pod }
    return matching.groupBy { Triple(it.info.system, it.info.podNamespace, it.info.pod) }
        .map { (key, rows) -> PodGroup(key.second, key.third, rows.sortedWith(containerOrder), system = key.first) }
        .sortedWith(podOrder)
}
