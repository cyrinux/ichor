package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AutoMode
import androidx.compose.material.icons.outlined.ChevronRight
import androidx.compose.material.icons.outlined.CloudDone
import androidx.compose.material.icons.outlined.CloudSync
import androidx.compose.material.icons.outlined.CloudUpload
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.PhotoCamera
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.LonghornAction
import name.levis.ichor.model.LonghornBackup
import name.levis.ichor.model.LonghornSnapshot
import name.levis.ichor.model.LonghornSnapshotAction
import name.levis.ichor.model.LonghornVolume
import name.levis.ichor.model.longhornActionKey
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.components.upwardScrollStaysInSheet
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.timeAgo

/**
 * How well [v] is protected, on its row: its snapshot count and last backup, tinted when the
 * backup is missing, old or failed. Opens the snapshots sheet.
 */
@Composable
internal fun LonghornProtectionLine(v: LonghornVolume, onClick: () -> Unit) {
    val tint = v.protectionColor() ?: MaterialTheme.colorScheme.primary
    val label = stringResource(R.string.longhorn_snapshots_title)
    Row(
        Modifier.padding(top = 4.dp).clip(RoundedCornerShape(8.dp)).clickable(role = Role.Button, onClickLabel = label, onClick = onClick).padding(vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Icon(Icons.Outlined.PhotoCamera, null, Modifier.size(14.dp), tint = MaterialTheme.colorScheme.primary)
        Text("${v.snapshots.size}", style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
        Spacer(Modifier.size(4.dp))
        Icon(if (v.latestBackupFailed) Icons.Outlined.ErrorOutline else Icons.Outlined.CloudDone, null, Modifier.size(14.dp), tint = tint)
        Text(v.lastBackupText(), style = MaterialTheme.typography.labelSmall, color = tint)
        Icon(Icons.Outlined.ChevronRight, label, Modifier.size(14.dp), tint = MaterialTheme.colorScheme.primary)
    }
}

private val LonghornVolume.latestBackupFailed: Boolean get() = backups.firstOrNull()?.failed == true

/** The last backup failed or is old: the row then shows its protection line. */
internal val LonghornVolume.backupNeedsAttention: Boolean get() = latestBackupFailed || backupStale

/** Warn for a missing or stale backup, bad for a failed one, null when all is well. */
@Composable
private fun LonghornVolume.protectionColor(): Color? {
    val colors = LocalStatusColors.current
    return when {
        latestBackupFailed -> colors.bad
        lastBackupAt == 0L || backupStale -> colors.warn
        else -> null
    }
}

@Composable
private fun LonghornVolume.lastBackupText(): String =
    if (lastBackupAt > 0) stringResource(R.string.longhorn_backup_ago, timeAgo(lastBackupAt)) else stringResource(R.string.longhorn_no_backup)

/**
 * The snapshots and backups of [volume], newest first, with what can be done to them: take a
 * snapshot, back the volume or one snapshot up, delete a snapshot.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun LonghornSnapshotsSheet(volume: LonghornVolume, actions: LonghornActions, onDismiss: () -> Unit) {
    val snapshotBusy by actions.snapshotBusy.collectAsStateWithLifecycle()
    val busy by actions.busy.collectAsStateWithLifecycle()
    val denial = rememberKubeDenial(KubeAction.LONGHORN_SNAPSHOT, volume.namespace)
    val canAct = volume.attached && denial == null
    var deleting by remember { mutableStateOf<LonghornSnapshot?>(null) }

    deleting?.let { s ->
        ConfirmDialog(
            title = stringResource(R.string.longhorn_snapshot_delete_title),
            text = stringResource(R.string.longhorn_snapshot_delete_text, s.name, volume.label) +
                // Whether a VolumeSnapshot keeps it could not be read.
                if (!volume.volumeSnapshotsKnown && s.userCreated) "\n\n" + stringResource(R.string.longhorn_snapshot_delete_csi_unknown) else "",
            confirm = stringResource(R.string.longhorn_snapshot_delete),
            onConfirm = {
                deleting = null
                actions.snapshot(volume, s.name, LonghornSnapshotAction.DELETE)
            },
            onDismiss = { deleting = null },
            destructive = true,
        )
    }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(Modifier.fillMaxWidth().upwardScrollStaysInSheet()) {
            item(key = "head") {
                Column(Modifier.padding(horizontal = 24.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Column {
                        Text(stringResource(R.string.longhorn_snapshots_title), style = MaterialTheme.typography.titleMedium)
                        Text(volume.label, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    Summary(volume)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        val taking = longhornSnapshotKey(volume, "") in snapshotBusy
                        FilledTonalButton(onClick = { actions.snapshot(volume, "", LonghornSnapshotAction.TAKE) }, enabled = canAct && !taking, modifier = Modifier.weight(1f)) {
                            ButtonIcon(Icons.Outlined.PhotoCamera, taking)
                            Text(stringResource(R.string.longhorn_take_snapshot), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                        val backingUp = longhornActionKey(volume.namespace, volume.name) in busy || volume.backingUp
                        OutlinedButton(
                            onClick = { actions.run(longhornActionKey(volume.namespace, volume.name), volume.namespace, volume.name, volume.label, LonghornAction.BACKUP) },
                            enabled = canAct && !backingUp,
                            modifier = Modifier.weight(1f),
                        ) {
                            ButtonIcon(Icons.Outlined.CloudUpload, backingUp)
                            Text(stringResource(R.string.longhorn_action_backup), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                    if (!volume.attached) MutedText(stringResource(R.string.longhorn_snapshot_needs_attached))
                    KubeDenialNote(denial)
                    if (volume.backingUp) {
                        LinearProgressIndicator(progress = { volume.backupProgress / 100f }, modifier = Modifier.fillMaxWidth().height(4.dp), drawStopIndicator = {})
                    }
                }
            }
            item(key = "snapshots") {
                SectionTitle(stringResource(R.string.longhorn_snapshots_label), Modifier.padding(start = 24.dp, end = 24.dp, top = 16.dp))
            }
            if (volume.snapshots.isEmpty()) item(key = "no-snapshots") {
                MutedText(stringResource(R.string.longhorn_no_snapshots), Modifier.padding(horizontal = 24.dp, vertical = 8.dp))
            }
            items(volume.snapshots, key = { "s:${it.name}" }) { s ->
                SnapshotRow(
                    s,
                    busy = longhornSnapshotKey(volume, s.name) in snapshotBusy,
                    enabled = canAct,
                    onBackup = { actions.snapshot(volume, s.name, LonghornSnapshotAction.BACKUP) },
                    onDelete = { deleting = s },
                )
            }
            item(key = "backups") {
                SectionTitle(stringResource(R.string.longhorn_backups_label), Modifier.padding(start = 24.dp, end = 24.dp, top = 16.dp))
            }
            if (volume.backups.isEmpty()) item(key = "no-backups") {
                MutedText(stringResource(R.string.longhorn_no_backups), Modifier.padding(horizontal = 24.dp, vertical = 8.dp))
            }
            items(volume.backups, key = { "b:${it.name}" }) { BackupRow(it) }
            if (volume.backupCount > volume.backups.size) item(key = "more") {
                MutedText(
                    stringResource(R.string.longhorn_backups_more, volume.backups.size.toString(), volume.backupCount.toString()),
                    Modifier.padding(horizontal = 24.dp, vertical = 8.dp),
                )
            }
            item(key = "end") { Spacer(Modifier.height(24.dp)) }
        }
    }
}

@Composable
private fun ButtonIcon(icon: ImageVector, busy: Boolean) {
    if (busy) {
        CircularProgressIndicator(Modifier.size(ButtonDefaults.IconSize), strokeWidth = 2.dp)
    } else {
        Icon(icon, null, Modifier.size(ButtonDefaults.IconSize))
    }
    Spacer(Modifier.size(ButtonDefaults.IconSpacing))
}

/** Three tiles: snapshots, last backup, backups. */
@Composable
private fun Summary(v: LonghornVolume) {
    val colors = LocalStatusColors.current
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        StatTile(
            label = stringResource(R.string.longhorn_snapshots_label),
            value = "${v.snapshots.size}",
            caption = when {
                v.snapshotsRemoving > 0 -> stringResource(R.string.longhorn_snapshots_removing, v.snapshotsRemoving.toString())
                v.snapshots.isNotEmpty() -> stringResource(R.string.longhorn_snapshot_newest, timeAgo(v.snapshots.first().createdAt))
                else -> ""
            },
            modifier = Modifier.weight(1f),
        )
        StatTile(
            label = stringResource(R.string.longhorn_last_backup),
            value = if (v.lastBackupAt > 0) timeAgo(v.lastBackupAt) else stringResource(R.string.longhorn_never),
            caption = when {
                v.latestBackupFailed -> stringResource(R.string.longhorn_backup_failed_latest)
                v.backupStale -> stringResource(R.string.longhorn_backup_stale)
                else -> ""
            },
            accent = v.protectionColor(),
            modifier = Modifier.weight(1.4f),
        )
        StatTile(
            label = stringResource(R.string.longhorn_backups_label),
            value = "${v.backupCount}",
            caption = "",
            accent = colors.warn.takeIf { v.backupCount == 0 },
            modifier = Modifier.weight(1f),
        )
    }
}

@Composable
private fun StatTile(label: String, value: String, caption: String, modifier: Modifier = Modifier, accent: Color? = null) {
    Surface(modifier, shape = RoundedCornerShape(12.dp), color = (accent ?: MaterialTheme.colorScheme.primary).copy(alpha = 0.08f)) {
        Column(Modifier.padding(horizontal = 12.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(value, style = MaterialTheme.typography.titleMedium, color = accent ?: MaterialTheme.colorScheme.onSurface, maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (caption.isNotEmpty()) Text(caption, style = MaterialTheme.typography.labelSmall, color = accent ?: MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2)
        }
    }
}

/** A round tinted badge holding [icon]. */
@Composable
private fun Badge(icon: ImageVector, color: Color) {
    Box(Modifier.size(36.dp).background(color.copy(alpha = 0.14f), CircleShape), contentAlignment = Alignment.Center) {
        Icon(icon, null, Modifier.size(18.dp), tint = color)
    }
}

@Composable
private fun SnapshotRow(s: LonghornSnapshot, busy: Boolean, enabled: Boolean, onBackup: () -> Unit, onDelete: () -> Unit) {
    val colors = LocalStatusColors.current
    Row(Modifier.fillMaxWidth().padding(start = 24.dp, end = 8.dp, top = 6.dp, bottom = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Badge(if (s.userCreated) Icons.Outlined.PhotoCamera else Icons.Outlined.AutoMode, MaterialTheme.colorScheme.primary)
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(timeAgo(s.createdAt).ifEmpty { s.name }, style = MaterialTheme.typography.bodyMedium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                listOfNotNull(s.name, formatBytes(s.size), stringResource(R.string.longhorn_snapshot_system).takeIf { !s.userCreated }).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                fontFamily = FontFamily.Monospace,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if (s.backedUp) {
                    StatusPill(stringResource(R.string.longhorn_snapshot_backed_up), colors.ok)
                } else {
                    StatusPill(stringResource(R.string.longhorn_snapshot_local_only), colors.muted)
                }
                if (!s.ready) StatusPill(stringResource(R.string.longhorn_snapshot_not_ready), colors.warn)
            }
            if (s.volumeSnapshot.isNotEmpty()) {
                StatusPill(stringResource(R.string.longhorn_snapshot_csi, s.volumeSnapshot), MaterialTheme.colorScheme.primary)
            }
            if (s.error.isNotEmpty()) Text(s.error, style = MaterialTheme.typography.labelSmall, color = colors.bad)
        }
        SnapshotMenu(s, busy, enabled, onBackup, onDelete)
    }
    HorizontalDivider(Modifier.padding(start = 72.dp))
}

@Composable
private fun SnapshotMenu(s: LonghornSnapshot, busy: Boolean, enabled: Boolean, onBackup: () -> Unit, onDelete: () -> Unit) {
    if (busy) {
        Box(Modifier.size(48.dp), contentAlignment = Alignment.Center) { CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp) }
        return
    }
    var open by remember { mutableStateOf(false) }
    Box {
        TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { open = true }, enabled = enabled)
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            if (!s.backedUp && s.ready) {
                DropdownMenuItem(
                    text = { Text(stringResource(R.string.longhorn_snapshot_backup)) },
                    leadingIcon = { Icon(Icons.Outlined.CloudUpload, null) },
                    onClick = { open = false; onBackup() },
                )
            }
            // A VolumeSnapshot's data: deleting it here would leave that one unrestorable.
            val kept = s.volumeSnapshot.isNotEmpty()
            DropdownMenuItem(
                text = {
                    Column {
                        Text(stringResource(R.string.longhorn_snapshot_delete), color = if (kept) Color.Unspecified else LocalStatusColors.current.bad)
                        if (kept) MutedText(stringResource(R.string.longhorn_snapshot_delete_csi, s.volumeSnapshot))
                    }
                },
                leadingIcon = { Icon(Icons.Outlined.Delete, null, tint = if (kept) LocalContentColor.current else LocalStatusColors.current.bad) },
                enabled = !kept,
                onClick = { open = false; onDelete() },
            )
        }
    }
}

@Composable
private fun BackupRow(b: LonghornBackup) {
    val colors = LocalStatusColors.current
    val (icon, color, state) = when {
        b.failed -> Triple(Icons.Outlined.ErrorOutline, colors.bad, stringResource(R.string.longhorn_backup_state_failed))
        b.completed -> Triple(Icons.Outlined.CloudDone, colors.ok, stringResource(R.string.longhorn_backup_state_completed))
        else -> Triple(Icons.Outlined.CloudSync, MaterialTheme.colorScheme.primary, stringResource(R.string.longhorn_backup_state_running))
    }
    Row(Modifier.fillMaxWidth().padding(horizontal = 24.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        Badge(icon, color)
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(timeAgo(b.createdAt).ifEmpty { b.name }, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                StatusPill(state, color)
            }
            Text(
                listOfNotNull(formatBytes(b.size).takeIf { b.size > 0 }, b.snapshot.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.longhorn_from_snapshot, it) }).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                fontFamily = FontFamily.Monospace,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (!b.completed && !b.failed) {
                LinearProgressIndicator(progress = { b.progress / 100f }, modifier = Modifier.fillMaxWidth().height(4.dp), drawStopIndicator = {})
            }
            if (b.error.isNotEmpty()) Text(b.error, style = MaterialTheme.typography.labelSmall, color = colors.bad)
        }
    }
}
