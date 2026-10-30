package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * The alert rules the user sets (Settings > Monitoring > Rules), as Ichorgo.alertRulesApply
 * reads them: a [default] rule and per-cluster overrides by context fingerprint. A field left
 * null takes the default's. Stored, backed up and sent to Go as this JSON; iOS keeps the same.
 */
@Serializable
data class AlertRules(
    val default: AlertRule = AlertRule(),
    val clusters: Map<String, AlertRule> = emptyMap(),
) {
    /** The override of [fingerprint], or an empty one (every field as default). */
    fun clusterRule(fingerprint: String): AlertRule = clusters[fingerprint] ?: AlertRule()

    /** With [fingerprint]'s override replaced by [rule]; an empty one is dropped. */
    fun withCluster(fingerprint: String, rule: AlertRule): AlertRules =
        copy(clusters = if (rule.isEmpty) clusters - fingerprint else clusters + (fingerprint to rule))

    /** Only the overrides of the clusters in [fingerprints]: a removed cluster's go with it. */
    fun retain(fingerprints: Collection<String>): AlertRules = copy(clusters = clusters.filterKeys { it in fingerprints })

    /**
     * Every quiet-hours window without a zone (the phone's, as the settings store it) given
     * [zone], the phone's IANA zone now: Go has no local zone of its own.
     */
    fun withZone(zone: String): AlertRules = AlertRules(
        default = default.withZone(zone),
        clusters = clusters.mapValues { (_, rule) -> rule.withZone(zone) },
    )

    companion object {
        val NONE = AlertRules()
    }
}

/**
 * One rule. [minSeverity]: [ALERT_SEVERITY_ALL], [ALERT_SEVERITY_WARNING] or
 * [ALERT_SEVERITY_CRITICAL]. [repeatMinutes]: how often a still-firing critical alert is posted
 * again until acknowledged, 0 off (see [ALERT_REPEAT_CHOICES]).
 */
@Serializable
data class AlertRule(
    val quietHours: QuietHours? = null,
    val minSeverity: String? = null,
    val digest: Boolean? = null,
    val repeatMinutes: Int? = null,
) {
    val isEmpty: Boolean get() = quietHours == null && minSeverity == null && digest == null && repeatMinutes == null

    fun withZone(zone: String): AlertRule = copy(quietHours = quietHours?.let { if (it.tz.isBlank()) it.copy(tz = zone) else it })
}

/**
 * A daily window, "HH:MM" (24 h) wall-clock times; it may cross midnight. [start] == [end] is no
 * window (how a cluster turns off the default's). [tz] "" is the phone's zone, filled in at check
 * time. [exceptCritical]: critical alerts (node down, etcd, unreachable) still notify.
 */
@Serializable
data class QuietHours(
    val start: String = "",
    val end: String = "",
    val tz: String = "",
    val exceptCritical: Boolean = false,
) {
    val isOff: Boolean get() = start == end

    companion object {
        /** What turning quiet hours on starts from. */
        val NIGHT = QuietHours(start = "23:00", end = "07:00", tz = "", exceptCritical = true)
        val OFF = QuietHours()
    }
}

const val ALERT_SEVERITY_ALL = "all"
const val ALERT_SEVERITY_INFO = "info"
const val ALERT_SEVERITY_WARNING = "warning"
const val ALERT_SEVERITY_CRITICAL = "critical"

/** The repeats the settings offer, in minutes, 0 off; the real cadence is the check's. */
val ALERT_REPEAT_CHOICES = listOf(0, 5, 15, 30, 60)

/**
 * One alert a check is about to post, as Go reads it; [cluster] is the context fingerprint.
 * [ongoing]: it did not change, it still fires since it was posted at [lastNotifiedMillis]
 * (first seen at [firstSeenMillis]); Go only says whether to post it again.
 */
@Serializable
data class AlertInput(
    val cluster: String,
    val key: String,
    val kind: String,
    val severity: String = "",
    val problem: Boolean,
    val snoozed: Boolean = false,
    val ongoing: Boolean = false,
    val firstSeenMillis: Long = 0,
    val lastNotifiedMillis: Long = 0,
    val acknowledged: Boolean = false,
)

const val ALERT_ACTION_NOTIFY = "notify"
const val ALERT_ACTION_MUTE = "mute"
const val ALERT_ACTION_DIGEST = "digest"
const val ALERT_ACTION_REPEAT = "repeat"

const val ALERT_REASON_SNOOZED = "snoozed"
const val ALERT_REASON_SEVERITY = "severity"
const val ALERT_REASON_QUIET = "quiet"
const val ALERT_REASON_DIGEST = "digest"

/**
 * What to do with one alert; [until] (unix ms) the end of the quiet window that muted it, else
 * 0. [repeats]: posted, it will be posted again while it fires, until acknowledged.
 */
@Serializable
data class AlertDecision(
    val cluster: String,
    val key: String,
    val action: String,
    val reason: String = "",
    val until: Long = 0,
    val repeats: Boolean = false,
)

/** A digest cluster's alerts of the run: how many, how many critical and warnings, and the first keys. */
@Serializable
data class AlertDigest(
    val cluster: String,
    val count: Int,
    val critical: Int = 0,
    val warning: Int = 0,
    val firstKeys: List<String> = emptyList(),
)

/** Ichorgo.alertRulesApply's answer: one decision per alert in order, one digest per digest cluster. */
@Serializable
data class AlertDecisions(
    val decisions: List<AlertDecision> = emptyList(),
    val digests: List<AlertDigest> = emptyList(),
)

/** The rules and the answer exactly as Go writes them: an unknown key is an error, not a silent default. */
val AlertRulesJson = Json {
    ignoreUnknownKeys = false
    explicitNulls = false
}
