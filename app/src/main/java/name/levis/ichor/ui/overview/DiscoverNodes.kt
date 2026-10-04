package name.levis.ichor.ui.overview

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
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
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.ViewModel
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import name.levis.ichor.R
import name.levis.ichor.data.TalosRepository
import name.levis.ichor.model.DiscoveredNode
import name.levis.ichor.model.NodeDiscovery
import name.levis.ichor.model.toOffer
import name.levis.ichor.ui.components.MutedText

/**
 * Cluster members the talosconfig context does not target (cluster discovery), offered to
 * add. Best effort: discovery off, or a node not answering, offers nothing.
 */
class NodeDiscoveryViewModel(private val talos: TalosRepository) : ViewModel() {
    private val _offer = MutableStateFlow<List<DiscoveredNode>>(emptyList())
    val offer: StateFlow<List<DiscoveredNode>> = _offer.asStateFlow()

    private var last: NodeDiscovery? = null

    /** Members set aside with "Not now", per context, until the app restarts. */
    private val dismissed = mutableMapOf<String, Set<String>>()

    suspend fun discover() {
        val discovery = try {
            talos.discoverNodes()
        } catch (e: CancellationException) {
            throw e
        } catch (_: Exception) {
            null
        }
        last = discovery
        _offer.value = discovery?.toOffer(dismissed[discovery.context].orEmpty()).orEmpty()
    }

    fun dismiss(nodes: List<DiscoveredNode>) {
        val discovery = last ?: return
        dismissed[discovery.context] = dismissed[discovery.context].orEmpty() + nodes.map { it.address }
        _offer.value = discovery.toOffer(dismissed[discovery.context].orEmpty())
    }

    fun clear() {
        last = null
        _offer.value = emptyList()
    }
}

/** Tells how many cluster members the talosconfig misses; tap to pick which to add. */
@Composable
fun DiscoveredNodesBanner(count: Int, onClick: () -> Unit) {
    Card(onClick = onClick, modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text(
                pluralStringResource(R.plurals.overview_discovered_nodes, count, count),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.primary,
            )
            MutedText(stringResource(R.string.overview_discovered_nodes_hint))
        }
    }
}

/** Picks which discovered [nodes] to add to the talosconfig (all by default). */
@Composable
fun AddDiscoveredNodesDialog(
    nodes: List<DiscoveredNode>,
    onAdd: (List<DiscoveredNode>) -> Unit,
    onNotNow: () -> Unit,
    onDismiss: () -> Unit,
) {
    var selected by remember(nodes) { mutableStateOf(nodes.map { it.address }.toSet()) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.discover_nodes_title)) },
        text = {
            Column {
                Text(stringResource(R.string.discover_nodes_body), style = MaterialTheme.typography.bodyMedium)
                LazyColumn(Modifier.heightIn(max = 360.dp).padding(top = 8.dp)) {
                    items(nodes, key = { it.address }) { node ->
                        val checked = node.address in selected
                        val toggle = { selected = if (checked) selected - node.address else selected + node.address }
                        Row(
                            verticalAlignment = Alignment.CenterVertically,
                            modifier = Modifier.fillMaxWidth().toggleable(value = checked, role = Role.Checkbox) { toggle() },
                        ) {
                            Checkbox(checked = checked, onCheckedChange = null)
                            Column {
                                Text(node.hostname.ifBlank { node.address }, style = MaterialTheme.typography.bodyLarge)
                                Text(
                                    "${node.address} · ${roleText(node.role)}",
                                    style = MaterialTheme.typography.bodySmall,
                                    fontFamily = FontFamily.Monospace,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = selected.isNotEmpty(),
                onClick = { onAdd(nodes.filter { it.address in selected }) },
            ) { Text(pluralStringResource(R.plurals.discover_nodes_add, selected.size, selected.size)) }
        },
        dismissButton = { TextButton(onClick = onNotNow) { Text(stringResource(R.string.discover_nodes_not_now)) } },
    )
}

@Composable
private fun roleText(role: String) = when (role) {
    "controlplane" -> stringResource(R.string.overview_role_control_plane)
    else -> role
}
