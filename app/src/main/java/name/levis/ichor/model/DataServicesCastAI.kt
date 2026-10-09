package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_castai.go (see DataServices.kt); the wire format is documented in
// the Linear plan document "Data services: Longhorn, Garage and CloudNativePG health".

/** CAST AI's Workload Autoscaler: one recommendation per workload it manages. */
@Serializable
data class CastAIStatus(
    val version: String = "",
    val error: String = "",
    val recommendations: List<CastAIRecommendation> = emptyList(),
    /** Recommended minus original requests over every compared workload; negative is a saving. */
    val cpuDeltaMilli: Long = 0,
    val memoryDeltaBytes: Long = 0,
    /** Workloads whose original requests are known. */
    val compared: Int = 0,
    /** Node consolidations (RebalancePlan), newest first; see DataServicesCastAIPlans.kt. */
    val plans: List<CastAIPlan> = emptyList(),
    /** Nodes a plan failed to remove that a later plan tried again. */
    val stuck: List<CastAIStuckNode> = emptyList(),
    /** Why the plans could not be read; [error] is the recommendations' only. */
    val plansError: String = "",
)

@Serializable
data class CastAIRecommendation(
    val namespace: String = "",
    val name: String = "",
    /** The target workload's kind (Deployment, StatefulSet, CronJob) and name. */
    val kind: String = "",
    val workload: String = "",
    /** immediate, deferred or "" when unknown. */
    val mode: String = "",
    val readOnly: Boolean = false,
    val health: String = "",
    /** Wire values of [CastAIReason]. */
    val reasons: List<String> = emptyList(),
    /** The failing condition's message or the read-only reason, in CAST AI's words. */
    val message: String = "",
    val containers: List<CastAIContainer> = emptyList(),
    val cpuDeltaMilli: Long = 0,
    val memoryDeltaBytes: Long = 0,
    /** A pod's requests as recommended and before CAST AI (an unknown original counts as recommended). */
    val cpuMilli: Long = 0,
    val memoryBytes: Long = 0,
    val originalCpuMilli: Long = 0,
    val originalMemoryBytes: Long = 0,
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$workload"
    val reasonList: List<CastAIReason> get() = reasons.mapNotNull(CastAIReason::from)
    val applyMode: CastAIMode get() = CastAIMode.from(mode)

    /** Whether the requests grow or shrink; [CastAIChange.UNKNOWN] without the originals. */
    val change: CastAIChange
        get() = when {
            containers.none { it.originalCpu.isNotEmpty() || it.originalMemory.isNotEmpty() } -> CastAIChange.UNKNOWN
            cpuDeltaMilli > 0 || memoryDeltaBytes > 0 -> CastAIChange.GROW
            cpuDeltaMilli < 0 || memoryDeltaBytes < 0 -> CastAIChange.SHRINK
            else -> CastAIChange.SAME
        }

    /** The highest memory request as a share of its limit, 0 without limits. */
    val memoryLimitPercent: Int get() = containers.maxOfOrNull { it.memoryLimitPercent } ?: 0

    val nearMemoryLimit: Boolean get() = containers.any { it.nearMemoryLimit }

    /** How much the recommendation moves, one core weighing as much as [GIB_PER_CORE] GiB (cloud pricing). */
    val weight: Double
        get() = kotlin.math.abs(cpuDeltaMilli) / 1000.0 + kotlin.math.abs(memoryDeltaBytes) / GIB / GIB_PER_CORE

    private companion object {
        const val GIB = 1024.0 * 1024 * 1024
        const val GIB_PER_CORE = 4.0
    }
}

/** Which way a recommendation moves a workload's requests. */
enum class CastAIChange { SHRINK, GROW, SAME, UNKNOWN }

/** One container's recommended requests and limits, with the requests CAST AI first saw ("" unknown). */
@Serializable
data class CastAIContainer(
    val name: String = "",
    val cpu: String = "",
    val memory: String = "",
    val cpuLimit: String = "",
    val memoryLimit: String = "",
    val originalCpu: String = "",
    val originalMemory: String = "",
    /** The recommended request as a share of its limit, 0 without a limit. */
    val cpuLimitPercent: Int = 0,
    val memoryLimitPercent: Int = 0,
) {
    /** CAST AI keeps limits, so a request this close to its memory limit risks an OOM kill. */
    val nearMemoryLimit: Boolean get() = memoryLimitPercent >= CASTAI_NEAR_LIMIT_PERCENT
}

const val CASTAI_NEAR_LIMIT_PERCENT = 85

/** Why a recommendation needs a look, as the Go core names it. */
enum class CastAIReason(val wire: String) {
    VPA("vpa"),
    HPA("hpa"),
    READ_ONLY("readOnly"),
    ;

    companion object {
        fun from(wire: String): CastAIReason? = entries.firstOrNull { it.wire == wire }
    }
}

/** How CAST AI applies a recommendation. */
enum class CastAIMode(val wire: String) {
    IMMEDIATE("immediate"),
    DEFERRED("deferred"),
    UNKNOWN(""),
    ;

    companion object {
        fun from(wire: String): CastAIMode = entries.firstOrNull { it.wire == wire && wire.isNotEmpty() } ?: UNKNOWN
    }
}

/** Millicores as Kubernetes writes them, signed: -380 is "-380m", 2000 is "2". */
fun formatMilliCores(milli: Long, signed: Boolean = false): String {
    val sign = when {
        milli < 0 -> "-"
        signed && milli > 0 -> "+"
        else -> ""
    }
    val abs = kotlin.math.abs(milli)
    return if (abs % 1000 == 0L) "$sign${abs / 1000}" else "$sign${abs}m"
}
