package name.levis.ichor.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * The fleet screen's list (the Go core's FleetSummary): every cluster's last background check,
 * worst first, with the tone counts, the worst tone ([FleetTone.UNKNOWN] for an empty fleet)
 * and one group per tag (drawn by a later step).
 */
@Serializable
data class FleetSummary(
    val clusters: List<FleetRow> = emptyList(),
    val counts: FleetCounts = FleetCounts(),
    val worst: FleetTone = FleetTone.UNKNOWN,
    val groups: List<FleetGroup> = emptyList(),
)

/** A row's tone, worst first: down or critical, degraded, not checked lately, fine. */
@Serializable
enum class FleetTone {
    @SerialName("bad")
    BAD,

    @SerialName("warn")
    WARN,

    @SerialName("unknown")
    UNKNOWN,

    @SerialName("ok")
    OK,
}

/**
 * One cluster's last check: [context] is the context to switch to, [checkedAt] when it ran
 * (unix ms, 0: never), [stale] when it is over an hour old (its counts stay on the row).
 */
@Serializable
data class FleetRow(
    val key: String,
    val name: String,
    val context: String,
    val kube: Boolean = false,
    val tags: List<String> = emptyList(),
    val tone: FleetTone = FleetTone.UNKNOWN,
    val stale: Boolean = false,
    val checkedAt: Long = 0,
    val nodes: Int = 0,
    val ready: Int = 0,
    val notReady: Int = 0,
    val unreachable: Int = 0,
    val etcdAlarms: Int = 0,
    val critical: Int = 0,
    val warnings: Int = 0,
    val tracksChecked: List<String> = emptyList(),
    val topFinding: FleetFinding? = null,
)

/**
 * The one thing a row says is wrong: [track] (nodes, reach, etcd, alertmanager, checkup, data,
 * gitops, storage, sensor, plugin), [severity] (critical or warning), [subject] in the core's
 * words ("node-b unreachable"), [detail] the GitOps tool (argocd, flux) or empty.
 */
@Serializable
data class FleetFinding(
    val track: String,
    val severity: String = FLEET_WARNING,
    val subject: String = "",
    val detail: String = "",
)

@Serializable
data class FleetCounts(val bad: Int = 0, val warn: Int = 0, val unknown: Int = 0, val ok: Int = 0)

/** The clusters with [tag] ("" for the untagged ones), by [keys] in ranked order. */
@Serializable
data class FleetGroup(val tag: String = "", val counts: FleetCounts = FleetCounts(), val keys: List<String> = emptyList())

const val FLEET_CRITICAL = "critical"
const val FLEET_WARNING = "warning"

/** The finding tracks whose subject names no user object (a node count, an etcd alarm). */
private val GENERIC_TRACKS = setOf("nodes", "reach", "etcd")

/**
 * Whether [FleetFinding.subject] may be shown: in screenshot mode ([masked]) only the generic
 * ones, since the others carry the alert, namespace or app names the check read.
 */
fun FleetFinding.subjectShown(masked: Boolean): Boolean = subject.isNotBlank() && (!masked || track in GENERIC_TRACKS)

/** Rows that need a look (bad or warn). */
val FleetCounts.needAttention: Int get() = bad + warn
