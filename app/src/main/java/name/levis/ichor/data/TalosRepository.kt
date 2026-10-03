package name.levis.ichor.data

import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.R
import name.levis.talosmobile.EventListener
import name.levis.talosmobile.HealthListener
import name.levis.talosmobile.LogListener
import name.levis.talosmobile.SnapshotListener
import name.levis.talosmobile.Talosmobile
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.KubeSpanOverview
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.KubePodList
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.KubeWorkloadList
import name.levis.ichor.model.LogEntry
import name.levis.ichor.model.LogTail
import name.levis.ichor.model.decodeLogTail
import name.levis.ichor.model.NodeStats
import name.levis.ichor.model.ClusterStatsSample
import name.levis.ichor.model.NodeResources
import name.levis.ichor.model.ServiceInfo
import name.levis.ichor.model.ProcessSample
import name.levis.ichor.model.ContainerSample
import name.levis.ichor.model.ServiceAction
import name.levis.ichor.model.TalosEvent
import name.levis.ichor.model.ClusterTime
import name.levis.ichor.model.ConnectionInfo
import name.levis.ichor.model.ImageInfo
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.NodeHardware
import name.levis.ichor.model.NodeNetwork
import name.levis.ichor.model.NodeTime
import name.levis.ichor.model.DiskHealthReport
import name.levis.ichor.model.DiskUsage
import name.levis.ichor.model.EtcdForfeitResult
import name.levis.ichor.model.EtcdMemberPlan
import name.levis.ichor.model.MountList
import name.levis.ichor.model.NodeDiscovery
import name.levis.ichor.model.NodeFeatures
import name.levis.ichor.model.ResourceDetail
import name.levis.ichor.model.ResourceList
import name.levis.ichor.model.ResourceType
import name.levis.ichor.model.VolumeList
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer

/** Events streamed by the cluster health check. */
sealed interface HealthEvent {
    data class Progress(val node: String, val message: String) : HealthEvent
    data class Done(val error: String?) : HealthEvent
}

/** Events streamed by an etcd snapshot download. */
sealed interface SnapshotEvent {
    data class Progress(val bytes: Long) : SnapshotEvent
    data class Done(val path: String, val size: Long, val sha256: String) : SnapshotEvent
    data class Failed(val message: String) : SnapshotEvent
}

/** Items of a live stream (events, followed log). [Done] ends it; [error] null when cancelled. */
sealed interface StreamItem<out T> {
    data class Item<T>(val value: T) : StreamItem<T>
    data class Done(val error: String?) : StreamItem<Nothing>
}

class NoConfigException : LocalizedException(UiText.Res(R.string.common_no_config))

/** Read-only access to the Talos API through the Go core. All calls are blocking in Go, so run on IO. */
class TalosRepository(private val configs: ConfigRepository, private val kubeServers: KubeServers) {

    /**
     * Last successful results, in memory only (cluster data is never written to disk), so
     * screens can show them instantly while refreshing. Keys include the config generation
     * and context, so importing or switching invalidates them.
     */
    private val cache = java.util.concurrent.ConcurrentHashMap<String, Any>()

    /** A cached result and when it was fetched (epoch millis). */
    data class Timed<T>(val value: T, val at: Long)

    @Suppress("UNCHECKED_CAST")
    fun <T> cached(key: String): Timed<T>? = cache[scoped(key)] as Timed<T>?

    private fun scoped(key: String): String {
        val stored = configs.config.value
        return "${configs.generation.value}|${stored?.activeContext}|$key"
    }

    /** Bumped by [invalidate]; screens showing cluster data reload when it changes. */
    private val _invalidations = MutableStateFlow(0)
    val invalidations: StateFlow<Int> = _invalidations.asStateFlow()

    /**
     * Drops every cached result and asks visible screens to reload, e.g. after screenshot
     * mode changed, so no value fetched under the previous setting stays on screen.
     */
    fun invalidate() {
        cache.clear()
        _invalidations.value++
    }

    private suspend fun <T : Any> remember(key: String, block: suspend () -> T): T =
        block().also { cache[scoped(key)] = Timed(it, System.currentTimeMillis()) }

    suspend fun kubespan(): KubeSpanOverview = remember(KUBESPAN) {
        call { cfg, ctx -> TalosJson.decodeFromString(KubeSpanOverview.serializer(), Talosmobile.kubeSpanStatus(cfg, ctx)) }
    }

    /** `talosctl -n NODE etcd defrag` (os:operator or os:admin); one member at a time. */
    suspend fun etcdDefragment(node: String) = call { cfg, ctx -> Talosmobile.etcdDefragment(cfg, ctx, node) }

    /** One sample of node counters for the live graphs (not cached: always fresh). */
    suspend fun stats(node: String): NodeStats = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeStats.serializer(), Talosmobile.nodeStats(cfg, ctx, node))
    }

    /** One sample of every node's CPU and memory counters for the live cluster summary (not cached). */
    suspend fun clusterStats(): ClusterStatsSample = call { cfg, ctx ->
        TalosJson.decodeFromString(ClusterStatsSample.serializer(), Talosmobile.clusterStats(cfg, ctx))
    }

    /** One sample of the node's processes for the Processes tab (not cached: always fresh). */
    suspend fun processes(node: String): ProcessSample = call { cfg, ctx ->
        TalosJson.decodeFromString(ProcessSample.serializer(), Talosmobile.nodeProcesses(cfg, ctx, node))
    }

    /** One sample of the node's CRI containers for the Pods tab (not cached: always fresh). */
    suspend fun containers(node: String): ContainerSample = call { cfg, ctx ->
        TalosJson.decodeFromString(ContainerSample.serializer(), Talosmobile.nodeContainers(cfg, ctx, node))
    }

    /** `talosctl -n NODE service ID start|stop|restart` (os:operator or os:admin). */
    suspend fun serviceAction(node: String, service: String, action: ServiceAction) = call { cfg, ctx ->
        Talosmobile.serviceAction(cfg, ctx, node, service, action.cli)
    }

    /**
     * Streams machine events (`talosctl events --tail`) from [nodes] (empty: all context
     * nodes), replaying the last [tail] per node. Cancelling the collector stops the stream.
     */
    fun events(nodes: List<String>, tail: Int): Flow<StreamItem<TalosEvent>> = callbackFlow {
        val stored = configs.forCall()
        val run = Talosmobile.startEvents(
            stored.yaml,
            stored.activeContext,
            nodes.joinToString(","),
            tail.toLong(),
            object : EventListener {
                override fun onEvent(json: String) {
                    runCatching { TalosJson.decodeFromString(TalosEvent.serializer(), json) }
                        .onSuccess { trySend(StreamItem.Item(it)) }
                }

                override fun onDone(errMessage: String) {
                    trySend(StreamItem.Done(errMessage.ifEmpty { null }))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    /**
     * Follows a service log (kernel log when [service] is null) like `talosctl logs -f
     * --tail`, starting with the last [tailLines]. Cancelling the collector stops it.
     */
    fun followLogs(node: String, service: String?, tailLines: Int): Flow<StreamItem<String>> = callbackFlow {
        val stored = configs.forCall()
        val run = Talosmobile.startLogFollow(
            stored.yaml,
            stored.activeContext,
            node,
            service.orEmpty(),
            tailLines.toLong(),
            object : LogListener {
                override fun onLine(line: String) {
                    trySend(StreamItem.Item(line))
                }

                override fun onDone(errMessage: String) {
                    trySend(StreamItem.Done(errMessage.ifEmpty { null }))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    /** Like [followLogs] for one CRI container (`talosctl logs -k -f`). */
    fun followContainerLogs(node: String, containerId: String, tailLines: Int): Flow<StreamItem<String>> = callbackFlow {
        val stored = configs.forCall()
        val run = Talosmobile.startContainerLogFollow(
            stored.yaml,
            stored.activeContext,
            node,
            containerId,
            tailLines.toLong(),
            object : LogListener {
                override fun onLine(line: String) {
                    trySend(StreamItem.Item(line))
                }

                override fun onDone(errMessage: String) {
                    trySend(StreamItem.Done(errMessage.ifEmpty { null }))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    /**
     * The node's active machine config as YAML (os:admin). Secrets are masked unless
     * [revealSecrets]. Never cached: it may hold secrets.
     */
    suspend fun machineConfig(node: String, revealSecrets: Boolean): String = call { cfg, ctx ->
        Talosmobile.nodeMachineConfig(cfg, ctx, node, revealSecrets)
    }

    /** `talosctl etcd alarm disarm` through [node] (os:operator or os:admin); alarms are cluster-wide. */
    suspend fun etcdAlarmDisarm(node: String) = call { cfg, ctx -> Talosmobile.etcdAlarmDisarm(cfg, ctx, node) }

    /**
     * Streams `talosctl -n NODE etcd snapshot` into [destPath] (written atomically by Go).
     * Cancelling the collector cancels the download.
     */
    fun etcdSnapshot(node: String, destPath: String): Flow<SnapshotEvent> = callbackFlow {
        val stored = configs.forCall()
        val run = Talosmobile.startEtcdSnapshot(
            stored.yaml,
            stored.activeContext,
            node,
            destPath,
            object : SnapshotListener {
                override fun onProgress(bytes: Long) {
                    trySend(SnapshotEvent.Progress(bytes))
                }

                override fun onDone(path: String, size: Long, sha256: String, errMessage: String) {
                    trySend(if (errMessage.isEmpty()) SnapshotEvent.Done(path, size, sha256) else SnapshotEvent.Failed(errMessage))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.CONFLATED) // progress may be dropped, the final event is always kept

    suspend fun overview(): ClusterOverview = remember(OVERVIEW) { overviewUncached() }

    private suspend fun overviewUncached(): ClusterOverview = call { cfg, ctx ->
        TalosJson.decodeFromString(ClusterOverview.serializer(), Talosmobile.clusterOverview(cfg, ctx))
    }

    suspend fun services(node: String): List<ServiceInfo> = remember(servicesKey(node)) { servicesUncached(node) }

    private suspend fun servicesUncached(node: String): List<ServiceInfo> = call { cfg, ctx ->
        TalosJson.decodeFromString(ListSerializer(ServiceInfo.serializer()), Talosmobile.nodeServices(cfg, ctx, node))
    }

    suspend fun resources(node: String): NodeResources = remember(resourcesKey(node)) { resourcesUncached(node) }

    private suspend fun resourcesUncached(node: String): NodeResources = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeResources.serializer(), Talosmobile.nodeResources(cfg, ctx, node))
    }

    suspend fun etcd(): EtcdOverview = remember(ETCD) { etcdUncached() }

    private suspend fun etcdUncached(): EtcdOverview = call { cfg, ctx ->
        TalosJson.decodeFromString(EtcdOverview.serializer(), Talosmobile.etcdStatus(cfg, ctx))
    }

    /** Last [lines] lines of a Talos service log, or of the kernel log when [service] is null. */
    suspend fun logs(node: String, service: String?, lines: Int = 500): LogTail = call { cfg, ctx ->
        val json = if (service == null) Talosmobile.kernelLogs(cfg, ctx, node, lines.toLong())
        else Talosmobile.serviceLogs(cfg, ctx, node, service, lines.toLong())
        decodeLogTail(TalosJson, json)
    }

    /** Last [lines] lines of a CRI container's log (`talosctl logs -k`). */
    suspend fun containerLogs(node: String, containerId: String, lines: Int = 500): LogTail = call { cfg, ctx ->
        decodeLogTail(TalosJson, Talosmobile.containerLogs(cfg, ctx, node, containerId, lines.toLong()))
    }

    /**
     * One followed log line as a structured entry (parsed locally by the Go core, no network);
     * the plain line if it cannot be decoded. Call off the main thread.
     */
    fun parseLogLine(line: String): LogEntry =
        runCatching { TalosJson.decodeFromString(LogEntry.serializer(), Talosmobile.parseLogLine(line)) }
            .getOrNull()
            ?.let { if (it.raw.isEmpty()) it.copy(raw = line) else it }
            ?: LogEntry.plain(line)

    /** Admin kubeconfig (os:admin role). A credential: only hand it to where the user chose. */
    suspend fun kubeconfig(): String = kubeCall { cfg, ctx, server -> Talosmobile.kubeconfig(cfg, ctx, server) }

    /** Deployments, StatefulSets and DaemonSets through the Kubernetes API (os:admin: Talos issues the kubeconfig). */
    suspend fun workloads(): List<KubeWorkload> = remember(WORKLOADS) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(KubeWorkloadList.serializer(), Talosmobile.kubeWorkloads(cfg, ctx, server)).workloads }
    }

    /** `kubectl rollout restart KIND/NAME -n NAMESPACE` (os:admin). */
    suspend fun rolloutRestart(workload: KubeWorkload) = kubeCall { cfg, ctx, server ->
        Talosmobile.kubeRolloutRestart(cfg, ctx, server, workload.kind, workload.namespace, workload.name)
    }

    /** Every pod with the status `kubectl get pods` shows (os:admin). */
    suspend fun pods(): List<KubePod> = remember(PODS) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(KubePodList.serializer(), Talosmobile.kubePods(cfg, ctx, server)).pods }
    }

    /**
     * Health of Longhorn, Garage and CloudNativePG (os:admin). [hints]: their catalog ids seen in
     * the inventory (see [name.levis.ichor.model.dataServiceHints]); "" checks everything.
     */
    suspend fun dataServices(hints: String): DataServices = remember(DATA_SERVICES) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(DataServices.serializer(), Talosmobile.kubeDataServices(cfg, ctx, server, hints)) }
    }

    /** `kubectl delete pod NAME -n NAMESPACE` (os:admin): its controller starts a new one. */
    suspend fun deletePod(pod: KubePod) = kubeCall { cfg, ctx, server -> Talosmobile.kubeDeletePod(cfg, ctx, server, pod.namespace, pod.name) }

    /** `talosctl reboot -m [mode]` (default, powercycle, force); needs os:operator or higher. */
    suspend fun reboot(node: String, mode: String) = call { cfg, ctx -> Talosmobile.reboot(cfg, ctx, node, mode) }

    /** `talosctl shutdown [--force]` (force skips cordon/drain); needs os:operator or higher. */
    suspend fun shutdown(node: String, force: Boolean) = call { cfg, ctx -> Talosmobile.shutdown(cfg, ctx, node, force) }

    /** Streams the server-side health check; cancelling the collector cancels the check. */
    fun health(): Flow<HealthEvent> = callbackFlow {
        val stored = configs.forCall()
        val run = Talosmobile.startClusterHealth(
            stored.yaml,
            stored.activeContext,
            object : HealthListener {
                override fun onProgress(node: String, message: String) {
                    trySend(HealthEvent.Progress(node, message))
                }

                override fun onDone(errMessage: String) {
                    trySend(HealthEvent.Done(errMessage.ifEmpty { null }))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED) // never drop progress lines or the final Done event

    suspend fun network(node: String): NodeNetwork = remember(networkKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(NodeNetwork.serializer(), Talosmobile.nodeNetwork(cfg, ctx, node)) }
    }

    /** The node's sockets, like `talosctl netstat -a -p` (not cached: always fresh). */
    suspend fun connections(node: String): List<ConnectionInfo> = call { cfg, ctx ->
        TalosJson.decodeFromString(ListSerializer(ConnectionInfo.serializer()), Talosmobile.nodeConnections(cfg, ctx, node))
    }

    /** The node's clock compared with its NTP server (not cached: an offset goes stale fast). */
    suspend fun nodeTime(node: String): NodeTime = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeTime.serializer(), Talosmobile.nodeTime(cfg, ctx, node))
    }

    suspend fun clusterTime(): ClusterTime = remember(CLUSTER_TIME) {
        call { cfg, ctx -> TalosJson.decodeFromString(ClusterTime.serializer(), Talosmobile.clusterTime(cfg, ctx)) }
    }

    suspend fun hardware(node: String): NodeHardware = remember(hardwareKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(NodeHardware.serializer(), Talosmobile.nodeHardware(cfg, ctx, node)) }
    }

    suspend fun images(node: String): List<ImageInfo> = remember(imagesKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(ListSerializer(ImageInfo.serializer()), Talosmobile.nodeImages(cfg, ctx, node)) }
    }

    /** The apps running in the cluster. One container listing per node: on demand, never polled. */
    suspend fun inventory(): Inventory = remember(INVENTORY) {
        call { cfg, ctx -> TalosJson.decodeFromString(Inventory.serializer(), Talosmobile.clusterInventory(cfg, ctx)) }
    }

    /**
     * A new single-context talosconfig for the active context with [roles] (comma-separated),
     * valid [ttlHours] (os:admin). A credential: never cached, logged or written by this class.
     */
    suspend fun generateTalosconfig(roles: String, ttlHours: Int): String = call { cfg, ctx ->
        Talosmobile.generateTalosconfig(cfg, ctx, roles, ttlHours.toLong())
    }

    /**
     * What [node]'s Talos version supports. Cached: it only changes with an upgrade, and
     * [invalidate] (or pull-to-refresh of the overview) drops it.
     */
    suspend fun features(node: String): NodeFeatures = cached<NodeFeatures>(featuresKey(node))?.value
        ?: remember(featuresKey(node)) {
            call { cfg, ctx -> TalosJson.decodeFromString(NodeFeatures.serializer(), Talosmobile.nodeFeatures(cfg, ctx, node)) }
        }

    /** [features] of each of [nodes], in parallel; nodes that do not answer are left out. */
    suspend fun clusterFeatures(nodes: List<String>): List<NodeFeatures> = coroutineScope {
        nodes.map { node -> async { runCatching { features(node) }.getOrNull() } }.awaitAll().filterNotNull()
    }

    /** Bumped by [forgetFeatures]; whoever shows features asks for them again. */
    private val _featureChanges = MutableStateFlow(0)
    val featureChanges: StateFlow<Int> = _featureChanges.asStateFlow()

    /**
     * Forgets the cached [features] of [node] (null: of every node), e.g. after an upgrade
     * or when the overview is refreshed.
     */
    fun forgetFeatures(node: String? = null) {
        if (node == null) {
            cache.keys.removeAll { FEATURES_PREFIX in it }
        } else {
            cache.remove(scoped(featuresKey(node)))
        }
        _featureChanges.value++
    }

    /** Mounted filesystems with their usage, like `talosctl mounts`. */
    suspend fun mounts(node: String): MountList = call { cfg, ctx ->
        TalosJson.decodeFromString(MountList.serializer(), Talosmobile.nodeMounts(cfg, ctx, node))
    }

    /** Volume status (`talosctl get volumestatus`); [VolumeList.supported] false on older Talos. */
    suspend fun volumes(node: String): VolumeList = call { cfg, ctx ->
        TalosJson.decodeFromString(VolumeList.serializer(), Talosmobile.nodeVolumes(cfg, ctx, node))
    }

    /** `talosctl usage PATH -d DEPTH`. */
    suspend fun diskUsage(node: String, path: String, depth: Int): DiskUsage = call { cfg, ctx ->
        TalosJson.decodeFromString(DiskUsage.serializer(), Talosmobile.nodeDiskUsage(cfg, ctx, node, path, depth.toLong()))
    }

    /** SMART/NVMe health per disk; [DiskHealthReport.supported] false on older Talos. */
    suspend fun diskHealth(node: String): DiskHealthReport = call { cfg, ctx ->
        TalosJson.decodeFromString(DiskHealthReport.serializer(), Talosmobile.nodeDiskHealth(cfg, ctx, node))
    }

    /** `talosctl -n NODE etcd forfeit-leadership` (os:admin); [node] must be the leader. */
    suspend fun etcdForfeitLeadership(node: String): EtcdForfeitResult = call { cfg, ctx ->
        TalosJson.decodeFromString(EtcdForfeitResult.serializer(), Talosmobile.etcdForfeitLeadership(cfg, ctx, node))
    }

    /** `talosctl -n NODE etcd remove-member MEMBER` (os:admin), asked to another member's [node]. */
    suspend fun etcdRemoveMember(node: String, memberId: String) = call { cfg, ctx ->
        Talosmobile.etcdRemoveMember(cfg, ctx, node, memberId)
    }

    /** What removing [memberId] would leave (members, quorum) and what forbids it. Read-only. */
    suspend fun etcdMemberPlan(memberId: String): EtcdMemberPlan = call { cfg, ctx ->
        TalosJson.decodeFromString(EtcdMemberPlan.serializer(), Talosmobile.etcdMemberPlan(cfg, ctx, memberId))
    }

    /** Resource types the node knows (`talosctl get rd`). */
    suspend fun resourceTypes(node: String): List<ResourceType> = remember(resourceTypesKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(ListSerializer(ResourceType.serializer()), Talosmobile.resourceTypes(cfg, ctx, node)) }
    }

    /** `talosctl get TYPE -n NODE --namespace NAMESPACE` (never cached: may be sensitive). */
    suspend fun resourceList(node: String, namespace: String, type: String): ResourceList = call { cfg, ctx ->
        TalosJson.decodeFromString(ResourceList.serializer(), Talosmobile.resourceList(cfg, ctx, node, namespace, type))
    }

    /** The cluster's members (Talos cluster discovery), flagged when the context already targets them. */
    suspend fun discoverNodes(): NodeDiscovery = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeDiscovery.serializer(), Talosmobile.discoverNodes(cfg, ctx))
    }

    /** `talosctl get TYPE ID -o yaml` (never cached: may hold secrets). */
    suspend fun resourceGet(node: String, namespace: String, type: String, id: String): String = call { cfg, ctx ->
        TalosJson.decodeFromString(ResourceDetail.serializer(), Talosmobile.resourceGet(cfg, ctx, node, namespace, type, id)).yaml
    }

    suspend fun driftSnapshot(): String = call { cfg, ctx -> Talosmobile.clusterDriftSnapshot(cfg, ctx) }
    suspend fun observation(): String = call { cfg, ctx -> Talosmobile.clusterObservation(cfg, ctx) }
    suspend fun bottlenecks(previous: NodeStats, current: NodeStats): name.levis.ichor.model.Bottlenecks = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(name.levis.ichor.model.Bottlenecks.serializer(), Talosmobile.calculateBottlenecks(
            TalosJson.encodeToString(NodeStats.serializer(), previous), TalosJson.encodeToString(NodeStats.serializer(), current),
        ))
    }

    private suspend fun <T> call(block: (config: String, context: String) -> T): T {
        val stored = configs.forCall()
        return withContext(Dispatchers.IO) { block(stored.yaml, stored.activeContext) }
    }

    /** [call] with the Kubernetes API address the user set for the cluster ("" for the kubeconfig's). */
    private suspend fun <T> kubeCall(block: (config: String, context: String, kubeServer: String) -> T): T {
        val stored = configs.forCall()
        val server = stored.activeSummary?.fingerprint?.let { kubeServers.servers.value[it] }.orEmpty()
        return withContext(Dispatchers.IO) { block(stored.yaml, stored.activeContext, server) }
    }
}

const val OVERVIEW = "overview"
const val ETCD = "etcd"
const val KUBESPAN = "kubespan"
const val INVENTORY = "inventory"
const val WORKLOADS = "workloads"
const val PODS = "pods"
const val DATA_SERVICES = "dataservices"
fun servicesKey(node: String) = "services|$node"
fun resourcesKey(node: String) = "resources|$node"
const val CLUSTER_TIME = "clustertime"
fun networkKey(node: String) = "network|$node"
fun hardwareKey(node: String) = "hardware|$node"
fun imagesKey(node: String) = "images|$node"

private const val FEATURES_PREFIX = "|features|"
fun featuresKey(node: String) = "features|$node"
fun resourceTypesKey(node: String) = "resourcetypes|$node"
