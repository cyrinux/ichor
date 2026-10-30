package name.levis.ichor.ui.plugins

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import name.levis.ichor.ui.design.IchorOutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import name.levis.ichor.ui.design.IchorTextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.PLUGIN_PARAM_BOOL
import name.levis.ichor.model.PLUGIN_PARAM_ENUM
import name.levis.ichor.model.PLUGIN_PARAM_INT
import name.levis.ichor.model.PLUGIN_PARAM_STRING
import name.levis.ichor.model.PluginAction
import name.levis.ichor.model.PluginActionPending
import name.levis.ichor.model.PluginParam
import name.levis.ichor.model.PluginParamProblem
import name.levis.ichor.model.PluginRef
import name.levis.ichor.model.displayName
import name.levis.ichor.model.encodeParams
import name.levis.ichor.model.initialParamValues
import name.levis.ichor.model.paramProblems
import name.levis.ichor.model.pendingAction
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.node.HostnameConfirmDialog

// A plugin action's button, params form and confirmation (docs/plugins.md "Actions"). The
// button is the tree's say only (enabled, allowed): Go checks the object again when it runs.

private const val CONFIRM_NONE = "none"
private const val CONFIRM_TYPED_NAME = "typedName"

/**
 * The buttons of an `actions` section on the row's object [ref]. A button runs when the tree
 * says it may (enabled, and allowed unless denied: an unknown answer leaves it to the API
 * server, claiming no permission); otherwise it stays off with the reason. [runner] null (or
 * no object to act on): every button stays off.
 */
@Composable
fun PluginActionButtons(items: List<PluginAction>, ref: PluginRef?, runner: PluginActionRunner?) {
    // Bound to the object the dialog was opened on (captured at the tap), not to this row's.
    var pending by remember { mutableStateOf<PluginActionPending?>(null) }
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        items.forEach { a ->
            val target = if (runner != null && a.usable) ref else null
            val busy = target != null && runner != null && pluginActionKey(target, a.id) in runner.busy
            IchorOutlinedButton(
                onClick = {
                    when {
                        target == null -> Unit
                        a.params.isNotEmpty() || a.confirm != CONFIRM_NONE -> pending = PluginActionPending(target, a.id)
                        else -> runner?.run(target, a, "")
                    }
                },
                enabled = target != null && !busy,
            ) {
                if (busy) {
                    CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
                    Spacer(Modifier.size(8.dp))
                }
                Text(a.label)
            }
            actionNote(a, runnable = target != null)?.let { MutedText(it) }
        }
    }
    // A reload that put another object in this row, or turned the action off, closes the dialog.
    val opened = pending
    val action = pendingAction(opened, ref, items)
    if (opened != null && action == null) LaunchedEffect(opened) { pending = null }
    if (opened != null && action != null && runner != null) {
        PluginActionDialog(
            action,
            target = opened.ref,
            onDismiss = { pending = null },
            onRun = { json ->
                pending = null
                runner.run(opened.ref, action, json)
            },
        )
    }
}

/** Why a button is off; null when it may run (an unknown can-I answer claims nothing). */
@Composable
private fun actionNote(a: PluginAction, runnable: Boolean): String? = when {
    !a.enabled -> a.disabledReason.ifEmpty { stringResource(R.string.plugin_action_disabled) }
    a.allowed == false -> stringResource(R.string.plugin_action_not_allowed)
    !runnable -> stringResource(R.string.plugin_action_disabled)
    else -> null
}

/**
 * Collects [action]'s params and confirms it per its `confirm`: the object's name typed for
 * typedName (with the form above it), the form's run button for the rest, a plain question
 * when there is no param. The title names [target] with its namespace; the name typed is
 * matched against [target]'s name, the object the run goes to. [onRun] gets the encoded
 * params, checked first.
 */
@Composable
private fun PluginActionDialog(action: PluginAction, target: PluginRef, onDismiss: () -> Unit, onRun: (String) -> Unit) {
    var values by remember(target.key, action.id) { mutableStateOf(initialParamValues(action.params)) }
    val problems = paramProblems(action.params, values)
    val objectName = target.displayName
    val title = stringResource(R.string.plugin_action_confirm_title, action.label, objectName)
    val run = { if (problems.isEmpty()) onRun(encodeParams(action.params, values)) }
    val form: @Composable () -> Unit = { ParamsForm(action.params, values, problems) { name, v -> values = values + (name to v) } }
    when {
        action.confirm == CONFIRM_TYPED_NAME -> HostnameConfirmDialog(
            title = title,
            hostname = target.name,
            confirmLabel = action.label,
            onConfirm = run,
            onDismiss = onDismiss,
            emphasized = true,
            enabled = problems.isEmpty(),
        ) { if (action.params.isNotEmpty()) form() }
        action.params.isEmpty() -> ConfirmDialog(
            title = title,
            text = stringResource(R.string.plugin_action_confirm_text, action.label, objectName),
            confirm = action.label,
            onConfirm = run,
            onDismiss = onDismiss,
        )
        else -> AlertDialog(
            onDismissRequest = onDismiss,
            title = { Text(title) },
            text = { Column(Modifier.verticalScroll(rememberScrollState())) { form() } },
            confirmButton = { IchorTextButton(onClick = run, enabled = problems.isEmpty()) { Text(action.label) } },
            dismissButton = { IchorTextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

/**
 * One field per param, its problem under it. "Required" shows as a plain hint, never in the
 * error colour: an empty required field is not a mistake yet, only why the run button is off.
 */
@Composable
private fun ParamsForm(params: List<PluginParam>, values: Map<String, String>, problems: Map<String, PluginParamProblem>, onChange: (String, String) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        params.forEach { p ->
            val value = values[p.name].orEmpty()
            val label = p.label.ifEmpty { p.name }
            val problem = problems[p.name]
            when (p.type) {
                PLUGIN_PARAM_STRING, PLUGIN_PARAM_INT -> OutlinedTextField(
                    value = value,
                    onValueChange = { onChange(p.name, it) },
                    label = { Text(label) },
                    singleLine = true,
                    isError = problem != null && problem != PluginParamProblem.REQUIRED,
                    supportingText = problem?.let { { Text(problemText(it)) } },
                    keyboardOptions = if (p.type == PLUGIN_PARAM_INT) KeyboardOptions(keyboardType = KeyboardType.Number) else KeyboardOptions.Default,
                    modifier = Modifier.fillMaxWidth(),
                )
                PLUGIN_PARAM_BOOL -> BoolField(label, value == "true") { onChange(p.name, it.toString()) }
                PLUGIN_PARAM_ENUM -> EnumField(label, p.values, value, problem) { onChange(p.name, it) }
                else -> Column {
                    Text(label, style = MaterialTheme.typography.labelLarge)
                    MutedText(problemText(PluginParamProblem.UNKNOWN_TYPE))
                }
            }
        }
    }
}

@Composable
private fun BoolField(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().toggleable(value = checked, role = Role.Switch, onValueChange = onChange),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(label, style = MaterialTheme.typography.bodyMedium, modifier = Modifier.weight(1f))
        Switch(checked = checked, onCheckedChange = null)
    }
}

/** An enum's values as chips; an optional one's chip toggles off, leaving Go its default. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun EnumField(label: String, choices: List<String>, value: String, problem: PluginParamProblem?, onChange: (String) -> Unit) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Text(label, style = MaterialTheme.typography.labelLarge)
        FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            choices.forEach { c ->
                FilterChip(selected = c == value, onClick = { onChange(if (c == value) "" else c) }, label = { Text(c) })
            }
        }
        problem?.let { MutedText(problemText(it)) }
    }
}

@Composable
private fun problemText(problem: PluginParamProblem): String = stringResource(
    when (problem) {
        PluginParamProblem.REQUIRED -> R.string.plugin_param_required
        PluginParamProblem.NOT_INTEGER -> R.string.plugin_param_not_integer
        PluginParamProblem.TOO_LONG -> R.string.plugin_param_too_long
        PluginParamProblem.NOT_A_VALUE -> R.string.plugin_param_pick
        PluginParamProblem.UNKNOWN_TYPE -> R.string.plugin_param_unsupported
    },
)
