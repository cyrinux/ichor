package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.serialization.KSerializer
import name.levis.ichor.model.KubeWatchEvent
import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.KubeLiveListener
import name.levis.ichorgo.KubeLiveRun
import name.levis.ichorgo.KubeWatchListener
import name.levis.ichorgo.KubeWatchRun

// What the Go core keeps live through the Kubernetes API's watches (kube_watch.go), as flows:
// collected while a screen is visible, cancelled when it is not; [StreamItem.Done] once the
// watch ends, with why. Pull-to-refresh stays: a watch adds to the one-shot reads, it does not
// replace them.

/**
 * A list kept live (a KubeWatchListener's events): each change decoded as a [KubeWatchEvent],
 * items with [item], a SYNC list with [list]. [target] is read when collected.
 */
fun <T> kubeWatchFlow(
    target: () -> KubeTarget,
    item: KSerializer<T>,
    list: (String) -> List<T>,
    start: (yaml: String, context: String, server: String, listener: KubeWatchListener) -> KubeWatchRun,
): Flow<StreamItem<KubeWatchEvent<T>>> = kubeWatchFlow(target, { TalosJson.decodeFromString(item, it) }, list, start)

/** [kubeWatchFlow] with items decoded by [item] (rows that need what the SYNC carried). */
fun <T> kubeWatchFlow(
    target: () -> KubeTarget,
    item: (String) -> T,
    list: (String) -> List<T>,
    start: (yaml: String, context: String, server: String, listener: KubeWatchListener) -> KubeWatchRun,
): Flow<StreamItem<KubeWatchEvent<T>>> = callbackFlow {
    val t = target()
    val run = start(
        t.yaml,
        t.context,
        t.server,
        object : KubeWatchListener {
            override fun onEvent(eventType: String, json: String) {
                KubeWatchEvent.decode(eventType, json, item, list)?.let { trySend(StreamItem.Item(it)) }
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
 * A view kept live (a KubeLiveListener's updates: a rollout, an object summary): each update
 * decoded with [serializer]; only the latest one waits for a slow collector.
 */
fun <T> kubeLiveFlow(
    target: () -> KubeTarget,
    serializer: KSerializer<T>,
    start: (yaml: String, context: String, server: String, listener: KubeLiveListener) -> KubeLiveRun,
): Flow<StreamItem<T>> = callbackFlow {
    val t = target()
    val run = start(
        t.yaml,
        t.context,
        t.server,
        object : KubeLiveListener {
            override fun onUpdate(json: String) {
                runCatching { TalosJson.decodeFromString(serializer, json) }.onSuccess { trySend(StreamItem.Item(it)) }
            }

            override fun onDone(errMessage: String) {
                trySend(StreamItem.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
                close()
            }
        },
    )
    awaitClose { run.cancel() }
}.buffer(Channel.CONFLATED)

/** How long a screen waits before following again a watch that ended (a refusal, the network). */
const val KUBE_WATCH_RETRY_MILLIS = 30_000L

/**
 * Collects the stream [start] makes, for as long as called: again [retryMillis] after it ends
 * or fails (a cluster that cannot be called now: no config, its VPN down), the failure handed
 * to [onItem] as a [StreamItem.Done]. Only cancellation gets out.
 */
suspend fun <T> watchForever(retryMillis: Long = KUBE_WATCH_RETRY_MILLIS, start: () -> Flow<StreamItem<T>>, onItem: suspend (StreamItem<T>) -> Unit): Nothing {
    while (true) {
        try {
            start().collect(onItem)
        } catch (e: CancellationException) {
            throw e
        } catch (e: Throwable) {
            onItem(StreamItem.Done(e.message ?: e.javaClass.simpleName))
        }
        delay(retryMillis)
    }
}
