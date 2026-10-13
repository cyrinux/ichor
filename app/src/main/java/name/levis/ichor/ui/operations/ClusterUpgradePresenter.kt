package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.ClusterUpgradeRunState
import name.levis.ichor.model.ClusterUpgradeNode
import name.levis.ichor.ui.upgrade.phaseLabel

/** Every node upgraded one after the other: a bar per node, Pause or Resume, and Abort. */
object ClusterUpgradePresenter : OperationPresenter<ClusterUpgradeRunState> {
    override fun progress(run: ClusterUpgradeRunState): OperationNotification {
        val p = run.progress
        val title = res(R.string.cluster_upgrade_notification_title, run.version)
        val actions = when {
            run.aborting -> emptyList()
            run.paused -> listOf(OperationAction.RESUME, OperationAction.ABORT)
            else -> listOf(OperationAction.PAUSE, OperationAction.ABORT)
        }
        if (p == null || p.total == 0) return OperationNotification(title, res(R.string.cluster_upgrade_starting), actions = actions)
        return OperationNotification(
            title = title,
            text = res(R.string.cluster_upgrade_step, (p.index + 1).coerceAtMost(p.total), p.total, p.name, phaseLabel(p)),
            progress = OperationProgress.step(p.index, p.total),
            actions = actions,
        )
    }

    override fun done(run: ClusterUpgradeRunState) = OperationResult(
        res(R.string.cluster_upgrade_notification_done, run.version),
        res(R.string.cluster_upgrade_done_detail, run.nodes.count { it.state == ClusterUpgradeNode.DONE }),
    )

    override fun failedTitle(run: ClusterUpgradeRunState) = res(R.string.cluster_upgrade_notification_failed, run.version)
}
