package name.levis.ichor.monitor

import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.ClusterStorageHealth
import name.levis.ichor.model.HISTORY_NOT_READY
import name.levis.ichor.model.HISTORY_READY
import name.levis.ichor.model.HISTORY_UNREACHABLE
import name.levis.ichor.model.HistoryAlertEntry
import name.levis.ichor.model.HistoryNodeEntry
import name.levis.ichor.model.HistoryRecord
import name.levis.ichor.model.HistoryVolumeEntry
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.util.daysUntil

/**
 * What a check read that its snapshot does not keep, for the history ring: each node's version
 * and memory use (by node key), and every volume's fill ([volumes] null when storage was not read).
 */
data class HistoryDetail(
    val versions: Map<String, String> = emptyMap(),
    val memUsedPercent: Map<String, Double> = emptyMap(),
    val volumes: List<HistoryVolumeEntry>? = null,
)

/** A Talos cluster's: versions and memory of the nodes that answered, volumes of [storage]. */
fun historyDetailOf(overview: ClusterOverview, storage: ClusterStorageHealth?): HistoryDetail {
    val answered = overview.nodes.filter { it.reachable && it.lastSeen == null }
    return HistoryDetail(
        versions = answered.filter { it.version.isNotBlank() }.associate { it.node to it.version },
        memUsedPercent = answered.filter { it.memTotal > 0 }
            .associate { it.node to 100.0 * (it.memTotal - it.memAvailable) / it.memTotal },
        volumes = storage?.let(::historyVolumesOf),
    )
}

/** A kubeconfig cluster's: the kubelet version of each node (no memory use, no volumes). */
fun historyDetailOf(nodes: KubeNodesOverview): HistoryDetail =
    HistoryDetail(versions = nodes.nodes.filter { it.kubelet.isNotBlank() }.associate { it.name to it.kubelet })

/** Every volume read, not only those over a threshold; a node that did not answer has none. */
fun historyVolumesOf(storage: ClusterStorageHealth): List<HistoryVolumeEntry> =
    storage.nodes.filter { it.error == null }.flatMap { node ->
        node.volumes.map { HistoryVolumeEntry(it.key, it.name, node.node, it.usedPercent) }
    }

/**
 * The record of each cluster checked in a run, by fingerprint, for its history ring: from the
 * check's real values, so none while screenshot mode is (or was, during the run) on, since every
 * name read then is a fake. A skipped cluster (VPN-only, no VPN) and one without a fingerprint
 * have none. [before] and [after] are the monitor's state around the run.
 */
fun historyRecords(before: MonitorState, reads: List<ClusterRead>, after: MonitorState, now: Long, masked: Boolean): Map<String, HistoryRecord> {
    if (masked) return emptyMap()
    return reads.filter { !it.skipped && it.context.fingerprint.isNotBlank() }.associate { read ->
        val key = monitorKeyOf(read.context)
        read.context.fingerprint to historyRecordOf(read, before.clusters[key], after.clusters[key], now)
    }
}

/**
 * One cluster's record: its nodes (with what [ClusterRead.detail] adds), and the issues still
 * open after the run from [kept], the snapshot the run kept (an unreadable track keeps its
 * known issues there, so they are sent again). A cluster that did not answer is a gap: not
 * reachable, its known issues sent again.
 */
fun historyRecordOf(read: ClusterRead, previous: ClusterSnapshot?, kept: ClusterSnapshot?, now: Long): HistoryRecord {
    val snapshot = read.snapshot
    val detail = read.detail ?: HistoryDetail()
    val alerts = kept?.let { openAlertsOf(it, previous, now) }.orEmpty()
    if (snapshot == null) return HistoryRecord(at = now, reachable = false, alerts = alerts)
    val nodes = snapshot.nodes.map { (node, state) ->
        HistoryNodeEntry(
            node = node,
            hostname = state.hostname,
            health = state.health.historyName,
            version = detail.versions[node],
            memUsedPercent = detail.memUsedPercent[node],
        )
    }
    return HistoryRecord(
        at = now,
        reachable = !snapshot.unreachableAsAWhole,
        nodes = nodes,
        volumes = detail.volumes.orEmpty(),
        alerts = alerts,
    )
}

private val NodeHealth.historyName: String
    get() = when (this) {
        NodeHealth.READY -> HISTORY_READY
        NodeHealth.NOT_READY -> HISTORY_NOT_READY
        NodeHealth.UNREACHABLE -> HISTORY_UNREACHABLE
    }

/**
 * The issues open in [kept], every track, keyed like their notifications ("data:…", "am:…").
 * Etcd alarms that could not be listed this time are [previous]'s; the certificate counts
 * while it is in its warning days (or expired).
 */
fun openAlertsOf(kept: ClusterSnapshot, previous: ClusterSnapshot?, now: Long): List<HistoryAlertEntry> = buildList {
    val etcd = if (kept.etcdChecked) {
        kept.etcdAlarms
    } else {
        previous?.takeIf { it.context == kept.context && it.etcdChecked }?.etcdAlarms.orEmpty()
    }
    etcd.forEach { add(HistoryAlertEntry("etcd:$it", "etcd", DATA_CRITICAL, it.substringAfter(':'))) }
    kept.dataIssues.forEach { (key, severity) -> add(HistoryAlertEntry("data:$key", "data", severity, key.substringAfter('|'))) }
    kept.gitopsIssues.forEach { (key, value) ->
        add(HistoryAlertEntry("gitops:$key", "gitops", gitopsSeverity(value), key.substringAfter('|')))
    }
    kept.checkupIssues.forEach { (key, severity) ->
        add(HistoryAlertEntry("checkup:$key", "checkup", severity, key.split('|', limit = 3).getOrElse(2) { "" }))
    }
    kept.amIssues.forEach { (key, value) ->
        val am = AmDetail.parse(value)
        add(HistoryAlertEntry("am:$key", "am", amSeverity(value), am.alertname))
    }
    kept.storageIssues.forEach { (key, value) ->
        val storage = StorageDetail.parse(value)
        add(HistoryAlertEntry("storage:$key", "storage", storageSeverity(value), "${storage.hostname} ${storage.name}".trim()))
    }
    if (kept.certNotAfter > 0) {
        val days = daysUntil(kept.certNotAfter, now)
        if (days <= CERT_WARN_DAYS) add(HistoryAlertEntry("cert", "cert", if (days < 0) DATA_CRITICAL else DATA_WARNING))
    }
}
