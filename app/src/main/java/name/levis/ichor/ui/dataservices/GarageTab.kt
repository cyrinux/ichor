package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ChevronRight
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.MoreVert
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.GARAGE_TRANQUILITY_DEFAULT
import name.levis.ichor.model.GARAGE_TRANQUILITY_FULL
import name.levis.ichor.model.GarageInstance
import name.levis.ichor.model.GarageNode
import name.levis.ichor.model.GarageStatus
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MeterRow
import name.levis.ichor.ui.components.ResourceRow
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatTile
import name.levis.ichor.ui.components.StatTileRow
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.color
import name.levis.ichor.ui.components.expandable
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.usedFraction

/**
 * Each Garage cluster: its state, three tiles (storage nodes, replicated partitions, write
 * quorum), the resync failures when there are, the rest behind "Details", the layout row
 * (opening the layout sheet), then its nodes (down ones first). Where Garage's CLI runs,
 * [actions] reports the blocks failing to resync, sets each node's resync speed and edits its
 * layout role (in the node's ⋮ menu). When the garage-operator manages Garage, [onObject]
 * opens the object summary of a cluster or bucket it declares, and the layout it declares is
 * shown but not edited from here.
 */
@Composable
fun GarageTab(status: GarageStatus, actions: GarageActions, onObject: (KubeObjectRef) -> Unit = {}) {
    val tuning by actions.tuning.collectAsStateWithLifecycle()
    val sheet by actions.sheet.collectAsStateWithLifecycle()
    val layoutSheet by actions.layout.sheet.collectAsStateWithLifecycle()
    val editor by actions.layout.editor.collectAsStateWithLifecycle()
    var confirm by remember { mutableStateOf<TranquilityChange?>(null) }
    var confirmRemove by remember { mutableStateOf<RoleRemoval?>(null) }

    confirm?.let { c ->
        TranquilityConfirmDialog(
            node = c.node,
            value = c.value,
            onConfirm = {
                confirm = null
                actions.setTranquility(c.instance, c.node, c.value)
            },
            onDismiss = { confirm = null },
        )
    }
    confirmRemove?.let { r ->
        ConfirmDialog(
            title = stringResource(R.string.garage_layout_remove_title, r.node.label),
            text = stringResource(R.string.garage_layout_remove_text),
            confirm = stringResource(R.string.garage_layout_remove),
            onConfirm = {
                confirmRemove = null
                actions.layout.stageRemoval(r.instance, r.node.id, r.node.label)
            },
            onDismiss = { confirmRemove = null },
            confirmColor = LocalStatusColors.current.warn,
        )
    }
    sheet?.let { GarageBlocksSheet(it, onReload = actions::reload, onRepair = actions::repair, onDismiss = actions::closeReport) }
    layoutSheet?.let { GarageLayoutSheet(it, actions.layout) }
    editor?.let { GarageRoleSheet(it, onStage = actions.layout::stageRole, onDismiss = actions.layout::closeEditor) }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error)) }
        if (status.hasOperator) item(key = "operator") { OperatorCard(status, onObject) }
        items(status.instances, key = { it.label }) { inst ->
            // The operator declares the layout of the clusters it drives: the app shows it, the GarageNode objects change it.
            val managed = status.clusters.any { it.instance == inst.label }
            InstanceCard(
                inst,
                tuning = tuning,
                managed = managed,
                onBlocks = { actions.openReport(inst) },
                onLayout = { actions.layout.open(inst, managed) },
                onTranquility = { node, value -> confirm = TranquilityChange(inst, node, value) },
                onEditRole = { node -> actions.layout.editRole(inst, node.id) },
                onRemoveRole = { node -> confirmRemove = RoleRemoval(inst, node) },
            )
        }
    }
}

/** A tranquility change waiting for confirmation. */
private data class TranquilityChange(val instance: GarageInstance, val node: GarageNode, val value: Long)

/** A node's removal from the layout waiting for confirmation. */
private data class RoleRemoval(val instance: GarageInstance, val node: GarageNode)

/** "160,739", or "—" for a count Garage did not give (-1). */
private fun count(value: Long): String = if (value >= 0) "%,d".format(value) else "—"

@Composable
private fun InstanceCard(
    inst: GarageInstance,
    tuning: Set<String>,
    managed: Boolean,
    onBlocks: () -> Unit,
    onLayout: () -> Unit,
    onTranquility: (GarageNode, Long) -> Unit,
    onEditRole: (GarageNode) -> Unit,
    onRemoveRole: (GarageNode) -> Unit,
) {
    val colors = LocalStatusColors.current
    // Actions run Garage's CLI in a ready pod, which only a full (cli-json) reading names.
    val actionable = inst.detailed && inst.pod.isNotEmpty()
    val failing = inst.resyncErrors > 0
    var details by rememberSaveable(inst.label) { mutableStateOf(false) }
    SectionCard(inst.label, pill = { StatusPill(inst.state.label(), inst.state.health.color()) }) {
        if (!inst.detailed) {
            InfoNotice(stringResource(R.string.garage_basic_only, inst.message.ifEmpty { "—" }))
        }
        InstanceTiles(inst)
        if (failing) {
            Text(
                stringResource(R.string.garage_resync_failing, count(inst.resyncQueue), count(inst.resyncErrors)),
                style = MaterialTheme.typography.bodySmall,
                color = colors.bad,
            )
        }
        Row(Modifier.fillMaxWidth().expandable(details) { details = !details }.padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.garage_details), style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
            Icon(if (details) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        if (details) {
            Column {
                if (inst.resyncQueue >= 0) InfoRow(stringResource(R.string.garage_resync_queue), count(inst.resyncQueue))
                if (inst.resyncErrors >= 0) InfoRow(stringResource(R.string.garage_resync_errors), count(inst.resyncErrors))
                if (inst.tableSyncQueue >= 0) InfoRow(stringResource(R.string.garage_table_queue), count(inst.tableSyncQueue))
                InfoRow(stringResource(R.string.garage_pods), stringResource(R.string.pods_ready_count, inst.podsReady, inst.pods))
                if (inst.version.isNotEmpty()) InfoRow(stringResource(R.string.garage_version), inst.version, mono = true)
            }
        }
        if (actionable) LayoutRow(inst, onLayout)

        if (inst.nodes.isNotEmpty()) {
            SectionTitle(stringResource(R.string.garage_nodes))
            Column {
                inst.nodes.forEachIndexed { i, node ->
                    if (i > 0) HorizontalDivider()
                    NodeRow(
                        node,
                        tuning = GarageActions.tuningKey(inst, node) in tuning,
                        onTranquility = { value: Long -> onTranquility(node, value) }
                            .takeIf { actionable && node.up && node.storage && node.tranquility >= 0 },
                        // A layout role is given to a connected node, changed or taken from a member; the operator's clusters keep theirs.
                        onEditRole = { onEditRole(node) }.takeIf { actionable && !managed && (node.storage || node.up) && node.id.length == 64 },
                        onRemoveRole = { onRemoveRole(node) }.takeIf { actionable && !managed && node.storage && node.id.length == 64 },
                    )
                }
            }
        }
        // The block report only has something to say while blocks fail to resync.
        if (actionable && failing) {
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.End) {
                TextButton(onClick = onBlocks) { Text(stringResource(R.string.garage_block_errors)) }
            }
        }
    }
}

/** "Layout v12 · 3 nodes": opens the layout sheet. */
@Composable
private fun LayoutRow(inst: GarageInstance, onLayout: () -> Unit) {
    val members = inst.nodes.count { it.storage }
    Row(Modifier.fillMaxWidth().clickable(role = Role.Button, onClick = onLayout).padding(vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(stringResource(R.string.garage_layout_row, inst.layoutVersion), style = MaterialTheme.typography.bodyMedium)
            Text(
                pluralStringResource(R.plurals.garage_layout_members, members, members),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Icon(Icons.Outlined.ChevronRight, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

/** Storage nodes up, partitions fully replicated and partitions with a write quorum, each over its bar. */
@Composable
private fun InstanceTiles(inst: GarageInstance) {
    val tiles = buildList<@Composable (Modifier) -> Unit> {
        if (inst.storageNodes > 0) add { m ->
            CountTile(stringResource(R.string.garage_up_of, inst.storageNodesUp, inst.storageNodes), stringResource(R.string.garage_storage_nodes), inst.storageNodesUp, inst.storageNodes, m)
        }
        if (inst.partitions > 0) {
            add { m -> CountTile("${inst.partitionsAllOk}/${inst.partitions}", stringResource(R.string.garage_partitions_replicated), inst.partitionsAllOk, inst.partitions, m) }
            add { m -> CountTile("${inst.partitionsQuorum}/${inst.partitions}", stringResource(R.string.garage_partitions_quorum), inst.partitionsQuorum, inst.partitions, m) }
        }
    }
    if (tiles.isNotEmpty()) StatTileRow(Modifier.fillMaxWidth().padding(vertical = 4.dp), *tiles.toTypedArray())
}

/**
 * A "6/7" tile over a bar that is ok only when full: a share of what should all be up, the
 * reverse of a usage bar (which turns red as it fills).
 */
@Composable
private fun CountTile(value: String, label: String, part: Int, total: Int, modifier: Modifier) {
    val colors = LocalStatusColors.current
    StatTile(null, value, label, modifier) {
        LinearProgressIndicator(
            progress = { (part.toFloat() / total).coerceIn(0f, 1f) },
            color = if (part >= total) colors.ok else colors.warn,
            trackColor = MaterialTheme.colorScheme.surfaceVariant,
            modifier = Modifier.padding(top = 4.dp).fillMaxWidth().height(6.dp),
            drawStopIndicator = {},
        )
    }
}

/**
 * A node: where it runs, its data disk as a meter, a warn tag while it has blocks to resync.
 * The ⋮ menu offers what is not null: its resync speed ([onTranquility]), a layout role to
 * give or change ([onEditRole]), its removal from the layout ([onRemoveRole]).
 */
@Composable
private fun NodeRow(node: GarageNode, tuning: Boolean, onTranquility: ((Long) -> Unit)?, onEditRole: (() -> Unit)?, onRemoveRole: (() -> Unit)?) {
    val colors = LocalStatusColors.current
    val down = !node.up && node.storage
    val health = when {
        down -> ServiceHealth.CRITICAL
        !node.up -> ServiceHealth.IDLE
        node.resyncErrors > 0 || node.statsError.isNotEmpty() -> ServiceHealth.WARNING
        else -> ServiceHealth.OK
    }
    val subtitle = listOfNotNull(
        if (!node.up) {
            if (node.lastSeenSecs >= 0) stringResource(R.string.garage_node_down_since, localizedDuration(node.lastSeenSecs)) else stringResource(R.string.garage_node_down)
        } else {
            null
        },
        node.kubeNode.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.data_services_on_node, it) },
        node.zone.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.garage_operator_zone, it) },
        stringResource(R.string.garage_operator_node_not_in_layout).takeIf { !node.storage },
        // Translations never go through format(): a "%" in one must not break the line.
        node.takeIf { it.resyncErrors > 0 }?.let { stringResource(R.string.garage_resync_errors) + " " + count(it.resyncErrors) },
        node.statsError.takeIf { it.isNotEmpty() && node.up }?.let { stringResource(R.string.garage_node_stats_error, it) },
        stringResource(R.string.garage_tranquility_full).takeIf { node.tranquility == GARAGE_TRANQUILITY_FULL },
        node.takeIf { it.dataTotal > 0 }?.let { "${formatBytes(it.dataTotal - it.dataAvail)} / ${formatBytes(it.dataTotal)}" },
    ).joinToString(" · ").ifEmpty { null }
    ResourceRow(
        health,
        node.label,
        subtitle = subtitle,
        trailing = {
            if (node.resyncQueue > 0) Tag(stringResource(R.string.garage_resync_tag, count(node.resyncQueue)), colors.warn)
            if (onTranquility != null || onEditRole != null || onRemoveRole != null) {
                NodeMenu(node, tuning, onTranquility, onEditRole, onRemoveRole)
            }
        },
    ) {
        if (node.dataTotal > 0) {
            val used = usedFraction(node.dataTotal, node.dataAvail)
            MeterRow(stringResource(R.string.garage_meter_data), used, "${(used * 100).toInt()}%", Modifier.padding(top = 2.dp))
        }
    }
}

/**
 * The node's ⋮ menu: full speed while its resync is throttled, back to Garage's default once
 * it is not; then its layout role (given, changed or removed).
 */
@Composable
private fun NodeMenu(node: GarageNode, tuning: Boolean, onTranquility: ((Long) -> Unit)?, onEditRole: (() -> Unit)?, onRemoveRole: (() -> Unit)?) {
    if (tuning) {
        Box(Modifier.size(48.dp), contentAlignment = Alignment.Center) {
            CircularProgressIndicator(Modifier.size(20.dp), strokeWidth = 2.dp)
        }
        return
    }
    val full = node.tranquility == GARAGE_TRANQUILITY_FULL
    var open by remember { mutableStateOf(false) }
    // The Box anchors the menu to the button, not to the row's start edge.
    Box {
        TooltipIconButton(Icons.Outlined.MoreVert, stringResource(R.string.common_more), onClick = { open = true })
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            if (onTranquility != null) {
                DropdownMenuItem(
                    text = { Text(stringResource(if (full) R.string.garage_tranquility_set_default else R.string.garage_tranquility_set_full)) },
                    onClick = {
                        open = false
                        onTranquility(if (full) GARAGE_TRANQUILITY_DEFAULT else GARAGE_TRANQUILITY_FULL)
                    },
                )
            }
            if (onEditRole != null) {
                DropdownMenuItem(
                    text = { Text(stringResource(if (node.storage) R.string.garage_layout_change_role else R.string.garage_layout_add)) },
                    onClick = {
                        open = false
                        onEditRole()
                    },
                )
            }
            if (onRemoveRole != null) {
                DropdownMenuItem(text = { Text(stringResource(R.string.garage_layout_remove)) }, onClick = {
                    open = false
                    onRemoveRole()
                })
            }
        }
    }
}
