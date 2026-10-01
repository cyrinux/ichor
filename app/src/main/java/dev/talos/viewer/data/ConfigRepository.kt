package dev.talos.viewer.data

import android.content.Context
import dev.talos.talosmobile.Talosmobile
import dev.talos.viewer.model.ConfigSummary
import dev.talos.viewer.model.ContextSummary
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import java.io.File

/** The imported talosconfig plus the context the user is looking at. */
data class StoredConfig(
    val yaml: String,
    val summary: ConfigSummary,
    val activeContext: String,
)

val StoredConfig.activeSummary: ContextSummary?
    get() = summary.contexts.firstOrNull { it.name == activeContext }

class ConfigRepository(context: Context) {

    private val store = SecureStore(File(context.filesDir, "talosconfig.enc"))
    private val prefs = context.getSharedPreferences("talos-viewer", Context.MODE_PRIVATE)

    private val _config = MutableStateFlow<StoredConfig?>(null)
    val config: StateFlow<StoredConfig?> = _config.asStateFlow()

    /** Loads the stored config, if any. Returns null when nothing was imported yet. */
    suspend fun load(): StoredConfig? = withContext(Dispatchers.IO) {
        val bytes = runCatching { store.read() }.getOrNull() ?: return@withContext null
        val yaml = bytes.decodeToString()
        val summary = runCatching { parse(yaml) }.getOrNull() ?: return@withContext null
        val stored = StoredConfig(yaml, summary, resolveActive(summary, prefs.getString(KEY_CONTEXT, null)))
        _config.value = stored
        stored
    }

    /** Validates without storing; throws with a readable message when invalid. */
    suspend fun validate(yaml: String): ConfigSummary = withContext(Dispatchers.IO) { parse(yaml) }

    suspend fun save(yaml: String) = withContext(Dispatchers.IO) {
        val summary = parse(yaml)
        store.write(yaml.encodeToByteArray())
        prefs.edit().putString(KEY_CONTEXT, summary.current).apply()
        _config.value = StoredConfig(yaml, summary, summary.current)
    }

    fun selectContext(name: String) {
        val current = _config.value ?: return
        if (current.summary.contexts.none { it.name == name }) return
        prefs.edit().putString(KEY_CONTEXT, name).apply()
        _config.value = current.copy(activeContext = name)
    }

    suspend fun clear() = withContext(Dispatchers.IO) {
        store.clear()
        prefs.edit().clear().apply()
        _config.value = null
    }

    private fun parse(yaml: String): ConfigSummary =
        TalosJson.decodeFromString(ConfigSummary.serializer(), Talosmobile.parseConfig(yaml))

    private fun resolveActive(summary: ConfigSummary, saved: String?): String =
        saved?.takeIf { name -> summary.contexts.any { it.name == name } } ?: summary.current

    private companion object {
        const val KEY_CONTEXT = "active_context"
    }
}
