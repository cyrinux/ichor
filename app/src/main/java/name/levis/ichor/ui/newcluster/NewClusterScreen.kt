package name.levis.ichor.ui.newcluster

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.JoinRunState
import name.levis.ichor.model.JoinPhase
import name.levis.ichor.model.MaintenanceInspection
import name.levis.ichor.model.NewClusterDraft
import name.levis.ichor.model.installDisks
import name.levis.ichor.model.joinAddress
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.ToggleRow
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorCard
import name.levis.ichor.ui.design.IchorFilterChip
import name.levis.ichor.ui.design.IchorOutlinedButton
import name.levis.ichor.ui.design.IchorScaffold
import name.levis.ichor.ui.design.IchorTopAppBar
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.join.JoinRunView
import name.levis.ichor.ui.join.rememberAuthenticated
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * "New Talos cluster": a machine booted from the Talos ISO on the local network becomes the first
 * control plane of a new cluster. Inspect it, prepare (the secrets stay in the Go core), confirm by
 * typing the cluster's name; the talosconfig is stored, then the run is followed. [onOpenCluster]
 * shows the new cluster once it is up.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NewClusterScreen(
    onBack: () -> Unit,
    onOpenCluster: () -> Unit,
    vm: NewClusterViewModel = viewModel(
        key = "new-cluster",
        factory = factory { NewClusterViewModel(app.nodeLifecycleRepository, app.configRepository, app.joinManager) },
    ),
) {
    val joins = (LocalContext.current.applicationContext as TalosApp).joinManager
    val run by joins.current.collectAsStateWithLifecycle()
    IchorScaffold(
        topBar = {
            IchorTopAppBar(title = { Text(stringResource(R.string.new_cluster_title)) }, navigationIcon = { BackButton(onBack) })
        },
    ) { padding ->
        Column(
            Modifier.pageContent(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            val current = run?.takeIf { it.request.newCluster != null }
            if (current != null) {
                RunSection(current, vm, onOpenCluster)
            } else {
                MutedText(stringResource(R.string.new_cluster_intro))
                MachineSection(vm)
                FormSection(vm)
                DraftSection(vm)
            }
        }
    }
}

@Composable
private fun MachineSection(vm: NewClusterViewModel) {
    val address by vm.address.collectAsStateWithLifecycle()
    val inspection by vm.inspection.collectAsStateWithLifecycle()
    SectionTitle(stringResource(R.string.join_node_step_inspect))
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        OutlinedTextField(
            value = address,
            onValueChange = vm::type,
            label = { Text(stringResource(R.string.join_node_address)) },
            placeholder = { Text(stringResource(R.string.join_node_address_hint)) },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go, autoCorrectEnabled = false),
            keyboardActions = KeyboardActions(onGo = { vm.inspect() }),
            modifier = Modifier.weight(1f),
        )
        IchorButton(onClick = vm::inspect, enabled = inspection != UiState.Loading && joinAddress(address).isNotEmpty()) {
            Text(stringResource(R.string.join_node_inspect))
        }
    }
    MutedText(stringResource(R.string.new_cluster_demo))
    when (val state = inspection) {
        null -> Unit
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(state.message, vm::inspect)
        is UiState.Loaded -> MachineCard(state.data)
    }
}

@Composable
private fun MachineCard(machine: MaintenanceInspection) {
    val colors = LocalStatusColors.current
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(machine.address, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
                if (machine.maintenance) {
                    StatusPill(stringResource(R.string.join_node_maintenance), colors.ok)
                } else {
                    StatusPill(stringResource(R.string.join_node_installed_pill), colors.warn)
                }
            }
            if (machine.maintenance) {
                MutedText(listOf("Talos ${machine.version}", machine.arch, machine.platform).filter { it.isNotBlank() }.joinToString("  ·  "))
            } else {
                Text(stringResource(R.string.join_node_installed, machine.address), style = MaterialTheme.typography.bodyMedium)
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun FormSection(vm: NewClusterViewModel) {
    val inspection by vm.inspection.collectAsStateWithLifecycle()
    val machine = (inspection as? UiState.Loaded)?.data?.takeIf { it.maintenance } ?: return
    val form by vm.form.collectAsStateWithLifecycle()
    val draft by vm.draft.collectAsStateWithLifecycle()
    SectionTitle(stringResource(R.string.new_cluster_step_cluster))
    OutlinedTextField(
        value = form.name,
        onValueChange = vm::name,
        label = { Text(stringResource(R.string.new_cluster_name)) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
    OutlinedTextField(
        value = form.disk,
        onValueChange = vm::disk,
        label = { Text(stringResource(R.string.join_config_disk)) },
        placeholder = { Text(stringResource(R.string.join_config_disk_hint)) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
    val disks = installDisks(machine.disks)
    if (disks.isNotEmpty()) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            disks.forEach { disk ->
                IchorFilterChip(selected = disk == form.disk, onClick = { vm.disk(disk) }, label = { Text(disk, fontFamily = FontFamily.Monospace) })
            }
        }
    }
    OutlinedTextField(
        value = form.kubernetesVersion,
        onValueChange = vm::kubernetesVersion,
        label = { Text(stringResource(R.string.new_cluster_kubernetes)) },
        placeholder = { Text(stringResource(R.string.new_cluster_kubernetes_hint)) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
    ToggleRow(
        title = stringResource(R.string.new_cluster_schedule),
        checked = form.schedule,
        onChange = vm::schedule,
        description = stringResource(R.string.new_cluster_schedule_help),
    )
    IchorButton(onClick = vm::prepare, enabled = form.complete && draft != UiState.Loading, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.new_cluster_prepare))
    }
}

@Composable
private fun DraftSection(vm: NewClusterViewModel) {
    val draft by vm.draft.collectAsStateWithLifecycle()
    val form by vm.form.collectAsStateWithLifecycle()
    val address by vm.address.collectAsStateWithLifecycle()
    val startError by vm.startError.collectAsStateWithLifecycle()
    var confirm by remember { mutableStateOf(false) }
    val authenticated = rememberAuthenticated()
    when (val state = draft) {
        null -> return
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(state.message, vm::prepare)
        is UiState.Loaded -> {
            DraftCard(state.data)
            IchorButton(onClick = { confirm = true }, modifier = Modifier.fillMaxWidth()) {
                Text(stringResource(R.string.new_cluster_create))
            }
            // Why the cluster could not be stored or started, under the button that tried.
            startError?.let { (res, arg) -> Text(stringResource(res, arg), color = LocalStatusColors.current.bad) }
        }
    }
    if (confirm) {
        HostnameConfirmDialog(
            title = stringResource(R.string.new_cluster_confirm_title, form.name),
            hostname = form.name,
            confirmLabel = stringResource(R.string.new_cluster_create),
            onConfirm = {
                confirm = false
                authenticated(form.name) { vm.create() }
            },
            onDismiss = { confirm = false },
            emphasized = true,
        ) {
            Text(
                stringResource(R.string.new_cluster_confirm_body, form.disk, joinAddress(address), form.name),
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
}

@Composable
private fun DraftCard(draft: NewClusterDraft) {
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(stringResource(R.string.new_cluster_versions, draft.talosVersion, draft.kubernetesVersion), style = MaterialTheme.typography.titleSmall)
            draft.warnings.forEach { Text("• $it", style = MaterialTheme.typography.bodySmall, color = LocalStatusColors.current.warn) }
            MutedText(stringResource(R.string.new_cluster_secrets))
            SelectionContainer {
                Box(Modifier.heightIn(max = 360.dp).verticalScroll(rememberScrollState()).horizontalScroll(rememberScrollState())) {
                    Text(draft.redactedControlplane, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, softWrap = false)
                }
            }
        }
    }
}

/** The followed run; a run that sent nothing offers to remove the stored entry it leaves. */
@Composable
private fun RunSection(run: JoinRunState, vm: NewClusterViewModel, onOpenCluster: () -> Unit) {
    val joins = (LocalContext.current.applicationContext as TalosApp).joinManager
    val sent = run.events.any { JoinPhase.of(it.phase) == JoinPhase.INSTALLING }
    val name = run.request.newCluster?.name.orEmpty()
    SectionTitle(stringResource(R.string.new_cluster_step_run))
    JoinRunView(run = run, onStop = joins::stop, onOpenNode = onOpenCluster, onClose = joins::dismiss)
    if (run.finished && run.error != null && !sent && run.request.address != DEMO_MACHINE) {
        MutedText(stringResource(R.string.new_cluster_remove_help))
        IchorOutlinedButton(onClick = { vm.removeEntry(name) {} }, modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.new_cluster_remove))
        }
    }
}
