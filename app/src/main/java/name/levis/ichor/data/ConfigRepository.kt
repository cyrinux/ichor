package name.levis.ichor.data

import android.content.Context
import name.levis.ichor.model.isDemo
import name.levis.ichorgo.Ichorgo
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.EndpointMatch
import name.levis.ichor.model.ImportChoice
import name.levis.ichor.model.ImportConflict
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import java.io.File

/** The stored talosconfig (a context per imported cluster) plus the context the user is looking at. */
data class StoredConfig(
    val yaml: String,
    val summary: ConfigSummary,
    val activeContext: String,
)

val StoredConfig.activeSummary: ContextSummary?
    get() = summary.contexts.firstOrNull { it.name == activeContext }

/** The active cluster's fingerprint, to key its state: null for the demo or when unknown. */
val StoredConfig.realFingerprint: String?
    get() = activeSummary?.takeUnless { it.isDemo }?.fingerprint?.takeIf { it.isNotBlank() }

/** [guard] may hold back a call to the cluster on screen by throwing, e.g. off its VPN. */
class ConfigRepository(context: Context, private val guard: (StoredConfig) -> Unit = {}) {

    private val store = SecureStore(
        File(context.filesDir, "talosconfig.enc"),
        strongBoxAvailable = hasStrongBox(context.packageManager),
    )

    /** Where the key protecting the stored config lives (null before the first import). */
    fun keyProtection(): KeyProtection? = runCatching { store.protection() }.getOrNull()
    private val prefs = context.getSharedPreferences("ichor", Context.MODE_PRIVATE)

    private val writes = Mutex()

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

    /** Adds the local demo alongside any imported clusters, replacing a demo added before. */
    suspend fun saveDemo() = withContext(Dispatchers.IO) {
        val yaml = Ichorgo.demoConfig()
        save(yaml, importConflicts(yaml).filter { it.sameAs != null }.map { ImportChoice(it.index, replace = true) })
    }

    /** The contexts of [yaml] named like a stored one (none before the first import). */
    suspend fun importConflicts(yaml: String): List<ImportConflict> = withContext(Dispatchers.IO) {
        val current = _config.value ?: return@withContext emptyList()
        TalosJson.decodeFromString(ListSerializer(ImportConflict.serializer()), Ichorgo.importConflicts(current.yaml, yaml))
    }

    /**
     * Stores [yaml]. With a config already stored its contexts are added to it (one stored
     * talosconfig, a context per cluster). A stored context is never overwritten: one named
     * like it gets the name in [choices], else name-1, name-2…, unless [choices] asks to
     * replace the stored context of the same cluster (see [importConflicts]). The imported
     * config's current context becomes the one shown.
     */
    suspend fun save(yaml: String, choices: List<ImportChoice> = emptyList()) = writing {
        val merged = _config.value?.let {
            Ichorgo.mergeConfig(it.yaml, yaml, TalosJson.encodeToString(ListSerializer(ImportChoice.serializer()), choices))
        } ?: yaml
        commit(merged, parse(merged), active = null)
    }

    /**
     * Replaces the stored config with [yaml] (a restored backup) in one write, so a failure
     * leaves the previous one in place, and shows the context at [activeIndex].
     */
    suspend fun replace(yaml: String, activeIndex: Int) = writing {
        val summary = parse(yaml)
        commit(yaml, summary, contextAt(summary, activeIndex))
    }

    /** The position of the context on screen (contexts keep their order whether masked or not). */
    fun activeIndex(): Int = _config.value?.let { it.summary.indexOf(it.activeContext) } ?: -1

    /**
     * Removes the cluster [name] (a context and its credentials) from the stored config,
     * showing its neighbour if it was the active one. Removing the last one deletes the
     * stored config. Returns whether a config is still stored.
     */
    suspend fun removeContext(name: String): Boolean = writing {
        val current = _config.value ?: return@writing false
        val removed = current.summary.indexOf(name)
        if (removed < 0) return@writing true
        if (current.summary.contexts.size == 1) {
            clear()
            return@writing false
        }
        val remaining = Ichorgo.removeContext(current.yaml, name)
        val summary = parse(remaining)
        // By position: the masked names of screenshot mode may change with the set of contexts.
        val active = contextAt(
            summary,
            activeIndexAfterRemoval(current.summary.indexOf(current.activeContext), removed, summary.contexts.size),
        )
        commit(remaining, summary, active)
        true
    }

    /**
     * Replaces [contextName]'s CA, certificate and key with those of [generatedYaml] (a
     * single-context talosconfig from GenerateTalosconfig), keeping the other contexts and
     * the context the user is looking at. The result goes through the import validation.
     */
    suspend fun replaceCredentials(contextName: String, generatedYaml: String) = writing {
        val current = _config.value ?: throw NoConfigException()
        val merged = Ichorgo.replaceContextCredentials(current.yaml, generatedYaml, contextName)
        val summary = parse(merged)
        check(summary.contexts.any { it.name == current.activeContext }) { "context ${current.activeContext} disappeared" }
        commit(merged, summary, current.activeContext, remember = false)
    }

    /**
     * Adds [nodes] (addresses, as discovered) to the nodes of [contextName], keeping the
     * other contexts and the context the user is looking at.
     */
    suspend fun addNodes(contextName: String, nodes: List<String>) =
        edit { Ichorgo.addContextNodes(it, contextName, nodes.joinToString(",")) }

    /** Replaces the endpoints of [contextName] with [endpoints], in order. */
    suspend fun setEndpoints(contextName: String, endpoints: List<String>) =
        edit { Ichorgo.setContextEndpoints(it, contextName, endpoints.joinToString(",")) }

    /**
     * Puts each endpoint a network search found first among the endpoints of the contexts it
     * answered for, all in one write: nothing is stored if one fails.
     */
    suspend fun addEndpoints(found: List<EndpointMatch>) = edit { yaml ->
        found.fold(yaml) { acc, match ->
            match.contexts.fold(acc) { config, name -> Ichorgo.addContextEndpoint(config, name, match.endpoint) }
        }
    }

    /**
     * Runs a change of the stored config off the main thread, one at a time: each reads the
     * stored config, changes it and writes it back, so two at once would lose one of them.
     */
    private suspend fun <T> writing(block: suspend CoroutineScope.() -> T): T =
        writes.withLock { withContext(Dispatchers.IO, block) }

    /** Stores the stored config changed by [change], keeping the context the user is looking at. */
    private suspend fun edit(change: (String) -> String) = writing {
        val current = _config.value ?: throw NoConfigException()
        val updated = change(current.yaml)
        commit(updated, parse(updated), current.activeContext, remember = false)
    }

    /**
     * Writes [yaml] as the stored config and shows [active] (null: the config's own current
     * context), which [remember] also saves as the context to come back to.
     */
    private suspend fun commit(yaml: String, summary: ConfigSummary, active: String?, remember: Boolean = true) {
        val shown = active ?: summary.current
        store.write(yaml.encodeToByteArray())
        if (remember) saveActive(summary, shown)
        _config.value = StoredConfig(yaml, summary, shown)
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
        TalosJson.decodeFromString(ConfigSummary.serializer(), Ichorgo.parseConfig(yaml))

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
