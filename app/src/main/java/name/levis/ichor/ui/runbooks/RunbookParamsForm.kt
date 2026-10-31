package name.levis.ichor.ui.runbooks

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.text.KeyboardOptions
import name.levis.ichor.ui.design.IchorButton
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenu
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import name.levis.ichor.ui.design.IchorSwitch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import kotlin.coroutines.cancellation.CancellationException
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.RunbookParam
import name.levis.ichor.model.missingRunbookValues

/** What the cluster offers for a parameter's kind; empty when it cannot say: the user types the value. */
internal suspend fun runbookValueChoices(app: TalosApp, params: List<RunbookParam>): Map<String, List<String>> {
    val choices = mutableMapOf<String, List<String>>()
    params.map { it.kind }.distinct().forEach { kind ->
        val names = try {
            when (kind) {
                RunbookParam.KIND_NAMESPACE -> app.kubeRepository.namespaces().namespaces
                RunbookParam.KIND_WORKLOAD -> WORKLOAD_KINDS.flatMap { app.kubeRepository.workloadsPage(it, null, "").items.map { w -> w.name } }
                RunbookParam.KIND_NODE -> app.talosRepository.overview().nodes.map { it.node }
                else -> emptyList()
            }
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            emptyList()
        }
        choices[kind] = names.distinct().sorted()
    }
    return choices
}

private val WORKLOAD_KINDS = listOf("Deployment", "StatefulSet", "DaemonSet")

/**
 * The values a runbook asks for, before its plan: one field per parameter, Continue off until
 * every required one is set. [error] is what the core said of the values last tried.
 */
@Composable
internal fun RunbookParamsForm(
    params: List<RunbookParam>,
    values: Map<String, String>,
    choices: Map<String, List<String>>,
    error: String?,
    onChange: (name: String, value: String) -> Unit,
    onContinue: () -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        Text(stringResource(R.string.runbook_params_title), style = MaterialTheme.typography.titleMedium)
        params.forEach { param ->
            val value = values[param.name].orEmpty()
            if (param.kind == RunbookParam.KIND_BOOL) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(param.prompt, modifier = Modifier.weight(1f))
                    IchorSwitch(checked = value == "true", onCheckedChange = { onChange(param.name, it.toString()) })
                }
            } else {
                RunbookValueField(param, value, choices[param.kind].orEmpty()) { onChange(param.name, it) }
            }
        }
        error?.let { Text(it, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall) }
        IchorButton(onClick = onContinue, enabled = missingRunbookValues(params, values).isEmpty(), modifier = Modifier.fillMaxWidth()) {
            Text(stringResource(R.string.runbook_params_continue))
        }
    }
}

/** A value to type, or to pick among [choices] when the cluster gave some. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun RunbookValueField(param: RunbookParam, value: String, choices: List<String>, onChange: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    val keyboard = if (param.kind == RunbookParam.KIND_INT) KeyboardOptions(keyboardType = KeyboardType.Number) else KeyboardOptions.Default
    ExposedDropdownMenuBox(expanded = open && choices.isNotEmpty(), onExpandedChange = { open = it }) {
        OutlinedTextField(
            value = value,
            onValueChange = onChange,
            singleLine = true,
            label = { Text(param.prompt) },
            keyboardOptions = keyboard,
            trailingIcon = { if (choices.isNotEmpty()) ExposedDropdownMenuDefaults.TrailingIcon(expanded = open) },
            modifier = Modifier.fillMaxWidth().menuAnchor(ExposedDropdownMenuAnchorType.PrimaryEditable),
        )
        ExposedDropdownMenu(expanded = open && choices.isNotEmpty(), onDismissRequest = { open = false }) {
            choices.forEach { choice ->
                DropdownMenuItem(
                    text = { Text(choice) },
                    onClick = {
                        onChange(choice)
                        open = false
                    },
                    contentPadding = ExposedDropdownMenuDefaults.ItemContentPadding,
                )
            }
        }
    }
}
