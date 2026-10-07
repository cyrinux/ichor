package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.isMeteredNetwork
import name.levis.ichor.model.ApiResource
import name.levis.ichor.model.KubePage
import name.levis.ichor.model.KubeScope
import name.levis.ichor.model.PagedLoad
import name.levis.ichor.model.ResourceRow
import name.levis.ichor.model.cellTone
import name.levis.ichor.model.filteredRows
import name.levis.ichor.model.hasWideColumns
import name.levis.ichor.model.rowFields
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.checkup.ageSince
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.DataFreshness
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.workloads.IncompleteNotice
import name.levis.ichor.ui.workloads.KubeListFrame
import name.levis.ichor.ui.workloads.LoadMoreOnScroll
import name.levis.ichor.ui.workloads.NamespacesViewModel
import name.levis.ichor.ui.workloads.PagedListViewModel
import name.levis.ichor.ui.workloads.PagedProgress
import name.levis.ichor.ui.workloads.rememberKubeScope
import name.levis.ichor.ui.components.pageContent

/** The objects of one resource, page by page in the server's order, as its Table shows them. */
class ResourceListViewModel(
    talos: TalosRepository,
    metered: () -> Boolean,
    private val browser: KubeBrowserRepository,
    private val type: ApiResource,
) : PagedListViewModel<ResourceRow>(talos, metered) {
    // In memory only: the browser's lists are not kept offline (no serializer for them).
    override fun key(namespace: String?) = "kube-browser|${type.groupVersion}/${type.resource}|${namespace ?: "*"}"

    override suspend fun page(namespace: String?, token: String): KubePage<ResourceRow> =
        browser.resourcePage(type.group, type.version, type.resource, namespace.takeIf { type.namespaced }, token)
}

/**
 * The objects of [type] like `kubectl get`: the server's columns (the `-o wide` ones behind
 * a toggle) as label/value chips under each name, the namespace picker of the Kubernetes
 * screens for a namespaced kind, more pages on scroll. Tapping an object opens it ([onObject]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ResourceListScreen(
    type: ApiResource,
    onBack: () -> Unit,
    onObject: (ResourceRow) -> Unit,
    vm: ResourceListViewModel = viewModel(
        key = "kube-browser-${type.groupVersion}/${type.resource}",
        factory = factory { ResourceListViewModel(app.talosRepository, { isMeteredNetwork(app) }, app.kubeBrowser, type) },
    ),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val state by vm.state.collectAsStateWithLifecycle()
    var query by rememberSaveable { mutableStateOf("") }
    var wide by rememberSaveable { mutableStateOf(false) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(type.kind, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(type.groupVersion, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        val body = Modifier.pageContent(padding).fillMaxSize()
        val content: @Composable ColumnScope.(UiState.Loaded<PagedLoad<ResourceRow>>) -> Unit = { s ->
            Rows(s, query, wide, onWide = { wide = it }, showNamespace = type.namespaced && vm.scope.namespace == null, vm = vm, onObject = onObject)
        }
        if (type.namespaced) {
            val namespaces: NamespacesViewModel = viewModel(factory = factory { NamespacesViewModel(app.talosRepository) })
            val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
            val control = rememberKubeScope(app, namespaces, mask.enabled)
            LaunchedEffect(control.scope, control.ready) { if (control.ready) vm.setScope(control.scope) }
            KubeListFrame(control, state, { rows -> rows.map { it.namespace }.distinct().sorted() }, query, { query = it }, { vm.refresh() }, body, R.string.kb_search_objects, content)
        } else {
            LaunchedEffect(Unit) { vm.setScope(KubeScope(chosen = true)) }
            Column(body) {
                SearchField(query, { query = it }, stringResource(R.string.kb_search_objects), Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp))
                HorizontalDivider()
                when (val s = state) {
                    UiState.Loading -> LoadingBox(Modifier.weight(1f))
                    is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, Modifier.weight(1f))
                    is UiState.Loaded -> content(s)
                }
            }
        }
    }
}

@Composable
private fun ColumnScope.Rows(
    s: UiState.Loaded<PagedLoad<ResourceRow>>,
    query: String,
    wide: Boolean,
    onWide: (Boolean) -> Unit,
    showNamespace: Boolean,
    vm: ResourceListViewModel,
    onObject: (ResourceRow) -> Unit,
) {
    val progress by vm.progress.collectAsStateWithLifecycle()
    val load = s.data
    val rows = remember(load, query) { load.items.filteredRows(query) }
    val columns = load.items.firstOrNull()?.columns.orEmpty()
    PagedProgress(progress)
    IncompleteNotice(load, searching = query.isNotBlank(), onLoadMore = vm::loadMore, onLoadAll = vm::loadAll)
    if (hasWideColumns(columns)) {
        FilterChip(selected = wide, onClick = { onWide(!wide) }, label = { Text(stringResource(R.string.kb_wide)) }, modifier = Modifier.padding(horizontal = 16.dp))
    }
    val now = remember(load) { System.currentTimeMillis() }
    PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = Modifier.weight(1f)) {
        if (rows.isEmpty()) {
            EmptyText(emptyOrNoMatch(query, R.string.kb_no_objects, R.string.kb_no_object_match))
        } else {
            val listState = rememberLazyListState()
            LoadMoreOnScroll(listState, enabled = load.hasMore && query.isBlank(), loaded = load.items.size, onLoadMore = vm::loadMore)
            LazyColumn(Modifier.fillMaxSize(), state = listState) {
                items(rows, key = { it.key }) { row ->
                    ObjectRow(row, wide, showNamespace, now) { onObject(row) }
                    HorizontalDivider()
                }
            }
        }
    }
    DataFreshness(s, edgeToEdge = false)
}

/** An object: its name first, then namespace and age, then each column as a chip. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ObjectRow(row: ResourceRow, wide: Boolean, showNamespace: Boolean, now: Long, onClick: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val fields = remember(row, wide) { rowFields(row, wide) }
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(row.name, style = MaterialTheme.typography.bodyLarge, fontFamily = FontFamily.Monospace, maxLines = 2, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (row.deleting) ToneLabel(stringResource(R.string.kb_deleting), LocalStatusColors.current.warn)
            if (row.created > 0) Text(ageSince(row.created * 1000, now), style = MaterialTheme.typography.labelSmall, color = muted)
        }
        if (showNamespace && row.namespace.isNotEmpty()) {
            Text(row.namespace, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = muted)
        }
        if (fields.isNotEmpty()) {
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                fields.forEach { f -> FieldChip(f.label, f.value, cellTone(f.label, f.value).color()) }
            }
        }
    }
}
