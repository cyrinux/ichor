package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.GarageBlock
import name.levis.ichor.model.GarageBlockImpact
import name.levis.ichor.model.GarageBlockNode
import name.levis.ichor.model.GarageBlockReport
import name.levis.ichor.model.GarageBlockVerdict
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * What the blocks failing to resync in a Garage cluster are: how many back live objects,
 * a sample per node with the objects referencing them, and the repair for the metadata.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun GarageBlocksSheet(state: GarageBlocksState, onReload: () -> Unit, onRepair: () -> Unit, onDismiss: () -> Unit) {
    var confirmRepair by remember { mutableStateOf(false) }
    if (confirmRepair) {
        RepairConfirmDialog(
            onConfirm = {
                confirmRepair = false
                onRepair()
            },
            onDismiss = { confirmRepair = false },
        )
    }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        LazyColumn(contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            item { Header(state, onReload) }
            if (state.loading) item { LinearProgressIndicator(Modifier.fillMaxWidth()) }
            state.error?.let { error ->
                item {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        InlineError(error.asString())
                        if (state.report == null) OutlinedButton(onClick = onReload) { Text(stringResource(R.string.common_retry)) }
                    }
                }
            }
            val report = state.report ?: return@LazyColumn
            item { Summary(report) }
            item { RepairButton(enabled = report.errored > 0 && !state.repairing, running = state.repairing, onClick = { confirmRepair = true }) }
            report.nodes.forEach { node ->
                item(key = "node|${node.id}") { NodeHeader(node) }
                items(node.blocks, key = { "block|${node.id}|${it.hash}" }) { BlockRow(it) }
            }
        }
    }
}

@Composable
private fun Header(state: GarageBlocksState, onReload: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(stringResource(R.string.garage_block_errors), style = MaterialTheme.typography.titleLarge)
            Text(state.instance.label, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        IconButton(onClick = onReload, enabled = !state.loading) { Icon(Icons.Outlined.Refresh, stringResource(R.string.data_services_refresh)) }
    }
}

@Composable
private fun Summary(report: GarageBlockReport) {
    val colors = LocalStatusColors.current
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        InfoRow(stringResource(R.string.garage_resync_errors), "%,d".format(report.errored))
        if (report.errored > 0) {
            InfoRow(stringResource(R.string.garage_blocks_looked_up), "%,d".format(report.detailed))
            InfoRow(stringResource(R.string.garage_blocks_live), "%,d".format(report.live))
            InfoRow(stringResource(R.string.garage_blocks_cleanup), "%,d".format(report.cleanupOnly))
            InfoRow(stringResource(R.string.garage_blocks_stale_refs), "%,d".format(report.staleRefs))
            InfoRow(stringResource(R.string.garage_blocks_rc_mismatches), "%,d".format(report.refcountMismatches))
            InfoRow(stringResource(R.string.garage_blocks_retryable), "%,d".format(report.retryable))
        }
        val (verdict, color) = when (report.verdict) {
            GarageBlockVerdict.LIVE_DATA -> stringResource(R.string.garage_blocks_verdict_live) to colors.bad
            GarageBlockVerdict.DELETED_ONLY -> stringResource(R.string.garage_blocks_verdict_deleted) to colors.warn
            GarageBlockVerdict.NONE_FAILING -> stringResource(R.string.garage_blocks_verdict_none) to colors.ok
        }
        Text(verdict, style = MaterialTheme.typography.bodyMedium, color = color, modifier = Modifier.padding(top = 4.dp))
        if (report.repairsRunning) InfoNotice(stringResource(R.string.garage_blocks_repair_running), Modifier.padding(top = 4.dp))
    }
}

@Composable
private fun RepairButton(enabled: Boolean, running: Boolean, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalArrangement = Arrangement.End, verticalAlignment = Alignment.CenterVertically) {
        if (running) CircularProgressIndicator(Modifier.padding(end = 12.dp).size(20.dp), strokeWidth = 2.dp)
        Button(onClick = onClick, enabled = enabled) { Text(stringResource(R.string.garage_blocks_repair)) }
    }
}

@Composable
private fun NodeHeader(node: GarageBlockNode) {
    Column {
        HorizontalDivider(Modifier.padding(vertical = 4.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            SectionTitle(node.label, Modifier.weight(1f))
            if (node.error.isEmpty()) {
                Text(
                    pluralStringResource(R.plurals.garage_blocks_failing, node.errored, node.errored),
                    style = MaterialTheme.typography.labelMedium,
                    color = if (node.errored > 0) LocalStatusColors.current.bad else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        if (node.error.isNotEmpty()) InlineError(stringResource(R.string.garage_blocks_node_error, node.error))
    }
}

@Composable
private fun BlockRow(block: GarageBlock) {
    val colors = LocalStatusColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(block.shortHash, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
            ImpactPill(block.impactKind)
        }
        val details = listOfNotNull(
            pluralStringResource(R.plurals.garage_block_attempts, block.errors.toInt(), block.errors.toInt()),
            block.nextTrySecs.takeIf { it > 0 }?.let { stringResource(R.string.garage_block_next_try, localizedDuration(it)) },
            stringResource(R.string.garage_block_stale_ref).takeIf { block.staleRef },
            stringResource(R.string.garage_block_rc_mismatch).takeIf { block.refcountMismatch },
        )
        Text(details.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = muted)
        if (block.error.isNotEmpty()) InlineError(stringResource(R.string.garage_block_lookup_failed, block.error))
        block.refs.forEach { ref ->
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    ref.label,
                    style = MaterialTheme.typography.labelSmall,
                    fontFamily = FontFamily.Monospace,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Spacer(Modifier.size(8.dp))
                Text(
                    stringResource(if (ref.live) R.string.garage_block_ref_live else R.string.garage_block_ref_deleted),
                    style = MaterialTheme.typography.labelSmall,
                    color = if (ref.live) colors.bad else muted,
                )
            }
        }
    }
}

@Composable
private fun ImpactPill(impact: GarageBlockImpact) {
    val colors = LocalStatusColors.current
    val (label, color) = when (impact) {
        GarageBlockImpact.LIVE -> R.string.garage_block_impact_live to colors.bad
        GarageBlockImpact.STALE_REF -> R.string.garage_block_impact_stale to colors.warn
        GarageBlockImpact.CLEANUP -> R.string.garage_block_impact_cleanup to colors.ok
        GarageBlockImpact.UNKNOWN -> R.string.garage_block_impact_unknown to colors.muted
    }
    StatusPill(stringResource(label), color)
}

@Composable
private fun RepairConfirmDialog(onConfirm: () -> Unit, onDismiss: () -> Unit) {
    ConfirmDialog(
        title = stringResource(R.string.garage_blocks_repair_title),
        text = stringResource(R.string.garage_blocks_repair_text),
        confirm = stringResource(R.string.garage_blocks_repair),
        onConfirm = onConfirm,
        onDismiss = onDismiss,
        destructive = true,
    )
}
