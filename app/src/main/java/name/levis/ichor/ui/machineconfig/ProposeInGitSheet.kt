package name.levis.ichor.ui.machineconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.model.QuickEdit
import name.levis.ichor.model.TalosConfigChange
import name.levis.ichor.model.TalosConfigPatch
import name.levis.ichor.model.TalosWriteBackPlan
import name.levis.ichor.model.changeValueText
import name.levis.ichor.model.encodeQuickEdit
import name.levis.ichor.ui.LocalizedException
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.UiText
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.uiText

/** What a write-back proposes: the values, and where they go. */
private data class WriteBack(val patch: TalosConfigPatch, val plan: TalosWriteBackPlan)

/**
 * A machine config change brought into the cluster repository: the values [draft] changes from
 * [base] (both redacted), the node's file they go into (or the new one), the values a shared
 * patch sets left out, then [onReview] opens the editor's review of that file with them written
 * (the path, and the quick edit for the route).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ProposeInGitSheet(
    repository: GitOpsRepository,
    base: String,
    draft: String,
    hostname: String,
    role: String,
    onReview: (path: String, quick: String) -> Unit,
    onDismiss: () -> Unit,
) {
    var state by remember { mutableStateOf<UiState<WriteBack>>(UiState.Loading) }
    LaunchedEffect(base, draft) {
        state = cancellableCatching {
            val patch = repository.talosConfigPatchFrom(base, draft)
            if (patch.touchesSecrets) throw LocalizedException(UiText.Res(R.string.talos_writeback_secret))
            WriteBack(patch, repository.talosWriteBackPlan(hostname, role, patch.patch))
        }.fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(stringResource(R.string.talos_writeback_title), style = MaterialTheme.typography.titleLarge)
            when (val s = state) {
                UiState.Loading -> CircularProgressIndicator(Modifier.padding(vertical = 24.dp))
                is UiState.Failed -> InlineError(s.message.asString())
                is UiState.Loaded -> Plan(s.data, hostname, onReview)
            }
        }
    }
}

@Composable
private fun Plan(writeBack: WriteBack, hostname: String, onReview: (String, String) -> Unit) {
    val (patch, plan) = writeBack
    SectionTitle(stringResource(R.string.talos_writeback_changes))
    patch.changes.forEach { ChangeRow(it) }
    patch.notes.forEach { MutedText(it) }
    MutedText(stringResource(if (plan.create) R.string.talos_writeback_create else R.string.talos_writeback_into, plan.file))
    plan.skipped.forEach { InlineError(stringResource(R.string.talos_git_skipped, it.path, it.why)) }
    if (plan.patch.isBlank()) {
        MutedText(stringResource(R.string.talos_writeback_nothing))
        return
    }
    Button(
        onClick = { onReview(plan.file, encodeQuickEdit(QuickEdit.talos(hostname, plan, patch.firstPath))) },
        modifier = Modifier.fillMaxWidth(),
    ) { Text(stringResource(R.string.talos_writeback_review)) }
}

@Composable
private fun ChangeRow(change: TalosConfigChange) {
    Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
        Text(change.path.ifEmpty { change.kind }, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
        MutedText(
            if (change.removed) stringResource(R.string.talos_writeback_removed)
            else stringResource(R.string.talos_writeback_from_to, changeValueText(change.from), changeValueText(change.to)),
        )
    }
}
