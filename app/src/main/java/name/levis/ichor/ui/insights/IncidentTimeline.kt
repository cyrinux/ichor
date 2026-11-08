package name.levis.ichor.ui.insights

import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import name.levis.ichor.model.IncidentEntry
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.incidentDetail
import name.levis.ichor.model.incidentMetrics
import java.util.Locale
import kotlin.math.abs

/** One recorder entry in the timeline: its health dot and its time relative to the recording start. */
data class TimelineEntry(val entry: IncidentEntry, val health: ServiceHealth, val offset: String)

/** A timeline line: a single entry, or a run of healthy service entries of one node shown as one row. */
sealed interface TimelineItem {
    val key: String

    data class Single(val row: TimelineEntry) : TimelineItem {
        override val key: String get() = row.entry.id
    }

    data class HealthyRun(val rows: List<TimelineEntry>) : TimelineItem {
        override val key: String get() = "run/${rows.first().entry.id}"
        val node: String get() = rows.first().entry.node
    }
}

/** Kinds whose consecutive healthy entries collapse into one "N services healthy" row. */
private const val COLLAPSIBLE_KIND = "service"

/**
 * The recorder [entries] (already in display order) as timeline items: each entry gets its
 * health and its offset from [startedAt]; consecutive ok service entries of the same node fold
 * into one [TimelineItem.HealthyRun] (a run of one stays a single row).
 */
fun incidentTimeline(entries: List<IncidentEntry>, startedAt: Long): List<TimelineItem> {
    val rows = entries.map { TimelineEntry(it, evidenceHealth(it), relativeTime(it.at - startedAt)) }
    val items = mutableListOf<TimelineItem>()
    var run = emptyList<TimelineEntry>()
    fun flush() {
        when (run.size) {
            0 -> Unit
            1 -> items += TimelineItem.Single(run.first())
            else -> items += TimelineItem.HealthyRun(run)
        }
        run = emptyList()
    }
    for (row in rows) {
        val collapsible = row.health == ServiceHealth.OK && row.entry.kind == COLLAPSIBLE_KIND
        if (!collapsible) {
            flush()
            items += TimelineItem.Single(row)
        } else if (run.isNotEmpty() && run.first().entry.node != row.entry.node) {
            flush()
            run = listOf(row)
        } else {
            run = run + row
        }
    }
    flush()
    return items
}

/**
 * An entry's dot: critical when the node is unreachable or the evidence failed, warning when a
 * condition changed for the worse (not ready, unhealthy, a link change, a warning), ok otherwise.
 */
fun evidenceHealth(entry: IncidentEntry): ServiceHealth {
    val detail = entry.incidentDetail()
    fun text(key: String) = (detail?.get(key) as? JsonPrimitive)?.contentOrNull.orEmpty()
    fun flag(key: String) = (detail?.get(key) as? JsonPrimitive)?.booleanOrNull
    val metrics = if (entry.kind == "metrics") entry.incidentMetrics() else null
    val bad = entry.severity == "error" || entry.kind == "error" ||
        (entry.kind == "status" && flag("reachable") == false)
    val warn = entry.severity == "warning" || entry.kind == "link" ||
        (entry.kind == "status" && (flag("ready") == false || (detail?.get("conditions") as? JsonArray)?.isNotEmpty() == true)) ||
        (entry.kind == "service" && (text("health") == "unhealthy" || text("state") == "absent")) ||
        (metrics != null && (metrics.errors.isNotEmpty() || metrics.network.any { it.errors > 0 || it.drops > 0 }))
    return when {
        bad -> ServiceHealth.CRITICAL
        warn -> ServiceHealth.WARNING
        else -> ServiceHealth.OK
    }
}

/** [millis] since the recording start as "+mm:ss" ("+h:mm:ss" past an hour, "-" for node clocks behind it). */
fun relativeTime(millis: Long): String {
    val sign = if (millis < 0) "-" else "+"
    val seconds = abs(millis) / 1000
    val h = seconds / 3600
    val m = (seconds % 3600) / 60
    val s = seconds % 60
    return if (h > 0) String.format(Locale.ROOT, "%s%d:%02d:%02d", sign, h, m, s) else String.format(Locale.ROOT, "%s%02d:%02d", sign, m, s)
}
