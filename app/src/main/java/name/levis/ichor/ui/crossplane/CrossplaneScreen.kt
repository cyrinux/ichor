package name.levis.ichor.ui.crossplane

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.Crossplane
import name.levis.ichor.model.CrossplaneFilter
import name.levis.ichor.model.CrossplaneAction
import name.levis.ichor.model.CrossplaneObject
import name.levis.ichor.model.actionKey
import name.levis.ichor.model.CrossplanePackage
import name.levis.ichor.model.CrossplaneDefinition
import name.levis.ichor.model.KubeObjectRef
import name.levis.ichor.model.filtered
import name.levis.ichor.model.problems
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SearchField
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory

/** One read of the cluster's Crossplane control plane: packages, definitions, resources. */
class CrossplaneViewModel(private val browser: KubeBrowserRepository) : LoadingViewModel<Crossplane>() {
    override suspend fun fetch() = browser.crossplane()

    /** Pause, resume and reconcile requests; a paused object shows so in a fresh reading. */
    val actions = CrossplaneActions(viewModelScope, browser, onChanged = { refresh() })
}

/** The action picked on an object, waiting for its confirmation (the object itself: labels repeat across kinds). */
private data class PendingAction(val obj: CrossplaneObject, val action: CrossplaneAction)

/**
 * Crossplane (os:admin): its packages, the APIs its XRDs define, and every composite, claim and
 * managed resource with its Ready and Synced state, problems first. A tap opens the object's
 * summary ([onObject]); a composite or managed resource's menu pauses, resumes or reconciles it.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun CrossplaneScreen(
    onBack: () -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    vm: CrossplaneViewModel = viewModel(factory = factory { CrossplaneViewModel(app.kubeBrowser) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    var query by rememberSaveable { mutableStateOf("") }
    var filter by rememberSaveable { mutableStateOf(CrossplaneFilter.ALL) }
    var pending by remember { mutableStateOf<PendingAction?>(null) }
    val busy by vm.actions.busy.collectAsStateWithLifecycle()
    CrossplaneActionToasts(vm.actions.results)

    IchorScaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.crossplane_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        val rest = Modifier.pageContent(padding).fillMaxSize()
        when (val s = state) {
            is UiState.Failed -> ErrorBox(s.message, { vm.refresh() }, rest)
            is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = rest) {
                CrossplaneList(s.data, filter, { filter = it }, query, { query = it }, onObject, busy) { o, action -> pending = PendingAction(o, action) }
                pending?.let { p ->
                    CrossplaneConfirm(p.obj, p.action, composite = p.obj in s.data.composites, onConfirm = {
                        pending = null
                        vm.actions.run(p.obj, p.action)
                    }, onDismiss = { pending = null })
                }
            }
            else -> LoadingBox(rest)
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun CrossplaneList(
    xp: Crossplane,
    filter: CrossplaneFilter,
    onFilter: (CrossplaneFilter) -> Unit,
    query: String,
    onQuery: (String) -> Unit,
    onObject: (KubeObjectRef) -> Unit,
    busy: Set<String>,
    onAction: (CrossplaneObject, CrossplaneAction) -> Unit,
) {
    // A claim is changed through its composite: no actions on it.
    val menu: (CrossplaneObject) -> (@Composable () -> Unit)? = { o ->
        if (o in xp.claims) null else ({ CrossplaneActionsMenu(o, busy = o.actionKey in busy) { onAction(o, it) } })
    }
    if (xp.error.isNotEmpty()) {
        MutedText(stringResource(R.string.crossplane_partial, xp.error), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
    }
    if (!xp.installed || xp.isEmpty()) {
        EmptyText(stringResource(R.string.crossplane_empty))
        return
    }

    LazyColumn(Modifier.fillMaxSize()) {
        item(key = "header") { CrossplaneHeader(xp) }
        item(key = "filters") {
            Column {
                SearchField(query, onQuery, stringResource(R.string.crossplane_search), Modifier.padding(horizontal = 16.dp, vertical = 4.dp))
                FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp), modifier = Modifier.padding(horizontal = 16.dp)) {
                    CrossplaneFilter.entries.forEach { f ->
                        FilterChip(selected = filter == f, onClick = { onFilter(f) }, label = { Text(filterLabel(f)) })
                    }
                }
                HorizontalDivider(Modifier.padding(top = 8.dp))
            }
        }
        val problems = xp.problems
        if (problems.isNotEmpty()) {
            item(key = "problems-title") { SectionTitle(stringResource(R.string.crossplane_problems), Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
            items(problems.size, key = { "problem:$it" }) { i ->
                ObjectRow(problems[i], showKind = true, onClick = { onObject(problems[i].ref()) }, actions = menu(problems[i]))
                HorizontalDivider()
            }
        }
        if (xp.packages.isNotEmpty()) {
            item(key = "packages-title") { SectionTitle(stringResource(R.string.crossplane_packages), Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
            items(xp.packages.size, key = { "pkg:$it" }) { i -> PackageRow(xp.packages[i], onClick = { onObject(xp.packages[i].ref()) }); HorizontalDivider() }
        }
        if (xp.definitions.isNotEmpty()) {
            item(key = "definitions-title") { SectionTitle(stringResource(R.string.crossplane_definitions), Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
            items(xp.definitions.size, key = { "def:$it" }) { i -> DefinitionRow(xp.definitions[i], onClick = { onObject(xp.definitions[i].ref()) }); HorizontalDivider() }
        }
        val composites = xp.composites.filtered(filter, query)
        val claims = xp.claims.filtered(filter, query)
        val managed = xp.managed.filtered(filter, query)
        if (composites.isNotEmpty() || claims.isNotEmpty()) {
            item(key = "composites-title") { SectionTitle(stringResource(R.string.crossplane_composites), Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
            items(composites.size, key = { "xr:$it" }) { i -> ObjectRow(composites[i], showKind = true, onClick = { onObject(composites[i].ref()) }, actions = menu(composites[i])); HorizontalDivider() }
            items(claims.size, key = { "claim:$it" }) { i -> ObjectRow(claims[i], showKind = true, onClick = { onObject(claims[i].ref()) }); HorizontalDivider() }
        }
        if (managed.isNotEmpty()) {
            item(key = "managed-title") { SectionTitle(stringResource(R.string.crossplane_managed), Modifier.padding(horizontal = 16.dp, vertical = 4.dp)) }
            items(managed.size, key = { "mr:$it" }) { i -> ObjectRow(managed[i], showKind = true, onClick = { onObject(managed[i].ref()) }, actions = menu(managed[i])); HorizontalDivider() }
        }
        if (composites.isEmpty() && claims.isEmpty() && managed.isEmpty() && xp.packages.isEmpty() && xp.definitions.isEmpty()) {
            item(key = "no-match") { EmptyText(emptyOrNoMatch(query, R.string.crossplane_empty, R.string.crossplane_no_match)) }
        }
    }
}

@Composable
private fun filterLabel(f: CrossplaneFilter): String = when (f) {
    CrossplaneFilter.ALL -> stringResource(R.string.crossplane_filter_all)
    CrossplaneFilter.CRITICAL -> stringResource(R.string.crossplane_filter_critical)
    CrossplaneFilter.WARNING -> stringResource(R.string.crossplane_filter_warning)
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun CrossplaneHeader(xp: Crossplane) {
    Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            AssistCount(stringResource(R.string.crossplane_count_critical, xp.counts.critical), critical = xp.counts.critical > 0)
            AssistCount(stringResource(R.string.crossplane_count_warning, xp.counts.warning), critical = false)
            AssistCount(stringResource(R.string.crossplane_count_ok, xp.counts.ok), critical = false)
        }
        MutedText(stringResource(R.string.crossplane_managed_count, xp.managed.size, xp.managedTotal))
        if (xp.truncated) MutedText(stringResource(R.string.crossplane_truncated))
    }
}

/** Nothing to list: the reader found the control plane empty (or only unreadable sections). */
private fun Crossplane.isEmpty(): Boolean =
    packages.isEmpty() && definitions.isEmpty() && composites.isEmpty() && claims.isEmpty() && managed.isEmpty()
