package name.levis.talosmobile.model

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.time.LocalDate
import java.time.ZoneOffset

class LogEntriesTest {

    private val utc = ZoneOffset.UTC
    private val day1 = 1_759_276_800_000L // 2025-10-01T00:00:00Z
    private val day2 = day1 + 86_400_000L

    private fun entry(level: String = "info", msg: String = "m", ts: Long = day1, fields: List<LogField> = emptyList()) =
        LogEntry(ts = ts, level = level, source = "talos", msg = msg, fields = fields, raw = "$level $msg")

    @Test
    fun decodesEntriesAndFallsBackToLines() {
        val json = Json { ignoreUnknownKeys = true }
        val structured = json.decodeFromString(
            LogTail.serializer(),
            """{"lines":["a"],"entries":[{"ts":5,"level":"warn","source":"kern","msg":"a","fields":[{"k":"x","v":"1"}],"raw":"a"}]}""",
        )
        assertEquals(listOf(LogEntry(5, "warn", "kern", "a", listOf(LogField("x", "1")), "a")), structured.toEntries())

        val old = json.decodeFromString(LogTail.serializer(), """{"lines":["a","b"],"truncated":true}""")
        assertEquals(listOf(LogEntry.plain("a"), LogEntry.plain("b")), old.toEntries())
        val malformed = decodeLogTail(json, """{"lines":["a"],"truncated":true,"entries":[{"ts":"soon"}]}""")
        assertEquals(LogTail(listOf("a"), truncated = true), malformed)
        assertEquals(listOf(LogEntry.plain("a")), malformed.toEntries())
        // A count mismatch is not trusted.
        assertEquals(listOf(LogEntry.plain("a"), LogEntry.plain("b")), LogTail(listOf("a", "b"), entries = listOf(entry())).toEntries())
    }

    @Test
    fun levelsAndFilters() {
        assertEquals(LogLevel.WARN, entry(level = "WARNING").logLevel)
        assertEquals(LogLevel.NONE, entry(level = "").logLevel)
        val error = entry(level = "error")
        val warn = entry(level = "warn")
        val info = entry(level = "info")
        assertTrue(error.matches(LogLevelFilter.ERRORS) && error.matches(LogLevelFilter.WARNINGS))
        assertEquals(listOf(true, true, false), listOf(error, warn, info).map { it.matches(LogLevelFilter.WARNINGS) })
        assertEquals(listOf(true, false, false), listOf(error, warn, info).map { it.matches(LogLevelFilter.ERRORS) })
        assertTrue(isErrorKey("Err") && isErrorKey("reason") && !isErrorKey("errors"))
    }

    @Test
    fun countsPerChip() {
        val entries = listOf(entry("error"), entry("warn"), entry("warn"), entry("info"), entry("")).numbered()
        assertEquals(LogLevelCounts(all = 5, warnings = 3, errors = 1), levelCounts(entries))
    }

    @Test
    fun textFilterSearchesTheRawLine() {
        val entries = listOf(LogEntry(msg = "short", raw = "2025 kern: Boom happened"), LogEntry.plain("other")).numbered()
        assertEquals(listOf(entries[0]), entries.matchingText("boom"))
        assertEquals(entries, entries.matchingText("  "))
    }

    @Test
    fun appendKeepsSeqAndCap() {
        val first = listOf(entry(msg = "a"), entry(msg = "b")).numbered()
        val added = listOf(entry(msg = "c")).numbered(firstSeq = 2)
        assertEquals(listOf(1L, 2L), first.appendCapped(added, cap = 2).map { it.seq })
    }

    @Test
    fun collapsesIdenticalConsecutiveEntriesIgnoringTime() {
        val entries = listOf(
            entry(msg = "probe failed", ts = day1 + 1_000),
            entry(msg = "probe failed", ts = day1 + 2_000),
            entry(msg = "probe failed", ts = day1 + 3_000),
            entry(msg = "probe failed", ts = day1 + 4_000, fields = listOf(LogField("n", "2"))),
            entry(msg = "ok", ts = day1 + 5_000),
            entry(msg = "probe failed", ts = day1 + 6_000),
        ).numbered()
        val rows = logRows(entries, LogLevelFilter.ALL, utc).filterIsInstance<LogRow.Entry>()
        assertEquals(listOf(3, 1, 1, 1), rows.map { it.count })
        assertEquals(LogRow.Entry(0, entries[2].entry, count = 3, firstTs = day1 + 1_000), rows[0])
        assertEquals(listOf("e0", "e3", "e4", "e5"), rows.map { it.key })
    }

    @Test
    fun collapsesAcrossFilteredOutEntries() {
        val entries = listOf(entry("error", "x"), entry("info", "noise"), entry("error", "x")).numbered()
        val rows = logRows(entries, LogLevelFilter.ERRORS, utc).filterIsInstance<LogRow.Entry>()
        assertEquals(listOf(2), rows.map { it.count })
    }

    @Test
    fun dayHeadersWhenTheDateChanges() {
        val entries = listOf(
            entry(msg = "a", ts = day1 + 10),
            entry(msg = "b", ts = 0),
            entry(msg = "c", ts = day1 + 20),
            entry(msg = "d", ts = day2 + 10),
        ).numbered()
        val rows = logRows(entries, LogLevelFilter.ALL, utc)
        assertEquals(
            listOf("d0", "e0", "e1", "e2", "d3", "e3"),
            rows.map { it.key },
        )
        assertEquals(LocalDate.of(2025, 10, 2), (rows[4] as LogRow.Day).date)
        // Undated logs (no timestamps) get no headers.
        assertEquals(listOf("e0"), logRows(listOf(LogEntry.plain("x")).numbered(), LogLevelFilter.ALL, utc).map { it.key })
    }

    @Test
    fun dayDependsOnTheZone() {
        val late = listOf(entry(ts = day1 - 1_000)).numbered() // 23:59:59 UTC the day before
        val rows = logRows(late, LogLevelFilter.ALL, ZoneOffset.ofHours(2))
        assertEquals(LocalDate.of(2025, 10, 1), (rows[0] as LogRow.Day).date)
    }
}
