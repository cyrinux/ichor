package name.levis.ichor.widget

import android.content.Context
import android.util.Log
import androidx.glance.appwidget.updateAll
import kotlinx.coroutines.CancellationException
import name.levis.ichor.TalosApp
import name.levis.ichor.data.FleetSources
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.monitor.fleetGlanceOf

private const val TAG = "FleetWidget"

/** Redraws the cluster widgets and the fleet widgets from what the monitor keeps. */
suspend fun updateAllWidgets(context: Context) {
    ClusterWidget().updateAll(context)
    FleetWidget().updateAll(context)
}

/**
 * Ranks the fleet from the snapshots stored now (the fleet screen's rules, through the Go core,
 * or the demo's fleet when the demo is the only cluster) and keeps the compact result for the
 * fleet widget and the watch, with the names as the app shows them now. A ranking error keeps
 * the last one: the widget never goes blank because of it.
 */
suspend fun TalosApp.refreshFleetGlance(
    now: Long = System.currentTimeMillis(),
    summary: ConfigSummary? = configRepository.config.value?.summary,
) {
    val masked = uiPreferences.privacyMask.value.enabled
    val sources = FleetSources(monitorStore.state.value, summary, clusterNames.names.value, masked)
    val fleet = try {
        fleetRepository.summary(sources, now)
    } catch (e: CancellationException) {
        throw e
    } catch (e: Exception) {
        Log.w(TAG, "fleet not ranked: ${e.message}")
        return
    }
    monitorStore.saveFleet(fleetGlanceOf(fleet, masked))
}
