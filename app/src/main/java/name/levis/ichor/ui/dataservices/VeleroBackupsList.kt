package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
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
import kotlinx.coroutines.CancellationException
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.VeleroBackupBlocker
import name.levis.ichor.model.VeleroBackupDetail
import name.levis.ichor.model.VeleroBackups
import name.levis.ichor.model.VeleroSchedule
import name.levis.ichor.model.backupNowBlocker
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText
import name.levis.ichor.util.formatDate
import name.levis.ichor.util.timeAgo

/** "Back up now" for a schedule, disabled with the reason when Velero or the credentials would refuse it. */
@Composable
fun VeleroBackupNowButton(schedule: VeleroSchedule, actions: VeleroActions, onAsk: () -> Unit) {
    val busy by actions.busy.collectAsStateWithLifecycle()
    val denial = rememberKubeDenial(KubeAction.VELERO_BACKUP_NOW, schedule.namespace)
    val blocker = schedule.backupNowBlocker
    val running = VeleroActions.backupKey(schedule) in busy
    OutlinedButton(onClick = onAsk, enabled = blocker == null && denial == null && !running) {
        if (running) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
        else Text(stringResource(R.string.velero_backup_now))
    }
    blocker?.let { Text(blockerText(it), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
    denial?.let { KubeDenialNote(it) }
}

@Composable
private fun blockerText(blocker: VeleroBackupBlocker): String = when (blocker) {
    VeleroBackupBlocker.PAUSED -> stringResource(R.string.velero_blocked_paused)
    VeleroBackupBlocker.INVALID -> stringResource(R.string.velero_blocked_invalid)
    VeleroBackupBlocker.RUNNING -> stringResource(R.string.velero_blocked_running)
}

/**
 * The schedule's backups, loaded when opened and again after each action: phase, when, counts,
 * expiry, and Delete and Restore on the finished ones.
 */
@Composable
fun VeleroBackupsSection(schedule: VeleroSchedule, actions: VeleroActions, onDelete: (VeleroBackupDetail) -> Unit, onRestore: (VeleroBackupDetail) -> Unit) {
    var open by rememberSaveable(schedule.label) { mutableStateOf(false) }
    val changes by actions.changes.collectAsStateWithLifecycle()
    var list by remember(schedule.label) { mutableStateOf<VeleroBackups?>(null) }
    var error by remember(schedule.label) { mutableStateOf<UiText?>(null) }
    LaunchedEffect(open, changes) {
        if (!open) return@LaunchedEffect
        try {
            list = actions.backups(schedule.namespace, schedule.name)
            error = null
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            error = e.uiText()
        }
    }

    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(Modifier.fillMaxWidth().clickable { open = !open }.padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(stringResource(R.string.velero_backups), style = MaterialTheme.typography.labelLarge, modifier = Modifier.weight(1f))
        Icon(if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null, tint = muted)
    }
    if (!open) return
    error?.let { InlineError(it.asString()) }
    val backups = list
    when {
        backups == null && error == null -> CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        backups == null -> Unit
        else -> {
            if (backups.error.isNotEmpty()) InlineError(stringResource(R.string.data_services_unreadable, backups.error))
            if (backups.backups.isEmpty()) Text(stringResource(R.string.velero_backups_empty), style = MaterialTheme.typography.labelSmall, color = muted)
            backups.backups.forEach { VeleroBackupRow(schedule.namespace, it, actions, onDelete, onRestore) }
        }
    }
}

@Composable
private fun VeleroBackupRow(namespace: String, b: VeleroBackupDetail, actions: VeleroActions, onDelete: (VeleroBackupDetail) -> Unit, onRestore: (VeleroBackupDetail) -> Unit) {
    val colors = LocalStatusColors.current
    val busy by actions.busy.collectAsStateWithLifecycle()
    val deleting = b.deleting || VeleroActions.deleteKey(namespace, b.name) in busy
    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(1.dp)) {
            Text(b.name, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                listOfNotNull(
                    b.phase.ifEmpty { null },
                    timeAgo(b.at).ifEmpty { null },
                    counts(b.errors, b.warnings),
                    b.expiresAt.takeIf { it > 0 }?.let { stringResource(R.string.velero_expires, formatDate(it)) },
                    stringResource(R.string.velero_deleting).takeIf { deleting },
                ).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = if (b.phase == "Failed" || b.phase == "PartiallyFailed") colors.warn else MaterialTheme.colorScheme.onSurfaceVariant,
            )
            b.failureReason.takeIf { it.isNotEmpty() }?.let { Text(it, style = MaterialTheme.typography.labelSmall, color = colors.bad) }
            if (b.deleteErrors.isNotEmpty()) {
                Text(stringResource(R.string.velero_delete_errors, b.deleteErrors.joinToString("; ")), style = MaterialTheme.typography.labelSmall, color = colors.bad)
            }
        }
        if (b.deletable && !deleting) {
            TextButton(onClick = { onRestore(b) }) { Text(stringResource(R.string.velero_restore)) }
            TextButton(onClick = { onDelete(b) }) { Text(stringResource(R.string.velero_delete)) }
        }
    }
}

/** "2 errors, 3 warnings", null when both are 0. */
@Composable
private fun counts(errors: Int, warnings: Int): String? = listOfNotNull(
    errors.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.velero_errors, it, it) },
    warnings.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.velero_warnings, it, it) },
).joinToString(", ").ifEmpty { null }
