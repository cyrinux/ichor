package name.levis.ichor.ui.operations

import androidx.annotation.StringRes
import name.levis.ichor.R
import name.levis.ichor.data.JoinRunState
import name.levis.ichor.model.ImageRunKind
import name.levis.ichor.ui.join.done
import name.levis.ichor.ui.join.label
import name.levis.ichor.ui.join.madeName

/**
 * A node being added: its install goes on by itself, so the app can only stop following it. The
 * same run may be a Talos image in the making (a snapshot, an AMI, an Azure image), where stopping
 * deletes what it created.
 */
object JoinPresenter : OperationPresenter<JoinRunState> {
    /** An image run's notification titles: in progress, made, failed. */
    private class ImageTitles(@StringRes val title: Int, @StringRes val done: Int, @StringRes val failed: Int)

    private val ImageRunKind.titles: ImageTitles
        get() = when (this) {
            ImageRunKind.HETZNER_SNAPSHOT -> ImageTitles(
                R.string.hetzner_snapshot_notification_title,
                R.string.hetzner_snapshot_notification_done,
                R.string.hetzner_snapshot_notification_failed,
            )
            ImageRunKind.AWS_AMI -> ImageTitles(
                R.string.aws_image_notification_title,
                R.string.aws_image_notification_done,
                R.string.aws_image_notification_failed,
            )
            // The same words as Azure's: a helper VM makes "a Talos image" at both.
            ImageRunKind.AZURE_IMAGE, ImageRunKind.GCP_IMAGE -> ImageTitles(
                R.string.azure_image_notification_title,
                R.string.azure_image_notification_done,
                R.string.azure_image_notification_failed,
            )
        }

    override fun progress(run: JoinRunState): OperationNotification {
        val kind = run.request.imageKind
        return OperationNotification(
            title = kind?.let { res(it.titles.title) } ?: res(R.string.join_notification_title, run.hostname),
            text = run.events.lastOrNull()?.message?.takeIf { it.isNotBlank() }?.let(::raw)
                ?: kind?.let { res(it.label(it.phases.first())) }
                ?: res(R.string.join_phase_applying),
            actions = if (run.stopping) emptyList() else listOf(OperationAction.STOP_FOLLOWING),
        )
    }

    override fun done(run: JoinRunState): OperationResult {
        val kind = run.request.imageKind ?: return OperationResult(
            res(R.string.join_notification_done, run.hostname),
            res(R.string.join_run_done, run.hostname, run.address),
        )
        return OperationResult(res(kind.titles.done), res(kind.done, kind.madeName(run)))
    }

    override fun failedTitle(run: JoinRunState) =
        run.request.imageKind?.let { res(it.titles.failed) } ?: res(R.string.join_notification_failed, run.hostname)
}
