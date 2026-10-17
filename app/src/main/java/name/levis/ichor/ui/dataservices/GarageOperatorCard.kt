package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
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
import name.levis.ichor.model.GarageBucketStatus
import name.levis.ichor.model.GarageClusterStatus
import name.levis.ichor.model.GarageKeyStatus
import name.levis.ichor.model.GarageNodeCr
import name.levis.ichor.model.GarageOpReason
import name.levis.ichor.model.GarageStatus
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.color
import name.levis.ichor.ui.components.localizedDuration
import name.levis.ichor.util.formatBytes

/** The garage-operator's API group and kinds, for the object summary a tap opens. */
private const val GARAGE_OPERATOR_GROUP = "garage.rajsingh.info"
private const val GARAGE_OPERATOR_V1 = "v1beta1"

/** The garage-operator's declarative layer: its own health, then the clusters, buckets, keys and node declarations it manages, problems first. */
@Composable
internal fun OperatorCard(status: GarageStatus, onObject: (KubeObjectRef) -> Unit) {
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
