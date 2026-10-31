package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material.icons.outlined.Refresh
import name.levis.ichor.ui.design.IchorAlertDialog
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import name.levis.ichor.ui.design.IchorFilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import name.levis.ichor.ui.design.IchorIconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorSwitch
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.material3.rememberModalBottomSheetState
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
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.GarageLayout
import name.levis.ichor.model.GarageLayoutNode
import name.levis.ichor.model.GarageLayoutRole
import name.levis.ichor.model.GarageLayoutStatus
import name.levis.ichor.model.GarageLayoutVersion
import name.levis.ichor.model.GarageStagedChange
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MeterRow
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatTile
import name.levis.ichor.ui.components.StatTileRow
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/**
 * A Garage cluster's layout: its version, partition size and usable capacity as tiles, the
 * staged changes with what applying them would move (apply with the new version typed, or
 * revert), the roles with how much of each capacity the layout uses, the nodes without a
 * role (they can join), and the version history. When the garage-operator declares the
 * layout ([GarageLayoutState.managed]) it is shown, not edited from here.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun GarageLayoutSheet(state: GarageLayoutState, actions: GarageLayoutActions) {
    var confirmApply by remember { mutableStateOf(false) }
    var confirmRevert by remember { mutableStateOf(false) }
    var confirmRemove by remember { mutableStateOf<GarageLayoutNode?>(null) }
    var redundancy by remember { mutableStateOf(false) }
    var confirmSkip by remember { mutableStateOf(false) }
    val layout = state.layout

    if (confirmApply && layout != null && layout.hasStaged) {
        ApplyConfirmDialog(layout, onConfirm = {
            confirmApply = false
            actions.apply(layout.nextVersion)
        }, onDismiss = { confirmApply = false })
    }
    if (confirmRevert) {
        ConfirmDialog(
            title = stringResource(R.string.garage_layout_revert_title),
            text = stringResource(R.string.garage_layout_revert_text),
            confirm = stringResource(R.string.garage_layout_revert),
            onConfirm = {
                confirmRevert = false
                actions.revert()
            },
            onDismiss = { confirmRevert = false },
        )
    }
    confirmRemove?.let { node ->
        RemoveConfirmDialog(node, onConfirm = {
            confirmRemove = null
            actions.stageRemoval(state.instance, node.id, node.label)
        }, onDismiss = { confirmRemove = null })
    }
    if (confirmSkip && layout != null) {
        SkipDeadNodesDialog(state, layout, onConfirm = { allowMissingData ->
            confirmSkip = false
            actions.skipDeadNodes(layout.version, allowMissingData)
        }, onDismiss = { confirmSkip = false })
    }
    if (redundancy && layout != null) {
        ZoneRedundancyDialog(layout, onPick = {
            redundancy = false
            actions.setZoneRedundancy(it)
        }, onDismiss = { redundancy = false })
    }

    ModalBottomSheet(onDismissRequest = actions::close, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            item { Header(state, actions::reload) }
            if (state.loading || state.busy) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
            state.error?.let { error ->
                item {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        InlineError(error.asString())
                        if (layout == null) IchorOutlinedButton(onClick = actions::reload) { Text(stringResource(R.string.common_retry)) }
                    }
                }
            }
            if (layout == null) return@LazyColumn
            val editable = !state.managed
            item { Summary(layout, onRedundancy = { redundancy = true }.takeIf { editable && !state.busy }) }
            if (state.managed) item { InfoNotice(stringResource(R.string.garage_layout_managed)) }
            if (layout.hasStaged) {
                item { SectionTitle(stringResource(R.string.garage_layout_staged_title, layout.nextVersion)) }
                if (layout.stagedZoneRedundancy.isNotEmpty()) {
                    item { InfoRow(stringResource(R.string.garage_layout_zone_redundancy), redundancyLabel(layout.stagedZoneRedundancy)) }
                }
                items(layout.staged, key = { "staged|${it.id}" }) { StagedRow(it) }
                item { Preview(layout) }
                item {
                    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End), verticalAlignment = Alignment.CenterVertically) {
                        if (state.busy) CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
                        IchorOutlinedButton(onClick = { confirmRevert = true }, enabled = editable && !state.busy) { Text(stringResource(R.string.garage_layout_revert)) }
                        IchorButton(onClick = { confirmApply = true }, enabled = editable && !state.busy && layout.preview?.ok != false) {
                            Text(stringResource(R.string.garage_layout_apply_version, layout.nextVersion))
                        }
                    }
                }
            }
            item { SectionTitle(stringResource(R.string.garage_layout_roles)) }
            if (layout.members.isEmpty()) item { InfoNotice(stringResource(R.string.garage_layout_no_roles)) }
            layout.members.forEachIndexed { i, node ->
                item(key = "member|${node.id}") {
                    if (i > 0) HorizontalDivider()
                    MemberRow(
                        node,
                        onEdit = { actions.editRole(state.instance, node.id) }.takeIf { editable && !state.busy },
                        onRemove = { confirmRemove = node }.takeIf { editable && !state.busy && node.stagedKind != GarageStagedChange.REMOVE },
                    )
                }
            }
            if (layout.candidates.isNotEmpty()) {
                item { SectionTitle(stringResource(R.string.garage_layout_candidates)) }
                layout.candidates.forEachIndexed { i, node ->
                    item(key = "candidate|${node.id}") {
                        if (i > 0) HorizontalDivider()
                        CandidateRow(node, onAdd = { actions.editRole(state.instance, node.id) }.takeIf { editable && !state.busy })
                    }
                }
            }
            item { History(layout, onSkip = { confirmSkip = true }.takeIf { editable && !state.busy && layout.stuck }) }
        }
    }
}

@Composable
private fun Header(state: GarageLayoutState, onReload: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(stringResource(R.string.garage_layout_title), style = MaterialTheme.typography.titleLarge)
            Text(state.instance.label, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        IchorIconButton(onClick = onReload, enabled = !state.loading) { Icon(Icons.Outlined.Refresh, stringResource(R.string.data_services_refresh)) }
    }
}

/** "maximum" or "2 zones". */
@Composable
private fun redundancyLabel(value: String): String {
    val zones = value.toIntOrNull()
    return if (zones != null) pluralStringResource(R.plurals.garage_layout_zones, zones, zones) else stringResource(R.string.garage_layout_redundancy_maximum)
}

/** Capacity or gateway, then the zone and tags: "450 GiB · zone-1 · demo-worker-1". */
@Composable
private fun roleLabel(role: GarageLayoutRole): String = listOf(
    if (role.gateway) stringResource(R.string.garage_operator_gateway) else formatBytes(role.capacity),
    stringResource(R.string.garage_operator_zone, role.zone),
).plus(role.tags).joinToString(" · ")

@Composable
private fun Summary(layout: GarageLayout, onRedundancy: (() -> Unit)?) {
    val usable = layout.members.sumOf { it.role!!.usableCapacity }
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        StatTileRow(
            Modifier.fillMaxWidth().padding(vertical = 4.dp),
            { m -> StatTile(null, "v${layout.version}", stringResource(R.string.garage_layout_version), m) },
            { m -> StatTile(null, if (layout.partitionSize > 0) formatBytes(layout.partitionSize) else "—", stringResource(R.string.garage_layout_partition_size), m) },
            { m -> StatTile(null, if (usable > 0) formatBytes(usable) else "—", stringResource(R.string.garage_layout_usable), m) },
        )
        InfoRow(stringResource(R.string.garage_storage_nodes), "${layout.storageNodes}")
        if (layout.gatewayNodes > 0) InfoRow(stringResource(R.string.garage_layout_gateways), "${layout.gatewayNodes}")
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.weight(1f)) { InfoRow(stringResource(R.string.garage_layout_zone_redundancy), redundancyLabel(layout.zoneRedundancy)) }
            if (onRedundancy != null) IchorTextButton(onClick = onRedundancy) { Text(stringResource(R.string.garage_layout_change)) }
        }
    }
}

/** A staged change: who joins (ok), changes (warn) or leaves (bad), and the role after it. */
@Composable
private fun StagedRow(node: GarageLayoutNode) {
    val colors = LocalStatusColors.current
    val (health, tag, color) = when (node.stagedKind) {
        GarageStagedChange.ADD -> Triple(ServiceHealth.OK, R.string.garage_layout_staged_add, colors.ok)
        GarageStagedChange.REMOVE -> Triple(ServiceHealth.CRITICAL, R.string.garage_layout_staged_remove, colors.bad)
        else -> Triple(ServiceHealth.WARNING, R.string.garage_layout_staged_change, colors.warn)
    }
    val subtitle = when (node.stagedKind) {
        GarageStagedChange.REMOVE -> node.role?.let { stringResource(R.string.garage_layout_was, roleLabel(it)) }
        GarageStagedChange.CHANGE -> listOfNotNull(node.stagedRole?.let { roleLabel(it) }, node.role?.let { stringResource(R.string.garage_layout_was, roleLabel(it)) }).joinToString(" · ")
        else -> node.stagedRole?.let { roleLabel(it) }
    }
    ResourceRow(health, node.label, subtitle = subtitle, trailing = { Tag(stringResource(tag), color) })
}

/** What applying would do, in Garage's own words, or why it cannot be computed. */
@Composable
private fun Preview(layout: GarageLayout) {
    val preview = layout.preview ?: return
    val colors = LocalStatusColors.current
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        if (preview.error.isNotEmpty()) {
            InlineError(stringResource(R.string.garage_layout_preview_error, preview.error))
            return@Column
        }
        if (preview.movedPartitions >= 0) {
            InfoRow(stringResource(R.string.garage_layout_moved_partitions), "%,d".format(preview.movedPartitions))
        }
        if (preview.usableCapacity > 0) InfoRow(stringResource(R.string.garage_layout_usable_after), formatBytes(preview.usableCapacity))
        if (preview.lowUsableCapacity) {
            Text(stringResource(R.string.garage_layout_low_usable), style = MaterialTheme.typography.bodySmall, color = colors.warn)
        }
        if (preview.lowPartitionSize) {
            Text(stringResource(R.string.garage_layout_low_partition), style = MaterialTheme.typography.bodySmall, color = colors.warn)
        }
        preview.messages.forEach { line ->
            Text(line, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

/** A node with a role: its state, the role, the share of its capacity the layout uses, and its menu. */
@Composable
private fun MemberRow(node: GarageLayoutNode, onEdit: (() -> Unit)?, onRemove: (() -> Unit)?) {
    val role = node.role ?: return
    val colors = LocalStatusColors.current
    val health = when {
        !node.known -> ServiceHealth.IDLE
        !node.up -> ServiceHealth.CRITICAL
        node.draining -> ServiceHealth.WARNING
        else -> ServiceHealth.OK
    }
    val subtitle = listOfNotNull(
        roleLabel(role),
        node.kubeNode.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.data_services_on_node, it) },
        stringResource(R.string.garage_node_down).takeIf { node.known && !node.up },
        stringResource(R.string.garage_layout_unknown_node).takeIf { !node.known },
        role.storedPartitions.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.garage_layout_partitions, it.toInt(), it.toInt()) },
    ).joinToString(" · ")
    ResourceRow(
        health,
        node.label,
        subtitle = subtitle,
        trailing = {
            when (node.stagedKind) {
                GarageStagedChange.REMOVE -> Tag(stringResource(R.string.garage_layout_staged_remove), colors.bad)
                GarageStagedChange.CHANGE -> Tag(stringResource(R.string.garage_layout_staged_change), colors.warn)
                else -> {}
            }
            if (onEdit != null || onRemove != null) RoleMenu(onEdit, onRemove)
        },
    ) {
        if (!role.gateway && role.usableCapacity > 0) {
            MeterRow(
                stringResource(R.string.garage_layout_meter_usable),
                role.usableFraction,
                "${formatBytes(role.usableCapacity)} / ${formatBytes(role.capacity)}",
                Modifier.padding(top = 2.dp),
                // Here a full bar is the good case: the whole capacity is usable.
                warnAt = 2f,
            )
        }
    }
}

@Composable
private fun RoleMenu(onEdit: (() -> Unit)?, onRemove: (() -> Unit)?) {
    var open by remember { mutableStateOf(false) }
    Box {
        TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { open = true })
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            if (onEdit != null) {
                DropdownMenuItem(text = { Text(stringResource(R.string.garage_layout_change_role)) }, onClick = {
                    open = false
                    onEdit()
                })
            }
            if (onRemove != null) {
                DropdownMenuItem(text = { Text(stringResource(R.string.garage_layout_remove)) }, onClick = {
                    open = false
                    onRemove()
                })
            }
        }
    }
}

/** A known node without a role: where it runs and its disk, with the way in. */
@Composable
private fun CandidateRow(node: GarageLayoutNode, onAdd: (() -> Unit)?) {
    val subtitle = listOfNotNull(
        node.kubeNode.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.data_services_on_node, it) },
        node.dataTotal.takeIf { it > 0 }?.let { stringResource(R.string.garage_layout_disk, formatBytes(it)) },
        stringResource(R.string.garage_node_down).takeIf { !node.up },
    ).joinToString(" · ").ifEmpty { null }
    ResourceRow(if (node.up) ServiceHealth.IDLE else ServiceHealth.WARNING, node.label, subtitle = subtitle, trailing = {
        if (onAdd != null) IchorTextButton(onClick = onAdd) { Text(stringResource(R.string.garage_layout_add)) }
    })
}

/**
 * The versions, newest first, behind an expander; the trackers while several are live, and,
 * when a draining version waits on a node that is down, the way past it ([onSkip]).
 */
@Composable
private fun History(layout: GarageLayout, onSkip: (() -> Unit)?) {
    val colors = LocalStatusColors.current
    var open by rememberSaveable { mutableStateOf(layout.stuck) }
    Column {
        Row(Modifier.fillMaxWidth().expandable(open) { open = !open }.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.garage_layout_history), style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.weight(1f))
            if (layout.stuck) Tag(stringResource(R.string.garage_layout_stuck), colors.bad)
            Icon(if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (!open) return@Column
        if (layout.stuck) {
            val down = layout.trackers.filter { t -> layout.node(t.id)?.let { !it.up } == true }.map { it.label }
            Text(
                stringResource(R.string.garage_layout_stuck_text, down.joinToString(", ").ifEmpty { "—" }),
                style = MaterialTheme.typography.bodySmall,
                color = colors.bad,
                modifier = Modifier.padding(bottom = 4.dp),
            )
            if (onSkip != null) {
                Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                    IchorTextButton(onClick = onSkip) { Text(stringResource(R.string.garage_layout_skip), color = colors.bad) }
                }
            }
        }
        layout.versions.forEach { VersionRow(it) }
        if (layout.trackers.isNotEmpty()) {
            InfoRow(stringResource(R.string.garage_layout_min_ack), "v${layout.minAck}")
            layout.trackers.forEach { t ->
                InfoRow(t.label, stringResource(R.string.garage_layout_tracker, t.ack, t.sync, t.syncAck), mono = true)
            }
        }
    }
}

@Composable
private fun VersionRow(version: GarageLayoutVersion) {
    val colors = LocalStatusColors.current
    val (label, color) = when (version.statusKind) {
        GarageLayoutStatus.CURRENT -> R.string.garage_layout_status_current to colors.ok
        GarageLayoutStatus.DRAINING -> R.string.garage_layout_status_draining to colors.warn
        GarageLayoutStatus.HISTORICAL -> R.string.garage_layout_status_historical to colors.muted
        GarageLayoutStatus.UNKNOWN -> R.string.garage_status_unknown to colors.muted
    }
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text("v${version.version}", style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
        Text(
            stringResource(R.string.garage_layout_version_nodes, version.storageNodes, version.gatewayNodes),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.weight(1f),
        )
        StatusPill(stringResource(label), color)
    }
}

/**
 * Skipping the dead nodes is irreversible and cluster-wide: the instance's name is typed. The
 * switch adds the data-loss variant, which also moves every node's sync tracker.
 */
@Composable
private fun SkipDeadNodesDialog(state: GarageLayoutState, layout: GarageLayout, onConfirm: (Boolean) -> Unit, onDismiss: () -> Unit) {
    var allowMissingData by remember { mutableStateOf(false) }
    val down = layout.trackers.filter { t -> layout.node(t.id)?.let { !it.up } == true }.map { it.label }
    HostnameConfirmDialog(
        title = stringResource(R.string.garage_layout_skip_title, state.instance.name),
        hostname = state.instance.name,
        confirmLabel = stringResource(R.string.garage_layout_skip_confirm),
        onConfirm = { onConfirm(allowMissingData) },
        onDismiss = onDismiss,
        emphasized = allowMissingData,
    ) {
        Text(stringResource(R.string.garage_layout_skip_text, down.joinToString(", ").ifEmpty { "—" }, layout.version), style = MaterialTheme.typography.bodyMedium)
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            IchorSwitch(checked = allowMissingData, onCheckedChange = { allowMissingData = it })
            Text(stringResource(R.string.garage_layout_skip_allow), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
        }
        if (allowMissingData) {
            Text(stringResource(R.string.garage_layout_skip_allow_text), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.bad)
        }
    }
}

/** Applying is cluster-wide and moves data: the new version number is typed, as Garage's CLI asks. */
@Composable
private fun ApplyConfirmDialog(layout: GarageLayout, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    HostnameConfirmDialog(
        title = stringResource(R.string.garage_layout_apply_title, layout.nextVersion),
        hostname = layout.nextVersion.toString(),
        confirmLabel = stringResource(R.string.garage_layout_apply),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
    ) {
        val moved = layout.preview?.movedPartitions ?: -1
        Text(
            if (moved >= 0) stringResource(R.string.garage_layout_apply_text_moved, moved) else stringResource(R.string.garage_layout_apply_text),
            style = MaterialTheme.typography.bodyMedium,
        )
    }
}

@Composable
private fun RemoveConfirmDialog(node: GarageLayoutNode, onConfirm: () -> Unit, onDismiss: () -> Unit) {
    ConfirmDialog(
        title = stringResource(R.string.garage_layout_remove_title, node.label),
        text = stringResource(R.string.garage_layout_remove_text),
        confirm = stringResource(R.string.garage_layout_remove),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
        confirmColor = LocalStatusColors.current.warn,
    )
}

/** Maximum, or a least number of zones; Garage refuses more than its replication factor. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ZoneRedundancyDialog(layout: GarageLayout, onPick: (String) -> Unit, onDismiss: () -> Unit) {
    val current = layout.stagedZoneRedundancy.ifEmpty { layout.zoneRedundancy }
    var picked by remember { mutableStateOf(current) }
    IchorAlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.garage_layout_zone_redundancy)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(stringResource(R.string.garage_layout_zone_redundancy_text), style = MaterialTheme.typography.bodyMedium)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    (listOf("maximum") + (1..3).map { it.toString() }).forEach { value ->
                        IchorFilterChip(selected = picked == value, onClick = { picked = value }, label = { Text(redundancyLabel(value)) })
                    }
                }
            }
        },
        confirmButton = {
            IchorTextButton(onClick = { onPick(picked) }, enabled = picked != current) { Text(stringResource(R.string.garage_layout_stage)) }
        },
        dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
    )
}
