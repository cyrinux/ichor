package name.levis.ichor.data

import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import name.levis.ichor.model.KubeEventsBatch
import name.levis.ichor.model.KubeEventsStatus
import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.KubeEventsListener
import name.levis.ichorgo.KubeEventsRun

/** What the cluster events stream (StartKubeEvents) hands over; [Done] ends it, [Done.error] null when cancelled. */
sealed interface KubeEventsItem {
    data class Batch(val batch: KubeEventsBatch) : KubeEventsItem
    data class Status(val status: KubeEventsStatus) : KubeEventsItem
    data class Done(val error: String?) : KubeEventsItem
}

/**
 * The cluster events stream [start] runs, as a flow: batches and status changes decoded, every
 * one kept (a batch dropped would leave rows out of step); cancelling the collector cancels
 * the run. [target] is read when collected.
 */
fun kubeEventsFlow(
    target: () -> KubeTarget,
    start: (yaml: String, context: String, server: String, listener: KubeEventsListener) -> KubeEventsRun,
): Flow<KubeEventsItem> = callbackFlow {
    val t = target()
    val run = start(
        t.yaml,
        t.context,
        t.server,
        object : KubeEventsListener {
            override fun onEvents(batchJSON: String) {
                runCatching { TalosJson.decodeFromString(KubeEventsBatch.serializer(), batchJSON) }
                    .onSuccess { trySend(KubeEventsItem.Batch(it)) }
            }

            override fun onStatus(stateJSON: String) {
                runCatching { TalosJson.decodeFromString(KubeEventsStatus.serializer(), stateJSON) }
                    .onSuccess { trySend(KubeEventsItem.Status(it)) }
            }

            override fun onDone(errMessage: String) {
                trySend(KubeEventsItem.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                close()
            }
        },
    )
    awaitClose { run.cancel() }
}.buffer(Channel.UNLIMITED)
