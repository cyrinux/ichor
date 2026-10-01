package name.levis.ichor.ui.capture

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.data.CaptureEvent
import name.levis.ichor.data.CaptureHandle
import name.levis.ichor.data.CaptureRepository
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.CaptureOptions
import name.levis.ichor.model.LinkInfo
import name.levis.ichor.model.MAX_LIVE_PACKETS
import name.levis.ichor.model.PacketSummary
import name.levis.ichor.model.appendCapped
import name.levis.ichor.model.captureFileName
import name.levis.ichor.model.defaultCaptureInterface
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.uiText
import java.io.File

/** The node's links, to pick the capture interface from. */
class CaptureLinksViewModel(private val talos: TalosRepository, private val node: String) : LoadingViewModel<List<LinkInfo>>() {
    override suspend fun fetch(): List<LinkInfo> = talos.network(node).links
}

/** A capture started from this screen; [finishedAt] 0 while it runs. */
data class CaptureSession(
    val file: File,
    val options: CaptureOptions,
    val startedAt: Long,
    val packets: List<PacketSummary> = emptyList(),
    val packetCount: Long = 0,
    val bytes: Long = 0,
    val finishedAt: Long = 0,
    val error: UiText? = null,
) {
    val running: Boolean get() = finishedAt == 0L

    /** Timestamp of the first packet: list times are relative to it. */
    val firstTs: Long get() = packets.firstOrNull()?.ts ?: 0
}

/** Pause between two applied batches of packets, so bursts redraw a few times only. */
private const val PACKET_BATCH_MS = 150L

/** Debounce of the filter validation while typing. */
private const val FILTER_CHECK_MS = 250L

class CaptureViewModel(
    private val captures: CaptureRepository,
    private val node: String,
    private val hostname: String,
) : ViewModel() {
    private val _options = MutableStateFlow(CaptureOptions())
    val options: StateFlow<CaptureOptions> = _options.asStateFlow()

    /** The Go core's verdict on the current filter, "" when valid. */
    private val _filterError = MutableStateFlow("")
    val filterError: StateFlow<String> = _filterError.asStateFlow()

    /** A filter check is pending (start waits for it). */
    private val _filterChecking = MutableStateFlow(false)
    val filterChecking: StateFlow<Boolean> = _filterChecking.asStateFlow()

    private val _session = MutableStateFlow<CaptureSession?>(null)
    val session: StateFlow<CaptureSession?> = _session.asStateFlow()

    private var handle: CaptureHandle? = null
    private var job: Job? = null
    private var filterJob: Job? = null
    private var stopped = false

    fun update(transform: (CaptureOptions) -> CaptureOptions) {
        val before = _options.value
        _options.update(transform)
        if (_options.value.filter != before.filter) checkFilter(_options.value.filter)
    }

    /** Picks the default interface once the links are known, unless one is chosen. */
    fun linksLoaded(links: List<LinkInfo>) {
        if (_options.value.iface.isEmpty()) defaultCaptureInterface(links)?.let { iface -> update { it.copy(iface = iface) } }
    }

    private fun checkFilter(filter: String) {
        filterJob?.cancel()
        if (filter.isBlank()) {
            _filterError.value = ""
            _filterChecking.value = false
            return
        }
        _filterChecking.value = true
        filterJob = viewModelScope.launch {
            delay(FILTER_CHECK_MS)
            _filterError.value = withContext(Dispatchers.Default) { captures.validateFilter(filter) }
            _filterChecking.value = false
        }
    }

    fun start() {
        if (_session.value?.running == true) return
        val opts = _options.value
        val file = File(captures.dir, captureFileName(hostname, opts.iface))
        val started = CaptureSession(file, opts, System.currentTimeMillis())
        _session.value = started
        stopped = false
        job = viewModelScope.launch {
            try {
                val h = captures.start(node, opts, file)
                handle = h
                if (stopped) h.stop() // stopped while starting
                collect(h)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Throwable) {
                finish(error = e.uiText())
            }
        }
    }

    private suspend fun collect(h: CaptureHandle) {
        while (true) {
            val first = h.events.receiveCatching().getOrNull() ?: break
            // Everything already queued joins the batch.
            val batch = buildList {
                add(first)
                while (true) add(h.events.tryReceive().getOrNull() ?: break)
            }
            apply(batch)
            if (batch.any { it is CaptureEvent.Done }) break
            delay(PACKET_BATCH_MS)
        }
        if (_session.value?.running == true) finish(error = null)
    }

    private fun apply(batch: List<CaptureEvent>) {
        val packets = batch.filterIsInstance<CaptureEvent.Packet>().map { it.summary }
        val stats = batch.filterIsInstance<CaptureEvent.Stats>().lastOrNull()
        val done = batch.filterIsInstance<CaptureEvent.Done>().lastOrNull()
        _session.update { s ->
            s?.copy(
                packets = s.packets.appendCapped(packets, MAX_LIVE_PACKETS),
                packetCount = done?.packets ?: stats?.packets ?: (s.packetCount + packets.size),
                bytes = done?.bytes ?: stats?.bytes ?: s.bytes,
            )
        }
        // Stopping early is not a failure, whatever the core reports for it.
        done?.let { finish(error = it.error?.takeUnless { stopped }?.let(UiText::Raw)) }
    }

    private fun finish(error: UiText?) {
        handle = null
        _session.update { it?.copy(finishedAt = System.currentTimeMillis(), error = error) }
    }

    /** Ends the capture early; the packets so far are kept in the file. */
    fun stop() {
        stopped = true
        handle?.stop?.invoke()
    }

    /** Back to the setup, keeping the saved file. */
    fun reset() {
        if (_session.value?.running == true) return
        job = null
        _session.value = null
    }

    fun deleteCurrent() {
        val s = _session.value ?: return
        if (s.running) return
        viewModelScope.launch {
            captures.delete(s.file)
            _session.value = null
        }
    }

    override fun onCleared() {
        // Leaving the screen ends the capture (the file keeps what was captured).
        handle?.stop?.invoke()
    }
}
