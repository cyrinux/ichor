package name.levis.ichor.localmodel

import android.content.Context
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import name.levis.ichor.BuildConfig
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest

/** How one run of a model download ended. */
sealed interface DownloadOutcome {
    /** The model is on the phone, checked. */
    data object Done : DownloadOutcome

    /** Stopped by the network: the part file stays, the next run resumes it. */
    data object Retry : DownloadOutcome

    /** Will not get better by trying again ([ModelDownloads.ERROR_CHECKSUM], an HTTP status...). */
    data class Failed(val error: String) : DownloadOutcome
}

/**
 * Downloads a catalog model into its part file, resuming what is already there (Range +
 * If-Range on the ETag), checks the size and SHA-256 and moves it in place. [onProgress] gets
 * the bytes on the phone at each new percent.
 */
class ModelDownloader(private val context: Context, private val store: LocalModelStore) {

    suspend fun run(model: LocalModelInfo, allowMetered: Boolean, onProgress: suspend (done: Long) -> Unit): DownloadOutcome {
        if (currentDownloadBlock(context, allowMetered) != DownloadBlock.NONE) return DownloadOutcome.Retry
        return try {
            download(model, allowMetered, onProgress)
        } catch (e: IOException) {
            // A stopped run reports nothing: the socket error may only be the stop itself.
            currentCoroutineContext().ensureActive()
            // The network went or the server cut: the part file stays, the next run resumes it.
            DownloadOutcome.Retry
        }
    }

    private suspend fun download(model: LocalModelInfo, allowMetered: Boolean, onProgress: suspend (Long) -> Unit): DownloadOutcome {
        val part = store.partial(model.id)
        val etagFile = store.etag(model.id)
        store.dir.mkdirs()
        var have = if (part.isFile) part.length() else 0L
        if (have > model.bytes) {
            part.delete()
            have = 0
        }
        if (store.dir.usableSpace < (model.bytes - have) * 11 / 10) {
            return DownloadOutcome.Failed(ModelDownloads.ERROR_SPACE)
        }

        onProgress(have)
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
                HTTP_RANGE_NOT_SATISFIABLE -> {
                    part.delete()
                    return DownloadOutcome.Retry
                }
                else -> return DownloadOutcome.Failed("HTTP ${connection.responseCode}")
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
                        // A read can block until the timeout: a run stopped meanwhile must not
                        // write behind the next run's back.
                        currentCoroutineContext().ensureActive()
                        if (n < 0) break
                        out.write(buffer, 0, n)
                        digest.update(buffer, 0, n)
                        done += n
                        if (done > model.bytes) {
                            part.delete()
                            return DownloadOutcome.Failed(ModelDownloads.ERROR_CHECKSUM)
                        }
                        val percent = done * 100 / model.bytes
                        if (percent != shown) {
                            shown = percent
                            onProgress(done)
                        }
                        // The network may have changed under the download (Wi-Fi gone, Data Saver on).
                        if (done - checkedAt >= RECHECK_BYTES) {
                            checkedAt = done
                            if (currentDownloadBlock(context, allowMetered) != DownloadBlock.NONE) return DownloadOutcome.Retry
                        }
                    }
                }
            }
            if (done != model.bytes) return DownloadOutcome.Retry
        } finally {
            connection.disconnect()
        }

        val sha = digest.digest().joinToString("") { "%02x".format(it) }
        if (!sha.equals(model.sha256, ignoreCase = true)) {
            part.delete()
            etagFile.delete()
            return DownloadOutcome.Failed(ModelDownloads.ERROR_CHECKSUM)
        }
        val target = store.target(model.id) ?: return DownloadOutcome.Failed(ModelDownloads.ERROR_UNKNOWN)
        if (!part.renameTo(target)) throw IOException("could not store the model")
        etagFile.delete()
        store.changed()
        return DownloadOutcome.Done
    }

    private fun digestInto(digest: MessageDigest, input: InputStream) {
        val buffer = ByteArray(1024 * 1024)
        while (true) {
            val n = input.read(buffer)
            if (n < 0) return
            digest.update(buffer, 0, n)
        }
    }

    private companion object {
        const val HTTP_RANGE_NOT_SATISFIABLE = 416
        const val RECHECK_BYTES = 16L * 1024 * 1024
    }
}
