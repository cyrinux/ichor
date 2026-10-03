package name.levis.ichor.ui.dataservices

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
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.GarageInstance
import name.levis.ichor.model.GarageNode
import name.levis.ichor.model.GarageStatus
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.usedFraction

/** Each Garage cluster: its state, why, the sync counters and its nodes (down ones first). */
@Composable
fun GarageTab(status: GarageStatus) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error)) }
        items(status.instances, key = { it.label }) { InstanceCard(it) }
    }
}

@Composable
private fun InstanceCard(inst: GarageInstance) {
    val colors = LocalStatusColors.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(inst.label, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
                StatusPill(inst.state.label(), inst.state.health.color())
            }
            if (inst.message.isNotEmpty() && inst.detailed) {
                Text(inst.message, style = MaterialTheme.typography.bodySmall, color = inst.state.health.color().takeIf { inst.state.health.needsAttention } ?: MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (!inst.detailed) {
                InfoNotice(stringResource(R.string.garage_basic_only, inst.message.ifEmpty { "—" }), Modifier.padding(vertical = 4.dp))
            }
            Spacer(Modifier.size(4.dp))
            if (inst.storageNodes > 0) {
                InfoRow(stringResource(R.string.garage_storage_nodes), stringResource(R.string.garage_up_of, inst.storageNodesUp, inst.storageNodes))
            }
            if (inst.partitions > 0) {
                InfoRow(stringResource(R.string.garage_partitions_replicated), "${inst.partitionsAllOk}/${inst.partitions}")
                InfoRow(stringResource(R.string.garage_partitions_quorum), "${inst.partitionsQuorum}/${inst.partitions}")
            }
            if (inst.resyncQueue >= 0) InfoRow(stringResource(R.string.garage_resync_queue), "%,d".format(inst.resyncQueue))
            if (inst.resyncErrors >= 0) {
                Row {
                    Text(
                        stringResource(R.string.garage_resync_errors),
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.weight(0.4f).padding(vertical = 3.dp),
                    )
                    Text(
                        "%,d".format(inst.resyncErrors),
                        style = MaterialTheme.typography.bodyMedium,
                        color = if (inst.resyncErrors > 0) colors.bad else MaterialTheme.colorScheme.onSurface,
                        modifier = Modifier.weight(0.6f).padding(vertical = 3.dp),
                    )
                }
            }
            if (inst.tableSyncQueue >= 0) InfoRow(stringResource(R.string.garage_table_queue), "%,d".format(inst.tableSyncQueue))
            InfoRow(stringResource(R.string.garage_pods), stringResource(R.string.pods_ready_count, inst.podsReady, inst.pods))
            if (inst.version.isNotEmpty()) InfoRow(stringResource(R.string.garage_version), inst.version, mono = true)

            if (inst.nodes.isNotEmpty()) {
                SectionTitle(stringResource(R.string.garage_nodes))
                inst.nodes.forEachIndexed { i, node ->
                    if (i > 0) HorizontalDivider()
                    NodeRow(node)
                }
            }
        }
    }
}

@Composable
private fun NodeRow(node: GarageNode) {
    val colors = LocalStatusColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val down = !node.up && node.storage
    Column(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(
                when {
                    down -> ServiceHealth.CRITICAL
                    !node.up -> ServiceHealth.IDLE
                    node.resyncErrors > 0 || node.statsError.isNotEmpty() -> ServiceHealth.WARNING
                    else -> ServiceHealth.OK
                },
            )
            Spacer(Modifier.size(10.dp))
            Text(node.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (node.zone.isNotEmpty()) Text(node.zone, style = MaterialTheme.typography.labelSmall, color = muted)
        }
        val details = listOfNotNull(
            if (!node.up) {
                if (node.lastSeenSecs >= 0) stringResource(R.string.garage_node_down_since, localizedDuration(node.lastSeenSecs)) else stringResource(R.string.garage_node_down)
            } else {
                null
            },
            node.kubeNode.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.data_services_on_node, it) },
            // Translations never go through format(): a "%" in one must not break the line.
            node.takeIf { it.resyncQueue >= 0 }?.let { stringResource(R.string.garage_resync_queue) + " " + "%,d".format(it.resyncQueue) },
            node.takeIf { it.resyncErrors > 0 }?.let { stringResource(R.string.garage_resync_errors) + " " + "%,d".format(it.resyncErrors) },
            node.statsError.takeIf { it.isNotEmpty() && node.up }?.let { stringResource(R.string.garage_node_stats_error, it) },
        )
        if (details.isNotEmpty()) {
            Text(details.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = if (down) colors.bad else muted, modifier = Modifier.padding(start = 20.dp))
        }
        if (node.dataTotal > 0) {
            UsageBar(usedFraction(node.dataTotal, node.dataAvail), Modifier.padding(start = 20.dp, top = 2.dp))
            Text(
                "${formatBytes(node.dataTotal - node.dataAvail)} / ${formatBytes(node.dataTotal)}",
                style = MaterialTheme.typography.labelSmall,
                color = muted,
                modifier = Modifier.padding(start = 20.dp),
            )
        }
    }
}
