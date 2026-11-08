package name.levis.ichor.ui

import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.repeatOnLifecycle
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.ensureActive
import name.levis.ichor.data.CallsCancelledException

/**
 * Fetches forever, [intervalMillis] apart: [onSample] gets each answer, [onError] each
 * failure. Cancelling the caller (leaving the screen) cancels the fetch in flight, which is
 * not an error to report.
 */
suspend fun <T> pollEvery(
    intervalMillis: () -> Long,
    fetch: suspend () -> T,
    onError: (Throwable) -> Unit,
    onSample: suspend (T) -> Unit,
): Nothing {
    while (true) {
        cancellableCatching(fetch).fold(onSuccess = { onSample(it) }, onFailure = onError)
        delay(intervalMillis())
    }
}

/**
 * [block], started again when the cluster switch cuts one of its reads: the loop outlives the
 * read (its screen may stay, now on the new cluster), which a plain cancellation would end for good.
 */
internal suspend fun surviveClusterSwitch(block: suspend () -> Unit) {
    while (true) {
        try {
            return block()
        } catch (_: CallsCancelledException) {
            currentCoroutineContext().ensureActive()
        }
    }
}

/** Runs [block] while the screen is visible: leaving the tab or backgrounding the app stops it. */
@Composable
fun PollWhileStarted(block: suspend () -> Unit) {
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(lifecycle) { lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) { surviveClusterSwitch(block) } }
}
