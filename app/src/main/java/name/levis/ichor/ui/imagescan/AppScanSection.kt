package name.levis.ichor.ui.imagescan

import android.content.Context
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Security
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.ImageScanSession
import name.levis.ichor.model.IMAGESCAN_PHASE_CLEANING
import name.levis.ichor.model.IMAGESCAN_PHASE_DATABASE
import name.levis.ichor.model.IMAGESCAN_PHASE_SCANNING
import name.levis.ichor.model.IMAGESCAN_PHASE_STARTING
import name.levis.ichor.model.ImageScanProgress
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.ImageScanner
import name.levis.ichor.model.OperatorReports
import name.levis.ichor.model.SavedScan
import name.levis.ichor.model.VulnSummary
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.operations.OperationText
import name.levis.ichor.ui.operations.res
import name.levis.ichor.ui.operations.resolve
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/** The scan in the cluster's namespace, named in the hint. */
const val IMAGESCAN_NAMESPACE = "ichor-imagescan"

/** What the app sheet needs to offer a vulnerability scan; null where the role cannot run one. */
data class AppScanUi(
    /** The Trivy Operator's reports on the app's images. */
    val operator: UiState<OperatorReports>,
    /** The scan of this app running or last run, null when none. */
    val session: ImageScanSession?,
    /** Another app's scan runs: one at a time. */
    val busyElsewhere: Boolean,
    /** The scanner picked in Settings, named in the hint. */
    val scanner: ImageScanner,
    val onScan: () -> Unit,
    val onStop: () -> Unit,
    /** Opens a report, with the core's own JSON for the exports ("" to encode it again). */
    val onOpen: (ImageScanReport, String) -> Unit,
    /** The newest report kept on the phone for this app, null when none. */
    val saved: SavedScan? = null,
    /** Opens [saved] (read from the phone, nothing runs in the cluster). */
    val onOpenSaved: (SavedScan) -> Unit = {},
    /** The cluster's kept reports. */
    val onSavedList: () -> Unit = {},
    /** A line under the summary (how an object's images are scanned), in place of the pods' hint; null for none. */
    val note: String? = null,
    /** False while there is nothing to scan yet (a workload's images not read). */
    val canStart: Boolean = true,
) {
    /** The report to show: the app's last scan when it scanned something, else the operator's. */
    val report: Pair<ImageScanReport, String>?
        get() = session?.usableReport?.let { it to session.reportJson }
            ?: (operator as? UiState.Loaded)?.data?.report?.takeIf { it.images.isNotEmpty() }?.let { it to "" }
}

/** The app's vulnerabilities: the scan running, the last report's summary, or how to start one. */
fun LazyListScope.appScanSection(ui: AppScanUi) {
    item { SectionTitle(stringResource(R.string.imagescan_title)) }
    item { ScanBody(ui) }
}

/** The same section in a plain column: an object's summary. */
@Composable
fun AppScanSection(ui: AppScanUi) {
    SectionTitle(stringResource(R.string.imagescan_title))
    ScanBody(ui)
}

@Composable
private fun ScanBody(ui: AppScanUi) {
    val session = ui.session
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (session?.running == true) {
            ScanProgressLines(session, ui.onStop)
            return@Column
        }
        session?.error?.let { Text(stringResource(R.string.imagescan_failed, coreErrorText(it)), color = LocalStatusColors.current.bad) }
        (ui.operator as? UiState.Failed)?.let {
            MutedText(stringResource(R.string.imagescan_operator_failed, it.message.asString()))
        }
        // This session's report first, then the newest kept on the phone, then the operator's.
        val current = session?.usableReport
        val saved = ui.saved.takeIf { current == null }
        val shown = ui.report
        when {
            current != null -> {
                LastScanLines(current.finished, current.summary)
                NewFindingsLine(current.newSummary, current.hasPrevious)
                ScanFailures(current)
            }
            saved != null -> {
                LastScanLines(saved.finished, saved.summary)
                NewFindingsLine(saved.new, hasPrevious = false)
            }
            shown != null -> ScanSummary(shown.first)
            // The pods' hint says "by digest": a note of its own replaces it.
            ui.note == null -> MutedText(stringResource(R.string.imagescan_hint, ui.scanner.label, IMAGESCAN_NAMESPACE))
        }
        ui.note?.let { MutedText(it) }
        val onView: (() -> Unit)? = when {
            current != null -> fun() = ui.onOpen(current, session?.reportJson.orEmpty())
            saved != null -> fun() = ui.onOpenSaved(saved)
            shown != null -> fun() = ui.onOpen(shown.first, shown.second)
            else -> null
        }
        if (ui.busyElsewhere) MutedText(stringResource(R.string.imagescan_busy_elsewhere))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            onView?.let { view ->
                FilledTonalButton(onClick = view) {
                    Icon(Icons.Outlined.Description, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.imagescan_view), modifier = Modifier.padding(start = 6.dp))
                }
            }
            OutlinedButton(onClick = ui.onScan, enabled = !ui.busyElsewhere && ui.canStart) {
                Icon(Icons.Outlined.Security, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(
                    stringResource(if (current != null || saved != null) R.string.imagescan_scan_again else R.string.imagescan_scan),
                    modifier = Modifier.padding(start = 6.dp),
                )
            }
        }
        TextButton(onClick = ui.onSavedList, contentPadding = PaddingValues(0.dp)) {
            Text(stringResource(R.string.imagescan_saved_link))
        }
    }
}

/** "Last scan 3 min ago" and the severities it found. */
@Composable
private fun LastScanLines(finished: Long, summary: VulnSummary) {
    MutedText(stringResource(R.string.imagescan_last_scan, timeAgo(finished)))
    SeverityPills(summary)
}

/** Severity pills, then where the report comes from and the images it could not scan. */
@Composable
fun ScanSummary(report: ImageScanReport) {
    SeverityPills(report.summary)
    MutedText(reportSource(report))
    ScanFailures(report)
}

/** "2 images could not be scanned", when some could not. */
@Composable
private fun ScanFailures(report: ImageScanReport) {
    if (report.failed > 0) {
        Text(
            stringResource(R.string.imagescan_images_failed, report.failed.toString()),
            style = MaterialTheme.typography.bodySmall,
            color = LocalStatusColors.current.warn,
        )
    }
}

/** The running scan's phase, a progress bar (by image while scanning) and Stop. */
@Composable
fun ScanProgressLines(session: ImageScanSession, onStop: () -> Unit) {
    val p = session.progress
    Text(if (session.stopping) stringResource(R.string.imagescan_stopping) else scanPhaseText(LocalContext.current, p, session.scanner))
    if (p?.phase == IMAGESCAN_PHASE_SCANNING && p.steps > 0) {
        LinearProgressIndicator(progress = { (p.step - 1).coerceAtLeast(0) / p.steps.toFloat() }, modifier = Modifier.fillMaxWidth())
    } else {
        LinearProgressIndicator(Modifier.fillMaxWidth())
    }
    if (!session.stopping) {
        OutlinedButton(onClick = onStop) { Text(stringResource(R.string.imagescan_stop)) }
    }
}

/** The scan's phase in words, on its screen and in its notification. */
fun scanPhaseLabel(p: ImageScanProgress?, scanner: ImageScanner): OperationText = when (p?.phase) {
    IMAGESCAN_PHASE_STARTING -> if (p.message.isNotEmpty()) {
        res(R.string.imagescan_phase_starting_reason, scanner.label, p.message)
    } else {
        res(R.string.imagescan_phase_starting, scanner.label)
    }
    IMAGESCAN_PHASE_DATABASE -> res(R.string.imagescan_phase_database)
    IMAGESCAN_PHASE_SCANNING -> res(
        R.string.imagescan_phase_scanning,
        p.step.toString(),
        p.steps.toString(),
        p.image.substringAfterLast('/'),
    )
    IMAGESCAN_PHASE_CLEANING -> res(R.string.imagescan_phase_cleaning)
    else -> res(R.string.imagescan_phase_preparing)
}

fun scanPhaseText(res: Context, p: ImageScanProgress?, scanner: ImageScanner): String = scanPhaseLabel(p, scanner).resolve(res)
