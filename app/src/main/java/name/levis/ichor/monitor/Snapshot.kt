package name.levis.ichor.monitor

import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.NodeHealth
import name.levis.ichor.model.health
import kotlinx.serialization.Serializable

/** What background monitoring last saw; drives both alerts (by diffing) and the widget. */
@Serializable
data class ClusterSnapshot(
    val context: String,
    val takenAt: Long,
    val nodes: Map<String, NodeState>,
    val etcdAlarms: List<String> = emptyList(),
    val etcdChecked: Boolean = false,
    val certNotAfter: Long = 0,
    /** Epoch day of the last certificate-expiry warning, so it fires at most once a day. */
    val lastCertWarnDay: Long = -1,
    /** The cluster's context fingerprint: the widget shows the name the user gave it. */
    val fingerprint: String = "",
    /** Watching Longhorn, Garage and CloudNativePG was on for this check (opt-in). */
    val dataWatched: Boolean = false,
    /** Their health could be read this time. */
    val dataChecked: Boolean = false,
    /**
     * Data-service issues ("system|label" → severity, see [dataIssuesOf]): what was observed in a
     * fresh snapshot; after [evaluate], the ones already notified (or present at the baseline).
     */
    val dataIssues: Map<String, String> = emptyMap(),
    /** Warnings seen once and not notified yet: a short rebuild after a reboot should not alert. */
    val dataPending: List<String> = emptyList(),
    /** Watching Argo CD and Flux apps was on for this check (opt-in). */
    val gitopsWatched: Boolean = false,
    /** Their apps could be read this time. */
    val gitopsChecked: Boolean = false,
    /**
     * GitOps app issues ("argocd|namespace/name" or "flux|Kind namespace/name" → "severity|reason",
     * see [gitopsIssuesOf]), kept like [dataIssues].
     */
    val gitopsIssues: Map<String, String> = emptyMap(),
    /** GitOps warnings seen once and not notified yet. */
    val gitopsPending: List<String> = emptyList(),
    /** Watching the cluster checkup was on for this check (opt-in). */
    val checkupWatched: Boolean = false,
    /** The checkup could be read this time. */
    val checkupChecked: Boolean = false,
    /**
     * Checkup findings ("section|kind|subject" → severity, see [checkupIssuesWithGaps]), kept like
     * [dataIssues].
     */
    val checkupIssues: Map<String, String> = emptyMap(),
    /** Checkup warnings seen once and not notified yet. */
    val checkupPending: List<String> = emptyList(),
    /** Watching the Alertmanager's alerts was on for this check (opt-in). */
    val amWatched: Boolean = false,
    /** Its alerts could be read this time (an Alertmanager was found and answered). */
    val amChecked: Boolean = false,
    /**
     * Firing Alertmanager alerts (fingerprint → "severity|alertname|where", see [amIssuesOf]),
     * kept like [dataIssues].
     */
    val amIssues: Map<String, String> = emptyMap(),
    /** Alertmanager warnings seen once and not notified yet. */
    val amPending: List<String> = emptyList(),
    /** Watching node storage (volume fill, SMART) was on for this check (opt-in, Talos clusters only). */
    val storageWatched: Boolean = false,
    /** Its health could be read this time. */
    val storageChecked: Boolean = false,
    /**
     * Volumes filling up and disks failing SMART ("node|volume" or "node|smart|device" → see
     * [StorageDetail]), kept like [dataIssues].
     */
    val storageIssues: Map<String, String> = emptyMap(),
    /** Storage warnings seen once and not notified yet. */
    val storagePending: List<String> = emptyList(),
    /**
     * Volumes whose fill trend alert is open ("node|volume" → [TrendDetail], see
     * [storageTrendStep]), set after the run's history record (see [withStorageTrends]).
     */
    val storageTrends: Map<String, String> = emptyMap(),
    /**
     * The cluster was added from a kubeconfig: its nodes come from the Kubernetes API (ready or
     * not, never unreachable), there is no etcd, and [certNotAfter] is the kubeconfig's credentials.
     */
    val kube: Boolean = false,
) {
    val readyCount: Int get() = nodes.values.count { it.health == NodeHealth.READY }
    val notReadyCount: Int get() = nodes.values.count { it.health == NodeHealth.NOT_READY }
    val unreachableCount: Int get() = nodes.values.count { it.health == NodeHealth.UNREACHABLE }

    /** No node answered: most likely the phone is off the cluster's network (VPN, home LAN). */
    val unreachableAsAWhole: Boolean get() = nodes.isNotEmpty() && unreachableCount == nodes.size
}

@Serializable
data class NodeState(
    val hostname: String,
    val health: NodeHealth,
    val reason: String = "",
)

fun snapshotOf(
    overview: ClusterOverview,
    etcd: EtcdOverview?,
    certNotAfter: Long,
    takenAt: Long,
    fingerprint: String = "",
    /** Data services watched ([dataServices] null when watched but unreadable). */
    dataWatched: Boolean = false,
    dataServices: DataServices? = null,
    /** GitOps apps watched ([gitopsIssues] null when watched but unreadable, see [gitopsIssuesOf]). */
    gitopsWatched: Boolean = false,
    gitopsIssues: Map<String, String>? = null,
    /** The checkup watched ([checkupIssues] null when watched but unreadable). */
    checkupWatched: Boolean = false,
    checkupIssues: Map<String, String>? = null,
    /** The Alertmanager watched ([amIssues] null when watched but unreadable or not found). */
    amWatched: Boolean = false,
    amIssues: Map<String, String>? = null,
    /** Node storage watched ([storageIssues] null when watched but unreadable, see [storageIssuesOf]). */
    storageWatched: Boolean = false,
    storageIssues: Map<String, String>? = null,
): ClusterSnapshot = ClusterSnapshot(
    storageWatched = storageWatched,
    storageChecked = storageWatched && storageIssues != null,
    storageIssues = storageIssues?.takeIf { storageWatched }.orEmpty(),
    amWatched = amWatched,
    amChecked = amWatched && amIssues != null,
    amIssues = amIssues?.takeIf { amWatched }.orEmpty(),
    checkupWatched = checkupWatched,
    checkupChecked = checkupWatched && checkupIssues != null,
    checkupIssues = checkupIssues?.takeIf { checkupWatched }.orEmpty(),
    gitopsWatched = gitopsWatched,
    gitopsChecked = gitopsWatched && gitopsIssues != null,
    gitopsIssues = gitopsIssues?.takeIf { gitopsWatched }.orEmpty(),
    dataWatched = dataWatched,
    dataChecked = dataWatched && dataServices != null,
    dataIssues = dataServices?.takeIf { dataWatched }?.let(::dataIssuesOf).orEmpty(),
    context = overview.context,
    fingerprint = fingerprint,
    takenAt = takenAt,
    nodes = overview.nodes.associate { n ->
        val reason = n.error ?: n.unmetConditions.joinToString("; ") { "${it.name}: ${it.reason}" }
        n.node to NodeState(n.hostname, n.health, reason)
    },
    etcdAlarms = etcd?.alarms?.map { "${it.memberId}:${it.alarm}" }.orEmpty().sorted(),
    // A failed alarm list is "not checked", never an all-clear.
    etcdChecked = etcd != null && etcd.error == null && etcd.alarmsError == null,
    certNotAfter = certNotAfter,
)

/**
 * The snapshot of a cluster added from a kubeconfig, from the nodes as the Kubernetes API lists
 * them: a node is ready or not (Kubernetes reports a lost kubelet as not ready), its pressure
 * conditions are the reason. No etcd. Credentials that may not list nodes give no node at all,
 * so the opt-in tracks still alert. The other parameters are [snapshotOf]'s.
 */
fun kubeSnapshotOf(
    nodes: KubeNodesOverview,
    context: String,
    certNotAfter: Long,
    takenAt: Long,
    fingerprint: String = "",
    dataWatched: Boolean = false,
    dataServices: DataServices? = null,
    gitopsWatched: Boolean = false,
    gitopsIssues: Map<String, String>? = null,
    checkupWatched: Boolean = false,
    checkupIssues: Map<String, String>? = null,
    amWatched: Boolean = false,
    amIssues: Map<String, String>? = null,
): ClusterSnapshot = ClusterSnapshot(
    kube = true,
    amWatched = amWatched,
    amChecked = amWatched && amIssues != null,
    amIssues = amIssues?.takeIf { amWatched }.orEmpty(),
    checkupWatched = checkupWatched,
    checkupChecked = checkupWatched && checkupIssues != null,
    checkupIssues = checkupIssues?.takeIf { checkupWatched }.orEmpty(),
    gitopsWatched = gitopsWatched,
    gitopsChecked = gitopsWatched && gitopsIssues != null,
    gitopsIssues = gitopsIssues?.takeIf { gitopsWatched }.orEmpty(),
    dataWatched = dataWatched,
    dataChecked = dataWatched && dataServices != null,
    dataIssues = dataServices?.takeIf { dataWatched }?.let(::dataIssuesOf).orEmpty(),
    context = context,
    fingerprint = fingerprint,
    takenAt = takenAt,
    nodes = nodes.nodes.associate { n ->
        n.name to NodeState(n.name, if (n.ready) NodeHealth.READY else NodeHealth.NOT_READY, n.pressure.joinToString("; "))
    },
    certNotAfter = certNotAfter,
)
