package name.levis.ichor.data

import android.content.Context
import name.levis.ichorgo.Ichorgo
import name.levis.ichor.model.ConfigSummary
import name.levis.ichor.model.EndpointMatch
import name.levis.ichor.model.ImportChoice
import name.levis.ichor.model.ImportConflict
import name.levis.ichor.model.isKube
import kotlinx.coroutines.CancellationException
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

/** A config is stored but could not be read: the Keystore is busy or lost its key, or the file is damaged. */
class ConfigUnreadableException(cause: Throwable) : Exception("Stored config could not be read", cause)

/** [guard] may hold back a call to the cluster on screen by throwing, e.g. off its VPN. */
class ConfigRepository(context: Context, private val guard: (StoredConfig) -> Unit = {}) {

    private val strongBox = hasStrongBox(context.packageManager)
    private val store = SecureStore(File(context.filesDir, "talosconfig.enc"), strongBoxAvailable = strongBox)

    /** Clusters added from a kubeconfig: a second sealed file with a key of its own, protected alike. */
    private val kubeStore = SecureStore(File(context.filesDir, "kubeconfig.enc"), keyAlias = "kubeconfig", strongBoxAvailable = strongBox)

    /** Where the key protecting the stored config lives (null before the first import). */
    fun keyProtection(): KeyProtection? = runCatching { store.protection() ?: kubeStore.protection() }.getOrNull()
    private val prefs = context.getSharedPreferences("ichor", Context.MODE_PRIVATE)

    private val writes = Mutex()

    private val _config = MutableStateFlow<StoredConfig?>(null)
    val config: StateFlow<StoredConfig?> = _config.asStateFlow()

    /** Bumped on import/delete, so caches keyed on it drop data from an older config. */
    private val _generation = MutableStateFlow(0)
    val generation: StateFlow<Int> = _generation.asStateFlow()

    /**
     * Loads the stored config, if any. Returns null only when nothing was imported yet (neither
     * a talosconfig nor a kubeconfig). A read that fails is tried again (the Keystore may be busy
     * for a moment, e.g. while the app starts after an update) and then throws
     * [ConfigUnreadableException]: the config may still be there, so it must not be taken for a
     * first start.
     */
    suspend fun load(): StoredConfig? = writing {
        // Already loaded by another caller (the monitor, the widget) while this one waited.
        _config.value?.let { return@writing it }
        val stored = try {
            retrying { readStored() }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            throw ConfigUnreadableException(e)
        } ?: return@writing null
        _config.value = stored
        stored
    }

    private fun readStored(): StoredConfig? {
        val talos = store.read()?.decodeToString()
        val kube = kubeStore.read()?.decodeToString()
        if (talos == null && kube == null) return null
        val summary = parse(talos.orEmpty(), kube.orEmpty())
        return StoredConfig(talos.orEmpty(), kube.orEmpty(), summary, resolveActive(summary, prefs.getString(KEY_CONTEXT, null), savedIndex()))
    }

    /** Validates a talosconfig without storing it; throws with a readable message when invalid. */
    suspend fun validate(yaml: String): ConfigSummary = withContext(Dispatchers.IO) { parseTalos(yaml) }

    /** A kubeconfig's contexts, each importable or saying why not; throws when it is not a kubeconfig. */
    suspend fun validateKube(yaml: String): ConfigSummary = withContext(Dispatchers.IO) { parseKube(yaml) }

    /** Adds the local demo alongside any imported clusters, replacing a demo added before. */
    suspend fun saveDemo() = withContext(Dispatchers.IO) {
        val yaml = Ichorgo.demoConfig()
        save(yaml, importConflicts(yaml).filter { it.sameAs != null }.map { ImportChoice(it.index, replace = true) })
    }

    /** The contexts of the talosconfig [yaml] named like a stored cluster of either kind (none before the first import). */
    suspend fun importConflicts(yaml: String): List<ImportConflict> = withContext(Dispatchers.IO) {
        val current = _config.value ?: return@withContext emptyList()
        decodeConflicts(Ichorgo.talosImportConflicts(current.talosYaml, current.kubeYaml, yaml))
    }

    /** The contexts of the kubeconfig [yaml] named like a stored cluster of either kind. */
    suspend fun kubeImportConflicts(yaml: String): List<ImportConflict> = withContext(Dispatchers.IO) {
        val current = _config.value ?: return@withContext emptyList()
        decodeConflicts(Ichorgo.kubeImportConflicts(current.kubeYaml, current.talosYaml, yaml))
    }

    /**
     * Stores the talosconfig [yaml]. With a config already stored its contexts are added to it
     * (one stored talosconfig, a context per cluster). A stored context is never overwritten:
     * one named like a stored cluster (of either kind) gets the name in [choices], else name-1,
     * name-2…, unless [choices] asks to replace the stored context of the same cluster (see
     * [importConflicts]). The imported config's current context becomes the one shown.
     */
    suspend fun save(yaml: String, choices: List<ImportChoice> = emptyList()) = writing {
        val current = _config.value
        val talos = current?.let { Ichorgo.mergeTalosconfig(it.talosYaml, it.kubeYaml, yaml, encodeChoices(choices)) } ?: yaml
        val kube = current?.kubeYaml.orEmpty()
        val talosSummary = parseTalos(talos)
        commit(talos, kube, mergeSummaries(talosSummary, parseKubeOrNull(kube)), active = talosSummary.current)
    }

    /**
     * Stores the contexts of the kubeconfig [yaml] the user kept (see [ImportChoice.skip]), next
     * to the stored ones, named like [save] does. Its current context (or the first one kept)
     * becomes the one shown. Throws when no context can be added.
     */
    suspend fun saveKube(yaml: String, choices: List<ImportChoice>) = writing {
        val current = _config.value
        val talos = current?.talosYaml.orEmpty()
        val kube = Ichorgo.mergeKubeconfig(current?.kubeYaml.orEmpty(), talos, yaml, encodeChoices(choices))
        val kubeSummary = parseKube(kube)
        commit(talos, kube, mergeSummaries(parseTalosOrNull(talos), kubeSummary), active = kubeSummary.current)
    }

    /**
     * Replaces the stored configs with [talos] and [kube] (a restored backup, either may be "")
     * and shows the context at [activeIndex]. Both are read before anything is written, so an
     * invalid one leaves the previous configs in place.
     */
    suspend fun replace(talos: String, kube: String, activeIndex: Int) = writing {
        require(talos.isNotBlank() || kube.isNotBlank()) { "no cluster to restore" }
        val summary = parse(talos, kube)
        commit(talos, kube, summary, contextAt(summary, activeIndex))
    }

    /** The position of the context on screen (contexts keep their order whether masked or not). */
    fun activeIndex(): Int = _config.value?.let { it.summary.indexOf(it.activeContext) } ?: -1

    /**
     * Removes the cluster [name] (a context and its credentials) from the config holding it,
     * showing its neighbour if it was the active one. Removing the last one deletes the
     * stored configs. Returns whether a config is still stored.
     */
    suspend fun removeContext(name: String): Boolean = writing {
        val current = _config.value ?: return@writing false
        val removed = current.summary.indexOf(name)
        if (removed < 0) return@writing true
        if (current.summary.contexts.size == 1) {
            clear()
            return@writing false
        }
        val (talos, kube) = withoutContext(current, name, current.summary.contexts[removed].isKube)
        val summary = parse(talos, kube)
        // By position: the masked names of screenshot mode may change with the set of contexts.
        val active = contextAt(
            summary,
            activeIndexAfterRemoval(current.summary.indexOf(current.activeContext), removed, summary.contexts.size),
        )
        commit(talos, kube, summary, active)
        true
    }

    /** The stored configs without [name]: the store holding it shrinks, or is emptied by its last context. */
    private fun withoutContext(current: StoredConfig, name: String, kube: Boolean): Pair<String, String> = when {
        kube -> current.talosYaml to Ichorgo.removeKubeContext(current.kubeYaml, name)
        talosContextCount(current.summary) == 1 -> "" to current.kubeYaml
        else -> Ichorgo.removeContext(current.talosYaml, name) to current.kubeYaml
    }

    /**
     * Replaces [contextName]'s CA, certificate and key with those of [generatedYaml] (a
     * single-context talosconfig from GenerateTalosconfig), keeping the other contexts and
     * the context the user is looking at. The result goes through the import validation.
     */
    suspend fun replaceCredentials(contextName: String, generatedYaml: String) = writing {
        val current = talosStored()
        val merged = Ichorgo.replaceContextCredentials(current.talosYaml, generatedYaml, contextName)
        val summary = parse(merged, current.kubeYaml)
        check(summary.contexts.any { it.name == current.activeContext }) { "context ${current.activeContext} disappeared" }
        commit(merged, current.kubeYaml, summary, current.activeContext, remember = false)
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

    /**
     * Stores the talosconfig changed by [change], keeping the context the user is looking at.
     * Endpoints, nodes and credentials are Talos things: the kubeconfig is left as it is.
     */
    private suspend fun edit(change: (String) -> String) = writing {
        val current = talosStored()
        val updated = change(current.talosYaml)
        commit(updated, current.kubeYaml, parse(updated, current.kubeYaml), current.activeContext, remember = false)
    }

    /** The stored config, for a change of its talosconfig: there must be one. */
    private fun talosStored(): StoredConfig {
        val current = _config.value ?: throw NoConfigException()
        check(current.talosYaml.isNotBlank()) { "no talosconfig stored" }
        return current
    }

    /**
     * Writes [talos] and [kube] as the stored configs (a store left empty is deleted) and shows
     * [active] (null: the summary's current context), which [remember] also saves as the context
     * to come back to. Only a store that changed is written.
     */
    private suspend fun commit(talos: String, kube: String, summary: ConfigSummary, active: String?, remember: Boolean = true) {
        val shown = active ?: summary.current
        val previous = _config.value
        if (previous == null || previous.talosYaml != talos) persist(store, talos)
        if (previous == null || previous.kubeYaml != kube) persist(kubeStore, kube)
        if (remember) saveActive(summary, shown)
        _config.value = StoredConfig(talos, kube, summary, shown)
        _generation.value++
    }

    private fun persist(target: SecureStore, yaml: String) {
        if (yaml.isBlank()) target.clear() else target.write(yaml.encodeToByteArray())
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
     * Parses the stored configs again, keeping the selected context. Screenshot mode masks
     * the summary (context names, endpoints, servers), so it is re-read when the mask changes.
     */
    suspend fun reparse() = withContext(Dispatchers.IO) {
        val current = _config.value ?: return@withContext
        val summary = parse(current.talosYaml, current.kubeYaml)
        _config.value = current.copy(summary = summary, activeContext = contextAt(summary, current.summary.indexOf(current.activeContext)))
    }

    private fun savedIndex(): Int = prefs.getInt(KEY_CONTEXT_INDEX, -1)

    suspend fun clear() = withContext(Dispatchers.IO) {
        store.clear()
        kubeStore.clear()
        prefs.edit().clear().apply()
        _config.value = null
        _generation.value++
    }

    /** Both stores as one list; a store left empty ("") holds no cluster. */
    private fun parse(talos: String, kube: String): ConfigSummary = mergeSummaries(parseTalosOrNull(talos), parseKubeOrNull(kube))

    private fun parseTalos(yaml: String): ConfigSummary =
        TalosJson.decodeFromString(ConfigSummary.serializer(), Ichorgo.parseConfig(yaml))

    private fun parseKube(yaml: String): ConfigSummary =
        TalosJson.decodeFromString(ConfigSummary.serializer(), Ichorgo.parseKubeconfig(yaml))

    private fun parseTalosOrNull(yaml: String): ConfigSummary? = yaml.takeIf { it.isNotBlank() }?.let(::parseTalos)
    private fun parseKubeOrNull(yaml: String): ConfigSummary? = yaml.takeIf { it.isNotBlank() }?.let(::parseKube)

    private fun decodeConflicts(json: String): List<ImportConflict> =
        TalosJson.decodeFromString(ListSerializer(ImportConflict.serializer()), json)

    private fun encodeChoices(choices: List<ImportChoice>): String =
        TalosJson.encodeToString(ListSerializer(ImportChoice.serializer()), choices)

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
