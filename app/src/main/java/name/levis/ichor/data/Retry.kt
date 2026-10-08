package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay

/** How long to wait before each new try of a failed read of the stored config, in ms. */
private val READ_RETRY_DELAYS = listOf(100L, 300L, 1000L)

/**
 * Runs [block], again after each of [delays] (ms) while it throws; the failure of the last
 * try is thrown. A cancellation is never retried, nor a failure [retryOn] refuses.
 */
internal suspend fun <T> retrying(
    delays: List<Long> = READ_RETRY_DELAYS,
    pause: suspend (Long) -> Unit = { delay(it) },
    retryOn: (Exception) -> Boolean = { true },
    block: suspend () -> T,
): T {
    for (wait in delays) {
        try {
            return block()
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            if (!retryOn(e)) throw e
            pause(wait)
        }
    }
    return block()
}
