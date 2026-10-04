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
        val (stored, server) = target()
        TalosJson.decodeFromString(CiliumStatus.serializer(), Ichorgo.kubeCilium(stored.yaml, stored.activeContext, server))
    }

    /** Kubernetes NetworkPolicies and, with Cilium, its own policies. */
    suspend fun policies(): NetPolicyReport = withContext(Dispatchers.IO) {
        val (stored, server) = target()
        TalosJson.decodeFromString(NetPolicyReport.serializer(), Ichorgo.kubeNetworkPolicies(stored.yaml, stored.activeContext, server))
    }

    /**
     * Follows the flows [filter] lets through like `hubble observe --follow`: a snapshot at
     * most every second, then [StreamItem.Done] with the error it ended with. Cancelling the
     * collector stops it.
     */
    fun flows(filter: HubbleFilter): Flow<StreamItem<HubbleSnapshot>> = callbackFlow {
        val (stored, server) = target()
        val run = Ichorgo.startHubbleFlows(
            stored.yaml,
            stored.activeContext,
            server,
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

    /** The config to call with and the Kubernetes API address the user set ("" for the kubeconfig's). */
    private fun target(): Pair<StoredConfig, String> {
        val stored = configs.forCall()
        return stored to stored.activeSummary?.fingerprint?.let { kubeServers.servers.value[it] }.orEmpty()
    }
}
