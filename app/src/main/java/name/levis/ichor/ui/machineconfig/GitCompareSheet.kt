package name.levis.ichor.ui.machineconfig

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.OpenInNew
import androidx.compose.material3.Button
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import name.levis.ichor.R
import name.levis.ichor.data.GitOpsRepository
import name.levis.ichor.model.TalosRepoCompare
import name.levis.ichor.model.TalosRepoFile
import name.levis.ichor.model.TalosRepoFileKind
import name.levis.ichor.model.TalosRepoFiles
import name.levis.ichor.model.orderedSelection
import name.levis.ichor.model.shortRevision
import name.levis.ichor.ui.UiState
import name.levis.ichor.ui.asString
import name.levis.ichor.ui.cancellableCatching
import name.levis.ichor.ui.components.InlineError
import name.levis.ichor.ui.components.MutedText
import name.levis.ichor.ui.components.SectionTitle
import name.levis.ichor.ui.components.StatusPill
import name.levis.ichor.ui.components.Tag
import name.levis.ichor.ui.components.TooltipIconButton
import name.levis.ichor.ui.diff.DiffResourceCard
import name.levis.ichor.ui.theme.LocalStatusColors
import name.levis.ichor.ui.uiText

/**
 * The node compared with the cluster repository: the files that configure it there (the rule
 * that matched each), the ones to apply picked, and Compare applies them onto the node's live
 * config. "Same as Git", or the diff with [onEditDraft] to bring Git's version onto the node
 * through the editor's review; [onOpenFile] opens a repository file at a line.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun GitCompareSheet(
    repository: GitOpsRepository,
    node: String,
    hostname: String,
    role: String,
    onOpenFile: (path: String, line: Int) -> Unit,
    onEditDraft: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    var files by remember { mutableStateOf<UiState<TalosRepoFiles>>(UiState.Loading) }
    var selected by remember { mutableStateOf(emptySet<String>()) }
    var compare by remember { mutableStateOf<UiState<TalosRepoCompare>?>(null) }
    LaunchedEffect(hostname) {
        files = cancellableCatching { repository.talosRepoFiles(hostname, role) }.fold(
            onSuccess = { found -> selected = found.defaultSelection; UiState.Loaded(found) },
            onFailure = { UiState.Failed(it.uiText()) },
        )
    }

    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(
            Modifier.fillMaxWidth().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp).padding(bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text(stringResource(R.string.talos_git_title), style = MaterialTheme.typography.titleLarge)
            when (val f = files) {
                UiState.Loading -> CircularProgressIndicator(Modifier.padding(vertical = 24.dp))
                is UiState.Failed -> InlineError(f.message.asString())
                is UiState.Loaded -> {
                    val found = f.data
                    MutedText(stringResource(R.string.talos_git_at, shortRevision(found.sha)))
                    if (found.generator == "talhelper" || found.generator == "talosctl") {
                        MutedText(stringResource(R.string.talos_git_generator, found.generator))
                    }
                    if (found.note.isNotEmpty()) MutedText(found.note)
                    if (found.files.isEmpty()) MutedText(stringResource(R.string.talos_git_none))
                    found.files.forEach { file ->
                        FileRow(
                            file,
                            checked = file.path in selected,
                            onCheck = { on -> selected = if (on) selected + file.path else selected - file.path },
                            onOpen = { onOpenFile(file.file, file.line) },
                        )
                    }
                    if (found.files.any { it.patch }) {
                        MutedText(stringResource(R.string.talos_git_order))
                        Button(
                            onClick = {
                                compare = UiState.Loading
                                scope.launch {
                                    val paths = orderedSelection(found.files, selected)
                                    compare = cancellableCatching { repository.talosRepoCompare(node, hostname, found.sha, paths) }
                                        .fold(onSuccess = { UiState.Loaded(it) }, onFailure = { UiState.Failed(it.uiText()) })
                                }
                            },
                            enabled = selected.isNotEmpty() && compare != UiState.Loading,
                            modifier = Modifier.fillMaxWidth(),
                        ) { Text(stringResource(R.string.talos_git_compare)) }
                    }
                }
            }
            when (val c = compare) {
                null -> Unit
                UiState.Loading -> CircularProgressIndicator(Modifier.padding(vertical = 24.dp))
                is UiState.Failed -> InlineError(c.message.asString())
                is UiState.Loaded -> CompareResult(c.data, onEditDraft)
            }
        }
    }
}

/** A file found: its check box (greyed when it cannot be applied), path, kind and rule. */
@Composable
private fun FileRow(file: TalosRepoFile, checked: Boolean, onCheck: (Boolean) -> Unit, onOpen: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Checkbox(checked = checked && file.patch, onCheckedChange = onCheck, enabled = file.patch)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                Tag(stringResource(file.kind.label()))
                Text(file.path, style = MaterialTheme.typography.bodyMedium, fontFamily = FontFamily.Monospace)
            }
            MutedText(file.why)
        }
        TooltipIconButton(Icons.AutoMirrored.Outlined.OpenInNew, stringResource(R.string.talos_git_open_file), onClick = onOpen)
    }
}

/** "Same as Git", or the diff, the reboot it needs, the files skipped and Edit from this diff. */
@Composable
private fun CompareResult(result: TalosRepoCompare, onEditDraft: (String) -> Unit) {
    val colors = LocalStatusColors.current
    result.skipped.forEach { InlineError(stringResource(R.string.talos_git_skipped, it.path, it.why)) }
    if (result.same) {
        StatusPill(stringResource(R.string.talos_git_same), colors.ok)
        MutedText(stringResource(R.string.talos_git_same_desc))
        return
    }
    SectionTitle(stringResource(R.string.talos_git_differs))
    if (result.needsReboot) StatusPill(stringResource(R.string.talos_git_reboot), colors.warn)
    var expanded by remember { mutableStateOf(true) }
    result.diff.resources.forEach { r -> DiffResourceCard(r, expanded = expanded) { expanded = !expanded } }
    OutlinedButton(onClick = { onEditDraft(result.draft) }, modifier = Modifier.fillMaxWidth()) {
        Text(stringResource(R.string.talos_git_edit_from_diff))
    }
    MutedText(stringResource(R.string.talos_git_edit_from_diff_desc))
}

private fun TalosRepoFileKind.label(): Int = when (this) {
    TalosRepoFileKind.NODE -> R.string.talos_git_kind_node
    TalosRepoFileKind.SHARED -> R.string.talos_git_kind_shared
    TalosRepoFileKind.TALCONFIG -> R.string.talos_git_kind_talconfig
    TalosRepoFileKind.SECRETS -> R.string.talos_git_kind_secrets
}
