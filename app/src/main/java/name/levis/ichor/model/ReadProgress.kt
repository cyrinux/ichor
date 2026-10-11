package name.levis.ichor.model

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.delay
import kotlinx.serialization.Serializable

/**
 * How far a slow read got (Go ReadProgress, the checkup's): its steps in order, each pending,
 * running, done or failed, with the count of a fan-out ([ReadStep.count] of [ReadStep.total]
 * nodes asked) and how long a finished one took.
 */
@Serializable
data class ReadProgress(
    val running: Boolean = false,
    val done: Int = 0,
    val total: Int = 0,
    val steps: List<ReadStep> = emptyList(),
) {
    /** The share of steps over, null while there are none to count. */
    val fraction: Float? get() = if (total > 0) done.toFloat() / total else null
}

@Serializable
data class ReadStep(
    val id: String = "",
    val state: String = PENDING,
    val count: Int = 0,
    val total: Int = 0,
    val ms: Long = 0,
) {
    companion object {
        const val PENDING = "pending"
        const val RUNNING = "running"
        const val DONE = "done"
        const val FAILED = "failed"
    }
}

/** How often a screen asks for the progress of the read it waits on. */
const val READ_PROGRESS_POLL_MS = 250L

/**
 * Reads the progress every [intervalMs] and hands the running ones to [emit], until the
 * caller cancels it (when the read returns). A progress not running yet (the read has not
 * started its count) or failing to read is skipped.
 */
suspend fun pollReadProgress(read: suspend () -> ReadProgress, emit: (ReadProgress) -> Unit, intervalMs: Long = READ_PROGRESS_POLL_MS) {
    while (true) {
        delay(intervalMs)
        val progress = try {
            read()
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            null
        } ?: continue
        if (progress.running) emit(progress)
    }
}
