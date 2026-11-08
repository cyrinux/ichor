package name.levis.ichor.model

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import kotlinx.serialization.json.Json
import name.levis.ichor.ui.logs.MIN_ANSI_CONTRAST
import name.levis.ichor.ui.logs.ansiPalette
import name.levis.ichor.ui.logs.contrastRatio
import name.levis.ichor.ui.logs.parseHexColor
import name.levis.ichor.ui.logs.readableOn
import name.levis.ichor.ui.logs.relativeLuminance
import name.levis.ichor.ui.logs.style
import name.levis.ichor.ui.logs.toAnnotated
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

    // ANSI colours. The vectors are go/ichorgo/logparse_ansi_test.go's ansiSampleLines, with the
    // spans its TestStripANSI table expects.

    private val surface = Color(0xFF121212)
    private val base = Color(0xFFE0E0E0)
    private val red = Color(0xFFFF6E6E)
    private val cyan = Color(0xFF4DD0E1)
    private val palette = ansiPalette(
        surface = surface,
        onSurface = base,
        onSurfaceVariant = Color(0xFFB0B0B0),
        red = red,
        green = Color(0xFF81C784),
        yellow = Color(0xFFFFD54F),
        blue = Color(0xFF90CAF9),
        magenta = Color(0xFFCE93D8),
        cyan = cyan,
    )

    private val sampleEntries = listOf(
        """{"ts":0,"level":"","source":"","msg":"","fields":[],"raw":"INFO[0042] request done    client_ip=10.0.0.7 status=200","spans":[
            {"t":"INFO","fg":6,"bg":-1},{"t":"[0042] request done    ","fg":-1,"bg":-1},{"t":"client_ip","fg":6,"bg":-1},
            {"t":"=10.0.0.7 ","fg":-1,"bg":-1},{"t":"status","fg":6,"bg":-1},{"t":"=200","fg":-1,"bg":-1}]}""",
        """{"ts":0,"level":"","source":"","msg":"","fields":[],"raw":"ERRO[0043] upstream timeout error=\"dial tcp: i/o timeout\"","spans":[
            {"t":"ERRO","fg":1,"bg":-1,"b":true},{"t":"[0043] upstream timeout ","fg":-1,"bg":-1},{"t":"error","fg":1,"bg":-1},
            {"t":"=\"dial tcp: i/o timeout\"","fg":-1,"bg":-1}]}""",
        """{"ts":0,"level":"","source":"","msg":"","fields":[],"raw":"WARN queue depth 850","spans":[
            {"t":"WARN","fg":-1,"bg":-1,"fgRgb":"#ff8700"},{"t":" queue depth ","fg":-1,"bg":-1},{"t":"850","fg":-1,"bg":-1,"fgRgb":"#ff8800"}]}""",
        """{"ts":0,"level":"","source":"","msg":"","fields":[],"raw":"run 1 finished","spans":[]}""",
    ).map { Json.decodeFromString(LogEntry.serializer(), it) }

    /** The styled ranges of [entry]'s line: (text, style). */
    private fun styled(entry: LogEntry): List<Pair<String, SpanStyle>> {
        val annotated = entry.spans.toAnnotated(palette, base)
        // No spans: AnsiLine draws the plain text instead.
        assertEquals(if (entry.spans.isEmpty()) "" else entry.text, annotated.text)
        return annotated.spanStyles.map { annotated.text.substring(it.start, it.end) to it.item }
    }

    @Test
    fun decodesSpans() {
        val (info, error, warn, link) = sampleEntries
        assertEquals(LogSpan("INFO", fg = 6), info.spans[0])
        assertEquals(LogSpan("ERRO", fg = 1, b = true), error.spans[0])
        assertEquals(LogSpan("850", fgRgb = "#ff8800"), warn.spans[2])
        assertEquals(emptyList<LogSpan>(), link.spans)
        // Older cores send no spans.
        assertEquals(emptyList<LogSpan>(), Json.decodeFromString(LogEntry.serializer(), """{"raw":"x"}""").spans)
        // The text filter searches the line without escapes.
        assertEquals(1, sampleEntries.numbered().matchingText("INFO[0042]").size)
    }

    @Test
    fun sharedVectorsBecomeStyledText() {
        val (info, error, warn, link) = sampleEntries
        assertEquals(
            listOf("INFO" to SpanStyle(color = cyan), "client_ip" to SpanStyle(color = cyan), "status" to SpanStyle(color = cyan)),
            styled(info),
        )
        assertEquals(
            listOf("ERRO" to SpanStyle(color = red, fontWeight = FontWeight.Bold), "error" to SpanStyle(color = red)),
            styled(error),
        )
        assertEquals(
            listOf("WARN" to SpanStyle(color = Color(0xFFFF8700)), "850" to SpanStyle(color = Color(0xFFFF8800))),
            styled(warn),
        )
        assertEquals(emptyList<Pair<String, SpanStyle>>(), styled(link))
    }

    @Test
    fun stylesBackgroundsAndBrightColours() {
        val dimItalic = LogSpan("x", d = true, i = true, u = true).style(palette, base)
        assertEquals(SpanStyle(color = base.copy(alpha = 0.6f), fontStyle = FontStyle.Italic, textDecoration = TextDecoration.Underline), dimItalic)
        // A background: the default text kept readable over it.
        val onRed = LogSpan("x", bg = 1).style(palette, base)!!
        assertEquals(red, onRed.background)
        assertTrue(contrastRatio(onRed.color, red) >= MIN_ANSI_CONTRAST)
        // Bright colours are lighter on a dark surface.
        assertTrue(palette.colors[9].relativeLuminance() > palette.colors[1].relativeLuminance())
        // ANSI black on a dark theme is not drawn black.
        assertTrue(contrastRatio(palette.colors[0], surface) >= MIN_ANSI_CONTRAST)
        assertEquals(null, LogSpan("plain").style(palette, base))
        assertEquals(null, parseHexColor("ff8700"))
    }

    @Test
    fun readableOnNudgesLowContrastColours() {
        val white = Color.White
        // Readable already: kept as is.
        assertEquals(cyan, cyan.readableOn(surface))
        assertEquals(Color(0xFF1565C0), Color(0xFF1565C0).readableOn(white))
        // Too dark on a dark surface: lighter, enough.
        val navy = Color(0xFF000040)
        val lighter = navy.readableOn(surface)
        assertTrue(lighter.relativeLuminance() > navy.relativeLuminance())
        assertTrue(contrastRatio(lighter, surface) >= MIN_ANSI_CONTRAST)
        // Too light on a light surface: darker, enough.
        val yellow = Color(0xFFFFEB3B)
        val darker = yellow.readableOn(white)
        assertTrue(darker.relativeLuminance() < yellow.relativeLuminance())
        assertTrue(contrastRatio(darker, white) >= MIN_ANSI_CONTRAST)
        // The truecolour sample on a light surface is nudged, not dropped.
        assertTrue(contrastRatio(Color(0xFFFF8700).readableOn(white), white) >= MIN_ANSI_CONTRAST)
    }

    @Test
    fun followBufferKeepsSpans() {
        val buffer = FollowBuffer().append(listOf("a")).appendEntries(sampleEntries.take(1))
        assertEquals(listOf(0L, 1L), buffer.lines.map { it.seq })
        assertEquals(6, buffer.lines[1].spans.size)
        assertEquals("a\nINFO[0042] request done    client_ip=10.0.0.7 status=200", buffer.text)
    }
}
