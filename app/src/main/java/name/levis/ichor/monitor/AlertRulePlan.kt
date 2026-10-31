package name.levis.ichor.monitor

import name.levis.ichor.model.ALERT_ACTION_DIGEST
import name.levis.ichor.model.ALERT_ACTION_MUTE
import name.levis.ichor.model.ALERT_REASON_QUIET
import name.levis.ichor.model.ALERT_REASON_SEVERITY
import name.levis.ichor.model.ALERT_SEVERITY_CRITICAL
import name.levis.ichor.model.ALERT_SEVERITY_INFO
import name.levis.ichor.model.ALERT_SEVERITY_WARNING
import name.levis.ichor.model.AlertDecisions
import name.levis.ichor.model.AlertDigest
import name.levis.ichor.model.AlertInput
import name.levis.ichor.model.ContextSummary

/**
 * The kind Go reads for an alert's rule: "node", "etcd" and "unreachable" are critical when
 * [ruleSeverity] gives none (Go's default by kind); the others are only named.
 */
val Alert.ruleKind: String
    get() = when (kind) {
        AlertKind.NODE_READY, AlertKind.NODE_NOT_READY, AlertKind.NODE_UNREACHABLE -> "node"
        AlertKind.ETCD_ALARM -> "etcd"
        AlertKind.CLUSTER_UNREACHABLE, AlertKind.CLUSTER_REACHABLE -> "unreachable"
        AlertKind.CERT_EXPIRING, AlertKind.CERT_EXPIRED -> "cert"
        AlertKind.KUBECONFIG_EXPIRING, AlertKind.KUBECONFIG_EXPIRED -> "kubeconfig"
        AlertKind.DATA_PROBLEM, AlertKind.DATA_OK -> "data"
        AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK -> "gitops"
        AlertKind.CHECKUP_PROBLEM, AlertKind.CHECKUP_OK -> "checkup"
        AlertKind.AM_FIRING, AlertKind.AM_RESOLVED -> "am"
        AlertKind.STORAGE_PROBLEM, AlertKind.STORAGE_OK -> "storage"
        AlertKind.STORAGE_TREND, AlertKind.STORAGE_TREND_OK -> "trend"
        AlertKind.SENSOR_PROBLEM, AlertKind.SENSOR_OK -> "sensor"
        AlertKind.KUBESPAN_DOWN, AlertKind.KUBESPAN_UP -> "kubespan"
        AlertKind.PLUGIN_PROBLEM, AlertKind.PLUGIN_OK -> "plugin"
        AlertKind.PROPOSAL_MERGED, AlertKind.PROPOSAL_CLOSED -> "proposal"
        AlertKind.SIGN_IN_REQUIRED -> "signin"
    }

/**
 * How bad an alert is for the minimum severity and "except critical", from what its detail
 * keeps (the same helpers the tracks diff on): "" for nodes, etcd and an unreachable cluster
 * (Go makes them critical), an Alertmanager alert's own label (info included), a pull request
 * info, an expired certificate critical.
 */
val Alert.ruleSeverity: String
    get() = when (kind) {
        AlertKind.NODE_READY, AlertKind.NODE_NOT_READY, AlertKind.NODE_UNREACHABLE, AlertKind.ETCD_ALARM,
        AlertKind.CLUSTER_UNREACHABLE, AlertKind.CLUSTER_REACHABLE,
        -> ""
        AlertKind.CERT_EXPIRED, AlertKind.KUBECONFIG_EXPIRED -> ALERT_SEVERITY_CRITICAL
        AlertKind.CERT_EXPIRING, AlertKind.KUBECONFIG_EXPIRING, AlertKind.SIGN_IN_REQUIRED -> ALERT_SEVERITY_WARNING
        AlertKind.DATA_PROBLEM, AlertKind.DATA_OK -> detail.substringAfter('|')
        AlertKind.GITOPS_PROBLEM, AlertKind.GITOPS_OK -> GitOpsDetail.parse(detail).severity
        AlertKind.CHECKUP_PROBLEM, AlertKind.CHECKUP_OK -> detail.substringAfterLast('|')
        AlertKind.AM_FIRING, AlertKind.AM_RESOLVED -> AmDetail.parse(detail).severity.lowercase()
        AlertKind.STORAGE_PROBLEM, AlertKind.STORAGE_OK -> storageSeverity(detail)
        AlertKind.STORAGE_TREND, AlertKind.STORAGE_TREND_OK ->
            if (TrendDetail.parse(detail).critical) ALERT_SEVERITY_CRITICAL else ALERT_SEVERITY_WARNING
        AlertKind.SENSOR_PROBLEM, AlertKind.SENSOR_OK -> sensorSeverity(detail)
        AlertKind.KUBESPAN_DOWN, AlertKind.KUBESPAN_UP -> kubespanSeverity(detail)
        AlertKind.PLUGIN_PROBLEM, AlertKind.PLUGIN_OK -> pluginSeverity(detail)
        AlertKind.PROPOSAL_MERGED, AlertKind.PROPOSAL_CLOSED -> ALERT_SEVERITY_INFO
    }

/** Every alert of a run, in order, as Go reads it; [snoozed] says whether a cluster's (fingerprint) key is snoozed. */
fun alertInputs(clusters: List<ClusterAlerts>, snoozed: (cluster: String, key: String) -> Boolean): List<AlertInput> =
    clusters.flatMap { group ->
        val fingerprint = group.context.fingerprint
        group.alerts.map { alert ->
            AlertInput(
                cluster = fingerprint,
                key = alert.key,
                kind = alert.ruleKind,
                severity = alert.ruleSeverity,
                problem = alert.problem,
                snoozed = snoozed(fingerprint, alert.key),
            )
        }
    }

/** An alert the rules kept quiet: [reason] quiet or severity, [until] the quiet window's end (unix ms). */
data class MutedAlert(val context: ContextSummary, val alert: Alert, val reason: String, val until: Long)

/** One digest notification: [digest] for the cluster [context], on the [channel] of its first alert. */
data class DigestPost(val context: ContextSummary, val digest: AlertDigest, val channel: AlertChannel)

/** What a run posts: the alerts on their own, the ones only recorded, and one digest per digest cluster. */
data class AlertPlan(
    val notify: List<ClusterAlerts>,
    val muted: List<MutedAlert> = emptyList(),
    val digests: List<DigestPost> = emptyList(),
)

/**
 * What to post of [clusters] given Go's [decisions] over [inputs] (see [alertInputs], same
 * order). Null decisions (the rules could not be applied) post every alert not snoozed, as
 * before the rules: an error never drops an alert. So does an alert Go gave no decision for.
 * A snoozed one is dropped either way; one muted by quiet hours or the minimum severity is
 * kept in [AlertPlan.muted] for the history.
 */
fun alertPlan(clusters: List<ClusterAlerts>, inputs: List<AlertInput>, decisions: AlertDecisions?): AlertPlan {
    val notify = mutableListOf<ClusterAlerts>()
    val muted = mutableListOf<MutedAlert>()
    val digested = linkedMapOf<String, ClusterAlerts>()
    var index = 0
    clusters.forEach { group ->
        val posted = mutableListOf<Alert>()
        group.alerts.forEach { alert ->
            val input = inputs.getOrNull(index)
            val decision = decisions?.decisions?.getOrNull(index)?.takeIf { input != null && it.cluster == input.cluster && it.key == input.key }
            index++
            when {
                decision == null -> {
                    if (input?.snoozed != true) posted.add(alert)
                }
                decision.action == ALERT_ACTION_MUTE -> {
                    if (decision.reason == ALERT_REASON_QUIET || decision.reason == ALERT_REASON_SEVERITY) {
                        muted.add(MutedAlert(group.context, alert, decision.reason, decision.until))
                    }
                }
                decision.action == ALERT_ACTION_DIGEST -> {
                    val fingerprint = group.context.fingerprint
                    val kept = digested[fingerprint]
                    digested[fingerprint] = kept?.copy(alerts = kept.alerts + alert) ?: ClusterAlerts(group.context, listOf(alert))
                }
                else -> posted.add(alert)
            }
        }
        if (posted.isNotEmpty()) notify += ClusterAlerts(group.context, posted)
    }
    val groups = decisions?.digests.orEmpty().associateBy { it.cluster }
    val digests = digested.mapNotNull { (fingerprint, group) ->
        groups[fingerprint]?.let { DigestPost(group.context, it, group.alerts.first().channel) }
    }
    // Sent to a digest Go did not count: posted on their own rather than lost.
    val orphans = digested.filterKeys { it !in groups }.values
    return AlertPlan(notify + orphans, muted, digests)
}
