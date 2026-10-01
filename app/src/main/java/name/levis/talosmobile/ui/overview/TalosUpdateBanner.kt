package name.levis.talosmobile.ui.overview

import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.talosmobile.R
import name.levis.talosmobile.TalosApp
import name.levis.talosmobile.model.NodeOverview
import name.levis.talosmobile.model.nodeVersionsCsv
import name.levis.talosmobile.model.outdatedNodes

/**
 * "Talos vX is available · N nodes on older versions", with the release notes. An os:admin
 * ([canUpgrade]) taps it to pick an outdated node and open its upgrade with that version.
 */
@Composable
fun TalosUpdateBanner(nodes: List<NodeOverview>, canUpgrade: Boolean, onUpgrade: (NodeOverview, String) -> Unit) {
    val context = LocalContext.current
    val checker = (context.applicationContext as TalosApp).talosUpdateChecker
    val versions = remember(nodes) { nodeVersionsCsv(nodes) }
    LaunchedEffect(versions) { checker.check(versions) }
    val result by checker.result.collectAsStateWithLifecycle()
    val check = result?.takeIf { it.first == versions }?.second ?: return
    if (!check.newer || check.latest.isEmpty()) return
    val outdated = remember(nodes, check.latest) { outdatedNodes(nodes, check.latest) }
    var choosing by remember { mutableStateOf(false) }
    // Counted locally from the reachable nodes, like on iOS.
    val count = outdated.size
    if (count == 0) return
    val tappable = canUpgrade && outdated.isNotEmpty()

    val content: @Composable () -> Unit = {
        Column(Modifier.padding(start = 16.dp, top = 16.dp, end = 16.dp, bottom = 4.dp)) {
            Text(
                stringResource(R.string.overview_talos_update, check.latest) + " · " +
                    pluralStringResource(R.plurals.overview_talos_outdated, count, count),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.primary,
            )
            if (tappable) {
                Text(
                    stringResource(R.string.overview_talos_tap_upgrade),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (check.notes.startsWith("https://")) {
                TextButton(onClick = { openUrl(context, check.notes) }) { Text(stringResource(R.string.overview_talos_release_notes)) }
            }
        }
    }
    if (tappable) Card(onClick = { choosing = true }, modifier = Modifier.fillMaxWidth()) { content() }
    else Card(Modifier.fillMaxWidth()) { content() }

    if (choosing) {
        AlertDialog(
            onDismissRequest = { choosing = false },
            title = { Text(stringResource(R.string.overview_talos_pick_node, check.latest)) },
            text = {
                Column {
                    Text(
                        stringResource(R.string.overview_talos_pick_hint),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(bottom = 8.dp),
                    )
                    outdated.forEach { node ->
                        Column(
                            Modifier.fillMaxWidth().clickable {
                                choosing = false
                                onUpgrade(node, check.latest)
                            }.padding(vertical = 8.dp),
                        ) {
                            Text(node.hostname, style = MaterialTheme.typography.bodyLarge)
                            Text(
                                "${node.version} · ${node.node}",
                                style = MaterialTheme.typography.bodySmall,
                                fontFamily = FontFamily.Monospace,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                }
            },
            confirmButton = {},
            dismissButton = { TextButton(onClick = { choosing = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

private fun openUrl(context: android.content.Context, url: String) {
    try {
        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
    } catch (_: ActivityNotFoundException) {
        // No browser: nothing to do.
    }
}
