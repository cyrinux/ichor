package name.levis.ichor.data

import name.levis.ichor.ui.goErrorText
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import name.levis.ichorgo.MaintenanceListener
import name.levis.ichorgo.MaintenanceRun
import name.levis.ichor.model.DrainPod
import name.levis.ichor.model.MaintenanceAction
import name.levis.ichor.model.MaintenancePhase
import name.levis.ichor.model.MaintenancePlan
import name.levis.ichor.model.MaintenanceProgress
import name.levis.ichor.model.cordonedAfter
import name.levis.ichor.ui.FollowedRun

/** The node maintenance the app follows: at most one at a time, across all nodes. */
data class MaintenanceRunState(
    val node: String,
    override val hostname: String,
    val action: MaintenanceAction,
    /** The node was cordoned before the run: it stays cordoned, even after a reboot. */
    val wasCordoned: Boolean = false,
    val events: List<MaintenanceProgress> = emptyList(),
    /** The pods being evicted, as last reported. */
    val pods: List<DrainPod> = emptyList(),
    override val finished: Boolean = false,
    override val error: String? = null,
    /** Stop was requested: the run ends after its current step. */
    val stopping: Boolean = false,
) : FollowedRun {
    val phase: MaintenancePhase? get() = events.lastOrNull()?.let { MaintenancePhase.of(it.phase) }
}

/**
 * Node maintenance (cordon → drain → reboot/shutdown/nothing) through the Go core. The run is
 * app-wide (not tied to a screen), like [UpgradeManager]: leaving the screen keeps it going.
 * [cordoned] remembers what the app last learnt of each node's cordon (a hint for the menu).
 * [onStarted] runs once a maintenance is followed (to keep the app alive meanwhile).
 */
class MaintenanceManager(
    private val talos: TalosRepository,
    private val onStarted: () -> Unit = {},
) {
    private val _current = MutableStateFlow<MaintenanceRunState?>(null)
    val current: StateFlow<MaintenanceRunState?> = _current.asStateFlow()
    private val _cordoned = MutableStateFlow<Map<String, Boolean>>(emptyMap())
    val cordoned: StateFlow<Map<String, Boolean>> = _cordoned.asStateFlow()
    private var run: MaintenanceRun? = null

    suspend fun plan(node: String): MaintenancePlan = talos.maintenancePlan(node).also { remember(node, it.cordoned) }

    /** Cordons ([on]) or uncordons [node]; throws when refused. */
    suspend fun cordon(node: String, on: Boolean) {
        talos.cordon(node, on)
        remember(node, on)
    }

    /** Starts the maintenance unless one is already followed; returns false then. [wasCordoned]: from the plan. */
    @Synchronized
    fun start(node: String, hostname: String, action: MaintenanceAction, includeBare: Boolean, acknowledged: Boolean, wasCordoned: Boolean): Boolean {
        if (_current.value?.running == true) return false
        _current.value = MaintenanceRunState(node, hostname, action, wasCordoned)
        run = try {
            talos.startMaintenance(node, action, includeBare, acknowledged, listener(node))
        } catch (e: Exception) {
            _current.value = null
            throw e
        }
        onStarted()
        return true
    }

    private fun listener(node: String) = object : MaintenanceListener {
        override fun onProgress(json: String) {
            val event = runCatching { TalosJson.decodeFromString(MaintenanceProgress.serializer(), json) }.getOrNull() ?: return
            _current.update {
                if (it?.node == node && it.running) it.copy(events = it.events + event, pods = event.pods.ifEmpty { it.pods }) else it
            }
        }

        override fun onDone(errMessage: String) {
            _current.update {
                if (it?.node == node && it.running) it.copy(finished = true, error = errMessage.ifEmpty { null }?.let(::goErrorText)) else it
            }
            _current.value?.takeIf { it.node == node }?.let { r ->
                cordonedAfter(r.action, r.phase, r.running, r.error != null, r.wasCordoned)?.let { remember(node, it) }
            }
        }
    }

    /** Stops the run after its current step; the node stays cordoned. */
    @Synchronized
    fun stop() {
        if (_current.value?.running != true) return
        _current.update { it?.copy(stopping = true) }
        run?.cancel()
    }

    /** Forgets a finished run. */
    @Synchronized
    fun dismiss() {
        if (_current.value?.finished == true) {
            run = null
            _current.value = null
        }
    }

    private fun remember(node: String, cordoned: Boolean) {
        _cordoned.update { it + (node to cordoned) }
    }
}
