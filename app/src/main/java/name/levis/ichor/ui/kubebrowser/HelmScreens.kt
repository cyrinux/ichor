package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.PrimaryScrollableTabRow
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.KubeBrowserRepository
import name.levis.ichor.model.HelmRelease
import name.levis.ichor.model.HelmReleaseDetail
import name.levis.ichor.model.HelmRevision
import name.levis.ichor.model.filteredReleases
import name.levis.ichor.model.helmStatusTone
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.checkup.ageSince
import name.levis.ichor.ui.components.AppTab
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SkeletonStyle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.emptyOrNoMatch
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.workloads.KubeFilters
import name.levis.ichor.ui.workloads.NamespacesViewModel
import name.levis.ichor.ui.workloads.rememberKubeScope
import name.levis.ichor.ui.workloads.scopeError

/** The Helm releases of a namespace (null for every one), their latest revision each. */
class HelmReleasesViewModel(private val browser: KubeBrowserRepository) : LoadingViewModel<List<HelmRelease>>() {
    private var namespace: String? = null
    private var started = false

    fun setNamespace(namespace: String?) {
        if (started && namespace == this.namespace) return
        started = true
        this.namespace = namespace
        refresh(reset = true)
    }

    override suspend fun fetch() = browser.helmReleases(namespace).releases
}

class HelmReleaseViewModel(private val browser: KubeBrowserRepository, private val namespace: String, private val name: String) :
    LoadingViewModel<HelmReleaseDetail>() {
    /** Rolling the release back to an older revision; reloads it once attempted. */
    val rollback = HelmRollbackController(browser, namespace, name, viewModelScope) { refresh() }

    override suspend fun fetch() = browser.helmRelease(namespace, name)
}

@Composable
private fun statusColor(status: String): Color = helmStatusTone(status).color().takeIf { it != Color.Unspecified } ?: LocalStatusColors.current.muted

/**
 * The Helm releases, like `helm list`, in the namespace the Kubernetes screens list: chart,
 * versions, revision and status. Read-only: tapping one shows its values, notes, manifest
 * and history ([onRelease]).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HelmReleasesScreen(
    onBack: () -> Unit,
    onRelease: (HelmRelease) -> Unit,
    vm: HelmReleasesViewModel = viewModel(factory = factory { HelmReleasesViewModel(app.kubeBrowser) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val state by vm.state.collectAsStateWithLifecycle()
    val namespaces: NamespacesViewModel = viewModel(factory = factory { NamespacesViewModel(app.talosRepository) })
    val mask by app.uiPreferences.privacyMask.collectAsStateWithLifecycle()
    val control = rememberKubeScope(app, namespaces, mask.enabled)
    LaunchedEffect(control.scope, control.ready) { if (control.ready) vm.setNamespace(control.scope.namespace) }
    var query by rememberSaveable { mutableStateOf("") }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.kb_helm_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Column(Modifier.pageContent(padding).fillMaxSize()) {
            val loaded = (state as? UiState.Loaded)?.data
            val listed = remember(loaded) { loaded.orEmpty().map { it.namespace }.distinct().sorted() }
            KubeFilters(control, listed, query, { query = it }, R.string.kb_helm_search)
            HorizontalDivider()
            val rest = Modifier.weight(1f)
            val s = state
            when {
                !control.ready -> EmptyText(stringResource(R.string.kube_scope_type_prompt), rest)
                s is UiState.Failed -> ErrorBox(scopeError(s.message, control.scope), { vm.refresh() }, rest)
                s is UiState.Loaded -> PullToRefreshBox(isRefreshing = s.refreshing, onRefresh = { vm.refresh() }, modifier = rest) {
                    val rows = remember(s.data, query) { s.data.filteredReleases(query) }
                    val now = remember(s.data) { System.currentTimeMillis() }
                    if (rows.isEmpty()) {
                        EmptyText(emptyOrNoMatch(query, R.string.kb_helm_empty, R.string.kb_helm_no_match))
                    } else {
                        LazyColumn(Modifier.fillMaxSize()) {
                            items(rows, key = { it.key }) { r ->
                                ReleaseRow(r, showNamespace = control.scope.namespace == null, now) { onRelease(r) }
                                HorizontalDivider()
                            }
                        }
                    }
                }
                else -> LoadingBox(rest)
            }
        }
    }
}

@Composable
private fun ReleaseRow(r: HelmRelease, showNamespace: Boolean, now: Long, onClick: () -> Unit) {
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    Column(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 10.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(r.name, style = MaterialTheme.typography.bodyLarge, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            ToneLabel(r.status, statusColor(r.status))
        }
        Text(
            listOfNotNull(r.namespace.takeIf { showNamespace }, "${r.chart} ${r.chartVersion}", r.appVersion.takeIf { it.isNotEmpty() }?.let { stringResource(R.string.kb_helm_app_version, it) })
                .joinToString("  ·  "),
            style = MaterialTheme.typography.labelSmall,
            color = muted,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            stringResource(R.string.kb_helm_revision_age, r.revision, ageSince(r.updated * 1000, now)),
            style = MaterialTheme.typography.labelSmall,
            color = muted,
        )
    }
}

private enum class ReleaseTab(val label: Int) {
    SUMMARY(R.string.kb_helm_tab_summary),
    VALUES(R.string.kb_helm_tab_values),
    NOTES(R.string.kb_helm_tab_notes),
    MANIFEST(R.string.kb_helm_tab_manifest),
}

/** One release: what is deployed and its history, the values set, the chart's notes and the rendered manifest. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HelmReleaseScreen(
    namespace: String,
    name: String,
    onBack: () -> Unit,
    vm: HelmReleaseViewModel = viewModel(key = "helm-$namespace/$name", factory = factory { HelmReleaseViewModel(app.kubeBrowser, namespace, name) }),
) {
    val context = LocalContext.current
    val state by vm.state.collectAsStateWithLifecycle()
    val rollback by vm.rollback.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    HelmRollbackHost(vm.rollback, rollback)
    var tab by rememberSaveable { mutableIntStateOf(0) }
    val detail = (state as? UiState.Loaded)?.data
    val shown = ReleaseTab.entries[tab]
    val text = when (shown) {
        ReleaseTab.VALUES -> detail?.values
        ReleaseTab.NOTES -> detail?.notes
        ReleaseTab.MANIFEST -> detail?.manifest
        ReleaseTab.SUMMARY -> null
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(name, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(namespace, style = MaterialTheme.typography.labelSmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = {
                    if (!text.isNullOrEmpty()) {
                        // Values may hold credentials: kept out of the clipboard preview.
                        TooltipIconButton(Icons.Outlined.ContentCopy, stringResource(R.string.kb_copy), onClick = { copyWithToast(context, name, text, sensitive = true) })
                    }
                    TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() })
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).fillMaxSize()) {
            PrimaryScrollableTabRow(selectedTabIndex = tab, edgePadding = 0.dp) {
                ReleaseTab.entries.forEachIndexed { i, t -> AppTab(selected = tab == i, onClick = { tab = i }, text = { Text(stringResource(t.label)) }) }
            }
            when (val s = state) {
                UiState.Loading -> LoadingBox(style = SkeletonStyle.TEXT)
                is UiState.Failed -> ErrorBox(s.message, { vm.refresh() })
                is UiState.Loaded -> when (shown) {
                    ReleaseTab.SUMMARY -> ReleaseSummary(s.data, onRollback = vm.rollback::open)
                    ReleaseTab.NOTES -> if (s.data.notes.isBlank()) EmptyText(stringResource(R.string.kb_helm_no_notes)) else PlainText(s.data.notes)
                    ReleaseTab.VALUES -> if (s.data.values.isBlank()) EmptyText(stringResource(R.string.kb_helm_no_values)) else YamlLines(s.data.values, Modifier.weight(1f))
                    ReleaseTab.MANIFEST -> YamlLines(s.data.manifest, Modifier.weight(1f))
                }
            }
        }
    }
}

@Composable
private fun PlainText(text: String) {
    SelectionContainer {
        Text(
            text,
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
        )
    }
}

@Composable
private fun ReleaseSummary(d: HelmReleaseDetail, onRollback: (revision: Int) -> Unit) {
    val now = remember(d) { System.currentTimeMillis() }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            ToneLabel(d.status, statusColor(d.status))
            MutedText(stringResource(R.string.kb_helm_revision_age, d.revision, ageSince(d.updated * 1000, now)))
        }
        InfoRow(stringResource(R.string.kb_helm_chart), "${d.chart} ${d.chartVersion}", mono = true)
        if (d.appVersion.isNotEmpty()) InfoRow(stringResource(R.string.kb_helm_app), d.appVersion, mono = true)
        if (d.description.isNotEmpty()) MutedText(d.description)
        Text(stringResource(R.string.kb_helm_history), style = MaterialTheme.typography.titleSmall, modifier = Modifier.padding(top = 8.dp))
        // The current revision is what runs: rolling back to it would change nothing.
        d.history.forEach { r -> RevisionRow(r, now, onRollback = if (r.revision != d.revision) ({ onRollback(r.revision) }) else null) }
        MutedText(stringResource(R.string.helm_rollback_footer), Modifier.padding(top = 8.dp))
    }
}

@Composable
private fun RevisionRow(r: HelmRevision, now: Long, onRollback: (() -> Unit)?) {
    Column(Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text("#${r.revision}", style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            ToneLabel(r.status, statusColor(r.status))
            MutedText(stringResource(R.string.kube_events_ago, ageSince(r.updated * 1000, now)), Modifier.weight(1f), maxLines = 1, overflow = TextOverflow.Ellipsis)
            if (onRollback != null) {
                TextButton(onClick = onRollback, contentPadding = PaddingValues(horizontal = 8.dp), modifier = Modifier.heightIn(min = 32.dp)) {
                    Text(stringResource(R.string.helm_rollback_action), style = MaterialTheme.typography.labelMedium)
                }
            }
        }
        if (r.description.isNotEmpty()) MutedText(r.description, maxLines = 3, overflow = TextOverflow.Ellipsis)
    }
}
