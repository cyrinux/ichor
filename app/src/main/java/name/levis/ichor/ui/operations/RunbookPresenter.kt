package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.RunbookRunState

/** A runbook run: "step N of M", "waiting for you" on a step to confirm, and Stop. */
object RunbookPresenter : OperationPresenter<RunbookRunState> {
    override fun progress(run: RunbookRunState): OperationNotification {
        val step = (run.current + 1).coerceAtMost(run.total)
        val of = res(R.string.runbook_step_of, step.toString(), run.total.toString())
        return OperationNotification(
            title = res(R.string.runbook_notification_title, run.plan.name),
            // A step waiting for the user is no failure, and no time limit runs out on it.
            text = if (run.waiting != null) OperationText.Joined(listOf(of, res(R.string.runbook_waiting)), " · ") else of,
            progress = OperationProgress.step(run.current, run.total),
            actions = listOf(OperationAction.STOP),
        )
    }

    override fun done(run: RunbookRunState): OperationResult {
        val name = run.plan.name
        return when {
            run.ok -> OperationResult(res(R.string.runbook_notification_done, name), res(R.string.runbook_done_ok))
            run.stopping -> OperationResult(res(R.string.runbook_notification_stopped, name), res(R.string.runbook_stopped))
            else -> OperationResult(res(R.string.runbook_notification_step_failed, name), res(R.string.runbook_done_failed))
        }
    }

    override fun failedTitle(run: RunbookRunState) = res(R.string.runbook_notification_failed, run.plan.name)
}
