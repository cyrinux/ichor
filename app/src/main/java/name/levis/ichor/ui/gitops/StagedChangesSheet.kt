package name.levis.ichor.ui.gitops

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.Close
import androidx.compose.material.icons.outlined.EditNote
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AssistChipDefaults
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.theme.LocalStatusColors

/** "Staged changes (2)": opens the Staged changes sheet. */
@Composable
fun StagedChip(count: Int, onClick: () -> Unit, modifier: Modifier = Modifier) {
    AssistChip(
        onClick = onClick,
        label = { Text(stringResource(R.string.staged_chip, count.toString())) },
        leadingIcon = { Icon(Icons.Outlined.EditNote, contentDescription = null, modifier = Modifier.size(AssistChipDefaults.IconSize)) },
        modifier = modifier,
    )
}

/**
 * The files staged for one pull request: each with its line counts, opened again for editing
 * ([onEdit]) or removed; then Open pull request. Once it is open, its link.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun StagedChangesSheet(
    state: StagedChangesState,
    onEdit: (String) -> Unit,
    onRemove: (String) -> Unit,
    onPropose: () -> Unit,
    onDismiss: () -> Unit,
) {
    val sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = sheetState) {
        val result = state.result
        if (result != null) {
            ProposeResult(result, tool = "", onDone = onDismiss, modifier = Modifier.navigationBarsPadding())
            return@ModalBottomSheet
        }
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(horizontal = 20.dp).padding(bottom = 16.dp).navigationBarsPadding(),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(stringResource(R.string.staged_title), style = MaterialTheme.typography.titleLarge)
            MutedText(stringResource(R.string.staged_session_note))
            state.rows.forEach { row -> StagedFileRow(row, onEdit = { onEdit(row.file.path) }, onRemove = { onRemove(row.file.path) }) }
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(8.dp, Alignment.End)) {
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.common_close)) }
                Button(onClick = onPropose, enabled = state.rows.isNotEmpty()) { Text(stringResource(R.string.gitops_propose_open)) }
            }
        }
    }
}

@Composable
private fun StagedFileRow(row: StagedRow, onEdit: () -> Unit, onRemove: () -> Unit) {
    val colors = LocalStatusColors.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(
            Modifier.weight(1f).clickable(role = Role.Button, onClickLabel = stringResource(R.string.gitops_edit), onClick = onEdit).padding(vertical = 6.dp),
        ) {
            Text(row.file.path.substringAfterLast('/'), style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace, maxLines = 1, overflow = TextOverflow.Ellipsis)
            MutedText(row.file.path, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (row.added != null && row.removed != null) {
            Text("+${row.added}", color = colors.ok, style = MaterialTheme.typography.labelLarge, fontFamily = FontFamily.Monospace)
            Text("−${row.removed}", color = colors.bad, style = MaterialTheme.typography.labelLarge, fontFamily = FontFamily.Monospace, modifier = Modifier.padding(start = 6.dp))
        }
        TooltipIconButton(Icons.Outlined.Close, stringResource(R.string.staged_remove), onClick = onRemove)
    }
}
