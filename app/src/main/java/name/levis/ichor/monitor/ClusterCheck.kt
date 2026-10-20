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
import name.levis.ichor.data.PluginRepository
import name.levis.ichor.i18n.AppLocale
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.sourceFor
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.isKube

/** How long one cluster's check may take: past it, the cluster counts as unreachable for this run. */
const val CLUSTER_CHECK_TIMEOUT_MS = 45_000L

/** How long a kubeconfig cluster's node disk read may take within a check: past it, it is unread. */
private const val NODE_DISK_TIMEOUT_MS = 30_000L

/** Where a cluster's state is kept: its context fingerprint (the context name when there is none). */
fun monitorKeyOf(context: ContextSummary): String = context.fingerprint.ifBlank { context.name }

/** The repositories of one cluster's check, every call to [context] whichever cluster is on screen. */
private class PinnedReads(app: TalosApp, context: String) {
    // Nothing kept offline: what a background check reads is not what a screen shows.
    private val go = GoCall(app.configRepository, app.kubeServers, offline = null, pinned = context, idle = app.maskSwitcher::awaitIdle)
    val talos = TalosRepository(go)
    val kube = KubeRepository(go)
    val gitops = GitOpsRepository(go)
    val data = DataServicesRepository(go)
    val alertmanager = AlertmanagerRepository(go)
    val plugins = PluginRepository(go)
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
    val reads = PinnedReads(app, context.name)
    val kube = context.isKube
    val overview = if (kube) null else runCatching { reads.talos.overview() }.getOrNull()
    val kubeNodes = if (kube) runCatching { reads.kube.kubeNodes() }.getOrNull() else null
    if (overview == null && kubeNodes == null) return null
    val store = app.monitorStore
    // A kubeconfig cluster's node disks (one kubelet read per node) run alongside the other
    // reads, so they cannot push the check past its time; failed or late, they are unread.
    val watchStorage = store.storageWatched.value
    val nodeDiskRead = if (watchStorage && kubeNodes != null) {
        checks.async { runCatching { reads.kube.nodeDisk() }.getOrNull() }
    } else {
        null
    }
    val etcd = if (kube) null else runCatching { reads.talos.etcd() }.getOrNull()
    val name = context.name
    val fingerprint = context.fingerprint

    // Opt-in, and only for roles that may use the Kubernetes API: each check then lists custom
    // resources and runs the Garage CLI in a pod.
    val workloads = context.allows(Feature.WORKLOADS)
    val watchData = store.dataServicesWatched.value && workloads

    // The CRD plugins' findings come with the data services: the same opt-in, for the same kind
    // of integration (the plugins render custom resources, most of them a Data services tab).
    // One call bounded by Go (10 s) reads them all, their words in the app's language, alongside
    // the other reads so it cannot push the check past its time.
    val watchPlugins = watchData
    val locale = AppLocale.language(app)
    val pluginRead = if (watchPlugins) {
        checks.async { runCatching { reads.plugins.findings(hints = "", locale = locale) }.getOrNull() }
    } else {
        null
    }
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

    // The plugins' findings: a plugin that could not be read keeps its known issues, a failed
    // call all of them. With the checkup watched, what it reports from a section of its own
    // (External Secrets) is left to it.
    val watchCheckup = store.checkupWatched.value && workloads
    val pluginIssues = pluginRead?.await()
        ?.let { pluginIssuesWithGaps(it, known = knownPluginIssues(prev, name), checkupWatched = watchCheckup) }

    // And for the checkup: it lists the cluster's pods and asks every kubelet. A section that
    // could not be read keeps its known findings. Its plugins section is left to the plugin
    // findings above when those are watched (their own words, a link to the plugin).
    val checkupIssues = if (watchCheckup) {
        runCatching { reads.kube.checkup(locale) }.getOrNull()?.let {
            checkupIssuesWithGaps(it, known = knownCheckupIssues(prev, name), gitopsWatched = watchGitops, pluginsWatched = watchPlugins)
        }
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

    // And for node storage: one Talos call reads every node's volumes and SMART; a cluster added
    // from a kubeconfig asks each node's kubelet for its disk (none when nodes/proxy is refused).
    // A node that does not answer keeps its known issues. The demo's disks are read (its history
    // shows them) but never open an issue: a demo never notifies.
    val storageWarn = store.storageWarnPercent.value
    val storageCritical = store.storageCriticalPercent.value
    val storageHealth = if (watchStorage && !kube) runCatching { reads.talos.storageHealth() }.getOrNull() else null
    val nodeDisk = nodeDiskRead?.let { read ->
        withTimeoutOrNull(NODE_DISK_TIMEOUT_MS) { read.await() } ?: run {
            read.cancel()
            null
        }
    }
    val storageIssues = when {
        !watchStorage -> null
        kubeNodes != null && context.isDemo -> nodeDisk?.let { emptyMap<String, String>() }
        kubeNodes != null -> nodeDisk?.let {
            kubeStorageIssuesOf(
                it,
                warn = storageWarn,
                critical = storageCritical,
                listed = kubeNodes.nodes.map { n -> n.name }.toSet(),
                known = knownStorageIssues(prev, name),
            )
        }
        else -> storageHealth?.let {
            storageIssuesOf(it, warn = storageWarn, critical = storageCritical, known = knownStorageIssues(prev, name))
        }
    }

    // And for node temperatures: one Talos call reads every node's hottest sensor (none for a
    // cluster added from a kubeconfig); nothing is read with the setting off.
    val watchSensors = store.sensorsWatched.value && !kube
    val sensorIssues = if (watchSensors) {
        runCatching { reads.talos.sensorHealth() }.getOrNull()?.let {
            sensorIssuesOf(it, store.sensorThreshold.value, known = knownSensorIssues(prev, name))
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
            storageWatched = watchStorage,
            storageIssues = storageIssues,
            pluginWatched = watchPlugins,
            pluginIssues = pluginIssues,
        )
        ClusterReading(snapshot, historyDetailOf(kubeNodes, nodeDisk))
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
            sensorWatched = watchSensors,
            sensorIssues = sensorIssues,
            pluginWatched = watchPlugins,
            pluginIssues = pluginIssues,
        )
        ClusterReading(snapshot, historyDetailOf(talos, storageHealth))
    }
}
