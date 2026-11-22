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
import name.levis.ichor.model.HetznerNewClusterSpec
import name.levis.ichor.model.AWSCreateSpec
import name.levis.ichor.model.AWSImageResult
import name.levis.ichor.model.AWSImageSpec
import name.levis.ichor.model.AzureCreateSpec
import name.levis.ichor.model.AzureImageResult
import name.levis.ichor.model.AzureImageSpec
import name.levis.ichor.model.GCPCreateSpec
import name.levis.ichor.model.GCPImageResult
import name.levis.ichor.model.GCPImageSpec
import name.levis.ichor.model.HetznerSnapshotResult
import name.levis.ichor.model.HetznerSnapshotSpec
import name.levis.ichor.model.ImageRunKind
import name.levis.ichor.model.JoinPhase
import name.levis.ichor.model.JoinProgress
import name.levis.ichor.model.MadeImage
import name.levis.ichor.model.OVHCreateSpec
import name.levis.ichor.model.OmniAllocateSpec
import name.levis.ichor.model.ScalewayCreateSpec
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
    /** An instance to create at AWS EC2 for the node, likewise. */
    val aws: AWSCreateSpec? = null,
    /** A VM to create at Azure for the node, likewise. */
    val azure: AzureCreateSpec? = null,
    /** A Compute Engine VM to create at Google Cloud for the node, likewise. */
    val gcp: GCPCreateSpec? = null,
    /** An Instance to create at Scaleway for the node, likewise. */
    val scaleway: ScalewayCreateSpec? = null,
    /** The cloud machine joins Omni, not the cluster directly. */
    val omni: Boolean = false,
    /** No node at all: a throwaway server becomes a Talos snapshot ([hostname] only labels the run). */
    val snapshot: HetznerSnapshotSpec? = null,
    /** No node either: a throwaway AWS instance becomes a Talos AMI ([hostname] only labels the run). */
    val awsImage: AWSImageSpec? = null,
    /** No node either: a throwaway Azure VM's data disk becomes a Talos managed image. */
    val azureImage: AzureImageSpec? = null,
    /** No node either: a throwaway Compute Engine VM's data disk becomes a Talos image. */
    val gcpImage: GCPImageSpec? = null,
    /** A machine of Omni to put in the cluster ([hostname] and [role] are the machine's and the set's). */
    val omniAllocate: OmniAllocateSpec? = null,
    /** No cluster yet: the machine at [address] becomes the first control plane of this one. */
    val newCluster: NewClusterStart? = null,
    /** No cluster yet: a server and a Primary IP at Hetzner become a new one-node cluster ([hostname] is its name). */
    val hetznerCluster: HetznerNewClusterSpec? = null,
) {
    /** The steps of this run's timeline. */
    val phases: List<JoinPhase>
        get() = when {
            omniAllocate != null -> JoinPhase.omniAllocateRun
            newCluster != null -> JoinPhase.newClusterRun
            hetznerCluster != null -> JoinPhase.hetznerClusterRun
            else -> imageKind?.phases ?: when {
                !cloud -> JoinPhase.maintenanceRun
                omni -> JoinPhase.cloudOmniRun
                else -> JoinPhase.cloudRun
            }
        }

    /** A machine to create at a cloud provider. */
    val cloud: Boolean
        get() = hetzner != null || ovh != null || aws != null || azure != null || gcp != null || scaleway != null ||
            hetznerCluster != null

    /** What the run makes when it makes a Talos image instead of a node; null for a node. */
    val imageKind: ImageRunKind?
        get() = when {
            snapshot != null -> ImageRunKind.HETZNER_SNAPSHOT
            awsImage != null -> ImageRunKind.AWS_AMI
            azureImage != null -> ImageRunKind.AZURE_IMAGE
            gcpImage != null -> ImageRunKind.GCP_IMAGE
            else -> null
        }

    /** No node at all: the run makes a Talos image (a Hetzner snapshot, an AWS AMI, an Azure or a Compute Engine image). */
    val imageRun: Boolean
        get() = imageKind != null
}

/**
 * A prepared new cluster (NewClusterPrepare): its draft in the Go core and its [talosconfig],
 * stored as the context [name] before the run. Kept out of [toString]: it is a credential.
 */
data class NewClusterStart(val draftId: String, val name: String, val talosconfig: String) {
    override fun toString(): String = "NewClusterStart($name)"
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
    /** The finished AMI or image of a [JoinRequest.awsImage], [JoinRequest.azureImage] or [JoinRequest.gcpImage] run. */
    val madeImage: MadeImage? = null,
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
    /** Why a new cluster's talosconfig could not be stored during its run. */
    private var storeError: String? = null
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
        // A new cluster's address goes to its own context, stored before the run (at Hetzner: during it).
        val context = request.newCluster?.name ?: request.hetznerCluster?.name ?: configs.config.value?.activeContext.orEmpty()
        _current.value = JoinRunState(request, context)
        storeError = null
        val stopped = MutableStateFlow(false).also { stopRequested = it }
        job = scope.launch {
            try {
                val events = request.newCluster?.let { lifecycle.newCluster(it.draftId, request.address, it.talosconfig, stopped.filter { s -> s }) }
                    ?: request.hetznerCluster?.let { lifecycle.hetznerNewCluster(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.omniAllocate?.let { lifecycle.omniAllocate(it, stopped.filter { s -> s }) }
                    ?: request.snapshot?.let { lifecycle.hetznerSnapshot(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.awsImage?.let { lifecycle.awsImage(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.azureImage?.let { lifecycle.azureImage(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.gcpImage?.let { lifecycle.gcpImage(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.hetzner?.let { lifecycle.hetznerCreate(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.ovh?.let { lifecycle.ovhCreate(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.aws?.let { lifecycle.awsCreate(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.azure?.let { lifecycle.azureCreate(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.gcp?.let { lifecycle.gcpCreate(providerToken, it, stopped.filter { s -> s }) }
                    ?: request.scaleway?.let { lifecycle.scalewayCreate(providerToken, it, stopped.filter { s -> s }) }
                    ?: lifecycle.joinNode(request.address, request.template, request.role, request.hostname, request.disk, request.staticAddress, request.replacing, stopped.filter { it })
                events
                    .collect { event ->
                        when (event) {
                            is JoinEvent.Progress -> {
                                val progress = event.progress
                                if (progress.talosconfig.isNotEmpty()) storeNewCluster(context, progress.talosconfig, stopped)
                                _current.update { it?.copy(events = it.events + progress.copy(talosconfig = "")) }
                            }
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

    /**
     * Stores a new cluster's talosconfig as the run hands it over, before its server is up: a
     * cluster is never bootstrapped without its access on this phone. A store that fails stops the
     * run, which then deletes what it made.
     */
    private suspend fun storeNewCluster(context: String, talosconfig: String, stopped: MutableStateFlow<Boolean>) {
        val error = try {
            if (configs.importConflicts(talosconfig).isNotEmpty()) {
                text(R.string.new_cluster_name_taken, listOf(context))
            } else {
                configs.save(talosconfig)
                null
            }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            text(R.string.new_cluster_store_failed, listOf(e.message ?: e.toString()))
        }
        if (error != null) {
            storeError = error
            _current.update { it?.copy(stopping = true) }
            stopped.value = true
        }
    }

    private suspend fun finish(context: String, done: JoinEvent.Done) {
        // The talosconfig could not be stored: that comes first, whatever the run said.
        storeError?.let { stored ->
            storeError = null
            _current.update { it?.copy(finished = true, error = listOfNotNull(stored, done.error).joinToString("\n")) }
            return
        }
        // An image run's "address" is its result; no node to add.
        if (_current.value?.request?.snapshot != null) {
            val result = runCatching { TalosJson.decodeFromString(HetznerSnapshotResult.serializer(), done.address) }.getOrNull()
            _current.update { it?.copy(finished = true, error = done.error, snapshotResult = result) }
            return
        }
        val kind = _current.value?.request?.imageKind
        if (kind != null) {
            val result = runCatching<MadeImage?> {
                when (kind) {
                    ImageRunKind.AWS_AMI -> TalosJson.decodeFromString(AWSImageResult.serializer(), done.address)
                    ImageRunKind.AZURE_IMAGE -> TalosJson.decodeFromString(AzureImageResult.serializer(), done.address)
                    ImageRunKind.GCP_IMAGE -> TalosJson.decodeFromString(GCPImageResult.serializer(), done.address)
                    // Handled above: its result is a snapshot, not an image.
                    ImageRunKind.HETZNER_SNAPSHOT -> null
                }
            }.getOrNull()
            _current.update { it?.copy(finished = true, error = done.error, madeImage = result) }
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
