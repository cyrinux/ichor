package name.levis.talosmobile.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import name.levis.talosmobile.SupportListener
import name.levis.talosmobile.Talosmobile
import name.levis.talosmobile.model.SupportProgress
import name.levis.talosmobile.model.isSupportBundleName
import java.io.File

/** Events of a support bundle being collected; [Done] or [Failed] ends it. */
sealed interface SupportEvent {
    data class Progress(val progress: SupportProgress) : SupportEvent
    data class Done(val path: String, val size: Long) : SupportEvent
    data class Failed(val message: String) : SupportEvent
}

/** A bundle saved in the app's private storage. */
data class SupportBundleFile(val file: File, val size: Long, val modified: Long) {
    val name: String get() = file.name
}

/**
 * Support bundles (`talosctl support`) written by the Go core into the app's private
 * `support/` directory. They hold logs and cluster details, unmasked, so they never leave
 * that directory unless the user saves or shares one.
 */
class SupportBundleRepository(private val configs: ConfigRepository, filesDir: File) {
    val dir = File(filesDir, SUPPORT_DIR)

    /** Collects a bundle of [nodes] into [dest]. Cancelling the collector cancels it. */
    fun collect(nodes: List<String>, dest: File): Flow<SupportEvent> = callbackFlow {
        val stored = configs.config.value ?: throw NoConfigException()
        dest.parentFile?.mkdirs()
        val run = Talosmobile.startSupportBundle(
            stored.yaml,
            stored.activeContext,
            nodes.joinToString(","),
            dest.path,
            object : SupportListener {
                override fun onProgress(json: String) {
                    runCatching { TalosJson.decodeFromString(SupportProgress.serializer(), json) }
                        .onSuccess { trySend(SupportEvent.Progress(it)) }
                }

                override fun onDone(path: String, size: Long, errMessage: String) {
                    trySend(if (errMessage.isEmpty()) SupportEvent.Done(path, size) else SupportEvent.Failed(errMessage))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED) // never drop the final event

    /** Saved bundles, newest first. */
    suspend fun list(): List<SupportBundleFile> = withContext(Dispatchers.IO) {
        // Partial files of a collection killed with the process (one takes minutes at most).
        val stale = System.currentTimeMillis() - STALE_PART_MILLIS
        dir.listFiles { f -> f.name.endsWith(".zip.part") && f.lastModified() < stale }?.forEach { it.delete() }
        dir.listFiles { f -> f.isFile && isSupportBundleName(f.name) }.orEmpty()
            .map { SupportBundleFile(it, it.length(), it.lastModified()) }
            .sortedByDescending { it.modified }
    }

    suspend fun delete(file: File) = withContext(Dispatchers.IO) {
        if (file.parentFile == dir) file.delete()
    }

    /** Removes what a cancelled or failed run left behind for [dest]. */
    suspend fun discard(dest: File) = withContext(Dispatchers.IO) {
        if (dest.parentFile == dir) {
            dest.delete()
            File(dest.path + ".part").delete()
        }
    }

    companion object {
        const val SUPPORT_DIR = "support"
        private const val STALE_PART_MILLIS = 60 * 60 * 1000L
    }
}
