package name.levis.talosmobile.data

import name.levis.talosmobile.HealthListener
import name.levis.talosmobile.Talosmobile
import name.levis.talosmobile.model.ClusterOverview
import name.levis.talosmobile.model.EtcdOverview
import name.levis.talosmobile.model.LogTail
import name.levis.talosmobile.model.NodeResources
import name.levis.talosmobile.model.ServiceInfo
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer

/** Events streamed by the cluster health check. */
sealed interface HealthEvent {
    data class Progress(val node: String, val message: String) : HealthEvent
    data class Done(val error: String?) : HealthEvent
}

class NoConfigException : IllegalStateException("No talosconfig imported")

/** Read-only access to the Talos API through the Go core. All calls are blocking in Go, so run on IO. */
class TalosRepository(private val configs: ConfigRepository) {

    suspend fun overview(): ClusterOverview = call { cfg, ctx ->
        TalosJson.decodeFromString(ClusterOverview.serializer(), Talosmobile.clusterOverview(cfg, ctx))
    }

    suspend fun services(node: String): List<ServiceInfo> = call { cfg, ctx ->
        TalosJson.decodeFromString(ListSerializer(ServiceInfo.serializer()), Talosmobile.nodeServices(cfg, ctx, node))
    }

    suspend fun resources(node: String): NodeResources = call { cfg, ctx ->
        TalosJson.decodeFromString(NodeResources.serializer(), Talosmobile.nodeResources(cfg, ctx, node))
    }

    suspend fun etcd(): EtcdOverview = call { cfg, ctx ->
        TalosJson.decodeFromString(EtcdOverview.serializer(), Talosmobile.etcdStatus(cfg, ctx))
    }

    /** Last [lines] lines of a Talos service log, or of the kernel log when [service] is null. */
    suspend fun logs(node: String, service: String?, lines: Int = 500): LogTail = call { cfg, ctx ->
        val json = if (service == null) Talosmobile.kernelLogs(cfg, ctx, node, lines.toLong())
        else Talosmobile.serviceLogs(cfg, ctx, node, service, lines.toLong())
        TalosJson.decodeFromString(LogTail.serializer(), json)
    }

    /** Admin kubeconfig (os:admin role). A credential: only hand it to where the user chose. */
    suspend fun kubeconfig(): String = call { cfg, ctx -> Talosmobile.kubeconfig(cfg, ctx) }

    /** `talosctl reboot -m [mode]` (default, powercycle, force); needs os:operator or higher. */
    suspend fun reboot(node: String, mode: String) = call { cfg, ctx -> Talosmobile.reboot(cfg, ctx, node, mode) }

    /** `talosctl shutdown [--force]` (force skips cordon/drain); needs os:operator or higher. */
    suspend fun shutdown(node: String, force: Boolean) = call { cfg, ctx -> Talosmobile.shutdown(cfg, ctx, node, force) }

    /** Streams the server-side health check; cancelling the collector cancels the check. */
    fun health(): Flow<HealthEvent> = callbackFlow {
        val stored = configs.config.value ?: throw NoConfigException()
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

    private suspend fun <T> call(block: (config: String, context: String) -> T): T {
        val stored = configs.config.value ?: throw NoConfigException()
        return withContext(Dispatchers.IO) { block(stored.yaml, stored.activeContext) }
    }
}
