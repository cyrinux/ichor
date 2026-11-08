package name.levis.ichor.localmodel

import android.app.job.JobInfo
import android.app.job.JobScheduler
import android.content.ComponentName
import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.Build
import android.os.PersistableBundle
import androidx.annotation.ChecksSdkIntAtLeast
import androidx.annotation.RequiresApi
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** What keeps a model download from running now, or [NONE]. */
enum class DownloadBlock { NONE, NO_NETWORK, METERED, DATA_SAVER, ROAMING }

/**
 * The download rules: never on roaming; on a metered network (mobile data, a hotspot, a Wi-Fi
 * marked metered) only when the user allowed it for this download, and never while Data Saver
 * is on. [dataSaver] is ConnectivityManager's restrictBackgroundStatus.
 */
fun downloadBlock(connected: Boolean, metered: Boolean, roaming: Boolean, dataSaver: Int, allowMetered: Boolean): DownloadBlock = when {
    !connected -> DownloadBlock.NO_NETWORK
    roaming -> DownloadBlock.ROAMING
    !metered -> DownloadBlock.NONE
    !allowMetered -> DownloadBlock.METERED
    dataSaver == ConnectivityManager.RESTRICT_BACKGROUND_STATUS_ENABLED -> DownloadBlock.DATA_SAVER
    else -> DownloadBlock.NONE
}

/** [downloadBlock] for the phone's network right now. */
fun currentDownloadBlock(context: Context, allowMetered: Boolean): DownloadBlock {
    val cm = context.getSystemService(ConnectivityManager::class.java) ?: return DownloadBlock.NO_NETWORK
    val caps = cm.activeNetwork?.let(cm::getNetworkCapabilities)
    return downloadBlock(
        connected = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) == true,
        metered = cm.isActiveNetworkMetered,
        // NOT_ROAMING exists from Android 9; before, every network would read as roaming.
        roaming = Build.VERSION.SDK_INT >= Build.VERSION_CODES.P &&
            caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_ROAMING) == false,
        dataSaver = cm.restrictBackgroundStatus,
        allowMetered = allowMetered,
    )
}

/** Whether this phone can download a catalog model: user-initiated data transfer jobs, Android 14+. */
@ChecksSdkIntAtLeast(api = Build.VERSION_CODES.UPSIDE_DOWN_CAKE)
fun downloadSupported(): Boolean = downloadSupportedOn(Build.VERSION.SDK_INT)

internal fun downloadSupportedOn(sdkInt: Int): Boolean = sdkInt >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE

/** Where the model download stands, for the settings screen. */
sealed interface DownloadStatus {
    data object Idle : DownloadStatus

    /** Scheduled, waiting for an allowed network (or a retry after one went). */
    data class Waiting(val id: String, val allowMetered: Boolean) : DownloadStatus

    /** Arriving: [done] bytes on the phone. */
    data class Running(val id: String, val done: Long, val allowMetered: Boolean) : DownloadStatus

    data class Failed(val id: String, val error: String) : DownloadStatus
}

/** The download JobScheduler still holds: its model and whether mobile data was allowed. */
data class PendingDownload(val id: String, val allowMetered: Boolean)

/**
 * [current] corrected by what JobScheduler holds: a job left from before the app was restarted
 * shows as waiting, and a waiting or running download whose job is gone (cancelled, or stopped
 * from the system's Task Manager, which ends the process without telling the app) goes back to
 * idle. A failed one stays until the next tap; a running one with its job is left to the job.
 */
fun reconcile(current: DownloadStatus, pending: PendingDownload?): DownloadStatus = when {
    pending != null && (current is DownloadStatus.Idle || current is DownloadStatus.Failed) ->
        DownloadStatus.Waiting(pending.id, pending.allowMetered)
    pending == null && (current is DownloadStatus.Waiting || current is DownloadStatus.Running) -> DownloadStatus.Idle
    else -> current
}

/**
 * Model downloads: one at a time, started only by the user's tap, as a user-initiated data
 * transfer job (Android 14+), so they wait for an allowed network, pause when it goes, resume
 * where they stopped, and show in the system's Task Manager. No foreground service.
 */
object ModelDownloads {
    const val KEY_ID = "id"
    const val KEY_METERED = "metered"

    const val ERROR_CHECKSUM = "checksum"
    const val ERROR_SPACE = "space"
    const val ERROR_UNKNOWN = "unknown"

    /** Its own JobScheduler namespace: the job id cannot meet one of WorkManager's. */
    private const val NAMESPACE = "local-model"
    private const val JOB_ID = 1
    private const val BACKOFF_MS = 30_000L

    private val _status = MutableStateFlow<DownloadStatus>(DownloadStatus.Idle)
    val status: StateFlow<DownloadStatus> = _status.asStateFlow()

    internal fun publish(status: DownloadStatus) {
        _status.value = status
    }

    @RequiresApi(Build.VERSION_CODES.UPSIDE_DOWN_CAKE)
    private fun scheduler(context: Context): JobScheduler =
        context.getSystemService(JobScheduler::class.java).forNamespace(NAMESPACE)

    /**
     * Schedules the download. Must run while the app is on screen (the user's tap): the system
     * refuses a user-initiated job otherwise, which shows as a failed download.
     */
    fun start(context: Context, store: LocalModelStore, id: String, allowMetered: Boolean) {
        val model = store.info(id)
        if (!downloadSupported() || model == null) {
            publish(DownloadStatus.Failed(id, ERROR_UNKNOWN))
            return
        }
        val job = JobInfo.Builder(JOB_ID, ComponentName(context, ModelDownloadJob::class.java))
            .setUserInitiated(true)
            // NOT_ROAMING: any network but a roaming one; the download checks Data Saver itself.
            .setRequiredNetworkType(if (allowMetered) JobInfo.NETWORK_TYPE_NOT_ROAMING else JobInfo.NETWORK_TYPE_UNMETERED)
            .setEstimatedNetworkBytes((model.bytes - partialBytes(store, id)).coerceAtLeast(0), 0)
            .setRequiresStorageNotLow(true)
            .setRequiresBatteryNotLow(true)
            .setBackoffCriteria(BACKOFF_MS, JobInfo.BACKOFF_POLICY_EXPONENTIAL)
            .setExtras(PersistableBundle().apply {
                putString(KEY_ID, id)
                putBoolean(KEY_METERED, allowMetered)
            })
            .build()
        val scheduled = scheduler(context).schedule(job) == JobScheduler.RESULT_SUCCESS
        publish(if (scheduled) DownloadStatus.Waiting(id, allowMetered) else DownloadStatus.Failed(id, ERROR_UNKNOWN))
    }

    /** Re-reads the job JobScheduler holds (after a restart of the app, or a stop from the Task Manager). */
    fun refresh(context: Context) {
        if (!downloadSupported()) return
        val pending = scheduler(context).getPendingJob(JOB_ID)?.extras?.let { extras ->
            extras.getString(KEY_ID)?.let { PendingDownload(it, extras.getBoolean(KEY_METERED, false)) }
        }
        publish(reconcile(_status.value, pending))
    }

    /** Stops the download and deletes what arrived. */
    fun cancel(context: Context, store: LocalModelStore, id: String) {
        if (downloadSupported()) scheduler(context).cancel(JOB_ID)
        publish(DownloadStatus.Idle)
        store.partial(id).delete()
        store.etag(id).delete()
        store.changed()
    }
}

/** A model file's size for people: "2.8 GB". */
fun formatModelSize(bytes: Long): String = "%.1f GB".format(bytes / 1_000_000_000.0)

/** The part of a download already on the phone (the .part file), in bytes. */
fun partialBytes(store: LocalModelStore, id: String): Long = store.partial(id).let { if (it.isFile) it.length() else 0L }
