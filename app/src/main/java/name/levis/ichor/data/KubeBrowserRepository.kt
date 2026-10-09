package name.levis.ichor.data

import name.levis.ichor.model.KubeServices
import name.levis.ichor.model.KubeStorage
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import name.levis.ichor.model.ApiResourceList
import name.levis.ichor.model.HelmReleaseDetail
import name.levis.ichor.model.HelmReleaseList
import name.levis.ichor.model.HelmRollbackPlan
import name.levis.ichor.model.DeletePropagation
import name.levis.ichor.model.KUBE_PAGE_SIZE
import name.levis.ichor.model.KubeConfigData
import name.levis.ichor.model.KubeDeletePreview
import name.levis.ichor.model.configDataKind
import name.levis.ichor.model.KubeEditPreview
import name.levis.ichor.model.KubeExplain
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubeObjectScale
import name.levis.ichor.model.KubeObjectSummary
import name.levis.ichor.model.KubePage
import name.levis.ichor.model.ResourcePageJson
import name.levis.ichor.model.ResourceRow
import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.Ichorgo
import name.levis.ichorgo.LogListener
import name.levis.ichorgo.PortForwardListener

/** What a port-forward reports, in order: [Ready] once listening, [ConnectionError] per failed connection, then [Done]. */
sealed interface ForwardEvent {
    data class Ready(val address: String) : ForwardEvent
    data class ConnectionError(val message: String) : ForwardEvent
    /** [error] null when stopped. */
    data class Done(val error: String?) : ForwardEvent
}

/**
 * The generic Kubernetes browser: any kind through
 * discovery and the server's Table, one object as YAML and its edit, Helm releases, a
 * followed pod log and a port-forward. Every call goes through the Kubernetes API of the
 * active cluster, Talos (its admin kubeconfig) or kubeconfig alike, with the API address the
 * user set; never the Talos API.
 */
class KubeBrowserRepository(private val configs: ConfigRepository, private val kubeServers: KubeServers) {
    /** The listable resources of the cluster, CRDs included (`kubectl api-resources`). */
    suspend fun apiResources(): ApiResourceList = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(ApiResourceList.serializer(), Ichorgo.kubeAPIResources(cfg, ctx, server))
    }

    /** One page of [resource] in [namespace] (null for every one, or a cluster-scoped kind); "" [token] for the first. */
    suspend fun resourcePage(group: String, version: String, resource: String, namespace: String?, token: String, limit: Int = KUBE_PAGE_SIZE): KubePage<ResourceRow> =
        kubeCall { cfg, ctx, server ->
            val json = Ichorgo.kubeResourcePage(cfg, ctx, server, group, version, resource, namespace.orEmpty(), token, limit.toLong())
            TalosJson.decodeFromString(ResourcePageJson.serializer(), json).toPage()
        }

    /** [ref] as YAML; a Secret's values only when [reveal]. Never cached: it may hold secrets. */
    suspend fun objectYaml(ref: KubeObjectRef, reveal: Boolean): String = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeObjectYAML(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.namespace, ref.name, reveal)
    }

    /**
     * A Secret or ConfigMap ([ref]) key by key, with the pods using it. A Secret's value comes
     * only for [revealKey] ("" for none; refused in screenshot mode). Never cached.
     */
    suspend fun configData(ref: KubeObjectRef, revealKey: String = ""): KubeConfigData = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.kubeConfigData(cfg, ctx, server, ref.configDataKind, ref.namespace, ref.name, revealKey)
        TalosJson.decodeFromString(KubeConfigData.serializer(), json)
    }

    /** [ref] summed up: conditions, owners and managers, metadata, spec highlights and events. */
    suspend fun objectSummary(ref: KubeObjectRef): KubeObjectSummary = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.kubeObjectSummary(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.namespace, ref.name)
        TalosJson.decodeFromString(KubeObjectSummary.serializer(), json)
    }

    /**
     * [objectSummary] kept live: the summary at the start, then again each time the object or
     * its events change, until the collector cancels or the watch ends ([StreamItem.Done]).
     */
    fun objectSummaryWatch(ref: KubeObjectRef): Flow<StreamItem<KubeObjectSummary>> =
        kubeLiveFlow(::target, KubeObjectSummary.serializer()) { cfg, ctx, server, listener ->
            Ichorgo.startKubeObjectWatch(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.namespace, ref.name, listener)
        }

    /** What saving [edited] as [ref] would store, from a dry run. */
    suspend fun updatePreview(ref: KubeObjectRef, edited: String): KubeEditPreview = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.kubeObjectUpdatePreview(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.namespace, ref.name, edited)
        TalosJson.decodeFromString(KubeEditPreview.serializer(), json)
    }

    /** Saves [edited] as [ref]; refused when the object changed since it was read. */
    suspend fun update(ref: KubeObjectRef, edited: String) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeObjectUpdate(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.namespace, ref.name, edited)
    }

    /** The schema help for [fieldPath] ("spec.template", "" for the kind) of [ref]'s kind, from OpenAPI v3. */
    suspend fun explain(ref: KubeObjectRef, fieldPath: String): KubeExplain = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.kubeExplain(cfg, ctx, server, ref.group, ref.version, ref.kind, fieldPath)
        TalosJson.decodeFromString(KubeExplain.serializer(), json)
    }

    /** How many pods [ref] wants and runs (a Job: its parallelism). */
    suspend fun objectScale(ref: KubeObjectRef): KubeObjectScale = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.kubeObjectScale(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.namespace, ref.name)
        TalosJson.decodeFromString(KubeObjectScale.serializer(), json)
    }

    /** Sets [ref]'s replicas (a Job's parallelism); returns the autoscaler warning, "" when none. */
    suspend fun scale(ref: KubeObjectRef, replicas: Int): String = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeScaleObject(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.kind, ref.namespace, ref.name, replicas.toLong())
    }

    /** What deleting [ref] would do: protection, finalizers, the objects it owns. Read-only. */
    suspend fun deletePreview(ref: KubeObjectRef): KubeDeletePreview = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.kubeObjectDeletePreview(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.namespace, ref.name)
        TalosJson.decodeFromString(KubeDeletePreview.serializer(), json)
    }

    /**
     * Deletes [ref] with [propagation]; refused when it changed since [resourceVersion] (the
     * preview's) was read, and for a protected object unless [force].
     */
    suspend fun delete(ref: KubeObjectRef, propagation: DeletePropagation, resourceVersion: String, force: Boolean) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeObjectDelete(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.namespace, ref.name, propagation.api, resourceVersion, -1L, force)
    }

    /** The PersistentVolumeClaims of [namespace] (null for every one), with volume, pods and fill. */
    suspend fun storage(namespace: String?): KubeStorage = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeStorage.serializer(), Ichorgo.kubeStorage(cfg, ctx, server, namespace.orEmpty()))
    }

    /** The Services of [namespace] (null for every one), with addresses, ready endpoints and routes. */
    suspend fun services(namespace: String?): KubeServices = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(KubeServices.serializer(), Ichorgo.kubeServices(cfg, ctx, server, namespace.orEmpty()))
    }

    /** The latest revision of each Helm release of [namespace] (null for every one). */
    suspend fun helmReleases(namespace: String?): HelmReleaseList = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(HelmReleaseList.serializer(), Ichorgo.kubeHelmReleases(cfg, ctx, server, namespace.orEmpty()))
    }

    suspend fun helmRelease(namespace: String, name: String): HelmReleaseDetail = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(HelmReleaseDetail.serializer(), Ichorgo.kubeHelmRelease(cfg, ctx, server, namespace, name))
    }

    /** What rolling the release back to [revision] (0 for the previous one) would change, from dry runs. */
    suspend fun helmRollbackPlan(namespace: String, name: String, revision: Int): HelmRollbackPlan = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(HelmRollbackPlan.serializer(), Ichorgo.kubeHelmRollbackPlan(cfg, ctx, server, namespace, name, revision.toLong()))
    }

    /** Rolls the release back to [revision] like `helm rollback`, suspending its Flux HelmRelease first. */
    suspend fun helmRollback(namespace: String, name: String, revision: Int) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeHelmRollback(cfg, ctx, server, namespace, name, revision.toLong())
    }

    /**
     * Follows [container] ("" for a pod with one) of [pod] like `kubectl logs -f --tail`: the
     * last [tailLines] lines, then each new one, until the collector cancels or the container
     * stops ([StreamItem.Done]).
     */
    fun followPodLogs(namespace: String, pod: String, container: String, tailLines: Int): Flow<StreamItem<String>> = callbackFlow {
        val target = target()
        val run = Ichorgo.startPodLogFollow(
            target.yaml,
            target.context,
            target.server,
            namespace,
            pod,
            container,
            tailLines.toLong(),
            object : LogListener {
                override fun onLine(line: String) {
                    trySend(StreamItem.Item(line))
                }

                override fun onDone(errMessage: String) {
                    trySend(StreamItem.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.UNLIMITED)

    /**
     * Forwards a loopback port of the phone to [remotePort] of [pod] while collected:
     * cancelling the collector closes the port and every connection.
     */
    fun portForward(namespace: String, pod: String, remotePort: Int): Flow<ForwardEvent> = callbackFlow {
        val target = target()
        val run = Ichorgo.startPortForward(
            target.yaml,
            target.context,
            target.server,
            namespace,
            pod,
            remotePort.toLong(),
            object : PortForwardListener {
                override fun onReady(address: String) {
                    trySend(ForwardEvent.Ready(address))
                }

                override fun onConnectionError(errMessage: String) {
                    trySend(ForwardEvent.ConnectionError(goErrorText(errMessage)))
                }

                override fun onDone(errMessage: String) {
                    trySend(ForwardEvent.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                    close()
                }
            },
        )
        awaitClose { run.stop() }
    }.buffer(Channel.UNLIMITED)

    /**
     * Where the calls go: the active cluster's config with the API address the user set, or
     * for a Talos cluster whose Kubernetes access is a kubeconfig cluster, that one (K5).
     */
    private fun target(): KubeTarget = kubeServers.targetFor(configs.forCall())

    private suspend fun <T> kubeCall(block: (config: String, context: String, kubeServer: String) -> T): T {
        val target = target()
        return withContext(Dispatchers.IO) { block(target.yaml, target.context, target.server) }
    }
}
