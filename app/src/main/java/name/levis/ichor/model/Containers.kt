package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/containers.go.

@Serializable
data class ContainerSample(val at: Long, val containers: List<ContainerInfo> = emptyList())

@Serializable
data class ContainerInfo(
    val id: String,
    /** Containerd namespace: [SYSTEM_CONTAINERS] (Talos' own), [TALOS_CONTAINERS] or [K8S_CONTAINERS]. */
    val namespace: String = K8S_CONTAINERS,
    val podNamespace: String = "",
    val pod: String = "",
    val name: String = "",
    val image: String = "",
    /** e.g. CONTAINER_RUNNING; a Talos container's state (running, backoff, failed...). */
    val status: String = "",
    val pid: Long = 0,
    val memory: Long = 0,
    /** Cumulative CPU time in nanoseconds. */
    val cpuNanos: Long = 0,
    // A Talos container's ContainerStatus (Talos 1.15); empty for the other namespaces.
    /** pending, pulling, healthy or degraded. */
    val health: String = "",
    val restartCount: Long = 0,
    val exitCode: Int = 0,
    val error: String = "",
    /** The unmet gates (image, mounts, dependencies) while pending. */
    val waitingFor: List<String> = emptyList(),
    /** What it wrote before it failed: workload output, shown as text, never notified. */
    val lastLog: String = "",
)

const val SYSTEM_CONTAINERS = "system"
const val K8S_CONTAINERS = "k8s.io"

/** Containers a ContainerConfig declares, run by Talos without Kubernetes (Talos 1.15). */
const val TALOS_CONTAINERS = "taloscontainers"

/** A Talos container (apid, trustd, an extension service), not a Kubernetes one. */
val ContainerInfo.system: Boolean get() = namespace == SYSTEM_CONTAINERS

/** A container a ContainerConfig declares. */
val ContainerInfo.talosContainer: Boolean get() = namespace == TALOS_CONTAINERS

/** The line a row shows for a Talos container in trouble: the last line it logged, else its error. */
val ContainerInfo.lastErrorLine: String
    get() = lastLog.lineSequence().map { it.trim() }.lastOrNull { it.isNotEmpty() } ?: error.trim()

/** A Talos container's health as a tone: healthy, degraded or on its way (pending, pulling). */
enum class TalosContainerHealth { HEALTHY, DEGRADED, STARTING }

val ContainerInfo.talosHealth: TalosContainerHealth
    get() = when {
        status.equals("failed", ignoreCase = true) || health.equals("degraded", ignoreCase = true) -> TalosContainerHealth.DEGRADED
        health.equals("healthy", ignoreCase = true) -> TalosContainerHealth.HEALTHY
        else -> TalosContainerHealth.STARTING
    }

val ContainerInfo.running: Boolean get() = status.equals("CONTAINER_RUNNING", ignoreCase = true) || status.equals("running", ignoreCase = true)

/** "CONTAINER_EXITED" → "exited". */
val ContainerInfo.statusLabel: String get() = status.removePrefix("CONTAINER_").lowercase()

data class ContainerRow(val info: ContainerInfo, val cpuPercent: Double)

/**
 * Every node's Talos containers in trouble (go/ichorgo/talos_containers_health.go): issues keyed
 * "node|name", valued "severity|state|restartCount|exitCode|hostname|name"; [unread] the nodes
 * that did not answer; [checked] at least one did.
 */
@Serializable
data class TalosContainersHealth(
    val context: String = "",
    val checked: Boolean = false,
    val unread: List<String> = emptyList(),
    val issues: Map<String, String> = emptyMap(),
)

/**
 * Containers of one pod, or Talos' own containers when [system], or the containers a
 * ContainerConfig declares when [talos], with totals for its header.
 */
data class PodGroup(
    val namespace: String,
    val pod: String,
    val containers: List<ContainerRow>,
    val system: Boolean = false,
    val talos: Boolean = false,
) {
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

/** The namespace a Talos-run container groups under; null for a Kubernetes one (grouped by pod). */
private val ContainerInfo.groupNamespace: String? get() = namespace.takeIf { system || talosContainer }

/** Where a group lists: Talos' own containers, then the Talos containers, then the pods. */
private val PodGroup.rank: Int get() = if (system) 0 else if (talos) 1 else 2

/**
 * Containers matching [filter] (namespace, pod, container name or image; case-insensitive),
 * grouped by pod; Talos' system containers form one group, listed first, then the Talos
 * containers (none: no group). Pods and the containers inside them are sorted by [sort],
 * heaviest first.
 */
fun List<ContainerRow>.podGroups(filter: String, sort: ContainerSort): List<PodGroup> {
    val query = filter.trim()
    val matching = if (query.isEmpty()) this else filter { it.matches(query) }
    val containerOrder = when (sort) {
        ContainerSort.CPU -> compareByDescending<ContainerRow> { it.cpuPercent }.thenByDescending { it.info.memory }
        ContainerSort.MEMORY -> compareByDescending<ContainerRow> { it.info.memory }.thenByDescending { it.cpuPercent }
    }.thenBy { it.info.name }
    val podOrder = compareBy<PodGroup> { it.rank }.then(
        when (sort) {
            ContainerSort.CPU -> compareByDescending<PodGroup> { it.cpuPercent }.thenByDescending { it.memory }
            ContainerSort.MEMORY -> compareByDescending<PodGroup> { it.memory }.thenByDescending { it.cpuPercent }
        },
    ).thenBy { it.namespace }.thenBy { it.pod }
    return matching.groupBy { Triple(it.info.groupNamespace, it.info.podNamespace, it.info.pod) }
        .map { (key, rows) ->
            PodGroup(
                key.second,
                key.third,
                rows.sortedWith(containerOrder),
                system = key.first == SYSTEM_CONTAINERS,
                talos = key.first == TALOS_CONTAINERS,
            )
        }
        .sortedWith(podOrder)
}
