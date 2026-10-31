package name.levis.ichor.ui.fleet

import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ContextSummary
import name.levis.ichor.model.FLEET_CRITICAL
import name.levis.ichor.model.FleetFinding
import name.levis.ichor.model.FleetRow
import name.levis.ichor.model.FleetTone
import name.levis.ichor.model.seedOf
import name.levis.ichor.model.subjectShown
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/**
 * One cluster of the fleet: its tone dot in the cluster's colour ring, its name, "2/3 ready",
 * the critical and warning counts, the top finding and the check's age. [cluster] is its
 * context on the phone (null for the demo's made-up fleet: no colour of its own).
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
internal fun FleetRowCard(row: FleetRow, cluster: ContextSummary?, colors: Map<String, Int>, masked: Boolean, now: Long, onClick: () -> Unit) {
    val status = LocalStatusColors.current
    IchorCard(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Row(Modifier.padding(16.dp), verticalAlignment = Alignment.Top, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            ToneDot(row.tone, ring = if (cluster != null) Color(colors.seedOf(cluster)) else status.muted)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(row.name, style = MaterialTheme.typography.titleMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    if (row.nodes > 0) {
                        Text(
                            stringResource(R.string.widget_ready_count, row.ready, row.nodes),
                            style = MaterialTheme.typography.bodyMedium,
                            modifier = Modifier.align(Alignment.CenterVertically),
                        )
                    }
                    if (row.critical > 0) StatusPill(stringResource(R.string.fleet_critical, row.critical), status.bad)
                    if (row.warnings > 0) StatusPill(stringResource(R.string.fleet_warnings, row.warnings), status.warn)
                }
                row.topFinding?.let { finding ->
                    Text(
                        findingText(finding, masked),
                        style = MaterialTheme.typography.bodyMedium,
                        color = if (finding.severity == FLEET_CRITICAL) status.bad else status.warn,
                        maxLines = 2,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                MutedText(checkedText(row, now))
            }
        }
    }
}

/** The tone as a filled dot, ringed in the cluster's colour, read aloud as the tone's word. */
@Composable
private fun ToneDot(tone: FleetTone, ring: Color) {
    val label = stringResource(toneLabel(tone))
    val status = LocalStatusColors.current
    val fill = when (tone) {
        FleetTone.BAD -> status.bad
        FleetTone.WARN -> status.warn
        FleetTone.UNKNOWN -> status.muted
        FleetTone.OK -> status.ok
    }
    Box(
        Modifier.padding(top = 4.dp).size(18.dp).border(2.dp, ring, CircleShape).padding(4.dp).background(fill, CircleShape)
            .semantics { contentDescription = label },
    )
}

@StringRes
private fun toneLabel(tone: FleetTone): Int = when (tone) {
    FleetTone.BAD -> R.string.fleet_tone_bad
    FleetTone.WARN -> R.string.fleet_tone_warn
    FleetTone.UNKNOWN -> R.string.fleet_tone_unknown
    FleetTone.OK -> R.string.fleet_tone_ok
}

/** "Alert: KubePodCrashLooping · shop/checkout": the track in the app's language, the subject as the core wrote it. */
@Composable
private fun findingText(finding: FleetFinding, masked: Boolean): String {
    val track = stringResource(trackLabel(finding))
    return if (finding.subjectShown(masked)) stringResource(R.string.fleet_finding, track, finding.subject) else track
}

/** The word for [finding]'s track, as the fleet screen and the fleet widget show it. */
@StringRes
internal fun trackLabel(finding: FleetFinding): Int = when (finding.track) {
    "nodes" -> R.string.overview_stat_nodes
    "reach" -> R.string.fleet_track_reach
    "etcd" -> R.string.fleet_track_etcd
    "alertmanager" -> R.string.fleet_track_alert
    "checkup" -> R.string.checkup_title
    "data" -> R.string.data_services_title
    "gitops" -> when (finding.detail) {
        "argocd" -> R.string.argo_title
        "flux" -> R.string.flux_title
        else -> R.string.argo_card_title
    }
    "storage" -> R.string.storage_title
    "sensor" -> R.string.fleet_track_sensor
    "plugin" -> R.string.plugins_title
    else -> R.string.fleet_track_other
}

/** "Checked 12 minutes ago"; a row not checked lately says when it last was, or that it never was. */
@Composable
private fun checkedText(row: FleetRow, now: Long): String = when {
    row.checkedAt <= 0 -> stringResource(R.string.fleet_never_checked)
    row.stale -> stringResource(R.string.fleet_last_checked, timeAgo(row.checkedAt, now))
    else -> stringResource(R.string.fleet_checked, timeAgo(row.checkedAt, now))
}
