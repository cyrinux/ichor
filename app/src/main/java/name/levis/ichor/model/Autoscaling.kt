package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_autoscaling.go: what scales the cluster (HPAs, KEDA, VPA,
// Karpenter) and the per-workload index. Quantities stay strings ("250m", "63%"): the Go
// side already decided the bools (atMax, atLimit).

/** What drives a workload's replica count. */
const val AUTOSCALED_BY_HPA = "hpa"
const val AUTOSCALED_BY_KEDA = "keda"
const val AUTOSCALED_BY_VPA = "vpa"

/** The state an autoscaler row is in, problems first (the Go order). */
const val AUTOSCALE_NOT_READY = "notReady"
const val AUTOSCALE_AT_MAX = "atMax"
const val AUTOSCALE_LIMITED = "limited"
const val AUTOSCALE_FALLBACK = "fallback"
const val AUTOSCALE_PAUSED = "paused"
const val AUTOSCALE_OK = "ok"

/** Everything that scales the cluster (os:admin); a group the cluster lacks says so. */
@Serializable
data class AutoscalingReport(
    val hpa: HpaGroup = HpaGroup(),
    val keda: KedaGroup = KedaGroup(),
    val vpa: VpaGroup = VpaGroup(),
    val karpenter: KarpenterGroup = KarpenterGroup(),
    /** One entry per (workload, autoscaler kind), for the workload sheet's chip. */
    val workloads: List<AutoscaledWorkload> = emptyList(),
    @SerialName("workloadsTruncated") val workloadsTruncated: Boolean = false,
)

/** What every group says about itself before its rows. */
@Serializable
data class AutoscalingGroupState(
    val installed: Boolean = false,
    val version: String = "",
    /** This group's read failed (often a 403): the other groups still show. */
    val error: String = "",
    val truncated: Boolean = false,
)

@Serializable
data class HpaGroup(
    val installed: Boolean = false,
    val version: String = "",
    val error: String = "",
    val truncated: Boolean = false,
    val items: List<HpaRow> = emptyList(),
)

@Serializable
data class KedaGroup(
    val installed: Boolean = false,
    val version: String = "",
    val error: String = "",
    val truncated: Boolean = false,
    val scaledObjects: List<ScaledObjectRow> = emptyList(),
    val scaledJobs: List<ScaledJobRow> = emptyList(),
)

@Serializable
data class VpaGroup(
    val installed: Boolean = false,
    val version: String = "",
    val error: String = "",
    val truncated: Boolean = false,
    val items: List<VpaRow> = emptyList(),
)

@Serializable
data class KarpenterGroup(
    val installed: Boolean = false,
    val version: String = "",
    val error: String = "",
    val truncated: Boolean = false,
    val nodePools: List<NodePoolRow> = emptyList(),
    val nodeClaims: List<NodeClaimRow> = emptyList(),
)

/** The workload an autoscaler drives. */
@Serializable
data class AutoscaleTarget(val kind: String = "", val name: String = "") {
    val label: String get() = listOf(kind, name).filter { it.isNotEmpty() }.joinToString(" ")
}

/** One HorizontalPodAutoscaler. One KEDA created (managedBy keda) links its ScaledObject. */
@Serializable
data class HpaRow(
    val namespace: String = "",
    val name: String = "",
    val target: AutoscaleTarget = AutoscaleTarget(),
    val min: Int = 0,
    val max: Int = 0,
    val current: Int = 0,
    val desired: Int = 0,
    val metrics: List<HpaMetric> = emptyList(),
    val conditions: HpaConditions = HpaConditions(),
    val atMax: Boolean = false,
    /** Unix ms, 0 when it never scaled. */
    val lastScaleAt: Long = 0,
    val managedBy: String = "",
    val scaledObject: String = "",
    val state: String = AUTOSCALE_OK,
) {
    val key: String get() = "$namespace/$name"
}

/** One metric the HPA scales on: current against target, as the user reads them. */
@Serializable
data class HpaMetric(val type: String = "", val name: String = "", val current: String = "", val target: String = "") {
    /** "Resource cpu" or the metric's own name when the type is not Resource. */
    val label: String get() = if (type == "Resource") name else "$type $name".trim()
}

@Serializable
data class HpaConditions(
    val ableToScale: Boolean = false,
    val scalingActive: Boolean = false,
    val scalingLimited: Boolean = false,
    val message: String = "",
)

/** One KEDA ScaledObject; current and desired come from the HPA it owns. */
@Serializable
data class ScaledObjectRow(
    val namespace: String = "",
    val name: String = "",
    val target: AutoscaleTarget = AutoscaleTarget(),
    val min: Int = 0,
    val max: Int = 0,
    val current: Int = 0,
    val desired: Int = 0,
    val triggers: List<KedaTrigger> = emptyList(),
    val fallbackReplicas: Int = 0,
    val paused: Boolean = false,
    /** Set when paused at a count. */
    val pausedReplicas: Int? = null,
    val ready: Boolean = false,
    val active: Boolean = false,
    val fallback: Boolean = false,
    val message: String = "",
    val hpaName: String = "",
    val state: String = AUTOSCALE_OK,
) {
    val key: String get() = "$namespace/$name"
}

@Serializable
data class ScaledJobRow(
    val namespace: String = "",
    val name: String = "",
    val triggers: List<KedaTrigger> = emptyList(),
    val maxReplicaCount: Int = 0,
    val paused: Boolean = false,
    val ready: Boolean = false,
    val message: String = "",
    val state: String = AUTOSCALE_OK,
) {
    val key: String get() = "$namespace/$name"
}

/** One trigger: its type and the one setting the user recognises. Never a secret. */
@Serializable
data class KedaTrigger(val type: String = "", val summary: String = "") {
    val label: String get() = listOf(type, summary).filter { it.isNotEmpty() }.joinToString(": ")
}

/** One VerticalPodAutoscaler with its recommendation per container. */
@Serializable
data class VpaRow(
    val namespace: String = "",
    val name: String = "",
    val target: AutoscaleTarget = AutoscaleTarget(),
    /** Off, Initial, Recreate, Auto or InPlaceOrRecreate. */
    val updateMode: String = "",
    val containers: List<VpaContainer> = emptyList(),
    val recommendationProvided: Boolean = false,
    val message: String = "",
    val state: String = AUTOSCALE_OK,
) {
    val key: String get() = "$namespace/$name"
}

/** The recommender's answer for one container next to its live requests ("" when unread). */
@Serializable
data class VpaContainer(
    val name: String = "",
    val cpuTarget: String = "",
    val memoryTarget: String = "",
    val cpuLower: String = "",
    val cpuUpper: String = "",
    val memoryLower: String = "",
    val memoryUpper: String = "",
    val cpuRequest: String = "",
    val memoryRequest: String = "",
)

/** One Karpenter NodePool: its limits against what it runs, and how it may disrupt nodes. */
@Serializable
data class NodePoolRow(
    val name: String = "",
    val limits: NodePoolLimits = NodePoolLimits(),
    val resources: NodePoolResources = NodePoolResources(),
    val atLimit: Boolean = false,
    val disruption: NodePoolDisruption = NodePoolDisruption(),
    val weight: Int = 0,
    val ready: Boolean = false,
    val message: String = "",
    val capacityTypes: List<String> = emptyList(),
) {
    val key: String get() = name
}

@Serializable
data class NodePoolLimits(val cpu: String = "", val memory: String = "")

@Serializable
data class NodePoolResources(val cpu: String = "", val memory: String = "", val nodes: Int = 0)

@Serializable
data class NodePoolDisruption(
    val consolidationPolicy: String = "",
    val consolidateAfter: String = "",
    val budgets: List<NodePoolBudget> = emptyList(),
)

@Serializable
data class NodePoolBudget(
    val nodes: String = "",
    val schedule: String = "",
    val duration: String = "",
    val reasons: List<String> = emptyList(),
)

/** One Karpenter NodeClaim: a node being launched, running, or on its way out. */
@Serializable
data class NodeClaimRow(
    val name: String = "",
    val pool: String = "",
    val nodeName: String = "",
    val instanceType: String = "",
    val capacityType: String = "",
    val launched: Boolean = false,
    val registered: Boolean = false,
    val initialized: Boolean = false,
    val ready: Boolean = false,
    val message: String = "",
    /** Unix ms. */
    val created: Long = 0,
    val pending: Boolean = false,
    val drifted: Boolean = false,
    val disrupting: Boolean = false,
) {
    val key: String get() = name
}

/** One (workload, autoscaler kind) pair: what the workload sheet names. */
@Serializable
data class AutoscaledWorkload(
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
    val by: String = "",
    val autoscaler: String = "",
    val min: Int = 0,
    val max: Int = 0,
    val current: Int = 0,
    val desired: Int = 0,
    val paused: Boolean = false,
    val state: String = AUTOSCALE_OK,
) {
    val key: String get() = "$kind/$namespace/$name/$by"
}

/** One problem row of the "Needs attention" section, across the four groups. */
data class AutoscalingProblem(
    /** hpa, keda, vpa or karpenter. */
    val group: String,
    /** The row's key inside its group ("ns/name", or the pool/claim name). */
    val key: String,
    val namespace: String,
    val name: String,
    val state: String,
    /** The row's own message, "" when it has none. */
    val message: String,
) {
    val label: String get() = listOf(namespace, name).filter { it.isNotEmpty() }.joinToString("/")
}

/** The rows that need attention, group by group in the Go order (problems first inside). */
fun AutoscalingReport.problems(): List<AutoscalingProblem> = buildList {
    hpa.items.filter { it.state != AUTOSCALE_OK }.forEach {
        add(AutoscalingProblem(AUTOSCALED_BY_HPA, it.key, it.namespace, it.name, it.state, it.conditions.message))
    }
    keda.scaledObjects.filter { it.state != AUTOSCALE_OK }.forEach {
        add(AutoscalingProblem(AUTOSCALED_BY_KEDA, it.key, it.namespace, it.name, it.state, it.message))
    }
    keda.scaledJobs.filter { it.state != AUTOSCALE_OK }.forEach {
        add(AutoscalingProblem(AUTOSCALED_BY_KEDA, it.key, it.namespace, it.name, it.state, it.message))
    }
    vpa.items.filter { it.state != AUTOSCALE_OK }.forEach {
        add(AutoscalingProblem(AUTOSCALED_BY_VPA, it.key, it.namespace, it.name, it.state, it.message))
    }
    karpenter.nodePools.filter { it.atLimit || !it.ready }.forEach {
        add(AutoscalingProblem("karpenter", it.key, "", it.name, if (it.atLimit) AUTOSCALE_AT_MAX else AUTOSCALE_NOT_READY, it.message))
    }
    karpenter.nodeClaims.filter { it.pending || it.drifted || it.disrupting || !it.ready }.forEach {
        val state = when {
            it.pending -> AUTOSCALE_NOT_READY
            it.disrupting -> AUTOSCALE_PAUSED
            it.drifted -> AUTOSCALE_LIMITED
            else -> AUTOSCALE_NOT_READY
        }
        add(AutoscalingProblem("karpenter", it.key, "", it.name, state, it.message))
    }
}

/** "2 of 1–10": where the replica count sits between the bounds. */
fun hpaBoundsText(current: Int, min: Int, max: Int): String = "$current of $min–$max"

/** "100m → 250m": the live request against what the recommender wants (the CAST AI style). */
fun vpaChangeText(request: String, target: String): String = when {
    request.isEmpty() -> target
    target.isEmpty() -> request
    request == target -> target
    else -> "$request → $target"
}

/**
 * The rows a [namespace] (null: every one) and a [query] keep, group by group. Karpenter is
 * cluster-level: its pools and claims show under every namespace.
 */
fun AutoscalingReport.filtered(namespace: String?, query: String): AutoscalingReport {
    val q = query.trim()
    fun keep(vararg texts: String): Boolean = q.isEmpty() || texts.any { it.contains(q, ignoreCase = true) }

    return copy(
        hpa = hpa.copy(items = hpa.items.filter { (namespace == null || it.namespace == namespace) && keep(it.name, it.namespace, it.target.kind, it.target.name) }),
        keda = keda.copy(
            scaledObjects = keda.scaledObjects.filter { (namespace == null || it.namespace == namespace) && keep(it.name, it.namespace, it.target.kind, it.target.name, *it.triggers.map { t -> t.label }.toTypedArray()) },
            scaledJobs = keda.scaledJobs.filter { (namespace == null || it.namespace == namespace) && keep(it.name, it.namespace, *it.triggers.map { t -> t.label }.toTypedArray()) },
        ),
        vpa = vpa.copy(items = vpa.items.filter { (namespace == null || it.namespace == namespace) && keep(it.name, it.namespace, it.target.kind, it.target.name, it.updateMode) }),
        karpenter = karpenter.copy(
            nodePools = karpenter.nodePools.filter { keep(it.name, it.capacityTypes.joinToString(" ")) },
            nodeClaims = karpenter.nodeClaims.filter { keep(it.name, it.pool, it.nodeName, it.instanceType, it.capacityType) },
        ),
    )
}

/** The namespaces with autoscaling rows, for the filter chips. */
val AutoscalingReport.autoscalingNamespaces: List<String>
    get() = (hpa.items.map { it.namespace } + keda.scaledObjects.map { it.namespace } + keda.scaledJobs.map { it.namespace } + vpa.items.map { it.namespace })
        .filter { it.isNotEmpty() }
        .distinct()
        .sorted()

/** The kinds an autoscaler's target usually has: their group and resource, for the browser. */
private val AUTOSCALE_TARGET_KINDS = mapOf(
    "Deployment" to ("apps" to "deployments"),
    "StatefulSet" to ("apps" to "statefulsets"),
    "DaemonSet" to ("apps" to "daemonsets"),
    "ReplicaSet" to ("apps" to "replicasets"),
    "Job" to ("batch" to "jobs"),
    "CronJob" to ("batch" to "cronjobs"),
    "Pod" to ("" to "pods"),
    "Service" to ("" to "services"),
)

/** The resource browser's reference of a well-known target kind; null for another. */
fun AutoscaleTarget.objectRef(namespace: String): KubeObjectRef? {
    val (group, resource) = AUTOSCALE_TARGET_KINDS[kind] ?: return null
    if (name.isEmpty()) return null
    return KubeObjectRef(group, "v1", resource, kind, namespace, name, editable = true)
}
