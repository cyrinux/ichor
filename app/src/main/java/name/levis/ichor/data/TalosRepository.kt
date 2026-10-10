package name.levis.ichor.data

import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.goErrorText
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.R
import name.levis.ichorgo.ConfigApplyListener
import name.levis.ichorgo.ConfigTryListener
import name.levis.ichorgo.MaintenanceListener
import name.levis.ichorgo.MaintenanceRun
import name.levis.ichorgo.Ichorgo
import name.levis.ichor.model.ConfigApplyEvent
import name.levis.ichor.model.ConfigApplyMode
import name.levis.ichor.model.ConfigApplyProgress
import name.levis.ichor.model.ConfigEdit
import name.levis.ichor.model.ConfigPreview
import name.levis.ichor.model.ConfigSchemaStatus
import name.levis.ichor.model.ConfigTree
import name.levis.ichor.model.ConfigTryCommand
import name.levis.ichor.model.ConfigTryEvent
import name.levis.ichor.model.ConfigTryProgress
import name.levis.ichor.model.AuditReport
import name.levis.ichor.model.CgroupReport
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.ClusterStorageHealth
import name.levis.ichor.model.MaintenanceAction
import name.levis.ichor.model.MaintenancePlan
import name.levis.ichor.model.MaintenanceUpgrade
import name.levis.ichor.model.EtcdOverview
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
import name.levis.ichor.model.Inventory
import name.levis.ichor.model.NodeHardware
import name.levis.ichor.model.NodeSensors
import name.levis.ichor.model.NodeNetwork
import name.levis.ichor.model.NodeTime
import name.levis.ichor.model.DiskHealthReport
import name.levis.ichor.model.DiskUsage
import name.levis.ichor.model.EtcdForfeitResult
import name.levis.ichor.model.EtcdMemberPlan
import name.levis.ichor.model.SnapshotEncryption
import name.levis.ichor.model.SnapshotRecipient
import name.levis.ichor.model.MountList
import name.levis.ichor.model.NodeDiscovery
import name.levis.ichor.model.EndpointMatch
import name.levis.ichor.model.EndpointProbe
import name.levis.ichor.model.KubeSpanDiagAll
import name.levis.ichor.model.NodeFeatures
import name.levis.ichor.model.UpgradeExtensionCheck
import name.levis.ichor.model.NodeResetPlan
import name.levis.ichor.model.ResetRequest
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
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.model.withLastKnown
import name.levis.ichor.model.outage

class NoConfigException : LocalizedException(UiText.Res(R.string.common_no_config))

/**
 * The Talos API through the Go core, and the results cache every repository shares (see
 * [GoCall]): Kubernetes calls live in [KubeRepository], Argo CD and Flux in [GitOpsRepository],
 * data services in [DataServicesRepository].
 */
class TalosRepository(go: GoCall) : GoRepository(go) {

    private val configs = go.configs
    private val kubeServers = go.kubeServers
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

    /** `talosctl -n NODE etcd defrag` (os:operator or os:admin); one member at a time. */
    suspend fun etcdDefragment(node: String) = call { cfg, ctx -> Ichorgo.etcdDefragment(cfg, ctx, node) }

    /** One sample of node counters for the live graphs (not cached: always fresh). */
    suspend fun stats(node: String): NodeStats = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeStats.serializer(), Ichorgo.nodeStats(cfg, ctx, node))
    }

    /** One sample of every node's CPU and memory counters for the live cluster summary (not cached). */
    suspend fun clusterStats(): ClusterStatsSample = call { cfg, ctx ->
        TalosJson.decodeFromString(ClusterStatsSample.serializer(), Ichorgo.clusterStats(cfg, ctx))
    }

    /** One sample of the node's processes for the Processes tab (not cached: always fresh). */
    suspend fun processes(node: String): ProcessSample = call { cfg, ctx ->
        TalosJson.decodeFromString(ProcessSample.serializer(), Ichorgo.nodeProcesses(cfg, ctx, node))
    }

    /** The node's cgroup tree with pressure, for the pressure card and the Cgroups tab (os:admin; not cached). */
    suspend fun cgroups(node: String): CgroupReport = call { cfg, ctx ->
        TalosJson.decodeFromString(CgroupReport.serializer(), Ichorgo.nodeCgroups(cfg, ctx, node))
    }

    /** One sample of the node's CRI containers for the Pods tab (not cached: always fresh). */
    suspend fun containers(node: String): ContainerSample = call { cfg, ctx ->
        TalosJson.decodeFromString(ContainerSample.serializer(), Ichorgo.nodeContainers(cfg, ctx, node))
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

    /**
     * The node's active machine config as YAML (os:admin). Secrets are masked unless
     * [revealSecrets]. Never cached: it may hold secrets.
     */
    suspend fun machineConfig(node: String, revealSecrets: Boolean): String = call { cfg, ctx ->
        Ichorgo.nodeMachineConfig(cfg, ctx, node, revealSecrets)
    }

    /**
     * Makes the Talos config schema of [node]'s version ready for [machineConfigDescribe]:
     * downloaded the first time a version is seen, then kept by the core. A schema that
     * cannot be had is not an error ([ConfigSchemaStatus.available] is false).
     */
    suspend fun machineConfigSchema(node: String): ConfigSchemaStatus = call { cfg, ctx ->
        TalosJson.decodeFromString(ConfigSchemaStatus.serializer(), Ichorgo.machineConfigSchemaPrepare(cfg, ctx, node))
    }

    /** [yaml] (a machine config or a draft of one) as a tree, described with the schema of [talosVersion] when the core has it. Local. */
    suspend fun machineConfigDescribe(yaml: String, talosVersion: String): ConfigTree = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ConfigTree.serializer(), Ichorgo.machineConfigDescribe(yaml, talosVersion))
    }

    /** [draft] with [edit] applied. Local: nothing is sent to the node. */
    suspend fun machineConfigEdit(draft: String, edit: ConfigEdit): String = withContext(Dispatchers.IO) {
        Ichorgo.machineConfigEdit(draft, TalosJson.encodeToString(ConfigEdit.serializer(), edit))
    }

    /**
     * What applying [draft] to [node] would change (os:admin), validated by the node with a dry
     * run. [base] is the redacted config the draft was made from; refused when the node no
     * longer holds it.
     */
    suspend fun machineConfigPreview(node: String, base: String, draft: String): ConfigPreview = call { cfg, ctx ->
        TalosJson.decodeFromString(ConfigPreview.serializer(), Ichorgo.machineConfigPreview(cfg, ctx, node, base, draft))
    }

    /**
     * Applies [draft] to [node] in Talos's try mode for [timeoutSeconds] and follows it: the
     * node reverts by itself unless [ConfigTryCommand.KEEP] comes through [commands] first.
     * Cancelling the collection only stops following; the node still reverts.
     */
    /** StartConfigApply: [draft] applied for good in [mode]; closing the flow stops following it. */
    fun applyMachineConfig(node: String, base: String, draft: String, mode: ConfigApplyMode): Flow<ConfigApplyEvent> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startConfigApply(
            stored.yaml,
            stored.activeContext,
            node,
            base,
            draft,
            mode.wire,
            object : ConfigApplyListener {
                override fun onProgress(json: String) {
                    runCatching { TalosJson.decodeFromString(ConfigApplyProgress.serializer(), json) }
                        .onSuccess { trySend(ConfigApplyEvent.Progress(it)) }
                }

                override fun onDone(errMessage: String) {
                    trySend(ConfigApplyEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    fun tryMachineConfig(node: String, base: String, draft: String, timeoutSeconds: Int, commands: Flow<ConfigTryCommand>): Flow<ConfigTryEvent> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startConfigTry(
            stored.yaml,
            stored.activeContext,
            node,
            base,
            draft,
            timeoutSeconds.toLong(),
            object : ConfigTryListener {
                override fun onProgress(json: String) {
                    runCatching { TalosJson.decodeFromString(ConfigTryProgress.serializer(), json) }
                        .onSuccess { trySend(ConfigTryEvent.Progress(it)) }
                }

                override fun onDone(outcome: String, errMessage: String) {
                    trySend(ConfigTryEvent.Done(outcome, errMessage))
                    close()
                }
            },
        )
        launch {
            commands.collect {
                when (it) {
                    ConfigTryCommand.KEEP -> run.keep()
                    ConfigTryCommand.REVERT -> run.revert()
                }
            }
        }
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    /** `talosctl etcd alarm disarm` through [node] (os:operator or os:admin); alarms are cluster-wide. */
    suspend fun etcdAlarmDisarm(node: String) = call { cfg, ctx -> Ichorgo.etcdAlarmDisarm(cfg, ctx, node) }

    /** Checks the public keys typed for an encrypted snapshot (one per line); throws with the line at fault. */
    suspend fun checkSnapshotRecipients(text: String): List<SnapshotRecipient> = withContext(Dispatchers.Default) {
        TalosJson.decodeFromString(ListSerializer(SnapshotRecipient.serializer()), Ichorgo.checkSnapshotRecipients(text))
    }

    /**
     * Streams `talosctl -n NODE etcd snapshot` into [destPath] (written atomically by Go),
     * age-encrypted while it streams unless [encryption] is [SnapshotEncryption.None].
     * Cancelling the collector cancels the download.
     */
    fun etcdSnapshot(node: String, destPath: String, encryption: SnapshotEncryption): Flow<SnapshotEvent> = streams.etcdSnapshot(node, destPath, encryption)

    /** The one-tap NOSPACE fix (see [TalosStreams.etcdNospaceFix]); needs os:admin. */
    fun etcdNospaceFix(snapshotNode: String, destPath: String, encryption: SnapshotEncryption): Flow<EtcdFixEvent> =
        streams.etcdNospaceFix(snapshotNode, destPath, encryption)

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

    private suspend fun overviewUncached(): ClusterOverview = call { cfg, ctx ->
        TalosJson.decodeFromString(ClusterOverview.serializer(), Ichorgo.clusterOverview(cfg, ctx))
    }

    /** Every node's volume fill and disk SMART verdict (the monitor's storage track; never cached). */
    suspend fun storageHealth(): ClusterStorageHealth = call { cfg, ctx ->
        TalosJson.decodeFromString(ClusterStorageHealth.serializer(), Ichorgo.clusterStorageHealth(cfg, ctx))
    }

    suspend fun services(node: String): List<ServiceInfo> = remember(servicesKey(node)) { servicesUncached(node) }

    private suspend fun servicesUncached(node: String): List<ServiceInfo> = call { cfg, ctx ->
        TalosJson.decodeFromString(ListSerializer(ServiceInfo.serializer()), Ichorgo.nodeServices(cfg, ctx, node))
    }

    suspend fun resources(node: String): NodeResources = remember(resourcesKey(node)) { resourcesUncached(node) }

    private suspend fun resourcesUncached(node: String): NodeResources = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeResources.serializer(), Ichorgo.nodeResources(cfg, ctx, node))
    }

    suspend fun etcd(): EtcdOverview = remember(ETCD) { etcdUncached() }

    private suspend fun etcdUncached(): EtcdOverview = call { cfg, ctx ->
        TalosJson.decodeFromString(EtcdOverview.serializer(), Ichorgo.etcdStatus(cfg, ctx))
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
     * What a maintenance of [node] would do: pods to evict, budgets, reboot checks (os:admin).
     * On a kubeconfig cluster [node] is the Kubernetes node name, and the plan is a drain's
     * (no Talos checks). Never cached.
     */
    suspend fun maintenancePlan(node: String): MaintenancePlan {
        val json = if (configs.forCall().activeIsKube) {
            kubeCall { cfg, ctx, server -> Ichorgo.kubeDrainPlan(cfg, ctx, server, node) }
        } else {
            talosKubeCall { cfg, ctx, server -> Ichorgo.nodeMaintenancePlan(cfg, ctx, server, node) }
        }
        return TalosJson.decodeFromString(MaintenancePlan.serializer(), json)
    }

    /**
     * `kubectl cordon` ([on]) or `uncordon` of [node] (os:admin): its Talos address, or its
     * Kubernetes name on a kubeconfig cluster. The Talos one resolves the node through Talos.
     */
    suspend fun cordon(node: String, on: Boolean) = if (configs.forCall().activeIsKube) {
        kubeCall { cfg, ctx, server -> Ichorgo.kubeNodeCordon(cfg, ctx, server, node, on) }
    } else {
        talosKubeCall { cfg, ctx, server -> Ichorgo.kubeCordon(cfg, ctx, server, node, on) }
    }

    /**
     * Starts the maintenance of [node] (cordon, drain, then [action]); returns at once, the
     * run reports to [listener]. The core refuses blockers, and acknowledgments unless [acknowledged].
     * A kubeconfig cluster only drains ([node]: the Kubernetes node name). [upgrade]: what
     * [MaintenanceAction.UPGRADE] installs, required for it.
     */
    fun startMaintenance(
        node: String,
        action: MaintenanceAction,
        includeBare: Boolean,
        acknowledged: Boolean,
        listener: MaintenanceListener,
        upgrade: MaintenanceUpgrade? = null,
    ): MaintenanceRun {
        val stored = configs.forCall()
        if (stored.activeIsKube) {
            require(action == MaintenanceAction.NONE) { "a cluster without Talos can only be drained" }
            val target = kubeServers.targetFor(stored)
            return Ichorgo.startKubeDrain(target.yaml, target.context, target.server, node, includeBare, listener)
        }
        val server = kubeServers.serverFor(stored)
        if (action == MaintenanceAction.UPGRADE) {
            val up = requireNotNull(upgrade) { "an upgrade maintenance needs its installer image" }
            return Ichorgo.startNodeMaintenanceUpgrade(
                stored.yaml, stored.activeContext, server, node, up.image, includeBare, acknowledged, up.force, listener,
            )
        }
        return Ichorgo.startNodeMaintenance(stored.yaml, stored.activeContext, server, node, action.wire, includeBare, acknowledged, listener)
    }

    /** `talosctl reboot -m [mode]` (default, powercycle, force); needs os:operator or higher. */
    /** [node]'s extensions against the Image Factory's official list for [image]'s version. Read-only. */
    suspend fun upgradeExtensionCheck(node: String, image: String): UpgradeExtensionCheck = call { cfg, ctx ->
        TalosJson.decodeFromString(UpgradeExtensionCheck.serializer(), Ichorgo.upgradeExtensionCheck(cfg, ctx, node, image))
    }

    suspend fun reboot(node: String, mode: String) = call { cfg, ctx -> Ichorgo.reboot(cfg, ctx, node, mode) }

    /** What resetting [node] would wipe and leave, and what forbids it. Read-only. */
    suspend fun resetPlan(node: String): NodeResetPlan = talosKubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(NodeResetPlan.serializer(), Ichorgo.nodeResetPlan(cfg, ctx, server, node))
    }

    /** `talosctl reset --wipe-mode --graceful --reboot` on [node] (os:admin); refused on a plan blocker. */
    suspend fun reset(node: String, request: ResetRequest) = call { cfg, ctx ->
        Ichorgo.nodeReset(cfg, ctx, node, request.wipe.wire, request.graceful, request.reboot)
    }

    /** `talosctl rollback`: [node] reboots into the Talos it ran before its last upgrade (os:admin). */
    suspend fun rollback(node: String) = call { cfg, ctx -> Ichorgo.rollback(cfg, ctx, node) }

    /** `talosctl shutdown [--force]` (force skips cordon/drain); needs os:operator or higher. */
    suspend fun shutdown(node: String, force: Boolean) = call { cfg, ctx -> Ichorgo.shutdown(cfg, ctx, node, force) }

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

    /** `talosctl usage PATH -d DEPTH`. */
    suspend fun diskUsage(node: String, path: String, depth: Int): DiskUsage = call { cfg, ctx ->
        TalosJson.decodeFromString(DiskUsage.serializer(), Ichorgo.nodeDiskUsage(cfg, ctx, node, path, depth.toLong()))
    }

    /** SMART/NVMe health per disk; [DiskHealthReport.supported] false on older Talos. */
    suspend fun diskHealth(node: String): DiskHealthReport = call { cfg, ctx ->
        TalosJson.decodeFromString(DiskHealthReport.serializer(), Ichorgo.nodeDiskHealth(cfg, ctx, node))
    }

    /** `talosctl -n NODE etcd forfeit-leadership` (os:admin); [node] must be the leader. */
    suspend fun etcdForfeitLeadership(node: String): EtcdForfeitResult = call { cfg, ctx ->
        TalosJson.decodeFromString(EtcdForfeitResult.serializer(), Ichorgo.etcdForfeitLeadership(cfg, ctx, node))
    }

    /** `talosctl -n NODE etcd remove-member MEMBER` (os:admin), asked to another member's [node]. */
    suspend fun etcdRemoveMember(node: String, memberId: String) = call { cfg, ctx ->
        Ichorgo.etcdRemoveMember(cfg, ctx, node, memberId)
    }

    /** What removing [memberId] would leave (members, quorum) and what forbids it. Read-only. */
    suspend fun etcdMemberPlan(memberId: String): EtcdMemberPlan = call { cfg, ctx ->
        TalosJson.decodeFromString(EtcdMemberPlan.serializer(), Ichorgo.etcdMemberPlan(cfg, ctx, memberId))
    }

    /** Resource types the node knows (`talosctl get rd`). */
    suspend fun resourceTypes(node: String): List<ResourceType> = remember(resourceTypesKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(ListSerializer(ResourceType.serializer()), Ichorgo.resourceTypes(cfg, ctx, node)) }
    }

    /** `talosctl get TYPE -n NODE --namespace NAMESPACE` (never cached: may be sensitive). */
    suspend fun resourceList(node: String, namespace: String, type: String): ResourceList = call { cfg, ctx ->
        TalosJson.decodeFromString(ResourceList.serializer(), Ichorgo.resourceList(cfg, ctx, node, namespace, type))
    }

    /** The cluster's members (Talos cluster discovery), flagged when the context already targets them. */
    suspend fun discoverNodes(): NodeDiscovery = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeDiscovery.serializer(), Ichorgo.discoverNodes(cfg, ctx))
    }

    /**
     * Hosts of [networks] (IPv4 or IPv6 CIDRs) answering the Talos API with the credentials of one of
     * the stored Talos contexts. Not through [call]: it looks for any cluster, on whatever network.
     */
    suspend fun findEndpoints(networks: List<String>): List<EndpointMatch> {
        val stored = configs.config.value ?: throw NoConfigException()
        // Only clusters added from a kubeconfig: no Talos credentials to search with.
        if (stored.talosYaml.isBlank()) return emptyList()
        return withContext(Dispatchers.IO) {
            TalosJson.decodeFromString(
                ListSerializer(EndpointMatch.serializer()),
                Ichorgo.findEndpoints(stored.talosYaml, networks.joinToString(",")),
            )
        }
    }

    /** Asks [endpoint] its version with [contextName]'s credentials, before adding it. */
    suspend fun probeEndpoint(contextName: String, endpoint: String): EndpointProbe {
        val stored = configs.config.value ?: throw NoConfigException()
        return withContext(Dispatchers.IO) {
            TalosJson.decodeFromString(EndpointProbe.serializer(), Ichorgo.probeEndpoint(stored.yamlFor(contextName), contextName, endpoint))
        }
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
