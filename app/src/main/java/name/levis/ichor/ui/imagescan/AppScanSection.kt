package name.levis.ichor.ui.imagescan

import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
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
import name.levis.ichor.model.OperatorReports
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.theme.LocalStatusColors

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
    val onScan: () -> Unit,
    val onStop: () -> Unit,
    /** Opens a report, with the core's own JSON for the exports ("" to encode it again). */
    val onOpen: (ImageScanReport, String) -> Unit,
) {
    /** The report to show: the app's last scan when it scanned something, else the operator's. */
    val report: Pair<ImageScanReport, String>?
        get() = session?.usableReport?.let { it to session.reportJson }
            ?: (operator as? UiState.Loaded)?.data?.report?.takeIf { it.images.isNotEmpty() }?.let { it to "" }

    /** Whether the report shown is this app's own scan (the button then scans again). */
    val scanned: Boolean get() = session?.usableReport != null
}

/** The app's vulnerabilities: the scan running, the last report's summary, or how to start one. */
fun LazyListScope.appScanSection(ui: AppScanUi) {
    item { SectionTitle(stringResource(R.string.imagescan_title)) }
    item { ScanBody(ui) }
}

@Composable
private fun ScanBody(ui: AppScanUi) {
    val session = ui.session
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (session?.running == true) {
            Running(session, ui.onStop)
            return@Column
        }
        session?.error?.let { Text(stringResource(R.string.imagescan_failed, coreErrorText(it)), color = LocalStatusColors.current.bad) }
        (ui.operator as? UiState.Failed)?.let {
            MutedText(stringResource(R.string.imagescan_operator_failed, it.message.asString()))
        }
        val shown = ui.report
        if (shown == null) {
            MutedText(stringResource(R.string.imagescan_hint, IMAGESCAN_NAMESPACE))
        } else {
            Summary(shown.first)
        }
        if (ui.busyElsewhere) MutedText(stringResource(R.string.imagescan_busy_elsewhere))
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            shown?.let { (report, json) ->
                FilledTonalButton(onClick = { ui.onOpen(report, json) }) {
                    Icon(Icons.Outlined.Description, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.imagescan_open), modifier = Modifier.padding(start = 6.dp))
                }
            }
            OutlinedButton(onClick = ui.onScan, enabled = !ui.busyElsewhere) {
                Icon(Icons.Outlined.Security, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(
                    stringResource(if (ui.scanned) R.string.imagescan_scan_again else R.string.imagescan_scan),
                    modifier = Modifier.padding(start = 6.dp),
                )
            }
        }
    }
}

/** Severity pills, then where the report comes from and the images it could not scan. */
@Composable
private fun Summary(report: ImageScanReport) {
    SeverityPills(report.summary)
    MutedText(reportSource(report))
    if (report.failed > 0) {
        Text(
            stringResource(R.string.imagescan_images_failed, report.failed.toString()),
            style = MaterialTheme.typography.bodySmall,
            color = LocalStatusColors.current.warn,
        )
    }
}

@Composable
private fun Running(session: ImageScanSession, onStop: () -> Unit) {
    val p = session.progress
    Text(if (session.stopping) stringResource(R.string.imagescan_stopping) else phaseText(p))
    if (p?.phase == IMAGESCAN_PHASE_SCANNING && p.steps > 0) {
        LinearProgressIndicator(progress = { (p.step - 1).coerceAtLeast(0) / p.steps.toFloat() }, modifier = Modifier.fillMaxWidth())
    } else {
        LinearProgressIndicator(Modifier.fillMaxWidth())
    }
    if (!session.stopping) {
        OutlinedButton(onClick = onStop) { Text(stringResource(R.string.imagescan_stop)) }
    }
}

@Composable
private fun phaseText(p: ImageScanProgress?): String = when (p?.phase) {
    IMAGESCAN_PHASE_STARTING -> if (p.message.isNotEmpty()) {
        stringResource(R.string.imagescan_phase_starting_reason, p.message)
    } else {
        stringResource(R.string.imagescan_phase_starting)
    }
    IMAGESCAN_PHASE_DATABASE -> stringResource(R.string.imagescan_phase_database)
    IMAGESCAN_PHASE_SCANNING -> stringResource(
        R.string.imagescan_phase_scanning,
        p.step.toString(),
        p.steps.toString(),
        p.image.substringAfterLast('/'),
    )
    IMAGESCAN_PHASE_CLEANING -> stringResource(R.string.imagescan_phase_cleaning)
    else -> stringResource(R.string.imagescan_phase_preparing)
}
