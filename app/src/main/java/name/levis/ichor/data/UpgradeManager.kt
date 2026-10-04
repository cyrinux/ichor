package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichorgo.Ichorgo
import name.levis.ichorgo.UpgradeListener
import name.levis.ichorgo.UpgradeRun
import name.levis.ichor.model.TalosRelease
import name.levis.ichor.model.UpgradePlan
import name.levis.ichor.model.UpgradeProgress

/** The upgrade the app follows: at most one at a time, across all nodes. */
data class UpgradeRunState(
    val node: String,
    val hostname: String,
    val fromVersion: String,
    val image: String,
    val events: List<UpgradeProgress> = emptyList(),
    val finished: Boolean = false,
    val newVersion: String = "",
    val error: String? = null,
) {
    val running: Boolean get() = !finished
}

/**
 * `talosctl upgrade` through the Go core. The run is app-wide (not tied to a screen), so
 * leaving the progress screen keeps following it and no second upgrade can start meanwhile.
 * An upgrade cannot be cancelled once requested: [stopFollowing] only stops watching it.
 * [onStarted] runs once an upgrade is followed (to keep the app alive meanwhile).
 */
class UpgradeManager(
    private val configs: ConfigRepository,
    private val kubeServers: KubeServers,
    private val onStarted: () -> Unit = {},
    private val onFinished: (node: String) -> Unit = {},
) {
    private val _current = MutableStateFlow<UpgradeRunState?>(null)
    val current: StateFlow<UpgradeRunState?> = _current.asStateFlow()
    private var run: UpgradeRun? = null

    suspend fun plan(node: String): UpgradePlan = call { cfg, ctx, server ->
        TalosJson.decodeFromString(UpgradePlan.serializer(), Ichorgo.upgradePlan(cfg, ctx, server, node))
    }

    /** Known Talos releases, newest first (from the network; may fail offline). */
    suspend fun releases(): List<TalosRelease> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ListSerializer(TalosRelease.serializer()), Ichorgo.talosReleases())
    }

    /** The installer image for [version], keeping the registry and Image Factory schematic of [currentImage]. */
    suspend fun image(currentImage: String, version: String): String = withContext(Dispatchers.IO) {
        Ichorgo.upgradeImage(currentImage, version)
    }

    /** Why going from [from] to [to] is risky (skips minor versions, downgrade), or "": to acknowledge before starting. */
    suspend fun versionRisk(from: String, to: String): String = withContext(Dispatchers.IO) {
        Ichorgo.upgradeVersionCheck(from, to)
    }

    /**
     * Starts the upgrade unless one is already followed; returns false then. The core refuses
     * it while the plan has risks to acknowledge and [acknowledged] is false (whatever [force]).
     */
    @Synchronized
    fun start(
        node: String,
        hostname: String,
        fromVersion: String,
        image: String,
        stage: Boolean,
        force: Boolean,
        acknowledged: Boolean,
    ): Boolean {
        if (_current.value?.running == true) return false
        val stored = configs.forCall()
        _current.value = UpgradeRunState(node, hostname, fromVersion, image)
        run = try {
            Ichorgo.startUpgrade(stored.yaml, stored.activeContext, kubeServer(stored), node, image, stage, force, acknowledged, listener(node))
        } catch (e: Exception) {
            _current.value = null
            throw e
        }
        onStarted()
        return true
    }

    private fun listener(node: String) = object : UpgradeListener {
        override fun onProgress(json: String) {
            val event = runCatching { TalosJson.decodeFromString(UpgradeProgress.serializer(), json) }.getOrNull() ?: return
            _current.update { if (it?.node == node && it.running) it.copy(events = it.events + event) else it }
        }

        override fun onDone(newVersion: String, errMessage: String) {
            _current.update {
                if (it?.node == node && it.running) {
                    it.copy(finished = true, newVersion = newVersion, error = errMessage.ifEmpty { null })
                } else {
                    it
                }
            }
            // The node may run another Talos version now (what it supports changed).
            onFinished(node)
        }
    }

    /** Stops watching the upgrade (it goes on on the node) and forgets it. */
    @Synchronized
    fun stopFollowing() {
        run?.cancel()
        run = null
        _current.value = null
    }

    /** Forgets a finished upgrade. */
    @Synchronized
    fun dismiss() {
        if (_current.value?.finished == true) {
            run = null
            _current.value = null
        }
    }

    private suspend fun <T> call(block: (config: String, context: String, kubeServer: String) -> T): T {
        val stored = configs.forCall()
        return withContext(Dispatchers.IO) { block(stored.yaml, stored.activeContext, kubeServer(stored)) }
    }

    /** The Kubernetes API address the user set for the active cluster ("" for the kubeconfig's). */
    private fun kubeServer(stored: StoredConfig): String =
        stored.activeSummary?.fingerprint?.let { kubeServers.servers.value[it] }.orEmpty()
}
