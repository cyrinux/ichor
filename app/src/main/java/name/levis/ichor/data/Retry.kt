package name.levis.ichor.data

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay

/** How long to wait before each new try of a failed read of the stored config, in ms. */
private val READ_RETRY_DELAYS = listOf(100L, 300L, 1000L)

/**
 * Runs [block], again after each of [delays] (ms) while it throws; the failure of the last
 * try is thrown. A cancellation is never retried.
 */
internal suspend fun <T> retrying(
    delays: List<Long> = READ_RETRY_DELAYS,
    pause: suspend (Long) -> Unit = { delay(it) },
    block: suspend () -> T,
): T {
    for (wait in delays) {
        try {
            return block()
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            pause(wait)
        }
    }
    return block()
}
