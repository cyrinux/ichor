package name.levis.ichor.ui.insights

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.data.SecureStore
import name.levis.ichor.data.StreamItem
import name.levis.ichor.data.TalosJson
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.*
import name.levis.ichor.ui.userMessage
import name.levis.ichorgo.Ichorgo
import java.io.File

/** One encrypted baseline and recording per cluster and screenshot mode; no cloud export. */
class InsightsStore(context: Context, private val scope: String) {
    private val directory = File(context.noBackupFilesDir, "insights").apply { mkdirs() }
    private fun store(kind: String) = SecureStore(File(directory, "$scope-$kind"), "ichor-insights-$scope-$kind")
    fun read(kind: String): String? = store(kind).read()?.toString(Charsets.UTF_8)
    fun save(kind: String, json: String) = store(kind).write(json.toByteArray())
    fun delete(kind: String) = store(kind).clear()
}

data class InsightsState(
    val snapshot: DriftSnapshot? = null,
    val changes: List<DriftChange> = emptyList(),
    val baselineAt: Long? = null,
    val document: IncidentDocument? = null,
    val busy: Boolean = false,
    val recording: Boolean = false,
    val error: String? = null,
)

class InsightsViewModel(private val talos: TalosRepository, private val store: InsightsStore, private val cluster: String) : ViewModel() {
    private val mutable = MutableStateFlow(InsightsState())
    val state = mutable.asStateFlow()
    private var snapshotJSON: String? = null
    private var baselineJSON: String? = null
    private var recordingJob: Job? = null

    fun load() = viewModelScope.launch {
        try {
            val saved = withContext(Dispatchers.IO) { store.read("incident") to store.read("baseline") }
            baselineJSON = saved.second
            mutable.value = mutable.value.copy(document = saved.first?.let { TalosJson.decodeFromString(IncidentDocument.serializer(), it) }, baselineAt = saved.second?.let { TalosJson.decodeFromString(DriftSnapshot.serializer(), it).at })
            refresh()
        } catch (e: CancellationException) { throw e }
        catch (e: Exception) { mutable.value = mutable.value.copy(error = e.userMessage()) }
    }

    fun refresh() = viewModelScope.launch {
        if (mutable.value.busy) return@launch
        mutable.value = mutable.value.copy(busy = true, error = null)
        try {
            val raw = talos.driftSnapshot()
            val snapshot = TalosJson.decodeFromString(DriftSnapshot.serializer(), raw)
            check(snapshot.scope == cluster)
            val changes = compare(baselineJSON, raw)
            snapshotJSON = raw
            mutable.value = mutable.value.copy(snapshot = snapshot, changes = changes)
        } catch (e: CancellationException) { throw e }
        catch (e: Exception) { mutable.value = mutable.value.copy(error = e.userMessage()) }
        finally { mutable.value = mutable.value.copy(busy = false) }
    }

    private suspend fun compare(baseline: String?, current: String): List<DriftChange> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ListSerializer(DriftChange.serializer()), Ichorgo.compareDrift(baseline.orEmpty(), current))
    }

    fun saveBaseline() = viewModelScope.launch {
        val raw = snapshotJSON ?: return@launch
        try {
            withContext(Dispatchers.IO) { store.save("baseline", raw) }
            baselineJSON = raw
            mutable.value = mutable.value.copy(baselineAt = mutable.value.snapshot?.at, changes = compare(raw, raw))
        } catch (e: Exception) { if (e is CancellationException) throw e; mutable.value = mutable.value.copy(error = e.userMessage()) }
    }

    fun deleteBaseline() = viewModelScope.launch {
        try {
            withContext(Dispatchers.IO) { store.delete("baseline") }
            baselineJSON = null
            mutable.value = mutable.value.copy(baselineAt = null, changes = snapshotJSON?.let { compare(null, it) }.orEmpty())
        } catch (e: Exception) { if (e is CancellationException) throw e; mutable.value = mutable.value.copy(error = e.userMessage()) }
    }

    fun deleteRecording() = viewModelScope.launch {
        if (mutable.value.recording) return@launch
        try { withContext(Dispatchers.IO) { store.delete("incident") }; mutable.value = mutable.value.copy(document = null) }
        catch (e: Exception) { if (e is CancellationException) throw e; mutable.value = mutable.value.copy(error = e.userMessage()) }
    }

    fun stop() { recordingJob?.cancel() }
    fun start() {
        if (recordingJob?.isActive == true) return
        recordingJob = viewModelScope.launch {
            mutable.value = mutable.value.copy(recording = true, document = null, error = null)
            val pending = ArrayList<TalosEvent>()
            var pendingLost = 0
            var document = ""
            var observation: String? = null
            val deadline = android.os.SystemClock.elapsedRealtime() + 10 * 60 * 1000
            val events = launch {
                try {
                    talos.events(emptyList(), 0).collect { event ->
                        when (event) {
                            is StreamItem.Item -> {
                                if (pending.size >= 100) { pending.removeAt(0); pendingLost++ }
                                pending.add(event.value)
                            }
                            is StreamItem.Done -> event.error?.let {
                                mutable.value = mutable.value.copy(error = it)
                                pending.add(TalosEvent(node = "", id = "stream-${System.currentTimeMillis()}", at = System.currentTimeMillis(), kind = "recording", subject = "events", action = "unavailable", message = it, severity = "warning"))
                            }
                        }
                    }
                } catch (e: CancellationException) { throw e }
                catch (e: Exception) { mutable.value = mutable.value.copy(error = e.userMessage()) }
            }
            suspend fun checkpoint(raw: String) = withContext(NonCancellable) {
                val batch = pending.toMutableList(); pending.clear()
                if (pendingLost > 0) {
                    val now = System.currentTimeMillis()
                    batch.add(TalosEvent(id = "overflow-$now", at = now, kind = "recording", subject = "events", action = "overflow", message = "{\"discarded\":$pendingLost}", severity = "warning"))
                    pendingLost = 0
                }
                val updated = withContext(Dispatchers.IO) {
                    val json = Ichorgo.updateIncident(document, raw, TalosJson.encodeToString(ListSerializer(TalosEvent.serializer()), batch))
                    store.save("incident", json)
                    json
                }
                document = updated
                mutable.value = mutable.value.copy(document = TalosJson.decodeFromString(IncidentDocument.serializer(), updated))
            }
            try {
                while (android.os.SystemClock.elapsedRealtime() < deadline) {
                    val raw = talos.observation()
                    check(TalosJson.parseToJsonElement(raw).let { (it as kotlinx.serialization.json.JsonObject)["scope"]?.toString()?.trim('"') } == cluster)
                    observation = raw
                    checkpoint(raw)
                    delay(5000)
                }
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { mutable.value = mutable.value.copy(error = e.userMessage()) }
            finally {
                events.cancel()
                withContext(NonCancellable) {
                    try { observation?.let { checkpoint(it) } }
                    catch (e: Exception) { mutable.value = mutable.value.copy(error = e.userMessage()) }
                }
                mutable.value = mutable.value.copy(recording = false)
            }
        }
    }
}
