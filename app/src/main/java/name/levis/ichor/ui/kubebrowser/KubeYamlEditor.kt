package name.levis.ichor.ui.kubebrowser

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.HelpOutline
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.KubeExplain
import name.levis.ichor.model.KubeExplainChild
import name.levis.ichor.model.SCHEMA_HELP_UNAVAILABLE
import name.levis.ichor.model.insertYamlField
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.netpol.TagBadge
import name.levis.ichor.ui.theme.LocalStatusColors

/**
 * The object's YAML as an editable text, with the schema help of the field at the cursor
 * (Explain field) and the fields that can go there (Add field), read from the cluster's
 * OpenAPI v3 schema. [kind] names the object when the cursor is at its top level.
 */
@Composable
fun KubeYamlEditor(
    draft: String,
    kind: String,
    help: SchemaHelp?,
    onChange: (String) -> Unit,
    onOpenHelp: (text: String, offset: Int, mode: SchemaHelpMode) -> Unit,
    onRetryHelp: () -> Unit,
    onCloseHelp: () -> Unit,
    modifier: Modifier = Modifier,
) {
    // The field owns its text and cursor while on screen: feeding the view model's draft
    // back would lose what is typed in between.
    var field by remember { mutableStateOf(TextFieldValue(draft)) }

    fun replace(value: TextFieldValue) {
        val changed = value.text != field.text
        field = value
        if (changed) onChange(value.text)
    }

    help?.let { h ->
        SchemaHelpSheet(
            help = h,
            kind = kind,
            onAdd = { child, listItem ->
                val (text, cursor) = insertYamlField(field.text, h.offset, child.name, listItem)
                onCloseHelp()
                replace(TextFieldValue(text, TextRange(cursor)))
            },
            onRetry = onRetryHelp,
            onDismiss = onCloseHelp,
        )
    }

    Column(modifier.fillMaxSize().imePadding()) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 8.dp), horizontalArrangement = Arrangement.spacedBy(4.dp)) {
            TextButton(onClick = { onOpenHelp(field.text, field.selection.start, SchemaHelpMode.EXPLAIN) }) {
                Icon(Icons.AutoMirrored.Outlined.HelpOutline, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.kb_explain_field), Modifier.padding(start = 6.dp))
            }
            TextButton(onClick = { onOpenHelp(field.text, field.selection.start, SchemaHelpMode.ADD) }) {
                Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(18.dp))
                Text(stringResource(R.string.kb_add_field), Modifier.padding(start = 6.dp))
            }
        }
        OutlinedTextField(
            value = field,
            onValueChange = ::replace,
            textStyle = TextStyle(fontFamily = FontFamily.Monospace, fontSize = MaterialTheme.typography.bodySmall.fontSize),
            keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.None, autoCorrectEnabled = false, keyboardType = KeyboardType.Ascii),
            modifier = Modifier.fillMaxSize().padding(start = 16.dp, end = 16.dp, bottom = 8.dp),
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun SchemaHelpSheet(
    help: SchemaHelp,
    kind: String,
    onAdd: (KubeExplainChild, listItem: Boolean) -> Unit,
    onRetry: () -> Unit,
    onDismiss: () -> Unit,
) {
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState()) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            val path = help.path.ifEmpty { kind }
            Text(
                if (help.mode == SchemaHelpMode.ADD) stringResource(R.string.kb_schema_add_title, path) else path,
                style = MaterialTheme.typography.titleMedium,
                fontFamily = FontFamily.Monospace,
            )
            when (val state = help.state) {
                UiState.Loading -> LinearProgressIndicator(Modifier.fillMaxWidth().padding(vertical = 8.dp))
                is UiState.Failed -> HelpFailure(state.message.asString(), onRetry)
                is UiState.Loaded ->
                    if (help.mode == SchemaHelpMode.EXPLAIN) ExplainContent(state.data)
                    else AddContent(state.data) { onAdd(it, state.data.isList && help.cursor.opensBlock) }
            }
        }
    }
}

@Composable
private fun HelpFailure(message: String, onRetry: () -> Unit) {
    if (message.startsWith(SCHEMA_HELP_UNAVAILABLE, ignoreCase = true)) {
        Text(stringResource(R.string.kb_schema_unavailable), style = MaterialTheme.typography.titleSmall)
        MutedText(message.substring(SCHEMA_HELP_UNAVAILABLE.length).trimStart(':', ' '))
        return
    }
    InlineError(message)
    TextButton(onClick = onRetry) { Text(stringResource(R.string.common_retry)) }
}

@Composable
private fun ExplainContent(explain: KubeExplain) {
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Text(explain.type, fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodyMedium)
        if (explain.format.isNotEmpty() && !explain.type.contains(explain.format)) MutedText(explain.format)
        if (explain.required) TagBadge(stringResource(R.string.kb_schema_required), LocalStatusColors.current.warn)
    }
    if (explain.description.isNotEmpty()) Text(explain.description, style = MaterialTheme.typography.bodyMedium)
    if (explain.enum.isNotEmpty()) {
        SectionTitle(stringResource(R.string.kb_schema_values))
        Text(explain.enum.joinToString("  ·  "), fontFamily = FontFamily.Monospace, style = MaterialTheme.typography.bodySmall)
    }
    if (explain.children.isNotEmpty()) {
        SectionTitle(stringResource(R.string.kb_schema_fields))
        explain.children.forEach { ChildRow(it) }
    }
}

@Composable
private fun AddContent(explain: KubeExplain, onAdd: (KubeExplainChild) -> Unit) {
    if (explain.children.isEmpty()) {
        MutedText(stringResource(R.string.kb_schema_no_fields))
        return
    }
    MutedText(stringResource(R.string.kb_schema_add_hint))
    explain.children.sortedByDescending { it.required }.forEach { child ->
        ChildRow(child, Modifier.clickable(onClickLabel = stringResource(R.string.kb_add_field)) { onAdd(child) })
    }
}

@Composable
private fun ChildRow(child: KubeExplainChild, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxWidth().padding(vertical = 6.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(child.name, fontFamily = FontFamily.Monospace, fontWeight = FontWeight.SemiBold, style = MaterialTheme.typography.bodyMedium)
            MutedText(child.type)
            if (child.required) TagBadge(stringResource(R.string.kb_schema_required), LocalStatusColors.current.warn)
        }
        if (child.description.isNotEmpty()) MutedText(child.description)
        if (child.enum.isNotEmpty()) MutedText(child.enum.joinToString("  ·  "))
    }
}
