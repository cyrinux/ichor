package name.levis.ichor.ui.images

import android.content.Context
import android.content.Intent
import androidx.core.content.ContextCompat
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.ImagePullRunState
import name.levis.ichor.ui.RunService

/**
 * Keeps the app running while it pulls an image on the nodes, like the maintenance service:
 * a pull over a slow link takes minutes per node.
 */
class ImagePullService : RunService<ImagePullRunState>() {
    override val channelId = "image-pull"
    override val progressId = 0x0b80
    override val resultId = 0x0b81
    override val channelName = R.string.image_pull_channel_name
    override val channelDescription = R.string.image_pull_channel_desc
    override val publicTitle = R.string.image_pull_notification_public
    override val resultPublicTitle = R.string.image_pull_notification_result_public

    override val current get() = (application as TalosApp).imagePullManager.current

    override fun progressText(res: Context, run: ImagePullRunState): Pair<String, String> =
        res.getString(R.string.image_pull_notification_title) to
            res.getString(R.string.image_pull_progress, run.progress.done, run.progress.total)

    override fun doneText(res: Context, run: ImagePullRunState): Pair<String, String> {
        val title = if (run.stopping) R.string.image_pull_stopped else R.string.image_pull_notification_done
        return res.getString(title) to res.getString(R.string.image_pull_progress, run.progress.done, run.progress.total)
    }

    override fun failedTitle(res: Context, run: ImagePullRunState): String =
        res.getString(R.string.image_pull_notification_failed)

    companion object {
        fun start(context: Context) {
            ContextCompat.startForegroundService(context, Intent(context, ImagePullService::class.java))
        }
    }
}
