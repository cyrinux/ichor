package name.levis.ichor.ui.updates

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import name.levis.ichor.ui.design.IchorAssistChip
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import name.levis.ichor.ui.design.IchorFilterChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.UpdateAutomation
import name.levis.ichor.model.UpdateRow
import name.levis.ichor.model.UpdateStep
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.StatusPill

/**
 * One image or chart: its name, "running → newest", who applied it, then a chip per candidate
 * (patch, minor, major: a tap opens that version's edit), the badge of what already bumps it,
 * or why it is not comparable. [resolving] while its file is being found; [note] says why a
 * tap led nowhere. While [selecting], a row that can be proposed has a checkbox ([onToggle])
 * and its chips pick the version it is ticked with ([selectedVersion]).
 */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun UpdateRowItem(
    row: UpdateRow,
    resolving: Boolean,
    note: UiText?,
    onPick: (String) -> Unit,
    modifier: Modifier = Modifier,
    selecting: Boolean = false,
    selectedVersion: String? = null,
    onToggle: () -> Unit = {},
) {
    val selectable = selecting && row.editable && row.candidates.isNotEmpty()
    Column(modifier.fillMaxWidth().padding(vertical = 8.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            if (selectable) Checkbox(checked = selectedVersion != null, onCheckedChange = { onToggle() })
            Text(
                row.name,
                style = MaterialTheme.typography.bodyLarge,
                fontFamily = FontFamily.Monospace,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            if (resolving) CircularProgressIndicator(Modifier.size(16.dp), strokeWidth = 2.dp)
        }
        val newest = row.newest
        MutedText(if (newest.isEmpty()) row.running else stringResource(R.string.updates_running_newest, row.running, newest))
        row.owner?.takeIf { it.name.isNotEmpty() }?.let { o -> MutedText("${o.kind} ${o.namespace}/${o.name}", maxLines = 1, overflow = TextOverflow.Ellipsis) }
        val automation = row.automation
        when {
            automation != null -> StatusPill(stringResource(automationLabel(automation)), MaterialTheme.colorScheme.primary)
            row.candidates.isNotEmpty() -> FlowRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                row.candidates.forEach { c ->
                    val label = @Composable { Text(stringResource(stepLabel(c.step), c.version), maxLines = 1, overflow = TextOverflow.Ellipsis) }
                    if (selectable) {
                        IchorFilterChip(selected = c.version == selectedVersion, onClick = { onPick(c.version) }, label = label)
                    } else {
                        IchorAssistChip(onClick = { onPick(c.version) }, enabled = row.editable && !resolving && !selecting, label = label)
                    }
                }
            }
            row.reason.isNotEmpty() -> MutedText(row.reason)
        }
        if (note != null) Text(note.asString(), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
    }
}

private fun stepLabel(step: UpdateStep): Int = when (step) {
    UpdateStep.PATCH -> R.string.updates_chip_patch
    UpdateStep.MINOR -> R.string.updates_chip_minor
    UpdateStep.MAJOR -> R.string.updates_chip_major
}

private fun automationLabel(a: UpdateAutomation): Int = when (a) {
    UpdateAutomation.FLUX_IMAGE -> R.string.updates_badge_flux
    UpdateAutomation.ARGO_IMAGE_UPDATER -> R.string.updates_badge_argo
    UpdateAutomation.SEMVER_RANGE -> R.string.updates_badge_range
    UpdateAutomation.BOT -> R.string.updates_badge_bot
}
