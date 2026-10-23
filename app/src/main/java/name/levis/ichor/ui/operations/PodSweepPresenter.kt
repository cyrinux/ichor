package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.PodSweepRun

/** "Clean up dead pods" in the background: a bar of the pods tried, and Stop. */
object PodSweepPresenter : OperationPresenter<PodSweepRun> {
    override fun progress(run: PodSweepRun) = OperationNotification(
        title = res(R.string.pod_sweep_notification_title, where(run)),
        text = if (run.stopping) res(R.string.pod_sweep_stopping) else res(R.string.pod_sweep_progress, run.done.toString(), run.total.toString()),
        progress = OperationProgress.step(run.done, run.total),
        actions = if (run.stopping) emptyList() else listOf(OperationAction.STOP),
    )

    override fun done(run: PodSweepRun) = OperationResult(
        res(if (run.stopped) R.string.pod_sweep_stopped else R.string.pod_sweep_done, where(run)),
        podSweepTally(run),
    )

    override fun failedTitle(run: PodSweepRun) = res(R.string.pod_sweep_failed, where(run))

    private fun where(run: PodSweepRun): OperationText =
        if (run.namespace.isEmpty()) res(R.string.pod_sweep_every_namespace) else raw(run.namespace)
}

/** How many pods went, and how many could not be deleted when some could not. */
fun podSweepTally(run: PodSweepRun): OperationText =
    if (run.failedCount > 0) {
        res(R.string.pod_sweep_result_failed, run.deleted.toString(), run.failedCount.toString())
    } else {
        res(R.string.pod_sweep_result, run.deleted.toString())
    }
