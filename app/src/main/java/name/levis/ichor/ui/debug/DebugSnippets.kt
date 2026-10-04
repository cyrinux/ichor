package name.levis.ichor.ui.debug

import androidx.annotation.StringRes
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.serialization.Serializable
import kotlinx.serialization.builtins.ListSerializer
import name.levis.ichor.R
import name.levis.ichor.data.TalosJson
import name.levis.ichor.ui.components.MutedText

/**
 * A ready-made debug shell command, listed by the Go core. A [run] snippet is sent with
 * Enter; the others end where the argument goes, for the user to type it.
 */
@Serializable
data class DebugSnippet(val group: String, val label: String, val command: String, val run: Boolean = false) {
    /** The bytes to send to the TTY: Enter is a carriage return, as a keyboard sends it. */
    fun bytes(): ByteArray = (if (run) "$command\r" else command).toByteArray()

    /** The command as listed: a typed one shows … where its argument goes. */
    val display: String get() = if (run) command else command.trimEnd() + " …"
}

fun decodeDebugSnippets(json: String): List<DebugSnippet> =
    TalosJson.decodeFromString(ListSerializer(DebugSnippet.serializer()), json)

/** Title of a snippet group; an unknown group (from a newer core) shows its key. */
@StringRes
fun debugGroupTitle(group: String): Int? = when (group) {
    "interfaces" -> R.string.debug_group_interfaces
    "control_plane" -> R.string.debug_group_control_plane
    "dns" -> R.string.debug_group_dns
    "reachability" -> R.string.debug_group_reachability
    "mtu" -> R.string.debug_group_mtu
    "tls" -> R.string.debug_group_tls
    "firewall" -> R.string.debug_group_firewall
    "kubespan" -> R.string.debug_group_kubespan
    "capture" -> R.string.debug_group_capture
    "node" -> R.string.debug_group_node
    "throughput" -> R.string.debug_group_throughput
    else -> null
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DebugSnippetsSheet(snippets: List<DebugSnippet>, onPick: (DebugSnippet) -> Unit, onDismiss: () -> Unit) {
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Text(
            stringResource(R.string.debug_snippets),
            style = MaterialTheme.typography.titleMedium,
            modifier = Modifier.padding(horizontal = 16.dp),
        )
        MutedText(
            stringResource(R.string.debug_snippets_hint),
            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
        )
        LazyColumn(contentPadding = PaddingValues(bottom = 32.dp)) {
            snippets.groupBy { it.group }.forEach { (group, items) ->
                item(key = "group-$group") {
                    Text(
                        debugGroupTitle(group)?.let { stringResource(it) } ?: group,
                        style = MaterialTheme.typography.labelLarge,
                        color = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.padding(start = 16.dp, end = 16.dp, top = 16.dp, bottom = 4.dp),
                    )
                }
                items(items, key = { "${it.group}/${it.label}" }) { snippet -> SnippetRow(snippet, onPick) }
            }
        }
    }
}

@Composable
private fun SnippetRow(snippet: DebugSnippet, onPick: (DebugSnippet) -> Unit) {
    Column(Modifier.fillMaxWidth().clickable { onPick(snippet) }.padding(horizontal = 16.dp, vertical = 8.dp)) {
        Text(snippet.label, style = MaterialTheme.typography.bodyMedium)
        Text(
            snippet.display,
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
    }
}
