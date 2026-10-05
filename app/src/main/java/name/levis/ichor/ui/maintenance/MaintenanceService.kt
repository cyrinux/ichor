package name.levis.ichor.ui.maintenance

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.MaintenanceRunState
import name.levis.ichor.ui.RunService

/**
 * Keeps the app running while it follows a node maintenance, like the upgrade service: a
 * drain can wait minutes for a PodDisruptionBudget.
 */
class MaintenanceService : RunService<MaintenanceRunState>() {
    override val channelId = "node-maintenance"
    override val progressId = 0x0b70
    override val resultId = 0x0b71
    override val channelName = R.string.maintenance_channel_name
    override val channelDescription = R.string.maintenance_channel_desc
    override val publicTitle = R.string.maintenance_notification_public
    override val resultPublicTitle = R.string.maintenance_notification_result_public

    override val current get() = (application as TalosApp).maintenanceManager.current

    override fun progressText(res: Context, run: MaintenanceRunState): Pair<String, String> {
        val step = run.events.lastOrNull()?.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.maintenance_phase_cordon)
        return res.getString(R.string.maintenance_notification_title, run.hostname) to step
    }

    override fun doneText(res: Context, run: MaintenanceRunState): Pair<String, String> =
        res.getString(R.string.maintenance_notification_done, run.hostname) to res.getString(run.doneText, run.hostname)

    override fun failedTitle(res: Context, run: MaintenanceRunState): String =
        res.getString(R.string.maintenance_notification_failed, run.hostname)

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, MaintenanceService::class.java))
        }
    }
}
