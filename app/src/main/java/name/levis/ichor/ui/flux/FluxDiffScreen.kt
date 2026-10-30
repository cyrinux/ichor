package name.levis.ichor.ui.flux

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.SnackbarResult
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.text.style.TextOverflow
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.model.KubeDiffResource
import name.levis.ichor.model.QuickEdit
import name.levis.ichor.model.isFindableChange
import name.levis.ichor.model.shortFluxRevision
import name.levis.ichor.model.shortRevisions
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.AllowLandscape
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.InfoRow
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.diff.DiffCounts
import name.levis.ichor.ui.diff.DiffResourceCard
import name.levis.ichor.ui.diff.FoldRow
import name.levis.ichor.ui.diff.UnchangedList
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * What reconciling a Flux Kustomization (or syncing an Argo CD Application, [tool]) now would
 * change, like `flux diff kustomization` or `argocd app diff`: the revisions compared, a count
 * per kind of change, then each object that would change with its diff (the first few
 * unfolded), and the unchanged ones folded. Built in the cluster with a server-side dry run:
 * nothing is written. [onOpenSource]: each object gets "Open source file" when the app has a
 * readable Git source, opening the file of source `index` that declares it at `line`.
 * [onKeepLive]: an object changed by hand in the cluster gets "Keep in Git", opening the editor
 * of its manifest with the live values written, reviewed, then proposed as a pull request.
 * [focus] ("kind/namespace/name", from an object's page) scrolls to that object, unfolded.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FluxDiffScreen(
    kind: String,
    namespace: String,
    name: String,
    onBack: () -> Unit,
    tool: DiffTool = DiffTool.FLUX,
    onOpenSource: ((index: Int, path: String, line: Int) -> Unit)? = null,
    onKeepLive: ((index: Int, path: String, edit: QuickEdit) -> Unit)? = null,
    onAddToken: (() -> Unit)? = null,
    focus: String = "",
) {
    AllowLandscape()
    val context = LocalContext.current
    val talos = context.applicationContext as TalosApp
    val vm: FluxDiffViewModel = viewModel(key = tool.name, factory = factory { FluxDiffViewModel(talos.gitOpsRepository, tool) })
    val finder: DiffSourceViewModel = viewModel(
        key = "source-${tool.name}",
        factory = factory { DiffSourceViewModel(talos.gitOpsRepository, tool.sourceTool, kind, namespace, name) },
    )
    val state by vm.state.collectAsStateWithLifecycle()
    val sourceIndex by finder.sourceIndex.collectAsStateWithLifecycle()
    val searching by finder.searching.collectAsStateWithLifecycle()
    val outcome by finder.outcome.collectAsStateWithLifecycle()
    val config by talos.configRepository.config.collectAsStateWithLifecycle()
    val generation by talos.configRepository.generation.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation) {
        vm.load(kind, namespace, name, config?.activeContext to generation)
        if (onOpenSource != null || onKeepLive != null) finder.load(config?.activeContext to generation)
    }
    val refresh = { vm.refresh(kind, namespace, name) }
    val snackbar = remember { SnackbarHostState() }
    val addToken = stringResource(R.string.gitops_source_add_token)
    LaunchedEffect(outcome) {
        when (val o = outcome) {
            is SourceFindOutcome.Open -> {
                if (o.keep != null) onKeepLive?.invoke(o.index, o.path, QuickEdit.keep(o.documentIndex, o.keep))
                else onOpenSource?.invoke(o.index, o.path, o.line)
                finder.consumed()
            }
            is SourceFindOutcome.Message -> {
                // Consumed once shown: consuming first would cancel this effect, and the snackbar with it.
                val action = if (o.addToken && onAddToken != null) addToken else null
                val result = snackbar.showSnackbar(o.text.resolve(context), actionLabel = action, withDismissAction = action != null)
                if (result == SnackbarResult.ActionPerformed) onAddToken?.invoke()
                finder.consumed()
            }
            is SourceFindOutcome.Choose, null -> Unit
        }
    }
    (outcome as? SourceFindOutcome.Choose)?.let { choose ->
        SourceMatchChooser(
            choose.matches,
            onPick = { m ->
                finder.consumed()
                if (choose.keep != null) onKeepLive?.invoke(choose.index, m.path, QuickEdit.keep(m.documentIndex, choose.keep))
                else onOpenSource?.invoke(choose.index, m.path, m.line)
            },
            onDismiss = finder::consumed,
        )
    }
    val find: ((KubeDiffResource) -> Unit)? = if (onOpenSource != null && sourceIndex != null) ({ finder.find(it) }) else null
    val keep: ((KubeDiffResource) -> Unit)? = if (onKeepLive != null && sourceIndex != null) ({ finder.find(it, keep = true) }) else null

    IchorScaffold(
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.flux_diff_title), maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(name, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = refresh) },
            )
        },
    ) { padding ->
        val modifier = Modifier.padding(padding)
        Loaded(state, refresh, modifier, freshness = true) { data ->
            DiffBody(data, tool, find, keep, searching, focus)
        }
    }
}

/** Unfolded at first: the first objects that would change, so one change shows at a glance. */
private const val UNFOLDED_AT_FIRST = 3

@Composable
private fun DiffBody(
    diff: FluxDiff,
    tool: DiffTool,
    onFind: ((KubeDiffResource) -> Unit)?,
    onKeep: ((KubeDiffResource) -> Unit)?,
    searching: String?,
    focus: String,
) {
    val colors = LocalStatusColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val changed = remember(diff) { diff.changed }
    val unchanged = remember(diff) { diff.unchanged }
    // Per object, by key: what the user folded or unfolded survives a refresh.
    val expanded = remember { mutableStateMapOf<String, Boolean>() }
    var showUnchanged by remember { mutableStateOf(false) }
    val list = rememberLazyListState()
    val focused = if (focus.isEmpty()) -1 else changed.indexOfFirst { it.focusKey == focus }
    // Once: the focused object below the summary (item 0); a refresh leaves the scroll alone.
    var scrolled by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(focused) {
        if (focused >= 0 && !scrolled) {
            list.scrollToItem(focused + 1)
            scrolled = true
        }
    }

    LazyColumn(Modifier.fillMaxSize(), state = list, contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item(key = "summary") {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                val short = { rev: String -> if (tool == DiffTool.ARGO) shortRevisions(rev) else shortFluxRevision(rev) }
                if (diff.revision.isNotEmpty()) InfoRow(stringResource(R.string.flux_diff_built), short(diff.revision), mono = true)
                if (diff.applied.isNotEmpty()) {
                    InfoRow(stringResource(R.string.flux_diff_applied), short(diff.applied), mono = true, valueColor = if (diff.newRevision) colors.warn else Color.Unspecified)
                }
                if (diff.inSync) {
                    Text(stringResource(R.string.flux_diff_in_sync), style = MaterialTheme.typography.bodyMedium, color = colors.ok)
                }
                // Opened for one object that has nothing to change: say so, rather than leave it to be looked for.
                if (focus.isNotEmpty() && focused < 0) {
                    val (kind, _, name) = (focus.split('/') + listOf("", "", "")).take(3)
                    Text(stringResource(R.string.gitops_focus_in_sync, kind, name), style = MaterialTheme.typography.bodyMedium, color = colors.ok)
                }
                DiffCounts(diff.counts)
                diff.warnings.forEach { Text(it, style = MaterialTheme.typography.bodySmall, color = colors.warn) }
            }
        }
        itemsIndexed(changed, key = { _, r -> r.key }) { index, r ->
            val open = expanded[r.key] ?: (index == focused || index < UNFOLDED_AT_FIRST && r.change.isChange)
            DiffResourceCard(
                r,
                open,
                onToggle = { expanded[r.key] = !open },
                onOpenSource = if (onFind != null && isFindableChange(r.change)) ({ onFind(r) }) else null,
                onKeepLive = if (onKeep != null && r.liveChanges.isNotEmpty()) ({ onKeep(r) }) else null,
                searching = searching == r.key,
            )
        }
        if (unchanged.isNotEmpty()) {
            item(key = "unchanged-fold") {
                FoldRow(pluralStringResource(R.plurals.flux_diff_unchanged, unchanged.size, unchanged.size), showUnchanged) { showUnchanged = !showUnchanged }
            }
            if (showUnchanged) item(key = "unchanged") { UnchangedList(unchanged) }
        }
        item(key = "footnote") {
            Text(stringResource(if (tool == DiffTool.ARGO) R.string.argo_diff_dry_run else R.string.flux_diff_dry_run), style = MaterialTheme.typography.labelSmall, color = muted)
        }
    }
}
