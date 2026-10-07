package name.levis.ichor.ui.imagescan

import androidx.annotation.StringRes
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.lerp
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.IMAGESCAN_NOT_SCANNED
import name.levis.ichor.model.IMAGESCAN_SOURCE_OPERATOR
import name.levis.ichor.model.IMAGESCAN_TIMED_OUT
import name.levis.ichor.model.ImageScanReport
import name.levis.ichor.model.VulnSeverity
import name.levis.ichor.model.VulnSummary
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

@StringRes
fun severityLabel(severity: VulnSeverity): Int = when (severity) {
    VulnSeverity.CRITICAL -> R.string.imagescan_severity_critical
    VulnSeverity.HIGH -> R.string.imagescan_severity_high
    VulnSeverity.MEDIUM -> R.string.imagescan_severity_medium
    VulnSeverity.LOW -> R.string.imagescan_severity_low
    VulnSeverity.UNKNOWN -> R.string.imagescan_severity_unknown
}

/** Critical and high in the "bad" colour (high halfway to "warn"), medium "warn", the rest muted. */
@Composable
fun severityColor(severity: VulnSeverity): Color {
    val colors = LocalStatusColors.current
    return when (severity) {
        VulnSeverity.CRITICAL -> colors.bad
        VulnSeverity.HIGH -> lerp(colors.bad, colors.warn, 0.45f)
        VulnSeverity.MEDIUM -> colors.warn
        VulnSeverity.LOW, VulnSeverity.UNKNOWN -> colors.muted
    }
}

/** "Critical 2 · High 5 · …": one pill per severity found, muted when none. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun SeverityPills(summary: VulnSummary) {
    val muted = LocalStatusColors.current.muted
    FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        VulnSeverity.entries.forEach { severity ->
            val count = summary.count(severity)
            if (severity == VulnSeverity.UNKNOWN && count == 0) return@forEach
            StatusPill(
                stringResource(R.string.imagescan_severity_count, stringResource(severityLabel(severity)), count.toString()),
                if (count > 0) severityColor(severity) else muted,
            )
        }
    }
}

/** "Trivy 0.75.0 · 3 min ago" for a scan, "From the Trivy Operator · 2 days ago" for its reports. */
@Composable
fun reportSource(report: ImageScanReport): String = if (report.source == IMAGESCAN_SOURCE_OPERATOR) {
    val at = report.images.maxOfOrNull { it.scannedAt }?.takeIf { it > 0 } ?: report.finished
    stringResource(R.string.imagescan_source_operator, timeAgo(at))
} else {
    stringResource(R.string.imagescan_source_scan, report.scanner, timeAgo(report.finished))
}

/** A core message in the user's language when it is one of the scan's own. */
@Composable
fun coreErrorText(message: String): String = when (message) {
    IMAGESCAN_NOT_SCANNED -> stringResource(R.string.imagescan_not_scanned)
    IMAGESCAN_TIMED_OUT -> stringResource(R.string.imagescan_timed_out)
    else -> message
}
