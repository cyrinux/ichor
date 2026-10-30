package name.levis.ichor.ui.join

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import name.levis.ichor.ui.design.IchorButton
import name.levis.ichor.ui.design.IchorCard
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.NodeLifecycleRepository
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.JoinConfigPreview
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.joinRole
import name.levis.ichor.model.joinTemplates
import name.levis.ichor.model.suggestedHostname
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.util.formatBytes

/** What the user chose for the new node's config. [template] is a member's address. */
data class JoinConfigForm(
    val template: String = "",
    val role: String = "",
    val hostname: String = "",
    val disk: String = "",
    val address: String = "",
) {
    val complete: Boolean get() = template.isNotBlank() && hostname.isNotBlank() && disk.isNotBlank()
}

/** Step 2 of adding a node: a member's config made the new node's, previewed with its secrets hidden. */
class JoinConfigViewModel(private val talos: TalosRepository, private val lifecycle: NodeLifecycleRepository) : ViewModel() {
    private val _members = MutableStateFlow<UiState<List<NodeOverview>>>(UiState.Loading)
    val members: StateFlow<UiState<List<NodeOverview>>> = _members.asStateFlow()
    private val _form = MutableStateFlow(JoinConfigForm())
    val form: StateFlow<JoinConfigForm> = _form.asStateFlow()
    private val _preview = MutableStateFlow<UiState<JoinConfigPreview>?>(null)
    val preview: StateFlow<UiState<JoinConfigPreview>?> = _preview.asStateFlow()
    private var previewing: Job? = null

    init {
        loadMembers()
    }

    fun loadMembers() {
        _members.value = UiState.Loading
        viewModelScope.launch { _members.value = uiStateOf { talos.overview().nodes } }
    }

    /** Picks [node] as the template: its role is the new node's, and the host name follows it unless typed. */
    fun pick(node: NodeOverview, members: List<NodeOverview>) = edit { form ->
        val role = joinRole(node.role)
        val suggested = suggestedHostname(form.role, members.map { it.hostname })
        val hostname = if (form.hostname.isBlank() || form.hostname == suggested) suggestedHostname(role, members.map { it.hostname }) else form.hostname
        form.copy(template = node.node, role = role, hostname = hostname)
    }

    fun hostname(value: String) = edit { it.copy(hostname = value.trim()) }

    fun disk(value: String) = edit { it.copy(disk = value.trim()) }

    fun address(value: String) = edit { it.copy(address = value.trim()) }

    fun generate() {
        val form = _form.value.takeIf { it.complete } ?: return
        previewing?.cancel()
        _preview.value = UiState.Loading
        previewing = viewModelScope.launch {
            _preview.value = uiStateOf { lifecycle.joinNodeConfigPreview(form.template, form.role, form.hostname, form.disk, form.address) }
        }
    }

    /** Any change makes the shown preview stale. */
    private fun edit(change: (JoinConfigForm) -> JoinConfigForm) {
        _form.update(change)
        previewing?.cancel()
        _preview.value = null
    }
}

/** Step 2's card. [disks] are the new node's writable disks from step 1, offered as chips. */
@Composable
fun JoinConfigStep(vm: JoinConfigViewModel, disks: List<String>) {
    val members by vm.members.collectAsStateWithLifecycle()
    val form by vm.form.collectAsStateWithLifecycle()
    val preview by vm.preview.collectAsStateWithLifecycle()
    SectionTitle(stringResource(R.string.join_node_step_config))
    MutedText(stringResource(R.string.join_config_intro))
    when (val state = members) {
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(state.message, vm::loadMembers)
        is UiState.Loaded -> {
            val nodes = state.data
            TemplatePicker(joinTemplates(nodes), form.template) { vm.pick(it, nodes) }
            if (form.template.isNotBlank()) {
                ConfigFields(form, disks, vm)
                IchorButton(onClick = vm::generate, enabled = form.complete && preview != UiState.Loading, modifier = Modifier.fillMaxWidth()) {
                    Text(stringResource(R.string.join_config_preview))
                }
            }
        }
    }
    when (val state = preview) {
        null -> Unit
        UiState.Loading -> LoadingBox()
        is UiState.Failed -> ErrorBox(state.message, vm::generate)
        is UiState.Loaded -> PreviewCard(state.data)
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun TemplatePicker(templates: List<NodeOverview>, selected: String, onPick: (NodeOverview) -> Unit) {
    if (templates.isEmpty()) {
        InfoNotice(stringResource(R.string.join_config_no_template))
        return
    }
    Text(stringResource(R.string.join_config_template), style = MaterialTheme.typography.labelLarge)
    FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        templates.forEach { node ->
            FilterChip(
                selected = node.node == selected,
                onClick = { onPick(node) },
                label = { Text("${node.hostname.ifBlank { node.node }} · ${roleLabel(joinRole(node.role))}") },
            )
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun ConfigFields(form: JoinConfigForm, disks: List<String>, vm: JoinConfigViewModel) {
    MutedText(stringResource(R.string.join_config_role, roleLabel(form.role)))
    OutlinedTextField(
        value = form.hostname,
        onValueChange = vm::hostname,
        label = { Text(stringResource(R.string.join_config_hostname)) },
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
    if (disks.isNotEmpty()) {
        FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            disks.forEach { disk ->
                FilterChip(selected = disk == form.disk, onClick = { vm.disk(disk) }, label = { Text(disk, fontFamily = FontFamily.Monospace) })
            }
        }
    }
    OutlinedTextField(
        value = form.address,
        onValueChange = vm::address,
        label = { Text(stringResource(R.string.join_config_address)) },
        placeholder = { Text(stringResource(R.string.join_config_address_hint)) },
        supportingText = { Text(stringResource(R.string.join_config_address_help)) },
        singleLine = true,
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
}

@Composable
private fun PreviewCard(preview: JoinConfigPreview) {
    IchorCard(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Text(stringResource(R.string.join_config_valid, preview.hostname, formatBytes(preview.size.toLong())), style = MaterialTheme.typography.titleSmall)
            preview.warnings.forEach { Text("• $it", style = MaterialTheme.typography.bodySmall) }
            MutedText(stringResource(R.string.join_config_secrets))
            SelectionContainer {
                Box(Modifier.heightIn(max = 360.dp).verticalScroll(rememberScrollState()).horizontalScroll(rememberScrollState())) {
                    Text(preview.redactedYaml, style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, softWrap = false)
                }
            }
        }
    }
}

@Composable
private fun roleLabel(role: String) = when (role) {
    "controlplane" -> stringResource(R.string.overview_role_control_plane)
    else -> role
}
