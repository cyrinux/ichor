package name.levis.ichor.ui.machineconfig

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import name.levis.ichor.MainActivity
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.ConfigTryRunState
import name.levis.ichor.model.ConfigTryState
import name.levis.ichor.ui.DeepLink
import name.levis.ichor.ui.RunService
import java.text.DateFormat
import java.util.Date

private const val ACTION_KEEP = "name.levis.ichor.CONFIG_TRY_KEEP"
private const val ACTION_REVERT = "name.levis.ichor.CONFIG_TRY_REVERT"

/**
 * Keeps the app running while a machine config is tried, so the countdown and Keep survive
 * leaving the screen: the notification counts down to the node's own revert and offers Keep
 * and Revert now. The node reverts by itself at the deadline whatever happens to the app.
 */
class ConfigTryService : RunService<ConfigTryRunState>() {
    // The node maintenance's channel: both are runs the user started on a node.
    override val channelId = "node-maintenance"
    override val progressId = 0x0b74
    override val resultId = 0x0b75
    override val channelName = R.string.maintenance_channel_name
    override val channelDescription = R.string.maintenance_channel_desc
    override val publicTitle = R.string.config_try_notification_public
    override val resultPublicTitle = R.string.config_try_notification_result_public

    override val current get() = (application as TalosApp).configTryManager.current

    override fun progressText(res: Context, run: ConfigTryRunState): Pair<String, String> {
        val running = run.state as? ConfigTryState.Running
        val step = when (running?.phase) {
            ConfigTryState.TRYING -> if (run.deadline > 0) {
                res.getString(R.string.config_try_notification_reverts_at, DateFormat.getTimeInstance(DateFormat.SHORT).format(Date(run.deadline)))
            } else {
                res.getString(R.string.machine_config_try_countdown)
            }
            ConfigTryState.KEEPING -> res.getString(R.string.machine_config_try_keeping)
            ConfigTryState.REVERTING -> res.getString(R.string.machine_config_try_reverting)
            else -> res.getString(R.string.machine_config_try_applying)
        }
        // Why the last keep or revert failed, while the try goes on.
        val detail = running?.message?.takeIf { it.isNotBlank() }?.let { "$step\n$it" } ?: step
        return res.getString(R.string.config_try_notification_title, run.hostname) to detail
    }

    /** The countdown to the node's revert, ticking by itself, and the two choices while it waits. */
    override fun decorateProgress(res: Context, builder: NotificationCompat.Builder, run: ConfigTryRunState) {
        if (run.deadline > 0) {
            builder.setWhen(run.deadline).setShowWhen(true).setUsesChronometer(true).setChronometerCountDown(true)
        }
        if (run.trying) {
            builder.addAction(keepAction(res)).addAction(revertAction(res))
        }
    }

    override fun doneText(res: Context, run: ConfigTryRunState): Pair<String, String> =
        if (run.state == ConfigTryState.Kept) {
            res.getString(R.string.config_try_notification_kept, run.hostname) to res.getString(R.string.machine_config_kept)
        } else {
            res.getString(R.string.config_try_notification_reverted, run.hostname) to res.getString(R.string.machine_config_reverted)
        }

    override fun failedTitle(res: Context, run: ConfigTryRunState): String =
        res.getString(R.string.config_try_notification_failed, run.hostname)

    // With the app lock on, Keep opens the app (unlocked first) on the try instead of keeping
    // from the shade: the screen's Keep is behind the lock too.
    private fun keepAction(res: Context): NotificationCompat.Action {
        val flags = PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        val locked = (application as TalosApp).appLock.enabled.value
        val intent = if (locked) {
            val open = Intent(this, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
                .putExtra(MainActivity.EXTRA_OPEN, DeepLink.CONFIG_TRY.name)
            PendingIntent.getActivity(this, progressId + 1, open, flags)
        } else {
            PendingIntent.getBroadcast(this, progressId + 1, Intent(this, ConfigTryActionReceiver::class.java).setAction(ACTION_KEEP), flags)
        }
        return NotificationCompat.Action.Builder(0, res.getString(R.string.machine_config_keep), intent)
            .setShowsUserInterface(locked)
            .setAuthenticationRequired(true)
            .build()
    }

    private fun revertAction(res: Context): NotificationCompat.Action {
        val flags = PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
        val intent = PendingIntent.getBroadcast(this, progressId + 2, Intent(this, ConfigTryActionReceiver::class.java).setAction(ACTION_REVERT), flags)
        return NotificationCompat.Action.Builder(0, res.getString(R.string.machine_config_revert_now), intent)
            .setAuthenticationRequired(true)
            .build()
    }

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, ConfigTryService::class.java))
        }
    }
}

/** Keep and Revert now from the try's notification. */
class ConfigTryActionReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val manager = (context.applicationContext as TalosApp).configTryManager
        when (intent.action) {
            // Re-checked here: the lock may have been turned on since the notification was posted.
            ACTION_KEEP -> if (!(context.applicationContext as TalosApp).appLock.enabled.value) manager.keep()
            ACTION_REVERT -> manager.revert()
        }
    }
}
