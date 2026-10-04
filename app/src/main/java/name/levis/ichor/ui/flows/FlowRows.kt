package name.levis.ichor.ui.flows

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DropGroup
import name.levis.ichor.model.HUBBLE_AUDIT
import name.levis.ichor.model.HUBBLE_DROPPED
import name.levis.ichor.model.HUBBLE_EGRESS
import name.levis.ichor.model.HUBBLE_FORWARDED
import name.levis.ichor.model.HUBBLE_ERROR
import name.levis.ichor.model.HUBBLE_INGRESS
import name.levis.ichor.model.HubbleFlow
import name.levis.ichor.model.humanizeReason
import name.levis.ichor.model.label
import name.levis.ichor.model.portLabel
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.agoLabel
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors
import java.text.DateFormat
import java.util.Date

/** Dropped red, audited (would be dropped) amber, forwarded green, anything else muted. */
@Composable
fun verdictColor(verdict: String): Color {
    val colors = LocalStatusColors.current
    return when (verdict) {
        HUBBLE_DROPPED, HUBBLE_ERROR -> colors.bad
        HUBBLE_AUDIT -> colors.warn
        HUBBLE_FORWARDED -> colors.ok
        else -> colors.muted
    }
}

/** A drop reason in plain words. */
@Composable
fun reasonText(reason: String): String = when (reason) {
    "POLICY_DENIED" -> stringResource(R.string.flows_reason_policy_denied)
    "POLICY_DENY" -> stringResource(R.string.flows_reason_policy_deny)
    "AUTH_REQUIRED" -> stringResource(R.string.flows_reason_auth_required)
    "STALE_OR_UNROUTABLE_IP" -> stringResource(R.string.flows_reason_stale_ip)
    "CT_MAP_INSERTION_FAILED" -> stringResource(R.string.flows_reason_ct_full)
    else -> humanizeReason(reason)
}

@Composable
fun directionText(direction: String): String? = when (direction) {
    HUBBLE_INGRESS -> stringResource(R.string.netpol_ingress)
    HUBBLE_EGRESS -> stringResource(R.string.netpol_egress)
    else -> null
}

/** "src → dst", the port, direction and verdict, the reason, how often and when last. */
@Composable
fun DropCard(group: DropGroup, onClick: () -> Unit) {
    val color = verdictColor(group.verdict)
    OutlinedCard(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp).clickable(onClick = onClick)) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(
                "${group.source.label} → ${group.destination.label}",
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                TagBadge(
                    stringResource(if (group.verdict == HUBBLE_AUDIT) R.string.flows_would_drop else R.string.flows_verdict_dropped),
                    color,
                )
                portLabel(group.protocol, group.port).takeIf { it.isNotEmpty() }?.let { TagBadge(it, MaterialTheme.colorScheme.primary, mono = true) }
                directionText(group.direction)?.let { TagBadge(it, MaterialTheme.colorScheme.outline) }
                Text(
                    stringResource(R.string.flows_count, group.count),
                    style = MaterialTheme.typography.labelLarge,
                    color = color,
                    modifier = Modifier.weight(1f),
                    textAlign = TextAlign.End,
                )
            }
            if (group.reason.isNotEmpty()) Text(reasonText(group.reason), style = MaterialTheme.typography.bodySmall)
            MutedText(
                listOf(
                    stringResource(R.string.flows_last_seen, agoLabel(System.currentTimeMillis() - group.lastSeen)),
                    group.nodes.joinToString(", "),
                ).filter { it.isNotEmpty() }.joinToString("  ·  "),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/** One flow, compact: time, verdict, "src → dst", port and flags, and its L7 line. */
@Composable
fun FlowLine(flow: HubbleFlow) {
    val color = verdictColor(flow.verdict)
    val time = DateFormat.getTimeInstance(DateFormat.MEDIUM).format(Date(flow.time))
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(time, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
            TagBadge(flow.verdict, color, mono = true)
            Text(
                listOf(portLabel(flow.protocol, flow.port), flow.flags).filter { it.isNotEmpty() }.joinToString(" "),
                style = MaterialTheme.typography.labelSmall,
                fontFamily = FontFamily.Monospace,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        Text(
            "${flow.source.label} → ${flow.destination.label}",
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            color = if (flow.verdict == HUBBLE_FORWARDED) MaterialTheme.colorScheme.onSurface else color,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        if (flow.l7.isNotEmpty()) {
            Text(flow.l7, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.tertiary, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}
