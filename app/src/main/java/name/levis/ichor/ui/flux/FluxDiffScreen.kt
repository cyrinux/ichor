package name.levis.ichor.ui.flux

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.model.shortFluxRevision
import name.levis.ichor.ui.UiState
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
 * What reconciling a Flux Kustomization now would change, like `flux diff kustomization`: the
 * revisions compared, a count per kind of change, then each object that would change with its
 * diff (the first few unfolded), and the unchanged ones folded. Built in the cluster with a
 * server-side dry run: nothing is written.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun FluxDiffScreen(kind: String, namespace: String, name: String, onBack: () -> Unit) {
    val talos = LocalContext.current.applicationContext as TalosApp
    val vm: FluxDiffViewModel = viewModel(factory = factory { FluxDiffViewModel(talos.talosRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    val config by talos.configRepository.config.collectAsStateWithLifecycle()
    val generation by talos.configRepository.generation.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation) { vm.load(kind, namespace, name, config?.activeContext to generation) }
    val refresh = { vm.refresh(kind, namespace, name) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.flux_diff_title), maxLines = 1)
                        Text(name, style = MaterialTheme.typography.labelMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = refresh) },
            )
        },
    ) { padding ->
        val modifier = Modifier.padding(padding)
        Loaded(state, refresh, modifier, freshness = true) { data ->
            DiffBody(data)
        }
    }
}

/** Unfolded at first: the first objects that would change, so one change shows at a glance. */
private const val UNFOLDED_AT_FIRST = 3

@Composable
private fun DiffBody(diff: FluxDiff) {
    val colors = LocalStatusColors.current
    val muted = MaterialTheme.colorScheme.onSurfaceVariant
    val changed = remember(diff) { diff.changed }
    val unchanged = remember(diff) { diff.unchanged }
    // Per object, by key: what the user folded or unfolded survives a refresh.
    val expanded = remember { mutableStateMapOf<String, Boolean>() }
    var showUnchanged by remember { mutableStateOf(false) }

    LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
        item(key = "summary") {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                if (diff.revision.isNotEmpty()) InfoRow(stringResource(R.string.flux_diff_built), shortFluxRevision(diff.revision), mono = true)
                if (diff.applied.isNotEmpty()) {
                    InfoRow(stringResource(R.string.flux_diff_applied), shortFluxRevision(diff.applied), mono = true, valueColor = if (diff.newRevision) colors.warn else Color.Unspecified)
                }
                if (diff.inSync) {
                    Text(stringResource(R.string.flux_diff_in_sync), style = MaterialTheme.typography.bodyMedium, color = colors.ok)
                }
                DiffCounts(diff.counts)
                diff.warnings.forEach { Text(it, style = MaterialTheme.typography.bodySmall, color = colors.warn) }
            }
        }
        itemsIndexed(changed, key = { _, r -> r.key }) { index, r ->
            val open = expanded[r.key] ?: (index < UNFOLDED_AT_FIRST && r.change.isChange)
            DiffResourceCard(r, open) { expanded[r.key] = !open }
        }
        if (unchanged.isNotEmpty()) {
            item(key = "unchanged-fold") {
                FoldRow(pluralStringResource(R.plurals.flux_diff_unchanged, unchanged.size, unchanged.size), showUnchanged) { showUnchanged = !showUnchanged }
            }
            if (showUnchanged) item(key = "unchanged") { UnchangedList(unchanged) }
        }
        item(key = "footnote") {
            Text(stringResource(R.string.flux_diff_dry_run), style = MaterialTheme.typography.labelSmall, color = muted)
        }
    }
}
