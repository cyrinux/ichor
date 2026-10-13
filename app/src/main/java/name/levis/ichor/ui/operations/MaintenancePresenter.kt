package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.MaintenanceRunState
import name.levis.ichor.ui.maintenance.doneText

/** A node maintenance (cordon, drain, reboot): Stop ends it after its current step. */
object MaintenancePresenter : OperationPresenter<MaintenanceRunState> {
    override fun progress(run: MaintenanceRunState) = OperationNotification(
        title = res(R.string.maintenance_notification_title, run.hostname),
        text = run.events.lastOrNull()?.message?.takeIf { it.isNotBlank() }?.let(::raw) ?: res(R.string.maintenance_phase_cordon),
        actions = if (run.stopping) emptyList() else listOf(OperationAction.STOP),
    )

    override fun done(run: MaintenanceRunState) = OperationResult(
        res(R.string.maintenance_notification_done, run.hostname),
        res(run.doneText, run.hostname),
    )

    override fun failedTitle(run: MaintenanceRunState) = res(R.string.maintenance_notification_failed, run.hostname)
}
