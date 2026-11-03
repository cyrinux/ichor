package name.levis.ichor.model

/** The built-in Ollama plugin's id (go/plugin/builtin/ollama.yaml). */
const val OLLAMA_PLUGIN = "ollama"

/** The plugin card that lists the servers: its rows get the "Pull a model…" button. */
const val OLLAMA_SERVERS_CARD = "servers"

/** The ollama.com library, where the names a pull takes are listed. */
const val OLLAMA_LIBRARY_URL = "https://ollama.com/library"

/** A model name the Go core accepts for a pull (StartOllamaPull checks it again). */
private val MODEL_NAME = Regex("^[A-Za-z0-9._/-]+(:[A-Za-z0-9._-]+)?$")
private const val MODEL_NAME_MAX = 128

fun isOllamaModelName(name: String): Boolean =
    name.length in 1..MODEL_NAME_MAX && MODEL_NAME.matches(name) && ".." !in name

/** Where a pull is: Ollama's step and the bytes done of total over the layers seen so far. */
data class OllamaPullProgress(val status: String = "", val completed: Long = 0, val total: Long = 0) {
    /** The share done, null before the first layer (the bar is then indeterminate). */
    val fraction: Float? get() = if (total > 0) (completed.toFloat() / total).coerceIn(0f, 1f) else null
}

/** What a running pull says; [Done] and [Failed] end it. */
sealed interface OllamaPullEvent {
    data class Progress(val progress: OllamaPullProgress) : OllamaPullEvent
    data object Done : OllamaPullEvent
    data class Failed(val message: String) : OllamaPullEvent
}

/** A pull of [model] onto the server [namespace]/[server] and where it is. */
data class OllamaPullState(
    val namespace: String,
    val server: String,
    val model: String,
    val progress: OllamaPullProgress = OllamaPullProgress(),
    val running: Boolean = true,
    val done: Boolean = false,
    val error: String? = null,
) {
    fun isFor(namespace: String, server: String) = this.namespace == namespace && this.server == server
}
