package name.levis.ichor.model

/** How one progress line of the cluster health check reads, for its color. */
enum class HealthLineStatus { OK, PENDING, WARN, BAD, INFO }

private const val WAITING_PREFIX = "waiting for "
private const val OK_SUFFIX = ": OK"
private const val PENDING_SUFFIX = "..."

/**
 * Classifies a Talos health line ("waiting for <condition>: <state>"): the state is "OK" once
 * the condition holds, "..." before its first evaluation, and otherwise why it does not hold yet.
 * [failed] marks the line the check gave up on, the last one of a run that ended in an error.
 */
fun healthLineStatus(line: String, failed: Boolean = false): HealthLineStatus {
    val text = line.trim()
    return when {
        text.endsWith(OK_SUFFIX) -> HealthLineStatus.OK
        !text.startsWith(WAITING_PREFIX) -> HealthLineStatus.INFO
        failed -> HealthLineStatus.BAD
        text.endsWith(PENDING_SUFFIX) || ": " !in text -> HealthLineStatus.PENDING
        else -> HealthLineStatus.WARN
    }
}
