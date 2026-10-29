package name.levis.ichor.ui.updates

import android.widget.Toast
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.ClusterUpdates
import name.levis.ichor.model.UpdateRow
import name.levis.ichor.model.updateSections
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyState
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.readableWidth
import name.levis.ichor.ui.factory

/**
 * The active cluster's Updates: its images and charts with a newer patch, minor or major
 * version, what already bumps them, and those not comparable with why. The first visit on a
 * cluster asks once before the registries are asked. A candidate's chip opens its file
 * ([onOpen]) with the quick edit prefilled, then the pull request.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun UpdatesScreen(onBack: () -> Unit, onOpen: (UpdateOpen) -> Unit) {
    val context = LocalContext.current
    val talos = context.applicationContext as TalosApp
    val vm: UpdatesViewModel = viewModel(factory = factory { UpdatesViewModel(talos.updatesRepository) })
    val state by vm.state.collectAsStateWithLifecycle()
    val open by vm.open.collectAsStateWithLifecycle()
    val config by talos.configRepository.config.collectAsStateWithLifecycle()
    val generation by talos.configRepository.generation.collectAsStateWithLifecycle()
    LaunchedEffect(config?.activeContext, generation) { vm.load(config?.activeContext to generation) }
    LaunchedEffect(open) {
        val next = open ?: return@LaunchedEffect
        vm.consumeOpen()
        onOpen(next)
    }
    val rendered = stringResource(R.string.updates_rendered)
    LaunchedEffect(state.renderedHint) {
        if (!state.renderedHint) return@LaunchedEffect
        vm.consumeRenderedHint()
        Toast.makeText(context, rendered, Toast.LENGTH_LONG).show()
    }
    if (state.needsConsent) {
        ConfirmDialog(
            title = stringResource(R.string.updates_consent_title),
            text = stringResource(R.string.updates_consent_body),
            confirm = stringResource(R.string.updates_consent_continue),
            onConfirm = vm::consent,
            onDismiss = onBack,
        )
    }
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.updates_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = vm::refresh) },
            )
        },
    ) { padding ->
        val modifier = Modifier.padding(padding)
        val updates = state.updates
        when {
            state.needsConsent -> Unit
            updates is UiState.Loading -> AskingLine(state.lastQueried, modifier)
            else -> Loaded(updates, vm::refresh, modifier) { data ->
                UpdatesList(data, state, onPick = vm::pick)
            }
        }
    }
}

/** The progress line while the registries are asked (one Go call: no per-row placeholder). */
@Composable
private fun AskingLine(queried: Int, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        LinearProgressIndicator(Modifier.fillMaxWidth())
        MutedText(if (queried > 0) stringResource(R.string.updates_asking_count, queried) else stringResource(R.string.updates_asking))
    }
}

@Composable
private fun UpdatesList(data: ClusterUpdates, state: UpdatesUiState, onPick: (UpdateRow, String) -> Unit) {
    val sections = remember(data) { updateSections(data) }
    LazyColumn(Modifier.fillMaxSize().readableWidth(), contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp)) {
        if (data.lookups.failed > 0) {
            item { MutedText(stringResource(R.string.updates_lookups_failed, data.lookups.failed, data.lookups.queried)) }
        }
        items(data.skipped, key = { "skipped:${it.what}" }) { MutedText(stringResource(R.string.updates_skipped, it.what, it.reason)) }
        if (sections.images.isEmpty() && sections.charts.isEmpty()) {
            item { EmptyState(Icons.Outlined.CheckCircle, stringResource(R.string.updates_empty)) }
        }
        section(R.string.updates_section_images, sections.images, state, onPick)
        section(R.string.updates_section_charts, sections.charts, state, onPick)
        section(R.string.updates_section_not_comparable, sections.notComparable, state, onPick)
    }
}

private fun LazyListScope.section(title: Int, rows: List<UpdateRow>, state: UpdatesUiState, onPick: (UpdateRow, String) -> Unit) {
    if (rows.isEmpty()) return
    item(key = "title:$title") { SectionTitle(stringResource(title)) }
    items(rows, key = { "$title:${it.key}" }) { row ->
        UpdateRowItem(row, resolving = state.resolving == row.key, note = state.notes[row.key], onPick = { onPick(row, it) })
        HorizontalDivider()
    }
}
