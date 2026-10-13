package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.JoinRunState

/** A node being added: its install goes on by itself, so the app can only stop following it. */
object JoinPresenter : OperationPresenter<JoinRunState> {
    override fun progress(run: JoinRunState) = OperationNotification(
        title = res(R.string.join_notification_title, run.hostname),
        text = run.events.lastOrNull()?.message?.takeIf { it.isNotBlank() }?.let(::raw) ?: res(R.string.join_phase_applying),
        actions = if (run.stopping) emptyList() else listOf(OperationAction.STOP_FOLLOWING),
    )

    override fun done(run: JoinRunState) = OperationResult(
        res(R.string.join_notification_done, run.hostname),
        res(R.string.join_run_done, run.hostname, run.address),
    )

    override fun failedTitle(run: JoinRunState) = res(R.string.join_notification_failed, run.hostname)
}
