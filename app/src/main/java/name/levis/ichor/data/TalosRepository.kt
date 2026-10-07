package name.levis.ichor.data

import name.levis.ichor.data.realFingerprint
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.R
import name.levis.ichorgo.ConfigTryListener
import name.levis.ichorgo.MaintenanceListener
import name.levis.ichorgo.MaintenanceRun
import name.levis.ichorgo.Ichorgo
import name.levis.ichor.model.ApiHealthReport
import name.levis.ichor.model.ConfigEdit
import name.levis.ichor.model.ConfigPreview
import name.levis.ichor.model.ConfigSchemaStatus
import name.levis.ichor.model.ConfigTree
import name.levis.ichor.model.ConfigTryCommand
import name.levis.ichor.model.ConfigTryEvent
import name.levis.ichor.model.ConfigTryProgress
import name.levis.ichor.model.CheckupReport
import name.levis.ichor.model.KubeEvent
import name.levis.ichor.model.KubeEventList
import name.levis.ichor.model.AuditReport
import name.levis.ichor.model.ArgoAction
import name.levis.ichor.model.SupportedIntegrations
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoFreezeAction
import name.levis.ichor.model.ArgoFreezeOptions
import name.levis.ichor.model.ArgoNetwork
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.ArgoSyncOptions
import name.levis.ichor.model.CertDetails
import name.levis.ichor.model.FluxAction
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.model.FluxStatus
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
import name.levis.ichor.model.KubePodPage
import name.levis.ichor.model.PodPhaseFilter
import name.levis.ichor.model.PodSelection
import name.levis.ichor.model.SELECTED_PODS_PAGE
import name.levis.ichor.model.KubeCronJobPage
import name.levis.ichor.model.KubeWorkloadPage
import name.levis.ichor.model.KubeNamespaces
import name.levis.ichor.model.KubeNodesOverview
import name.levis.ichor.model.KubePage
import name.levis.ichor.model.KUBE_PAGE_SIZE
import name.levis.ichor.model.KubeRoute
import name.levis.ichor.model.KubeRouteList
import name.levis.ichor.model.RoutePod
import name.levis.ichor.model.WorkloadRef
import name.levis.ichor.model.KubeRolloutStatus
import name.levis.ichor.model.KubeRevision
import name.levis.ichor.model.KubeRevisionList
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
import name.levis.ichor.model.toGoJson
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
import name.levis.ichor.model.IntegrationReport
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
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.KSerializer
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.model.withLastKnown
import name.levis.ichor.model.outage

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

    private val streams = TalosStreams(configs)
    private val results = ResultCache(
        offline,
        scope = { "${configs.generation.value}|${configs.config.value?.activeContext}|" },
        // Never the demo's: its results stay in memory.
        cluster = { configs.config.value?.realFingerprint },
    )

    /** A cached result and when it was fetched (epoch millis). */
    data class Timed<T>(val value: T, val at: Long)

    /** The last result of [key]: fetched in this process, or else the last known one kept offline. */
    fun <T> cached(key: String): Timed<T>? = results.cached(key)

    /** Bumped once [restoreOffline] read what was kept offline (see [ResultCache.restores]). */
    val restores: StateFlow<Int> get() = results.restores

    suspend fun restoreOffline() = results.restoreOffline()

    /** Bumped by [invalidate]; screens showing cluster data reload when it changes. */
    val invalidations: StateFlow<Int> get() = results.invalidations

    fun invalidate() = results.invalidate()

    private suspend fun <T : Any> remember(
        key: String,
        persistable: (T) -> Boolean = { true },
        block: suspend (last: () -> Timed<T>?) -> T,
    ): T = results.remember(key, persistable, block)

    fun keeper(key: String): suspend (Any) -> Unit = results.keeper(key)

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

    /**
     * Admin kubeconfig (os:admin role), or for a cluster added from a kubeconfig its stored
     * context as imported. A credential: only hand it to where the user chose.
     */
    suspend fun kubeconfig(): String {
        val stored = configs.forCall()
        if (stored.activeIsKube) return withContext(Dispatchers.IO) { Ichorgo.exportKubeContext(stored.kubeYaml, stored.activeContext) }
        return talosKubeCall { cfg, ctx, server -> Ichorgo.kubeconfig(cfg, ctx, server) }
    }

    /** The cluster's nodes as Kubernetes lists them: the home of a cluster added from a kubeconfig. */
    suspend fun kubeNodes(): KubeNodesOverview = remember(KUBE_NODES) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(KubeNodesOverview.serializer(), Ichorgo.kubeNodes(cfg, ctx, server)) }
    }

    /**
     * The Deployments, StatefulSets and DaemonSets running [pods] (an app's), through their
     * owners (os:admin): only those pods and owners are read, never a cluster-wide list.
     */
    suspend fun appWorkloads(pods: List<RoutePod>): List<KubeWorkload> = kubeCall { cfg, ctx, server ->
        val json = TalosJson.encodeToString(ListSerializer(RoutePod.serializer()), pods)
        TalosJson.decodeFromString(KubeWorkloadList.serializer(), Ichorgo.kubeAppWorkloads(cfg, ctx, server, json)).workloads
    }

    /** [workloads] as they are now (os:admin); one deleted since is left out. */
    suspend fun workloadsNamed(workloads: List<WorkloadRef>): List<KubeWorkload> = kubeCall { cfg, ctx, server ->
        val json = TalosJson.encodeToString(ListSerializer(WorkloadRef.serializer()), workloads)
        TalosJson.decodeFromString(KubeWorkloadList.serializer(), Ichorgo.kubeWorkloadsNamed(cfg, ctx, server, json)).workloads
    }

    /** `kubectl rollout restart KIND/NAME -n NAMESPACE` (os:admin). */
    suspend fun rolloutRestart(workload: KubeWorkload) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeRolloutRestart(cfg, ctx, server, workload.kind, workload.namespace, workload.name)
    }

    /** `kubectl rollout status KIND/NAME -n NAMESPACE` with the pods (os:admin). Never cached: polled. */
    suspend fun rolloutStatus(workload: KubeWorkload): KubeRolloutStatus = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeRolloutStatus.serializer(), Ichorgo.kubeRolloutStatus(cfg, ctx, server, workload.kind, workload.namespace, workload.name))
    }

    /**
     * `kubectl scale KIND/NAME --replicas=N -n NAMESPACE` (os:admin): a warning ("" when none)
     * when a HorizontalPodAutoscaler manages the replicas and will change them again.
     */
    suspend fun scale(workload: KubeWorkload, replicas: Int): String = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeScale(cfg, ctx, server, workload.kind, workload.namespace, workload.name, replicas.toLong())
    }

    /** `kubectl rollout history deployment/NAME -n NAMESPACE`, newest first (os:admin). Never cached. */
    suspend fun deploymentRevisions(workload: KubeWorkload): List<KubeRevision> = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeRevisionList.serializer(), Ichorgo.kubeDeploymentRevisions(cfg, ctx, server, workload.namespace, workload.name)).revisions
    }

    /** `kubectl rollout undo deployment/NAME --to-revision=N -n NAMESPACE` (os:admin). */
    suspend fun rollbackDeployment(workload: KubeWorkload, revision: Int) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeRollbackDeployment(cfg, ctx, server, workload.namespace, workload.name, revision.toLong())
    }

    /** CronJobs with their recent runs through the Kubernetes API (os:admin). */
    suspend fun cronJobs(): List<KubeCronJob> = remember(CRON_JOBS) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(KubeCronJobList.serializer(), Ichorgo.kubeCronJobs(cfg, ctx, server)).cronJobs }
    }

    /** `kubectl create job --from=cronjob/NAME -n NAMESPACE` (os:admin): the new Job's name. */
    suspend fun triggerCronJob(cronJob: KubeCronJob): String = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeTriggerCronJob(cfg, ctx, server, cronJob.namespace, cronJob.name)
    }

    /** Suspends (no new runs) or resumes [cronJob] (os:admin). */
    suspend fun suspendCronJob(cronJob: KubeCronJob, suspend: Boolean) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeSuspendCronJob(cfg, ctx, server, cronJob.namespace, cronJob.name, suspend)
    }

    /** The Ingress and HTTPRoute URLs serving [pods] (os:admin). */
    suspend fun appRoutes(pods: List<RoutePod>): List<KubeRoute> = kubeCall { cfg, ctx, server ->
        val json = TalosJson.encodeToString(ListSerializer(RoutePod.serializer()), pods)
        TalosJson.decodeFromString(KubeRouteList.serializer(), Ichorgo.kubeAppRoutes(cfg, ctx, server, json)).routes
    }

    /**
     * `kubectl logs POD [-c CONTAINER] [--previous] --tail=N` through the Kubernetes API
     * (os:admin): [previous] reads the container's last terminated run. [container] "" for a
     * pod with one container.
     */
    suspend fun podLogs(pod: KubePod, container: String, previous: Boolean, tailLines: Int): String = kubeCall { cfg, ctx, server ->
        Ichorgo.kubePodLogs(cfg, ctx, server, pod.namespace, pod.name, container, previous, tailLines.toLong())
    }

    /** The cluster's namespaces, to pick the scope of the Kubernetes lists (os:admin). */
    suspend fun namespaces(): KubeNamespaces = remember(NAMESPACES) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(KubeNamespaces.serializer(), Ichorgo.kubeNamespaces(cfg, ctx, server)) }
    }

    /**
     * One page of the pods of [namespace] (null for every one), in the API server's order
     * (os:admin). [table]: the server's Table rows, without images nor containers ([pod]
     * reads those). [token]: the previous page's, "" for the first.
     */
    suspend fun podsPage(namespace: String?, token: String, table: Boolean, limit: Int = KUBE_PAGE_SIZE): KubePage<KubePod> = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.kubePodsPage(cfg, ctx, server, namespace.orEmpty(), token, limit.toLong(), table)
        TalosJson.decodeFromString(KubePodPage.serializer(), json).toPage(detailed = !table)
    }

    /** The Kubernetes name of the Talos node [node] (its address), as its kubelet registered it. */
    suspend fun kubeNodeName(node: String): String = call { cfg, ctx -> Ichorgo.kubeNodeName(cfg, ctx, node) }

    /**
     * One page of the pods scheduled on the Kubernetes node [kubeNode] ([kubeNodeName]), in
     * every namespace, narrowed to [phase]; as [podsPage] otherwise (os:admin).
     */
    suspend fun nodePodsPage(kubeNode: String, phase: PodPhaseFilter, token: String, table: Boolean, limit: Int = SELECTED_PODS_PAGE): KubePage<KubePod> =
        kubeCall { cfg, ctx, server ->
            val json = Ichorgo.kubeNodePodsPage(cfg, ctx, server, kubeNode, phase.query, token, limit.toLong(), table)
            TalosJson.decodeFromString(KubePodPage.serializer(), json).toPage(detailed = !table)
        }

    /** One page of the pods [workload]'s selector matches, narrowed to [phase]; as [podsPage] otherwise (os:admin). */
    suspend fun workloadPodsPage(workload: PodSelection.OfWorkload, phase: PodPhaseFilter, token: String, table: Boolean, limit: Int = SELECTED_PODS_PAGE): KubePage<KubePod> =
        kubeCall { cfg, ctx, server ->
            val json = Ichorgo.kubeWorkloadPodsPage(cfg, ctx, server, workload.kind, workload.namespace, workload.name, phase.query, token, limit.toLong(), table)
            TalosJson.decodeFromString(KubePodPage.serializer(), json).toPage(detailed = !table)
        }

    /** One pod in full (images, containers, last termination), for a row read from a Table (os:admin). */
    suspend fun pod(namespace: String, name: String): KubePod = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(KubePod.serializer(), Ichorgo.kubePod(cfg, ctx, server, namespace, name))
    }

    /** One page of the workloads of [kind] in [namespace] (null for every one), as [podsPage] (os:admin). */
    suspend fun workloadsPage(kind: String, namespace: String?, token: String, limit: Int = KUBE_PAGE_SIZE): KubePage<KubeWorkload> = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.kubeWorkloadsPage(cfg, ctx, server, kind, namespace.orEmpty(), token, limit.toLong())
        TalosJson.decodeFromString(KubeWorkloadPage.serializer(), json).toPage()
    }

    /** One workload as it is now (os:admin): its replicas, for a restart asked outside the Workloads list. */
    suspend fun workload(kind: String, namespace: String, name: String): KubeWorkload = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeWorkload.serializer(), Ichorgo.kubeWorkload(cfg, ctx, server, kind, namespace, name))
    }

    /**
     * The workload from a list already loaded: of its namespace or of every namespace; null
     * when none holds it.
     */
    fun cachedWorkload(kind: String, namespace: String, name: String): KubeWorkload? =
        listOf(workloadsKey(namespace), workloadsKey(null)).firstNotNullOfOrNull { key ->
            cached<List<KubeWorkload>>(key)?.value?.firstOrNull { it.kind == kind && it.namespace == namespace && it.name == name }
        }

    /** One page of the CronJobs of [namespace] (null for every one) with their runs, as [podsPage] (os:admin). */
    suspend fun cronJobsPage(namespace: String?, token: String, limit: Int = KUBE_PAGE_SIZE): KubePage<KubeCronJob> = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.kubeCronJobsPage(cfg, ctx, server, namespace.orEmpty(), token, limit.toLong())
        TalosJson.decodeFromString(KubeCronJobPage.serializer(), json).toPage()
    }

    /**
     * Health of Longhorn, Garage and CloudNativePG (os:admin). [hints]: their catalog ids seen in
     * the inventory (see [name.levis.ichor.model.dataServiceHints]); "" checks everything.
     */
    suspend fun dataServices(hints: String): DataServices = remember(DATA_SERVICES) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(DataServices.serializer(), Ichorgo.kubeDataServices(cfg, ctx, server, hints)) }
    }

    /**
     * The projects the app integrates with and whether the cluster runs each one, from one API
     * discovery (os:admin). [hints]: see [Inventory.supportedIntegrationHints]. Never cached.
     */
    suspend fun supportedIntegrations(hints: String): SupportedIntegrations = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(SupportedIntegrations.serializer(), Ichorgo.kubeSupportedIntegrations(cfg, ctx, server, hints))
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
     * What explains the state of the cert-manager certificate [namespace]/[name] (os:admin): its
     * requests, ACME orders and challenges, their events and controller log lines. Never cached.
     */
    suspend fun certificateDetails(namespace: String, name: String): CertDetails = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(CertDetails.serializer(), Ichorgo.kubeCertManagerDetails(cfg, ctx, server, namespace, name))
    }

    /**
     * Issues the cert-manager certificate [namespace]/[name] again now, like `cmctl renew`
     * (os:admin). Throws when refused, also while it is already being issued.
     */
    suspend fun renewCertificate(namespace: String, name: String) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeCertManagerRenew(cfg, ctx, server, namespace, name)
    }

    /**
     * Starts a backup of the CloudNativePG cluster [namespace]/[name] now, like `kubectl cnpg backup`
     * (os:admin): the new Backup's name. Throws when refused (hibernated, no backup method, one running).
     */
    suspend fun cnpgBackup(namespace: String, name: String): String = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeCNPGBackup(cfg, ctx, server, namespace, name)
    }

    /**
     * The Kubernetes API server's health and what loads it (os:admin): readyz and livez, then
     * two /metrics scrapes a few seconds apart for the live rates. Live figures: never cached.
     */
    suspend fun apiHealth(): ApiHealthReport = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(ApiHealthReport.serializer(), Ichorgo.kubeAPIHealth(cfg, ctx, server))
    }

    /**
     * The cluster checkup (os:admin): what no other screen shows, section by section. It lists the
     * cluster's pods and asks every kubelet for its volumes: on demand, never cached.
     */
    suspend fun checkup(): CheckupReport = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(CheckupReport.serializer(), Ichorgo.kubeCheckup(cfg, ctx, server))
    }

    /**
     * The Kubernetes events of the [kind] named [name] in [namespace], newest first (os:admin); with
     * an empty [kind], those of [name] and of what it owns by name (ReplicaSets, pods).
     */
    suspend fun kubeEvents(namespace: String, kind: String, name: String): List<KubeEvent> = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeEventList.serializer(), Ichorgo.kubeEvents(cfg, ctx, server, namespace, kind, name)).events
    }

    /**
     * Who loads the Kubernetes API server over the last [minutes], from the control planes'
     * audit logs read through the Talos API (os:admin): tens of MB, never cached.
     */
    suspend fun auditAnalysis(minutes: Int): AuditReport = call { cfg, ctx ->
        TalosJson.decodeFromString(AuditReport.serializer(), Ichorgo.kubeAuditAnalysis(cfg, ctx, minutes.toLong()))
    }

    /**
     * Runs [action] on the Longhorn volume or node [namespace]/[name] (os:admin); [value] is the
     * replica count of [LonghornAction.REPLICAS]. Throws when refused.
     */
    suspend fun longhornAction(namespace: String, name: String, action: LonghornAction, value: Int = 0) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeLonghornAction(cfg, ctx, server, namespace, name, action.wire, value.toLong())
    }

    /**
     * The API groups the cluster serves that Ichor does not read yet, by operator, with their
     * kinds (os:admin): what an integration request can name. Never cached.
     */
    suspend fun integrations(): IntegrationReport = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(IntegrationReport.serializer(), Ichorgo.kubeIntegrations(cfg, ctx, server))
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
     * Changes the sync windows of the AppProject [namespace]/[project] (os:admin): freezes apps
     * for a while, extends or ends a freeze, removes a window, clears ended freezes. Throws when
     * refused.
     */
    suspend fun argoFreeze(namespace: String, project: String, action: ArgoFreezeAction, options: ArgoFreezeOptions = ArgoFreezeOptions()) =
        kubeCall { cfg, ctx, server ->
            Ichorgo.kubeArgoFreeze(cfg, ctx, server, namespace, project, action.wire, TalosJson.encodeToString(ArgoFreezeOptions.serializer(), options))
        }

    /**
     * How traffic reaches [app] (os:admin): hosts, Gateways, routes, Services, pods and nodes.
     * Never cached: the app detail asks again whenever it reloads the app.
     */
    suspend fun argoNetwork(app: ArgoApp): ArgoNetwork = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(ArgoNetwork.serializer(), Ichorgo.kubeArgoNetwork(cfg, ctx, server, app.namespace, app.name))
    }

    /**
     * Flux Kustomizations, HelmReleases and sources through their custom resources (os:admin);
     * `installed` is false without Flux.
     */
    suspend fun flux(): FluxStatus = remember(FLUX) {
        kubeCall { cfg, ctx, server -> TalosJson.decodeFromString(FluxStatus.serializer(), Ichorgo.kubeFlux(cfg, ctx, server)) }
    }

    /** Runs [action] on the Flux object [kind] [namespace]/[name] (os:admin). Throws when refused. */
    suspend fun fluxAction(kind: String, namespace: String, name: String, action: FluxAction) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeFluxAction(cfg, ctx, server, kind, namespace, name, action.wire)
    }

    /**
     * What reconciling the Flux object [kind] [namespace]/[name] now would change, object by
     * object (os:admin, read only: server-side apply dry runs). Not cached: always fresh.
     */
    suspend fun fluxDiff(kind: String, namespace: String, name: String): FluxDiff = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(FluxDiff.serializer(), Ichorgo.kubeFluxDiff(cfg, ctx, server, kind, namespace, name))
    }

    /** `kubectl delete pod NAME -n NAMESPACE` (os:admin): its controller starts a new one. */
    suspend fun deletePod(pod: KubePod) = kubeCall { cfg, ctx, server -> Ichorgo.kubeDeletePod(cfg, ctx, server, pod.namespace, pod.name) }

    /** What a maintenance of [node] would do: pods to evict, budgets, reboot checks (os:admin). Never cached. */
    suspend fun maintenancePlan(node: String): MaintenancePlan = talosKubeCall { cfg, ctx, server ->
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
        val server = kubeServers.serverFor(stored)
        return Ichorgo.startNodeMaintenance(stored.yaml, stored.activeContext, server, node, action.wire, includeBare, acknowledged, listener)
    }

    /** `talosctl reboot -m [mode]` (default, powercycle, force); needs os:operator or higher. */
    suspend fun reboot(node: String, mode: String) = call { cfg, ctx -> Ichorgo.reboot(cfg, ctx, node, mode) }

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

    /** Prometheus-compatible query APIs among the cluster's Services, the likeliest first. */
    suspend fun promDiscover(): List<PromSource> = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(PromDiscovery.serializer(), Ichorgo.promDiscover(cfg, ctx, server)).sources
    }

    /** [query] from [start] to [end] (unix seconds) against [source], about 250 points. */
    suspend fun promRange(source: PromSource, query: String, start: Long, end: Long): PromResult = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.promQueryRange(cfg, ctx, server, source.toGoJson(), query, start, end, 0)
        TalosJson.decodeFromString(PromResult.serializer(), json)
    }

    /** The built-in panels. */
    suspend fun promPresets(): List<PromPanel> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ListSerializer(PromPanel.serializer()), Ichorgo.promPresets())
    }

    /** [source] checked and cleaned up by Go, its secret kept. */
    suspend fun normalizePromSource(source: PromSource): PromSource = withContext(Dispatchers.IO) {
        val json = Ichorgo.normalizePromSource(source.toGoJson())
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

    /**
     * A Kubernetes call with the Kubernetes API address the user set for the cluster ("" for the
     * kubeconfig's), through the cluster's Kubernetes access when set (K5, see [kubeTarget]).
     */
    private suspend fun <T> kubeCall(block: (config: String, context: String, kubeServer: String) -> T): T {
        val target = kubeServers.targetFor(configs.forCall())
        return withContext(Dispatchers.IO) { block(target.yaml, target.context, target.server) }
    }

    /** A call that needs both Talos and Kubernetes (maintenance, the admin kubeconfig): always the Talos path. */
    private suspend fun <T> talosKubeCall(block: (config: String, context: String, kubeServer: String) -> T): T {
        val stored = configs.forCall()
        val server = kubeServers.serverFor(stored)
        return withContext(Dispatchers.IO) { block(stored.yaml, stored.activeContext, server) }
    }
}
