package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.ArgoSyncRunState
import name.levis.ichor.model.ARGO_SYNC_PENDING
import name.levis.ichor.model.ARGO_SYNC_SUCCEEDED
import name.levis.ichor.model.ARGO_SYNC_TERMINATED
import name.levis.ichor.model.ARGO_SYNC_TERMINATING

/** An Argo CD sync started from the phone: its wave and resources, a bar of them, and Terminate. */
object ArgoSyncPresenter : OperationPresenter<ArgoSyncRunState> {
    override fun progress(run: ArgoSyncRunState): OperationNotification {
        val p = run.progress
        val step = when {
            run.terminating || p?.phase == ARGO_SYNC_TERMINATING -> res(R.string.argo_sync_terminating)
            p == null || p.phase == ARGO_SYNC_PENDING -> res(R.string.argo_sync_pending)
            else -> OperationText.Joined(
                listOfNotNull(
                    res(R.string.argo_sync_wave, p.wave, p.waves).takeIf { p.wave > 0 && p.waves > 0 },
                    res(R.string.argo_sync_resources, p.done, p.total).takeIf { p.total > 0 },
                ).ifEmpty { listOf(raw(p.message)) },
                " · ",
            )
        }
        return OperationNotification(
            title = res(R.string.argo_sync_notification_title, run.name),
            // Why the last Terminate was refused, while the sync goes on.
            text = OperationText.Joined(listOf(step, raw(run.terminateError.orEmpty())), "\n"),
            progress = p?.takeIf { it.total > 0 }?.let { OperationProgress.step(it.done, it.total) } ?: OperationProgress.INDETERMINATE,
            actions = if (run.terminating) emptyList() else listOf(OperationAction.TERMINATE),
        )
    }

    override fun done(run: ArgoSyncRunState): OperationResult = when (run.result) {
        ARGO_SYNC_SUCCEEDED -> OperationResult(
            res(R.string.argo_sync_notification_done, run.name),
            run.progress?.takeIf { it.total > 0 }?.let { res(R.string.argo_sync_resources, it.done, it.total) } ?: raw(run.progress?.message.orEmpty()),
        )
        ARGO_SYNC_TERMINATED -> OperationResult(res(R.string.argo_sync_notification_terminated, run.name), raw(run.progress?.message.orEmpty()))
        // No longer followed: the sync goes on in Argo CD.
        else -> OperationResult(res(R.string.operation_stopped_following, run.name), res(R.string.operation_goes_on))
    }

    override fun failedTitle(run: ArgoSyncRunState) = res(R.string.argo_sync_notification_failed, run.name)
}
