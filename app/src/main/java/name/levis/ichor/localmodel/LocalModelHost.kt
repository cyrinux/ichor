package name.levis.ichor.localmodel

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.data.TalosJson
import name.levis.ichorgo.LocalModel
import name.levis.ichorgo.LocalModelSink

@Serializable
private data class GenerateOptions(
    val contextTokens: Int = 8192,
    val answerTokens: Int = 1024,
    val temperature: Double = 0.3,
    val assistantPrefix: String = "",
)

@Serializable
private data class Turn(val role: String, val content: String)

/**
 * The Go core's LocalModel: loads the model the user picked on the first question, keeps it
 * a minute after the last one, and frees it when the app goes to the background or the
 * system is short of memory. One question at a time (the Go core asks them so).
 */
class LocalModelHost(private val store: LocalModelStore, private val scope: CoroutineScope) : LocalModel {
    private class Loaded(val handle: Long, val modelId: String, val contextTokens: Int)

    private val lock = Any()
    private var loaded: Loaded? = null
    private var unloadJob: Job? = null

    @Volatile private var running = 0L

    @Volatile private var cancelled = false

    override fun generate(modelID: String, optionsJSON: String, system: String, messagesJSON: String, sink: LocalModelSink): String {
        cancelled = false
        if (!LlamaNative.available) return "no-engine"
        val file = store.file(modelID) ?: return "not-downloaded"
        val options = runCatching { TalosJson.decodeFromString(GenerateOptions.serializer(), optionsJSON) }.getOrElse { GenerateOptions() }
        val turns = runCatching { TalosJson.decodeFromString(ListSerializer(Turn.serializer()), messagesJSON) }.getOrElse { return "unreadable question" }

        synchronized(lock) {
            unloadJob?.cancel()
            val model = loaded?.takeIf { it.modelId == modelID && it.contextTokens == options.contextTokens }
                ?: run {
                    freeLocked()
                    val error = arrayOfNulls<String>(1)
                    val handle = LlamaNative.load(file.path, options.contextTokens, threads(), error)
                    if (handle == 0L) return error[0] ?: "the model could not be loaded"
                    Loaded(handle, modelID, options.contextTokens).also { loaded = it }
                }
            if (cancelled) return ""
            running = model.handle
            try {
                return LlamaNative.generate(
                    model.handle,
                    arrayOf("system") + turns.map { it.role },
                    arrayOf(system.toByteArray()) + turns.map { it.content.toByteArray() },
                    options.assistantPrefix.toByteArray(),
                    options.answerTokens,
                    options.temperature.toFloat(),
                ) { bytes -> sink.onPiece(String(bytes, Charsets.UTF_8)) }
            } finally {
                running = 0L
                unloadJob = scope.launch {
                    delay(KEEP_LOADED_MS)
                    unload()
                }
            }
        }
    }

    override fun cancel() {
        cancelled = true
        running.takeIf { it != 0L }?.let(LlamaNative::cancel)
    }

    /** Frees the model once the answer being written, if any, is done (it is loaded again on the next question). */
    fun unload() {
        synchronized(lock) {
            unloadJob?.cancel()
            freeLocked()
        }
    }

    /** The system is short of memory: stop the answer and free the model at once. */
    fun release() {
        cancel()
        unload()
    }

    private fun freeLocked() {
        loaded?.let { LlamaNative.free(it.handle) }
        loaded = null
    }

    private fun threads() = (Runtime.getRuntime().availableProcessors() / 2).coerceIn(2, 4)

    companion object {
        private const val KEEP_LOADED_MS = 60_000L
    }
}
