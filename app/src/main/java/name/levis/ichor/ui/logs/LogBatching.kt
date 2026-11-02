package name.levis.ichor.ui.logs

import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.produceIn
import name.levis.ichor.data.StreamItem

/** Batches queued log lines so a burst redraws the screen a few times only. */
internal suspend fun Flow<StreamItem<String>>.collectLogBatches(apply: suspend (List<StreamItem<String>>) -> Unit) = coroutineScope {
    val items = produceIn(this)
    while (true) {
        val next = items.receiveCatching()
        next.exceptionOrNull()?.let { throw it }
        val first = next.getOrNull() ?: break
        val batch = buildList {
            add(first)
            while (true) add(items.tryReceive().getOrNull() ?: break)
        }
        apply(batch)
        delay(100L)
    }
}
