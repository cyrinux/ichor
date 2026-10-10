package name.levis.ichor.ui.kubeevents

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Sensors
import androidx.compose.material.icons.outlined.SensorsOff
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.SyncProblem
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.LiveKubeEvent
import name.levis.ichor.ui.checkup.ageSince
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * One coalesced event: its reason (amber for a Warning, with an icon so colour is not the
 * only sign), the object it is about, how often and when last, and its message on two
 * lines. [onClick] null: the object's kind is unknown, the row does not open anything.
 */
@Composable
internal fun KubeEventItem(e: LiveKubeEvent, now: Long, showNamespace: Boolean, onClick: (() -> Unit)?) {
    val colors = LocalStatusColors.current
    val tint = if (e.isWarning) colors.warn else colors.muted
    val target = e.regarding.let { if (showNamespace || it.namespace.isEmpty()) it.label else "${it.kind} ${it.name}" }
    Column(
        Modifier.fillMaxWidth()
            .then(if (onClick != null) Modifier.clickable(onClickLabel = stringResource(R.string.kube_events_open_object), onClick = onClick) else Modifier)
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            if (e.isWarning) {
                Icon(Icons.Outlined.Warning, contentDescription = stringResource(R.string.events_severity_warning), tint = tint, modifier = Modifier.size(16.dp))
            }
            TagBadge(e.reason, tint)
            if (e.count > 1) {
                val repeated = pluralStringResource(R.plurals.events_repeated, e.count, e.count)
                MutedText("×${e.count}", modifier = Modifier.semantics { contentDescription = repeated })
            }
            MutedText(stringResource(R.string.kube_events_ago, ageSince(e.lastSeen, now)), modifier = Modifier.weight(1f), maxLines = 1)
        }
        Text(
            target,
            style = MaterialTheme.typography.labelMedium,
            fontFamily = FontFamily.Monospace,
            color = colors.muted,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        if (e.note.isNotEmpty()) {
            Text(e.note, style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
    }
}

/**
 * How the stream runs: connecting, live, reconnecting (why), reading the events again,
 * polling, or how it ended; and the core's note when rows were cut. Icon plus text.
 */
@Composable
internal fun KubeEventsStatusLine(state: KubeEventsState, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val status = state.status
    val (icon: ImageVector, color, text) = when {
        !state.streaming && state.error != null ->
            Triple(Icons.Outlined.ErrorOutline, colors.bad, stringResource(R.string.common_stream_failed, state.error))
        !state.streaming -> Triple(Icons.Outlined.SensorsOff, colors.muted, stringResource(R.string.common_stream_stopped))
        status == null -> Triple(Icons.Outlined.Sync, colors.muted, stringResource(R.string.kube_events_connecting))
        status.state == "reconnecting" -> Triple(
            Icons.Outlined.SyncProblem,
            colors.warn,
            if (status.reason.isEmpty()) stringResource(R.string.kube_events_reconnecting)
            else stringResource(R.string.kube_events_reconnecting_reason, status.reason),
        )
        status.state == "relisting" -> Triple(Icons.Outlined.Sync, colors.muted, stringResource(R.string.kube_events_relisting))
        status.state == "polling" -> Triple(Icons.Outlined.Sensors, colors.warn, stringResource(R.string.kube_events_polling))
        else -> Triple(Icons.Outlined.Sensors, colors.ok, stringResource(R.string.common_live))
    }
    // Polite: a stream starting, reconnecting or failing is announced without cutting speech off.
    Column(modifier.semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite }) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(icon, contentDescription = null, tint = color, modifier = Modifier.size(16.dp))
            Text(text, color = color, style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(start = 6.dp))
        }
        status?.note?.takeIf { it.isNotEmpty() && state.streaming }?.let { MutedText(it) }
    }
}
