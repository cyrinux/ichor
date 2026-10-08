package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/kube_castai.go (see DataServices.kt); the wire format is documented in
// plans/data-services/README.md.

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
) {
    val serviceHealth: ServiceHealth get() = ServiceHealth.from(health)
    val label: String get() = "$namespace/$workload"
    val reasonList: List<CastAIReason> get() = reasons.mapNotNull(CastAIReason::from)
    val applyMode: CastAIMode get() = CastAIMode.from(mode)
}

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
)

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
