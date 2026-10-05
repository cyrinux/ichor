package name.levis.ichor.monitor

import android.content.Context
import androidx.glance.appwidget.updateAll
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import name.levis.ichor.TalosApp
import name.levis.ichor.model.Feature
import name.levis.ichor.model.allows
import name.levis.ichor.widget.ClusterWidget

/**
 * Periodic check: overview + etcd, diffed against the previous snapshot. If the cluster is
 * unreachable as a whole (e.g. off VPN) the previous snapshot is kept (see [evaluate]), so
 * you are not spammed.
 */
class MonitorWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {

    override suspend fun doWork(): Result {
        val app = applicationContext as TalosApp
        val store = app.monitorStore
        val stored = app.configRepository.config.value ?: app.configRepository.load()
        if (stored == null) {
            // Config deleted: forget what we saw so the widget stops showing it.
            store.clearSnapshot()
            ClusterWidget().updateAll(applicationContext)
            return Result.success()
        }

        val overview = runCatching { app.talosRepository.overview() }.getOrNull() ?: return Result.success()
        val etcd = runCatching { app.talosRepository.etcd() }.getOrNull()
        val active = stored.summary.contexts.firstOrNull { it.name == stored.activeContext }
        val certNotAfter = active?.certNotAfter ?: 0

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
            gitopsIssuesWithGaps(argo, flux, known = knownGitOpsIssues(store.snapshot(), overview.context))
        } else {
            null
        }

        val now = System.currentTimeMillis()
        val current = snapshotOf(
            overview, etcd, certNotAfter, now, active?.fingerprint.orEmpty(), watchData, dataServices,
            gitopsWatched = watchGitops,
            gitopsIssues = gitopsIssues,
        )
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
