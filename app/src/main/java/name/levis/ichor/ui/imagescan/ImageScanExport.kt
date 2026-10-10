package name.levis.ichor.ui.imagescan

import android.content.Context
import android.widget.Toast
import androidx.compose.runtime.Composable
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.platform.LocalContext
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import name.levis.ichor.R
import name.levis.ichor.model.ImageScanFormat
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.ui.uiText
import name.levis.ichor.ui.components.shareFile
import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale

/** Where exported reports wait for the share sheet (res/xml/capture_paths.xml). */
private fun exportDir(context: Context) = File(context.cacheDir, "scans")

/** How long an exported report stays for the app it was shared to (an upload reads it late). */
private const val EXPORT_KEEP_MS = 60 * 60 * 1000L

/**
 * Writes [content], a report in [format], to a file named after [appName] and the date, for
 * the share sheet. Exports older than [EXPORT_KEEP_MS] are removed first. Blocking.
 */
fun writeReport(context: Context, appName: String, format: ImageScanFormat, content: String): File {
    val dir = exportDir(context).apply { mkdirs() }
    val now = System.currentTimeMillis()
    dir.listFiles()?.filter { now - it.lastModified() > EXPORT_KEEP_MS }?.forEach { it.delete() }
    val stamp = SimpleDateFormat("yyyyMMdd-HHmm", Locale.ROOT).format(Date())
    val safe = appName.lowercase(Locale.ROOT).replace(Regex("[^a-z0-9._-]+"), "-").trim('-').ifEmpty { "app" }
    val file = File(dir, "$safe-vulnerabilities-$stamp.${format.extension}")
    file.writeText(content)
    return file
}

/**
 * [report] over the app's sheet, exported on request: [json] (the core's own report, or "")
 * written in the chosen format by [export], then shared as a file named after [appName].
 */
@Composable
fun ImageScanReportOpen(
    report: ImageScanReport,
    json: String,
    appName: String,
    export: suspend (ImageScanReport, String, ImageScanFormat) -> String,
    onDismiss: () -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    ImageScanReportSheet(
        report = report,
        onExport = { format ->
            scope.launch {
                runCatching {
                    val content = export(report, json, format)
                    withContext(Dispatchers.IO) { writeReport(context, appName, format, content) }
                }.onSuccess { file ->
                    shareFile(context, file, format.mime, R.string.imagescan_share_chooser)
                }.onFailure { e ->
                    val reason = e.uiText().resolve(context)
                    Toast.makeText(context, context.getString(R.string.imagescan_export_failed, reason), Toast.LENGTH_LONG).show()
                }
            }
        },
        onDismiss = onDismiss,
    )
}
