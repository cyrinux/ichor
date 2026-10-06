package name.levis.ichor.ui.overview

import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.settings.openUrl
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.nodeVersionsCsv
import name.levis.ichor.model.outdatedNodes

/**
 * "Talos vX is available · N nodes on older versions", with the release notes. An os:admin
 * ([canUpgrade]) taps it to open the rollout ([TalosRolloutDialog]), which stays open across the
 * upgrade screen and until closed, even once no reachable node is outdated any more.
 */
@Composable
fun TalosUpdateBanner(nodes: List<NodeOverview>, canUpgrade: Boolean, onUpgrade: (NodeOverview, String) -> Unit) {
    val context = LocalContext.current
    val checker = (context.applicationContext as TalosApp).talosUpdateChecker
    val versions = remember(nodes) { nodeVersionsCsv(nodes) }
    LaunchedEffect(versions) { checker.check(versions) }
    val result by checker.result.collectAsStateWithLifecycle()
    // The latest release is the same whatever versions the check was asked for, and they change
    // node by node during an upgrade: the last answer stays while (or if) the next one is missing.
    val check = result?.second?.takeIf { it.latest.isNotEmpty() } ?: return
    // Counted locally from the reachable nodes, like on iOS.
    val count = remember(nodes, check.latest) { outdatedNodes(nodes, check.latest).size }
    var choosing by rememberSaveable { mutableStateOf(false) }

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
    if (count > 0) {
        if (canUpgrade) Card(onClick = { choosing = true }, modifier = Modifier.fillMaxWidth()) { content() }
        else Card(Modifier.fillMaxWidth()) { content() }
    }

    if (choosing && canUpgrade) {
        TalosRolloutDialog(nodes, check.latest, onUpgrade = { onUpgrade(it, check.latest) }, onDismiss = { choosing = false })
    }
}
