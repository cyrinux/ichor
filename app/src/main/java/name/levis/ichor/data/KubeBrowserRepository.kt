package name.levis.ichor.data

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
import name.levis.ichor.model.KUBE_PAGE_SIZE
import name.levis.ichor.model.KubeEditPreview
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.KubePage
import name.levis.ichor.model.ResourcePageJson
import name.levis.ichor.model.ResourceRow
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
 * The generic Kubernetes browser (plans/roadmap/kubeconfig-only.md §7): any kind through
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

    /** What saving [edited] as [ref] would store, from a dry run. */
    suspend fun updatePreview(ref: KubeObjectRef, edited: String): KubeEditPreview = kubeCall { cfg, ctx, server ->
        val json = Ichorgo.kubeObjectUpdatePreview(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.namespace, ref.name, edited)
        TalosJson.decodeFromString(KubeEditPreview.serializer(), json)
    }

    /** Saves [edited] as [ref]; refused when the object changed since it was read. */
    suspend fun update(ref: KubeObjectRef, edited: String) = kubeCall { cfg, ctx, server ->
        Ichorgo.kubeObjectUpdate(cfg, ctx, server, ref.group, ref.version, ref.resource, ref.namespace, ref.name, edited)
    }

    /** The latest revision of each Helm release of [namespace] (null for every one). */
    suspend fun helmReleases(namespace: String?): HelmReleaseList = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(HelmReleaseList.serializer(), Ichorgo.kubeHelmReleases(cfg, ctx, server, namespace.orEmpty()))
    }

    suspend fun helmRelease(namespace: String, name: String): HelmReleaseDetail = kubeCall { cfg, ctx, server ->
        TalosJson.decodeFromString(HelmReleaseDetail.serializer(), Ichorgo.kubeHelmRelease(cfg, ctx, server, namespace, name))
    }

    /**
     * Follows [container] ("" for a pod with one) of [pod] like `kubectl logs -f --tail`: the
     * last [tailLines] lines, then each new one, until the collector cancels or the container
     * stops ([StreamItem.Done]).
     */
    fun followPodLogs(namespace: String, pod: String, container: String, tailLines: Int): Flow<StreamItem<String>> = callbackFlow {
        val (stored, server) = target()
        val run = Ichorgo.startPodLogFollow(
            stored.yaml,
            stored.activeContext,
            server,
            namespace,
            pod,
            container,
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
     * Forwards a loopback port of the phone to [remotePort] of [pod] while collected:
     * cancelling the collector closes the port and every connection.
     */
    fun portForward(namespace: String, pod: String, remotePort: Int): Flow<ForwardEvent> = callbackFlow {
        val (stored, server) = target()
        val run = Ichorgo.startPortForward(
            stored.yaml,
            stored.activeContext,
            server,
            namespace,
            pod,
            remotePort.toLong(),
            object : PortForwardListener {
                override fun onReady(address: String) {
                    trySend(ForwardEvent.Ready(address))
                }

                override fun onConnectionError(errMessage: String) {
                    trySend(ForwardEvent.ConnectionError(errMessage))
                }

                override fun onDone(errMessage: String) {
                    trySend(ForwardEvent.Done(errMessage.ifEmpty { null }))
                    close()
                }
            },
        )
        awaitClose { run.stop() }
    }.buffer(Channel.UNLIMITED)

    /** The config to call with and the Kubernetes API address the user set ("" for the kubeconfig's). */
    private fun target(): Pair<StoredConfig, String> {
        val stored = configs.forCall()
        return stored to kubeServers.serverFor(stored)
    }

    private suspend fun <T> kubeCall(block: (config: String, context: String, kubeServer: String) -> T): T {
        val (stored, server) = target()
        return withContext(Dispatchers.IO) { block(stored.yaml, stored.activeContext, server) }
    }
}
