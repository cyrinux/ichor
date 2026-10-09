package name.levis.ichor.model

import kotlinx.serialization.Serializable

// Mirrors go/ichorgo/alertmanager*.go (times in unix ms, 0 when unset).

/** An alert's or a group's severity, folded by the Go core from the `severity` label. */
object AmSeverity {
    const val CRITICAL = "critical"
    const val WARNING = "warning"
    const val INFO = "info"
    const val OTHER = "other"

    /** Worst first, as the groups are ordered. */
    val ALL = listOf(CRITICAL, WARNING, INFO, OTHER)
}

/** An alert's state in Alertmanager: suppressed means silenced or inhibited. */
object AmState {
    const val ACTIVE = "active"
    const val SUPPRESSED = "suppressed"
    const val UNPROCESSED = "unprocessed"
}

/** A silence's state. */
object AmSilenceState {
    const val ACTIVE = "active"
    const val PENDING = "pending"
    const val EXPIRED = "expired"
}

/** Alertmanager's matcher: `=` (equal, not regex), `!=`, `=~` (regex) or `!~`. */
@Serializable
data class AmMatcher(
    val name: String,
    val value: String,
    val isRegex: Boolean = false,
    val isEqual: Boolean = true,
) {
    /** `alertname="KubePodCrashLooping"`, as Alertmanager spells it. */
    val text: String
        get() {
            val op = when {
                isRegex && isEqual -> "=~"
                isRegex -> "!~"
                isEqual -> "="
                else -> "!="
            }
            return "$name$op\"$value\""
        }
}

@Serializable
data class AmAlert(
    val fingerprint: String = "",
    val alertname: String = "",
    val severity: String = AmSeverity.OTHER,
    val labels: Map<String, String> = emptyMap(),
    val annotations: Map<String, String> = emptyMap(),
    val summary: String = "",
    val description: String = "",
    val runbookURL: String = "",
    val generatorURL: String = "",
    val startsAt: Long = 0,
    val endsAt: Long = 0,
    val updatedAt: Long = 0,
    val receivers: List<String> = emptyList(),
    val state: String = AmState.ACTIVE,
    val silencedBy: List<String> = emptyList(),
    val inhibitedBy: List<String> = emptyList(),
) {
    val suppressed: Boolean get() = state == AmState.SUPPRESSED
    val silenced: Boolean get() = silencedBy.isNotEmpty()
    val inhibited: Boolean get() = inhibitedBy.isNotEmpty()
}

@Serializable
data class AmGroup(
    val alertname: String = "",
    val severity: String = AmSeverity.OTHER,
    val count: Int = 0,
    val active: Int = 0,
    val alerts: List<AmAlert> = emptyList(),
)

/** The overview card's figures: non-suppressed alerts by severity, plus the suppressed ones. */
@Serializable
data class AmCounts(
    val critical: Int = 0,
    val warning: Int = 0,
    val info: Int = 0,
    val other: Int = 0,
    val suppressed: Int = 0,
) {
    val firing: Int get() = critical + warning + info + other
}

@Serializable
data class AmAlerts(
    val groups: List<AmGroup> = emptyList(),
    val counts: AmCounts = AmCounts(),
    val total: Int = 0,
    val truncated: Boolean = false,
)

@Serializable
data class AmSilence(
    val id: String = "",
    val state: String = AmSilenceState.ACTIVE,
    val matchers: List<AmMatcher> = emptyList(),
    val createdBy: String = "",
    val comment: String = "",
    val startsAt: Long = 0,
    val endsAt: Long = 0,
    val updatedAt: Long = 0,
)

@Serializable
data class AmSilences(val silences: List<AmSilence> = emptyList())

/** A cluster's Alertmanager setup; [source] null until one is found or chosen. */
@Serializable
data class AlertmanagerConfig(val source: PromSource? = null)

/** What the alerts list shows: the states, the severities (empty: all) and a text. */
data class AmFilter(
    val active: Boolean = true,
    val silenced: Boolean = false,
    val inhibited: Boolean = false,
    val severities: Set<String> = emptySet(),
    val query: String = "",
)

/** Whether [alert] passes the state and severity choices of [filter] and contains its text. */
fun AmFilter.matches(alert: AmAlert): Boolean {
    val stateShown = when {
        !alert.suppressed -> active
        else -> silenced && alert.silenced || inhibited && alert.inhibited
    }
    if (!stateShown) return false
    if (severities.isNotEmpty() && alert.severity !in severities) return false
    val needle = query.trim()
    if (needle.isEmpty()) return true
    return alert.alertname.contains(needle, ignoreCase = true) ||
        alert.summary.contains(needle, ignoreCase = true) ||
        alert.labels.any { (k, v) -> k.contains(needle, ignoreCase = true) || v.contains(needle, ignoreCase = true) }
}

/** The groups narrowed to the alerts [filter] shows, the empty ones dropped. */
fun AmAlerts.filtered(filter: AmFilter): List<AmGroup> = groups.mapNotNull { group ->
    val shown = group.alerts.filter(filter::matches)
    if (shown.isEmpty()) null else group.copy(alerts = shown)
}

/**
 * Where an alert points in the cluster, from its labels: a pod (`pod` + `namespace`), a
 * namespace, or a node (`node`, else `instance` without its port, when one of [nodes] has that
 * name). Null fields: nothing to open.
 */
data class AmObjectLinks(val pod: String? = null, val namespace: String? = null, val node: String? = null)

fun AmAlert.objectLinks(nodes: Collection<String>): AmObjectLinks {
    val namespace = labels["namespace"]?.takeIf { it.isNotBlank() }
    val pod = labels["pod"]?.takeIf { it.isNotBlank() && namespace != null }
    val candidates = listOfNotNull(labels["node"], labels["instance"]?.let(::instanceHost))
    val node = candidates.firstOrNull { it.isNotBlank() && it in nodes }
    return AmObjectLinks(pod = pod, namespace = namespace, node = node)
}

/** "10.0.0.5:9100" → "10.0.0.5", "[fd00::1]:9100" → "fd00::1", "worker-1" stays. */
private fun instanceHost(instance: String): String {
    if (instance.startsWith("[")) return instance.substringAfter('[').substringBefore(']')
    return if (instance.count { it == ':' } == 1) instance.substringBefore(':') else instance
}

/** A short "where" for an alert: namespace/pod, the instance, the node, the namespace, or "". */
val AmAlert.where: String
    get() {
        val namespace = labels["namespace"].orEmpty()
        val pod = labels["pod"].orEmpty()
        return when {
            namespace.isNotEmpty() && pod.isNotEmpty() -> "$namespace/$pod"
            labels["node"].orEmpty().isNotEmpty() -> labels.getValue("node")
            labels["instance"].orEmpty().isNotEmpty() -> labels.getValue("instance")
            else -> namespace
        }
    }

/** The silence lengths offered, in minutes: 1 hour, 4 hours, 1 day, 1 week. */
val AM_SILENCE_PRESETS = listOf(60L, 4 * 60L, 24 * 60L, 7 * 24 * 60L)

/** The longest silence Alertmanager is asked for here (30 days, as the Go core checks). */
const val AM_MAX_SILENCE_MINUTES = 30 * 24 * 60L
