package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.FluxReconcileRunState
import name.levis.ichor.model.FLUX_FOLLOW_READY
import name.levis.ichor.model.FLUX_FOLLOW_STILL

/** A Flux reconcile requested from the phone: a spinner until Ready, and Stop following. */
object FluxReconcilePresenter : OperationPresenter<FluxReconcileRunState> {
    override fun progress(run: FluxReconcileRunState) = OperationNotification(
        title = res(R.string.flux_reconcile_notification_title, run.name),
        text = run.progress?.message?.ifEmpty { null }?.let(::raw) ?: res(R.string.flux_reconcile_waiting),
        actions = listOf(OperationAction.STOP_FOLLOWING),
    )

    override fun done(run: FluxReconcileRunState): OperationResult = when {
        run.stopped -> OperationResult(res(R.string.operation_stopped_following, run.name), res(R.string.operation_goes_on))
        run.result == FLUX_FOLLOW_READY -> OperationResult(res(R.string.flux_reconcile_notification_ready, run.name), raw(run.progress?.message.orEmpty()))
        run.result == FLUX_FOLLOW_STILL -> OperationResult(res(R.string.flux_reconcile_notification_still, run.name), res(R.string.flux_reconcile_still_text))
        else -> OperationResult(res(R.string.operation_stopped_following, run.name), res(R.string.operation_goes_on))
    }

    override fun failedTitle(run: FluxReconcileRunState) = res(R.string.flux_reconcile_notification_failed, run.name)
}
