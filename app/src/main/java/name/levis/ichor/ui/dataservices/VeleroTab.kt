package name.levis.ichor.ui.dataservices

import androidx.compose.animation.animateContentSize
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.VeleroAdhocBackup
import name.levis.ichor.model.VeleroBackup
import name.levis.ichor.model.VeleroLocation
import name.levis.ichor.model.VeleroReason
import name.levis.ichor.model.VeleroSchedule
import name.levis.ichor.model.VeleroStatus
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/**
 * Velero's storage locations, its schedules (problems first) with their last backup, opening to
 * the details, and the backups taken by hand that failed in the last week.
 */
@Composable
fun VeleroTab(status: VeleroStatus) {
    var expanded by rememberSaveable { mutableStateOf(setOf<String>()) }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        items(status.locations, key = { "l:${it.label}" }) { LocationRow(it) }
        if (status.locations.isNotEmpty()) item(key = "locations-end") { HorizontalDivider() }
        if (status.schedules.isEmpty()) item(key = "empty") { EmptyText(stringResource(R.string.velero_empty)) }
        items(status.schedules, key = { "s:${it.label}" }) { s ->
            ScheduleRow(s, open = s.label in expanded, onToggle = { expanded = if (s.label in expanded) expanded - s.label else expanded + s.label })
            HorizontalDivider()
        }
        if (status.adhoc.isNotEmpty()) item(key = "adhoc") {
            SectionTitle(stringResource(R.string.velero_adhoc), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        }
        items(status.adhoc, key = { "a:${it.label}" }) { AdhocRow(it) }
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
private fun ScheduleRow(s: VeleroSchedule, open: Boolean, onToggle: () -> Unit) {
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
        if (open) Details(s)
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
        InfoRow(stringResource(R.string.velero_last_success), ago(s.lastSuccessAt))
        s.lastBackup?.failureReason?.takeIf { it.isNotEmpty() }?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = colors.bad) }
        s.validationErrors.forEach { Text(it, style = MaterialTheme.typography.labelSmall, color = colors.warn) }
    }
}

@Composable
private fun AdhocRow(b: VeleroAdhocBackup) {
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

@Composable
private fun ago(millis: Long): String = timeAgo(millis).ifEmpty { stringResource(R.string.cnpg_never) }
