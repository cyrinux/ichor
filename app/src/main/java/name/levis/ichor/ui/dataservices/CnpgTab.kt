package name.levis.ichor.ui.dataservices

import androidx.compose.animation.animateContentSize
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ExpandLess
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
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
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.CnpgCluster
import name.levis.ichor.model.CnpgPod
import name.levis.ichor.model.CnpgReason
import name.levis.ichor.model.CnpgStatus
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.filtered
import name.levis.ichor.model.pendingInstances
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.timeAgo
import name.levis.ichor.ui.components.expandable

/**
 * Every CloudNativePG cluster, built for dozens: a summary, the ones that need a look by
 * default, one compact line each that opens to the details.
 */
@Composable
fun CnpgTab(status: CnpgStatus) {
    val attention = remember(status) { status.clusters.count { it.serviceHealth.needsAttention } }
    var problemsOnly by rememberSaveable { mutableStateOf(attention > 0) }
    var query by rememberSaveable { mutableStateOf("") }
    var expanded by rememberSaveable { mutableStateOf(setOf<String>()) }
    val rows = remember(status, problemsOnly, query) { status.clusters.filtered(problemsOnly, query) }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item { InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp)) }
        item(key = "summary") {
            val total = status.clusters.size
            val pending = status.pendingInstances
            Column(Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp)) {
                Text(
                    listOfNotNull(
                        pluralStringResource(R.plurals.cnpg_clusters, total, total),
                        attention.takeIf { it > 0 }?.let { pluralStringResource(R.plurals.apps_attention, it, it) },
                    ).joinToString(" · "),
                    style = MaterialTheme.typography.titleSmall,
                )
                if (pending > 0) {
                    Text(pluralStringResource(R.plurals.cnpg_pending, pending, pending), style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.warn)
                }
            }
        }
        item(key = "filters") {
            Filters(
                query = query,
                onQuery = { query = it },
                chips = listOf(true to stringResource(R.string.data_services_filter_problems), false to stringResource(R.string.data_services_filter_all)),
                selected = problemsOnly,
                onSelect = { problemsOnly = it },
            )
            HorizontalDivider()
        }
        if (rows.isEmpty()) item(key = "empty") {
            EmptyText(
                when {
                    query.isNotBlank() -> stringResource(R.string.data_services_no_match, query.trim())
                    problemsOnly -> stringResource(R.string.data_services_all_fine)
                    else -> stringResource(R.string.cnpg_empty)
                },
            )
        }
        items(rows, key = { it.label }) { c ->
            ClusterRow(c, open = c.label in expanded, onToggle = { expanded = if (c.label in expanded) expanded - c.label else expanded + c.label })
            HorizontalDivider()
        }
    }
}

@Composable
private fun ClusterRow(c: CnpgCluster, open: Boolean, onToggle: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().expandable(open, onToggle = onToggle).animateContentSize().padding(horizontal = 16.dp, vertical = 8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(c.serviceHealth)
            Spacer(Modifier.size(12.dp))
            Column(Modifier.weight(1f)) {
                Text(c.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val line = listOfNotNull(
                    stringResource(R.string.pods_ready_count, c.readyInstances, c.instances),
                    stringResource(R.string.cnpg_hibernated).takeIf { c.hibernated },
                ) + c.reasonList.map { reasonText(it, c) }
                Text(
                    line.joinToString(" · "),
                    style = MaterialTheme.typography.labelSmall,
                    color = c.serviceHealth.color().takeIf { c.serviceHealth.needsAttention } ?: muted,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Icon(if (open) Icons.Outlined.ExpandLess else Icons.Outlined.ExpandMore, contentDescription = null, tint = muted)
        }
        if (open) Details(c)
    }
}

@Composable
private fun Details(c: CnpgCluster) {
    Column(Modifier.padding(start = 22.dp, top = 8.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        if (c.phase.isNotEmpty()) InfoRow(stringResource(R.string.cnpg_phase), listOf(c.phase, c.phaseReason).filter { it.isNotEmpty() }.joinToString(": "))
        if (c.currentPrimary.isNotEmpty()) {
            val primary = if (c.targetPrimary.isNotEmpty() && c.targetPrimary != c.currentPrimary) "${c.currentPrimary} → ${c.targetPrimary}" else c.currentPrimary
            InfoRow(stringResource(R.string.cnpg_primary), primary, mono = true)
        }
        if (c.instancePods.isNotEmpty()) {
            Text(stringResource(R.string.cnpg_instances), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 4.dp))
            c.instancePods.forEach { PodLine(it) }
        }
        InfoRow(stringResource(R.string.cnpg_archiving), archivingText(c.archiving))
        InfoRow(
            stringResource(R.string.cnpg_backups),
            when (c.backupMethod) {
                "plugin" -> stringResource(R.string.cnpg_backup_plugin, c.objectStore.ifEmpty { "—" })
                "in-tree" -> stringResource(R.string.cnpg_backup_in_tree)
                else -> stringResource(R.string.cnpg_backup_none)
            },
        )
        InfoRow(stringResource(R.string.cnpg_last_backup), ago(c.lastSuccessAt))
        if (c.lastFailureAt > 0) InfoRow(stringResource(R.string.cnpg_last_failure), ago(c.lastFailureAt))
        if (c.recoverableAt > 0) InfoRow(stringResource(R.string.cnpg_recoverable), ago(c.recoverableAt))
    }
}

@Composable
private fun PodLine(p: CnpgPod) {
    val colors = LocalStatusColors.current
    Row(Modifier.padding(start = 8.dp, top = 2.dp), verticalAlignment = Alignment.CenterVertically) {
        HealthDot(if (p.ready) ServiceHealth.OK else ServiceHealth.CRITICAL, Modifier.size(8.dp))
        Spacer(Modifier.size(8.dp))
        val role = when (p.role) {
            "primary" -> stringResource(R.string.cnpg_role_primary)
            "replica" -> stringResource(R.string.cnpg_role_replica)
            else -> null
        }
        val where = when {
            p.node.isEmpty() && p.phase == "Pending" -> stringResource(R.string.cnpg_pod_pending)
            p.node.isNotEmpty() -> stringResource(R.string.data_services_on_node, p.node)
            else -> p.phase
        }
        Text(
            listOfNotNull(p.name, role, where).joinToString(" · "),
            style = MaterialTheme.typography.labelSmall,
            fontFamily = FontFamily.Monospace,
            color = if (p.ready) MaterialTheme.colorScheme.onSurface else colors.bad,
        )
    }
}

@Composable
private fun reasonText(reason: CnpgReason, c: CnpgCluster): String = when (reason) {
    CnpgReason.NO_INSTANCE -> stringResource(R.string.cnpg_reason_no_instance)
    CnpgReason.FAILOVER -> stringResource(R.string.cnpg_reason_failover)
    CnpgReason.INSTANCES -> stringResource(R.string.cnpg_reason_instances)
    CnpgReason.SWITCHOVER -> stringResource(R.string.cnpg_reason_switchover) + " (${c.currentPrimary} → ${c.targetPrimary})"
    CnpgReason.NOT_READY -> stringResource(R.string.cnpg_reason_not_ready)
    CnpgReason.ARCHIVING -> stringResource(R.string.cnpg_reason_archiving)
    CnpgReason.BACKUP_FAILED -> stringResource(R.string.cnpg_reason_backup_failed)
    CnpgReason.BACKUP_STALE -> stringResource(R.string.cnpg_reason_backup_stale)
}

@Composable
private fun archivingText(archiving: String): String = when (archiving) {
    "ok" -> stringResource(R.string.cnpg_archiving_ok)
    "failing" -> stringResource(R.string.cnpg_archiving_failing)
    "off" -> stringResource(R.string.cnpg_archiving_off)
    else -> stringResource(R.string.cnpg_archiving_unknown)
}

@Composable
private fun ago(millis: Long): String = timeAgo(millis).ifEmpty { stringResource(R.string.cnpg_never) }
