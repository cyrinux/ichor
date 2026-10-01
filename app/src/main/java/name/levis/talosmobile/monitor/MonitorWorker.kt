package name.levis.talosmobile.monitor

import android.content.Context
import androidx.glance.appwidget.updateAll
import androidx.work.CoroutineWorker
import androidx.work.WorkerParameters
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.widget.ClusterWidget

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
        val certNotAfter = stored.summary.contexts.firstOrNull { it.name == stored.activeContext }?.certNotAfter ?: 0

        val now = System.currentTimeMillis()
        val evaluation = evaluate(store.snapshot(), snapshotOf(overview, etcd, certNotAfter, now), now)
        store.saveSnapshot(evaluation.next)

        if (store.alertsEnabled.value) {
            val hide = app.appLock.enabled.value
            evaluation.alerts.forEach { postAlert(applicationContext, it, hideOnLockScreen = hide) }
        }
        ClusterWidget().updateAll(applicationContext)
        return Result.success()
    }
}
