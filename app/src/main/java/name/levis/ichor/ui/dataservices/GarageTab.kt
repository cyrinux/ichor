package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.GARAGE_TRANQUILITY_DEFAULT
import name.levis.ichor.model.GARAGE_TRANQUILITY_FULL
import name.levis.ichor.model.GarageBucketStatus
import name.levis.ichor.model.GarageClusterStatus
import name.levis.ichor.model.GarageInstance
import name.levis.ichor.model.GarageKeyStatus
import name.levis.ichor.model.GarageNode
import name.levis.ichor.model.GarageNodeCr
import name.levis.ichor.model.GarageOpReason
import name.levis.ichor.model.GarageStatus
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.components.color
import name.levis.ichor.ui.components.label
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.usedFraction

/** The garage-operator's API group and kinds, for the object summary a tap opens. */
private const val GARAGE_OPERATOR_GROUP = "garage.rajsingh.info"
private const val GARAGE_OPERATOR_V1 = "v1beta1"

/**
 * Each Garage cluster: its state, why, the sync counters and its nodes (down ones first).
 * Where Garage's CLI runs, [actions] reports the blocks failing to resync and sets each
 * node's resync speed. When the garage-operator manages Garage, [onObject] opens the object
 * summary of a cluster or bucket it declares.
 */
@Composable
fun GarageTab(status: GarageStatus, actions: GarageActions, onObject: (KubeObjectRef) -> Unit = {}) {
    val tuning by actions.tuning.collectAsStateWithLifecycle()
    val sheet by actions.sheet.collectAsStateWithLifecycle()
    var confirm by remember { mutableStateOf<TranquilityChange?>(null) }

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
    sheet?.let { GarageBlocksSheet(it, onReload = actions::reload, onRepair = actions::repair, onDismiss = actions::closeReport) }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error)) }
        if (status.hasOperator) item(key = "operator") { OperatorCard(status, onObject) }
        items(status.instances, key = { it.label }) { inst ->
            InstanceCard(
                inst,
                tuning = tuning,
                onBlocks = { actions.openReport(inst) },
                onTranquility = { node, value -> confirm = TranquilityChange(inst, node, value) },
            )
        }
    }
}

/** A tranquility change waiting for confirmation. */
private data class TranquilityChange(val instance: GarageInstance, val node: GarageNode, val value: Long)

@Composable
private fun InstanceCard(inst: GarageInstance, tuning: Set<String>, onBlocks: () -> Unit, onTranquility: (GarageNode, Long) -> Unit) {
    val colors = LocalStatusColors.current
    // Actions run Garage's CLI in a ready pod, which only a full (cli-json) reading names.
    val actionable = inst.detailed && inst.pod.isNotEmpty()
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
            if (actionable) BlockErrorsButton(failing = inst.resyncErrors > 0, onClick = onBlocks)

            if (inst.nodes.isNotEmpty()) {
                SectionTitle(stringResource(R.string.garage_nodes))
                inst.nodes.forEachIndexed { i, node ->
                    if (i > 0) HorizontalDivider()
                    NodeRow(
                        node,
                        tuning = GarageActions.tuningKey(inst, node) in tuning,
                        onTranquility = { value: Long -> onTranquility(node, value) }
                            .takeIf { actionable && node.up && node.storage && node.tranquility >= 0 },
                    )
                }
            }
        }
    }
}

/** Opens the block report; stands out while blocks fail to resync. */
@Composable
private fun BlockErrorsButton(failing: Boolean, onClick: () -> Unit) {
    Row(Modifier.fillMaxWidth().padding(top = 4.dp), horizontalArrangement = Arrangement.End) {
        if (failing) {
            FilledTonalButton(onClick = onClick) { Text(stringResource(R.string.garage_block_errors)) }
        } else {
            OutlinedButton(onClick = onClick) { Text(stringResource(R.string.garage_block_errors)) }
        }
    }
}

/** [onTranquility] is null when the node's resync speed cannot be changed from here. */
@Composable
private fun NodeRow(node: GarageNode, tuning: Boolean, onTranquility: ((Long) -> Unit)?) {
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
            when {
                node.tranquility == GARAGE_TRANQUILITY_FULL -> stringResource(R.string.garage_tranquility_full)
                node.tranquility > 0 -> stringResource(R.string.garage_tranquility_value, node.tranquility)
                else -> null
            },
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
        if (onTranquility != null) TranquilityAction(node.tranquility, tuning, onTranquility)
    }
}

/** Full speed while the node's resync is throttled, back to Garage's default once it is not. */
@Composable
private fun TranquilityAction(current: Long, tuning: Boolean, onSet: (Long) -> Unit) {
    val full = current == GARAGE_TRANQUILITY_FULL
    Box(Modifier.padding(start = 8.dp).height(40.dp), contentAlignment = Alignment.CenterStart) {
        if (tuning) {
            CircularProgressIndicator(Modifier.padding(start = 12.dp).size(18.dp), strokeWidth = 2.dp)
        } else {
            TextButton(onClick = { onSet(if (full) GARAGE_TRANQUILITY_DEFAULT else GARAGE_TRANQUILITY_FULL) }) {
                Text(stringResource(if (full) R.string.garage_tranquility_set_default else R.string.garage_tranquility_set_full))
            }
        }
    }
}

/** The garage-operator's declarative layer: its own health, then the clusters, buckets, keys and node declarations it manages, problems first. */
@Composable
private fun OperatorCard(status: GarageStatus, onObject: (KubeObjectRef) -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            status.operator?.let { op ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    HealthDot(op.serviceHealth)
                    Spacer(Modifier.size(10.dp))
                    Text(op.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text(stringResource(R.string.pods_ready_count, op.ready, op.desired), style = MaterialTheme.typography.labelSmall, color = muted)
                }
                OperatorProblems(op.reasonList, emptyList(), op.serviceHealth)
            }

            if (status.clusters.isNotEmpty()) {
                SectionTitle(stringResource(R.string.garage_operator_clusters))
                status.clusters.forEachIndexed { i, c ->
                    if (i > 0) HorizontalDivider()
                    OperatorClusterRow(c, status.version, onObject)
                }
            }

            if (status.buckets.isNotEmpty()) {
                SectionTitle(stringResource(R.string.garage_operator_buckets))
                status.buckets.forEachIndexed { i, b ->
                    if (i > 0) HorizontalDivider()
                    OperatorBucketRow(b, onObject)
                }
            }

            if (status.keys.isNotEmpty()) {
                SectionTitle(stringResource(R.string.garage_operator_keys))
                status.keys.forEachIndexed { i, k ->
                    if (i > 0) HorizontalDivider()
                    OperatorKeyRow(k)
                }
            }

            if (status.nodes.isNotEmpty()) {
                SectionTitle(stringResource(R.string.garage_operator_nodes))
                status.nodes.forEachIndexed { i, n ->
                    if (i > 0) HorizontalDivider()
                    OperatorNodeRow(n)
                }
            }

            if (status.truncated) {
                InfoNotice(stringResource(R.string.garage_operator_truncated), Modifier.padding(top = 4.dp))
            }
        }
    }
}

/** The reasons the Go core named, then the operator conditions' own messages. */
@Composable
private fun OperatorProblems(reasons: List<GarageOpReason>, messages: List<String>, health: ServiceHealth) {
    val problems = (reasons.map { garageOpReasonText(it) } + messages).distinct()
    if (problems.isNotEmpty()) {
        Text(
            problems.joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            color = if (health.needsAttention) health.color() else MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(start = 20.dp),
        )
    }
}

@Composable
private fun OperatorClusterRow(c: GarageClusterStatus, version: String, onObject: (KubeObjectRef) -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(
        Modifier.fillMaxWidth().clickable {
            onObject(KubeObjectRef(GARAGE_OPERATOR_GROUP, version, "garageclusters", "GarageCluster", c.namespace, c.name, editable = false))
        }.padding(vertical = 6.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(c.serviceHealth)
            Spacer(Modifier.size(10.dp))
            Text(c.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(c.phase, style = MaterialTheme.typography.labelSmall, color = muted)
        }
        val details = listOfNotNull(
            stringResource(R.string.garage_operator_tiers, c.storageReadyReplicas, c.storageReplicas, c.gatewayReadyReplicas, c.gatewayReplicas)
                .takeIf { c.storageReplicas > 0 || c.gatewayReplicas > 0 },
            c.garageVersion.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.garage_version) + " " + it },
            stringResource(R.string.garage_operator_layout, c.layoutVersion),
        )
        if (details.isNotEmpty()) {
            Text(details.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.padding(start = 20.dp))
        }
        OperatorProblems(c.reasonList, c.conditions.map { it.message }, c.serviceHealth)
    }
}

@Composable
private fun OperatorBucketRow(b: GarageBucketStatus, onObject: (KubeObjectRef) -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(
        Modifier.fillMaxWidth().clickable {
            onObject(KubeObjectRef(GARAGE_OPERATOR_GROUP, GARAGE_OPERATOR_V1, "garagebuckets", "GarageBucket", b.namespace, b.name, editable = false))
        }.padding(vertical = 6.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(b.serviceHealth)
            Spacer(Modifier.size(10.dp))
            Text(b.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(b.phase, style = MaterialTheme.typography.labelSmall, color = muted)
        }
        val details = listOfNotNull(
            b.size.takeIf { it.isNotEmpty() },
            b.quotaUsage?.let { q ->
                if (q.sizeLimit > 0) stringResource(R.string.garage_operator_quota_of, formatBytes(q.sizeBytes), formatBytes(q.sizeLimit)) else formatBytes(q.sizeBytes)
            },
            b.quotaUsage?.takeIf { it.objectCount > 0 }?.let { stringResource(R.string.garage_operator_objects, "%,d".format(it.objectCount)) },
            b.websiteUrl.takeIf { it.isNotEmpty() },
            b.keysWithAccess.takeIf { it > 0 }?.let { stringResource(R.string.garage_operator_keys_with_access, it) },
        )
        if (details.isNotEmpty()) {
            Text(details.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.padding(start = 20.dp))
        }
        OperatorProblems(b.reasonList, b.conditions.map { it.message }, b.serviceHealth)
    }
}

@Composable
private fun OperatorKeyRow(k: GarageKeyStatus) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(k.serviceHealth)
            Spacer(Modifier.size(10.dp))
            Text(k.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(k.phase, style = MaterialTheme.typography.labelSmall, color = muted)
        }
        val grants = buildList {
            k.allBuckets?.let { add(stringResource(R.string.garage_operator_all_buckets) + " " + permissionWords(it.read, it.write, it.owner)) }
            k.bucketPermissions.take(3).forEach { p -> add(p.bucket + " " + permissionWords(p.read, p.write, p.owner)) }
            val more = k.bucketPermissionsTotal - minOf(3, k.bucketPermissions.size)
            if (more > 0) add(stringResource(R.string.garage_operator_more_permissions, more))
        }
        val details = listOfNotNull(
            k.keyId.takeIf { it.isNotEmpty() },
            grants.joinToString(" · ").ifEmpty { null },
        )
        if (details.isNotEmpty()) {
            Text(details.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.padding(start = 20.dp))
        }
        OperatorProblems(k.reasonList, emptyList(), k.serviceHealth)
    }
}

/** read+write+owner, in the app's words. */
@Composable
private fun permissionWords(read: Boolean, write: Boolean, owner: Boolean): String = buildList {
    if (read) add(stringResource(R.string.garage_perm_read))
    if (write) add(stringResource(R.string.garage_perm_write))
    if (owner) add(stringResource(R.string.garage_perm_owner))
}.joinToString("+").ifEmpty { "—" }

@Composable
private fun OperatorNodeRow(n: GarageNodeCr) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(n.serviceHealth)
            Spacer(Modifier.size(10.dp))
            Text(n.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(n.phase, style = MaterialTheme.typography.labelSmall, color = muted)
        }
        val details = listOfNotNull(
            n.zone.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.garage_operator_zone, it) },
            n.capacity.takeIf { it.isNotEmpty() },
            if (n.gateway) stringResource(R.string.garage_operator_gateway) else null,
            when {
                !n.connected && n.lastSeenSecs >= 0 -> stringResource(R.string.garage_operator_node_last_seen, localizedDuration(n.lastSeenSecs))
                !n.connected -> stringResource(R.string.garage_operator_node_not_connected)
                else -> null
            },
            if (!n.inLayout) stringResource(R.string.garage_operator_node_not_in_layout) else null,
        )
        if (details.isNotEmpty()) {
            Text(details.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = if (n.serviceHealth.needsAttention) n.serviceHealth.color() else muted, modifier = Modifier.padding(start = 20.dp))
        }
    }
}

/** Why a garage-operator item is not ok, in the app's words. */
@Composable
private fun garageOpReasonText(reason: GarageOpReason): String = when (reason) {
    GarageOpReason.CLUSTER_FAILED -> stringResource(R.string.garage_op_cluster_failed)
    GarageOpReason.ADMIN_UNREACHABLE -> stringResource(R.string.garage_op_admin_unreachable)
    GarageOpReason.OPERATOR_DOWN -> stringResource(R.string.garage_op_operator_down)
    GarageOpReason.DEGRADED -> stringResource(R.string.garage_op_degraded)
    GarageOpReason.STORAGE_REPLICAS -> stringResource(R.string.garage_op_storage_replicas)
    GarageOpReason.QUORUM_AT_RISK -> stringResource(R.string.garage_op_quorum_at_risk)
    GarageOpReason.LAYOUT_STAGED -> stringResource(R.string.garage_op_layout_staged)
    GarageOpReason.TOMBSTONES -> stringResource(R.string.garage_op_tombstones)
    GarageOpReason.NODE_DISCONNECTED -> stringResource(R.string.garage_op_node_disconnected)
    GarageOpReason.NODE_NOT_IN_LAYOUT -> stringResource(R.string.garage_op_node_not_in_layout)
    GarageOpReason.BUCKET_FAILED -> stringResource(R.string.garage_op_bucket_failed)
    GarageOpReason.LIFECYCLE -> stringResource(R.string.garage_op_lifecycle)
    GarageOpReason.LOOKUP_STUCK -> stringResource(R.string.garage_op_lookup_stuck)
    GarageOpReason.METADATA_DEGRADED -> stringResource(R.string.garage_op_metadata_degraded)
    GarageOpReason.DELETION_BLOCKED -> stringResource(R.string.garage_op_deletion_blocked)
    GarageOpReason.WEBSITE_EXPOSURE -> stringResource(R.string.garage_op_website_exposure)
    GarageOpReason.KEY_FAILED -> stringResource(R.string.garage_op_key_failed)
}
