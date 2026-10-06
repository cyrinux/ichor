package name.levis.ichor.ui.machineconfig

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Add
import androidx.compose.material.icons.outlined.ChevronRight
import androidx.compose.material.icons.outlined.DeleteOutline
import androidx.compose.material.icons.outlined.ExpandMore
import androidx.compose.material.icons.outlined.Lock
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ConfigEdit
import name.levis.ichor.model.ConfigNode
import name.levis.ichor.model.ConfigRow
import name.levis.ichor.model.ConfigTree
import name.levis.ichor.model.ConfigType
import name.levis.ichor.model.rows
import name.levis.ichor.ui.components.ConfirmDialog
import name.levis.ichor.ui.components.EmptyText
import name.levis.ichor.ui.components.InfoNotice
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.components.expandable

/** How far each level of the tree is pushed right. */
private val Indent = 14.dp

/**
 * The machine config field by field: one section per YAML document, objects and lists that
 * open on tap, and what the schema says under each key. While [editing], a value opens its
 * editor, and objects and lists can grow or be removed; [onEdit] gets each change.
 */
@Composable
fun ConfigTreeView(
    tree: ConfigTree,
    query: String,
    editing: Boolean,
    enabled: Boolean,
    onEdit: (ConfigEdit) -> Unit,
    modifier: Modifier = Modifier,
) {
    var expanded by remember { mutableStateOf(emptySet<String>()) }
    var sheet by remember { mutableStateOf<ConfigSheet?>(null) }
    var removing by remember { mutableStateOf<ConfigRow.Field?>(null) }
    val rows = remember(tree, expanded, query) { tree.rows(expanded, query) }

    if (rows.isEmpty()) {
        EmptyText(stringResource(R.string.machine_config_no_match), modifier.padding(16.dp))
        return
    }

    LazyColumn(modifier.fillMaxSize(), contentPadding = PaddingValues(start = 16.dp, end = 8.dp, bottom = 24.dp)) {
        if (!tree.schema) {
            item(key = "no-schema") { InfoNotice(stringResource(R.string.machine_config_no_schema), Modifier.padding(vertical = 8.dp)) }
        }
        items(rows, key = { it.id }) { row ->
            when (row) {
                is ConfigRow.Title -> SectionTitle(row.title, Modifier.padding(top = 8.dp))
                is ConfigRow.Field -> when {
                    !row.isRoot -> FieldRow(
                        row = row,
                        editing = editing && enabled,
                        onToggle = { expanded = if (row.id in expanded) expanded - row.id else expanded + row.id },
                        onOpen = { sheet = it },
                        onRemove = { removing = row },
                    )
                    editing && row.node.canGrow -> TextButton(enabled = enabled, onClick = { sheet = ConfigSheet.Add(row.doc, row.node) }) {
                        Icon(Icons.Outlined.Add, contentDescription = null, modifier = Modifier.size(18.dp))
                        Text(stringResource(R.string.machine_config_add_field), Modifier.padding(start = 4.dp))
                    }
                }
            }
        }
    }

    sheet?.let { ConfigSheetHost(it, onEdit = onEdit, onDismiss = { sheet = null }) }

    removing?.let { row ->
        ConfirmDialog(
            title = stringResource(R.string.machine_config_remove_title, row.node.key),
            text = stringResource(R.string.machine_config_remove_text),
            confirm = stringResource(R.string.machine_config_remove),
            onConfirm = {
                removing = null
                onEdit(ConfigEdit.remove(row.doc, row.node.path))
            },
            onDismiss = { removing = null },
            destructive = true,
        )
    }
}

@Composable
private fun FieldRow(
    row: ConfigRow.Field,
    editing: Boolean,
    onToggle: () -> Unit,
    onOpen: (ConfigSheet) -> Unit,
    onRemove: () -> Unit,
) {
    val node = row.node
    val editable = editing && !node.isContainer && !node.redacted
    val tap = when {
        node.isContainer -> Modifier.expandable(row.expanded, onToggle = onToggle)
        editable -> Modifier.clickable(onClickLabel = stringResource(R.string.machine_config_edit)) { onOpen(ConfigSheet.Value(row.doc, node)) }
        else -> Modifier
    }
    Row(
        Modifier.fillMaxWidth().then(tap).padding(start = Indent * (row.depth - 1), top = 6.dp, bottom = 6.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        if (node.isContainer) {
            Icon(
                if (row.expanded) Icons.Outlined.ExpandMore else Icons.Outlined.ChevronRight,
                contentDescription = null,
                tint = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.size(18.dp),
            )
        } else {
            Spacer(Modifier.width(18.dp))
        }
        Column(Modifier.weight(1f).padding(start = 4.dp)) {
            Text(
                if (row.inList) "[${node.key}]" else node.key,
                style = MaterialTheme.typography.bodyMedium,
                fontFamily = FontFamily.Monospace,
                fontWeight = if (node.isContainer) FontWeight.SemiBold else FontWeight.Normal,
                color = if (node.isContainer) MaterialTheme.colorScheme.onSurface else MaterialTheme.colorScheme.onSurfaceVariant,
            )
            FieldValue(node)
            if (node.description.isNotEmpty()) {
                MutedText(node.description, maxLines = if (row.expanded || !node.isContainer) 3 else 1, overflow = TextOverflow.Ellipsis)
            }
        }
        if (editing && node.isContainer) {
            if (node.canGrow) {
                TooltipIconButton(
                    Icons.Outlined.Add,
                    stringResource(if (node.type == ConfigType.ARRAY) R.string.machine_config_add_item else R.string.machine_config_add_field),
                    onClick = { onOpen(ConfigSheet.Add(row.doc, node)) },
                )
            }
            TooltipIconButton(Icons.Outlined.DeleteOutline, stringResource(R.string.machine_config_remove), onClick = onRemove)
        }
    }
}

@Composable
private fun FieldValue(node: ConfigNode) {
    when {
        node.type == ConfigType.OBJECT -> MutedText(pluralStringResource(R.plurals.machine_config_fields, node.children.size, node.children.size))
        node.type == ConfigType.ARRAY -> MutedText(pluralStringResource(R.plurals.machine_config_items, node.children.size, node.children.size))
        node.redacted -> Row(verticalAlignment = Alignment.CenterVertically) {
            Icon(Icons.Outlined.Lock, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(14.dp))
            MutedText(stringResource(R.string.machine_config_hidden), Modifier.padding(start = 4.dp))
        }
        node.value.isEmpty() -> MutedText(stringResource(R.string.machine_config_empty_value))
        else -> Text(node.value, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
    }
}
