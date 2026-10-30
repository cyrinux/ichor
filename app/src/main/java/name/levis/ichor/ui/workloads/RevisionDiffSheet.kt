package name.levis.ichor.ui.workloads

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import name.levis.ichor.R
import name.levis.ichor.model.ImageChange
import name.levis.ichor.model.KubePermission
import name.levis.ichor.model.KubeRevision
import name.levis.ichor.model.KubeRevisionDiff
import name.levis.ichor.model.KubeWorkload
import name.levis.ichor.model.changes
import name.levis.ichor.model.parseDiffLines
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.KubeDenialNote
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.diff.DiffLines
import name.levis.ichor.ui.node.HostnameConfirmDialog
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiStateOf
import name.levis.ichor.util.timeAgo

/**
 * What a rollback of [workload] to [revision] undoes: the images from → to on top, then the
 * pod template diff, and "Roll back to revision N" behind the typed workload name. [manual]:
 * the pods only take the template when deleted (OnDelete); [paused]: a paused Deployment,
 * refused until it resumes. [onRollback] runs once the name is typed.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun RevisionDiffSheet(
    workload: KubeWorkload,
    revision: KubeRevision,
    manual: Boolean,
    paused: Boolean,
    actions: WorkloadActions,
    denial: KubePermission?,
    busy: Boolean,
    onRollback: () -> Unit,
    onDismiss: () -> Unit,
) {
    val colors = LocalStatusColors.current
    val diff by produceState<UiState<KubeRevisionDiff>>(UiState.Loading, workload.key, revision.revision) {
        value = uiStateOf { actions.revisionDiff(workload, revision.revision) }
    }
    var confirm by remember { mutableStateOf(false) }

    ModalBottomSheet(onDismissRequest = onDismiss, sheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true)) {
        Column(
            Modifier.verticalScroll(rememberScrollState()).padding(start = 16.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    stringResource(R.string.revision_diff_title, revision.revision, workload.name),
                    style = MaterialTheme.typography.titleLarge,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                Text("${workload.kind}  ·  ${workload.namespace}", style = MaterialTheme.typography.bodySmall, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (revision.created > 0) MutedText(timeAgo(revision.created))
                if (revision.changeCause.isNotEmpty()) MutedText(revision.changeCause)
            }
            when (val s = diff) {
                UiState.Loading -> CircularProgressIndicator(Modifier.size(24.dp), strokeWidth = 2.dp)
                is UiState.Failed -> InlineError(s.message.asString())
                is UiState.Loaded -> DiffBody(s.data)
            }
            if (manual) Text(stringResource(R.string.revision_diff_manual), color = colors.warn, style = MaterialTheme.typography.bodySmall)
            if (paused) Text(stringResource(R.string.revision_diff_paused), color = colors.warn, style = MaterialTheme.typography.bodySmall)
            KubeDenialNote(denial)
            Button(
                onClick = { confirm = true },
                enabled = !busy && !paused && denial == null && !revision.current,
                colors = ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.error, contentColor = MaterialTheme.colorScheme.onError),
                modifier = Modifier.fillMaxWidth(),
            ) { Text(stringResource(R.string.revision_diff_rollback, revision.revision)) }
        }
    }

    if (confirm) {
        HostnameConfirmDialog(
            title = stringResource(R.string.workloads_rollback_title, workload.name, revision.revision),
            hostname = workload.name,
            confirmLabel = stringResource(R.string.workloads_rollback),
            onConfirm = {
                confirm = false
                onRollback()
            },
            onDismiss = { confirm = false },
        ) {
            val images = revision.images.joinToString(", ").ifEmpty { revision.replicaSet }
            Text(stringResource(R.string.workloads_rollback_text, workload.namespace, images), style = MaterialTheme.typography.bodyMedium)
        }
    }
}

@Composable
private fun DiffBody(diff: KubeRevisionDiff) {
    val changes = remember(diff) { diff.images.changes() }
    if (changes.isNotEmpty()) {
        SectionTitle(stringResource(R.string.revision_diff_images))
        changes.forEach { ImageChangeRow(it) }
    }
    val lines = remember(diff) { parseDiffLines(diff.diff) }
    if (lines.isEmpty()) {
        MutedText(stringResource(R.string.revision_diff_same))
    } else {
        SectionTitle(stringResource(R.string.revision_diff_changes))
        DiffLines(lines)
    }
}

/** "nginx:1.27 → nginx:1.25", read as "nginx:1.27, back to nginx:1.25". */
@Composable
private fun ImageChangeRow(change: ImageChange) {
    val none = stringResource(R.string.revision_diff_image_none)
    val from = change.from.ifEmpty { none }
    val to = change.to.ifEmpty { none }
    val description = stringResource(R.string.revision_diff_image_a11y, from, to)
    Text(
        stringResource(R.string.revision_diff_image, from, to),
        style = MaterialTheme.typography.labelMedium,
        fontFamily = FontFamily.Monospace,
        color = LocalStatusColors.current.warn,
        modifier = Modifier.fillMaxWidth().semantics { contentDescription = description },
    )
}
