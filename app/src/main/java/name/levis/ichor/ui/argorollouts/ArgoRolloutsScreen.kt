package name.levis.ichor.ui.argorollouts

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
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTopAppBar
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
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.repeatOnLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.data.ArgoRolloutsRepository
import name.levis.ichor.model.ArgoRollout
import name.levis.ichor.model.ArgoRolloutsStatus
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.HealthDot
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory

class ArgoRolloutsViewModel(private val repository: ArgoRolloutsRepository) : LoadingViewModel<ArgoRolloutsStatus>() {
    /** Polls until then (epoch millis) after an action, even before the rollout shows a change. */
    private var boostUntil = 0L

    val actions = ArgoRolloutActions(viewModelScope, repository, onChanged = {
        boostUntil = System.currentTimeMillis() + BOOST_MILLIS
        refresh()
    })

    override suspend fun fetch() = repository.rollouts()

    /** Whether the list is worth polling: a rollout moving or an analysis deciding, or an action just run. */
    val shouldPoll: Boolean
        get() = System.currentTimeMillis() < boostUntil || (state.value as? UiState.Loaded)?.data?.needsPolling == true

    companion object {
        const val POLL_MILLIS = 3_000L
        private const val BOOST_MILLIS = 15_000L
    }
}

/**
 * The Argo Rollouts of the cluster by namespace, the worst first in each: strategy, phase,
 * the canary's steps and weight, and why it is paused. One opens its sheet with promote,
 * promote fully, abort, retry and pause. Polls every 3 s while a rollout moves.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ArgoRolloutsScreen(
    onBack: () -> Unit,
    vm: ArgoRolloutsViewModel = viewModel(factory = factory { ArgoRolloutsViewModel(app.argoRolloutsRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val busy by vm.actions.busy.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    ArgoRolloutPolling(vm)
    ArgoRolloutResultToasts(vm.actions.results)
    var open by rememberSaveable { mutableStateOf<String?>(null) }
    val loaded = (state as? UiState.Loaded)?.data

    loaded?.rollouts?.firstOrNull { it.key == open }?.let {
        ArgoRolloutSheet(it, busy = it.key in busy, actions = vm.actions, onDismiss = { open = null })
    }

    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = { Text(stringResource(R.string.argo_rollouts_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Loaded(state, vm::refresh, Modifier.pageContent(padding)) { status ->
            when {
                !status.installed -> EmptyText(stringResource(R.string.argo_rollouts_not_installed))
                status.rollouts.isEmpty() -> EmptyText(stringResource(R.string.argo_rollouts_empty))
                else -> RolloutList(status, busy, onOpen = { open = it.key })
            }
        }
    }
}

/** While the screen is started, polls every 3 s when [ArgoRolloutsViewModel.shouldPoll] says so. */
@Composable
private fun ArgoRolloutPolling(vm: ArgoRolloutsViewModel) {
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    LaunchedEffect(vm, lifecycle) {
        lifecycle.repeatOnLifecycle(Lifecycle.State.STARTED) {
            while (true) {
                delay(ArgoRolloutsViewModel.POLL_MILLIS)
                if (vm.shouldPoll && (vm.state.value as? UiState.Loaded)?.refreshing != true) vm.refresh()
            }
        }
    }
}

@Composable
private fun RolloutList(status: ArgoRolloutsStatus, busy: Set<String>, onOpen: (ArgoRollout) -> Unit) {
    val groups = remember(status) { status.byNamespace }
    LazyColumn(Modifier.fillMaxSize()) {
        groups.forEach { (namespace, rollouts) ->
            item(key = "ns:$namespace") {
                SectionTitle(namespace, Modifier.padding(start = 16.dp, end = 16.dp, top = 12.dp, bottom = 4.dp))
            }
            items(rollouts, key = { it.key }) { rollout ->
                RolloutRow(rollout, rollout.key in busy, onClick = { onOpen(rollout) })
                HorizontalDivider()
            }
        }
    }
}

/** Health, name and strategy; the phase with the step; the step bar; why it is paused. */
@Composable
private fun RolloutRow(rollout: ArgoRollout, busy: Boolean, onClick: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            HealthDot(rollout.serviceHealth)
            Text(rollout.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (busy) MutedText(stringResource(R.string.vm_action_running))
            StrategyTag(rollout)
        }
        MutedText(listOfNotNull(rollout.phase.ifEmpty { null }, argoRolloutStepText(rollout)).joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        if (rollout.steps.isNotEmpty()) ArgoRolloutStepBar(rollout)
        ArgoRolloutPauseLine(rollout)
    }
}
