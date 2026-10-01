package name.levis.talosmobile.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId

// Mirrors the structured log entries of go/talosmobile (NodeLogs `entries`, ParseLogLine).

@Serializable
data class LogField(val k: String = "", val v: String = "")

@Serializable
data class LogEntry(
    /** Unix millis; 0 when unknown. */
    val ts: Long = 0,
    /** error, warn, info, debug or "" when unknown. */
    val level: String = "",
    val source: String = "",
    val msg: String = "",
    val fields: List<LogField> = emptyList(),
    val raw: String = "",
) {
    /** The original line; older cores only send it. */
    val text: String get() = raw.ifEmpty { msg }

    /** Same content, ignoring the time: such consecutive entries collapse into one row. */
    fun sameAs(other: LogEntry) = level == other.level && source == other.source && msg == other.msg && fields == other.fields

    companion object {
        /** An unparsed line, shown as is. */
        fun plain(line: String) = LogEntry(msg = line, raw = line)
    }
}

/** An entry with its arrival number, which keys its row so rows stay stable while following. */
data class SeqLogEntry(val seq: Long, val entry: LogEntry)

enum class LogLevel { ERROR, WARN, INFO, DEBUG, NONE }

val LogEntry.logLevel: LogLevel
    get() = when (level.lowercase()) {
        "error" -> LogLevel.ERROR
        "warn", "warning" -> LogLevel.WARN
        "info" -> LogLevel.INFO
        "debug" -> LogLevel.DEBUG
        else -> LogLevel.NONE
    }

enum class LogLevelFilter { ALL, WARNINGS, ERRORS }

fun LogEntry.matches(filter: LogLevelFilter): Boolean = when (filter) {
    LogLevelFilter.ALL -> true
    LogLevelFilter.WARNINGS -> logLevel == LogLevel.ERROR || logLevel == LogLevel.WARN
    LogLevelFilter.ERRORS -> logLevel == LogLevel.ERROR
}

/** Field keys whose value is an error and is shown as one. */
fun isErrorKey(key: String) = key.lowercase() in setOf("error", "err", "reason")

/** The structured entries of [tail], or its plain lines when the core sent none (or a mismatch). */
fun LogTail.toEntries(): List<LogEntry> =
    if (entries.size == lines.size && entries.isNotEmpty()) entries else lines.map(LogEntry::plain)

@Serializable
private data class PlainLogTail(val lines: List<String> = emptyList(), val truncated: Boolean = false)

/** [json] as a LogTail; malformed `entries` are dropped so the lines still show. */
fun decodeLogTail(decoder: Json, json: String): LogTail = try {
    decoder.decodeFromString(LogTail.serializer(), json)
} catch (_: SerializationException) {
    decoder.decodeFromString(PlainLogTail.serializer(), json).let { LogTail(it.lines, it.truncated) }
}

/** Entries numbered from [firstSeq]. */
fun List<LogEntry>.numbered(firstSeq: Long = 0): List<SeqLogEntry> = mapIndexed { i, e -> SeqLogEntry(firstSeq + i, e) }

/** Entries whose original line contains [filter] (case-insensitive); all of them when it is blank. */
fun List<SeqLogEntry>.matchingText(filter: String): List<SeqLogEntry> =
    if (filter.isBlank()) this else filter { it.entry.text.contains(filter, ignoreCase = true) }

data class LogLevelCounts(val all: Int, val warnings: Int, val errors: Int) {
    fun of(filter: LogLevelFilter) = when (filter) {
        LogLevelFilter.ALL -> all
        LogLevelFilter.WARNINGS -> warnings
        LogLevelFilter.ERRORS -> errors
    }
}

fun levelCounts(entries: List<SeqLogEntry>): LogLevelCounts {
    var warnings = 0
    var errors = 0
    for (e in entries) {
        when (e.entry.logLevel) {
            LogLevel.ERROR -> errors++
            LogLevel.WARN -> warnings++
            else -> Unit
        }
    }
    return LogLevelCounts(all = entries.size, warnings = warnings + errors, errors = errors)
}

/** One row of the log list. [key] is stable across appends. */
sealed interface LogRow {
    val key: String

    /** A day header, shown before the first entry of each day. */
    data class Day(val date: LocalDate, val beforeSeq: Long) : LogRow {
        override val key get() = "d$beforeSeq"
    }

    /**
     * [count] consecutive identical entries; [entry] is the newest one, [firstTs] the time of
     * the oldest (0 when unknown).
     */
    data class Entry(val seq: Long, val entry: LogEntry, val count: Int = 1, val firstTs: Long = entry.ts) : LogRow {
        override val key get() = "e$seq"
    }
}

/**
 * Rows for [entries] (oldest first): those matching [level] with runs of identical entries
 * collapsed, and a day header whenever the local date (in [zone]) changes.
 */
fun logRows(entries: List<SeqLogEntry>, level: LogLevelFilter, zone: ZoneId): List<LogRow> {
    val rows = ArrayList<LogRow>()
    var lastDay: LocalDate? = null
    for ((seq, entry) in entries) {
        if (!entry.matches(level)) continue
        val last = rows.lastOrNull()
        if (last is LogRow.Entry && last.entry.sameAs(entry)) {
            rows[rows.lastIndex] = last.copy(entry = entry, count = last.count + 1, firstTs = last.firstTs.takeIf { it > 0 } ?: entry.ts)
            continue
        }
        if (entry.ts > 0) {
            val day = Instant.ofEpochMilli(entry.ts).atZone(zone).toLocalDate()
            if (day != lastDay) rows += LogRow.Day(day, seq)
            lastDay = day
        }
        rows += LogRow.Entry(seq, entry)
    }
    return rows
}
