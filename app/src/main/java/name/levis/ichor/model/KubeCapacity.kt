package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_capacity.go and kube_capacity_nodeloss.go. CPU in cores, memory in bytes.

/** A pod's QoS class: who the kubelet evicts first under node pressure (BestEffort). */
enum class QosClass { BEST_EFFORT, BURSTABLE, GUARANTEED, UNKNOWN }

/** A workload's risk word, worst first. */
enum class CapacityRisk { OOM, EVICT_FIRST, UNBOUNDED, OK }

/**
 * The cluster's capacity (Go `KubeCapacity`): free room per node and in total, each workload's
 * priority, QoS and OOM risk, how many more replicas fit, and whether one node's pods would fit
 * on the others ([nPlusOne]). [metricsAvailable] false: no metrics-server, usage is 0.
 */
@Serializable
data class KubeCapacity(
    val metricsAvailable: Boolean = false,
    val metricsForbidden: Boolean = false,
    val priorityClassesRead: Boolean = false,
    val pdbsRead: Boolean = false,
    val priorityClasses: List<CapacityPriorityClass> = emptyList(),
    val totals: CapacityTotals = CapacityTotals(),
    val nodes: List<CapacityNode> = emptyList(),
    /** What goes first first: lowest priority, no class before a class of the same value. */
    val workloads: List<CapacityWorkload> = emptyList(),
    val nPlusOne: NPlusOne = NPlusOne(),
    val assumptions: List<String> = emptyList(),
)

@Serializable
data class CapacityPriorityClass(
    val name: String = "",
    val value: Int = 0,
    val globalDefault: Boolean = false,
    val preemptionPolicy: String = "",
)

@Serializable
data class CapacityTotals(
    val cpuAllocatable: Double = 0.0,
    val cpuRequests: Double = 0.0,
    val cpuLimits: Double = 0.0,
    val cpuUsage: Double = 0.0,
    val memoryAllocatable: Double = 0.0,
    val memoryRequests: Double = 0.0,
    val memoryLimits: Double = 0.0,
    val memoryUsage: Double = 0.0,
    val pods: Int = 0,
    val podCapacity: Int = 0,
    val nodes: Int = 0,
    val schedulableNodes: Int = 0,
) {
    val cpuFree: Double get() = (cpuAllocatable - cpuRequests).coerceAtLeast(0.0)
    val memoryFree: Double get() = (memoryAllocatable - memoryRequests).coerceAtLeast(0.0)
    val podsFree: Int get() = (podCapacity - pods).coerceAtLeast(0)
}

/** A node's requests, limits and usage against what it offers; [overcommitted]: memory limits exceed it. */
@Serializable
data class CapacityNode(
    val name: String = "",
    val roles: List<String> = emptyList(),
    val ready: Boolean = false,
    val cordoned: Boolean = false,
    val taints: List<String> = emptyList(),
    val cpuRequests: Double = 0.0,
    val cpuAllocatable: Double = 0.0,
    val cpuPercent: Double = 0.0,
    val memoryRequests: Double = 0.0,
    val memoryAllocatable: Double = 0.0,
    val memoryPercent: Double = 0.0,
    val pods: Int = 0,
    val podCapacity: Int = 0,
    val cpuLimits: Double = 0.0,
    val memoryLimits: Double = 0.0,
    val cpuUsage: Double = 0.0,
    val memoryUsage: Double = 0.0,
    val cpuFree: Double = 0.0,
    val memoryFree: Double = 0.0,
    val podsFree: Int = 0,
    val overcommitted: Boolean = false,
) {
    /** Ordinary pods may land on it: ready, not cordoned, no NoSchedule/NoExecute taint. */
    val schedulable: Boolean
        get() = ready && !cordoned && taints.none { it.endsWith(":NoSchedule") || it.endsWith(":NoExecute") }

    /** Memory in use against allocatable, 0..1 (0 without metrics-server). */
    val memoryUsageFraction: Float get() = fraction(memoryUsage, memoryAllocatable)

    val cpuUsageFraction: Float get() = fraction(cpuUsage, cpuAllocatable)
}

/**
 * The running pods of one controller: one replica's requests and limits (a limit of 0: some pod
 * sets none), the usage of all of them. [pdbAllowed] is -1 without a PodDisruptionBudget.
 */
@Serializable
data class CapacityWorkload(
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val replicas: Int = 0,
    val cpuRequest: Double = 0.0,
    val cpuLimit: Double = 0.0,
    val memoryRequest: Double = 0.0,
    val memoryLimit: Double = 0.0,
    val cpuUsage: Double = 0.0,
    val memoryUsage: Double = 0.0,
    val priorityClassName: String = "",
    val priority: Int = 0,
    val noPriorityClass: Boolean = false,
    val qos: String = "",
    val pdb: String = "",
    val pdbAllowed: Int = -1,
    val oomKills: Int = 0,
    val risk: String = "",
    val moreFit: Int = 0,
    val moreFitNA: Boolean = false,
) {
    val key: String get() = "$kind/$namespace/$name"
    val qosClass: QosClass get() = qosOf(qos)
    val riskLevel: CapacityRisk get() = riskOf(risk)
    val atRisk: Boolean get() = riskLevel != CapacityRisk.OK
    val isSystem: Boolean get() = isSystemClass(priorityClassName, priority)
}

/** OK when every node's pods would fit on the others; otherwise [node] is the first that would not. */
@Serializable
data class NPlusOne(
    val ok: Boolean = true,
    val node: String = "",
    val pendingPods: Int = 0,
    val shortCPU: Double = 0.0,
    val shortMemory: Double = 0.0,
)

/**
 * Losing [node] — one node, or the joined names when several were picked ([nodes]) — where
 * their pods land ([placed]), what they evict ([preempted]), what stays [pending], and what
 * to scale down, lowest priority first. [worstCase]: the node was chosen as the worst of
 * [candidates].
 */
@Serializable
data class NodeLossPlan(
    val node: String = "",
    val nodes: List<String> = emptyList(),
    val worstCase: Boolean = false,
    val cpuLost: Double = 0.0,
    val memoryLost: Double = 0.0,
    val podsLost: Int = 0,
    val daemonSetPods: Int = 0,
    val placed: List<NodeLossPod> = emptyList(),
    val preempted: List<NodeLossPod> = emptyList(),
    val pending: List<NodeLossPod> = emptyList(),
    val suggestions: List<NodeLossSuggestion> = emptyList(),
    val covered: Boolean = true,
    val candidates: List<NodeLossCandidate> = emptyList(),
    val assumptions: List<String> = emptyList(),
)

/**
 * A pod of the plan. Placed: [to]; preempted: [node] it is evicted from and [by] ("ns/pod");
 * Pending: [reason] (cpu, memory or pods).
 */
@Serializable
data class NodeLossPod(
    val namespace: String = "",
    val name: String = "",
    val workload: String = "",
    val priority: Int = 0,
    val priorityClassName: String = "",
    val qos: String = "",
    val to: String = "",
    val node: String = "",
    val by: String = "",
    val pdbViolated: Boolean = false,
    val reason: String = "",
)

/** Scale [kind]/[namespace]/[name] from [replicas] to [scaleTo]; [pdbBlocks]: its budget lets none go. */
@Serializable
data class NodeLossSuggestion(
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val replicas: Int = 0,
    val scaleTo: Int = 0,
    val priority: Int = 0,
    val priorityClassName: String = "",
    val noPriorityClass: Boolean = false,
    val pdb: String = "",
    val pdbMinAvailable: Int = 0,
    val pdbBlocks: Boolean = false,
    val frees: NodeLossFrees = NodeLossFrees(),
) {
    val key: String get() = "$kind/$namespace/$name"
}

@Serializable
data class NodeLossFrees(val cpu: Double = 0.0, val memory: Double = 0.0, val pods: Int = 0)

@Serializable
data class NodeLossCandidate(val name: String = "", val pending: Int = 0, val preempted: Int = 0)

/** Where the system classes start: system-cluster-critical is 2000000000. */
const val SYSTEM_PRIORITY = 1_000_000_000

/** A system class, by name or by value: never preempted, never suggested. */
fun isSystemClass(name: String, priority: Int): Boolean = name.startsWith("system-") || priority >= SYSTEM_PRIORITY

/** [free] as a share of [of], 0..1; 0 when [of] is 0. */
fun freeFraction(free: Double, of: Double): Float = fraction(free, of)

private fun fraction(part: Double, of: Double): Float = if (of > 0) (part / of).coerceIn(0.0, 1.0).toFloat() else 0f

private fun qosOf(value: String): QosClass = when (value) {
    "BestEffort" -> QosClass.BEST_EFFORT
    "Burstable" -> QosClass.BURSTABLE
    "Guaranteed" -> QosClass.GUARANTEED
    else -> QosClass.UNKNOWN
}

private fun riskOf(value: String): CapacityRisk = when (value) {
    "oom" -> CapacityRisk.OOM
    "evictFirst" -> CapacityRisk.EVICT_FIRST
    "unbounded" -> CapacityRisk.UNBOUNDED
    else -> CapacityRisk.OK
}

/** The workloads a filter keeps, in the order given. */
enum class CapacityFilter { ALL, AT_RISK, NO_PRIORITY_CLASS }

fun List<CapacityWorkload>.filteredBy(filter: CapacityFilter): List<CapacityWorkload> = when (filter) {
    CapacityFilter.ALL -> this
    CapacityFilter.AT_RISK -> filter { it.atRisk }
    CapacityFilter.NO_PRIORITY_CLASS -> filter { it.noPriorityClass }
}

/**
 * The resource browser's reference of a suggestion's workload, to open its scale dialog; null
 * for a kind the browser does not scale.
 */
fun NodeLossSuggestion.objectRef(): KubeObjectRef? {
    if (kind == "Job") return KubeObjectRef.job(namespace, name)
    val resource = when (kind) {
        "Deployment" -> "deployments"
        "StatefulSet" -> "statefulsets"
        "ReplicaSet" -> "replicasets"
        else -> return null
    }
    return KubeObjectRef("apps", "v1", resource, kind, namespace, name, editable = true, scalable = true)
}
