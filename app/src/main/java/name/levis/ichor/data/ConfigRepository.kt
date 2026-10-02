package name.levis.ichor.data

import android.content.Context
import name.levis.talosmobile.Talosmobile
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import java.io.File

/** The stored talosconfig (a context per imported cluster) plus the context the user is looking at. */
data class StoredConfig(
    val yaml: String,
    val summary: ConfigSummary,
    val activeContext: String,
)

val StoredConfig.activeSummary: ContextSummary?
    get() = summary.contexts.firstOrNull { it.name == activeContext }

/** [guard] may hold back a call to the cluster on screen by throwing, e.g. off its VPN. */
class ConfigRepository(context: Context, private val guard: (StoredConfig) -> Unit = {}) {

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

    /**
     * Stores [yaml]. With a config already stored its contexts are added to it (one stored
     * talosconfig, a context per cluster): a context of an already imported cluster (same
     * name and CA) is updated, any other gets its own entry. The imported config's current
     * context becomes the one shown.
     */
    suspend fun save(yaml: String) = withContext(Dispatchers.IO) {
        val merged = _config.value?.let { Talosmobile.mergeConfig(it.yaml, yaml) } ?: yaml
        val summary = parse(merged)
        store.write(merged.encodeToByteArray())
        saveActive(summary, summary.current)
        _config.value = StoredConfig(merged, summary, summary.current)
        _generation.value++
    }

    /**
     * Removes the cluster [name] (a context and its credentials) from the stored config,
     * showing its neighbour if it was the active one. Removing the last one deletes the
     * stored config. Returns whether a config is still stored.
     */
    suspend fun removeContext(name: String): Boolean = withContext(Dispatchers.IO) {
        val current = _config.value ?: return@withContext false
        val removed = current.summary.indexOf(name)
        if (removed < 0) return@withContext true
        if (current.summary.contexts.size == 1) {
            clear()
            return@withContext false
        }
        val remaining = Talosmobile.removeContext(current.yaml, name)
        val summary = parse(remaining)
        // By position: the masked names of screenshot mode may change with the set of contexts.
        val active = contextAt(
            summary,
            activeIndexAfterRemoval(current.summary.indexOf(current.activeContext), removed, summary.contexts.size),
        )
        store.write(remaining.encodeToByteArray())
        saveActive(summary, active)
        _config.value = StoredConfig(remaining, summary, active)
        _generation.value++
        true
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

    /**
     * The config for a call to the cluster on screen. Throws [NoConfigException] without
     * one, or whatever [guard] throws when the call must not be tried (see [VpnRequiredException]).
     */
    fun forCall(): StoredConfig = (_config.value ?: throw NoConfigException()).also(guard)

    fun selectContext(name: String) {
        val current = _config.value ?: return
        if (current.summary.contexts.none { it.name == name }) return
        saveActive(current.summary, name)
        _config.value = current.copy(activeContext = name)
    }

    private fun saveActive(summary: ConfigSummary, name: String) =
        prefs.edit().putString(KEY_CONTEXT, name).putInt(KEY_CONTEXT_INDEX, summary.indexOf(name)).apply()

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

/** The context [step] positions after [active] (before it when negative), or null past either end. */
internal fun adjacentContext(summary: ConfigSummary, active: String, step: Int): String? {
    val index = summary.indexOf(active)
    if (index < 0 || step == 0) return null
    return summary.contexts.getOrNull(index + step)?.name
}

/**
 * Where the active context is once the one at [removed] is gone, among the [remaining]
 * ones: it keeps showing the same context, or the removed one's neighbour.
 */
internal fun activeIndexAfterRemoval(active: Int, removed: Int, remaining: Int): Int = when {
    active > removed -> active - 1
    active == removed -> minOf(removed, remaining - 1)
    else -> active
}
