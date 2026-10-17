package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.withContext
import name.levis.ichor.model.ReadProgress
import name.levis.ichorgo.Ichorgo

/**
 * The JSON boundary to the Go core shared by [TalosRepository], [KubeRepository],
 * [GitOpsRepository] and [DataServicesRepository]: which credentials a call runs with, and the
 * results they keep. Every Go call blocks, so they run on IO. With a [pinned] context, every
 * call goes to that cluster whichever is on screen (the background monitor's reads). Every call
 * first waits for [idle]: none runs while screenshot mode switches (see [MaskSwitcher]).
 */
class GoCall(
    val configs: ConfigRepository,
    val kubeServers: KubeServers,
    offline: OfflineCache? = null,
    private val pinned: String? = null,
    private val idle: suspend () -> Unit = {},
) {
    val results = ResultCache(
        offline,
        // The mask switch too: a call that ran across one writes where nothing reads any more.
        scope = { "${MaskEpoch.value.value}|${configs.generation.value}|${pinned ?: configs.config.value?.activeContext}|" },
        // Never the demo's: its results stay in memory.
        cluster = { configs.config.value?.realFingerprint },
    )

    private fun stored(): StoredConfig = pinned?.let(configs::forCall) ?: configs.forCall()

    /** The talosconfig and context a Talos call goes to now (see [talos]); for the streams started outside a call. */
    fun talosTarget(): StoredConfig = stored()

    suspend fun <T : Any> remember(
        key: String,
        persistable: (T) -> Boolean = { true },
        block: suspend (last: () -> TalosRepository.Timed<T>?) -> T,
    ): T = results.remember(key, persistable, block)

    /** A Talos API call with the active (or pinned) talosconfig context. */
    suspend fun <T> talos(block: (config: String, context: String) -> T): T {
        idle()
        val stored = stored()
        return withContext(Dispatchers.IO) { block(stored.yaml, stored.activeContext) }
    }

    /**
     * A Kubernetes call with the Kubernetes API address the user set for the cluster ("" for the
     * kubeconfig's), through the cluster's Kubernetes access when set (K5, see [kubeTarget]).
     */
    suspend fun <T> kube(block: (config: String, context: String, kubeServer: String) -> T): T {
        idle()
        val target = kubeTarget()
        return withContext(Dispatchers.IO) { block(target.yaml, target.context, target.server) }
    }

    /** Where a Kubernetes call goes now (see [kube]); for the streams started outside a call. */
    fun kubeTarget(): KubeTarget = kubeServers.targetFor(stored())

    /** A call that needs both Talos and Kubernetes (maintenance, the admin kubeconfig): always the Talos path. */
    suspend fun <T> talosKube(block: (config: String, context: String, kubeServer: String) -> T): T {
        idle()
        val stored = stored()
        val server = kubeServers.serverFor(stored)
        return withContext(Dispatchers.IO) { block(stored.yaml, stored.activeContext, server) }
    }
}

/** A repository over [GoCall]: what it caches goes to the results every repository shares. */
abstract class GoRepository(protected val go: GoCall) {
    /** The last result of [key]: fetched in this process, or else the last known one kept offline. */
    fun <T> cached(key: String): TalosRepository.Timed<T>? = go.results.cached(key)

    /** Whether [key] was fetched in this process for the cluster on screen (see [ResultCache.fetched]). */
    fun fetched(key: String): Boolean = go.results.fetched(key)

    /** Bumped once the last known state kept offline was read (see [ResultCache.restores]). */
    val restores: StateFlow<Int> get() = go.results.restores

    /** Bumped by [TalosRepository.invalidate]; screens showing cluster data reload when it changes. */
    val invalidations: StateFlow<Int> get() = go.results.invalidations

    fun keeper(key: String): suspend (Any) -> Unit = go.results.keeper(key)

    /**
     * How far the running read [op] on the active cluster got ("checkup", "dataservices",
     * "integrations", "netpol", "apihealth"); polled while it runs. Off the main thread: in
     * screenshot mode the result waits for the mask.
     */
    suspend fun readProgress(op: String): ReadProgress = go.kube { cfg, ctx, _ ->
        TalosJson.decodeFromString(ReadProgress.serializer(), Ichorgo.readProgress(cfg, ctx, op))
    }
}
