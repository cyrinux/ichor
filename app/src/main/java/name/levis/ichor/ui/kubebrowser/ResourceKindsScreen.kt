package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.ApiResource
import name.levis.ichor.model.ApiResourceList
import name.levis.ichor.model.groupResources
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.components.pageContent

class ResourceKindsViewModel(private val browser: KubeBrowserRepository) : LoadingViewModel<ApiResourceList>() {
    override suspend fun fetch() = browser.apiResources()
}

/**
 * Every kind the API server serves, CRDs included, like `kubectl api-resources`: searchable
 * by kind, resource, short name ("deploy") or group, grouped by API group with the built-in
 * ones first. Tapping one lists its objects ([onKind]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ResourceKindsScreen(
    onBack: () -> Unit,
    onKind: (ApiResource) -> Unit,
    vm: ResourceKindsViewModel = viewModel(factory = factory { ResourceKindsViewModel(app.kubeBrowser) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    var query by rememberSaveable { mutableStateOf("") }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.kb_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = vm::refresh) },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            SearchField(query, { query = it }, stringResource(R.string.kb_search_kinds), Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp))
            when (val s = state) {
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(s.message, vm::refresh)
                is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = vm::refresh, modifier = Modifier.weight(1f)) {
                    KindList(s.data, query, onKind)
                }
            }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun KindList(list: ApiResourceList, query: String, onKind: (ApiResource) -> Unit) {
    val groups = remember(list, query) { groupResources(list.resources, query) }
    LazyColumn(Modifier.fillMaxSize()) {
        if (list.failed.isNotEmpty()) {
            item(key = "failed") {
                InfoNotice(stringResource(R.string.kb_failed_groups, list.failed.joinToString(", ")), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
            }
        }
        if (groups.isEmpty()) {
            item(key = "empty") { EmptyText(emptyOrNoMatch(query, R.string.kb_no_kinds, R.string.kb_no_kind_match)) }
        }
        groups.forEach { group ->
            stickyHeader(key = "g-${group.group}") {
                Surface(color = MaterialTheme.colorScheme.surface, modifier = Modifier.fillMaxWidth()) {
                    Text(
                        group.group.ifEmpty { stringResource(R.string.kb_core_group) },
                        style = MaterialTheme.typography.titleSmall,
                        color = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp),
                    )
                }
            }
            items(group.resources, key = { "r-${it.key}" }) { r ->
                KindRow(r) { onKind(r) }
                HorizontalDivider()
            }
        }
    }
}

/** A kind: its name, resource and version, short names, and whether it lives in namespaces. */
@Composable
private fun KindRow(r: ApiResource, onClick: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(r.kind, style = MaterialTheme.typography.bodyLarge, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(
                listOf(r.resource, r.groupVersion).joinToString("  ·  "),
                style = MaterialTheme.typography.labelSmall,
                fontFamily = FontFamily.Monospace,
                color = muted,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        if (r.shortNames.isNotEmpty()) ToneLabel(r.shortNames.joinToString(","), muted, mono = true)
        ToneLabel(
            stringResource(if (r.namespaced) R.string.kb_namespaced else R.string.kb_cluster_scoped),
            if (r.namespaced) MaterialTheme.colorScheme.primary else LocalStatusColors.current.muted,
        )
    }
}
