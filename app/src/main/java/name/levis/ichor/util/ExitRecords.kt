package name.levis.ichor.util

import android.annotation.SuppressLint
import android.app.ActivityManager
import android.app.ApplicationExitInfo
import android.content.Context
import android.os.Build
import androidx.annotation.RequiresApi
import java.io.IOException
import java.io.InputStream
import name.levis.ichor.data.Diagnostics
import name.levis.ichor.data.UiPreferences
import name.levis.ichor.model.ExitRecord
import name.levis.ichor.model.ExitReport

/**
 * The exits of the app's process the OS kept (ApplicationExitInfo, Android 11+): an ANR, a
 * crash, a low-memory kill, a force stop. Read on the next launch, the new ones are copied
 * once to the diagnostics log; the Diagnostics screen and the zip read them all. Below
 * Android 11 there is no such record: [read] says unsupported.
 */
object ExitRecords {
    /** How many records the OS is asked for (it keeps a few per package). */
    private const val MAX_RECORDS = 10

    /** The part of a trace kept: the head of an ANR dump holds the main thread. */
    internal const val TRACE_CAP = 64 * 1024

    /** A run of readable bytes shorter than this in a native tombstone is noise. */
    private const val MIN_RUN = 4

    /** The OS's records, newest first; never throws (an unreadable one is logged and skipped). */
    fun read(context: Context): ExitReport {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.R) return ExitReport(supported = false)
        val manager = context.getSystemService(ActivityManager::class.java) ?: return ExitReport(supported = true)
        val infos = try {
            manager.getHistoricalProcessExitReasons(context.packageName, 0, MAX_RECORDS)
        } catch (e: SecurityException) {
            logFailure(e)
            emptyList()
        }
        return ExitReport(supported = true, exits = infos.map(::record))
    }

    /**
     * Copies the records newer than the last launch's to the diagnostics log, once, then moves
     * [prefs]' mark. Blocking: call it on an IO thread.
     */
    fun logNew(context: Context, prefs: UiPreferences) {
        val report = read(context)
        val since = prefs.lastExitRecordAt
        val fresh = newerThan(report.exits, since)
        fresh.sortedBy { it.at }.forEach { exit ->
            Diagnostics.log(
                "warn",
                "android",
                "process exit",
                mapOf(
                    "reason" to exit.reason, "status" to exit.status, "importance" to exit.importance,
                    "pss_kb" to exit.pss, "rss_kb" to exit.rss, "exit_at" to exit.at,
                ),
            )
        }
        fresh.maxOfOrNull { it.at }?.let { prefs.lastExitRecordAt = it }
    }

    /** The records of [exits] after [since] (unix ms). */
    internal fun newerThan(exits: List<ExitRecord>, since: Long): List<ExitRecord> = exits.filter { it.at > since }

    @RequiresApi(Build.VERSION_CODES.R)
    private fun record(info: ApplicationExitInfo): ExitRecord = ExitRecord(
        at = info.timestamp,
        reason = reasonName(info.reason),
        status = info.status,
        importance = info.importance,
        description = info.description.orEmpty(),
        pss = info.pss,
        rss = info.rss,
        trace = if (keepsTrace(info.reason)) trace(info) else "",
    )

    @RequiresApi(Build.VERSION_CODES.R)
    private fun trace(info: ApplicationExitInfo): String = try {
        info.traceInputStream?.use { traceText(it, native = info.reason == ApplicationExitInfo.REASON_CRASH_NATIVE) }.orEmpty()
    } catch (e: IOException) {
        logFailure(e)
        ""
    } catch (e: SecurityException) {
        logFailure(e)
        ""
    }

    /** Only an ANR's and a native crash's traces say where the app was. */
    @SuppressLint("InlinedApi")
    internal fun keepsTrace(reason: Int): Boolean =
        reason == ApplicationExitInfo.REASON_ANR || reason == ApplicationExitInfo.REASON_CRASH_NATIVE

    /**
     * The first [TRACE_CAP] bytes of [stream] as text. A native tombstone is a protobuf: only
     * its runs of readable characters are kept (abort message, frames, library names).
     */
    internal fun traceText(stream: InputStream, native: Boolean): String {
        val head = readCapped(stream, TRACE_CAP)
        return if (native) readableRuns(head) else head.decodeToString()
    }

    /** At most [cap] bytes of [stream]: the rest is never read. */
    internal fun readCapped(stream: InputStream, cap: Int): ByteArray {
        val out = ByteArray(cap)
        var size = 0
        while (size < cap) {
            val n = stream.read(out, size, cap - size)
            if (n < 0) break
            size += n
        }
        return out.copyOf(size)
    }

    /** The runs of printable ASCII of [bytes] at least [MIN_RUN] long, one per line. */
    internal fun readableRuns(bytes: ByteArray): String {
        val runs = mutableListOf<String>()
        val run = StringBuilder()
        fun flush() {
            if (run.length >= MIN_RUN) runs += run.toString()
            run.setLength(0)
        }
        bytes.forEach { b ->
            val c = b.toInt() and 0xff
            if (c in 0x20..0x7e) run.append(c.toChar()) else flush()
        }
        flush()
        return runs.joinToString("\n")
    }

    /**
     * ApplicationExitInfo.REASON_* as a stable lowercase name; a newer one is "reason_<n>".
     * The constants are compile-time ints, so the mapping runs on any version.
     */
    @SuppressLint("InlinedApi")
    internal fun reasonName(reason: Int): String = when (reason) {
        ApplicationExitInfo.REASON_UNKNOWN -> "unknown"
        ApplicationExitInfo.REASON_EXIT_SELF -> "exit_self"
        ApplicationExitInfo.REASON_SIGNALED -> "signaled"
        ApplicationExitInfo.REASON_LOW_MEMORY -> "low_memory"
        ApplicationExitInfo.REASON_CRASH -> "crash"
        ApplicationExitInfo.REASON_CRASH_NATIVE -> "crash_native"
        ApplicationExitInfo.REASON_ANR -> "anr"
        ApplicationExitInfo.REASON_INITIALIZATION_FAILURE -> "initialization_failure"
        ApplicationExitInfo.REASON_PERMISSION_CHANGE -> "permission_change"
        ApplicationExitInfo.REASON_EXCESSIVE_RESOURCE_USAGE -> "excessive_resource_usage"
        ApplicationExitInfo.REASON_USER_REQUESTED -> "user_requested"
        ApplicationExitInfo.REASON_USER_STOPPED -> "user_stopped"
        ApplicationExitInfo.REASON_DEPENDENCY_DIED -> "dependency_died"
        ApplicationExitInfo.REASON_OTHER -> "other"
        ApplicationExitInfo.REASON_FREEZER -> "freezer"
        ApplicationExitInfo.REASON_PACKAGE_STATE_CHANGE -> "package_state_change"
        ApplicationExitInfo.REASON_PACKAGE_UPDATED -> "package_updated"
        else -> "reason_$reason"
    }

    private fun logFailure(e: Exception) {
        Diagnostics.log("warn", "android", "exit records unreadable", mapOf("error" to e.javaClass.simpleName))
    }
}
