package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.RolloutRunState

/** A rollout restart: a bar of the new pods ready, and Stop following. */
object RolloutPresenter : OperationPresenter<RolloutRunState> {
    override fun progress(run: RolloutRunState) = OperationNotification(
        title = res(R.string.rollout_notification_title, run.workload.name),
        text = if (run.status == null) res(R.string.rollout_starting) else res(R.string.rollout_notification_ready, run.ready, run.total),
        progress = if (run.status == null) OperationProgress.INDETERMINATE else OperationProgress.step(run.ready, run.total),
        actions = listOf(OperationAction.STOP_FOLLOWING),
    )

    override fun done(run: RolloutRunState): OperationResult {
        val name = run.workload.name
        val status = run.status
        return when {
            run.stopped || status == null -> OperationResult(res(R.string.operation_stopped_following, name), res(R.string.operation_goes_on))
            status.failed -> OperationResult(res(R.string.rollout_notification_stalled, name), res(R.string.rollout_failed))
            status.manual -> OperationResult(res(R.string.rollout_notification_done, name), res(R.string.rollout_manual))
            status.done -> OperationResult(res(R.string.rollout_notification_done, name), res(R.string.rollout_notification_ready, run.ready, run.total))
            // The watch ended before the rollout did.
            else -> OperationResult(res(R.string.operation_stopped_following, name), res(R.string.operation_goes_on))
        }
    }

    override fun failedTitle(run: RolloutRunState) = res(R.string.rollout_notification_failed, run.workload.name)
}
