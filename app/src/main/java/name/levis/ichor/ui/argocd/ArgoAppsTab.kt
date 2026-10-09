package name.levis.ichor.ui.argocd

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.AcUnit
import androidx.compose.material.icons.outlined.ArrowDropDown
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.ViewAgenda
import androidx.compose.material3.AssistChip
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeAction
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.rememberKubeDenial
import name.levis.ichor.ui.components.rememberKubeDenialAcross
import name.levis.ichor.model.ArgoAction
import name.levis.ichor.model.ArgoApp
import name.levis.ichor.model.ArgoFilter
import name.levis.ichor.model.ArgoGroupBy
import name.levis.ichor.model.ArgoStatus
import name.levis.ichor.model.FreezeScope
import name.levis.ichor.model.filterCounts
import name.levis.ichor.model.filtered
import name.levis.ichor.model.grouped
import name.levis.ichor.model.sortedApps
import name.levis.ichor.model.syncAllCandidates
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.TooltipIconButton

/**
 * Every Application, worst first: filter chips with counts, a search field and a group-by
 * menu above swipeable rows. The header's Select button or a long press starts a
 * multi-selection ([selecting], [selection]); with the OutOfSync chip on, "Sync all" asks
 * [onSyncAll] for the apps it would sync.
 */
@Composable
fun ArgoAppsTab(
    status: ArgoStatus,
    downNodes: Set<String>,
    busy: Set<String>,
    selecting: Boolean,
    onSelecting: (Boolean) -> Unit,
    selection: Set<String>,
    onSelection: (Set<String>) -> Unit,
    onOpen: (ArgoApp) -> Unit,
    onAct: (ArgoApp, ArgoAction) -> Unit,
    onSyncAll: (List<ArgoApp>) -> Unit,
    onFreeze: (ArgoApp, FreezeScope) -> Unit,
) {
    var filter by rememberSaveable { mutableStateOf(ArgoFilter.ALL) }
    var query by rememberSaveable { mutableStateOf("") }
    var groupBy by rememberSaveable { mutableStateOf(ArgoGroupBy.NONE) }
    val apps = remember(status) { status.sortedApps }
    val counts = remember(apps) { apps.filterCounts() }
    val groups = remember(apps, filter, query, groupBy) { apps.filtered(filter, query).grouped(groupBy) }
    // Asked in each namespace of the Applications (usually Argo CD's own, one): the first refusal stands for the list.
    val listDenial = rememberKubeDenialAcross(KubeAction.ARGO_SYNC, remember(apps) { apps.map { it.namespace }.distinct() })

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
        item(key = "controls") {
            Controls(query, { query = it }, filter, { filter = it }, counts, groupBy, { groupBy = it }) {
                SelectButton(selecting, onSelecting, enabled = apps.isNotEmpty())
            }
        }
        if (listDenial != null) item(key = "denied") {
            KubeDenialNote(listDenial, Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
        }
        if (filter == ArgoFilter.OUT_OF_SYNC) {
            val candidates = apps.filtered(filter, query).syncAllCandidates()
            if (candidates.isNotEmpty()) item(key = "syncall") {
                // Allowed only when allowed in every namespace of the apps it syncs.
                val denial = rememberKubeDenialAcross(KubeAction.ARGO_SYNC, candidates.map { it.namespace }.distinct())
                FilledTonalButton(onClick = { onSyncAll(candidates) }, enabled = denial == null, modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) {
                    Icon(Icons.Outlined.Sync, contentDescription = null, modifier = Modifier.size(18.dp))
                    Text(stringResource(R.string.argo_sync_all, candidates.size), modifier = Modifier.padding(start = 8.dp))
                }
            }
        }
        item(key = "divider") { HorizontalDivider() }
        if (groups.all { it.second.isEmpty() }) item(key = "empty") {
            EmptyText(
                when {
                    apps.isEmpty() -> stringResource(R.string.argo_no_apps)
                    query.isNotBlank() -> stringResource(R.string.data_services_no_match, query.trim())
                    else -> stringResource(R.string.data_services_all_fine)
                },
            )
        }
        groups.forEach { (name, list) ->
            if (groupBy != ArgoGroupBy.NONE && list.isNotEmpty()) item(key = "group-$name") {
                // A project or a destination namespace can be frozen as a whole from its header.
                val scope = when {
                    name.isEmpty() -> null
                    groupBy == ArgoGroupBy.PROJECT -> FreezeScope.PROJECT
                    // A freeze holds one project: only a namespace whose apps all share one.
                    groupBy == ArgoGroupBy.NAMESPACE && list.map { it.project }.distinct().size == 1 -> FreezeScope.NAMESPACE
                    else -> null
                }
                GroupHeader(groupTitle(groupBy, name), list.size, onFreeze = scope?.let { sc -> { onFreeze(list.first(), sc) } })
            }
            items(list, key = { it.key }) { app ->
                val toggle = { onSelection(if (app.key in selection) selection - app.key else selection + app.key) }
                val denied = rememberKubeDenial(KubeAction.ARGO_SYNC, app.namespace) != null
                val swipeEnabled = !selecting && app.key !in busy && !app.isRunning && !denied
                val sync = { onAct(app, ArgoAction.SYNC) }
                val refresh = { onAct(app, ArgoAction.REFRESH) }
                SwipeableArgoRow(swipeEnabled = swipeEnabled, onSync = sync, onRefresh = refresh) {
                    ArgoAppRow(
                        app = app,
                        downNodes = downNodes,
                        selected = app.key in selection,
                        busy = app.key in busy,
                        onClick = { if (selecting) toggle() else onOpen(app) },
                        onLongClick = toggle,
                        onClickLabel = if (selecting) null else stringResource(R.string.common_open),
                        onSync = sync.takeIf { swipeEnabled },
                        onRefresh = refresh.takeIf { swipeEnabled },
                    )
                }
                HorizontalDivider()
            }
        }
    }
}

@Composable
private fun Controls(
    query: String,
    onQuery: (String) -> Unit,
    filter: ArgoFilter,
    onFilter: (ArgoFilter) -> Unit,
    counts: Map<ArgoFilter, Int>,
    groupBy: ArgoGroupBy,
    onGroupBy: (ArgoGroupBy) -> Unit,
    trailing: @Composable () -> Unit,
) {
    Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(Modifier.padding(start = 16.dp, end = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            SearchField(query, onQuery, stringResource(R.string.argo_search), Modifier.weight(1f))
            GroupByMenu(groupBy, onGroupBy, Modifier.padding(start = 8.dp))
            trailing()
        }
        LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            items(ArgoFilter.entries.filter { it != ArgoFilter.ALL }, key = { it.name }) { f ->
                val count = counts[f] ?: 0
                FilterChip(
                    selected = filter == f,
                    onClick = { onFilter(if (filter == f) ArgoFilter.ALL else f) },
                    enabled = count > 0 || filter == f,
                    label = {
                        Text(stringResource(f.label))
                        Text(
                            count.toString(),
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(start = 6.dp),
                        )
                    },
                )
            }
        }
    }
}

/**
 * Starts the multi-selection a long press on a row also starts (the visible way in), or ends it
 * while one runs.
 */
@Composable
private fun SelectButton(selecting: Boolean, onSelecting: (Boolean) -> Unit, enabled: Boolean) {
    TextButton(onClick = { onSelecting(!selecting) }, enabled = enabled || selecting) {
        Text(stringResource(if (selecting) R.string.common_cancel else R.string.argo_select))
    }
}

@Composable
private fun GroupByMenu(groupBy: ArgoGroupBy, onGroupBy: (ArgoGroupBy) -> Unit, modifier: Modifier = Modifier) {
    var open by remember { mutableStateOf(false) }
    Box(modifier) {
        AssistChip(
            onClick = { open = true },
            label = { Text(stringResource(groupBy.label)) },
            leadingIcon = { Icon(Icons.Outlined.ViewAgenda, contentDescription = stringResource(R.string.argo_group_by), modifier = Modifier.size(18.dp)) },
            trailingIcon = { Icon(Icons.Outlined.ArrowDropDown, contentDescription = null, modifier = Modifier.size(18.dp)) },
        )
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            ArgoGroupBy.entries.forEach { g ->
                DropdownMenuItem(text = { Text(stringResource(g.label)) }, onClick = { onGroupBy(g); open = false })
            }
        }
    }
}

@Composable
private fun GroupHeader(title: String, count: Int, onFreeze: (() -> Unit)?) {
    Row(
        Modifier.fillMaxWidth().padding(start = 16.dp, end = if (onFreeze != null) 4.dp else 16.dp, top = 16.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(title, style = MaterialTheme.typography.titleSmall, color = MaterialTheme.colorScheme.primary, modifier = Modifier.weight(1f).semantics { heading() })
        Text(count.toString(), style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        onFreeze?.let { TooltipIconButton(Icons.Outlined.AcUnit, stringResource(R.string.argo_freeze_group, title), onClick = it) }
    }
}

@Composable
private fun groupTitle(by: ArgoGroupBy, name: String): String = when {
    name.isNotEmpty() -> name
    by == ArgoGroupBy.APP_SET -> stringResource(R.string.argo_group_no_app_set)
    else -> "—"
}

private val ArgoFilter.label: Int
    get() = when (this) {
        ArgoFilter.ALL -> R.string.data_services_filter_all
        ArgoFilter.ATTENTION -> R.string.argo_filter_attention
        ArgoFilter.OUT_OF_SYNC -> R.string.argo_filter_out_of_sync
        ArgoFilter.PROGRESSING -> R.string.argo_filter_progressing
        ArgoFilter.SYNCING -> R.string.argo_filter_syncing
        ArgoFilter.AUTO_SYNC_OFF -> R.string.argo_filter_auto_sync_off
        ArgoFilter.FROZEN -> R.string.argo_filter_frozen
    }

private val ArgoGroupBy.label: Int
    get() = when (this) {
        ArgoGroupBy.NONE -> R.string.argo_group_none
        ArgoGroupBy.PROJECT -> R.string.argo_group_project
        ArgoGroupBy.APP_SET -> R.string.argo_group_app_set
        ArgoGroupBy.NAMESPACE -> R.string.argo_group_namespace
    }
