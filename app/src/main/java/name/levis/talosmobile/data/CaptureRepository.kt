package name.levis.talosmobile.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.ReceiveChannel
import kotlinx.coroutines.withContext
import name.levis.talosmobile.CaptureListener
import name.levis.talosmobile.Talosmobile
import name.levis.talosmobile.model.CAPTURE_SNAP_LEN
import name.levis.talosmobile.model.CaptureOptions
import name.levis.talosmobile.model.PacketDetail
import name.levis.talosmobile.model.PacketSummary
import name.levis.talosmobile.model.PcapPage
import name.levis.talosmobile.model.isCaptureFileName
import java.io.File

/** Events of a running packet capture. [Done.error] is null when it ended normally or was stopped. */
sealed interface CaptureEvent {
    data class Packet(val summary: PacketSummary) : CaptureEvent
    data class Stats(val packets: Long, val bytes: Long) : CaptureEvent
    data class Done(val path: String, val packets: Long, val bytes: Long, val error: String?) : CaptureEvent
}

/** A running capture: its [events], and [stop] to end it early. */
class CaptureHandle(val events: ReceiveChannel<CaptureEvent>, val stop: () -> Unit)

/** A capture saved in the app's private storage. */
data class CaptureFile(val file: File, val size: Long, val modified: Long) {
    val name: String get() = file.name
}

/**
 * Packet captures (`talosctl pcap`) written by the Go core into the app's private
 * `captures/` directory, and reading them back. They may hold sensitive traffic, so they
 * never leave that directory unless the user saves or shares one.
 */
class CaptureRepository(private val configs: ConfigRepository, filesDir: File) {
    val dir = File(filesDir, CAPTURE_DIR)

    /** "" when [expression] is a valid capture filter, else why not. Local, no network. */
    fun validateFilter(expression: String): String =
        runCatching { Talosmobile.validateCaptureFilter(expression) }.getOrElse { it.message.orEmpty().ifEmpty { "invalid filter" } }

    /**
     * Starts capturing on [node] into [dest]. Events arrive on [CaptureHandle.events], which
     * closes after [CaptureEvent.Done]; [CaptureHandle.stop] ends the capture early (the file
     * is kept and Done still follows).
     */
    suspend fun start(node: String, options: CaptureOptions, dest: File): CaptureHandle = withContext(Dispatchers.IO) {
        val stored = configs.config.value ?: throw NoConfigException()
        dest.parentFile?.mkdirs()
        val events = Channel<CaptureEvent>(Channel.UNLIMITED) // never drop packets or the final Done
        val run = Talosmobile.startPacketCapture(
            stored.yaml,
            stored.activeContext,
            node,
            options.iface,
            options.filter.trim(),
            options.promiscuous,
            CAPTURE_SNAP_LEN,
            options.maxSeconds,
            options.maxBytes,
            dest.path,
            object : CaptureListener {
                override fun onPacket(summaryJson: String) {
                    runCatching { TalosJson.decodeFromString(PacketSummary.serializer(), summaryJson) }
                        .onSuccess { events.trySend(CaptureEvent.Packet(it)) }
                }

                override fun onStats(packets: Long, bytes: Long) {
                    events.trySend(CaptureEvent.Stats(packets, bytes))
                }

                override fun onDone(path: String, packets: Long, bytes: Long, errMessage: String) {
                    events.trySend(CaptureEvent.Done(path, packets, bytes, errMessage.ifEmpty { null }))
                    events.close()
                }
            },
        )
        CaptureHandle(events, run::cancel)
    }

    suspend fun read(file: File, offset: Int, limit: Int): PcapPage = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(PcapPage.serializer(), Talosmobile.readPcap(file.path, offset.toLong(), limit.toLong()))
    }

    suspend fun detail(file: File, index: Long): PacketDetail = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(PacketDetail.serializer(), Talosmobile.packetDetail(file.path, index))
    }

    /** Saved captures, newest first. */
    suspend fun list(): List<CaptureFile> = withContext(Dispatchers.IO) {
        // Partial files of a capture killed with the process (captures last 10 min at most).
        val stale = System.currentTimeMillis() - STALE_PART_MILLIS
        dir.listFiles { f -> f.name.endsWith(".pcap.part") && f.lastModified() < stale }?.forEach { it.delete() }
        dir.listFiles { f -> f.isFile && isCaptureFileName(f.name) }.orEmpty()
            .map { CaptureFile(it, it.length(), it.lastModified()) }
            .sortedByDescending { it.modified }
    }

    /** The saved capture called [name], or null if there is none (or the name is not one). */
    fun find(name: String): File? = File(dir, name).takeIf { isCaptureFileName(name) && it.isFile }

    suspend fun delete(file: File) = withContext(Dispatchers.IO) {
        if (file.parentFile == dir) file.delete()
    }

    companion object {
        const val CAPTURE_DIR = "captures"
        private const val STALE_PART_MILLIS = 60 * 60 * 1000L
    }
}
