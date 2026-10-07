package name.levis.ichor.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import name.levis.ichor.model.CiliumStatus
import name.levis.ichor.model.HubbleFilter
import name.levis.ichor.model.HubbleSnapshot
import name.levis.ichor.model.NetPolicyReport
import name.levis.ichorgo.HubbleListener
import name.levis.ichorgo.Ichorgo

/**
 * Network policies (any CNI) and, with Cilium, its live flows through Hubble, both through the
 * Kubernetes API (os:admin).
 */
class CiliumRepository(private val configs: ConfigRepository, private val kubeServers: KubeServers) {
    /** Whether Cilium runs, and with Hubble. */
    suspend fun status(): CiliumStatus = withContext(Dispatchers.IO) {
        val target = target()
        TalosJson.decodeFromString(CiliumStatus.serializer(), Ichorgo.kubeCilium(target.yaml, target.context, target.server))
    }

    /** Kubernetes NetworkPolicies and, with Cilium, its own policies. */
    suspend fun policies(): NetPolicyReport = withContext(Dispatchers.IO) {
        val target = target()
        TalosJson.decodeFromString(NetPolicyReport.serializer(), Ichorgo.kubeNetworkPolicies(target.yaml, target.context, target.server))
    }

    /**
     * Follows the flows [filter] lets through like `hubble observe --follow`: a snapshot at
     * most every second, then [StreamItem.Done] with the error it ended with. Cancelling the
     * collector stops it.
     */
    fun flows(filter: HubbleFilter): Flow<StreamItem<HubbleSnapshot>> = callbackFlow {
        val target = target()
        val run = Ichorgo.startHubbleFlows(
            target.yaml,
            target.context,
            target.server,
            filter.namespace.orEmpty(),
            filter.pod.orEmpty(),
            filter.dropsOnly,
            object : HubbleListener {
                override fun onUpdate(json: String) {
                    runCatching { TalosJson.decodeFromString(HubbleSnapshot.serializer(), json) }
                        .onSuccess { trySend(StreamItem.Item(it)) }
                }

                override fun onDone(errMessage: String) {
                    trySend(StreamItem.Done(errMessage.ifEmpty { null }))
                    close()
                }
            },
        )
        awaitClose { run.cancel() }
    }.buffer(Channel.CONFLATED)

    /** Where the calls go: the cluster's config or its Kubernetes access (see [kubeTarget]). */
    private fun target(): KubeTarget = kubeServers.targetFor(configs.forCall())
}
