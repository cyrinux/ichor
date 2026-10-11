package name.levis.ichor.ui.machineconfig

import androidx.annotation.StringRes
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ConfigSnippet
import name.levis.ichor.model.SnippetParam
import name.levis.ichor.model.byGroup
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.components.ErrorBox
import name.levis.ichor.ui.components.LoadingBox
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle

/**
 * The ready-made config changes: pick one, fill its parameters, and [onApply] adds it to the
 * draft (the change then shows in the tree and the review, like any edit).
 */
@Composable
fun SnippetPickerDialog(
    snippets: UiState<List<ConfigSnippet>>?,
    onRetry: () -> Unit,
    onApply: (id: String, params: Map<String, String>) -> Unit,
    onDismiss: () -> Unit,
) {
    var selectedId by rememberSaveable { mutableStateOf<String?>(null) }
    val list = (snippets as? UiState.Loaded)?.data.orEmpty()
    val selected = list.firstOrNull { it.id == selectedId }
    val values = remember(selectedId) { mutableStateMapOf<String, String>() }
    val complete = selected != null && selected.params.all { values[it.name].orEmpty().isNotBlank() || it.type == "bool" }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(selected?.let { stringResource(snippetLabel(it.id)) } ?: stringResource(R.string.machine_config_snippets)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                when {
                    selected != null -> SnippetForm(selected, values)
                    snippets == null || snippets == UiState.Loading -> LoadingBox()
                    snippets is UiState.Failed -> ErrorBox(snippets.message, onRetry)
                    else -> list.byGroup().forEach { (group, items) ->
                        SectionTitle(stringResource(snippetGroup(group)))
                        items.forEach { snippet -> SnippetRow(snippet) { selectedId = snippet.id } }
                    }
                }
            }
        },
        confirmButton = {
            if (selected != null) {
                TextButton(
                    onClick = {
                        val params = selected.params.associate { p -> p.name to (values[p.name] ?: if (p.type == "bool") "false" else "") }
                        onApply(selected.id, params)
                    },
                    enabled = complete,
                ) { Text(stringResource(R.string.snippet_apply)) }
            }
        },
        dismissButton = {
            if (selected != null) {
                TextButton(onClick = { selectedId = null }) { Text(stringResource(R.string.common_back)) }
            } else {
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
            }
        },
    )
}

@Composable
private fun SnippetRow(snippet: ConfigSnippet, onClick: () -> Unit) {
    Column(Modifier.fillMaxWidth().clickable(onClick = onClick).padding(vertical = 8.dp)) {
        Text(stringResource(snippetLabel(snippet.id)), style = MaterialTheme.typography.bodyLarge)
        MutedText(stringResource(snippetDescription(snippet.id)))
    }
}

@Composable
private fun SnippetForm(snippet: ConfigSnippet, values: MutableMap<String, String>) {
    MutedText(stringResource(snippetDescription(snippet.id)))
    if (snippet.staged) MutedText(stringResource(R.string.snippet_staged_note))
    snippet.params.forEach { param -> ParamField(param, values[param.name].orEmpty()) { values[param.name] = it } }
}

@Composable
private fun ParamField(param: SnippetParam, value: String, onChange: (String) -> Unit) {
    if (param.type == "bool") {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(snippetParam(param.name)), modifier = Modifier.weight(1f))
            Switch(checked = value == "true", onCheckedChange = { onChange(it.toString()) })
        }
        return
    }
    val list = param.type == "list"
    OutlinedTextField(
        value = value,
        onValueChange = onChange,
        label = { Text(stringResource(snippetParam(param.name))) },
        placeholder = { Text(param.hint, fontFamily = FontFamily.Monospace) },
        singleLine = !list,
        minLines = if (list) 3 else 1,
        keyboardOptions = KeyboardOptions(
            keyboardType = if (param.type == "int") KeyboardType.Number else KeyboardType.Ascii,
            autoCorrectEnabled = false,
        ),
        modifier = Modifier.fillMaxWidth(),
    )
}

@StringRes
private fun snippetGroup(group: String): Int = when (group) {
    "network" -> R.string.snippet_group_network
    "kernel" -> R.string.snippet_group_kernel
    "kubernetes" -> R.string.snippet_group_kubernetes
    else -> R.string.snippet_group_cluster
}

@StringRes
private fun snippetLabel(id: String): Int = when (id) {
    "ntp" -> R.string.snippet_ntp
    "dns" -> R.string.snippet_dns
    "mtu" -> R.string.snippet_mtu
    "sysctl" -> R.string.snippet_sysctl
    "kernel_args" -> R.string.snippet_kernel_args
    "kubelet_arg" -> R.string.snippet_kubelet_arg
    "apiserver_arg" -> R.string.snippet_apiserver_arg
    "node_label" -> R.string.snippet_node_label
    "registry_mirror" -> R.string.snippet_registry_mirror
    else -> R.string.snippet_kubespan
}

@StringRes
private fun snippetDescription(id: String): Int = when (id) {
    "ntp" -> R.string.snippet_ntp_desc
    "dns" -> R.string.snippet_dns_desc
    "mtu" -> R.string.snippet_mtu_desc
    "sysctl" -> R.string.snippet_sysctl_desc
    "kernel_args" -> R.string.snippet_kernel_args_desc
    "kubelet_arg" -> R.string.snippet_kubelet_arg_desc
    "apiserver_arg" -> R.string.snippet_apiserver_arg_desc
    "node_label" -> R.string.snippet_node_label_desc
    "registry_mirror" -> R.string.snippet_registry_mirror_desc
    else -> R.string.snippet_kubespan_desc
}

@StringRes
private fun snippetParam(name: String): Int = when (name) {
    "servers" -> R.string.snippet_param_servers
    "nameservers" -> R.string.snippet_param_nameservers
    "interface" -> R.string.snippet_param_interface
    "mtu" -> R.string.snippet_param_mtu
    "key" -> R.string.snippet_param_key
    "args" -> R.string.snippet_param_args
    "host" -> R.string.snippet_param_host
    "endpoint" -> R.string.snippet_param_endpoint
    "enabled" -> R.string.snippet_param_enabled
    else -> R.string.snippet_param_value
}
