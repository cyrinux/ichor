package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenu
import androidx.compose.material3.ExposedDropdownMenuAnchorType
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import name.levis.ichor.ui.design.IchorFilterChip
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
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.model.NetPerfNode

/** Up to this many nodes fit as chips on a phone; more get a dropdown. */
private const val CHIP_NODE_LIMIT = 3

/** Picks a test node by name: chips for a small cluster, a dropdown that lists every node otherwise. */
@Composable
internal fun NodePicker(
    label: String,
    nodes: List<NetPerfNode>,
    selected: String,
    hint: @Composable () -> Unit = {},
    onSelect: (String) -> Unit,
) {
    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(Modifier.padding(horizontal = 16.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(label, style = MaterialTheme.typography.labelLarge)
            hint()
        }
        if (nodes.size > CHIP_NODE_LIMIT) {
            NodeDropdown(label, nodes, selected, onSelect)
        } else {
            NodeChips(nodes, selected, onSelect)
        }
    }
}

@Composable
private fun NodeChips(nodes: List<NetPerfNode>, selected: String, onSelect: (String) -> Unit) {
    LazyRow(contentPadding = PaddingValues(horizontal = 16.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        items(nodes, key = { it.name }) { node ->
            IchorFilterChip(
                selected = node.name == selected,
                onClick = { onSelect(node.name) },
                enabled = node.ready,
                label = { Text(node.name, fontFamily = FontFamily.Monospace) },
            )
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun NodeDropdown(label: String, nodes: List<NetPerfNode>, selected: String, onSelect: (String) -> Unit) {
    var open by remember { mutableStateOf(false) }
    val mono = MaterialTheme.typography.bodyLarge.merge(TextStyle(fontFamily = FontFamily.Monospace))
    ExposedDropdownMenuBox(expanded = open, onExpandedChange = { open = it }, modifier = Modifier.padding(horizontal = 16.dp)) {
        OutlinedTextField(
            value = selected,
            onValueChange = {},
            readOnly = true,
            singleLine = true,
            label = { Text(label) },
            textStyle = mono,
            trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = open) },
            modifier = Modifier.fillMaxWidth().menuAnchor(ExposedDropdownMenuAnchorType.PrimaryNotEditable),
        )
        ExposedDropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            nodes.forEach { node ->
                DropdownMenuItem(
                    text = { Text(node.name, fontFamily = FontFamily.Monospace) },
                    onClick = {
                        onSelect(node.name)
                        open = false
                    },
                    enabled = node.ready,
                    contentPadding = ExposedDropdownMenuDefaults.ItemContentPadding,
                )
            }
        }
    }
}
