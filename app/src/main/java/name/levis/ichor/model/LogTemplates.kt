package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlin.math.roundToInt

/** Ichorgo.logTemplates: a log's most repeated messages, the variable parts masked as `<*>`. */
@Serializable
data class LogTemplates(val groups: List<LogTemplateGroup> = emptyList(), val lines: Int = 0, val distinct: Int = 0)

/**
 * One repeated message: [template] with its numbers, ids, addresses and durations as `<*>`, [count]
 * lines of [share] of the log, [sample] the newest one as written (what the log filter is set to),
 * [pods] how many of a workload's pods wrote it (0 for one pod's log).
 */
@Serializable
data class LogTemplateGroup(
    val template: String = "",
    val level: String = "",
    val source: String = "",
    val count: Int = 0,
    val share: Double = 0.0,
    val firstTs: Long = 0,
    val lastTs: Long = 0,
    val sample: String = "",
    val pods: Int = 0,
) {
    /** "61 %", rounded; "<1 %" for a message that is there but rare. */
    fun shareText(): String {
        val percent = (share * 100).roundToInt()
        return if (percent < 1 && count > 0) "<1 %" else "$percent %"
    }

    val logLevel: LogLevel
        get() = when (level.lowercase()) {
            "error" -> LogLevel.ERROR
            "warn", "warning" -> LogLevel.WARN
            "info" -> LogLevel.INFO
            "debug" -> LogLevel.DEBUG
            else -> LogLevel.NONE
        }
}

/** Fewer lines than this and grouping says nothing a glance at the log does not. */
const val LOG_TEMPLATES_MIN_LINES = 20

/** The placeholder Go puts in place of a masked token. */
const val LOG_TEMPLATE_HOLE = "<*>"
