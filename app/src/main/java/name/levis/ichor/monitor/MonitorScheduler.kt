package name.levis.ichor.monitor

import android.content.Context
import androidx.glance.appwidget.GlanceAppWidgetManager
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import name.levis.ichor.TalosApp
import name.levis.ichor.widget.ClusterWidget
import name.levis.ichor.widget.staleInMillis
import java.util.concurrent.TimeUnit

private const val PERIODIC = "cluster-monitor"
private const val ONE_SHOT = "cluster-monitor-now"
private const val WIDGET_STALE = "cluster-widget-stale"

/** Runs the monitor while alerts are on or a widget is placed; otherwise cancels it. */
suspend fun syncMonitoring(context: Context, runNow: Boolean = false) {
    val app = context.applicationContext as TalosApp
    val widgets = runCatching {
        GlanceAppWidgetManager(context).getGlanceIds(ClusterWidget::class.java).isNotEmpty()
    }.getOrDefault(false)
    val needed = app.monitorStore.alertsEnabled.value || widgets
    val work = WorkManager.getInstance(context)

    if (!needed) {
        work.cancelUniqueWork(PERIODIC)
        return
    }

    val constraints = Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build()
    val minutes = app.monitorStore.intervalMinutes.value
    work.enqueueUniquePeriodicWork(
        PERIODIC,
        ExistingPeriodicWorkPolicy.UPDATE,
        PeriodicWorkRequestBuilder<MonitorWorker>(minutes, TimeUnit.MINUTES).setConstraints(constraints).build(),
    )
    if (runNow) {
        work.enqueueUniqueWork(
            ONE_SHOT,
            ExistingWorkPolicy.REPLACE,
            OneTimeWorkRequestBuilder<MonitorWorker>().setConstraints(constraints).build(),
        )
    }
}

/**
 * Redraws the widgets when the first of [snapshots] turns stale, so an old "all ready" is dimmed
 * even when no check can run (no network). Replaced by every newer run.
 */
fun scheduleWidgetStaleRefresh(context: Context, snapshots: Collection<ClusterSnapshot>, now: Long) {
    val delay = snapshots.mapNotNull { staleInMillis(it, now) }.minOrNull() ?: return
    WorkManager.getInstance(context).enqueueUniqueWork(
        WIDGET_STALE,
        ExistingWorkPolicy.REPLACE,
        OneTimeWorkRequestBuilder<WidgetRefreshWorker>().setInitialDelay(delay, TimeUnit.MILLISECONDS).build(),
    )
}
