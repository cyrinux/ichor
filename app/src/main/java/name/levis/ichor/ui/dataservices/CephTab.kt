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
import name.levis.ichor.model.CephCheck
import name.levis.ichor.model.CephCluster
import name.levis.ichor.model.CephOsd
import name.levis.ichor.model.CephPool
import name.levis.ichor.model.CephReason
import name.levis.ichor.model.CephStatus
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.formatPercent

/** Every Ceph cluster (problems first) with its health checks and capacity, its OSDs, then pools and stores. */
@Composable
fun CephTab(status: CephStatus) {
    // The namespace only tells OSDs apart when Rook runs more than one cluster.
    val manyClusters = status.osds.map { it.namespace }.distinct().size > 1
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.clusters.isEmpty()) item { EmptyText(stringResource(R.string.ceph_empty)) }
        items(status.clusters, key = { "c:${it.label}" }) { c ->
            ClusterRow(c)
            HorizontalDivider()
        }
        if (status.osds.isNotEmpty()) item(key = "osds") {
            SectionTitle(stringResource(R.string.ceph_osds_title), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        }
        items(status.osds, key = { "o:${it.namespace}/${it.pod}" }) { OsdLine(it, manyClusters) }
        if (status.pools.isNotEmpty()) item(key = "pools") {
            SectionTitle(stringResource(R.string.ceph_pools_title), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        }
        items(status.pools, key = { "p:${it.kind}/${it.label}" }) { PoolRow(it) }
    }
}

@Composable
private fun ClusterRow(c: CephCluster) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(c.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Text(c.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (c.cephHealth.isNotEmpty()) Text(c.cephHealth, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = cephHealthColor(c.cephHealth))
        }
        val line = listOfNotNull(
            stringResource(R.string.ceph_external).takeIf { c.external },
            stringResource(R.string.ceph_osds, c.osdsUp, c.osdsTotal).takeIf { c.osdsTotal > 0 },
            stringResource(R.string.ceph_mons, c.monsReady, c.monsTotal).takeIf { c.monsTotal > 0 },
            c.version.ifEmpty { null },
        ) + c.reasonList.map { reasonText(it, c) }
        Text(
            line.joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            color = c.serviceHealth.color().takeIf { c.serviceHealth.needsAttention } ?: muted,
            modifier = Modifier.padding(start = 22.dp),
        )
        if (c.bytesTotal > 0) {
            UsageBar(c.usedFraction.toFloat().coerceIn(0f, 1f), Modifier.padding(start = 22.dp, top = 4.dp), warnAt = 0.85f)
            Text(
                stringResource(R.string.ceph_capacity, formatBytes(c.bytesUsed), formatBytes(c.bytesTotal), formatPercent(c.usedFraction * 100, 0)),
                style = MaterialTheme.typography.labelSmall,
                color = muted,
                modifier = Modifier.padding(start = 22.dp),
            )
        }
        // Rook's message only says something when Ceph has not (a cluster being set up, a failure).
        if (c.message.isNotEmpty() && c.checks.isEmpty() && c.phase != "Ready" && c.phase != "Connected") {
            Text(c.message, style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.padding(start = 22.dp))
        }
        c.checks.forEach { CheckLine(it) }
    }
}

@Composable
private fun CheckLine(check: CephCheck) {
    Text(
        "${check.name}: ${check.message}",
        style = MaterialTheme.typography.labelSmall,
        color = cephHealthColor(check.severity),
        modifier = Modifier.padding(start = 22.dp, top = 2.dp),
    )
}

@Composable
private fun OsdLine(osd: CephOsd, withNamespace: Boolean) {
    val where = podWhere(osd.node, osd.phase)
    Row(Modifier.padding(horizontal = 16.dp, vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
        HealthDot(if (osd.ready) ServiceHealth.OK else ServiceHealth.CRITICAL, Modifier.size(8.dp))
        Spacer(Modifier.size(8.dp))
        Text(
            listOfNotNull(osd.namespace.takeIf { withNamespace }, "osd.${osd.id}", where).joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            color = if (osd.ready) MaterialTheme.colorScheme.onSurface else LocalStatusColors.current.bad,
        )
    }
}

@Composable
private fun PoolRow(pool: CephPool) {
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
        HealthDot(pool.serviceHealth)
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f)) {
            Text(pool.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                listOfNotNull(kindLabel(pool.kind), pool.phase.ifEmpty { null }).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = pool.serviceHealth.color().takeIf { pool.serviceHealth.needsAttention } ?: MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun cephHealthColor(health: String) = when (health) {
    "HEALTH_ERR" -> LocalStatusColors.current.bad
    "HEALTH_WARN" -> LocalStatusColors.current.warn
    "HEALTH_OK" -> LocalStatusColors.current.ok
    else -> MaterialTheme.colorScheme.onSurfaceVariant
}

@Composable
private fun kindLabel(kind: String): String = when (kind) {
    "blockPool" -> stringResource(R.string.ceph_kind_block_pool)
    "filesystem" -> stringResource(R.string.ceph_kind_filesystem)
    "objectStore" -> stringResource(R.string.ceph_kind_object_store)
    else -> kind
}

@Composable
private fun reasonText(reason: CephReason, c: CephCluster): String = when (reason) {
    CephReason.HEALTH_ERR -> stringResource(R.string.ceph_reason_health_err)
    CephReason.FAILURE -> stringResource(R.string.ceph_reason_failure)
    CephReason.FULL -> stringResource(R.string.ceph_reason_full)
    CephReason.NO_OSD -> stringResource(R.string.ceph_reason_no_osd)
    CephReason.NO_QUORUM -> stringResource(R.string.ceph_reason_no_quorum)
    CephReason.HEALTH_WARN -> stringResource(R.string.ceph_reason_health_warn)
    CephReason.NEAR_FULL -> stringResource(R.string.ceph_reason_near_full)
    CephReason.OSDS -> stringResource(R.string.ceph_reason_osds)
    CephReason.MONS -> stringResource(R.string.ceph_reason_mons)
    // Rook's phase is its own word (Progressing, Updating...).
    CephReason.NOT_READY -> stringResource(R.string.dragonfly_reason_phase, c.phase)
}
