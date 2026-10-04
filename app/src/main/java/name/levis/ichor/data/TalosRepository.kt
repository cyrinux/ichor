package name.levis.ichor.data

import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.R
import name.levis.ichorgo.EventListener
import name.levis.ichorgo.HealthListener
import name.levis.ichorgo.LogListener
import name.levis.ichorgo.MaintenanceListener
import name.levis.ichorgo.MaintenanceRun
import name.levis.ichorgo.SnapshotListener
import name.levis.ichorgo.Ichorgo
import name.levis.ichor.model.ArgoAction
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoNetwork
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ArgoSyncOptions
import name.levis.ichor.model.CgroupReport
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.DataServices
import name.levis.ichor.model.GarageBlockReport
import name.levis.ichor.model.GarageInstance
import name.levis.ichor.model.GarageRepairResult
import name.levis.ichor.model.LonghornAction
import name.levis.ichor.model.MaintenanceAction
import name.levis.ichor.model.MaintenancePlan
import name.levis.ichor.model.EtcdOverview
import name.levis.ichor.model.ClusterTopology
import name.levis.ichor.model.KubeSpanOverview
import name.levis.ichor.model.KubePod
import name.levis.ichor.model.KubeCronJob
import name.levis.ichor.model.KubeCronJobList
import name.levis.ichor.model.KubePodList
import name.levis.ichor.model.KubeRoute
import name.levis.ichor.model.KubeRouteList
import name.levis.ichor.model.RoutePod
import name.levis.ichor.model.KubeRolloutStatus
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.KubeWorkloadList
import name.levis.ichor.model.LogEntry
import name.levis.ichor.model.LogTail
import name.levis.ichor.model.decodeLogTail
import name.levis.ichor.model.NodeStats
import name.levis.ichor.model.PromDiscovery
import name.levis.ichor.model.PromPanel
import name.levis.ichor.model.PromResult
import name.levis.ichor.model.PromSource
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
import name.levis.ichor.model.SnapshotEncryption
import name.levis.ichor.model.SnapshotRecipient
import name.levis.ichor.model.MountList
import name.levis.ichor.model.NodeDiscovery
import name.levis.ichor.model.EndpointMatch
import name.levis.ichor.model.EndpointProbe
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
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.KSerializer
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.withLastKnown
import name.levis.ichor.model.outage

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

/**
 * Read-only access to the Talos API through the Go core. All calls are blocking in Go, so run on IO.
 * [offline] keeps some results on disk, only when the user turned it on.
 */
class TalosRepository(
    private val configs: ConfigRepository,
    private val kubeServers: KubeServers,
    private val offline: OfflineCache? = null,
) {

    /**
     * Last successful results in memory, so screens can show them instantly while refreshing.
     * Keys include the config generation and context, so importing or switching invalidates them.
     * Cluster data reaches the disk only through [offline], when the user turned it on.
     */
    private val cache = java.util.concurrent.ConcurrentHashMap<String, Any>()

    /** A cached result and when it was fetched (epoch millis). */
    data class Timed<T>(val value: T, val at: Long)

    /** The last result of [key]: fetched in this process, or else the last known one [offline]. */
    @Suppress("UNCHECKED_CAST")
    fun <T> cached(key: String): Timed<T>? = (cache[scoped(key)] ?: restored(key)) as Timed<T>?

    /** The active cluster's fingerprint, when its results may be kept on disk (never the demo's). */
    private fun offlineCluster(): String? =
        configs.config.value?.activeSummary?.takeUnless { it.isDemo }?.fingerprint?.takeIf { it.isNotBlank() }

    private fun restored(key: String, cluster: String? = offlineCluster()): Timed<Any>? {
        val serializer = PERSISTED[key.substringBefore('|')] ?: return null
        val stored = offline?.peek(cluster ?: return null, key) ?: return null
        // Stored by an older version whose model no longer decodes: as good as nothing.
        val value = runCatching { TalosJson.decodeFromString(serializer, stored.value) }.getOrNull() ?: return null
        return Timed(value, stored.at)
    }

    private val _restores = MutableStateFlow(0)

    /**
     * Bumped once [restoreOffline] read what [offline] kept: a screen that started loading
     * before (e.g. right after switching cluster) asks [cached] again.
     */
    val restores: StateFlow<Int> = _restores.asStateFlow()

    /** Reads what [offline] kept of the active cluster, so [cached] has it before the first fetch. */
    suspend fun restoreOffline() {
        val cluster = offlineCluster() ?: return
        offline?.load(cluster) ?: return
        _restores.update { it + 1 }
    }

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

    /**
     * Runs [block] and keeps its result for the cluster that was active when it started; on
     * disk too when [persistable] says it describes the cluster.
     */
    private suspend fun <T : Any> remember(
        key: String,
        persistable: (T) -> Boolean = { true },
        block: suspend (last: () -> Timed<T>?) -> T,
    ): T {
        val scope = scoped(key)
        val cluster = offlineCluster()
        val epoch = offline?.epoch ?: 0
        // The last result of the cluster this fetch is for, even if another one is shown by now.
        @Suppress("UNCHECKED_CAST")
        val value = block { (cache[scope] ?: restored(key, cluster)) as Timed<T>? }
        val at = System.currentTimeMillis()
        cache[scope] = Timed(value, at)
        if (cluster != null && persistable(value)) persist(cluster, key, value, at, epoch)
        return value
    }

    private suspend fun persist(cluster: String, key: String, value: Any, at: Long, epoch: Int) {
        @Suppress("UNCHECKED_CAST")
        val serializer = PERSISTED[key.substringBefore('|')] as KSerializer<Any>? ?: return
        try {
            offline?.save(cluster, key, TalosJson.encodeToString(serializer, value), at, epoch)
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            // Not kept on disk (full, key gone): the screen still gets its fresh result.
        }
    }

    suspend fun kubespan(): KubeSpanOverview = remember(KUBESPAN) {
        call { cfg, ctx -> TalosJson.decodeFromString(KubeSpanOverview.serializer(), Ichorgo.kubeSpanStatus(cfg, ctx)) }
    }

    /** The cluster map: nodes, KubeSpan links and sites (zones or shared LANs). */
    suspend fun topology(): ClusterTopology = remember(TOPOLOGY) {
        call { cfg, ctx -> TalosJson.decodeFromString(ClusterTopology.serializer(), Ichorgo.clusterTopology(cfg, ctx)) }
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
    fun events(nodes: List<String>, tail: Int): Flow<StreamItem<TalosEvent>> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startEvents(
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
        val run = Ichorgo.startLogFollow(
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
        val run = Ichorgo.startContainerLogFollow(
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
        Ichorgo.nodeMachineConfig(cfg, ctx, node, revealSecrets)
    }

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
    fun etcdSnapshot(node: String, destPath: String, encryption: SnapshotEncryption): Flow<SnapshotEvent> = callbackFlow {
        val stored = configs.forCall()
        val listener = object : SnapshotListener {
            override fun onProgress(bytes: Long) {
                trySend(SnapshotEvent.Progress(bytes))
            }

            override fun onDone(path: String, size: Long, sha256: String, errMessage: String) {
                trySend(if (errMessage.isEmpty()) SnapshotEvent.Done(path, size, sha256) else SnapshotEvent.Failed(errMessage))
                close()
            }
        }
        val (yaml, context) = stored.yaml to stored.activeContext
        val run = when (encryption) {
            SnapshotEncryption.None -> Ichorgo.startEtcdSnapshot(yaml, context, node, destPath, listener)
            is SnapshotEncryption.Keys -> Ichorgo.startEtcdSnapshotEncrypted(yaml, context, node, destPath, encryption.recipients, "", listener)
            is SnapshotEncryption.Passphrase -> Ichorgo.startEtcdSnapshotEncrypted(yaml, context, node, destPath, "", encryption.passphrase, listener)
        }
        awaitClose { run.cancel() }
    }.buffer(Channel.CONFLATED) // progress may be dropped, the final event is always kept

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

    /** Admin kubeconfig (os:admin role). A credential: only hand it to where the user chose. */
    suspend fun kubeconfig(): String = kubeCall { cfg, ctx, server -> Ichorgo.kubeconfig(cfg, ctx, server) }

    /** Deployments, StatefulSets and DaemonSets through the Kubernetes API (os:admin: Talos issues the kubeconfig). */
    suspend fun workloads(): List<KubeWorkload> = remember(WORKLOADS) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(KubeWorkloadList.serializer(), Ichorgo.kubeWorkloads(cfg, ctx, server)).workloads }
    }

    /** `kubectl rollout restart KIND/NAME -n NAMESPACE` (os:admin). */
    suspend fun rolloutRestart(workload: KubeWorkload) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeRolloutRestart(cfg, ctx, server, workload.kind, workload.namespace, workload.name)
    }

    /** `kubectl rollout status KIND/NAME -n NAMESPACE` with the pods (os:admin). Never cached: polled. */
    suspend fun rolloutStatus(workload: KubeWorkload): KubeRolloutStatus = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeRolloutStatus.serializer(), Ichorgo.kubeRolloutStatus(cfg, ctx, server, workload.kind, workload.namespace, workload.name))
    }

    /** CronJobs with their recent runs through the Kubernetes API (os:admin). */
    suspend fun cronJobs(): List<KubeCronJob> = remember(CRON_JOBS) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(KubeCronJobList.serializer(), Ichorgo.kubeCronJobs(cfg, ctx, server)).cronJobs }
    }

    /** `kubectl create job --from=cronjob/NAME -n NAMESPACE` (os:admin): the new Job's name. */
    suspend fun triggerCronJob(cronJob: KubeCronJob): String = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeTriggerCronJob(cfg, ctx, server, cronJob.namespace, cronJob.name)
    }

    /** The Ingress and HTTPRoute URLs serving [pods] (os:admin). */
    suspend fun appRoutes(pods: List<RoutePod>): List<KubeRoute> = kubeCall { cfg, ctx, server ->
        val json = TalosJson.encodeToString(ListSerializer(RoutePod.serializer()), pods)
        TalosJson.decodeFromString(KubeRouteList.serializer(), Ichorgo.kubeAppRoutes(cfg, ctx, server, json)).routes
    }

    /** Every pod with the status `kubectl get pods` shows (os:admin). */
    suspend fun pods(): List<KubePod> = remember(PODS) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(KubePodList.serializer(), Ichorgo.kubePods(cfg, ctx, server)).pods }
    }

    /**
     * Health of Longhorn, Garage and CloudNativePG (os:admin). [hints]: their catalog ids seen in
     * the inventory (see [name.levis.ichor.model.dataServiceHints]); "" checks everything.
     */
    suspend fun dataServices(hints: String): DataServices = remember(DATA_SERVICES) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(DataServices.serializer(), Ichorgo.kubeDataServices(cfg, ctx, server, hints)) }
    }

    /** What the blocks failing to resync in a Garage cluster are, run in its ready pod (os:admin). Never cached. */
    suspend fun garageBlockErrors(instance: GarageInstance): GarageBlockReport = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageBlockReport.serializer(), Ichorgo.kubeGarageBlockErrors(cfg, ctx, server, instance.namespace, instance.pod))
    }

    /** Launches the safe repairs for those blocks (os:admin); Garage runs them in the background. */
    suspend fun garageRepairBlocks(instance: GarageInstance): GarageRepairResult = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(GarageRepairResult.serializer(), Ichorgo.kubeGarageRepairBlocks(cfg, ctx, server, instance.namespace, instance.pod))
    }

    /** Sets a node's resync tranquility (os:admin); [nodeId] is a Garage node ID, or "*" for every node. */
    suspend fun garageSetTranquility(instance: GarageInstance, nodeId: String, value: Long) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeGarageSetTranquility(cfg, ctx, server, instance.namespace, instance.pod, nodeId, value)
    }

    /**
     * Runs [action] on the Longhorn volume or node [namespace]/[name] (os:admin); [value] is the
     * replica count of [LonghornAction.REPLICAS]. Throws when refused.
     */
    suspend fun longhornAction(namespace: String, name: String, action: LonghornAction, value: Int = 0) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeLonghornAction(cfg, ctx, server, namespace, name, action.wire, value.toLong())
    }

    /**
     * Argo CD Applications, ApplicationSets and projects through their custom resources
     * (os:admin); `installed` is false without Argo CD.
     */
    suspend fun argoCD(): ArgoStatus = remember(ARGO_CD) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(ArgoStatus.serializer(), Ichorgo.kubeArgoCD(cfg, ctx, server)) }
    }

    /** Runs [action] on [app] (os:admin); [options] for a sync or a rollback. Throws when refused. */
    suspend fun argoAction(app: ArgoApp, action: ArgoAction, options: ArgoSyncOptions? = null) = kubeCall { cfg, ctx, server ->
        val json = options?.let { TalosJson.encodeToString(ArgoSyncOptions.serializer(), it) }.orEmpty()
        Ichorgo.kubeArgoAction(cfg, ctx, server, app.namespace, app.name, action.wire, json)
    }

    /**
     * How traffic reaches [app] (os:admin): hosts, Gateways, routes, Services, pods and nodes.
     * Never cached: the app detail asks again whenever it reloads the app.
     */
    suspend fun argoNetwork(app: ArgoApp): ArgoNetwork = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(ArgoNetwork.serializer(), Ichorgo.kubeArgoNetwork(cfg, ctx, server, app.namespace, app.name))
    }

    /** `kubectl delete pod NAME -n NAMESPACE` (os:admin): its controller starts a new one. */
    suspend fun deletePod(pod: KubePod) = kubeCall { cfg, ctx, server -> Ichorgo.kubeDeletePod(cfg, ctx, server, pod.namespace, pod.name) }

    /** What a maintenance of [node] would do: pods to evict, budgets, reboot checks (os:admin). Never cached. */
    suspend fun maintenancePlan(node: String): MaintenancePlan = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(MaintenancePlan.serializer(), Ichorgo.nodeMaintenancePlan(cfg, ctx, server, node))
    }

    /** `kubectl cordon` ([on]) or `uncordon` of [node] (os:admin). */
    suspend fun cordon(node: String, on: Boolean) = kubeCall { cfg, ctx, server -> Ichorgo.kubeCordon(cfg, ctx, server, node, on) }

    /**
     * Starts the maintenance of [node] (cordon, drain, then [action]); returns at once, the
     * run reports to [listener]. The core refuses blockers, and acknowledgments unless [acknowledged].
     */
    fun startMaintenance(
        node: String,
        action: MaintenanceAction,
        includeBare: Boolean,
        acknowledged: Boolean,
        listener: MaintenanceListener,
    ): MaintenanceRun {
        val stored = configs.forCall()
        val server = stored.activeSummary?.fingerprint?.let { kubeServers.servers.value[it] }.orEmpty()
        return Ichorgo.startNodeMaintenance(stored.yaml, stored.activeContext, server, node, action.wire, includeBare, acknowledged, listener)
    }

    /** `talosctl reboot -m [mode]` (default, powercycle, force); needs os:operator or higher. */
    suspend fun reboot(node: String, mode: String) = call { cfg, ctx -> Ichorgo.reboot(cfg, ctx, node, mode) }

    /** `talosctl shutdown [--force]` (force skips cordon/drain); needs os:operator or higher. */
    suspend fun shutdown(node: String, force: Boolean) = call { cfg, ctx -> Ichorgo.shutdown(cfg, ctx, node, force) }

    /** Streams the server-side health check; cancelling the collector cancels the check. */
    fun health(): Flow<HealthEvent> = callbackFlow {
        val stored = configs.forCall()
        val run = Ichorgo.startClusterHealth(
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

    suspend fun images(node: String): List<ImageInfo> = remember(imagesKey(node)) {
        call { cfg, ctx -> TalosJson.decodeFromString(ListSerializer(ImageInfo.serializer()), Ichorgo.nodeImages(cfg, ctx, node)) }
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
            cache.keys.removeAll { FEATURES_PREFIX in it }
        } else {
            cache.remove(scoped(featuresKey(node)))
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
     * the stored contexts. Not through [call]: it looks for any cluster, on whatever network.
     */
    suspend fun findEndpoints(networks: List<String>): List<EndpointMatch> {
        val stored = configs.config.value ?: throw NoConfigException()
        return withContext(Dispatchers.IO) {
            TalosJson.decodeFromString(
                ListSerializer(EndpointMatch.serializer()),
                Ichorgo.findEndpoints(stored.yaml, networks.joinToString(",")),
            )
        }
    }

    /** Asks [endpoint] its version with [contextName]'s credentials, before adding it. */
    suspend fun probeEndpoint(contextName: String, endpoint: String): EndpointProbe {
        val stored = configs.config.value ?: throw NoConfigException()
        return withContext(Dispatchers.IO) {
            TalosJson.decodeFromString(EndpointProbe.serializer(), Ichorgo.probeEndpoint(stored.yaml, contextName, endpoint))
        }
    }

    /** `talosctl get TYPE ID -o yaml` (never cached: may hold secrets). */
    suspend fun resourceGet(node: String, namespace: String, type: String, id: String): String = call { cfg, ctx ->
        TalosJson.decodeFromString(ResourceDetail.serializer(), Ichorgo.resourceGet(cfg, ctx, node, namespace, type, id)).yaml
    }

    /** Prometheus-compatible query APIs among the cluster's Services, the likeliest first. */
    suspend fun promDiscover(): List<PromSource> = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(PromDiscovery.serializer(), Ichorgo.promDiscover(cfg, ctx, server)).sources
    }

    /** [query] from [start] to [end] (unix seconds) against [source], about 250 points. */
    suspend fun promRange(source: PromSource, query: String, start: Long, end: Long): PromResult = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.promQueryRange(cfg, ctx, server, TalosJson.encodeToString(PromSource.serializer(), source), query, start, end, 0)
        TalosJson.decodeFromString(PromResult.serializer(), json)
    }

    /** The built-in panels. */
    suspend fun promPresets(): List<PromPanel> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ListSerializer(PromPanel.serializer()), Ichorgo.promPresets())
    }

    /** [source] checked and cleaned up by Go, its secret kept. */
    suspend fun normalizePromSource(source: PromSource): PromSource = withContext(Dispatchers.IO) {
        val json = Ichorgo.normalizePromSource(TalosJson.encodeToString(PromSource.serializer(), source))
        val checked = TalosJson.decodeFromString(PromSource.serializer(), json)
        // Go never returns the secret; without authentication there is none to keep.
        checked.copy(secret = if (checked.auth == PromSource.AUTH_NONE) "" else source.secret.trim())
    }

    suspend fun driftSnapshot(): String = call { cfg, ctx -> Ichorgo.clusterDriftSnapshot(cfg, ctx) }
    suspend fun observation(): String = call { cfg, ctx -> Ichorgo.clusterObservation(cfg, ctx) }
    suspend fun bottlenecks(previous: NodeStats, current: NodeStats): name.levis.ichor.model.Bottlenecks = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(name.levis.ichor.model.Bottlenecks.serializer(), Ichorgo.calculateBottlenecks(
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
const val TOPOLOGY = "topology"
const val INVENTORY = "inventory"
const val WORKLOADS = "workloads"
const val PODS = "pods"
const val CRON_JOBS = "cronjobs"
const val DATA_SERVICES = "dataservices"
const val ARGO_CD = "argocd"
fun servicesKey(node: String) = "services|$node"
fun resourcesKey(node: String) = "resources|$node"
const val CLUSTER_TIME = "clustertime"
fun networkKey(node: String) = "network|$node"
fun hardwareKey(node: String) = "hardware|$node"
fun imagesKey(node: String) = "images|$node"

/**
 * The results [TalosRepository] may keep on disk, by key prefix, when "Keep last known state"
 * is on: what describes the cluster. Never live figures (stats, processes, connections, time,
 * logs) nor anything that may hold secrets (machine config, resources, kubeconfig).
 */
private val PERSISTED: Map<String, KSerializer<*>> = mapOf(
    OVERVIEW to ClusterOverview.serializer(),
    ETCD to EtcdOverview.serializer(),
    KUBESPAN to KubeSpanOverview.serializer(),
    TOPOLOGY to ClusterTopology.serializer(),
    INVENTORY to Inventory.serializer(),
    WORKLOADS to ListSerializer(KubeWorkload.serializer()),
    PODS to ListSerializer(KubePod.serializer()),
    CRON_JOBS to ListSerializer(KubeCronJob.serializer()),
    "services" to ListSerializer(ServiceInfo.serializer()),
    "resources" to NodeResources.serializer(),
    "network" to NodeNetwork.serializer(),
    "hardware" to NodeHardware.serializer(),
    "images" to ListSerializer(ImageInfo.serializer()),
)

private const val FEATURES_PREFIX = "|features|"
fun featuresKey(node: String) = "features|$node"
fun resourceTypesKey(node: String) = "resourcetypes|$node"
