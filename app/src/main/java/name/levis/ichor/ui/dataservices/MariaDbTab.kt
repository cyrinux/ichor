package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.MariaDbCluster
import name.levis.ichor.model.MariaDbPod
import name.levis.ichor.model.MariaDbReason
import name.levis.ichor.model.MariaDbStatus
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.util.timeAgo

/** Every MariaDB cluster (problems first) with its topology, pods and their role, and its backups. */
@Composable
fun MariaDbTab(status: MariaDbStatus) {
    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        if (status.clusters.isEmpty()) item { EmptyText(stringResource(R.string.mariadb_empty)) }
        items(status.clusters, key = { it.label }) { c ->
            ClusterRow(c)
            HorizontalDivider()
        }
    }
}

@Composable
private fun ClusterRow(c: MariaDbCluster) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        val line = listOfNotNull(
            topologyText(c.topology),
            stringResource(R.string.pods_ready_count, c.readyPods, c.replicas),
            stringResource(R.string.mariadb_suspended).takeIf { c.suspended },
        ) + c.reasonList.map { reasonText(it, c) }
        ServiceRowHeader(c.serviceHealth, c.label, line)
        c.pods.forEach { DataPodLine(it.name, it.ready, it.node, it.phase, it.role) }
        BackupLine(c)
    }
}

/** "last backup 3 hours ago · failed 1 hour ago · schedule 0 3 * * *", nothing without any backup. */
@Composable
private fun BackupLine(c: MariaDbCluster) {
    if (c.lastBackupAt == 0L && c.lastBackupFailedAt == 0L && c.backupSchedule.isEmpty()) return
    val parts = listOfNotNull(
        stringResource(R.string.mariadb_last_backup, timeAgo(c.lastBackupAt).ifEmpty { stringResource(R.string.cnpg_never) }),
        timeAgo(c.lastBackupFailedAt).takeIf { it.isNotEmpty() }?.let { stringResource(R.string.mariadb_last_failure, it) },
        c.backupSchedule.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.mariadb_schedule, it) },
    )
    Text(
        parts.joinToString(" · "),
        style = MaterialTheme.typography.labelSmall,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(start = 22.dp, top = 2.dp),
    )
}

@Composable
private fun topologyText(topology: String): String? = when (topology) {
    "standalone" -> stringResource(R.string.mariadb_topology_standalone)
    "replication" -> stringResource(R.string.mariadb_topology_replication)
    "galera" -> "Galera"
    else -> topology.ifEmpty { null }
}

@Composable
private fun reasonText(reason: MariaDbReason, c: MariaDbCluster): String = when (reason) {
    MariaDbReason.NO_READY -> stringResource(R.string.dragonfly_reason_no_ready)
    MariaDbReason.NO_PRIMARY -> stringResource(R.string.mariadb_reason_no_primary)
    MariaDbReason.PODS -> stringResource(R.string.dragonfly_reason_pods)
    MariaDbReason.GALERA_RECOVERY -> stringResource(R.string.mariadb_reason_galera_recovery)
    MariaDbReason.BACKUP_FAILED -> stringResource(R.string.cnpg_reason_backup_failed)
    MariaDbReason.BACKUP_STALE -> stringResource(R.string.cnpg_reason_backup_stale)
    // The operator's own words (switching primary, updating...).
    MariaDbReason.NOT_READY -> c.message.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.dragonfly_reason_phase, it) }
        ?: stringResource(R.string.cnpg_reason_not_ready)
}
