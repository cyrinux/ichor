package name.levis.ichor.data

import android.content.Context
import android.os.Build
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import name.levis.ichor.BuildConfig
import name.levis.ichor.model.AppFacts
import name.levis.ichor.model.DataStoreStatus
import name.levis.ichor.model.DiagEntry
import name.levis.ichor.model.DiagExportSummary
import name.levis.ichor.model.ExitReport
import name.levis.ichorgo.Ichorgo

/**
 * The core's redacted diagnostics log (go/ichorgo/diag*.go) and the zip made from it, on
 * this device only: nothing leaves the phone unless the user shares the zip. Every line is
 * redacted by the core when it is written; still, never hand it a cluster, node, namespace,
 * object name or path.
 */
object Diagnostics {
    /** The newest [limit] entries (0 = all), newest first. */
    suspend fun entries(limit: Int): List<DiagEntry> = withContext(Dispatchers.IO) {
        TalosJson.decodeFromString(ListSerializer(DiagEntry.serializer()), Ichorgo.diagEntries(limit.toLong()))
    }

    /**
     * Adds an entry: [level] debug, info, warn or error; [msg] a constant text; [attrs]
     * numbers, booleans and short words. Never throws: a log is not worth a failure.
     */
    fun log(level: String, component: String, msg: String, attrs: Map<String, Any> = emptyMap()) {
        runCatching { Ichorgo.diagLog(level, component, msg, attrsJson(attrs)) }
    }

    /** Forgets every entry, and the kept files. */
    suspend fun clear() = withContext(Dispatchers.IO) { Ichorgo.diagClear() }

    /** Keeps the log in the data directory across launches ([on]) or in memory only (its files deleted). */
    suspend fun setPersist(on: Boolean) = withContext(Dispatchers.IO) { runCatching { Ichorgo.setDiagPersist(on) } }

    /**
     * Writes the zip at [dest] (under the cache directory) with [app] as app.json and
     * [platform] as platform.json, and says what it holds.
     */
    suspend fun export(dest: File, app: AppFacts, platform: ExitReport): DiagExportSummary = withContext(Dispatchers.IO) {
        val out = Ichorgo.diagExport(dest.path, appJson(app), DiagJson.encodeToString(ExitReport.serializer(), platform))
        TalosJson.decodeFromString(DiagExportSummary.serializer(), out)
    }

    /** Every key written, defaults included: app.json names each fact even when it is empty. */
    private val DiagJson = Json(TalosJson) { encodeDefaults = true }

    /** [app] as app.json. */
    internal fun appJson(app: AppFacts): String = DiagJson.encodeToString(AppFacts.serializer(), app)

    /** This build and device, for app.json. */
    fun appFacts(context: Context): AppFacts = appFacts(
        versionName = BuildConfig.VERSION_NAME,
        versionCode = BuildConfig.VERSION_CODE,
        flavor = BuildConfig.BUILD_TYPE,
        sdkInt = Build.VERSION.SDK_INT,
        model = Build.MODEL,
        manufacturer = Build.MANUFACTURER,
        locale = context.resources.configuration.locales[0]?.toLanguageTag(),
        dataStore = runCatching { Ichorgo.dataStoreStatus() }.getOrNull(),
    )

    /** [appFacts] from plain values: a value the device does not give is "" (never null in app.json). */
    internal fun appFacts(
        versionName: String?,
        versionCode: Int,
        flavor: String?,
        sdkInt: Int,
        model: String?,
        manufacturer: String?,
        locale: String?,
        dataStore: String?,
    ) = AppFacts(
        versionName = versionName.orEmpty(),
        versionCode = versionCode,
        flavor = flavor.orEmpty(),
        sdkInt = sdkInt,
        model = model.orEmpty(),
        manufacturer = manufacturer.orEmpty(),
        locale = locale.orEmpty(),
        dataStore = dataStore?.let { runCatching { TalosJson.decodeFromString(DataStoreStatus.serializer(), it) }.getOrNull() }
            ?: DataStoreStatus(),
    )

    /** "ichor-diagnostics-20261029-1412.zip" for [at] (unix ms, local time). */
    fun zipName(at: Long): String = "ichor-diagnostics-${SimpleDateFormat("yyyyMMdd-HHmm", Locale.ROOT).format(Date(at))}.zip"

    /** [attrs] as the flat JSON object DiagLog takes; a value of another type is its text. */
    internal fun attrsJson(attrs: Map<String, Any>): String = JsonObject(
        attrs.mapValues { (_, v) ->
            when (v) {
                is Number -> JsonPrimitive(v)
                is Boolean -> JsonPrimitive(v)
                else -> JsonPrimitive(v.toString())
            }
        },
    ).toString()
}
