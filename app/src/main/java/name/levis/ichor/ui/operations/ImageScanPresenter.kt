package name.levis.ichor.ui.operations

import name.levis.ichor.R
import name.levis.ichor.data.ImageScanSession
import name.levis.ichor.model.IMAGESCAN_PHASE_SCANNING
import name.levis.ichor.ui.imagescan.scanPhaseLabel

/** A vulnerability scan of images in the cluster: a bar of the images scanned, and Stop. */
object ImageScanPresenter : OperationPresenter<ImageScanSession> {
    override fun progress(run: ImageScanSession): OperationNotification {
        val p = run.progress
        return OperationNotification(
            title = res(R.string.imagescan_notification_title, run.name),
            text = if (run.stopping) res(R.string.imagescan_stopping) else scanPhaseLabel(p, run.scanner),
            progress = if (p?.phase == IMAGESCAN_PHASE_SCANNING) OperationProgress.step(p.step - 1, p.steps) else OperationProgress.INDETERMINATE,
            actions = if (run.stopping) emptyList() else listOf(OperationAction.STOP),
        )
    }

    override fun done(run: ImageScanSession): OperationResult {
        val title = if (run.stopped) R.string.imagescan_notification_stopped else R.string.imagescan_notification_done
        val detail = run.report?.summary?.let {
            res(R.string.imagescan_notification_summary, it.critical.toString(), it.high.toString(), it.medium.toString(), it.low.toString())
        } ?: raw("")
        return OperationResult(res(title, run.name), detail)
    }

    override fun failedTitle(run: ImageScanSession) = res(R.string.imagescan_notification_failed, run.name)
}
