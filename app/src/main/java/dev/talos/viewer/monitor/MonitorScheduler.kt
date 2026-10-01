package dev.talos.viewer.monitor

import android.content.Context
import androidx.glance.appwidget.GlanceAppWidgetManager
import androidx.work.Constraints
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import dev.talos.viewer.TalosApp
import dev.talos.viewer.widget.ClusterWidget
import java.util.concurrent.TimeUnit

private const val PERIODIC = "cluster-monitor"
private const val ONE_SHOT = "cluster-monitor-now"

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
