package name.levis.ichor.model

import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonPrimitive

// Mirrors go/ichorgo/diag.go and diag_export.go. The keys Go always writes have no default,
// so an entry missing one fails to decode instead of showing an empty row.

/** One entry of the core's redacted diagnostics log (DiagEntries). */
@Serializable
data class DiagEntry(
    /** Unix millis. */
    val at: Long,
    /** debug, info, warn or error. */
    val level: String,
    /** kube, talos, gitops, net, store, trace, api, android, ios. */
    val component: String,
    val msg: String,
    /** Numbers, booleans and short words, already redacted. */
    val attrs: Map<String, JsonPrimitive> = emptyMap(),
    val err: String = "",
)

/** What DiagExport returns: where the zip is, its size, the log entries in it and its members. */
@Serializable
data class DiagExportSummary(val path: String, val bytes: Long, val entries: Int, val files: List<String>)

/**
 * One exit of the app's process the OS kept (ApplicationExitInfo, Android 11+). [trace] is
 * set for ANRs (the thread dump) and native crashes (the tombstone's readable text), capped.
 */
@Serializable
data class ExitRecord(
    /** Unix millis. */
    val at: Long,
    /** anr, crash, crash_native, low_memory, user_requested… (see util.ExitRecords). */
    val reason: String,
    /** The exit status or the signal number. */
    val status: Int,
    /** The process importance when it died (100 foreground … 400 cached). */
    val importance: Int,
    val description: String = "",
    /** Kilobytes. */
    val pss: Long = 0,
    /** Kilobytes. */
    val rss: Long = 0,
    val trace: String = "",
)

/** The OS's exit records for the app: platform.json in the zip. [supported] is false below Android 11. */
@Serializable
data class ExitReport(val supported: Boolean, val exits: List<ExitRecord> = emptyList())

/** The app's own facts: app.json in the zip. Never a path, a cluster or an account. */
@Serializable
data class AppFacts(
    val versionName: String,
    val versionCode: Int,
    /** The build type: debug, release (the open-source APK) or play. */
    val flavor: String,
    val sdkInt: Int,
    val model: String,
    val manufacturer: String,
    /** A language tag such as "fr-FR". */
    val locale: String,
    /** Ichorgo.dataStoreStatus(): whether the core keeps its data, and why not. */
    val dataStore: DataStoreStatus,
)

/** Mirrors go/ichorgo/nodenames.go dataStoreStatus. */
@Serializable
data class DataStoreStatus(val persisting: Boolean = false, val dir: String = "", val reason: String = "")

/** What the Diagnostics screen shows: the exit records on top, then the newest log entries. */
data class DiagnosticsView(val exits: ExitReport, val entries: List<DiagEntry>)
