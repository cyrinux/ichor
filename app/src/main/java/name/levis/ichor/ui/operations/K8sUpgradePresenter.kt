package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.K8sUpgradeRunState
import name.levis.ichor.ui.upgrade.k8sStepLabel

/** Kubernetes upgraded node after node: a bar where the core counts, and Stop (after the node it changes). */
object K8sUpgradePresenter : OperationPresenter<K8sUpgradeRunState> {
    override fun progress(run: K8sUpgradeRunState): OperationNotification {
        val title = res(if (run.dryRun) R.string.k8s_upgrade_dry_run_title else R.string.k8s_upgrade_notification_title, run.version)
        val actions = if (run.cancelling) emptyList() else listOf(OperationAction.STOP)
        val p = run.latest ?: return OperationNotification(title, res(R.string.cluster_upgrade_starting), actions = actions)
        return OperationNotification(title, k8sStepLabel(p), OperationProgress.step(p.index, p.total), actions = actions)
    }

    override fun done(run: K8sUpgradeRunState) = if (run.dryRun) {
        OperationResult(res(R.string.k8s_upgrade_dry_run_done, run.version), res(R.string.k8s_upgrade_dry_run_done_detail))
    } else {
        OperationResult(res(R.string.k8s_upgrade_notification_done, run.version), raw(run.hostname))
    }

    override fun failedTitle(run: K8sUpgradeRunState) = res(R.string.k8s_upgrade_notification_failed, run.version)
}
