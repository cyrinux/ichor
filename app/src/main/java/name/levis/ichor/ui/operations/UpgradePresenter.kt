package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.UpgradeRunState

/** A Talos upgrade of one node: it cannot be cancelled once requested, only no longer followed. */
object UpgradePresenter : OperationPresenter<UpgradeRunState> {
    override fun progress(run: UpgradeRunState) = OperationNotification(
        title = res(R.string.upgrade_notification_title, run.hostname),
        text = run.events.lastOrNull()?.message?.takeIf { it.isNotBlank() }?.let(::raw) ?: res(R.string.upgrade_phase_requested),
        actions = listOf(OperationAction.STOP_FOLLOWING),
    )

    override fun done(run: UpgradeRunState) = OperationResult(
        res(R.string.upgrade_notification_done, run.hostname),
        res(R.string.upgrade_done_versions, run.fromVersion.ifEmpty { "?" }, run.newVersion.ifEmpty { "?" }),
    )

    override fun failedTitle(run: UpgradeRunState) = res(R.string.upgrade_notification_failed, run.hostname)
}
