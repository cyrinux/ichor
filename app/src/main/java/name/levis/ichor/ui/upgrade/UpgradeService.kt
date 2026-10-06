package name.levis.ichor.ui.upgrade

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.UpgradeRunState
import name.levis.ichor.ui.RunService

/**
 * Keeps the app running while it follows a Talos upgrade, so suspending it does not break the
 * connection mid-install.
 */
class UpgradeService : RunService<UpgradeRunState>() {
    override val channelId = "talos-upgrades"
    override val progressId = 0x0b6e
    override val resultId = 0x0b6f
    override val channelName = R.string.upgrade_channel_name
    override val channelDescription = R.string.upgrade_channel_desc
    override val publicTitle = R.string.upgrade_notification_public
    override val resultPublicTitle = R.string.upgrade_notification_result_public

    override val current get() = (application as TalosApp).upgradeManager.current

    override fun progressText(res: Context, run: UpgradeRunState): Pair<String, String> {
        val step = run.events.lastOrNull()?.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.upgrade_phase_requested)
        return res.getString(R.string.upgrade_notification_title, run.hostname) to step
    }

    override fun doneText(res: Context, run: UpgradeRunState): Pair<String, String> =
        res.getString(R.string.upgrade_notification_done, run.hostname) to
            res.getString(R.string.upgrade_done_versions, run.fromVersion.ifEmpty { "?" }, run.newVersion.ifEmpty { "?" })

    override fun failedTitle(res: Context, run: UpgradeRunState): String =
        res.getString(R.string.upgrade_notification_failed, run.hostname)

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, UpgradeService::class.java))
        }
    }
}
