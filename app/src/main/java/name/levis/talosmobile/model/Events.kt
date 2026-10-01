package name.levis.talosmobile.model

import kotlinx.serialization.Serializable

// Mirrors go/talosmobile/events.go.

@Serializable
data class TalosEvent(
    val node: String = "",
    val id: String = "",
    /** Unix millis. */
    val at: Long = 0,
    /** service, sequence, phase, task, machine, config, address, restart or other. */
    val kind: String = "other",
    val subject: String = "",
    val action: String = "",
    val message: String = "",
    /** info, warning or error. */
    val severity: String = "info",
)

enum class EventFilter { ALL, PROBLEMS, SERVICES, BOOT }

/** Most events kept in memory: older ones are dropped. */
const val MAX_EVENTS = 1000

val TalosEvent.isProblem: Boolean get() = severity == "warning" || severity == "error"

fun TalosEvent.matches(filter: EventFilter): Boolean = when (filter) {
    EventFilter.ALL -> true
    EventFilter.PROBLEMS -> isProblem
    EventFilter.SERVICES -> kind == "service"
    EventFilter.BOOT -> kind == "sequence" || kind == "phase" || kind == "task"
}

/**
 * [events] (newest first) with [event] inserted at its place by time. Duplicates (same node
 * and id, e.g. replayed again after a reconnect) are ignored; at most [cap] events are kept.
 */
fun List<TalosEvent>.withEvent(event: TalosEvent, cap: Int = MAX_EVENTS): List<TalosEvent> {
    if (event.id.isNotEmpty() && any { it.id == event.id && it.node == event.node }) return this
    val index = indexOfFirst { it.at < event.at }.let { if (it < 0) size else it }
    return (subList(0, index) + event + subList(index, size)).take(cap)
}

/** One timeline row: [count] consecutive identical events, [event] is the newest of them. */
data class EventRow(val event: TalosEvent, val count: Int, val oldestAt: Long)

private fun TalosEvent.sameAs(other: TalosEvent) =
    node == other.node && kind == other.kind && subject == other.subject && action == other.action && message == other.message

/**
 * Filters [events] (newest first) and collapses runs of identical events into one row
 * (Talos repeats an identical "address" event every 10 minutes).
 */
fun timelineRows(events: List<TalosEvent>, filter: EventFilter): List<EventRow> {
    val rows = mutableListOf<EventRow>()
    for (event in events) {
        if (!event.matches(filter)) continue
        val last = rows.lastOrNull()
        if (last != null && last.event.sameAs(event)) {
            rows[rows.lastIndex] = last.copy(count = last.count + 1, oldestAt = event.at)
        } else {
            rows += EventRow(event, 1, event.at)
        }
    }
    return rows
}
