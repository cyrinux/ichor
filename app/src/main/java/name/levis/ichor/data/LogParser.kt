package name.levis.ichor.data

import name.levis.ichor.model.LogEntry
import name.levis.ichor.model.LogTail
import name.levis.ichor.model.decodeLogTail
import name.levis.ichor.model.logLines
import name.levis.ichorgo.Ichorgo

/**
 * The Go core's log parsing, local and without a network call: shared by the Talos and
 * Kubernetes log views. Every call is blocking: run it off the main thread.
 */
object LogParser {
    /** One line as a structured entry, its ANSI colours as spans; the plain line if it cannot be decoded. */
    fun line(line: String): LogEntry =
        runCatching { TalosJson.decodeFromString(LogEntry.serializer(), Ichorgo.parseLogLine(line)) }.getOrNull() ?: LogEntry.plain(line)

    /** A whole log (a pod's snapshot) as its lines and entries; its plain lines if the core fails. */
    fun text(text: String): LogTail =
        runCatching { decodeLogTail(TalosJson, Ichorgo.parseLogText(text)) }.getOrNull() ?: LogTail(lines = logLines(text))

    /** [text] without its ANSI escapes: what is copied and shared. */
    fun strip(text: String): String = runCatching { Ichorgo.stripLogEscapes(text) }.getOrDefault(text)
}
