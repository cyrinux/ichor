package name.levis.ichor.localmodel

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.job.JobParameters
import android.app.job.JobService
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.annotation.RequiresApi
import androidx.core.app.NotificationCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp

/**
 * The model download as a user-initiated data transfer job: the system shows it in its Task
 * Manager and keeps it running under its notification, with no foreground service. A network
 * that goes reschedules it (exponential backoff), and the next run resumes the part file.
 */
@RequiresApi(Build.VERSION_CODES.UPSIDE_DOWN_CAKE)
class ModelDownloadJob : JobService() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private var running: Job? = null

    override fun onStartJob(params: JobParameters): Boolean {
        val id = params.extras.getString(ModelDownloads.KEY_ID).orEmpty()
        val allowMetered = params.extras.getBoolean(ModelDownloads.KEY_METERED, false)
        val store = (application as TalosApp).localModels
        val model = store.info(id)
        if (model == null) {
            ModelDownloads.publish(DownloadStatus.Failed(id, ModelDownloads.ERROR_UNKNOWN))
            return false
        }
        // First thing: the system expects the job's notification right after it starts.
        showProgress(params, model, partialBytes(store, id))
        ModelDownloads.publish(DownloadStatus.Running(id, partialBytes(store, id), allowMetered))
        running = scope.launch {
            val outcome = ModelDownloader(applicationContext, store).run(model, allowMetered) { done ->
                // A stopped run leaves the status to whoever stopped it (cancel, onStopJob).
                if (isActive) {
                    ModelDownloads.publish(DownloadStatus.Running(id, done, allowMetered))
                    showProgress(params, model, done)
                }
            }
            if (!isActive) return@launch
            when (outcome) {
                DownloadOutcome.Done -> ModelDownloads.publish(DownloadStatus.Idle)
                DownloadOutcome.Retry -> ModelDownloads.publish(DownloadStatus.Waiting(id, allowMetered))
                is DownloadOutcome.Failed -> ModelDownloads.publish(DownloadStatus.Failed(id, outcome.error))
            }
            jobFinished(params, outcome == DownloadOutcome.Retry)
        }
        return true
    }

    /**
     * The system stopped the job (network gone, constraints, thermal): it reschedules it, and
     * the part file is resumed. A cancel from the app has already reset the status.
     */
    override fun onStopJob(params: JobParameters): Boolean {
        running?.cancel()
        if (params.stopReason == JobParameters.STOP_REASON_CANCELLED_BY_APP) return false
        val id = params.extras.getString(ModelDownloads.KEY_ID).orEmpty()
        ModelDownloads.publish(DownloadStatus.Waiting(id, params.extras.getBoolean(ModelDownloads.KEY_METERED, false)))
        return true
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    private fun showProgress(params: JobParameters, model: LocalModelInfo, done: Long) {
        setNotification(params, NOTIFICATION_ID, notification(model, done), JOB_END_NOTIFICATION_POLICY_REMOVE)
    }

    private fun notification(model: LocalModelInfo, done: Long): Notification {
        val manager = getSystemService(NotificationManager::class.java)
        if (manager.getNotificationChannel(CHANNEL_ID) == null) {
            manager.createNotificationChannel(
                NotificationChannel(CHANNEL_ID, getString(R.string.ai_local_channel), NotificationManager.IMPORTANCE_LOW),
            )
        }
        val percent = if (model.bytes > 0) (done * 100 / model.bytes).toInt() else 0
        val cancel = PendingIntent.getBroadcast(
            this,
            0,
            Intent(this, ModelDownloadCancelReceiver::class.java).putExtra(ModelDownloads.KEY_ID, model.id),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setContentTitle(getString(R.string.ai_local_notification))
            .setContentText(getString(R.string.ai_local_downloading, percent, formatModelSize(done), formatModelSize(model.bytes)))
            .setProgress(100, percent, model.bytes == 0L)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .addAction(0, getString(R.string.common_cancel), cancel)
            .build()
    }

    private companion object {
        const val CHANNEL_ID = "model-download"
        const val NOTIFICATION_ID = 7301
    }
}

/** Cancel on the download's notification: stops the job and deletes what arrived. */
class ModelDownloadCancelReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val id = intent.getStringExtra(ModelDownloads.KEY_ID) ?: return
        ModelDownloads.cancel(context, (context.applicationContext as TalosApp).localModels, id)
    }
}
