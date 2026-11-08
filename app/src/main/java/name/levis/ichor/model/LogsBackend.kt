package name.levis.ichor.model

import kotlinx.serialization.Serializable

/** Log backends the app can search (PromSource.kind). */
const val LOGS_KIND_LOKI = "loki"
const val LOGS_KIND_VICTORIALOGS = "victorialogs"

/** The range chips, in hours back from now (the last is 7 days). */
val LOG_SEARCH_HOURS = listOf(1, 6, 24, 168)
const val LOG_SEARCH_DEFAULT_HOURS = 24

/** 31 days: the longest range the Go core accepts. */
const val LOG_SEARCH_MAX_HOURS = 744
const val LOG_SEARCH_MAX_SAVED = 20

@Serializable
data class LogsCandidate(val kind: String = "", val source: PromSource = PromSource())

/** What LogsDiscover answers: the log backends among the Services, the likeliest first. */
@Serializable
data class LogsDiscovery(val candidates: List<LogsCandidate> = emptyList())

/** A cluster's log backend (null: the likeliest one found) and its saved queries, newest first. */
@Serializable
data class LogsBackendConfig(val source: PromSource? = null, val saved: List<String> = emptyList())

/** One line of a search; [time] in unix ms. */
@Serializable
data class LogsLine(val time: Long = 0, val labels: Map<String, String> = emptyMap(), val text: String = "")

@Serializable
data class LogsStats(val returned: Int = 0, val truncated: Boolean = false)

/** A search's lines, newest first. */
@Serializable
data class LogsResult(val lines: List<LogsLine> = emptyList(), val stats: LogsStats = LogsStats())

/** What LogsLabels answers: label names, or the values of one. */
@Serializable
data class LogsValues(val values: List<String> = emptyList())

/** [saved] with [query] at the top, once, keeping the newest [LOG_SEARCH_MAX_SAVED]. */
fun savedWith(saved: List<String>, query: String): List<String> {
    val trimmed = query.trim()
    if (trimmed.isEmpty()) return saved
    return (listOf(trimmed) + saved.filterNot { it == trimmed }).take(LOG_SEARCH_MAX_SAVED)
}

/** Adds the matcher [label] = [value] to [query], in the language of the backend [kind]. */
fun withLabelMatcher(kind: String, query: String, label: String, value: String): String {
    val quoted = "\"" + value.replace("\\", "\\\\").replace("\"", "\\\"") + "\""
    val current = query.trim()
    if (kind == LOGS_KIND_VICTORIALOGS) {
        val matcher = "$label:$quoted"
        return if (current.isEmpty()) matcher else "$current AND $matcher"
    }
    val matcher = "$label=$quoted"
    val close = current.indexOf('}')
    return when {
        current.isEmpty() -> "{$matcher}"
        close < 0 -> "{$matcher} $current"
        current.substring(0, close).trimEnd().endsWith("{") -> current.substring(0, close).trimEnd() + matcher + current.substring(close)
        else -> current.substring(0, close).trimEnd() + "," + matcher + current.substring(close)
    }
}

/** The lines as text, oldest first as in a terminal, for Share. */
fun LogsResult.asText(format: (Long) -> String): String =
    lines.asReversed().joinToString("\n") { line -> "${format(line.time)} ${line.text}" }
