package name.levis.ichor.ui.dataservices

import android.text.format.DateUtils
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.LonghornBackupTarget
import name.levis.ichor.model.LonghornNode
import name.levis.ichor.model.LonghornStatus
import name.levis.ichor.model.LonghornVolume
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.VolumeFilter
import name.levis.ichor.model.filtered
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes

/** Longhorn volumes (problems first), the backup targets and each node's disks. */
@Composable
fun LonghornTab(status: LonghornStatus, garageDetected: Boolean, onGarage: () -> Unit) {
    val hasProblems = remember(status) { status.volumes.any { it.serviceHealth.needsAttention } }
    var filter by rememberSaveable { mutableStateOf(if (hasProblems) VolumeFilter.PROBLEMS else VolumeFilter.ALL) }
    var query by rememberSaveable { mutableStateOf("") }
    val rows = remember(status, filter, query) { status.volumes.filtered(filter, query) }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 16.dp)) {
        if (status.error.isNotEmpty()) item {
            InlineError(stringResource(R.string.data_services_unreadable, status.error), Modifier.padding(16.dp))
        }
        status.backupTargets.forEach { target ->
            item(key = "bt:${target.name}|${target.url}") { BackupTargetRow(target, garageDetected, onGarage) }
        }
        item(key = "filters") {
            Filters(
                query = query,
                onQuery = { query = it },
                chips = listOf(
                    VolumeFilter.PROBLEMS to stringResource(R.string.data_services_filter_problems),
                    VolumeFilter.ALL to stringResource(R.string.data_services_filter_all),
                    VolumeFilter.DETACHED to stringResource(R.string.longhorn_filter_detached),
                ),
                selected = filter,
                onSelect = { filter = it },
            )
            HorizontalDivider()
        }
        if (rows.isEmpty()) item(key = "empty") {
            EmptyText(
                when {
                    query.isNotBlank() -> stringResource(R.string.data_services_no_match, query.trim())
                    filter == VolumeFilter.PROBLEMS -> stringResource(R.string.data_services_all_fine)
                    else -> stringResource(R.string.longhorn_empty)
                },
            )
        }
        items(rows, key = { "v:${it.name}" }) { v ->
            VolumeRow(v)
            HorizontalDivider()
        }
        if (status.nodes.isNotEmpty()) item(key = "nodes") {
            SectionTitle(stringResource(R.string.longhorn_nodes), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        }
        items(status.nodes, key = { "n:${it.name}" }) { NodeRow(it) }
    }
}

@Composable
private fun VolumeRow(v: LonghornVolume) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        HealthDot(v.serviceHealth)
        Spacer(Modifier.size(12.dp))
        Column(Modifier.weight(1f)) {
            Text(v.label, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                listOfNotNull(
                    stateLabel(v.state),
                    robustnessLabel(v.robustness).takeIf { v.robustness.isNotEmpty() && v.robustness != "unknown" || v.serviceHealth == ServiceHealth.CRITICAL },
                    stringResource(R.string.longhorn_replicas, v.replicasHealthy, v.replicasDesired),
                    v.rebuilding.takeIf { it > 0 }?.let { stringResource(R.string.longhorn_rebuilding, it) },
                ).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = v.serviceHealth.color().takeIf { v.serviceHealth.needsAttention } ?: muted,
            )
            Text(
                listOfNotNull(
                    v.node.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.data_services_on_node, it) }
                        ?: v.replicaNodes.takeIf { it.isNotEmpty() }?.joinToString(", "),
                    formatBytes(v.actualSize) + " / " + formatBytes(v.size),
                    if (v.lastBackupAt > 0) {
                        stringResource(R.string.longhorn_backup_ago, DateUtils.getRelativeTimeSpanString(v.lastBackupAt, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS))
                    } else {
                        stringResource(R.string.longhorn_no_backup)
                    },
                ).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = muted,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

@Composable
private fun BackupTargetRow(target: LonghornBackupTarget, garageDetected: Boolean, onGarage: () -> Unit) {
    val colors = LocalStatusColors.current
    val linkToGarage = !target.available && garageDetected
    Column(
        Modifier.fillMaxWidth().then(if (linkToGarage) Modifier.clickable(onClick = onGarage) else Modifier).padding(horizontal = 16.dp, vertical = 8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Column(Modifier.weight(1f)) {
                Text(stringResource(R.string.longhorn_backup_target), style = MaterialTheme.typography.bodyMedium)
                Text(target.url, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (target.available) {
                StatusPill(stringResource(R.string.longhorn_backup_target_available), colors.ok)
            } else {
                StatusPill(stringResource(R.string.longhorn_backup_target_unavailable), colors.bad)
            }
        }
        if (!target.available && target.message.isNotEmpty()) {
            Text(target.message, style = MaterialTheme.typography.labelSmall, color = colors.bad)
        }
        if (linkToGarage) {
            Text(stringResource(R.string.longhorn_backup_target_garage), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
        }
    }
}

@Composable
private fun NodeRow(node: LonghornNode) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(if (node.ready) ServiceHealth.OK else ServiceHealth.CRITICAL)
            Spacer(Modifier.size(12.dp))
            Text(node.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
            when {
                !node.ready -> Text(stringResource(R.string.longhorn_node_not_ready), style = MaterialTheme.typography.labelSmall, color = colors.bad)
                !node.schedulable -> Text(stringResource(R.string.longhorn_node_unschedulable), style = MaterialTheme.typography.labelSmall, color = colors.warn)
            }
        }
        node.disks.forEach { d ->
            if (d.maximum > 0) {
                UsageBar((d.scheduled.toFloat() / d.maximum).coerceIn(0f, 1f), Modifier.padding(start = 22.dp))
            }
            Text(
                listOfNotNull(
                    d.path.ifEmpty { null },
                    stringResource(R.string.longhorn_disk_scheduled, formatBytes(d.scheduled), formatBytes(d.maximum)),
                    stringResource(R.string.longhorn_node_unschedulable).takeIf { !d.schedulable },
                ).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(start = 22.dp),
            )
        }
    }
}

@Composable
private fun stateLabel(state: String): String = when (state) {
    "attached" -> stringResource(R.string.longhorn_state_attached)
    "detached" -> stringResource(R.string.longhorn_state_detached)
    "attaching" -> stringResource(R.string.longhorn_state_attaching)
    "detaching" -> stringResource(R.string.longhorn_state_detaching)
    "creating" -> stringResource(R.string.longhorn_state_creating)
    "deleting" -> stringResource(R.string.longhorn_state_deleting)
    else -> state
}

@Composable
private fun robustnessLabel(robustness: String): String = when (robustness) {
    "healthy" -> stringResource(R.string.longhorn_robustness_healthy)
    "degraded" -> stringResource(R.string.longhorn_robustness_degraded)
    "faulted" -> stringResource(R.string.longhorn_robustness_faulted)
    else -> stringResource(R.string.longhorn_robustness_unknown)
}

/** Search field and filter chips, shared by the Longhorn and Postgres tabs. */
@Composable
internal fun <T> Filters(query: String, onQuery: (String) -> Unit, chips: List<Pair<T, String>>, selected: T, onSelect: (T) -> Unit) {
    Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        SearchField(query, onQuery, stringResource(R.string.data_services_search), Modifier.fillMaxWidth().padding(horizontal = 16.dp))
        LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(chips, key = { it.first.toString() }) { (value, label) ->
                FilterChip(selected = selected == value, onClick = { onSelect(value) }, label = { Text(label) })
            }
        }
    }
}
