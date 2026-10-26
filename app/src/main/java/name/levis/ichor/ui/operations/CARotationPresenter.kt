package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.CARotationRunState

/** A cluster CA rotation: a bar of its five steps, the last message, and Stop. */
object CARotationPresenter : OperationPresenter<CARotationRunState> {
    override fun progress(run: CARotationRunState) = OperationNotification(
        title = res(R.string.ca_rotate_notification_title, run.context),
        text = when {
            run.stopping -> res(R.string.ca_rotate_stopping)
            else -> run.last?.message?.takeIf { it.isNotBlank() }?.let(::raw) ?: res(R.string.ca_rotate_phase_check)
        },
        progress = run.last?.let { OperationProgress.step(it.step - 1, it.total) } ?: OperationProgress.INDETERMINATE,
        actions = if (run.stopping) emptyList() else listOf(OperationAction.STOP),
    )

    override fun done(run: CARotationRunState) = OperationResult(
        res(R.string.ca_rotate_notification_done, run.context),
        res(R.string.ca_rotate_done_body),
    )

    override fun failedTitle(run: CARotationRunState) = res(R.string.ca_rotate_notification_failed, run.context)
}
