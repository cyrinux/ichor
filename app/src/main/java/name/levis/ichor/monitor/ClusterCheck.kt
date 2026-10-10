package name.levis.ichor.monitor

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.withTimeoutOrNull
import name.levis.ichor.TalosApp
import name.levis.ichor.data.AlertmanagerRepository
import name.levis.ichor.data.DataServicesRepository
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.data.GoCall
import name.levis.ichor.data.KubeRepository
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.sourceFor
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.isKube

/** How long one cluster's check may take: past it, the cluster counts as unreachable for this run. */
const val CLUSTER_CHECK_TIMEOUT_MS = 45_000L

/** Where a cluster's state is kept: its context fingerprint (the context name when there is none). */
fun monitorKeyOf(context: ContextSummary): String = context.fingerprint.ifBlank { context.name }

/** The repositories of one cluster's check, every call to [context] whichever cluster is on screen. */
private class PinnedReads(app: TalosApp, context: String) {
    // Nothing kept offline: what a background check reads is not what a screen shows.
    private val go = GoCall(app.configRepository, app.kubeServers, offline = null, pinned = context)
    val talos = TalosRepository(go)
    val kube = KubeRepository(go)
    val gitops = GitOpsRepository(go)
    val data = DataServicesRepository(go)
    val alertmanager = AlertmanagerRepository(go)
}

// The Go calls block and cannot be interrupted: a check past its time is left to finish here,
// its result unused, so it never holds the other clusters back.
private val checks = CoroutineScope(SupervisorJob() + Dispatchers.IO)

/** [read] within [CLUSTER_CHECK_TIMEOUT_MS], else null (as for a cluster that could not be read). */
suspend fun <T : Any> readWithin(timeoutMs: Long = CLUSTER_CHECK_TIMEOUT_MS, read: suspend () -> T?): T? {
    val running = checks.async { runCatching { read() }.getOrNull() }
    return withTimeoutOrNull(timeoutMs) { running.await() } ?: run {
        running.cancel()
        null
    }
}

/** A check's fresh [snapshot], and what it read that only the history ring keeps. */
data class ClusterReading(val snapshot: ClusterSnapshot, val detail: HistoryDetail)

/**
 * Overview + etcd of the cluster of [context] (a cluster added from a kubeconfig: its Kubernetes
 * node list), plus the opt-in tracks, as a fresh snapshot taken at [now]; [prev] is its last one,
 * whose known issues a part that could not be read keeps. Null when the cluster could not be read
 * at all (off VPN, a sign-in needed, down).
 */
suspend fun readCluster(app: TalosApp, context: ContextSummary, prev: ClusterSnapshot?, now: Long): ClusterReading? {
    val store = app.monitorStore
    val reads = PinnedReads(app, context.name)
    val kube = context.isKube
    val overview = if (kube) null else runCatching { reads.talos.overview() }.getOrNull()
    val kubeNodes = if (kube) runCatching { reads.kube.kubeNodes() }.getOrNull() else null
    if (overview == null && kubeNodes == null) return null
    val etcd = if (kube) null else runCatching { reads.talos.etcd() }.getOrNull()
    val name = context.name
    val fingerprint = context.fingerprint

    // Opt-in, and only for roles that may use the Kubernetes API: each check then lists custom
    // resources and runs the Garage CLI in a pod.
    val workloads = context.allows(Feature.WORKLOADS)
    val watchData = store.dataServicesWatched.value && workloads
    val dataServices = if (watchData) runCatching { reads.data.dataServices(hints = "") }.getOrNull() else null

    // Same for Argo CD and Flux apps: each call answers installed=false quickly when absent. A
    // part that could not be read keeps its known issues, so they neither clear falsely nor
    // hold the other part's alerts back.
    val watchGitops = store.gitopsWatched.value && workloads
    val gitopsIssues = if (watchGitops) {
        val argo = runCatching { reads.gitops.argoCD() }.getOrNull()
        val flux = runCatching { reads.gitops.flux() }.getOrNull()
        gitopsIssuesWithGaps(argo, flux, known = knownGitOpsIssues(prev, name))
    } else {
        null
    }

    // And for the checkup: it lists the cluster's pods and asks every kubelet. A section that
    // could not be read keeps its known findings.
    val watchCheckup = store.checkupWatched.value && workloads
    val checkupIssues = if (watchCheckup) {
        runCatching { reads.kube.checkup() }.getOrNull()
            ?.let { checkupIssuesWithGaps(it, known = knownCheckupIssues(prev, name)) }
    } else {
        null
    }

    // And for the Alertmanager: the one chosen for the cluster, else the likeliest found (a
    // Service list). Only alerts neither silenced nor inhibited; none found or unreadable
    // keeps what was known, and so does a truncated read for the alerts past its cut.
    val watchAm = store.alertmanagerWatched.value && workloads
    val amIssues = if (watchAm) {
        runCatching {
            reads.alertmanager.sourceFor(app.alertmanagerStore, fingerprint)
                ?.let { reads.alertmanager.alerts(it, active = true, silenced = false, inhibited = false) }
                ?.let { amIssuesWithGaps(it, known = knownAmIssues(prev, name)) }
        }.getOrNull()
    } else {
        null
    }

    // And for node storage: one Talos call reads every node's volumes and SMART (none for a
    // cluster added from a kubeconfig). A node that does not answer keeps its known issues.
    val watchStorage = store.storageWatched.value && !kube
    val storageHealth = if (watchStorage) runCatching { reads.talos.storageHealth() }.getOrNull() else null
    val storageIssues = if (watchStorage) {
        storageHealth?.let {
            storageIssuesOf(
                it,
                warn = store.storageWarnPercent.value,
                critical = store.storageCriticalPercent.value,
                known = knownStorageIssues(prev, name),
            )
        }
    } else {
        null
    }

    return if (kubeNodes != null) {
        val snapshot = kubeSnapshotOf(
            kubeNodes, name, context.certNotAfter, now, fingerprint, watchData, dataServices,
            gitopsWatched = watchGitops,
            gitopsIssues = gitopsIssues,
            checkupWatched = watchCheckup,
            checkupIssues = checkupIssues,
            amWatched = watchAm,
            amIssues = amIssues,
        )
        ClusterReading(snapshot, historyDetailOf(kubeNodes))
    } else {
        val talos = checkNotNull(overview)
        val snapshot = snapshotOf(
            talos, etcd, context.certNotAfter, now, fingerprint, watchData, dataServices,
            gitopsWatched = watchGitops,
            gitopsIssues = gitopsIssues,
            checkupWatched = watchCheckup,
            checkupIssues = checkupIssues,
            amWatched = watchAm,
            amIssues = amIssues,
            storageWatched = watchStorage,
            storageIssues = storageIssues,
        )
        ClusterReading(snapshot, historyDetailOf(talos, storageHealth))
    }
}
