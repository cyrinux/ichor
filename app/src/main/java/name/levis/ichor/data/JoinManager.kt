package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.model.HetznerCreateSpec
import name.levis.ichor.model.HetznerSnapshotResult
import name.levis.ichor.model.HetznerSnapshotSpec
import name.levis.ichor.model.JoinPhase
import name.levis.ichor.model.JoinProgress
import name.levis.ichor.model.OVHCreateSpec
import name.levis.ichor.model.OmniAllocateSpec
import name.levis.ichor.ui.FollowedRun

/** What the user confirmed for the new node: the maintenance [address] and step 2's choices. */
data class JoinRequest(
    val address: String,
    val template: String,
    val role: String,
    val hostname: String,
    val disk: String,
    val staticAddress: String,
    /** The new node replaces a member discovery still lists under [hostname] (a dead node). */
    val replacing: Boolean = false,
    /** A server to create at Hetzner for the node, with the config as its user data ([address] unused). */
    val hetzner: HetznerCreateSpec? = null,
    /** An instance to create at OVHcloud for the node, likewise. */
    val ovh: OVHCreateSpec? = null,
    /** The cloud machine joins Omni, not the cluster directly. */
    val omni: Boolean = false,
    /** No node at all: a throwaway server becomes a Talos snapshot ([hostname] only labels the run). */
    val snapshot: HetznerSnapshotSpec? = null,
    /** A machine of Omni to put in the cluster ([hostname] and [role] are the machine's and the set's). */
    val omniAllocate: OmniAllocateSpec? = null,
) {
    /** The steps of this run's timeline. */
    val phases: List<JoinPhase>
        get() = when {
            omniAllocate != null -> JoinPhase.omniAllocateRun
            snapshot != null -> JoinPhase.snapshotRun
            hetzner == null && ovh == null -> JoinPhase.maintenanceRun
            omni -> JoinPhase.cloudOmniRun
            else -> JoinPhase.cloudRun
        }
}

/** The node join the app follows: at most one at a time. */
data class JoinRunState(
    val request: JoinRequest,
    /** The context the node joins, where its address is added once it joined. */
    val context: String,
    val events: List<JoinProgress> = emptyList(),
    /** The joined node's address; empty until it joined. */
    val address: String = "",
    override val finished: Boolean = false,
    override val error: String? = null,
    /** Stop was requested: the run stops following (the install goes on). */
    val stopping: Boolean = false,
    /** The finished snapshot of a [JoinRequest.snapshot] run. */
    val snapshotResult: HetznerSnapshotResult? = null,
) : FollowedRun {
    override val hostname: String get() = request.hostname
}

/**
 * Joining a node from the phone (StartNodeJoin), app-wide like [MaintenanceManager]: leaving the
 * screen keeps it going. Once the node joined, its address is added to the context's nodes.
 * [onStarted] runs once a join is followed (to keep the app alive meanwhile); [text] reads a
 * string resource in the app's language.
 */
class JoinManager(
    private val lifecycle: NodeLifecycleRepository,
    private val configs: ConfigRepository,
    private val text: (Int, List<String>) -> String,
    private val onStarted: () -> Unit = {},
) {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private val _current = MutableStateFlow<JoinRunState?>(null)
    val current: StateFlow<JoinRunState?> = _current.asStateFlow()
    private var job: Job? = null
    /** Set by [stop]: the Go run stops following and still reports how it ended. */
    private var stopRequested = MutableStateFlow(false)

    /**
     * Starts the join unless one is already followed; returns false then. [providerToken] is the
     * provider's credential for a request with a machine to create: kept out of the request,
     * which stays on screen.
     */
    @Synchronized
    fun start(request: JoinRequest, providerToken: String = ""): Boolean {
        if (_current.value?.running == true) return false
        val context = configs.config.value?.activeContext.orEmpty()
        _current.value = JoinRunState(request, context)
        val stopped = MutableStateFlow(false).also { stopRequested = it }
        job = scope.launch {
            try {
                val events = request.omniAllocate?.let { lifecycle.omniAllocate(it, stopped.filter { s -> s }) }
                    ?: request.snapshot?.let { lifecycle.hetznerSnapshot(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.hetzner?.let { lifecycle.hetznerCreate(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.ovh?.let { lifecycle.ovhCreate(providerToken, it, stopped.filter { s -> s }) }
                    ?: lifecycle.joinNode(request.address, request.template, request.role, request.hostname, request.disk, request.staticAddress, request.replacing, stopped.filter { it })
                events
                    .collect { event ->
                        when (event) {
                            is JoinEvent.Progress -> _current.update { it?.copy(events = it.events + event.progress) }
                            is JoinEvent.Done -> finish(context, event)
                        }
                    }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _current.update { it?.copy(finished = true, error = e.message ?: e.toString()) }
            }
        }
        onStarted()
        return true
    }

    private suspend fun finish(context: String, done: JoinEvent.Done) {
        // A snapshot run's "address" is its result; no node to add.
        if (_current.value?.request?.snapshot != null) {
            val result = runCatching { TalosJson.decodeFromString(HetznerSnapshotResult.serializer(), done.address) }.getOrNull()
            _current.update { it?.copy(finished = true, error = done.error, snapshotResult = result) }
            return
        }
        // Omni keeps its own list of machines: no address to add.
        if (done.error == null && done.address.isBlank() && _current.value?.request?.omni == true) {
            _current.update { it?.copy(finished = true) }
            return
        }
        if (done.error != null || done.address.isBlank()) {
            _current.update { it?.copy(finished = true, error = done.error ?: text(R.string.join_run_no_address, emptyList())) }
            return
        }
        val error = try {
            configs.addNodes(context, listOf(done.address))
            null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            text(R.string.join_run_not_added, listOf(done.address, context, e.message.orEmpty()))
        }
        _current.update { it?.copy(finished = true, address = done.address, error = error) }
    }

    /**
     * Stops following; once the config is sent, the node goes on installing and joins by itself.
     * The Go run then reports how it ended (nothing sent, or the install going on).
     */
    @Synchronized
    fun stop() {
        if (_current.value?.running != true) return
        _current.update { it?.copy(stopping = true) }
        stopRequested.value = true
    }

    /** Forgets a finished run. */
    @Synchronized
    fun dismiss() {
        if (_current.value?.finished == true) {
            job = null
            _current.value = null
        }
    }
}
