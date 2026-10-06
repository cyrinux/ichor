package name.levis.ichor.ui.dataservices

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
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
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.model.LonghornAction
import name.levis.ichor.model.LonghornBackupTarget
import name.levis.ichor.model.LonghornNode
import name.levis.ichor.model.LonghornStatus
import name.levis.ichor.model.LonghornVolume
import name.levis.ichor.model.ServiceHealth
import name.levis.ichor.model.VolumeFilter
import name.levis.ichor.model.actions
import name.levis.ichor.model.filtered
import name.levis.ichor.model.longhornActionKey
import name.levis.ichor.model.replicaChoices
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.UsageBar
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatBytes
import name.levis.ichor.util.timeAgo

/** Longhorn volumes (problems first), the backup targets and each node's disks. */
@Composable
fun LonghornTab(status: LonghornStatus, actions: LonghornActions, garageDetected: Boolean, onGarage: () -> Unit) {
    val hasProblems = remember(status) { status.volumes.any { it.serviceHealth.needsAttention } }
    var filter by rememberSaveable { mutableStateOf(if (hasProblems) VolumeFilter.PROBLEMS else VolumeFilter.ALL) }
    var query by rememberSaveable { mutableStateOf("") }
    val rows = remember(status, filter, query) { status.volumes.filtered(filter, query) }
    val busy by actions.busy.collectAsStateWithLifecycle()
    var replicasOf by remember { mutableStateOf<LonghornVolume?>(null) }
    var evicting by remember { mutableStateOf<LonghornNode?>(null) }

    replicasOf?.let { v ->
        ReplicaCountDialog(
            label = v.label,
            current = v.replicasDesired,
            choices = status.replicaChoices(v),
            onConfirm = { count ->
                replicasOf = null
                actions.run(v.key, v.namespace, v.name, v.label, LonghornAction.REPLICAS, count)
            },
            onDismiss = { replicasOf = null },
        )
    }
    evicting?.let { n ->
        EvictConfirmDialog(
            node = n.name,
            onConfirm = {
                evicting = null
                actions.run(n.key, n.namespace, n.name, n.name, LonghornAction.EVICT)
            },
            onDismiss = { evicting = null },
        )
    }
    val onVolume = { v: LonghornVolume, action: LonghornAction ->
        if (action == LonghornAction.REPLICAS) replicasOf = v else actions.run(v.key, v.namespace, v.name, v.label, action)
    }
    val onNode = { n: LonghornNode, action: LonghornAction ->
        if (action == LonghornAction.EVICT) evicting = n else actions.run(n.key, n.namespace, n.name, n.name, action)
    }

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
            VolumeRow(v, v.key in busy) { onVolume(v, it) }
            HorizontalDivider()
        }
        if (status.nodes.isNotEmpty()) item(key = "nodes") {
            SectionTitle(stringResource(R.string.longhorn_nodes), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        }
        items(status.nodes, key = { "n:${it.name}" }) { n -> NodeRow(n, n.key in busy) { onNode(n, it) } }
    }
}

private val LonghornVolume.key get() = longhornActionKey(namespace, name)
private val LonghornNode.key get() = longhornActionKey(namespace, "node/$name")

@Composable
private fun VolumeRow(v: LonghornVolume, busy: Boolean, onAction: (LonghornAction) -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val colors = LocalStatusColors.current
    Row(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 8.dp, bottom = 8.dp), verticalAlignment = Alignment.CenterVertically) {
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
                        stringResource(R.string.longhorn_backup_ago, timeAgo(v.lastBackupAt))
                    } else {
                        stringResource(R.string.longhorn_no_backup)
                    },
                ).joinToString(" · "),
                style = MaterialTheme.typography.labelSmall,
                color = muted,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            // 0 is also what an unread engine gives: the status line already counts the rebuilds.
            if (v.rebuilding > 0 && v.rebuildProgress > 0) OperationProgress(stringResource(R.string.longhorn_rebuild_progress, v.rebuildProgress), v.rebuildProgress)
            if (v.backingUp) OperationProgress(stringResource(R.string.longhorn_backup_progress, v.backupProgress), v.backupProgress)
            if (v.restoring) OperationProgress(stringResource(R.string.longhorn_restore_progress, v.restoreProgress), v.restoreProgress)
            if (v.scheduleError.isNotEmpty()) {
                Text(stringResource(R.string.longhorn_schedule_error, v.scheduleError), style = MaterialTheme.typography.labelSmall, color = colors.warn)
            }
            if (v.tooManySnapshots) {
                Text(stringResource(R.string.longhorn_too_many_snapshots), style = MaterialTheme.typography.labelSmall, color = colors.warn)
            }
        }
        LonghornActionMenu(v.actions, busy, onAction)
    }
}

/** "Rebuilding 42%" over a thin bar. */
@Composable
private fun OperationProgress(label: String, percent: Int) {
    Column(Modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(label, style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.primary)
        LinearProgressIndicator(
            progress = { percent / 100f },
            modifier = Modifier.fillMaxWidth().height(4.dp),
            trackColor = MaterialTheme.colorScheme.surfaceVariant,
            drawStopIndicator = {},
        )
    }
}

@Composable
private fun BackupTargetRow(target: LonghornBackupTarget, garageDetected: Boolean, onGarage: () -> Unit) {
    val colors = LocalStatusColors.current
    val linkToGarage = !target.available && garageDetected
    Column(
        Modifier.fillMaxWidth().then(if (linkToGarage) Modifier.clickable(role = Role.Button, onClick = onGarage) else Modifier).padding(horizontal = 16.dp, vertical = 8.dp),
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
private fun NodeRow(node: LonghornNode, busy: Boolean, onAction: (LonghornAction) -> Unit) {
    val colors = LocalStatusColors.current
    Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 4.dp, top = 6.dp, bottom = 6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            HealthDot(if (node.ready) ServiceHealth.OK else ServiceHealth.CRITICAL)
            Spacer(Modifier.size(12.dp))
            Text(node.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
            when {
                !node.ready -> Text(stringResource(R.string.longhorn_node_not_ready), style = MaterialTheme.typography.labelSmall, color = colors.bad)
                node.evictionRequested -> Text(
                    pluralStringResource(R.plurals.longhorn_node_evicting, node.replicas, node.replicas),
                    style = MaterialTheme.typography.labelSmall,
                    color = colors.warn,
                )
                !node.allowScheduling -> Text(stringResource(R.string.longhorn_node_scheduling_off), style = MaterialTheme.typography.labelSmall, color = colors.warn)
                !node.schedulable -> Text(stringResource(R.string.longhorn_node_unschedulable), style = MaterialTheme.typography.labelSmall, color = colors.warn)
                else -> Text(
                    pluralStringResource(R.plurals.longhorn_node_replicas, node.replicas, node.replicas),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            LonghornActionMenu(node.actions, busy, onAction)
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
