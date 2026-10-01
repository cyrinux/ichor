package name.levis.talosmobile.data

import android.content.Context
import name.levis.talosmobile.Talosmobile
import name.levis.talosmobile.model.ConfigSummary
import name.levis.talosmobile.model.ContextSummary
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

    private val store = SecureStore(
        File(context.filesDir, "talosconfig.enc"),
        strongBoxAvailable = hasStrongBox(context.packageManager),
    )

    /** Where the key protecting the stored config lives (null before the first import). */
    fun keyProtection(): KeyProtection? = runCatching { store.protection() }.getOrNull()
    private val prefs = context.getSharedPreferences("talosdev-mobile", Context.MODE_PRIVATE)

    private val _config = MutableStateFlow<StoredConfig?>(null)
    val config: StateFlow<StoredConfig?> = _config.asStateFlow()

    /** Bumped on import/delete, so caches keyed on it drop data from an older config. */
    private val _generation = MutableStateFlow(0)
    val generation: StateFlow<Int> = _generation.asStateFlow()

    /** Loads the stored config, if any. Returns null when nothing was imported yet. */
    suspend fun load(): StoredConfig? = withContext(Dispatchers.IO) {
        val bytes = runCatching { store.read() }.getOrNull() ?: return@withContext null
        val yaml = bytes.decodeToString()
        val summary = runCatching { parse(yaml) }.getOrNull() ?: return@withContext null
        val stored = StoredConfig(yaml, summary, resolveActive(summary, prefs.getString(KEY_CONTEXT, null), savedIndex()))
        _config.value = stored
        stored
    }

    /** Validates without storing; throws with a readable message when invalid. */
    suspend fun validate(yaml: String): ConfigSummary = withContext(Dispatchers.IO) { parse(yaml) }

    suspend fun save(yaml: String) = withContext(Dispatchers.IO) {
        val summary = parse(yaml)
        store.write(yaml.encodeToByteArray())
        prefs.edit().putString(KEY_CONTEXT, summary.current).putInt(KEY_CONTEXT_INDEX, summary.indexOf(summary.current)).apply()
        _config.value = StoredConfig(yaml, summary, summary.current)
        _generation.value++
    }

    /**
     * Replaces [contextName]'s CA, certificate and key with those of [generatedYaml] (a
     * single-context talosconfig from GenerateTalosconfig), keeping the other contexts and
     * the context the user is looking at. The result goes through the import validation.
     */
    suspend fun replaceCredentials(contextName: String, generatedYaml: String) = withContext(Dispatchers.IO) {
        val current = _config.value ?: throw NoConfigException()
        val merged = Talosmobile.replaceContextCredentials(current.yaml, generatedYaml, contextName)
        val summary = parse(merged)
        check(summary.contexts.any { it.name == current.activeContext }) { "context ${current.activeContext} disappeared" }
        store.write(merged.encodeToByteArray())
        _config.value = StoredConfig(merged, summary, current.activeContext)
        _generation.value++
    }

    fun selectContext(name: String) {
        val current = _config.value ?: return
        if (current.summary.contexts.none { it.name == name }) return
        prefs.edit().putString(KEY_CONTEXT, name).putInt(KEY_CONTEXT_INDEX, current.summary.indexOf(name)).apply()
        _config.value = current.copy(activeContext = name)
    }

    /**
     * Parses the stored config again, keeping the selected context. Screenshot mode masks
     * the summary (context names, endpoints), so it is re-read when the mask changes.
     */
    suspend fun reparse() = withContext(Dispatchers.IO) {
        val current = _config.value ?: return@withContext
        val summary = parse(current.yaml)
        _config.value = StoredConfig(current.yaml, summary, contextAt(summary, current.summary.indexOf(current.activeContext)))
    }

    private fun savedIndex(): Int = prefs.getInt(KEY_CONTEXT_INDEX, -1)

    suspend fun clear() = withContext(Dispatchers.IO) {
        store.clear()
        prefs.edit().clear().apply()
        _config.value = null
        _generation.value++
    }

    private fun parse(yaml: String): ConfigSummary =
        TalosJson.decodeFromString(ConfigSummary.serializer(), Talosmobile.parseConfig(yaml))

    private companion object {
        const val KEY_CONTEXT = "active_context"
        const val KEY_CONTEXT_INDEX = "active_context_index"
    }
}

private fun ConfigSummary.indexOf(name: String): Int = contexts.indexOfFirst { it.name == name }

/** The context at [index] (contexts keep their order whether masked or not), else the config's current one. */
internal fun contextAt(summary: ConfigSummary, index: Int): String =
    summary.contexts.getOrNull(index)?.name ?: summary.current

/**
 * The context to show on load. The saved name may be a masked one (selected in screenshot
 * mode), so the saved position wins; the name covers configs saved before positions were.
 */
internal fun resolveActive(summary: ConfigSummary, savedName: String?, savedIndex: Int): String =
    summary.contexts.getOrNull(savedIndex)?.name
        ?: savedName?.takeIf { name -> summary.contexts.any { it.name == name } }
        ?: summary.current
