package name.levis.talosmobile.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.talosmobile.Talosmobile
import name.levis.talosmobile.UpgradeListener
import name.levis.talosmobile.UpgradeRun
import name.levis.talosmobile.model.TalosRelease
import name.levis.talosmobile.model.UpgradePlan
import name.levis.talosmobile.model.UpgradeProgress

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
 */
class UpgradeManager(private val configs: ConfigRepository, private val onFinished: (node: String) -> Unit = {}) {
    private val _current = MutableStateFlow<UpgradeRunState?>(null)
    val current: StateFlow<UpgradeRunState?> = _current.asStateFlow()
    private var run: UpgradeRun? = null

    suspend fun plan(node: String): UpgradePlan = call { cfg, ctx ->
        TalosJson.decodeFromString(UpgradePlan.serializer(), Talosmobile.upgradePlan(cfg, ctx, node))
    }

    /** Known Talos releases, newest first (from the network; may fail offline). */
    suspend fun releases(): List<TalosRelease> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ListSerializer(TalosRelease.serializer()), Talosmobile.talosReleases())
    }

    /** The installer image for [version], keeping the registry and Image Factory schematic of [currentImage]. */
    suspend fun image(currentImage: String, version: String): String = withContext(Dispatchers.IO) {
        Talosmobile.upgradeImage(currentImage, version)
    }

    /** Starts the upgrade unless one is already followed; returns false then. */
    @Synchronized
    fun start(node: String, hostname: String, fromVersion: String, image: String, stage: Boolean, force: Boolean): Boolean {
        if (_current.value?.running == true) return false
        val stored = configs.config.value ?: throw NoConfigException()
        _current.value = UpgradeRunState(node, hostname, fromVersion, image)
        run = try {
            Talosmobile.startUpgrade(stored.yaml, stored.activeContext, node, image, stage, force, listener(node))
        } catch (e: Exception) {
            _current.value = null
            throw e
        }
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

    private suspend fun <T> call(block: (config: String, context: String) -> T): T {
        val stored = configs.config.value ?: throw NoConfigException()
        return withContext(Dispatchers.IO) { block(stored.yaml, stored.activeContext) }
    }
}
