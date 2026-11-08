package name.levis.ichor.data

import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.buffer
import kotlinx.coroutines.flow.callbackFlow
import name.levis.ichor.ui.goErrorText
import name.levis.ichorgo.LogListener
import name.levis.ichorgo.LogRun

/** Adapts a Go log follow run to a flow, cancelling the run with its collector. */
internal fun logFollowFlow(start: (LogListener) -> LogRun): Flow<StreamItem<String>> = callbackFlow {
    val run = start(object : LogListener {
        override fun onLine(line: String) {
            trySend(StreamItem.Item(line))
        }

        override fun onDone(errMessage: String) {
            trySend(StreamItem.Done(errMessage.ifEmpty { null }?.let(::goErrorText)))
            close()
        }
    })
    awaitClose { run.cancel() }
}.buffer(Channel.UNLIMITED)
