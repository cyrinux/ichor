package name.levis.ichor.data

import name.levis.ichor.ui.goErrorText
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.ReceiveChannel
import kotlinx.coroutines.withContext
import name.levis.ichor.model.NetPerfNode
import name.levis.ichor.model.NetPerfNodeList
import name.levis.ichor.model.NetPerfProgress
import name.levis.ichor.model.NetPerfReport
import name.levis.ichor.model.NetPerfSetup
import name.levis.ichorgo.NetPerfListener
import name.levis.ichorgo.Ichorgo

/** Events of a running network test. [Done.error] is null when it completed. */
sealed interface NetPerfEvent {
    data class Progress(val progress: NetPerfProgress) : NetPerfEvent
    data class Done(val report: NetPerfReport, val error: String?) : NetPerfEvent
}

/** A running network test: its [events], and [stop] to end it early. */
class NetPerfHandle(val events: ReceiveChannel<NetPerfEvent>, val stop: () -> Unit)

/**
 * Network tests between two nodes, through the Kubernetes API (os:admin): netperf pods in a
 * temporary namespace, like `cilium connectivity perf`.
 */
class NetPerfRepository(private val configs: ConfigRepository, private val kubeServers: KubeServers) {
    /** The Kubernetes nodes a test can run between, by name. */
    suspend fun nodes(): List<NetPerfNode> = withContext(Dispatchers.IO) {
        val target = target()
        TalosJson.decodeFromString(NetPerfNodeList.serializer(), Ichorgo.netPerfNodes(target.yaml, target.context, target.server)).nodes
    }

    /**
     * Starts a test. Events arrive on [NetPerfHandle.events], which closes after
     * [NetPerfEvent.Done]; [NetPerfHandle.stop] ends it early, and Done follows once the
     * test namespace is deleted. It returns at once (the core runs the test in the
     * background), so the caller holds the handle before anything can cancel it.
     */
    fun start(setup: NetPerfSetup): NetPerfHandle {
        val target = target()
        val events = Channel<NetPerfEvent>(Channel.UNLIMITED) // never drop the final Done
        val run = Ichorgo.startNetPerf(
            target.yaml,
            target.context,
            target.server,
            setup.server,
            setup.client,
            setup.hostNetwork,
            setup.seconds.toLong(),
            object : NetPerfListener {
                override fun onProgress(json: String) {
                    runCatching { TalosJson.decodeFromString(NetPerfProgress.serializer(), json) }
                        .onSuccess { events.trySend(NetPerfEvent.Progress(it)) }
                }

                override fun onDone(reportJSON: String, errMessage: String) {
                    val report = runCatching { TalosJson.decodeFromString(NetPerfReport.serializer(), reportJSON) }.getOrElse { NetPerfReport() }
                    events.trySend(NetPerfEvent.Done(report, errMessage.ifEmpty { null }?.let(::goErrorText)))
                    events.close()
                }
            },
        )
        return NetPerfHandle(events, run::cancel)
    }

    /** The config to call with and the API address, through the cluster's Kubernetes access when set (K5). */
    private fun target(): KubeTarget = kubeServers.targetFor(configs.forCall())
}
