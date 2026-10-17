package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.model.LonghornAction
import name.levis.ichor.model.LonghornBackupTarget
import name.levis.ichor.model.LonghornNode
import name.levis.ichor.model.LonghornVolume
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.actions
import name.levis.ichor.model.diskPathLabel
import name.levis.ichor.model.engineImageTag
import name.levis.ichor.ui.components.MeterRow
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/**
 * A volume in two lines: its state, replicas and size, then why a replica cannot be placed when
 * Longhorn says. Salvage, attach or detach and the snapshots sheet are in its ⋮ menu with the
 * other actions, each confirmed by its own dialog.
 */
@Composable
internal fun VolumeRow(
    v: LonghornVolume,
    busy: Boolean,
    defaultEngineVersion: String,
    onAttachment: (Boolean) -> Unit,
    onSalvage: () -> Unit,
    onEngine: () -> Unit,
    onSnapshots: () -> Unit,
    onAction: (LonghornAction) -> Unit,
) {
    val colors = LocalStatusColors.current
    val subtitle = listOfNotNull(
        stateLabel(v.state),
        robustnessLabel(v.robustness).takeIf { v.robustness.isNotEmpty() && v.robustness != "unknown" || v.serviceHealth == ServiceHealth.CRITICAL },
        stringResource(R.string.longhorn_replicas, v.replicasHealthy, v.replicasDesired),
        v.rebuilding.takeIf { it > 0 }?.let { stringResource(R.string.longhorn_rebuilding, it) },
        formatBytes(v.actualSize) + " / " + formatBytes(v.size),
    ).joinToString(" · ")
    ResourceRow(
        v.serviceHealth,
        v.label,
        Modifier.padding(start = 16.dp, end = 4.dp),
        subtitle = subtitle,
        trailing = { LonghornActionMenu(v.actions, v.namespace, busy, onAction, volumeExtras(v, onAttachment, onSalvage, onEngine, onSnapshots)) },
    ) {
        if (v.scheduleError.isNotEmpty()) MutedText(stringResource(R.string.longhorn_schedule_error, v.scheduleError))
        EngineLine(v, defaultEngineVersion)
        if (v.tooManySnapshots) {
            Text(stringResource(R.string.longhorn_too_many_snapshots), style = MaterialTheme.typography.labelSmall, color = colors.warn)
        }
        // Only a failed or old backup earns a line; the sheet, from the menu, has the rest.
        if (v.backupNeedsAttention) LonghornProtectionLine(v, onSnapshots)
        // 0 is also what an unread engine gives: the status line already counts the rebuilds.
        if (v.rebuilding > 0 && v.rebuildProgress > 0) OperationProgress(stringResource(R.string.longhorn_rebuild_progress, v.rebuildProgress), v.rebuildProgress)
        if (v.backingUp) OperationProgress(stringResource(R.string.longhorn_backup_progress, v.backupProgress), v.backupProgress)
        if (v.restoring) OperationProgress(stringResource(R.string.longhorn_restore_progress, v.restoreProgress), v.restoreProgress)
    }
}

/**
 * "Engine upgrading" while Longhorn swaps it, else "Engine v1.12.0, v1.13.0 available" when it
 * lags the default; nothing on the default engine.
 */
@Composable
private fun EngineLine(v: LonghornVolume, defaultEngineVersion: String) {
    val colors = LocalStatusColors.current
    when {
        v.engineUpgrading -> Text(stringResource(R.string.longhorn_engine_upgrading), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
        v.engineOutdated && defaultEngineVersion.isNotEmpty() -> {
            val current = v.engineVersion.ifEmpty { engineImageTag(v.engineCurrentImage) }.ifEmpty { "—" }
            Text(stringResource(R.string.longhorn_engine_hint, current, defaultEngineVersion), style = MaterialTheme.typography.labelSmall, color = colors.warn)
        }
    }
}

/** The menu items a volume has beyond its [LonghornAction]s: salvage, attach or detach, engine upgrade, snapshots. */
@Composable
private fun volumeExtras(v: LonghornVolume, onAttachment: (Boolean) -> Unit, onSalvage: () -> Unit, onEngine: () -> Unit, onSnapshots: () -> Unit): List<LonghornMenuExtra> {
    val salvage = stringResource(R.string.longhorn_salvage)
    val attach = stringResource(R.string.longhorn_attach)
    val detach = stringResource(R.string.longhorn_detach)
    val engine = stringResource(R.string.longhorn_engine_upgrade)
    val snapshots = stringResource(R.string.longhorn_snapshots_title)
    return listOfNotNull(
        LonghornMenuExtra(salvage, KubeAction.LONGHORN_SALVAGE, onSalvage).takeIf { v.salvageable },
        v.attachmentChoice?.let { a -> LonghornMenuExtra(if (a) attach else detach, KubeAction.LONGHORN_ATTACHMENT) { onAttachment(a) } },
        LonghornMenuExtra(engine, KubeAction.LONGHORN_ENGINE_UPGRADE, onEngine).takeIf { v.engineUpgradable },
        LonghornMenuExtra(snapshots, null, onSnapshots),
    )
}

/**
 * "Rebuilding 42%" over a thin bar in the primary colour: progress, not a fill level, so not
 * a [MeterRow] (whose colour turns red as it fills).
 */
@Composable
private fun OperationProgress(label: String, percent: Int) {
    Column(Modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
        LinearProgressIndicator(
            progress = { percent / 100f },
            modifier = Modifier.fillMaxWidth().height(4.dp),
            trackColor = MaterialTheme.colorScheme.surfaceVariant,
            drawStopIndicator = {},
        )
    }
}

/** A backup target: its URL, its availability as the dot; when unavailable and Garage runs, a tap opens Garage. */
@Composable
internal fun BackupTargetRow(target: LonghornBackupTarget, garageDetected: Boolean, onGarage: () -> Unit) {
    val colors = LocalStatusColors.current
    val linkToGarage = !target.available && garageDetected
    ResourceRow(
        if (target.available) ServiceHealth.OK else ServiceHealth.CRITICAL,
        stringResource(R.string.longhorn_backup_target),
        Modifier.padding(horizontal = 16.dp),
        subtitle = target.url,
        mono = false,
        onClick = onGarage.takeIf { linkToGarage },
    ) {
        if (!target.available && target.message.isNotEmpty()) {
            Text(target.message, style = MaterialTheme.typography.labelSmall, color = colors.bad)
        }
        if (linkToGarage) {
            Text(stringResource(R.string.longhorn_backup_target_garage), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
        }
    }
}

/**
 * A node: its name, a replicas [Tag] (a warn one when scheduling is off or replicas are being
 * evicted) and one meter per disk. [showPath] names each disk's mount when the nodes differ.
 */
@Composable
internal fun LonghornNodeRow(node: LonghornNode, busy: Boolean, showPath: Boolean, onAction: (LonghornAction) -> Unit) {
    val colors = LocalStatusColors.current
    val scheduled = node.disks.sumOf { it.scheduled }
    val maximum = node.disks.sumOf { it.maximum }
    ResourceRow(
        if (node.ready) ServiceHealth.OK else ServiceHealth.CRITICAL,
        node.name,
        Modifier.padding(start = 16.dp, end = 4.dp),
        subtitle = stringResource(R.string.longhorn_disk_scheduled, formatBytes(scheduled), formatBytes(maximum)).takeIf { maximum > 0 },
        trailing = {
            Row(horizontalArrangement = Arrangement.spacedBy(4.dp), verticalAlignment = Alignment.CenterVertically) {
                when {
                    !node.ready -> Tag(stringResource(R.string.longhorn_node_not_ready), colors.bad)
                    node.evictionRequested -> Tag(pluralStringResource(R.plurals.longhorn_node_evicting, node.replicas, node.replicas), colors.warn)
                    !node.allowScheduling -> Tag(stringResource(R.string.longhorn_node_scheduling_off), colors.warn)
                    !node.schedulable -> Tag(stringResource(R.string.longhorn_node_unschedulable), colors.warn)
                }
                if (!node.evictionRequested) Tag(pluralStringResource(R.plurals.longhorn_node_replicas, node.replicas, node.replicas))
            }
            LonghornActionMenu(node.actions, node.namespace, busy, onAction)
        },
    ) {
        node.disks.forEach { d ->
            if (d.maximum > 0) {
                val fraction = (d.scheduled.toFloat() / d.maximum).coerceIn(0f, 1f)
                MeterRow(diskPathLabel(d.path).ifEmpty { "—" }, fraction, "${(fraction * 100).toInt()}%", Modifier.padding(top = 2.dp, end = 12.dp))
            }
            val extra = listOfNotNull(
                d.path.takeIf { showPath && it.isNotEmpty() },
                stringResource(R.string.longhorn_node_unschedulable).takeIf { !d.schedulable },
            )
            if (extra.isNotEmpty()) {
                Text(extra.joinToString(" · "), style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
    }
}

@Composable
private fun stateLabel(state: String): String = when (state) {
    "attached" -> stringResource(R.string.longhorn_state_attached)
    "detached" -> stringResource(R.string.longhorn_state_detached)
    "attaching" -> stringResource(R.string.longhorn_state_attaching)
    "detaching" -> stringResource(R.string.longhorn_state_detaching)
    "creating" -> stringResource(R.string.longhorn_state_creating)
    "deleting" -> stringResource(R.string.longhorn_state_deleting)
    else -> state
}

@Composable
private fun robustnessLabel(robustness: String): String = when (robustness) {
    "healthy" -> stringResource(R.string.longhorn_robustness_healthy)
    "degraded" -> stringResource(R.string.longhorn_robustness_degraded)
    "faulted" -> stringResource(R.string.longhorn_robustness_faulted)
    else -> stringResource(R.string.longhorn_robustness_unknown)
}
