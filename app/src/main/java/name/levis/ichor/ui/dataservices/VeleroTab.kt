package name.levis.ichor.ui.dataservices

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.VeleroAdhocBackup
import name.levis.ichor.model.VeleroBackup
import name.levis.ichor.model.VeleroLocation
import name.levis.ichor.model.VeleroReason
import name.levis.ichor.model.VeleroSchedule
import name.levis.ichor.model.VeleroStatus
import name.levis.ichor.ui.components.CardListPadding
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.cardGap
import name.levis.ichor.ui.components.cardGroupTitle
import name.levis.ichor.ui.components.cardRows
import name.levis.ichor.ui.components.color
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/**
 * Velero's storage locations, its schedules (problems first) with their last backup, opening to
 * the details (back up now, the schedule's backups), and the backups taken by hand that failed
 * in the last week.
 */
@Composable
fun VeleroTab(status: VeleroStatus, actions: VeleroActions) {
    var expanded by rememberSaveable { mutableStateOf(setOf<String>()) }
    var askNow by remember { mutableStateOf<VeleroSchedule?>(null) }
    var askDelete by remember { mutableStateOf<Pair<String, String>?>(null) }
    var askRestore by remember { mutableStateOf<Pair<String, String>?>(null) }
    askNow?.let { s -> VeleroBackupNowDialog(s, onConfirm = { actions.backupNow(s); askNow = null }, onDismiss = { askNow = null }) }
    askDelete?.let { (ns, backup) ->
        VeleroDeleteBackupDialog(ns, backup, onConfirm = { actions.delete(ns, backup); askDelete = null }, onDismiss = { askDelete = null })
    }
    askRestore?.let { (ns, backup) -> VeleroRestoreDialog(ns, backup, actions, onDismiss = { askRestore = null }) }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = CardListPadding) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        cardRows(status.locations, key = { "l:${it.label}" }) { LocationRow(it) }
        if (status.locations.isNotEmpty()) cardGap("locations-end")
        if (status.schedules.isEmpty()) item(key = "empty") { EmptyText(stringResource(R.string.velero_empty)) }
        cardRows(status.schedules, key = { "s:${it.label}" }) { s ->
            ScheduleRow(s, open = s.label in expanded, onToggle = { expanded = if (s.label in expanded) expanded - s.label else expanded + s.label }) {
                Details(s)
                Column(Modifier.padding(start = 22.dp, top = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                    VeleroBackupNowButton(s, actions, onAsk = { askNow = s })
                    VeleroBackupsSection(s, actions, onDelete = { askDelete = s.namespace to it.name }, onRestore = { askRestore = s.namespace to it.name })
                }
            }
        }
        if (status.adhoc.isNotEmpty()) cardGroupTitle("adhoc", R.string.velero_adhoc)
        cardRows(status.adhoc, key = { "a:${it.label}" }) { b -> AdhocRow(b, actions, onDelete = { askDelete = b.namespace to b.name }, onRestore = { askRestore = b.namespace to b.name }) }
    }
}

@Composable
private fun LocationRow(loc: VeleroLocation) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(
                    listOfNotNull(stringResource(R.string.velero_location, loc.name), stringResource(R.string.velero_default).takeIf { loc.default }).joinToString(" · "),
                    style = MaterialTheme.typography.bodyMedium,
                )
                Text(
                    listOf(loc.provider, loc.bucket).filter { it.isNotEmpty() }.joinToString(" · "),
                    style = MaterialTheme.typography.labelSmall,
                    fontFamily = FontFamily.Monospace,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            when (loc.phase) {
                "Available" -> StatusPill(stringResource(R.string.longhorn_backup_target_available), colors.ok)
                "Unavailable" -> StatusPill(stringResource(R.string.longhorn_backup_target_unavailable), colors.bad)
            }
        }
        if (loc.phase == "Unavailable" && loc.message.isNotEmpty()) {
            Text(loc.message, style = MaterialTheme.typography.labelSmall, color = colors.bad, maxLines = 3, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
private fun ScheduleRow(s: VeleroSchedule, open: Boolean, onToggle: () -> Unit, details: @Composable () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().clickable(onClick = onToggle).animateContentSize().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(s.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Column(Modifier.weight(1f)) {
                Text(s.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val line = listOfNotNull(
                    stringResource(R.string.velero_paused).takeIf { s.paused },
                    stringResource(R.string.velero_in_progress).takeIf { s.inProgress },
                ) + s.reasonList.map { reasonText(it) }
                Text(
                    (listOf(lastBackupText(s.lastBackup)) + line).joinToString(" · "),
                    style = MaterialTheme.typography.labelSmall,
                    color = s.serviceHealth.color().takeIf { s.serviceHealth.needsAttention } ?: muted,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Icon(if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null, tint = muted)
        }
        if (open) details()
    }
}

@Composable
private fun Details(s: VeleroSchedule) {
    val colors = LocalStatusColors.current
    Column(Modifier.padding(start = 22.dp, top = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        InfoRow(stringResource(R.string.velero_schedule), s.schedule, mono = true)
        if (s.storageLocation.isNotEmpty()) InfoRow(stringResource(R.string.velero_storage_location), s.storageLocation, mono = true)
        InfoRow(
            stringResource(R.string.velero_namespaces),
            s.includedNamespaces.filter { it != "*" }.ifEmpty { null }?.joinToString(", ") ?: stringResource(R.string.velero_all_namespaces),
        )
        s.lastBackup?.let { b ->
            InfoRow(stringResource(R.string.cnpg_last_backup), b.name, mono = true)
            InfoRow(stringResource(R.string.cnpg_phase), listOfNotNull(b.phase, countsText(b.errors, b.warnings)).joinToString(" · "))
        }
        InfoRow(stringResource(R.string.velero_last_success), agoOrNever(s.lastSuccessAt))
        s.lastBackup?.failureReason?.takeIf { it.isNotEmpty() }?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = colors.bad) }
        s.validationErrors.forEach { Text(it, style = MaterialTheme.typography.labelSmall, color = colors.warn) }
    }
}

@Composable
private fun AdhocRow(b: VeleroAdhocBackup, actions: VeleroActions, onDelete: () -> Unit, onRestore: () -> Unit) {
    val busy by actions.busy.collectAsStateWithLifecycle()
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        HealthDot(b.serviceHealth)
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f)) {
            Text(b.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                listOfNotNull(b.phase, timeAgo(b.completedAt.takeIf { it > 0 } ?: b.startedAt).ifEmpty { null }, countsText(b.errors, b.warnings), b.failureReason.ifEmpty { null })
                    .joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = b.serviceHealth.color(),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (VeleroActions.deleteKey(b.namespace, b.name) in busy) Text(stringResource(R.string.velero_deleting), style = MaterialTheme.typography.labelSmall)
        else {
            // Only a partially failed backup can be a restore source; the others failed for good.
            if (b.phase == "PartiallyFailed") IchorTextButton(onClick = onRestore) { Text(stringResource(R.string.velero_restore)) }
            IchorTextButton(onClick = onDelete) { Text(stringResource(R.string.velero_delete)) }
        }
    }
}

/** "Completed 3 hours ago", or "no backup yet". */
@Composable
private fun lastBackupText(b: VeleroBackup?): String {
    if (b == null) return stringResource(R.string.velero_no_backup)
    val at = b.completedAt.takeIf { it > 0 } ?: b.startedAt
    return listOf(b.phase, timeAgo(at)).filter { it.isNotEmpty() }.joinToString(" ")
}

/** "2 errors, 3 warnings", null when both are 0. */
@Composable
private fun countsText(errors: Int, warnings: Int): String? = listOfNotNull(
    errors.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.velero_errors, it, it) },
    warnings.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.velero_warnings, it, it) },
).joinToString(", ").ifEmpty { null }

@Composable
private fun reasonText(reason: VeleroReason): String = when (reason) {
    VeleroReason.FAILED -> stringResource(R.string.velero_reason_failed)
    VeleroReason.LOCATION -> stringResource(R.string.velero_reason_location)
    VeleroReason.PARTIALLY_FAILED -> stringResource(R.string.velero_reason_partially_failed)
    VeleroReason.STALE -> stringResource(R.string.velero_reason_stale)
    VeleroReason.INVALID -> stringResource(R.string.velero_reason_invalid)
}
