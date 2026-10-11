package name.levis.ichor.ui.imagescan

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.ImageScanSession
import name.levis.ichor.ui.RunService

/**
 * Keeps the app running while an image scan runs, like the image pull service: a scan takes
 * minutes, and a suspended app (screen locked) would lose its log and the report with it.
 */
class ImageScanService : RunService<ImageScanSession>() {
    override val channelId = "image-scan"
    override val progressId = 0x0b90
    override val resultId = 0x0b91
    override val channelName = R.string.imagescan_channel_name
    override val channelDescription = R.string.imagescan_channel_desc
    override val publicTitle = R.string.imagescan_notification_public
    override val resultPublicTitle = R.string.imagescan_notification_result_public

    override val current get() = (application as TalosApp).imageScanRepository.session

    override fun progressText(res: Context, run: ImageScanSession): Pair<String, String> =
        res.getString(R.string.imagescan_notification_title, run.name) to
            if (run.stopping) res.getString(R.string.imagescan_stopping) else scanPhaseText(res, run.progress, run.scanner)

    override fun doneText(res: Context, run: ImageScanSession): Pair<String, String> {
        val title = if (run.stopped) R.string.imagescan_notification_stopped else R.string.imagescan_notification_done
        val detail = run.report?.summary?.let {
            res.getString(R.string.imagescan_notification_summary, it.critical.toString(), it.high.toString(), it.medium.toString(), it.low.toString())
        }.orEmpty()
        return res.getString(title, run.name) to detail
    }

    override fun failedTitle(res: Context, run: ImageScanSession): String =
        res.getString(R.string.imagescan_notification_failed, run.name)

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, ImageScanService::class.java))
        }
    }
}
