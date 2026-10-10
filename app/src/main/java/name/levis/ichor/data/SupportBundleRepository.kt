package name.levis.ichor.data

import name.levis.ichor.ui.goErrorText
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichorgo.SupportListener
import name.levis.ichorgo.Ichorgo
import name.levis.ichor.model.SupportProgress
import name.levis.ichor.model.isSupportBundleName
import java.io.File
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream

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
 * that directory unless the user saves or shares one. [history]: the cluster's history ring,
 * anonymised (null without one), added to each bundle as [HISTORY_ENTRY].
 */
class SupportBundleRepository(
    private val configs: ConfigRepository,
    private val kubeServers: KubeServers,
    filesDir: File,
    private val history: suspend () -> String? = { null },
) {
    val dir = File(filesDir, SUPPORT_DIR)

    /** Collects a bundle of [nodes] into [dest]. Cancelling the collector cancels it. */
    fun collect(nodes: List<String>, dest: File): Flow<SupportEvent> = callbackFlow {
        val stored = configs.forCall()
        dest.parentFile?.mkdirs()
        val run = Ichorgo.startSupportBundle(
            stored.yaml,
            stored.activeContext,
            kubeServers.serverFor(stored),
            nodes.joinToString(","),
            dest.path,
            object : SupportListener {
                override fun onProgress(json: String) {
                    runCatching { TalosJson.decodeFromString(SupportProgress.serializer(), json) }
                        .onSuccess { trySend(SupportEvent.Progress(it)) }
                }

                override fun onDone(path: String, size: Long, errMessage: String) {
                    if (errMessage.isNotEmpty()) {
                        trySend(SupportEvent.Failed(goErrorText(errMessage)))
                        close()
                        return
                    }
                    launch(Dispatchers.IO) {
                        // Best effort: a bundle without its history is still worth sending.
                        val export = runCatching { history() }.getOrNull()
                        val added = export != null && runCatching { addZipEntry(File(path), HISTORY_ENTRY, export.toByteArray()) }.isSuccess
                        trySend(SupportEvent.Done(path, if (added) File(path).length() else size))
                        close()
                    }
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
        const val HISTORY_ENTRY = "cluster/history.json"
        private const val STALE_PART_MILLIS = 60 * 60 * 1000L
    }
}

/**
 * Adds (or replaces) the entry [name] of the zip [zip]: the entries are copied to a new file,
 * which then takes its place, so a failure leaves the zip as it was.
 */
internal fun addZipEntry(zip: File, name: String, bytes: ByteArray) {
    val part = File(zip.path + ".add")
    try {
        ZipFile(zip).use { source ->
            ZipOutputStream(part.outputStream().buffered()).use { out ->
                source.entries().asSequence().filter { it.name != name }.forEach { entry ->
                    out.putNextEntry(ZipEntry(entry.name).apply { time = entry.time })
                    source.getInputStream(entry).use { it.copyTo(out) }
                    out.closeEntry()
                }
                out.putNextEntry(ZipEntry(name))
                out.write(bytes)
                out.closeEntry()
            }
        }
        check(part.renameTo(zip)) { "Could not replace the support bundle" }
    } finally {
        part.delete()
    }
}
