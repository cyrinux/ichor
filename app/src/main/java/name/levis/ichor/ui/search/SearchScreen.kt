package name.levis.ichor.ui.search

import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.History
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material.icons.outlined.Schedule
import androidx.compose.material.icons.outlined.Search
import androidx.compose.material.icons.outlined.Sync
import androidx.compose.material.icons.outlined.SyncAlt
import androidx.compose.material.icons.outlined.ViewInAr
import androidx.compose.material.icons.outlined.Widgets
import androidx.compose.material3.DockedSearchBar
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import name.levis.ichor.ui.design.IchorIconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.SearchBarDefaults
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.OVERVIEW
import name.levis.ichor.data.activeSummary
import name.levis.ichor.data.isMeteredNetwork
import name.levis.ichor.model.ClusterOverview
import name.levis.ichor.model.RecentTarget
import name.levis.ichor.model.RecentTargets
import name.levis.ichor.model.Feature
import name.levis.ichor.model.ScreenMatch
import name.levis.ichor.model.SearchGroup
import name.levis.ichor.model.SearchResult
import name.levis.ichor.model.ShareTarget
import name.levis.ichor.model.allows
import name.levis.ichor.model.groupedByCluster
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.isStale
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.rememberClusterLabels
import name.levis.ichor.ui.factory
import name.levis.ichor.util.timeAgo

/**
 * Search across clusters (U3): the nodes, pods, workloads, CronJobs, Argo CD and Flux apps
 * every cluster on the phone last showed, the active cluster first. A result opens like a
 * share link, on its cluster ([onOpen]); a screen of the active cluster opens by its route
 * ([onOpenRoute]). Typing never calls a cluster; the refresh action does.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SearchScreen(onBack: () -> Unit, onOpen: (ShareTarget) -> Unit, onOpenRoute: (String) -> Unit) {
    val context = LocalContext.current
    val app = context.applicationContext as TalosApp
    val vm: SearchViewModel = viewModel(
        factory = factory {
            SearchViewModel(app.searchRepository, app.uiPreferences, app.kubeRepository, app.gitOpsRepository) { isMeteredNetwork(app) }
        },
    )
    val state by vm.state.collectAsStateWithLifecycle()
    val query by vm.query.collectAsStateWithLifecycle()
    val recent by vm.recent.collectAsStateWithLifecycle()
    val recentTargets by vm.recentTargets.collectAsStateWithLifecycle()
    val refreshing by vm.refreshing.collectAsStateWithLifecycle()
    val refreshFailed by vm.refreshFailed.collectAsStateWithLifecycle()
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val labels = rememberClusterLabels()
    val active = config?.activeSummary
    // Reading pods and workloads needs a role allowed the Kubernetes API; the demo has its own.
    val canRefresh = active?.allows(Feature.WORKLOADS) == true && !active.isDemo
    // The active cluster's screens; the nodes as its last overview listed them.
    val nodes = remember(active, state.snapshot) { app.kubeRepository.cached<ClusterOverview>(OVERVIEW)?.value?.nodes.orEmpty() }
    val specs = remember(active, nodes) { active?.let { screenCatalog(it, nodes) }.orEmpty() }
    LaunchedEffect(specs) { vm.setCatalog(screenEntries(specs, context::getString)) }
    val focus = remember { FocusRequester() }
    LaunchedEffect(Unit) { focus.requestFocus() }

    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = { Text(stringResource(R.string.search_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (canRefresh) {
                        TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.search_refresh_active), onClick = vm::refreshActive)
                    }
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            if (refreshing) LinearProgressIndicator(Modifier.fillMaxWidth())
            DockedSearchBar(
                inputField = {
                    SearchBarDefaults.InputField(
                        query = query,
                        onQueryChange = vm::setQuery,
                        onSearch = { vm.remember(it) },
                        expanded = false,
                        onExpandedChange = {},
                        placeholder = { Text(stringResource(R.string.search_hint), maxLines = 1, overflow = TextOverflow.Ellipsis) },
                        leadingIcon = { Icon(Icons.Outlined.Search, contentDescription = null) },
                        trailingIcon = if (query.isNotEmpty()) {
                            { IchorIconButton(onClick = { vm.setQuery("") }) { Icon(Icons.Outlined.Close, stringResource(R.string.search_clear)) } }
                        } else {
                            null
                        },
                        modifier = Modifier.focusRequester(focus),
                    )
                },
                expanded = false,
                onExpandedChange = {},
                modifier = Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
            ) {}
            val snapshot = state.snapshot
            if (snapshot?.othersNeedOfflineCache == true) {
                MutedText(stringResource(R.string.search_offline_needed), modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
            }
            if (refreshFailed) {
                MutedText(stringResource(R.string.search_refresh_failed), modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
            }
            when {
                snapshot == null -> Unit
                query.isBlank() -> {
                    // A removed cluster's objects are not offered; the active one's show no cluster.
                    val contexts = config?.summary?.contexts.orEmpty()
                    val opened = RecentTargets.known(recentTargets, contexts.map { it.clusterId }.filter { it.isNotEmpty() }.toSet())
                    RecentSearches(
                        recent = recent,
                        opened = opened,
                        nothingKnown = snapshot.isEmpty,
                        clusterOf = { cluster ->
                            if (cluster == active?.clusterId) null else contexts.firstOrNull { it.clusterId == cluster }?.let(labels::of)
                        },
                        onPick = vm::setQuery,
                        onForget = vm::forget,
                        onOpen = onOpen,
                        onForgetTarget = vm::forgetTarget,
                    )
                }
                // Typed but not yet ranked: the previous results stay until the new ones.
                state.query.isBlank() -> Unit
                state.results.results.isEmpty() && state.screens.results.isEmpty() -> EmptyText(
                    if (snapshot.isEmpty) stringResource(R.string.search_nothing_known) else stringResource(R.string.search_empty, state.query.trim()),
                )
                else -> Results(
                    groups = state.results.results.groupedByCluster(active?.let { it.clusterId.ifEmpty { it.fingerprint } }),
                    shown = state.results.results.size,
                    total = state.results.total,
                    labelOf = { group -> config?.summary?.contexts?.firstOrNull { it.name == group.context }?.let(labels::of) ?: group.context },
                    onOpen = { target ->
                        vm.remember()
                        onOpen(target)
                    },
                    screens = state.screens.results,
                    specs = remember(specs) { specs.associateBy { it.id } },
                    onOpenRoute = { route ->
                        vm.remember()
                        onOpenRoute(route)
                    },
                )
            }
        }
    }
}

@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun RecentSearches(
    recent: List<String>,
    opened: List<RecentTarget>,
    nothingKnown: Boolean,
    clusterOf: (String) -> String?,
    onPick: (String) -> Unit,
    onForget: (String) -> Unit,
    onOpen: (ShareTarget) -> Unit,
    onForgetTarget: (ShareTarget) -> Unit,
) {
    if (recent.isEmpty() && opened.isEmpty()) {
        if (nothingKnown) EmptyText(stringResource(R.string.search_nothing_known))
        return
    }
    val removeLabel = stringResource(R.string.search_recent_remove)
    LazyColumn(Modifier.fillMaxSize()) {
        recentlyOpened(opened, clusterOf, onOpen, onForgetTarget)
        if (recent.isEmpty()) return@LazyColumn
        item { SectionTitle(stringResource(R.string.search_recent), Modifier.padding(horizontal = 16.dp, vertical = 8.dp)) }
        items(recent, key = { it }) { query ->
            ListItem(
                headlineContent = { Text(query, maxLines = 1, overflow = TextOverflow.Ellipsis) },
                leadingContent = { Icon(Icons.Outlined.History, contentDescription = null) },
                modifier = Modifier.combinedClickable(
                    onClick = { onPick(query) },
                    onLongClickLabel = removeLabel,
                    onLongClick = { onForget(query) },
                ),
            )
        }
    }
}

@Composable
private fun Results(
    groups: List<SearchGroup>,
    shown: Int,
    total: Int,
    labelOf: (SearchGroup) -> String,
    onOpen: (ShareTarget) -> Unit,
    screens: List<ScreenMatch>,
    specs: Map<String, ScreenSpec>,
    onOpenRoute: (String) -> Unit,
) {
    val now = remember(groups) { System.currentTimeMillis() }
    var allScreens by remember(screens) { mutableStateOf(false) }
    LazyColumn(Modifier.fillMaxSize()) {
        screensGroup(screens, specs, allScreens, onExpand = { allScreens = true }, onOpen = onOpenRoute)
        if (total > shown) {
            item { MutedText(stringResource(R.string.search_shown, shown, total), modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
        }
        groups.forEach { group ->
            item(key = "cluster-${group.cluster}") {
                SectionTitle(labelOf(group), Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp))
            }
            // No key: a Kustomization and a HelmRelease (or two workload kinds) may share a name.
            items(group.results) { result ->
                ResultRow(result, now, onOpen)
            }
        }
    }
}

@Composable
private fun ResultRow(result: SearchResult, now: Long, onOpen: (ShareTarget) -> Unit) {
    val target = result.target
    val lastSeen = if (result.isStale(now)) stringResource(R.string.search_last_seen, timeAgo(result.at, now)) else null
    ListItem(
        headlineContent = { Text(result.name, maxLines = 1, overflow = TextOverflow.Ellipsis) },
        supportingContent = {
            Column {
                Text(listOf(result.detail, result.namespace).filter { it.isNotEmpty() }.joinToString(" · "), maxLines = 2, overflow = TextOverflow.Ellipsis)
                lastSeen?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant) }
            }
        },
        leadingContent = { Icon(kindIcon(result.kind), contentDescription = kindLabel(result.kind)) },
        // A result no share link can name only shows.
        modifier = if (target != null) Modifier.clickable { onOpen(target) } else Modifier,
    )
}

internal fun kindIcon(kind: String): ImageVector = when (kind) {
    "node" -> Icons.Outlined.Dns
    "pod" -> Icons.Outlined.ViewInAr
    "workload" -> Icons.Outlined.Widgets
    "cronjob" -> Icons.Outlined.Schedule
    "argo-app" -> Icons.Outlined.Sync
    else -> Icons.Outlined.SyncAlt
}

@Composable
internal fun kindLabel(kind: String): String = when (kind) {
    "node" -> stringResource(R.string.search_kind_node)
    "pod" -> stringResource(R.string.search_kind_pod)
    "workload" -> stringResource(R.string.search_kind_workload)
    "cronjob" -> stringResource(R.string.search_kind_cronjob)
    "argo-app" -> stringResource(R.string.search_kind_argo_app)
    else -> stringResource(R.string.search_kind_flux_app)
}
