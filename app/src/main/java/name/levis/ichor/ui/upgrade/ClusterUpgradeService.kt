package name.levis.ichor.ui.upgrade

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.ClusterUpgradeRunState
import name.levis.ichor.model.ClusterUpgradeNode
import name.levis.ichor.model.ClusterUpgradeProgress
import name.levis.ichor.model.UpgradePhase
import name.levis.ichor.ui.RunService

/**
 * Keeps the app running while every node is upgraded one after the other, like the upgrade
 * service: a roll lasts many minutes per node, and losing the app mid-roll pauses it until it
 * is started again (it then continues with the nodes left).
 */
class ClusterUpgradeService : RunService<ClusterUpgradeRunState>() {
    override val channelId = "talos-upgrades"
    override val progressId = 0x0b78
    override val resultId = 0x0b79
    override val channelName = R.string.upgrade_channel_name
    override val channelDescription = R.string.upgrade_channel_desc
    override val publicTitle = R.string.cluster_upgrade_notification_public
    override val resultPublicTitle = R.string.cluster_upgrade_notification_result_public

    override val current get() = (application as TalosApp).clusterUpgradeManager.current

    override fun progressText(res: Context, run: ClusterUpgradeRunState): Pair<String, String> {
        val p = run.progress
        val title = res.getString(R.string.cluster_upgrade_notification_title, run.version)
        if (p == null || p.total == 0) return title to res.getString(R.string.cluster_upgrade_starting)
        val step = res.getString(R.string.cluster_upgrade_step, (p.index + 1).coerceAtMost(p.total), p.total, p.name, phaseText(res, p))
        return title to step
    }

    override fun doneText(res: Context, run: ClusterUpgradeRunState): Pair<String, String> =
        res.getString(R.string.cluster_upgrade_notification_done, run.version) to
            res.getString(R.string.cluster_upgrade_done_detail, run.nodes.count { it.state == ClusterUpgradeNode.DONE })

    override fun failedTitle(res: Context, run: ClusterUpgradeRunState): String =
        res.getString(R.string.cluster_upgrade_notification_failed, run.version)

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, ClusterUpgradeService::class.java))
        }
    }
}

/** What the roll does now, in words: the node's own upgrade phase, the gate, or why it waits. */
fun phaseText(res: Context, p: ClusterUpgradeProgress): String = when (p.phase) {
    ClusterUpgradeProgress.GATE -> res.getString(R.string.cluster_upgrade_gate)
    ClusterUpgradeProgress.PAUSED -> res.getString(R.string.cluster_upgrade_paused)
    ClusterUpgradeProgress.DONE -> res.getString(R.string.upgrade_phase_done)
    else -> UpgradePhase.of(p.nodePhase)?.let { res.getString(it.label) } ?: p.nodePhase.ifEmpty { res.getString(R.string.upgrade_phase_requested) }
}
