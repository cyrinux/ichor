package name.levis.talosmobile.model

import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/** Members that can serve a snapshot: queried successfully and reporting no errors. */
fun snapshotCandidates(statuses: List<EtcdNodeStatus>): List<EtcdNodeStatus> =
    statuses.filter { it.error == null && it.errors.isEmpty() && it.memberId.isNotEmpty() }
        // Followers first (the snapshot loads the member serving it), learners and the leader last.
        .sortedWith(compareBy<EtcdNodeStatus> { it.isLeader }.thenBy { it.isLearner }.thenBy { it.node })

/** Default member for a snapshot: a healthy follower, else any healthy member, else none. */
fun chooseSnapshotMember(statuses: List<EtcdNodeStatus>): EtcdNodeStatus? = snapshotCandidates(statuses).firstOrNull()

/** `etcd-<context>-<hostname>-<yyyyMMdd-HHmm>.snapshot`, with unsafe file name characters replaced. */
fun snapshotFileName(context: String, hostname: String, now: Date = Date(), zone: TimeZone = TimeZone.getDefault()): String {
    val stamp = SimpleDateFormat("yyyyMMdd-HHmm", Locale.ROOT).apply { timeZone = zone }.format(now)
    fun safe(part: String) = part.replace(Regex("[^A-Za-z0-9._-]+"), "-").trim('-').ifEmpty { "unknown" }
    return "etcd-${safe(context)}-${safe(hostname)}-$stamp.snapshot"
}
