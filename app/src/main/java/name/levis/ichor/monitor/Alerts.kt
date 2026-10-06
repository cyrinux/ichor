package name.levis.ichor.monitor

import name.levis.ichor.model.NodeHealth
import name.levis.ichor.util.daysUntil

/** What an alert is about; Notifications.kt turns it into translated text. */
enum class AlertKind { NODE_READY, NODE_NOT_READY, NODE_UNREACHABLE, ETCD_ALARM, CERT_EXPIRING, CERT_EXPIRED, DATA_PROBLEM, DATA_OK, GITOPS_PROBLEM, GITOPS_OK, CHECKUP_PROBLEM, CHECKUP_OK }

/**
 * [key] identifies the subject, so a newer alert replaces the older notification.
 * [subject]: hostname (node), alarm name (etcd), volume/cluster (data services) or
 * "namespace/name" (Argo CD) / "Kind namespace/name" (Flux), or what a checkup finding is about;
 * [detail]: node address/reason, etcd member, "system|severity" (data services) or
 * "tool|severity|reason" (GitOps apps), "section|kind|severity" (checkup);
 * [days]: days until (or since) the certificate expiry.
 */
data class Alert(
    val key: String,
    val kind: AlertKind,
    val problem: Boolean,
    val subject: String = "",
    val detail: String = "",
    val days: Int = 0,
)

data class Evaluation(val alerts: List<Alert>, val next: ClusterSnapshot)

/** Days before expiry when the daily "renew your talosconfig" alert starts. */
const val CERT_WARN_DAYS = 7

/**
 * Compares the previous and current snapshots. Only *changes* alert, so a node that stays
 * down notifies once. The first snapshot (or a context switch) is a silent baseline.
 *
 * A check where no node answered says nothing about the cluster (the phone is off its
 * network): it alerts nothing and keeps the previous snapshot, so neither leaving nor
 * coming back notifies for every node.
 */
fun evaluate(prev: ClusterSnapshot?, cur: ClusterSnapshot, nowMillis: Long): Evaluation {
    val alerts = mutableListOf<Alert>()
    val comparable = prev != null && prev.context == cur.context && !prev.unreachableAsAWhole
    val blind = comparable && cur.unreachableAsAWhole

    if (comparable && !blind) {
        cur.nodes.forEach { (addr, state) ->
            val before = prev.nodes[addr] ?: return@forEach
            if (before.health != state.health) alerts += nodeAlert(addr, state)
        }
        if (cur.etcdChecked && prev.etcdChecked) {
            (cur.etcdAlarms - prev.etcdAlarms.toSet()).forEach { alarm ->
                alerts += Alert(
                    key = "etcd:$alarm",
                    kind = AlertKind.ETCD_ALARM,
                    problem = true,
                    subject = alarm.substringAfter(':'),
                    detail = alarm.substringBefore(':'),
                )
            }
        }
    }

    val today = Math.floorDiv(nowMillis, 86_400_000L)
    val lastWarn = prev?.lastCertWarnDay ?: -1
    var warnedDay = lastWarn
    if (cur.certNotAfter > 0 && lastWarn != today) {
        val days = daysUntil(cur.certNotAfter, nowMillis)
        if (days <= CERT_WARN_DAYS) {
            val kind = if (days < 0) AlertKind.CERT_EXPIRED else AlertKind.CERT_EXPIRING
            alerts += Alert("cert", kind, problem = true, days = kotlin.math.abs(days).toInt())
            warnedDay = today
        }
    }

    val data = if (blind) prev.dataTrack else evaluateTrack(prev?.dataTrack, cur.dataTrack, comparable, { it }, ::dataAlert, alerts)
    val gitops = if (blind) prev.gitopsTrack else evaluateTrack(prev?.gitopsTrack, cur.gitopsTrack, comparable, ::gitopsSeverity, ::gitopsAlert, alerts)
    val checkup = if (blind) prev.checkupTrack else evaluateTrack(prev?.checkupTrack, cur.checkupTrack, comparable, { it }, ::checkupAlert, alerts)

    val next = if (blind) prev.copy(certNotAfter = cur.certNotAfter) else cur
    return Evaluation(
        alerts,
        next.copy(
            lastCertWarnDay = warnedDay,
            dataWatched = data.watched,
            dataChecked = data.checked,
            dataIssues = data.issues,
            dataPending = data.pending,
            gitopsWatched = gitops.watched,
            gitopsChecked = gitops.checked,
            gitopsIssues = gitops.issues,
            gitopsPending = gitops.pending,
            checkupWatched = checkup.watched,
            checkupChecked = checkup.checked,
            checkupIssues = checkup.issues,
            checkupPending = checkup.pending,
        ),
    )
}

/** One opt-in track of issues (data services, GitOps apps), as a snapshot keeps it. */
private data class Track(val watched: Boolean, val checked: Boolean, val issues: Map<String, String>, val pending: List<String>)

private val ClusterSnapshot.dataTrack: Track get() = Track(dataWatched, dataChecked, dataIssues, dataPending)
private val ClusterSnapshot.gitopsTrack: Track get() = Track(gitopsWatched, gitopsChecked, gitopsIssues, gitopsPending)
private val ClusterSnapshot.checkupTrack: Track get() = Track(checkupWatched, checkupChecked, checkupIssues, checkupPending)

/**
 * Issues of one track (key → value, [severityOf] reading [DATA_CRITICAL] or [DATA_WARNING] from the
 * value), diffed like etcd alarms: a critical issue alerts at once, a warning only when seen on two
 * checks in a row; an issue that clears says so once. The first check (or a context switch, or
 * turning watching on) is a silent baseline; a check that could not read them keeps what was known;
 * turning watching off forgets it.
 */
private fun evaluateTrack(
    prev: Track?,
    cur: Track,
    comparable: Boolean,
    severityOf: (String) -> String,
    alertOf: (key: String, value: String, problem: Boolean) -> Alert,
    alerts: MutableList<Alert>,
): Track {
    if (!cur.watched) return Track(watched = false, checked = false, issues = emptyMap(), pending = emptyList())
    val known = prev?.takeIf { comparable && it.watched && it.checked }
    if (!cur.checked) {
        return known?.let { Track(true, true, it.issues, it.pending) } ?: Track(true, false, emptyMap(), emptyList())
    }
    if (known == null) return Track(true, true, cur.issues, emptyList())

    val notified = mutableMapOf<String, String>()
    val pending = mutableListOf<String>()
    cur.issues.forEach { (key, value) ->
        val severity = severityOf(value)
        val before = known.issues[key]?.let(severityOf)
        when {
            // Already notified at this severity or a worse one: quiet.
            before == severity || (before == DATA_CRITICAL && severity == DATA_WARNING) -> notified[key] = value
            // New or worse critical, or a warning seen for the second time in a row.
            severity == DATA_CRITICAL || key in known.pending -> {
                alerts += alertOf(key, value, true)
                notified[key] = value
            }
            else -> pending += key
        }
    }
    (known.issues.keys - cur.issues.keys).sorted().forEach { key ->
        alerts += alertOf(key, known.issues.getValue(key), false)
    }
    return Track(true, true, notified, pending)
}

/** [key] "tool|subject", [value] "severity|reason": detail "tool|severity|reason". */
private fun gitopsAlert(key: String, value: String, problem: Boolean): Alert = Alert(
    key = "gitops:$key",
    kind = if (problem) AlertKind.GITOPS_PROBLEM else AlertKind.GITOPS_OK,
    problem = problem,
    subject = key.substringAfter('|'),
    detail = key.substringBefore('|') + "|" + value,
)

/** [key] "section|kind|subject", [severity] the finding's: detail "section|kind|severity". */
private fun checkupAlert(key: String, severity: String, problem: Boolean): Alert {
    val parts = key.split('|', limit = 3)
    return Alert(
        key = "checkup:$key",
        kind = if (problem) AlertKind.CHECKUP_PROBLEM else AlertKind.CHECKUP_OK,
        problem = problem,
        subject = parts.getOrElse(2) { "" },
        detail = listOf(parts[0], parts.getOrElse(1) { "" }, severity).joinToString("|"),
    )
}

private fun dataAlert(key: String, severity: String, problem: Boolean): Alert = Alert(
    key = "data:$key",
    kind = if (problem) AlertKind.DATA_PROBLEM else AlertKind.DATA_OK,
    problem = problem,
    subject = key.substringAfter('|'),
    detail = key.substringBefore('|') + "|" + severity,
)

private fun nodeAlert(addr: String, state: NodeState): Alert {
    val reason = state.reason.ifBlank { addr }
    return when (state.health) {
        NodeHealth.READY -> Alert("node:$addr", AlertKind.NODE_READY, false, state.hostname, addr)
        NodeHealth.NOT_READY -> Alert("node:$addr", AlertKind.NODE_NOT_READY, true, state.hostname, reason)
        NodeHealth.UNREACHABLE -> Alert("node:$addr", AlertKind.NODE_UNREACHABLE, true, state.hostname, reason)
    }
}
