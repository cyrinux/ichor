package name.levis.ichor.localmodel

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.pm.ServiceInfo
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.work.BackoffPolicy
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingWorkPolicy
import androidx.work.ForegroundInfo
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import name.levis.ichor.BuildConfig
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import java.io.FileOutputStream
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.util.concurrent.TimeUnit

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
        roaming = caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_ROAMING) == false,
        dataSaver = cm.restrictBackgroundStatus,
        allowMetered = allowMetered,
    )
}

/**
 * Model downloads: one at a time, started only by the user's tap, through WorkManager so they
 * wait for an allowed network, pause when it goes and resume where they stopped.
 */
object ModelDownloads {
    const val WORK = "local-model-download"
    const val KEY_ID = "id"
    const val KEY_METERED = "metered"
    const val KEY_DONE = "done"
    const val KEY_TOTAL = "total"
    const val KEY_ERROR = "error"

    /** Tags of the work: the model it downloads, and whether mobile data was allowed. */
    const val TAG_PREFIX = "model:"
    const val TAG_METERED = "metered"

    const val ERROR_CHECKSUM = "checksum"
    const val ERROR_SPACE = "space"
    const val ERROR_UNKNOWN = "unknown"

    fun start(context: Context, id: String, allowMetered: Boolean) {
        val constraints = Constraints.Builder()
            // NOT_ROAMING: any network but a roaming one; the worker checks Data Saver itself.
            .setRequiredNetworkType(if (allowMetered) NetworkType.NOT_ROAMING else NetworkType.UNMETERED)
            .setRequiresStorageNotLow(true)
            .setRequiresBatteryNotLow(true)
            .build()
        val request = OneTimeWorkRequestBuilder<ModelDownloadWorker>()
            .setConstraints(constraints)
            .setInputData(workDataOf(KEY_ID to id, KEY_METERED to allowMetered))
            .setBackoffCriteria(BackoffPolicy.LINEAR, 30, TimeUnit.SECONDS)
            .addTag(TAG_PREFIX + id)
            .apply { if (allowMetered) addTag(TAG_METERED) }
            .build()
        WorkManager.getInstance(context).enqueueUniqueWork(WORK, ExistingWorkPolicy.REPLACE, request)
    }

    /** Stops the download and deletes what arrived. */
    fun cancel(context: Context, store: LocalModelStore, id: String) {
        WorkManager.getInstance(context).cancelUniqueWork(WORK)
        store.partial(id).delete()
        store.etag(id).delete()
        store.changed()
    }
}

class ModelDownloadWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {

    private val store get() = (applicationContext as TalosApp).localModels

    override suspend fun doWork(): Result {
        val id = inputData.getString(ModelDownloads.KEY_ID).orEmpty()
        val model = store.info(id) ?: return Result.failure(workDataOf(ModelDownloads.KEY_ERROR to ModelDownloads.ERROR_UNKNOWN))
        val allowMetered = inputData.getBoolean(ModelDownloads.KEY_METERED, false)
        if (currentDownloadBlock(applicationContext, allowMetered) != DownloadBlock.NONE) return Result.retry()
        return try {
            download(model, allowMetered)
        } catch (e: IOException) {
            // The network went or the server cut: the part file stays, the next run resumes it.
            Result.retry()
        }
    }

    override suspend fun getForegroundInfo(): ForegroundInfo = foregroundInfo(0, 0)

    private suspend fun download(model: LocalModelInfo, allowMetered: Boolean): Result {
        val part = store.partial(model.id)
        val etagFile = store.etag(model.id)
        store.dir.mkdirs()
        var have = if (part.isFile) part.length() else 0L
        if (have > model.bytes) {
            part.delete()
            have = 0
        }
        if (store.dir.usableSpace < (model.bytes - have) * 11 / 10) {
            return Result.failure(workDataOf(ModelDownloads.KEY_ERROR to ModelDownloads.ERROR_SPACE))
        }

        setForeground(foregroundInfo(have, model.bytes))
        val digest = MessageDigest.getInstance("SHA-256")
        if (have > 0) part.inputStream().use { input -> digestInto(digest, input) }

        val connection = (URL(model.url).openConnection() as HttpURLConnection).apply {
            connectTimeout = 30_000
            readTimeout = 60_000
            setRequestProperty("User-Agent", "Ichor/${BuildConfig.VERSION_NAME}")
            if (have > 0) {
                setRequestProperty("Range", "bytes=$have-")
                etagFile.takeIf { it.isFile }?.readText()?.let { setRequestProperty("If-Range", it) }
            }
        }
        try {
            when (connection.responseCode) {
                HttpURLConnection.HTTP_PARTIAL -> Unit
                HttpURLConnection.HTTP_OK -> {
                    // The server sends the whole file: start over.
                    have = 0
                    digest.reset()
                }
                416 -> {
                    part.delete()
                    return Result.retry()
                }
                else -> return Result.failure(workDataOf(ModelDownloads.KEY_ERROR to "HTTP ${connection.responseCode}"))
            }
            connection.getHeaderField("ETag")?.let { etagFile.writeText(it) }

            var done = have
            var shown = -1L
            var checkedAt = done
            FileOutputStream(part, have > 0).use { out ->
                connection.inputStream.use { input ->
                    val buffer = ByteArray(256 * 1024)
                    while (true) {
                        currentCoroutineContext().ensureActive()
                        val n = input.read(buffer)
                        if (n < 0) break
                        out.write(buffer, 0, n)
                        digest.update(buffer, 0, n)
                        done += n
                        if (done > model.bytes) {
                            part.delete()
                            return Result.failure(workDataOf(ModelDownloads.KEY_ERROR to ModelDownloads.ERROR_CHECKSUM))
                        }
                        val percent = done * 100 / model.bytes
                        if (percent != shown) {
                            shown = percent
                            setProgress(workDataOf(ModelDownloads.KEY_DONE to done, ModelDownloads.KEY_TOTAL to model.bytes))
                            setForeground(foregroundInfo(done, model.bytes))
                        }
                        // The network may have changed under the download (Wi-Fi gone, Data Saver on).
                        if (done - checkedAt >= RECHECK_BYTES) {
                            checkedAt = done
                            if (currentDownloadBlock(applicationContext, allowMetered) != DownloadBlock.NONE) return Result.retry()
                        }
                    }
                }
            }
            if (done != model.bytes) return Result.retry()
        } finally {
            connection.disconnect()
        }

        val sha = digest.digest().joinToString("") { "%02x".format(it) }
        if (!sha.equals(model.sha256, ignoreCase = true)) {
            part.delete()
            etagFile.delete()
            return Result.failure(workDataOf(ModelDownloads.KEY_ERROR to ModelDownloads.ERROR_CHECKSUM))
        }
        val target = store.target(model.id) ?: return Result.failure(workDataOf(ModelDownloads.KEY_ERROR to ModelDownloads.ERROR_UNKNOWN))
        if (!part.renameTo(target)) throw IOException("could not store the model")
        etagFile.delete()
        store.changed()
        return Result.success()
    }

    private fun digestInto(digest: MessageDigest, input: java.io.InputStream) {
        val buffer = ByteArray(1024 * 1024)
        while (true) {
            val n = input.read(buffer)
            if (n < 0) return
            digest.update(buffer, 0, n)
        }
    }

    private fun foregroundInfo(done: Long, total: Long): ForegroundInfo {
        val manager = applicationContext.getSystemService(NotificationManager::class.java)
        if (manager.getNotificationChannel(CHANNEL_ID) == null) {
            manager.createNotificationChannel(
                NotificationChannel(CHANNEL_ID, applicationContext.getString(R.string.ai_local_channel), NotificationManager.IMPORTANCE_LOW),
            )
        }
        val percent = if (total > 0) (done * 100 / total).toInt() else 0
        val notification: Notification = NotificationCompat.Builder(applicationContext, CHANNEL_ID)
            .setSmallIcon(android.R.drawable.stat_sys_download)
            .setContentTitle(applicationContext.getString(R.string.ai_local_notification))
            .setProgress(100, percent, total == 0L)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .addAction(0, applicationContext.getString(R.string.common_cancel), WorkManager.getInstance(applicationContext).createCancelPendingIntent(id))
            .build()
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            ForegroundInfo(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC)
        } else {
            ForegroundInfo(NOTIFICATION_ID, notification)
        }
    }

    companion object {
        private const val CHANNEL_ID = "model-download"
        private const val NOTIFICATION_ID = 7301
        private const val RECHECK_BYTES = 16L * 1024 * 1024
    }
}

/** A model file's size for people: "2.8 GB". */
fun formatModelSize(bytes: Long): String = "%.1f GB".format(bytes / 1_000_000_000.0)

/** The part of a download already on the phone (the .part file), in bytes. */
fun partialBytes(store: LocalModelStore, id: String): Long = store.partial(id).let { if (it.isFile) it.length() else 0L }
