package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.ImagePullRunState

/** An image pulled on the nodes: a bar of the nodes done, and Stop. */
object ImagePullPresenter : OperationPresenter<ImagePullRunState> {
    override fun progress(run: ImagePullRunState) = OperationNotification(
        title = res(R.string.image_pull_notification_title),
        text = res(R.string.image_pull_progress, run.progress.done, run.progress.total),
        progress = OperationProgress.step(run.progress.done, run.progress.total),
        actions = if (run.stopping) emptyList() else listOf(OperationAction.STOP),
    )

    override fun done(run: ImagePullRunState) = OperationResult(
        res(if (run.stopping) R.string.image_pull_stopped else R.string.image_pull_notification_done),
        res(R.string.image_pull_progress, run.progress.done, run.progress.total),
    )

    override fun failedTitle(run: ImagePullRunState) = res(R.string.image_pull_notification_failed)
}
