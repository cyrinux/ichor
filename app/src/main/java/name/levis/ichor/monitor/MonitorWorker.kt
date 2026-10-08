package name.levis.ichor.monitor

import android.content.Context
import androidx.glance.appwidget.updateAll
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import name.levis.ichor.TalosApp
import name.levis.ichor.data.activeIsKube
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.model.needsEndpoint
import name.levis.ichor.widget.ClusterWidget

/**
 * Periodic check: overview + etcd (a cluster added from a kubeconfig: its Kubernetes node list),
 * diffed against the previous snapshot. If the cluster is unreachable as a whole (e.g. off VPN)
 * the previous snapshot is kept (see [evaluate]), so you are not spammed.
 */
class MonitorWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {

    override suspend fun doWork(): Result {
        val app = applicationContext as TalosApp
        val store = app.monitorStore
        // Unreadable for now is not deleted: keep the snapshot and check again later.
        val stored = app.configRepository.config.value
            ?: runCatching { app.configRepository.load() }.getOrElse { return Result.retry() }
        if (stored == null) {
            // Config deleted: forget what we saw so the widget stops showing it.
            store.clearSnapshot()
            ClusterWidget().updateAll(applicationContext)
            return Result.success()
        }

        // A Talos cluster without an endpoint yet cannot be checked: the widget shows no other
        // cluster's nodes for it.
        if (stored.activeSummary?.needsEndpoint == true) {
            store.clearSnapshot()
            ClusterWidget().updateAll(applicationContext)
            return Result.success()
        }
        val active = stored.summary.contexts.firstOrNull { it.name == stored.activeContext }
        val certNotAfter = active?.certNotAfter ?: 0
        val kube = stored.activeIsKube
        // The checks start from the Talos overview, or from the Kubernetes node list of a cluster
        // added from a kubeconfig. Unreadable (off VPN, a sign-in needed): nothing to compare, the
        // previous snapshot stays.
        val overview = if (kube) null else runCatching { app.talosRepository.overview() }.getOrNull()
        val kubeNodes = if (kube) runCatching { app.talosRepository.kubeNodes() }.getOrNull() else null
        if (overview == null && kubeNodes == null) return Result.success()
        val etcd = if (kube) null else runCatching { app.talosRepository.etcd() }.getOrNull()
        val context = stored.activeContext

        // Opt-in, and only for roles that may use the Kubernetes API: each check then lists custom
        // resources and runs the Garage CLI in a pod.
        val watchData = store.dataServicesWatched.value && active?.allows(Feature.WORKLOADS) == true
        val dataServices = if (watchData) runCatching { app.talosRepository.dataServices(hints = "") }.getOrNull() else null

        // Same for Argo CD and Flux apps: each call answers installed=false quickly when absent. A
        // part that could not be read keeps its known issues, so they neither clear falsely nor
        // hold the other part's alerts back.
        val watchGitops = store.gitopsWatched.value && active?.allows(Feature.WORKLOADS) == true
        val gitopsIssues = if (watchGitops) {
            val argo = runCatching { app.talosRepository.argoCD() }.getOrNull()
            val flux = runCatching { app.talosRepository.flux() }.getOrNull()
            gitopsIssuesWithGaps(argo, flux, known = knownGitOpsIssues(store.snapshot(), context))
        } else {
            null
        }

        // And for the checkup: it lists the cluster's pods and asks every kubelet. A section that
        // could not be read keeps its known findings.
        val watchCheckup = store.checkupWatched.value && active?.allows(Feature.WORKLOADS) == true
        val checkupIssues = if (watchCheckup) {
            runCatching { app.talosRepository.checkup() }.getOrNull()
                ?.let { checkupIssuesWithGaps(it, known = knownCheckupIssues(store.snapshot(), context)) }
        } else {
            null
        }

        val now = System.currentTimeMillis()
        val current = if (kubeNodes != null) {
            kubeSnapshotOf(
                kubeNodes, context, certNotAfter, now, active?.fingerprint.orEmpty(), watchData, dataServices,
                gitopsWatched = watchGitops,
                gitopsIssues = gitopsIssues,
                checkupWatched = watchCheckup,
                checkupIssues = checkupIssues,
            )
        } else {
            snapshotOf(
                checkNotNull(overview), etcd, certNotAfter, now, active?.fingerprint.orEmpty(), watchData, dataServices,
                gitopsWatched = watchGitops,
                gitopsIssues = gitopsIssues,
                checkupWatched = watchCheckup,
                checkupIssues = checkupIssues,
            )
        }
        val evaluation = evaluate(store.snapshot(), current, now)
        store.saveSnapshot(evaluation.next)
        scheduleWidgetStaleRefresh(applicationContext, evaluation.next, now)

        if (store.alertsEnabled.value) {
            val hide = app.appLock.enabled.value
            evaluation.alerts.forEach { postAlert(applicationContext, it, hideOnLockScreen = hide) }
        }
        ClusterWidget().updateAll(applicationContext)
        return Result.success()
    }
}

/** Redraws the widget from the stored snapshot, without any network call. */
class WidgetRefreshWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        ClusterWidget().updateAll(applicationContext)
        return Result.success()
    }
}
