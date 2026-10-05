package name.levis.ichor.model

import kotlinx.serialization.Serializable
import java.util.Locale

// Mirrors go/ichorgo/kube_audit*.go: who loads the Kubernetes API server and what they do
// wrong, from the control planes' audit logs.

/** The kinds of [AuditFinding], as Go names them. */
object AuditKind {
    const val THROTTLED = "throttled"
    const val HOT_CLIENT = "hotClient"
    const val LIST_LOOP = "listLoop"
    const val WATCH_CHURN = "watchChurn"
    const val FORBIDDEN = "forbidden"
    const val MISSING_API = "missingAPI"
    const val MISSING_OBJECT = "missingObject"
    const val CONFLICTS = "conflicts"
    const val ALREADY_EXISTS = "alreadyExists"
    const val HOT_OBJECT = "hotObject"
    const val EVENT_SPAM = "eventSpam"
    const val SLOW = "slow"
    const val SERVER_ERRORS = "serverErrors"
    const val WIDE_ERRORS = "widespreadErrors"
    const val WIDE_SLOW = "widespreadSlow"
    const val WIDE_WATCH = "widespreadWatchChurn"
    const val STALE_LOG = "staleLog"
    const val UNAUTHORIZED = "unauthorized"
}

enum class AuditSeverity { CRITICAL, WARNING, INFO }

/** What the audit logs of the control planes show over [seconds] (from..to, unix ms). */
@Serializable
data class AuditReport(
    val nodes: List<AuditNodeRead> = emptyList(),
    val from: Long = 0,
    val to: Long = 0,
    val seconds: Double = 0.0,
    val requests: Int = 0,
    val findings: List<AuditFinding> = emptyList(),
    val actors: List<AuditActorRow> = emptyList(),
)

/** How reading one control plane's log went: compressed bytes on the wire, events, or why it failed. */
@Serializable
data class AuditNodeRead(
    val node: String,
    val bytes: Long = 0,
    val events: Int = 0,
    val last: Long = 0,
    val error: String = "",
)

/** Who sent requests: a service account (namespace, name), a node, a control-plane component or a person. */
@Serializable
data class AuditActor(
    val user: String = "",
    val agent: String = "",
    val kind: String = "",
    val namespace: String = "",
    val name: String = "",
)

/**
 * One problem with its evidence. [value] depends on [kind]: the share of all requests in
 * percent (hotClient), the mean interval in seconds (listLoop, watchChurn, hotObject,
 * widespreadWatchChurn), the mean latency in ms (slow, widespreadSlow), or how long ago in
 * seconds (staleLog, with the node in [name]).
 */
@Serializable
data class AuditFinding(
    val kind: String,
    val severity: String = "",
    val actor: AuditActor = AuditActor(),
    val count: Int = 0,
    val rate: Double = 0.0,
    val verb: String = "",
    val resource: String = "",
    val namespace: String = "",
    val name: String = "",
    val value: Double = 0.0,
    val code: Int = 0,
    val actors: Int = 0,
    val objects: Int = 0,
    val examples: List<String> = emptyList(),
)

@Serializable
data class AuditActorRow(
    val actor: AuditActor = AuditActor(),
    val requests: Int = 0,
    val rate: Double = 0.0,
    val share: Double = 0.0,
    val errors: Int = 0,
    val throttled: Int = 0,
    val latencyMs: Double = 0.0,
    val topVerb: String = "",
    val topResource: String = "",
    val topCount: Int = 0,
)

val AuditFinding.level: AuditSeverity
    get() = when (severity) {
        "critical" -> AuditSeverity.CRITICAL
        "warning" -> AuditSeverity.WARNING
        else -> AuditSeverity.INFO
    }

/** The finding is about the API server itself, not one client. */
val AuditFinding.aboutServer: Boolean
    get() = kind == AuditKind.WIDE_ERRORS || kind == AuditKind.WIDE_SLOW || kind == AuditKind.WIDE_WATCH || kind == AuditKind.STALE_LOG

/** The object the finding is about: "namespace/name", the namespace, or the resource cluster-wide. */
val AuditFinding.target: String
    get() = when {
        name.isNotEmpty() && namespace.isNotEmpty() -> "$resource $namespace/$name"
        name.isNotEmpty() -> "$resource $name"
        namespace.isNotEmpty() -> "$resource ($namespace)"
        else -> resource
    }

/** Who: "monitoring/pod-exporter" for a service account, the node, the component or the user. */
val AuditActor.label: String
    get() = when (kind) {
        "serviceAccount" -> "$namespace/$name"
        else -> name.ifEmpty { user }
    }

/** An interval or age in seconds as "0.5 s", "26 s", "4 min" or "3.2 h". */
fun formatSeconds(seconds: Double): String = when {
    seconds < 10 -> String.format(Locale.ROOT, "%.1f s", seconds)
    seconds < 120 -> String.format(Locale.ROOT, "%.0f s", seconds)
    seconds < 7200 -> String.format(Locale.ROOT, "%.0f min", seconds / 60)
    else -> String.format(Locale.ROOT, "%.1f h", seconds / 3600)
}

/** Compressed bytes read, as "6.4 MB". */
fun formatMegabytes(bytes: Long): String = String.format(Locale.ROOT, "%.1f MB", bytes / 1_000_000.0)
