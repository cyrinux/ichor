package name.levis.ichor.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.model.OLLAMA_CLUSTER_PROVIDER
import name.levis.ichor.model.OllamaForward
import name.levis.ichor.model.OllamaServerRef
import name.levis.ichorgo.Ichorgo

/**
 * The cluster's Ollama as the AI server: the Go core keeps one port-forward to a ready pod of
 * the chosen server (KubeOllamaForward), opened again before a question or a model list when
 * it died or its pod went away. Leaving the cluster stops it (CancelCalls, in Go), and so does
 * picking another provider (here).
 */
class AiForwardRepository(
    private val configs: ConfigRepository,
    private val kubeServers: KubeServers,
    private val prefs: () -> AiPreferences,
    private val scope: CoroutineScope,
) {
    private val _forward = MutableStateFlow<OllamaForward?>(null)

    /** The forward running now, as last seen; null when none. */
    val forward: StateFlow<OllamaForward?> = _forward.asStateFlow()

    init {
        scope.launch {
            prefs().settings.map { it.provider }.distinctUntilChanged().collect { provider ->
                if (provider != OLLAMA_CLUSTER_PROVIDER) stop()
            }
        }
    }

    /**
     * The base URL the AI calls get for [settings]: the stored one, or for the cluster's Ollama
     * the forward's, opened first if needed. Fails with a readable message (no server chosen,
     * no pod ready, no right to forward, the demo cluster).
     */
    suspend fun baseUrl(settings: AiSettings): String {
        if (settings.provider != OLLAMA_CLUSTER_PROVIDER) return settings.baseUrl
        val ref = OllamaServerRef.parse(settings.baseUrl) ?: return ""
        return connect(ref).baseUrl
    }

    /** Opens (or keeps) the forward to [ref] on the open cluster. */
    suspend fun connect(ref: OllamaServerRef): OllamaForward = withContext(Dispatchers.IO) {
        val target = kubeServers.targetFor(configs.forCall())
        val json = Ichorgo.kubeOllamaForward(target.yaml, target.context, target.server, ref.namespace, ref.server)
        TalosJson.decodeFromString(OllamaForward.serializer(), json).also { _forward.value = it }
    }

    /**
     * Points the AI diagnosis at [ref] once its forward opened: provider, server, and no model
     * (the server's newest answers until one is picked) when the server changed.
     */
    suspend fun use(ref: OllamaServerRef): OllamaForward {
        val forward = connect(ref)
        val p = prefs()
        val now = p.settings.value
        val same = now.baseUrls[OLLAMA_CLUSTER_PROVIDER] == ref.text
        val chosen = now.copy(enabled = true, provider = OLLAMA_CLUSTER_PROVIDER).withBaseUrl(ref.text)
        p.update(if (same) chosen else chosen.withModel(""))
        return forward
    }

    /** Reads whether the forward still runs (it may have ended in the background). */
    suspend fun refresh() {
        val json = withContext(Dispatchers.IO) { runCatching { Ichorgo.ollamaForwardState() }.getOrDefault("") }
        _forward.value = json.takeIf { it.isNotEmpty() }?.let { TalosJson.decodeFromString(OllamaForward.serializer(), it) }
    }

    /** Off the main thread: Go waits for a forward still opening. */
    fun stop() {
        _forward.value = null
        scope.launch(Dispatchers.IO) { runCatching { Ichorgo.stopOllamaForward() } }
    }
}
