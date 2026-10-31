package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.JoinRunState

/**
 * A node being added: its install goes on by itself, so the app can only stop following it. The
 * same run may be a Talos snapshot in the making, where stopping deletes what it created.
 */
object JoinPresenter : OperationPresenter<JoinRunState> {
    private val JoinRunState.isSnapshot get() = request.snapshot != null

    override fun progress(run: JoinRunState) = OperationNotification(
        title = if (run.isSnapshot) res(R.string.hetzner_snapshot_notification_title) else res(R.string.join_notification_title, run.hostname),
        text = run.events.lastOrNull()?.message?.takeIf { it.isNotBlank() }?.let(::raw) ?: res(if (run.isSnapshot) R.string.hetzner_phase_key else R.string.join_phase_applying),
        actions = if (run.stopping) emptyList() else listOf(OperationAction.STOP_FOLLOWING),
    )

    override fun done(run: JoinRunState) = if (run.isSnapshot) {
        OperationResult(
            res(R.string.hetzner_snapshot_notification_done),
            res(R.string.hetzner_snapshot_done, run.snapshotResult?.description.orEmpty()),
        )
    } else {
        OperationResult(
            res(R.string.join_notification_done, run.hostname),
            res(R.string.join_run_done, run.hostname, run.address),
        )
    }

    override fun failedTitle(run: JoinRunState) =
        if (run.isSnapshot) res(R.string.hetzner_snapshot_notification_failed) else res(R.string.join_notification_failed, run.hostname)
}
