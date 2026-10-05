package name.levis.ichor.ui.apihealth

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Error
import androidx.compose.material.icons.outlined.Info
import androidx.compose.material.icons.outlined.WarningAmber
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.AuditActorRow
import name.levis.ichor.model.AuditFinding
import name.levis.ichor.model.AuditKind
import name.levis.ichor.model.AuditNodeRead
import name.levis.ichor.model.AuditSeverity
import name.levis.ichor.model.aboutServer
import name.levis.ichor.model.formatMegabytes
import name.levis.ichor.model.formatMs
import name.levis.ichor.model.formatRate
import name.levis.ichor.model.formatSeconds
import name.levis.ichor.model.label
import name.levis.ichor.model.level
import name.levis.ichor.model.target
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors
import kotlin.math.roundToInt

/** What the finding says happens, in the user's words. */
@Composable
fun findingTitle(f: AuditFinding): String = when (f.kind) {
    AuditKind.THROTTLED -> stringResource(R.string.audit_throttled, f.count)
    AuditKind.HOT_CLIENT -> stringResource(R.string.audit_hotClient, f.value.roundToInt(), f.verb, f.resource)
    AuditKind.LIST_LOOP -> stringResource(R.string.audit_listLoop, f.target, formatSeconds(f.value))
    AuditKind.WATCH_CHURN -> stringResource(R.string.audit_watchChurn, formatSeconds(f.value))
    AuditKind.FORBIDDEN -> stringResource(R.string.audit_forbidden, f.count, f.verb, f.target)
    AuditKind.MISSING_API -> stringResource(R.string.audit_missingAPI, f.count, f.resource)
    AuditKind.MISSING_OBJECT -> stringResource(R.string.audit_missingObject, f.target, f.count)
    AuditKind.CONFLICTS -> stringResource(R.string.audit_conflicts, f.verb, f.target, f.count)
    AuditKind.ALREADY_EXISTS -> stringResource(R.string.audit_alreadyExists, f.resource, f.count)
    AuditKind.HOT_OBJECT -> stringResource(R.string.audit_hotObject, f.target, formatSeconds(f.value))
    AuditKind.EVENT_SPAM -> stringResource(R.string.audit_eventSpam, formatRate(f.rate))
    AuditKind.SLOW -> stringResource(R.string.audit_slow, f.verb, f.target, formatMs(f.value))
    AuditKind.SERVER_ERRORS -> stringResource(R.string.audit_serverErrors, f.verb, f.target, f.count, f.code)
    AuditKind.WIDE_ERRORS -> stringResource(R.string.audit_widespreadErrors, f.actors, f.code, f.verb, f.resource)
    AuditKind.WIDE_SLOW -> stringResource(R.string.audit_widespreadSlow, f.actors, formatMs(f.value))
    AuditKind.WIDE_WATCH -> stringResource(R.string.audit_widespreadWatchChurn, f.actors, formatSeconds(f.value))
    AuditKind.STALE_LOG -> stringResource(R.string.audit_staleLog, f.name, localizedDuration(f.value.toLong()))
    AuditKind.UNAUTHORIZED -> stringResource(R.string.audit_unauthorized, f.count)
    else -> f.kind
}

/** What to change; empty for a kind this version does not know. */
@Composable
fun findingFix(kind: String): String {
    val res = when (kind) {
        AuditKind.THROTTLED -> R.string.audit_throttled_fix
        AuditKind.HOT_CLIENT -> R.string.audit_hotClient_fix
        AuditKind.LIST_LOOP -> R.string.audit_listLoop_fix
        AuditKind.WATCH_CHURN -> R.string.audit_watchChurn_fix
        AuditKind.FORBIDDEN -> R.string.audit_forbidden_fix
        AuditKind.MISSING_API -> R.string.audit_missingAPI_fix
        AuditKind.MISSING_OBJECT -> R.string.audit_missingObject_fix
        AuditKind.CONFLICTS -> R.string.audit_conflicts_fix
        AuditKind.ALREADY_EXISTS -> R.string.audit_alreadyExists_fix
        AuditKind.HOT_OBJECT -> R.string.audit_hotObject_fix
        AuditKind.EVENT_SPAM -> R.string.audit_eventSpam_fix
        AuditKind.SLOW -> R.string.audit_slow_fix
        AuditKind.SERVER_ERRORS -> R.string.audit_serverErrors_fix
        AuditKind.WIDE_ERRORS -> R.string.audit_widespreadErrors_fix
        AuditKind.WIDE_SLOW -> R.string.audit_widespreadSlow_fix
        AuditKind.WIDE_WATCH -> R.string.audit_widespreadWatchChurn_fix
        AuditKind.STALE_LOG -> R.string.audit_staleLog_fix
        AuditKind.UNAUTHORIZED -> R.string.audit_unauthorized_fix
        else -> return ""
    }
    return stringResource(res)
}

/** One problem: who, what happens, the objects it touches and what to change. */
@Composable
fun FindingCard(f: AuditFinding) {
    val colors = LocalStatusColors.current
    val (icon, tint) = when (f.level) {
        AuditSeverity.CRITICAL -> Icons.Outlined.Error to colors.bad
        AuditSeverity.WARNING -> Icons.Outlined.WarningAmber to colors.warn
        AuditSeverity.INFO -> Icons.Outlined.Info to colors.muted
    }
    OutlinedCard(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
        Row(Modifier.padding(12.dp), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Icon(icon, contentDescription = null, tint = tint, modifier = Modifier.size(20.dp))
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(3.dp)) {
                Text(
                    if (f.aboutServer) stringResource(R.string.audit_server) else f.actor.label,
                    style = MaterialTheme.typography.labelLarge,
                    fontFamily = if (f.aboutServer) null else FontFamily.Monospace,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                if (!f.aboutServer && f.actor.agent.isNotEmpty() && f.actor.agent != f.actor.name) MutedText(f.actor.agent, maxLines = 1)
                Text(findingTitle(f), style = MaterialTheme.typography.bodyMedium)
                if (f.objects > 1) {
                    MutedText(stringResource(R.string.audit_examples, f.objects, f.examples.joinToString(", ")), maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
                findingFix(f.kind).takeIf { it.isNotEmpty() }?.let { MutedText(it) }
            }
        }
    }
}

/** A busy client: its rate and share, what it mostly does, its errors. */
@Composable
fun ActorRow(row: AuditActorRow) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(row.actor.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            Text(formatRate(row.rate), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
        }
        if (row.actor.agent.isNotEmpty() && row.actor.agent != row.actor.name) MutedText(row.actor.agent, maxLines = 1)
        MutedText(stringResource(R.string.audit_actor_share, (row.share * 100).roundToInt(), row.topVerb, row.topResource), maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (row.errors > 0 || row.throttled > 0) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if (row.throttled > 0) TagBadge(stringResource(R.string.audit_actor_throttled, row.throttled), colors.bad)
                if (row.errors > 0) TagBadge(stringResource(R.string.audit_actor_errors, row.errors), colors.warn)
            }
        }
    }
}

/** What reading one control plane's log took, or why it failed. */
@Composable
fun NodeReadRow(n: AuditNodeRead) {
    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(n.node, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
        if (n.error.isEmpty()) MutedText(stringResource(R.string.audit_node_read, formatMegabytes(n.bytes), n.events))
    }
    if (n.error.isNotEmpty()) InlineError(n.error)
}
