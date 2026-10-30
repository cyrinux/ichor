package name.levis.ichor.ui.vms

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorScaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.data.VirtualMachineRepository
import name.levis.ichor.model.VirtualMachine
import name.levis.ichor.model.VirtualMachines
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.theme.LocalStatusColors

class VirtualMachinesViewModel(private val repository: VirtualMachineRepository) : LoadingViewModel<VirtualMachines>() {
    /** Polls until then (epoch millis) after an action, even before the VM shows a change. */
    var boostUntil = 0L
        private set

    val actions = VmActions(viewModelScope, repository, onChanged = {
        boostUntil = System.currentTimeMillis() + BOOST_MILLIS
        refresh()
    })

    override suspend fun fetch() = repository.virtualMachines()

    /** Whether the list is worth polling: a VM on its way, or an action just asked. */
    val shouldPoll: Boolean
        get() = System.currentTimeMillis() < boostUntil || (state.value as? UiState.Loaded)?.data?.busy == true

    companion object {
        const val POLL_MILLIS = 5_000L
        private const val BOOST_MILLIS = 20_000L
    }
}

private const val KUBEVIRT_SITE = "https://kubevirt.io"

/**
 * KubeVirt's (and Harvester's) virtual machines: counts by state, failing and changing ones
 * first, each with its node, address, guest OS and any import or migration in flight; one opens
 * its sheet with the start/stop/restart/pause/migrate actions. Polls while a VM is on its way.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VirtualMachinesScreen(
    onBack: () -> Unit,
    onConsole: (VirtualMachine) -> Unit,
    vm: VirtualMachinesViewModel = viewModel(factory = factory { VirtualMachinesViewModel(app.vmRepository) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    val busy by vm.actions.busy.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) { if (state == UiState.Loading) vm.refresh() }
    LaunchedEffect(Unit) {
        while (true) {
            delay(VirtualMachinesViewModel.POLL_MILLIS)
            if (vm.shouldPoll && (vm.state.value as? UiState.Loaded)?.refreshing != true) vm.refresh()
        }
    }
    VmResultToasts(vm.actions.results)
    var namespace by rememberSaveable { mutableStateOf<String?>(null) }
    var open by rememberSaveable { mutableStateOf<String?>(null) }
    val loaded = (state as? UiState.Loaded)?.data

    loaded?.vms?.firstOrNull { it.key == open }?.let {
        VirtualMachineSheet(it, busy = it.key in busy, actions = vm.actions, onConsole = onConsole, onDismiss = { open = null })
    }

    IchorScaffold(
        topBar = {
            TopAppBar(
                title = { Text(stringResource(if (loaded?.harvester == true) R.string.vm_title_harvester else R.string.vm_title)) },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Loaded(state, vm::refresh, Modifier.pageContent(padding)) { report ->
            if (report.installed) {
                VmList(report, namespace, busy, onNamespace = { namespace = it }, onOpen = { open = it.key })
            } else {
                NotInstalled(report.error)
            }
        }
    }
}

/** No KubeVirt here: what it would show, and where to read about it; not an error. */
@Composable
private fun NotInstalled(error: String) {
    val uriHandler = LocalUriHandler.current
    Column(Modifier.fillMaxWidth().padding(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(stringResource(R.string.vm_not_installed), style = MaterialTheme.typography.titleMedium)
        MutedText(stringResource(R.string.vm_not_installed_hint))
        TextButton(onClick = { runCatching { uriHandler.openUri(KUBEVIRT_SITE) } }) { Text("kubevirt.io") }
        if (error.isNotEmpty()) InlineError(stringResource(R.string.vm_read_error, error))
    }
}

@Composable
private fun VmList(report: VirtualMachines, namespace: String?, busy: Set<String>, onNamespace: (String?) -> Unit, onOpen: (VirtualMachine) -> Unit) {
    val selected = namespace?.takeIf { it in report.namespaces }
    val rows = remember(report, selected) { report.inNamespace(selected) }
    LazyColumn(Modifier.fillMaxSize()) {
        item(key = "counts") {
            Column(Modifier.padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                CountChips(report, Modifier.padding(horizontal = 16.dp))
                if (report.error.isNotEmpty()) InlineError(stringResource(R.string.vm_read_error, report.error), Modifier.padding(horizontal = 16.dp))
                if (report.namespaces.size > 1) {
                    LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        item {
                            FilterChip(selected = selected == null, onClick = { onNamespace(null) }, label = { Text(stringResource(R.string.workloads_all_namespaces)) })
                        }
                        items(report.namespaces, key = { it }) { ns ->
                            FilterChip(selected = selected == ns, onClick = { onNamespace(ns) }, label = { Text(ns, fontFamily = FontFamily.Monospace) })
                        }
                    }
                }
            }
            HorizontalDivider()
        }
        if (rows.isEmpty()) item(key = "empty") { EmptyText(stringResource(R.string.vm_empty)) }
        items(rows, key = { it.key }) { machine ->
            VmRow(machine, machine.key in busy, onClick = { onOpen(machine) })
            HorizontalDivider()
        }
    }
}

/** Running, stopped, failing, migrating and importing counts; only the non-zero ones. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun CountChips(report: VirtualMachines, modifier: Modifier = Modifier) {
    val colors = LocalStatusColors.current
    val c = report.counts
    val chips = listOf(
        Triple(c.running, R.string.vm_count_running, colors.ok),
        Triple(c.stopped, R.string.vm_count_stopped, colors.muted),
        Triple(c.failing, R.string.vm_count_failing, colors.bad),
        Triple(c.migrating, R.string.vm_count_migrating, colors.warn),
        Triple(c.importing, R.string.vm_count_importing, colors.warn),
    ).filter { it.first > 0 }
    if (chips.isEmpty()) return
    FlowRow(modifier, horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        chips.forEach { (count, label, color) -> Tag(stringResource(label, count), color) }
    }
}

/** Name and status, then namespace, node, first address and guest OS, then what is in flight. */
@Composable
private fun VmRow(vm: VirtualMachine, busy: Boolean, onClick: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(vm.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            if (busy) MutedText(stringResource(R.string.vm_action_running))
            VmStatusBadge(vm)
        }
        MutedText(listOf(vm.namespace, vm.node, vm.firstIp.orEmpty(), vm.guestOS).filter { it.isNotEmpty() }.joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        vmProgressText(vm.progress)?.let { Text(it, style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.warn) }
    }
}
