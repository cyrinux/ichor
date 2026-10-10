package name.levis.ichor.ui.machineconfig

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.ConfigMultiRunState
import name.levis.ichor.model.MultiConfigNodeState
import name.levis.ichor.ui.RunService

/**
 * Keeps the app running while a config change is applied to several nodes, like the
 * maintenance service: in reboot mode each node is waited for, minutes per node.
 */
class ConfigMultiService : RunService<ConfigMultiRunState>() {
    // The node maintenance's channel: both are runs the user started on nodes.
    override val channelId = "node-maintenance"
    override val progressId = 0x0b76
    override val resultId = 0x0b77
    override val channelName = R.string.maintenance_channel_name
    override val channelDescription = R.string.maintenance_channel_desc
    override val publicTitle = R.string.config_multi_notification_public
    override val resultPublicTitle = R.string.config_multi_notification_result_public

    override val current get() = (application as TalosApp).configMultiManager.current

    override fun progressText(res: Context, state: ConfigMultiRunState): Pair<String, String> {
        val progress = state.run.progress
        val title = res.getString(R.string.config_multi_notification_title, state.hostname)
        if (progress == null || progress.total == 0) return title to res.getString(R.string.machine_config_apply_applying)
        val node = progress.nodes.firstOrNull { it.state == MultiConfigNodeState.APPLYING } ?: progress.nodes.getOrNull(progress.index)
        val step = res.getString(R.string.machine_config_multi_progress, (progress.index + 1).coerceAtMost(progress.total), progress.total)
        val detail = listOfNotNull(node?.name, progress.message.takeIf { it.isNotBlank() }).joinToString(": ")
        return title to listOf(step, detail).filter { it.isNotEmpty() }.joinToString(" · ")
    }

    override fun doneText(res: Context, state: ConfigMultiRunState): Pair<String, String> {
        val done = state.run.progress?.nodes.orEmpty().filter { it.state == MultiConfigNodeState.DONE }.map { it.name }
        return res.getString(R.string.config_multi_notification_done, state.hostname) to done.joinToString(", ")
    }

    override fun failedTitle(res: Context, state: ConfigMultiRunState): String =
        res.getString(R.string.config_multi_notification_failed, state.hostname)

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, ConfigMultiService::class.java))
        }
    }
}
