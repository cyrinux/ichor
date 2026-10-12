package name.levis.ichor.ui.join

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.JoinRunState
import name.levis.ichor.ui.RunService

/**
 * Keeps the app running while it follows a node join, like the maintenance service: the install
 * and the reboot take minutes, and the phone must still be talking to the cluster when the node
 * comes back.
 */
class JoinService : RunService<JoinRunState>() {
    override val channelId = "node-join"
    override val progressId = 0x0b80
    override val resultId = 0x0b81
    override val channelName = R.string.join_channel_name
    override val channelDescription = R.string.join_channel_desc
    override val publicTitle = R.string.join_notification_public
    override val resultPublicTitle = R.string.join_notification_result_public

    override val current get() = (application as TalosApp).joinManager.current

    override fun progressText(res: Context, run: JoinRunState): Pair<String, String> {
        val step = run.events.lastOrNull()?.message?.takeIf { it.isNotBlank() } ?: res.getString(R.string.join_phase_applying)
        return res.getString(R.string.join_notification_title, run.hostname) to step
    }

    override fun doneText(res: Context, run: JoinRunState): Pair<String, String> =
        res.getString(R.string.join_notification_done, run.hostname) to res.getString(R.string.join_run_done, run.hostname, run.address)

    override fun failedTitle(res: Context, run: JoinRunState): String = res.getString(R.string.join_notification_failed, run.hostname)

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, JoinService::class.java))
        }
    }
}
