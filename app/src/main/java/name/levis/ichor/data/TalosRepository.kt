package name.levis.ichor.data

import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.R
import name.levis.ichorgo.Ichorgo
import name.levis.ichorgo.OverviewListener
import name.levis.ichor.util.StartupTrace
import name.levis.ichor.model.MetaKeyList
import name.levis.ichor.model.AuditReport
import name.levis.ichor.model.CgroupReport
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.NetTool
import name.levis.ichor.model.NetToolEvent
import name.levis.ichor.model.NetToolOptions
import name.levis.ichor.model.ClusterSensorHealth
import name.levis.ichor.model.ClusterStorageHealth
import name.levis.ichor.model.KubeNodeMeta
import name.levis.ichor.model.NodeMetaEdits
import name.levis.ichor.model.NodeMetaPlan
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.KubeSpanOverview
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
import name.levis.ichor.model.SystemImage
import name.levis.ichor.model.TalosEvent
import name.levis.ichor.model.ClusterTime
import name.levis.ichor.model.ConnectionInfo
import name.levis.ichor.model.ImageInfo
import name.levis.ichor.model.ImagePullNamespace
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.NodeHardware
import name.levis.ichor.model.NodeSensors
import name.levis.ichor.model.NodeNetwork
import name.levis.ichor.model.NodeTime
import name.levis.ichor.model.DiskHealthReport
import name.levis.ichor.model.DiskLayoutList
import name.levis.ichor.model.DiskUsage
import name.levis.ichor.model.MountList
import name.levis.ichor.model.KubeSpanDiagAll
import name.levis.ichor.model.NodeFeatures
import name.levis.ichor.model.ResourceDetail
import name.levis.ichor.model.ResourceList
import name.levis.ichor.model.ResourceType
import name.levis.ichor.model.VolumeList
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.flowOn
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.serializer
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.model.withLastKnown
import name.levis.ichor.model.OverviewPart
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.collectOverview
import name.levis.ichor.model.overviewTargets
import name.levis.ichor.model.outage

class NoConfigException : LocalizedException(UiText.Res(R.string.common_no_config))

/**
 * The Talos API through the Go core, and the results cache every repository shares (see
 * [GoCall]): Kubernetes calls live in [KubeRepository], Argo CD and Flux in [GitOpsRepository],
 * data services in [DataServicesRepository]; etcd in [EtcdRepository], the machine config in
 * [MachineConfigRepository], maintenance, power, upgrades and joins in [NodeLifecycleRepository].
 */
class TalosRepository(go: GoCall) : GoRepository(go) {

    private val configs = go.configs
    private val results = go.results
    private val streams = TalosStreams(configs)

    /** A cached result and when it was fetched (epoch millis). */
    data class Timed<T>(val value: T, val at: Long)

    suspend fun restoreOffline() = results.restoreOffline()

    fun invalidate() = results.invalidate()

    private suspend fun <T : Any> remember(
        key: String,
        persistable: (T) -> Boolean = { true },
        block: suspend (last: () -> Timed<T>?) -> T,
    ): T = go.remember(key, persistable, block)

    /** Why each node's KubeSpan peers are up or down, every node compared. Read-only, not cached. */
    suspend fun kubespanDiagnostics(): KubeSpanDiagAll = call { cfg, ctx ->
        TalosJson.decodeFromString(KubeSpanDiagAll.serializer(), Ichorgo.kubeSpanDiagnosticsAll(cfg, ctx))
    }

    suspend fun kubespan(): KubeSpanOverview = remember(KUBESPAN) {
        call { cfg, ctx -> TalosJson.decodeFromString(KubeSpanOverview.serializer(), Ichorgo.kubeSpanStatus(cfg, ctx)) }
    }

    /**
     * The cluster map: nodes, KubeSpan links and sites (zones or shared LANs). Those that no
     * longer answer keep their name and site from the last map (see [withLastKnown]).
     */
    suspend fun topology(): ClusterTopology = remember(TOPOLOGY) { last ->
        val fresh = call { cfg, ctx -> TalosJson.decodeFromString(ClusterTopology.serializer(), Ichorgo.clusterTopology(cfg, ctx)) }
        val previous = last()
        fresh.withLastKnown(previous?.value, previous?.at ?: 0L)
    }

    /** One sample of node counters for the live graphs (not cached: always fresh). */
    suspend fun stats(node: String): NodeStats = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeStats.serializer(), Ichorgo.nodeStats(cfg, ctx, node))
    }

    /**
     * One sample of the CPU and memory counters of [nodes] (empty: every node) for the live
     * cluster summary (not cached). Nodes the context does not have are left out.
     */
    suspend fun clusterStats(nodes: List<String> = emptyList()): ClusterStatsSample = call { cfg, ctx ->
        TalosJson.decodeFromString(ClusterStatsSample.serializer(), Ichorgo.clusterStatsNodes(cfg, ctx, nodes.joinToString("\n")))
    }

    /** One sample of the node's processes for the Processes tab (not cached: always fresh). */
    suspend fun processes(node: String): ProcessSample = call { cfg, ctx ->
        TalosJson.decodeFromString(ProcessSample.serializer(), Ichorgo.nodeProcesses(cfg, ctx, node))
    }

    /** The node's cgroup tree with pressure, for the pressure card and the Cgroups tab (os:admin; not cached). */
    suspend fun cgroups(node: String): CgroupReport = call { cfg, ctx ->
        TalosJson.decodeFromString(CgroupReport.serializer(), Ichorgo.nodeCgroups(cfg, ctx, node))
    }

    /** One sample of the node's system and CRI containers for the Pods tab (not cached: always fresh). */
    suspend fun containers(node: String): ContainerSample = call { cfg, ctx ->
        TalosJson.decodeFromString(ContainerSample.serializer(), Ichorgo.nodeContainers(cfg, ctx, node))
    }

    /** `talosctl -n NODE restart [-k] ID` (os:admin); [namespace] is `system` or `k8s.io`. */
    suspend fun containerRestart(node: String, namespace: String, id: String) = call { cfg, ctx ->
        Ichorgo.containerRestart(cfg, ctx, node, namespace, id)
    }

    /** The keys of [node]'s META partition (never cached: values change under it). */
    suspend fun metaKeys(node: String): MetaKeyList = call { cfg, ctx ->
        TalosJson.decodeFromString(MetaKeyList.serializer(), Ichorgo.nodeMetaKeys(cfg, ctx, node))
    }

    suspend fun metaWrite(node: String, key: Int, value: String) = call { cfg, ctx ->
        Ichorgo.nodeMetaWrite(cfg, ctx, node, key.toLong(), value)
    }

    suspend fun metaDelete(node: String, key: Int) = call { cfg, ctx ->
        Ichorgo.nodeMetaDelete(cfg, ctx, node, key.toLong())
    }

    /** `talosctl -n NODE service ID start|stop|restart` (os:operator or os:admin). */
    suspend fun serviceAction(node: String, service: String, action: ServiceAction) = call { cfg, ctx ->
        Ichorgo.serviceAction(cfg, ctx, node, service, action.cli)
    }

    /**
     * Streams machine events (`talosctl events --tail`) from [nodes] (empty: all context
     * nodes), replaying the last [tail] per node. Cancelling the collector stops the stream.
     */
    fun events(nodes: List<String>, tail: Int): Flow<StreamItem<TalosEvent>> = streams.events(nodes, tail)

    /**
     * Follows a service log (kernel log when [service] is null) like `talosctl logs -f
     * --tail`, starting with the last [tailLines]. Cancelling the collector stops it.
     */
    fun followLogs(node: String, service: String?, tailLines: Int): Flow<StreamItem<String>> = streams.followLogs(node, service, tailLines)

    /** Like [followLogs] for one CRI container (`talosctl logs -k -f`). */
    fun followContainerLogs(node: String, containerId: String, tailLines: Int): Flow<StreamItem<String>> = streams.followContainerLogs(node, containerId, tailLines)

    /** A network check from [node] (see [TalosStreams.netTool]); needs os:admin. */
    fun netTool(node: String, tool: NetTool, target: String, options: NetToolOptions): Flow<NetToolEvent> =
        streams.netTool(node, tool, target, options)

    /** Pulls an image on several nodes (see [TalosStreams.imagePull]); needs os:admin. */
    fun imagePull(nodes: List<String>, image: String, namespace: ImagePullNamespace): Flow<ImagePullEvent> =
        streams.imagePull(nodes, image, namespace)

    /**
     * The cluster's nodes. Those that no longer answer keep what the last overview knew of
     * them (see [withLastKnown]), read once the call returned so a restored one counts too.
     * Kept on disk only while a node answers: an outage must not make old data look fresh.
     */
    suspend fun overview(): ClusterOverview = remember(OVERVIEW, persistable = { it.outage == null }) { last ->
        val fresh = overviewUncached()
        val previous = last()
        fresh.withLastKnown(previous?.value, previous?.at ?: 0L)
    }

    /**
     * [overview] node by node: [onPartial] gets the nodes that answered so far (a
     * [ClusterOverview.partial] one, never kept), the complete overview is returned and kept as
     * [overview] keeps it, so the cache, the offline file and the monitor only see complete ones.
     */
    suspend fun overview(onPartial: (ClusterOverview) -> Unit): ClusterOverview =
        remember(OVERVIEW, persistable = { it.outage == null && !it.partial }) { last ->
            val target = go.talosTarget()
            val targets = target.activeSummary?.overviewTargets().orEmpty()
            val fresh = overviewStream(target).collectOverview(target.activeContext, targets, onPartial)
            val previous = last()
            fresh.withLastKnown(previous?.value, previous?.at ?: 0L)
        }

    /** StartClusterOverview: each node as it answers, then the complete overview; closing the flow stops it. */
    fun overviewStream(): Flow<OverviewPart> = overviewStream(go.talosTarget())

    private fun overviewStream(target: StoredConfig): Flow<OverviewPart> = callbackFlow {
        StartupTrace.markOnce("go clusterOverview start")
        val run = Ichorgo.startClusterOverview(
            target.yaml,
            target.activeContext,
            object : OverviewListener {
                override fun onNode(json: String) {
                    runCatching { TalosJson.decodeFromString(NodeOverview.serializer(), json) }
                        .onSuccess { trySend(OverviewPart.Node(it)) }
                }

                override fun onDone(overviewJSON: String, errMessage: String) {
                    StartupTrace.markOnce("go clusterOverview end")
                    if (errMessage.isNotEmpty()) {
                        close(Exception(errMessage))
                        return
                    }
                    runCatching { TalosJson.decodeFromString(ClusterOverview.serializer(), overviewJSON) }
                        .onSuccess { trySend(OverviewPart.Done(it)); close() }
                        .onFailure { close(it) }
                }
            },
        )
        awaitClose { run.stop() }
    }.buffer(Channel.UNLIMITED).flowOn(Dispatchers.IO)

    private suspend fun overviewUncached(): ClusterOverview = call { cfg, ctx ->
        StartupTrace.markOnce("go clusterOverview start")
        Ichorgo.clusterOverview(cfg, ctx)
            .also { StartupTrace.markOnce("go clusterOverview end") }
            .let { TalosJson.decodeFromString(ClusterOverview.serializer(), it) }
    }

    /** Every node's volume fill and disk SMART verdict (the monitor's storage track; never cached). */
    suspend fun storageHealth(): ClusterStorageHealth = call { cfg, ctx ->
        TalosJson.decodeFromString(ClusterStorageHealth.serializer(), Ichorgo.clusterStorageHealth(cfg, ctx))
    }

    /** Every node's hottest sensor and throttling, for the monitor's temperature alert. */
    suspend fun sensorHealth(): ClusterSensorHealth = call { cfg, ctx ->
        TalosJson.decodeFromString(ClusterSensorHealth.serializer(), Ichorgo.clusterSensorHealth(cfg, ctx))
    }

    suspend fun services(node: String): List<ServiceInfo> = remember(servicesKey(node)) { servicesUncached(node) }

    private suspend fun servicesUncached(node: String): List<ServiceInfo> = call { cfg, ctx ->
        TalosJson.decodeFromString(ListSerializer(ServiceInfo.serializer()), Ichorgo.nodeServices(cfg, ctx, node))
    }

    suspend fun resources(node: String): NodeResources = remember(resourcesKey(node)) { resourcesUncached(node) }

    private suspend fun resourcesUncached(node: String): NodeResources = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeResources.serializer(), Ichorgo.nodeResources(cfg, ctx, node))
    }

    /** Last [lines] lines of a Talos service log, or of the kernel log when [service] is null. */
    suspend fun logs(node: String, service: String?, lines: Int = 500): LogTail = call { cfg, ctx ->
        val json = if (service == null) Ichorgo.kernelLogs(cfg, ctx, node, lines.toLong())
        else Ichorgo.serviceLogs(cfg, ctx, node, service, lines.toLong())
        decodeLogTail(TalosJson, json)
    }

    /** Last [lines] lines of a CRI container's log (`talosctl logs -k`). */
    suspend fun containerLogs(node: String, containerId: String, lines: Int = 500): LogTail = call { cfg, ctx ->
        decodeLogTail(TalosJson, Ichorgo.containerLogs(cfg, ctx, node, containerId, lines.toLong()))
    }

    /**
     * One followed log line as a structured entry (parsed locally by the Go core, no network);
     * the plain line if it cannot be decoded. Call off the main thread.
     */
    fun parseLogLine(line: String): LogEntry =
        runCatching { TalosJson.decodeFromString(LogEntry.serializer(), Ichorgo.parseLogLine(line)) }
            .getOrNull()
            ?.let { if (it.raw.isEmpty()) it.copy(raw = line) else it }
            ?: LogEntry.plain(line)

    /**
     * Admin kubeconfig (os:admin role), or for a cluster added from a kubeconfig its stored
     * context as imported. A credential: only hand it to where the user chose.
     */
    suspend fun kubeconfig(): String {
        val stored = configs.forCall()
        if (stored.activeIsKube) return withContext(Dispatchers.IO) { Ichorgo.exportKubeContext(stored.kubeYaml, stored.activeContext) }
        return talosKubeCall { cfg, ctx, server -> Ichorgo.kubeconfig(cfg, ctx, server) }
    }

    /** The Kubernetes name of the Talos node [node] (its address), as its kubelet registered it. */
    suspend fun kubeNodeName(node: String): String = call { cfg, ctx -> Ichorgo.kubeNodeName(cfg, ctx, node) }

    /**
     * Who loads the Kubernetes API server over the last [minutes], from the control planes'
     * audit logs read through the Talos API (os:admin): tens of MB, never cached.
     */
    suspend fun auditAnalysis(minutes: Int): AuditReport = call { cfg, ctx ->
        TalosJson.decodeFromString(AuditReport.serializer(), Ichorgo.kubeAuditAnalysis(cfg, ctx, minutes.toLong()))
    }

    /**
     * The taints and labels of [node] (os:admin): its Talos address, or its Kubernetes name on a
     * kubeconfig cluster, like [cordon].
     */
    suspend fun nodeMeta(node: String): KubeNodeMeta {
        val json = if (configs.forCall().activeIsKube) {
            kubeCall { cfg, ctx, server -> Ichorgo.kubeNodeMeta(cfg, ctx, server, node) }
        } else {
            talosKubeCall { cfg, ctx, server -> Ichorgo.nodeMeta(cfg, ctx, server, node) }
        }
        return TalosJson.decodeFromString(KubeNodeMeta.serializer(), json)
    }

    /** What [edits] would change on [node] (a dry run through the API server); never cached. */
    suspend fun nodeMetaPlan(node: String, edits: NodeMetaEdits): NodeMetaPlan {
        val body = edits.toJson()
        val json = if (configs.forCall().activeIsKube) {
            kubeCall { cfg, ctx, server -> Ichorgo.kubeNodeMetaPlan(cfg, ctx, server, node, body) }
        } else {
            talosKubeCall { cfg, ctx, server -> Ichorgo.nodeMetaPlan(cfg, ctx, server, node, body) }
        }
        return TalosJson.decodeFromString(NodeMetaPlan.serializer(), json)
    }

    /** Applies [edits] to [node]; [force] lets protected taints and labels change. Throws when refused. */
    suspend fun nodeMetaApply(node: String, edits: NodeMetaEdits, force: Boolean) {
        val body = edits.toJson()
        if (configs.forCall().activeIsKube) {
            kubeCall { cfg, ctx, server -> Ichorgo.kubeNodeMetaApply(cfg, ctx, server, node, body, force) }
        } else {
            talosKubeCall { cfg, ctx, server -> Ichorgo.nodeMetaApply(cfg, ctx, server, node, body, force) }
        }
    }

    /** Streams the server-side health check; cancelling the collector cancels the check. */
    fun health(): Flow<HealthEvent> = streams.health()

    suspend fun network(node: String): NodeNetwork = remember(networkKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(NodeNetwork.serializer(), Ichorgo.nodeNetwork(cfg, ctx, node)) }
    }

    /** The node's sockets, like `talosctl netstat -a -p` (not cached: always fresh). */
    suspend fun connections(node: String): List<ConnectionInfo> = call { cfg, ctx ->
        TalosJson.decodeFromString(ListSerializer(ConnectionInfo.serializer()), Ichorgo.nodeConnections(cfg, ctx, node))
    }

    /** The node's clock compared with its NTP server (not cached: an offset goes stale fast). */
    suspend fun nodeTime(node: String): NodeTime = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeTime.serializer(), Ichorgo.nodeTime(cfg, ctx, node))
    }

    suspend fun clusterTime(): ClusterTime = remember(CLUSTER_TIME) {
        call { cfg, ctx -> TalosJson.decodeFromString(ClusterTime.serializer(), Ichorgo.clusterTime(cfg, ctx)) }
    }

    suspend fun hardware(node: String): NodeHardware = remember(hardwareKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(NodeHardware.serializer(), Ichorgo.nodeHardware(cfg, ctx, node)) }
    }

    /** Temperatures, fans, CPU frequencies and PCI devices; read on the Hardware screen only. */
    suspend fun sensors(node: String): NodeSensors = remember(sensorsKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(NodeSensors.serializer(), Ichorgo.nodeSensors(cfg, ctx, node)) }
    }

    suspend fun images(node: String): List<ImageInfo> = remember(imagesKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(ListSerializer(ImageInfo.serializer()), Ichorgo.nodeImages(cfg, ctx, node)) }
    }

    /** The images Talos runs on node outside of any app, with their digests (os:admin: the machine config). */
    suspend fun systemImages(node: String): List<SystemImage> = call { cfg, ctx ->
        TalosJson.decodeFromString(ListSerializer(SystemImage.serializer()), Ichorgo.talosSystemImages(cfg, ctx, node))
    }

    /** The apps running in the cluster. One container listing per node: on demand, never polled. */
    suspend fun inventory(): Inventory = remember(INVENTORY) {
        call { cfg, ctx -> TalosJson.decodeFromString(Inventory.serializer(), Ichorgo.clusterInventory(cfg, ctx)) }
    }

    /**
     * A new single-context talosconfig for the active context with [roles] (comma-separated),
     * valid [ttlHours] (os:admin). A credential: never cached, logged or written by this class.
     */
    suspend fun generateTalosconfig(roles: String, ttlHours: Int): String = call { cfg, ctx ->
        Ichorgo.generateTalosconfig(cfg, ctx, roles, ttlHours.toLong())
    }

    /**
     * What [node]'s Talos version supports. Cached: it only changes with an upgrade, and
     * [invalidate] (or pull-to-refresh of the overview) drops it.
     */
    suspend fun features(node: String): NodeFeatures = cached<NodeFeatures>(featuresKey(node))?.value
        ?: remember(featuresKey(node)) {
            call { cfg, ctx -> TalosJson.decodeFromString(NodeFeatures.serializer(), Ichorgo.nodeFeatures(cfg, ctx, node)) }
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
            results.forgetContaining(FEATURES_PREFIX)
        } else {
            results.forget(featuresKey(node))
        }
        _featureChanges.value++
    }

    /** Mounted filesystems with their usage, like `talosctl mounts`. */
    suspend fun mounts(node: String): MountList = call { cfg, ctx ->
        TalosJson.decodeFromString(MountList.serializer(), Ichorgo.nodeMounts(cfg, ctx, node))
    }

    /** Volume status (`talosctl get volumestatus`); [VolumeList.supported] false on older Talos. */
    suspend fun volumes(node: String): VolumeList = call { cfg, ctx ->
        TalosJson.decodeFromString(VolumeList.serializer(), Ichorgo.nodeVolumes(cfg, ctx, node))
    }

    /** Disks with their partitions, free space and encryption; [DiskLayoutList.supported] false before Talos 1.8. */
    suspend fun disks(node: String): DiskLayoutList = call { cfg, ctx ->
        TalosJson.decodeFromString(DiskLayoutList.serializer(), Ichorgo.nodeDisks(cfg, ctx, node))
    }

    /** `talosctl usage PATH -d DEPTH`. */
    suspend fun diskUsage(node: String, path: String, depth: Int): DiskUsage = call { cfg, ctx ->
        TalosJson.decodeFromString(DiskUsage.serializer(), Ichorgo.nodeDiskUsage(cfg, ctx, node, path, depth.toLong()))
    }

    /** SMART/NVMe health per disk; [DiskHealthReport.supported] false on older Talos. */
    suspend fun diskHealth(node: String): DiskHealthReport = call { cfg, ctx ->
        TalosJson.decodeFromString(DiskHealthReport.serializer(), Ichorgo.nodeDiskHealth(cfg, ctx, node))
    }

    /** Resource types the node knows (`talosctl get rd`). */
    suspend fun resourceTypes(node: String): List<ResourceType> = remember(resourceTypesKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(ListSerializer(ResourceType.serializer()), Ichorgo.resourceTypes(cfg, ctx, node)) }
    }

    /** `talosctl get TYPE -n NODE --namespace NAMESPACE` (never cached: may be sensitive). */
    suspend fun resourceList(node: String, namespace: String, type: String): ResourceList = call { cfg, ctx ->
        TalosJson.decodeFromString(ResourceList.serializer(), Ichorgo.resourceList(cfg, ctx, node, namespace, type))
    }

    /** `talosctl get TYPE ID -o yaml` (never cached: may hold secrets). */
    suspend fun resourceGet(node: String, namespace: String, type: String, id: String): String = call { cfg, ctx ->
        TalosJson.decodeFromString(ResourceDetail.serializer(), Ichorgo.resourceGet(cfg, ctx, node, namespace, type, id)).yaml
    }

    suspend fun driftSnapshot(): String = call { cfg, ctx -> Ichorgo.clusterDriftSnapshot(cfg, ctx) }
    suspend fun observation(): String = call { cfg, ctx -> Ichorgo.clusterObservation(cfg, ctx) }
    suspend fun bottlenecks(previous: NodeStats, current: NodeStats): name.levis.ichor.model.Bottlenecks = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(name.levis.ichor.model.Bottlenecks.serializer(), Ichorgo.calculateBottlenecks(
            TalosJson.encodeToString(NodeStats.serializer(), previous), TalosJson.encodeToString(NodeStats.serializer(), current),
        ))
    }

    private suspend fun <T> call(block: (config: String, context: String) -> T): T = go.talos(block)

    private suspend fun <T> kubeCall(block: (config: String, context: String, kubeServer: String) -> T): T = go.kube(block)

    private suspend fun <T> talosKubeCall(block: (config: String, context: String, kubeServer: String) -> T): T = go.talosKube(block)
}
