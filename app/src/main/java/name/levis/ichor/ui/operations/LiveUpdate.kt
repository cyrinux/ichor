package name.levis.ichor.ui.operations

/** Up to this many steps, the bar has one segment per step; a longer run is one segment. */
const val LIVE_UPDATE_MAX_SEGMENTS = 15

/**
 * What an Android 16 Live Update adds to a running operation's notification, from its
 * [OperationNotification]: the bar as [segments] (lengths, one per step: the nodes of a cluster
 * upgrade) with [done] of them filled, or a spinner when [indeterminate] (no segments and not
 * indeterminate: no bar at all); the status-bar chip's [chip] text ("2/5", the count the iPhone
 * shows in the Dynamic Island; null lets a countdown tick there instead); and the [actions].
 */
data class LiveUpdate(
    val segments: List<Int>,
    val done: Int,
    val indeterminate: Boolean,
    val chip: String?,
    val actions: List<OperationAction>,
) {
    val hasBar: Boolean get() = indeterminate || segments.isNotEmpty()
}

fun liveUpdateOf(notification: OperationNotification): LiveUpdate {
    val progress = notification.progress
    val total = progress?.total ?: 0
    if (progress == null || total <= 0) {
        return LiveUpdate(emptyList(), 0, indeterminate = progress != null, chip = null, actions = notification.actions)
    }
    val done = progress.done.coerceIn(0, total)
    val segments = if (total <= LIVE_UPDATE_MAX_SEGMENTS) List(total) { 1 } else listOf(total)
    // A config try's countdown is the chip's, as the system shows the notification's timer there.
    val chip = if (notification.countdownTo > 0) null else "$done/$total"
    return LiveUpdate(segments, done, indeterminate = false, chip = chip, actions = notification.actions)
}
