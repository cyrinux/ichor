package name.levis.ichor.ui.overview

import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.settings.openUrl
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import name.levis.ichor.R
import name.levis.ichor.TalosApp
import name.levis.ichor.model.NodeOverview
import name.levis.ichor.model.TalosUpdateCheck
import name.levis.ichor.model.TalosUpdateOffer
import name.levis.ichor.model.nodeVersionsCsv

/**
 * The latest Talos release as last answered, for the update card and the rollout of [nodes].
 * Asks the update checker (GitHub, at most every 6 hours) while composed.
 */
@Composable
fun rememberTalosUpdateCheck(nodes: List<NodeOverview>): TalosUpdateCheck? {
    val checker = (LocalContext.current.applicationContext as TalosApp).talosUpdateChecker
    val versions = remember(nodes) { nodeVersionsCsv(nodes) }
    LaunchedEffect(versions) { checker.check(versions) }
    val result by checker.result.collectAsStateWithLifecycle()
    // The latest release is the same whatever versions the check was asked for, and they change
    // node by node during an upgrade: the last answer stays while (or if) the next one is missing.
    return result?.second?.takeIf { it.latest.isNotEmpty() }
}

/**
 * "Talos vX is available · N nodes on older versions", with the release notes and [onSkip] to
 * leave that release aside. An os:admin ([canUpgrade]) taps it to open the rollout ([onRollout]).
 */
@Composable
fun TalosUpdateCard(offer: TalosUpdateOffer, canUpgrade: Boolean, onRollout: () -> Unit, onSkip: () -> Unit) {
    val context = LocalContext.current
    val content: @Composable () -> Unit = {
        Column(Modifier.padding(start = 16.dp, top = 16.dp, end = 16.dp, bottom = 4.dp)) {
            Text(
                stringResource(R.string.overview_talos_update, offer.latest) + " · " +
                    pluralStringResource(R.plurals.overview_talos_outdated, offer.outdated, offer.outdated),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.primary,
            )
            if (canUpgrade) {
                MutedText(stringResource(R.string.overview_talos_tap_upgrade))
            }
            Row(horizontalArrangement = Arrangement.SpaceBetween, modifier = Modifier.fillMaxWidth()) {
                if (offer.notes.startsWith("https://")) {
                    TextButton(onClick = { openUrl(context, offer.notes) }) { Text(stringResource(R.string.overview_talos_release_notes)) }
                } else {
                    Spacer(Modifier)
                }
                TextButton(onClick = onSkip) { Text(stringResource(R.string.overview_talos_skip, offer.latest)) }
            }
        }
    }
    if (canUpgrade) Card(onClick = onRollout, modifier = Modifier.fillMaxWidth()) { content() }
    else Card(Modifier.fillMaxWidth()) { content() }
}
