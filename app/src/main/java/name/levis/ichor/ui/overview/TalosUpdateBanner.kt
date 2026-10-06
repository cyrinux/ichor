package name.levis.ichor.ui.overview

import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.settings.openUrl
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
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
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.UpgradeChoices
import name.levis.ichor.model.nodeVersionsCsv
import name.levis.ichor.model.upgradeChoices

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
    val choices = remember(nodes, check.latest) { upgradeChoices(nodes, check.latest) }
    var choosing by remember { mutableStateOf(false) }
    // Counted locally from the reachable nodes, like on iOS.
    val count = choices.count
    if (count == 0) return

    val content: @Composable () -> Unit = {
        Column(Modifier.padding(start = 16.dp, top = 16.dp, end = 16.dp, bottom = 4.dp)) {
            Text(
                stringResource(R.string.overview_talos_update, check.latest) + " · " +
                    pluralStringResource(R.plurals.overview_talos_outdated, count, count),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.primary,
            )
            if (canUpgrade) {
                MutedText(stringResource(R.string.overview_talos_tap_upgrade))
            }
            if (check.notes.startsWith("https://")) {
                TextButton(onClick = { openUrl(context, check.notes) }) { Text(stringResource(R.string.overview_talos_release_notes)) }
            }
        }
    }
    if (canUpgrade) Card(onClick = { choosing = true }, modifier = Modifier.fillMaxWidth()) { content() }
    else Card(Modifier.fillMaxWidth()) { content() }

    if (choosing) {
        AlertDialog(
            onDismissRequest = { choosing = false },
            title = { Text(stringResource(R.string.overview_talos_pick_node, check.latest)) },
            text = {
                UpgradeChoiceList(choices) { node ->
                    choosing = false
                    onUpgrade(node, check.latest)
                }
            },
            confirmButton = {},
            dismissButton = { TextButton(onClick = { choosing = false }) { Text(stringResource(R.string.common_cancel)) } },
        )
    }
}

/**
 * The outdated nodes under their role, control plane first. The workers stay tappable while a
 * control-plane node is outdated (the order is advice, and a new release is often tried on a
 * worker first), only less prominent.
 */
@Composable
private fun UpgradeChoiceList(choices: UpgradeChoices, onPick: (NodeOverview) -> Unit) {
    // Scrolls: right after a release, a large cluster lists more nodes than fit the dialog.
    Column(Modifier.verticalScroll(rememberScrollState())) {
        MutedText(stringResource(R.string.overview_talos_pick_hint))
        if (choices.controlPlane.isNotEmpty()) {
            SectionTitle(stringResource(R.string.upgrade_control_plane))
            choices.controlPlane.forEach { UpgradeChoiceRow(it, muted = false, onPick) }
        }
        if (choices.workers.isNotEmpty()) {
            val title = if (choices.workersWait) R.string.overview_talos_pick_workers_later else R.string.overview_talos_pick_workers
            SectionTitle(stringResource(title))
            choices.workers.forEach { UpgradeChoiceRow(it, muted = choices.workersWait, onPick) }
        }
    }
}

/** Less prominent than the other rows, yet clearly not disabled (Material dims those to 0.38). */
private const val MUTED_CHOICE_ALPHA = 0.6f

@Composable
private fun UpgradeChoiceRow(node: NodeOverview, muted: Boolean, onPick: (NodeOverview) -> Unit) {
    val row = Modifier.fillMaxWidth().clickable { onPick(node) }.padding(vertical = 8.dp)
    Column(if (muted) row.alpha(MUTED_CHOICE_ALPHA) else row) {
        Text(node.hostname, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurface)
        Text(
            "${node.version} · ${node.node}",
            style = MaterialTheme.typography.bodySmall,
            fontFamily = FontFamily.Monospace,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
