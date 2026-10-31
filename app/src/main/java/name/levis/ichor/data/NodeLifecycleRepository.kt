package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.builtins.MapSerializer
import kotlinx.serialization.builtins.serializer
import name.levis.ichor.model.BootMedia
import name.levis.ichor.model.CARotationPlan
import name.levis.ichor.model.CARotationProgress
import name.levis.ichor.model.CloudUserData
import name.levis.ichor.model.ClusterUpgradeCommand
import name.levis.ichor.model.ClusterUpgradePlan
import name.levis.ichor.model.CpReplacePlan
import name.levis.ichor.model.CpReplaceWait
import name.levis.ichor.model.EndpointMatch
import name.levis.ichor.model.EndpointProbe
import name.levis.ichor.model.HetznerCreateSpec
import name.levis.ichor.model.HetznerOptions
import name.levis.ichor.model.HetznerSnapshotCost
import name.levis.ichor.model.HetznerSnapshotSpec
import name.levis.ichor.model.HetznerTokenCheck
import name.levis.ichor.model.JoinConfigPreview
import name.levis.ichor.model.K8sUpgradePlan
import name.levis.ichor.model.K8sVersionChoice
import name.levis.ichor.model.MaintenanceAction
import name.levis.ichor.model.MaintenanceInspection
import name.levis.ichor.model.MaintenancePlan
import name.levis.ichor.model.MaintenanceUpgrade
import name.levis.ichor.model.NodeDiscovery
import name.levis.ichor.model.NodeResetPlan
import name.levis.ichor.model.ResetRequest
import name.levis.ichor.model.UpgradeExtensionCheck
import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.TalosCARotationListener
import name.levis.ichorgo.ClusterUpgradeListener
import name.levis.ichorgo.Ichorgo
import name.levis.ichorgo.K8sUpgradeListener
import name.levis.ichorgo.MaintenanceListener
import name.levis.ichorgo.MaintenanceRun

/**
 * A node's life in the cluster: maintenance (cordon, drain, then an action), power, reset and
 * rollback, Talos and Kubernetes upgrades, control-plane replacement, and finding and joining
 * new nodes.
 */
class NodeLifecycleRepository(go: GoCall) : GoRepository(go) {

    private val configs = go.configs
    private val kubeServers = go.kubeServers
    private val streams = TalosStreams(configs)

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

    /** The Kubernetes versions offered (K8sUpgradeVersions): one minor up at most, inside the Talos range. */
    suspend fun k8sUpgradeVersions(): K8sVersionChoice = call { cfg, ctx ->
        TalosJson.decodeFromString(K8sVersionChoice.serializer(), Ichorgo.k8sUpgradeVersions(cfg, ctx))
    }

    /** What upgrading Kubernetes to [version] would change (K8sUpgradePlan), read-only. */
    suspend fun k8sUpgradePlan(version: String): K8sUpgradePlan = talosKubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(K8sUpgradePlan.serializer(), Ichorgo.k8sUpgradePlan(cfg, ctx, server, version))
    }

    /** StartK8sUpgrade: returns at once, the run reports to [onProgress] and [onDone] (once). */
    fun startK8sUpgrade(version: String, dryRun: Boolean, onProgress: (String) -> Unit, onDone: (String) -> Unit): K8sUpgradeHandle {
        val stored = configs.forCall()
        val run = Ichorgo.startK8sUpgrade(
            stored.yaml,
            stored.activeContext,
            kubeServers.serverFor(stored),
            version,
            dryRun,
            object : K8sUpgradeListener {
                override fun onProgress(json: String) = onProgress(json)
                override fun onDone(errMessage: String) = onDone(errMessage)
            },
        )
        return K8sUpgradeHandle { run.cancel() }
    }

    /**
     * Every node of the context to [version], in the order the roll follows, with each node's
     * checks (ClusterUpgradePlan). Nodes already on [version] are "done": a rerun continues.
     */
    suspend fun clusterUpgradePlan(version: String): ClusterUpgradePlan = talosKubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(ClusterUpgradePlan.serializer(), Ichorgo.clusterUpgradePlan(cfg, ctx, server, version))
    }

    /** StartClusterUpgrade: returns at once, the roll reports to [onProgress] and [onDone] (once). */
    fun startClusterUpgrade(
        version: String,
        drain: Boolean,
        acknowledged: Boolean,
        onProgress: (String) -> Unit,
        onDone: (String) -> Unit,
    ): ClusterUpgradeHandle {
        val stored = configs.forCall()
        val run = Ichorgo.startClusterUpgrade(
            stored.yaml,
            stored.activeContext,
            kubeServers.serverFor(stored),
            version,
            drain,
            acknowledged,
            object : ClusterUpgradeListener {
                override fun onProgress(json: String) = onProgress(json)
                override fun onDone(errMessage: String) = onDone(errMessage)
            },
        )
        return object : ClusterUpgradeHandle {
            override fun send(command: ClusterUpgradeCommand) = when (command) {
                ClusterUpgradeCommand.PAUSE -> run.pause()
                ClusterUpgradeCommand.RESUME -> run.resume()
                ClusterUpgradeCommand.ABORT -> run.abort()
            }

            override fun cancel() = run.cancel()
        }
    }

    /** [node]'s extensions against the Image Factory's official list for [image]'s version. Read-only. */
    suspend fun upgradeExtensionCheck(node: String, image: String): UpgradeExtensionCheck = call { cfg, ctx ->
        TalosJson.decodeFromString(UpgradeExtensionCheck.serializer(), Ichorgo.upgradeExtensionCheck(cfg, ctx, node, image))
    }

    /** `talosctl reboot -m [mode]` (default, powercycle, force); needs os:operator or higher. */
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

    /**
     * Where replacing the failed etcd member [memberId] stands (steps from live state). [node]:
     * the member's address as last seen, which names the old node once the member is removed. Read-only.
     */
    suspend fun controlPlaneReplacePlan(memberId: String, node: String): CpReplacePlan = call { cfg, ctx ->
        TalosJson.decodeFromString(CpReplacePlan.serializer(), Ichorgo.controlPlaneReplacePlan(cfg, ctx, memberId, node))
    }

    /**
     * Removes the failed member [memberId] as the replacement's step 2 (os:admin): Go reads the
     * plan again and refuses a member that recovered or a removal that loses quorum.
     */
    suspend fun controlPlaneReplaceRemove(memberId: String) = call { cfg, ctx -> Ichorgo.controlPlaneReplaceRemove(cfg, ctx, memberId) }

    /** Polls etcd up to [timeoutSec] until it has more healthy voting members than [membersBefore]. Read-only. */
    suspend fun controlPlaneReplaceWait(membersBefore: Int, timeoutSec: Int): CpReplaceWait = call { cfg, ctx ->
        TalosJson.decodeFromString(CpReplaceWait.serializer(), Ichorgo.controlPlaneReplaceWait(cfg, ctx, membersBefore.toLong(), timeoutSec.toLong()))
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

    /**
     * A node in maintenance mode at [address] (IP or host name on the LAN or VPN): version, disks
     * and links over the unauthenticated maintenance API. Not through [call]: the node belongs to
     * no cluster yet, so no talosconfig is involved.
     */
    suspend fun maintenanceNodeInspect(address: String): MaintenanceInspection = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(MaintenanceInspection.serializer(), Ichorgo.maintenanceNodeInspect(address, 0L))
    }

    /**
     * What boots a new machine of the active cluster: [template]'s (or the first member's, "")
     * version and schematic, the Image Factory links and the booter command, for [arch] ("" for
     * the template's). Never cached: on Omni it carries the join token.
     */
    suspend fun nodeBootMedia(template: String, arch: String, secureBoot: Boolean): BootMedia = call { cfg, ctx ->
        TalosJson.decodeFromString(BootMedia.serializer(), Ichorgo.nodeBootMedia(cfg, ctx, template, arch, secureBoot))
    }

    /**
     * The user data of a new node on a cloud host and the Talos cloud images to create it from:
     * Omni's join config on Omni (the other arguments are not used), else a new member's full
     * config made from [template]'s. Never cached: it carries a credential.
     */
    suspend fun nodeCloudUserData(template: String, role: String, hostname: String, installDisk: String, extraEditsJson: String = ""): CloudUserData =
        call { cfg, ctx ->
            TalosJson.decodeFromString(
                CloudUserData.serializer(),
                Ichorgo.nodeCloudUserData(cfg, ctx, template, role, hostname, installDisk, extraEditsJson),
            )
        }

    /** A new node's config from [template]'s, redacted (never cached; the full one only goes to the node). */
    suspend fun joinNodeConfigPreview(template: String, role: String, hostname: String, installDisk: String, staticAddress: String): JoinConfigPreview =
        call { cfg, ctx ->
            TalosJson.decodeFromString(
                JoinConfigPreview.serializer(),
                Ichorgo.joinNodeConfigPreview(cfg, ctx, template, role, hostname, installDisk, staticAddress, ""),
            )
        }

    /** What to confirm before joining a node of [role]: one warning per line, from the Go core. */
    suspend fun nodeJoinWarnings(role: String): List<String> = withContext(Dispatchers.IO) {
        Ichorgo.nodeJoinWarnings(role).lines().filter { it.isNotBlank() }
    }

    /** See [TalosStreams.joinNode]. */
    fun joinNode(
        address: String,
        template: String,
        role: String,
        hostname: String,
        installDisk: String,
        staticAddress: String,
        replacing: Boolean,
        stop: Flow<Any>,
    ): Flow<JoinEvent> = streams.joinNode(address, template, role, hostname, installDisk, staticAddress, replacing, stop)

    /** What a Hetzner token may do. Creates nothing. */
    suspend fun hetznerTest(token: String): HetznerTokenCheck = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(HetznerTokenCheck.serializer(), Ichorgo.hetznerTest(hetznerTokenJson(token)))
    }

    /** Server types, locations and snapshots ([token] may be blank on the demo). Never cached. */
    suspend fun hetznerOptions(token: String): HetznerOptions = call { cfg, ctx ->
        TalosJson.decodeFromString(HetznerOptions.serializer(), Ichorgo.hetznerOptions(cfg, ctx, hetznerTokenJson(token)))
    }

    /** See [TalosStreams.hetznerCreate]. */
    fun hetznerCreate(token: String, spec: HetznerCreateSpec, stop: Flow<Any>): Flow<JoinEvent> =
        streams.hetznerCreate(hetznerTokenJson(token), TalosJson.encodeToString(HetznerCreateSpec.serializer(), spec), stop)

    /** The cost line of a snapshot run. Creates nothing. */
    suspend fun hetznerSnapshotEstimate(token: String, arch: String, location: String): HetznerSnapshotCost = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(HetznerSnapshotCost.serializer(), Ichorgo.hetznerSnapshotEstimate(hetznerTokenJson(token), arch, location))
    }

    /** See [TalosStreams.hetznerSnapshot]. */
    fun hetznerSnapshot(token: String, spec: HetznerSnapshotSpec, stop: Flow<Any>): Flow<JoinEvent> =
        streams.hetznerSnapshot(hetznerTokenJson(token), TalosJson.encodeToString(HetznerSnapshotSpec.serializer(), spec), stop)

    private fun hetznerTokenJson(token: String): String =
        TalosJson.encodeToString(MapSerializer(String.serializer(), String.serializer()), mapOf("token" to token))

    /** What rotating the active context's Talos API CA would do (os:admin). Never cached. */
    suspend fun caRotationPlan(): CARotationPlan = call { cfg, ctx ->
        TalosJson.decodeFromString(CARotationPlan.serializer(), Ichorgo.talosCARotationPlan(cfg, ctx))
    }

    /**
     * Starts rotating the active context's Talos API CA, for [CARotationManager]. The
     * talosconfigs it hands over are credentials: they only go through the channel, never logged.
     */
    fun startCARotation(): CARotationHandle {
        val stored = configs.forCall()
        val events = Channel<CARotationEvent>(Channel.UNLIMITED) // never drop the final event
        val run = Ichorgo.startTalosCARotation(
            stored.yaml,
            stored.activeContext,
            object : TalosCARotationListener {
                override fun onProgress(json: String) {
                    runCatching { TalosJson.decodeFromString(CARotationProgress.serializer(), json) }
                        .onSuccess { events.trySend(CARotationEvent.Progress(it)) }
                }

                override fun onTalosconfig(talosconfigYAML: String, isFinal: Boolean) {
                    events.trySend(CARotationEvent.Talosconfig(talosconfigYAML, isFinal))
                }

                override fun onDone(errMessage: String) {
                    events.trySend(CARotationEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    events.close()
                }
            },
        )
        return CARotationHandle(events, run::proceed, run::cancel)
    }

    private suspend fun <T> call(block: (config: String, context: String) -> T): T = go.talos(block)

    private suspend fun <T> kubeCall(block: (config: String, context: String, kubeServer: String) -> T): T = go.kube(block)

    private suspend fun <T> talosKubeCall(block: (config: String, context: String, kubeServer: String) -> T): T = go.talosKube(block)
}
