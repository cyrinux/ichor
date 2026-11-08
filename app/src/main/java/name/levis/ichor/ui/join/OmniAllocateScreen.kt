package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.RadioButtonUnchecked
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.JoinRequest
import name.levis.ichor.data.NodeLifecycleRepository
import name.levis.ichor.model.OmniAllocateSpec
import name.levis.ichor.model.OmniMachine
import name.levis.ichor.model.OmniMachineSet
import name.levis.ichor.model.OmniMachineSets
import name.levis.ichor.model.omniLabelsJson
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ClusterScreenTitle
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadFailureRow
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionCard
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorListItem
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorScaffold
import name.levis.ichor.ui.design.IchorTopAppBar
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.uiStateOf

/** The role and the labels a new machine of Omni registers with, and what the classes ask for. */
class OmniLabelsViewModel(private val lifecycle: NodeLifecycleRepository) : ViewModel() {
    private val _role = MutableStateFlow("worker")
    val role: StateFlow<String> = _role.asStateFlow()
    private val _extra = MutableStateFlow("")
    private val _labelsJson = MutableStateFlow(omniLabelsJson("worker", ""))
    val labelsJson: StateFlow<String> = _labelsJson.asStateFlow()
    private val _classLabels = MutableStateFlow<List<String>>(emptyList())
    val classLabels: StateFlow<List<String>> = _classLabels.asStateFlow()

    init {
        viewModelScope.launch {
            // Only a hint next to the field: the card works without it.
            _classLabels.value = runCatching { lifecycle.omniMachineSets().machineSets.flatMap { it.matchLabels }.distinct() }.getOrDefault(emptyList())
        }
    }

    fun setRole(role: String) {
        _role.value = role
        _labelsJson.value = omniLabelsJson(role, _extra.value)
    }

    /** Applied on request: each change signs a new join config. */
    fun applyExtra(extra: String) {
        _extra.value = extra
        _labelsJson.value = omniLabelsJson(_role.value, extra)
    }
}

/** On Omni's Boot a new machine: the role, more labels, and what the cluster's machine classes match. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun OmniLabelsCard(vm: OmniLabelsViewModel) {
    val role by vm.role.collectAsStateWithLifecycle()
    val classLabels by vm.classLabels.collectAsStateWithLifecycle()
    var extra by rememberSaveable { mutableStateOf("") }
    SectionCard(stringResource(R.string.omni_labels_title)) {
        MutedText(stringResource(R.string.omni_labels_intro))
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            IchorFilterChip(selected = role == "worker", onClick = { vm.setRole("worker") }, label = { Text(stringResource(R.string.omni_role_worker)) })
            IchorFilterChip(selected = role == "controlplane", onClick = { vm.setRole("controlplane") }, label = { Text(stringResource(R.string.omni_role_controlplane)) })
        }
        OutlinedTextField(
            value = extra,
            onValueChange = { extra = it },
            label = { Text(stringResource(R.string.omni_labels_extra)) },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        if (classLabels.isNotEmpty()) MutedText(stringResource(R.string.omni_labels_classes, classLabels.joinToString("; ")))
        IchorOutlinedButton(onClick = { vm.applyExtra(extra) }) { Text(stringResource(R.string.omni_labels_apply)) }
    }
}

/** What the allocation screen lists. */
data class OmniAllocateData(val sets: OmniMachineSets, val machines: List<OmniMachine>)

class OmniAllocateViewModel(private val lifecycle: NodeLifecycleRepository) : ViewModel() {
    private val _data = MutableStateFlow<UiState<OmniAllocateData>>(UiState.Loading)
    val data: StateFlow<UiState<OmniAllocateData>> = _data.asStateFlow()

    init {
        load()
    }

    fun load() {
        _data.value = UiState.Loading
        viewModelScope.launch {
            _data.value = uiStateOf { OmniAllocateData(lifecycle.omniMachineSets(), lifecycle.omniUnallocatedMachines()) }
        }
    }
}

/**
 * "Allocate in Omni": the machines no cluster has and the cluster's machine sets. The confirmation
 * is plain for a worker and the typed host name for a control plane, which changes etcd's quorum;
 * the followed run's timeline then replaces the form.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun OmniAllocateScreen(
    onBack: () -> Unit,
    vm: OmniAllocateViewModel = viewModel(key = "omni-allocate", factory = factory { OmniAllocateViewModel(app.nodeLifecycleRepository) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val data by vm.data.collectAsStateWithLifecycle()
    val run by app.joinManager.current.collectAsStateWithLifecycle()
    IchorScaffold(
        topBar = {
            IchorTopAppBar(
                title = { ClusterScreenTitle(stringResource(R.string.omni_allocate_title)) },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Column(
            Modifier.pageContent(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            val current = run
            if (current?.request?.omniAllocate != null) {
                JoinRunView(
                    current,
                    onStop = app.joinManager::stop,
                    onOpenNode = null,
                    onClose = {
                        app.joinManager.dismiss()
                        vm.load()
                    },
                )
            } else {
                MutedText(stringResource(R.string.omni_allocate_intro))
                when (val state = data) {
                    UiState.Loading -> LoadingBox()
                    is UiState.Failed -> LoadFailureRow(state.message, vm::load)
                    is UiState.Loaded -> AllocateForm(state.data, busy = current?.running == true, onReload = vm::load) { app.joinManager.start(it) }
                }
            }
        }
    }
}

@Composable
private fun AllocateForm(data: OmniAllocateData, busy: Boolean, onReload: () -> Unit, onStart: (JoinRequest) -> Unit) {
    var machineId by rememberSaveable { mutableStateOf("") }
    var setId by rememberSaveable { mutableStateOf("") }
    val machine = data.machines.firstOrNull { it.id == machineId }
    val set = data.sets.machineSets.firstOrNull { it.id == setId }
    SectionCard(stringResource(R.string.omni_allocate_machines)) {
        if (data.machines.isEmpty()) InfoNotice(stringResource(R.string.omni_allocate_none))
        data.machines.forEach { m -> MachineRow(m, m.id == machineId) { machineId = m.id } }
        IchorOutlinedButton(onClick = onReload) { Text(stringResource(R.string.common_refresh)) }
    }
    SectionCard(stringResource(R.string.omni_allocate_sets)) {
        data.sets.machineSets.forEach { s -> SetRow(s, s.id == setId) { setId = s.id } }
    }
    if (machine != null && set != null) AllocateAction(machine, set, busy, onStart)
}

@Composable
private fun MachineRow(machine: OmniMachine, selected: Boolean, onSelect: () -> Unit) {
    IchorListItem(
        headlineContent = { Text(machine.name, fontFamily = FontFamily.Monospace) },
        supportingContent = {
            val state = listOfNotNull(
                machine.addresses.firstOrNull(),
                machine.talosVersion.ifBlank { null },
                stringResource(R.string.omni_allocate_maintenance).takeIf { machine.maintenance },
                stringResource(R.string.omni_allocate_offline).takeIf { !machine.connected },
            )
            Text(state.joinToString(" · "), style = MaterialTheme.typography.bodySmall)
        },
        leadingContent = { SelectedMark(selected) },
        modifier = Modifier.selectable(selected, role = Role.RadioButton, onClick = onSelect),
    )
}

@Composable
private fun SetRow(set: OmniMachineSet, selected: Boolean, onSelect: () -> Unit) {
    IchorListItem(
        headlineContent = { Text(set.id, fontFamily = FontFamily.Monospace) },
        overlineContent = { Text(stringResource(if (set.isControlPlane) R.string.omni_role_controlplane else R.string.omni_role_worker)) },
        supportingContent = {
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(allocationText(set), style = MaterialTheme.typography.bodySmall)
                if (set.matchLabels.isNotEmpty()) {
                    Text(stringResource(R.string.omni_allocate_needs, set.matchLabels.joinToString("; ")), style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace)
                }
            }
        },
        leadingContent = { SelectedMark(selected) },
        modifier = Modifier.selectable(selected, role = Role.RadioButton, onClick = onSelect),
    )
}

@Composable
private fun allocationText(set: OmniMachineSet): String = when (set.allocation.type) {
    "static" -> stringResource(R.string.omni_allocate_set_static, set.allocation.className)
    "unlimited" -> stringResource(R.string.omni_allocate_set_unlimited, set.allocation.className)
    else -> stringResource(R.string.omni_allocate_set_none)
}

@Composable
private fun SelectedMark(selected: Boolean) {
    // The row's selectable state says it to a screen reader.
    Icon(if (selected) Icons.Outlined.CheckCircle else Icons.Outlined.RadioButtonUnchecked, contentDescription = null)
}

@Composable
private fun AllocateAction(machine: OmniMachine, set: OmniMachineSet, busy: Boolean, onStart: (JoinRequest) -> Unit) {
    var confirm by remember { mutableStateOf(false) }
    val authenticated = rememberAuthenticated()
    val start = {
        confirm = false
        authenticated(machine.name) {
            onStart(
                JoinRequest(
                    address = "", template = "", role = set.role, hostname = machine.name, disk = "", staticAddress = "",
                    omni = true, omniAllocate = OmniAllocateSpec(machine.id, set.id),
                ),
            )
        }
    }
    IchorButton(onClick = { confirm = true }, enabled = !busy, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.omni_allocate_action))
    }
    if (!confirm) return
    val title = stringResource(R.string.omni_allocate_confirm_title, machine.name)
    if (set.isControlPlane) {
        HostnameConfirmDialog(
            title = title,
            hostname = machine.name,
            confirmLabel = stringResource(R.string.omni_allocate_action),
            onConfirm = start,
            onDismiss = { confirm = false },
            emphasized = true,
        ) {
            Text(stringResource(R.string.omni_allocate_confirm_controlplane, machine.name, set.id), style = MaterialTheme.typography.bodyMedium)
        }
    } else {
        ConfirmDialog(
            title = title,
            text = stringResource(R.string.omni_allocate_confirm_worker, machine.name, set.id),
            confirm = stringResource(R.string.omni_allocate_action),
            onConfirm = start,
            onDismiss = { confirm = false },
        )
    }
}
