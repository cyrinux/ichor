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
import name.levis.ichor.model.PerconaCluster
import name.levis.ichor.model.PerconaPod
import name.levis.ichor.model.PerconaReason
import name.levis.ichor.model.PerconaStatus
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo

/** Every Percona XtraDB cluster (problems first) with its members, its proxy and its backups. */
@Composable
fun PerconaTab(status: PerconaStatus) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.clusters.isEmpty()) item { EmptyText(stringResource(R.string.percona_empty)) }
        items(status.clusters, key = { it.label }) { c ->
            ClusterRow(c)
            HorizontalDivider()
        }
    }
}

@Composable
private fun ClusterRow(c: PerconaCluster) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(c.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Text(c.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        // Product names stay untranslated: "HAProxy 2/2".
        val proxy = when (c.proxy) {
            "haproxy" -> "HAProxy ${c.proxyReady}/${c.proxySize}"
            "proxysql" -> "ProxySQL ${c.proxyReady}/${c.proxySize}"
            else -> null
        }
        val line = listOfNotNull(
            stringResource(R.string.percona_paused).takeIf { c.paused } ?: stringResource(R.string.pods_ready_count, c.pxcReady, c.pxcSize),
            proxy,
        ) + c.reasonList.map { reasonText(it) }
        Text(
            line.joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            color = c.serviceHealth.color().takeIf { c.serviceHealth.needsAttention } ?: muted,
            modifier = Modifier.padding(start = 22.dp),
        )
        if (c.message.isNotEmpty() && c.serviceHealth.needsAttention) {
            Text(c.message, style = MaterialTheme.typography.labelSmall, color = muted, modifier = Modifier.padding(start = 22.dp))
        }
        BackupLine(c)
        c.pods.forEach { PodLine(it) }
    }
}

/** Last success and failure, and the schedules that should keep them coming; nothing without either. */
@Composable
private fun BackupLine(c: PerconaCluster) {
    if (c.backupSchedules.isEmpty() && c.lastBackupAt == 0L && c.lastBackupFailedAt == 0L) return
    val parts = listOfNotNull(
        stringResource(R.string.percona_last_backup, timeAgo(c.lastBackupAt).ifEmpty { stringResource(R.string.cnpg_never) }),
        c.lastBackupFailedAt.takeIf { it > 0 }?.let { stringResource(R.string.percona_last_failure, timeAgo(it)) },
        c.backupSchedules.takeIf { it.isNotEmpty() }
            ?.let { s -> stringResource(R.string.percona_schedules, s.joinToString(", ") { "${it.name} (${it.schedule})" }) },
    )
    Text(
        parts.joinToString(" · "),
        style = MaterialTheme.typography.labelSmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 22.dp),
    )
}

@Composable
private fun PodLine(p: PerconaPod) {
    val where = when {
        p.node.isEmpty() && p.phase == "Pending" -> stringResource(R.string.cnpg_pod_pending)
        p.node.isNotEmpty() -> stringResource(R.string.data_services_on_node, p.node)
        else -> p.phase
    }
    Row(Modifier.padding(start = 22.dp, top = 2.dp), verticalAlignment = Alignment.CenterVertically) {
        HealthDot(if (p.ready) ServiceHealth.OK else ServiceHealth.CRITICAL, Modifier.size(8.dp))
        Spacer(Modifier.size(8.dp))
        Text(
            listOf(p.name, where).filter { it.isNotEmpty() }.joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            color = if (p.ready) MaterialTheme.colorScheme.onSurface else LocalStatusColors.current.bad,
        )
    }
}

@Composable
private fun reasonText(reason: PerconaReason): String = when (reason) {
    PerconaReason.ERROR -> stringResource(R.string.percona_reason_error)
    PerconaReason.NO_MEMBER -> stringResource(R.string.percona_reason_no_member)
    PerconaReason.MEMBERS -> stringResource(R.string.percona_reason_members)
    PerconaReason.PROXY -> stringResource(R.string.percona_reason_proxy)
    PerconaReason.INITIALIZING -> stringResource(R.string.percona_reason_initializing)
    PerconaReason.BACKUP_FAILED -> stringResource(R.string.cnpg_reason_backup_failed)
    PerconaReason.BACKUP_STALE -> stringResource(R.string.cnpg_reason_backup_stale)
}
