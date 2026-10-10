package name.levis.ichor.ui.join

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.data.activeSummary
import name.levis.ichor.model.MaintenanceInspection
import name.levis.ichor.model.isDemo
import name.levis.ichor.model.joinAddress
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.app
import name.levis.ichor.ui.components.BackButton
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.pageContent
import name.levis.ichor.ui.factory
import name.levis.ichor.ui.hardware.DiskRow
import name.levis.ichor.ui.hardware.SystemRows
import name.levis.ichor.ui.network.LinkRow
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf

/** Step 1 of adding a node: the typed address and what the node in maintenance mode answered. */
class JoinNodeViewModel(private val talos: TalosRepository) : ViewModel() {
    private val _address = MutableStateFlow("")
    val address: StateFlow<String> = _address.asStateFlow()
    private val _inspection = MutableStateFlow<UiState<MaintenanceInspection>?>(null)
    val inspection: StateFlow<UiState<MaintenanceInspection>?> = _inspection.asStateFlow()
    private var inspecting: Job? = null

    fun type(address: String) {
        _address.value = address
    }

    fun inspect() {
        val address = joinAddress(_address.value).ifEmpty { return }
        inspecting?.cancel()
        _inspection.value = UiState.Loading
        inspecting = viewModelScope.launch { _inspection.value = uiStateOf { talos.maintenanceNodeInspect(address) } }
    }
}

/**
 * "Add a node…": a machine booted from the Talos ISO waits in maintenance mode; type the address
 * its console shows and see its version, disks and links. Generating its config and applying it
 * are the next steps (placeholders for now).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun JoinNodeScreen(
    onBack: () -> Unit,
    vm: JoinNodeViewModel = viewModel(key = "join-node", factory = factory { JoinNodeViewModel(app.talosRepository) }),
) {
    val app = LocalContext.current.applicationContext as TalosApp
    val config by app.configRepository.config.collectAsStateWithLifecycle()
    val address by vm.address.collectAsStateWithLifecycle()
    val inspection by vm.inspection.collectAsStateWithLifecycle()
    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(stringResource(R.string.join_node_title))
                        Text(config?.activeContext.orEmpty(), style = MaterialTheme.typography.labelMedium)
                    }
                },
                navigationIcon = { BackButton(onBack) },
            )
        },
    ) { padding ->
        Column(
            Modifier.pageContent(padding).fillMaxSize().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            SectionTitle(stringResource(R.string.join_node_step_inspect))
            MutedText(stringResource(R.string.join_node_intro))
            if (config?.activeSummary?.isDemo == true) InfoNotice(stringResource(R.string.join_node_demo))
            AddressField(address, onAddress = vm::type, onInspect = vm::inspect, busy = inspection == UiState.Loading)
            when (val state = inspection) {
                null -> Unit
                UiState.Loading -> LoadingBox()
                is UiState.Failed -> ErrorBox(state.message, vm::inspect)
                is UiState.Loaded -> InspectionView(state.data)
            }
            NextSteps()
        }
    }
}

@Composable
private fun AddressField(address: String, onAddress: (String) -> Unit, onInspect: () -> Unit, busy: Boolean) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        OutlinedTextField(
            value = address,
            onValueChange = onAddress,
            label = { Text(stringResource(R.string.join_node_address)) },
            placeholder = { Text(stringResource(R.string.join_node_address_hint)) },
            singleLine = true,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go, autoCorrectEnabled = false),
            keyboardActions = KeyboardActions(onGo = { onInspect() }),
            modifier = Modifier.weight(1f),
        )
        Button(onClick = onInspect, enabled = !busy && joinAddress(address).isNotEmpty()) {
            Text(stringResource(R.string.join_node_inspect))
        }
    }
}

@Composable
private fun InspectionView(node: MaintenanceInspection) {
    val colors = LocalStatusColors.current
    if (!node.maintenance) {
        Card(Modifier.fillMaxWidth()) {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                StatusPill(stringResource(R.string.join_node_installed_pill), colors.warn)
                Text(stringResource(R.string.join_node_installed, node.address), style = MaterialTheme.typography.bodyMedium)
            }
        }
        return
    }
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(node.address, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace, modifier = Modifier.weight(1f))
                StatusPill(stringResource(R.string.join_node_maintenance), colors.ok)
            }
            MutedText(listOf("Talos ${node.version}", node.arch, node.platform).filter { it.isNotBlank() }.joinToString("  ·  "))
            node.system?.let { SystemRows(it) }
        }
    }
    Section(stringResource(R.string.hardware_disks), node.errors["disks"], node.disks.isEmpty()) {
        node.disks.forEach { DiskRow(it) }
    }
    Section(stringResource(R.string.network_links), node.errors["links"], node.links.isEmpty()) {
        node.links.forEach { link ->
            LinkRow(link)
            node.addressesOn(link.name).forEach { Text(it, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace) }
        }
    }
}

/** A card with a title, the section's error if it failed, "none" when empty, else [content]. */
@Composable
private fun Section(title: String, error: String?, empty: Boolean, content: @Composable () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            SectionTitle(title)
            when {
                error != null -> Text(error, color = LocalStatusColors.current.bad, style = MaterialTheme.typography.bodySmall)
                empty -> MutedText(stringResource(R.string.hardware_none))
                else -> content()
            }
        }
    }
}

/** Steps 2 and 3, shipped by the next changes. */
@Composable
private fun NextSteps() {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text(stringResource(R.string.join_node_step_config), style = MaterialTheme.typography.bodyMedium)
            Text(stringResource(R.string.join_node_step_apply), style = MaterialTheme.typography.bodyMedium)
            MutedText(stringResource(R.string.join_node_coming))
        }
    }
}
