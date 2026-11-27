package name.levis.ichor.ui.talosvms

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Refresh
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
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
import kotlinx.coroutines.delay
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.TalosClusterVms
import name.levis.ichor.model.TalosNodeVmsEntry
import name.levis.ichor.model.TalosVm
import name.levis.ichor.ui.LoadingViewModel
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.Loaded
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.design.IchorScaffold
import name.levis.ichor.ui.design.IchorTopAppBar
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.storage.UnsupportedReason
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.vms.VmStateTag

/** The Talos hypervisor's VMs of the cluster, or of [node] alone when set (read-only, os:reader). */
class TalosVmsViewModel(
    private val talos: TalosRepository,
    private val node: String?,
    private val hostname: String,
) : LoadingViewModel<TalosClusterVms>() {
    override suspend fun fetch(): TalosClusterVms =
        if (node == null) talos.clusterVirtualMachines() else TalosClusterVms.ofNode(node, hostname, talos.nodeVirtualMachines(node))

    /** A VM on its way (pending, not yet in its wanted state): worth reading again. */
    val shouldPoll: Boolean get() = (state.value as? UiState.Loaded)?.data?.busy == true

    companion object {
        const val POLL_MILLIS = 5_000L
    }
}

/**
 * The virtual machines the Talos hypervisor (Talos 1.15) runs: counts by state, then one group
 * per node (just [node]'s VMs when set), each VM with its state, resources and first address;
 * one opens its sheet. A node whose Talos lacks the hypervisor says what it needs. Read-only:
 * power, console and editing come later, in the sheet.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TalosVmsScreen(
    node: String?,
    hostname: String,
    onBack: () -> Unit,
    vm: TalosVmsViewModel = viewModel(key = "talos-vms-${node.orEmpty()}", factory = factory { TalosVmsViewModel(app.talosRepository, node, hostname) }),
) {
    val state by vm.state.collectAsStateWithLifecycle()
    LaunchedEffect(Unit) {
        while (true) {
            delay(TalosVmsViewModel.POLL_MILLIS)
            if (vm.shouldPoll && (vm.state.value as? UiState.Loaded)?.refreshing != true) vm.refresh()
        }
    }
    var open by rememberSaveable { mutableStateOf<String?>(null) }
    val loaded = (state as? UiState.Loaded)?.data

    loaded?.nodes?.firstNotNullOfOrNull { entry -> entry.vms.firstOrNull { vmKey(entry, it) == open }?.let { entry to it } }?.let { (entry, machine) ->
        TalosVmSheet(machine, hostname = entry.hostname.ifEmpty { entry.node }, onDismiss = { open = null })
    }

    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.talos_vms_title))
                        if (node != null) Text(hostname, style = MaterialTheme.typography.labelMedium, fontFamily = FontFamily.Monospace)
                    }
                },
                navigationIcon = { BackButton(onBack) },
                actions = { TooltipIconButton(Icons.Outlined.Refresh, stringResource(R.string.common_refresh), onClick = { vm.refresh() }) },
            )
        },
    ) { padding ->
        Loaded(state, vm::refresh, Modifier.pageContent(padding)) { report ->
            TalosVmList(report, grouped = node == null, onOpen = { entry, machine -> open = vmKey(entry, machine) })
        }
    }
}

/** A VM's key across the cluster: two nodes may run VMs of the same name. */
private fun vmKey(entry: TalosNodeVmsEntry, vm: TalosVm) = "${entry.node}/${vm.name}"

@Composable
private fun TalosVmList(report: TalosClusterVms, grouped: Boolean, onOpen: (TalosNodeVmsEntry, TalosVm) -> Unit) {
    val shown = report.shown
    LazyColumn(Modifier.fillMaxSize()) {
        item(key = "counts") {
            TalosVmCountChips(report.all, Modifier.padding(horizontal = 16.dp, vertical = 8.dp))
            HorizontalDivider()
        }
        if (shown.isEmpty()) item(key = "empty") { EmptyText(stringResource(R.string.vm_empty)) }
        shown.forEach { entry -> nodeGroup(entry, grouped, onOpen) }
    }
}

/** One node's header (on the cluster list), what could not be read, then its VMs. */
private fun LazyListScope.nodeGroup(entry: TalosNodeVmsEntry, grouped: Boolean, onOpen: (TalosNodeVmsEntry, TalosVm) -> Unit) {
    item(key = "node-${entry.node}") {
        Column(Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            if (grouped) SectionTitle(entry.hostname.ifEmpty { entry.node })
            when {
                entry.error.isNotEmpty() -> InlineError(stringResource(R.string.vm_read_error, entry.error))
                !entry.supported -> UnsupportedReason(entry.reason)
                entry.errors.isNotEmpty() -> InlineError(stringResource(R.string.vm_read_error, entry.errors.values.joinToString("; ")))
            }
        }
    }
    items(entry.vms, key = { "vm-${vmKey(entry, it)}" }) { machine ->
        TalosVmRow(machine, onClick = { onOpen(entry, machine) })
        HorizontalDivider()
    }
}

/** Name and state, then CPUs, memory and the first address; the error under it, in red. */
@Composable
private fun TalosVmRow(vm: TalosVm, onClick: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().clickable(onClick = onClick).padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(2.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(vm.name, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis, modifier = Modifier.weight(1f))
            VmStateTag(vm.stateLabel, vm.vmState)
        }
        talosVmResources(vm)?.let { resources ->
            MutedText(listOfNotNull(resources, vm.firstAddress).joinToString("  ·  "), maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (vm.error.isNotBlank()) {
            Text(vm.error, style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.bad, maxLines = 2, overflow = TextOverflow.Ellipsis)
        }
    }
}
