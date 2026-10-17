package name.levis.ichor.ui.argocd

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.DiffChange
import name.levis.ichor.model.FluxDiff
import name.levis.ichor.model.KubeDiffResource
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.diff.DiffCounts
import name.levis.ichor.ui.diff.DiffResourceCard

/** The sheet lists this many objects at most; the full diff screen has the rest. */
private const val SYNC_SHEET_MAX_CARDS = 20

/** Counts by change of [resources], in [DiffChange] order (as [FluxDiff.counts]). */
fun changeCounts(resources: List<KubeDiffResource>): List<Pair<DiffChange, Int>> = FluxDiff(resources = resources).counts

/**
 * What the sync would change, from Argo CD's last comparison: [changes] are the objects of the
 * ticked set (all of them when none is ticked), null while [diff] loads or when it failed.
 * [onFullDiff] opens the diff screen (null when the screen cannot).
 */
@Composable
fun ArgoSyncChanges(diff: UiState<FluxDiff>, changes: List<KubeDiffResource>?, onFullDiff: (() -> Unit)?) {
    var expanded by remember { mutableStateOf(setOf<String>()) }
    Column(Modifier.fillMaxWidth().padding(bottom = 8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(stringResource(R.string.argo_sync_changes), style = MaterialTheme.typography.titleSmall)
        when {
            diff is UiState.Failed -> {
                InlineError(diff.message.asString())
                FullDiffLink(onFullDiff)
            }
            changes == null -> {
                LinearProgressIndicator(Modifier.fillMaxWidth())
                MutedText(stringResource(R.string.argo_sync_changes_loading))
            }
            changes.isEmpty() -> MutedText(stringResource(R.string.argo_sync_changes_none))
            else -> {
                DiffCounts(changeCounts(changes))
                changes.take(SYNC_SHEET_MAX_CARDS).forEach { r ->
                    DiffResourceCard(r, expanded = r.key in expanded) {
                        expanded = if (r.key in expanded) expanded - r.key else expanded + r.key
                    }
                }
                if (changes.size > SYNC_SHEET_MAX_CARDS) {
                    MutedText(stringResource(R.string.argo_sync_changes_more, (changes.size - SYNC_SHEET_MAX_CARDS).toString()))
                    FullDiffLink(onFullDiff)
                }
            }
        }
    }
}

@Composable
private fun FullDiffLink(onFullDiff: (() -> Unit)?) {
    if (onFullDiff == null) return
    TextButton(onClick = onFullDiff) { Text(stringResource(R.string.argo_sync_open_full_diff)) }
}
