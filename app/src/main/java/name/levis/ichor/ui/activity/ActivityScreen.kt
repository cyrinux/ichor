package name.levis.ichor.ui.activity

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.DeleteSweep
import androidx.compose.material.icons.outlined.ErrorOutline
import androidx.compose.material.icons.outlined.Share
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.ActivityLog
import name.levis.ichor.model.ActivityEntry
import name.levis.ichor.model.ActivityFilter
import name.levis.ichor.model.actionLabel
import name.levis.ichor.model.distinctOf
import name.levis.ichor.model.failed
import name.levis.ichor.model.matches
import name.levis.ichor.model.target
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.components.shareText
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.util.formatDateTime

/** The action audit log of [cluster] ("" for every cluster). */
class ActivityViewModel(private val cluster: String) : LoadingViewModel<List<ActivityEntry>>() {
    override suspend fun fetch() = ActivityLog.entries(cluster)

    suspend fun export(): String = ActivityLog.export(cluster)

    fun clear() {
        viewModelScope.launch {
            runCatching { ActivityLog.clear(cluster) }
            refresh()
        }
    }
}

/**
 * What the app changed on [cluster] ("" for every cluster, with a cluster filter): newest
 * first, filtered by action or failures, shared as a Markdown table, or cleared.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ActivityScreen(
    cluster: String,
    onBack: () -> Unit,
    vm: ActivityViewModel = viewModel(key = "activity-$cluster", factory = factory { ActivityViewModel(cluster) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { vm.refresh() }
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var confirmClear by remember { mutableStateOf(false) }
    val entries = (state as? UiState.Loaded)?.data.orEmpty()
    val shareTitle = stringResource(R.string.activity_share)

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.activity_title))
                        if (cluster.isNotEmpty()) {
                            Text(cluster, style = MaterialTheme.typography.bodySmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        }
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    TooltipIconButton(Icons.Outlined.Share, shareTitle, enabled = entries.isNotEmpty(), onClick = {
                        scope.launch {
                            runCatching { vm.export() }.onSuccess { shareText(context, it, shareTitle) }
                        }
                    })
                    TooltipIconButton(
                        Icons.Outlined.DeleteSweep,
                        stringResource(R.string.activity_clear),
                        enabled = entries.isNotEmpty(),
                        onClick = { confirmClear = true },
                    )
                },
            )
        },
    ) { padding ->
        when (val s = state) {
            UiState.Loading -> LoadingBox(Modifier.pageContent(padding))
            is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, Modifier.pageContent(padding))
            is UiState.Loaded -> ActivityList(s.data, showCluster = cluster.isEmpty(), Modifier.pageContent(padding))
        }
    }

    if (confirmClear) {
        ConfirmDialog(
            title = stringResource(R.string.activity_clear_title),
            text = stringResource(R.string.activity_clear_body),
            confirm = stringResource(R.string.activity_clear),
            destructive = true,
            onConfirm = {
                confirmClear = false
                vm.clear()
            },
            onDismiss = { confirmClear = false },
        )
    }
}

@Composable
private fun ActivityList(entries: List<ActivityEntry>, showCluster: Boolean, modifier: Modifier) {
    var clusterFilter by rememberSaveable { mutableStateOf<String?>(null) }
    var actionFilter by rememberSaveable { mutableStateOf<String?>(null) }
    var failedOnly by rememberSaveable { mutableStateOf(false) }
    val filter = ActivityFilter(clusterFilter, actionFilter, failedOnly)
    val rows = entries.filter { it.matches(filter) }
    val clusters = if (showCluster) entries.distinctOf { it.cluster } else emptyList()
    val actions = entries.distinctOf { it.action }

    Column(modifier.fillMaxSize()) {
        if (entries.isNotEmpty()) {
            Row(
                Modifier.horizontalScroll(rememberScrollState()).padding(horizontal = 16.dp),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                FilterChip(
                    selected = filter == ActivityFilter(),
                    onClick = {
                        clusterFilter = null
                        actionFilter = null
                        failedOnly = false
                    },
                    label = { Text(stringResource(R.string.activity_filter_all)) },
                )
                FilterChip(selected = failedOnly, onClick = { failedOnly = !failedOnly }, label = { Text(stringResource(R.string.activity_filter_failed)) })
                // Only a choice when there is more than one.
                if (clusters.size > 1) {
                    clusters.forEach { c ->
                        FilterChip(selected = clusterFilter == c, onClick = { clusterFilter = c.takeIf { clusterFilter != c } }, label = { Text(c) })
                    }
                }
                if (actions.size > 1) {
                    actions.forEach { a ->
                        val label = ActivityEntry(action = a).actionLabel
                        FilterChip(selected = actionFilter == a, onClick = { actionFilter = a.takeIf { actionFilter != a } }, label = { Text(label) })
                    }
                }
            }
            HorizontalDivider()
        }
        if (rows.isEmpty()) {
            EmptyText(stringResource(if (entries.isEmpty()) R.string.activity_empty else R.string.activity_empty_filtered))
        } else {
            LazyColumn(Modifier.fillMaxSize()) {
                itemsIndexed(rows, key = { i, e -> "${e.at}|${e.cluster}|${e.action}|$i" }) { _, entry ->
                    ActivityRow(entry, showCluster)
                    HorizontalDivider()
                }
            }
        }
    }
}

@Composable
private fun ActivityRow(entry: ActivityEntry, showCluster: Boolean) {
    val status = LocalStatusColors.current
    ListItem(
        leadingContent = {
            if (entry.failed) {
                Icon(Icons.Outlined.ErrorOutline, stringResource(R.string.activity_filter_failed), tint = status.bad)
            } else {
                Icon(Icons.Outlined.CheckCircle, null, tint = status.ok)
            }
        },
        headlineContent = {
            Text(listOf(entry.actionLabel, entry.target).filter { it.isNotEmpty() }.joinToString(" · "), maxLines = 2, overflow = TextOverflow.Ellipsis)
        },
        supportingContent = {
            Column {
                val where = listOfNotNull(
                    formatDateTime(entry.at),
                    entry.cluster.takeIf { showCluster && it.isNotEmpty() },
                    stringResource(R.string.activity_demo).takeIf { entry.demo },
                )
                Text(where.joinToString(" · "), style = MaterialTheme.typography.bodySmall)
                if (entry.params.isNotEmpty()) {
                    Text(entry.params, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                if (entry.error.isNotEmpty()) {
                    Text(entry.error, style = MaterialTheme.typography.bodySmall, color = status.bad)
                }
            }
        },
    )
}
