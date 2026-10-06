package name.levis.ichor.ui.machineconfig

import androidx.annotation.StringRes
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ConfigAddable
import name.levis.ichor.model.ConfigEdit
import name.levis.ichor.model.ConfigNode
import name.levis.ichor.model.ConfigType
import name.levis.ichor.model.valueTypes
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.theme.LocalStatusColors

/** What the tree asks to edit in a sheet. */
sealed interface ConfigSheet {
    /** The value of the scalar [node]. */
    data class Value(val doc: Int, val node: ConfigNode) : ConfigSheet

    /** A new field of the object, or a new item of the list, [node]. */
    data class Add(val doc: Int, val node: ConfigNode) : ConfigSheet
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConfigSheetHost(sheet: ConfigSheet, onEdit: (ConfigEdit) -> Unit, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .navigationBarsPadding()
                .imePadding()
                .padding(start = 20.dp, end = 20.dp, bottom = 16.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            val done: (ConfigEdit) -> Unit = {
                onEdit(it)
                onDismiss()
            }
            when (sheet) {
                is ConfigSheet.Value -> ValueSheet(sheet, done, onDismiss)
                is ConfigSheet.Add -> AddSheet(sheet, done, onDismiss)
            }
        }
    }
}

/** The type a typed value is sent as: the schema's or the YAML's when it is a plain one, else read as YAML. */
private fun scalarType(type: String): String =
    if (type == ConfigType.STRING || type == ConfigType.INTEGER || type == ConfigType.NUMBER || type == ConfigType.BOOLEAN) type else ConfigType.ANY

@Composable
private fun ValueSheet(sheet: ConfigSheet.Value, onEdit: (ConfigEdit) -> Unit, onDismiss: () -> Unit) {
    val node = sheet.node
    val type = scalarType(node.type)
    var value by remember(node) { mutableStateOf(node.value) }

    Text(node.key, style = MaterialTheme.typography.titleMedium, fontFamily = FontFamily.Monospace)
    if (node.description.isNotEmpty()) MutedText(node.description)
    ConfigValueInput(type, node.allowed, value) { value = it }
    Row(verticalAlignment = Alignment.CenterVertically) {
        TextButton(onClick = { onEdit(ConfigEdit.remove(sheet.doc, node.path)) }) {
            Text(stringResource(R.string.machine_config_remove), color = LocalStatusColors.current.bad)
        }
        Spacer(Modifier.weight(1f))
        TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
        Button(enabled = value != node.value, onClick = { onEdit(ConfigEdit.set(sheet.doc, node.path, type, value)) }) {
            Text(stringResource(R.string.machine_config_save))
        }
    }
}

@Composable
private fun AddSheet(sheet: ConfigSheet.Add, onEdit: (ConfigEdit) -> Unit, onDismiss: () -> Unit) {
    val node = sheet.node
    val isList = node.type == ConfigType.ARRAY
    var picked by remember(node) { mutableStateOf<ConfigAddable?>(null) }
    var customKey by remember(node) { mutableStateOf("") }
    var value by remember(node) { mutableStateOf("") }

    val declared = picked?.type ?: if (isList) node.itemType else node.freeKeyType
    val types = valueTypes(declared)
    var type by remember(declared) { mutableStateOf(types.first()) }
    val key = picked?.key ?: customKey.trim()

    Text(
        stringResource(if (isList) R.string.machine_config_add_item else R.string.machine_config_add_field),
        style = MaterialTheme.typography.titleMedium,
    )
    if (!isList) {
        node.addable.forEach { field ->
            AddableRow(field, selected = picked == field) {
                picked = field
                customKey = ""
                value = ""
            }
        }
        if (node.freeKeyType.isNotEmpty()) {
            OutlinedTextField(
                value = customKey,
                onValueChange = {
                    customKey = it
                    picked = null
                },
                label = { Text(stringResource(R.string.machine_config_field_name)) },
                singleLine = true,
                textStyle = TextStyle(fontFamily = FontFamily.Monospace),
                keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false),
                modifier = Modifier.fillMaxWidth(),
            )
        }
    }
    if (types.size > 1) {
        ChipRow(types, selected = type, label = { stringResource(typeLabel(it)) }) {
            type = it
            value = ""
        }
    }
    if (type != ConfigType.OBJECT && type != ConfigType.ARRAY) {
        ConfigValueInput(type, picked?.allowed.orEmpty(), value) { value = it }
    }
    Row(verticalAlignment = Alignment.CenterVertically) {
        Spacer(Modifier.weight(1f))
        TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_cancel)) }
        Button(
            enabled = isList || key.isNotEmpty(),
            onClick = {
                // An on/off value that was never touched is off, not empty.
                val sent = if (type == ConfigType.BOOLEAN && value.isEmpty()) "false" else value
                onEdit(ConfigEdit.add(sheet.doc, node.path, if (isList) "" else key, type, sent))
            },
        ) { Text(stringResource(R.string.machine_config_add)) }
    }
}

@Composable
private fun AddableRow(field: ConfigAddable, selected: Boolean, onSelect: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().selectable(selected = selected, role = Role.RadioButton, onClick = onSelect),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RadioButton(selected = selected, onClick = null)
        Column(Modifier.padding(start = 12.dp, top = 6.dp, bottom = 6.dp)) {
            Text(field.key, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            if (field.description.isNotEmpty()) MutedText(field.description, maxLines = if (selected) Int.MAX_VALUE else 2, overflow = TextOverflow.Ellipsis)
        }
    }
}

@StringRes
private fun typeLabel(type: String): Int = when (type) {
    ConfigType.INTEGER -> R.string.machine_config_type_integer
    ConfigType.NUMBER -> R.string.machine_config_type_number
    ConfigType.BOOLEAN -> R.string.machine_config_type_boolean
    ConfigType.OBJECT -> R.string.machine_config_type_object
    ConfigType.ARRAY -> R.string.machine_config_type_array
    else -> R.string.machine_config_type_string
}

@Composable
private fun ChipRow(options: List<String>, selected: String, label: @Composable (String) -> String = { it }, onSelect: (String) -> Unit) {
    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.forEach { option ->
            FilterChip(selected = option == selected, onClick = { onSelect(option) }, label = { Text(label(option)) })
        }
    }
}

/** The input for a value of [type]: the allowed values when the schema lists them, a switch, or a text field. */
@Composable
private fun ConfigValueInput(type: String, allowed: List<String>, value: String, onValue: (String) -> Unit) {
    when {
        allowed.isNotEmpty() -> ChipRow(allowed, selected = value, onSelect = onValue)
        type == ConfigType.BOOLEAN -> Row(verticalAlignment = Alignment.CenterVertically) {
            Text(stringResource(R.string.machine_config_value), Modifier.weight(1f))
            Switch(checked = value == "true", onCheckedChange = { onValue(it.toString()) })
        }
        else -> OutlinedTextField(
            value = value,
            onValueChange = onValue,
            label = { Text(stringResource(R.string.machine_config_value)) },
            textStyle = TextStyle(fontFamily = FontFamily.Monospace),
            keyboardOptions = KeyboardOptions(
                capitalization = KeyboardCapitalization.None,
                autoCorrectEnabled = false,
                keyboardType = when (type) {
                    ConfigType.INTEGER -> KeyboardType.Number
                    ConfigType.NUMBER -> KeyboardType.Decimal
                    else -> KeyboardType.Ascii
                },
            ),
            maxLines = 6,
            modifier = Modifier.fillMaxWidth(),
        )
    }
}
